package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

// A real Docker daemon pulls an image from a small local registry that can
// be slowed down or made to stop sending in the middle of a layer
// (ACORNFOX_DOCKER_IT=1, on a machine with Docker and no AcornFox; see
// docker_it_test.go). Every run makes a new image with random content, so
// nothing is left from an earlier run. The image is built like the ones
// BuildKit pushes: an index with the image manifest and an attestation
// manifest next to it.

type fakeBlob struct {
	mediaType string
	data      []byte
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

// fakeImage is an image in a fake registry: index digest, the big layer.
type fakeImage struct {
	repo     string
	index    string
	big      string
	bigSize  int64
	blobs    map[string]fakeBlob // layers, configs
	manifest map[string]fakeBlob // index and manifests by digest
}

func tarGz(t *testing.T, name string, body []byte) (gz, raw []byte) {
	t.Helper()
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), ModTime: time.Unix(0, 0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var gb bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gb, gzip.NoCompression)
	_, _ = zw.Write(tb.Bytes())
	_ = zw.Close()
	return gb.Bytes(), tb.Bytes()
}

// newFakeImage makes repo's image: a small layer, a big layer of bigMB
// random bytes, and (with attest) an attestation manifest.
func newFakeImage(t *testing.T, repo string, bigMB int, attest bool) *fakeImage {
	t.Helper()
	img := &fakeImage{repo: repo, blobs: map[string]fakeBlob{}, manifest: map[string]fakeBlob{}}
	const layerType = "application/vnd.oci.image.layer.v1.tar+gzip"
	small, smallRaw := tarGz(t, "hello.txt", []byte("hello\n"))
	body := make([]byte, bigMB<<20)
	if _, err := rand.Read(body); err != nil {
		t.Fatal(err)
	}
	big, bigRaw := tarGz(t, "big.bin", body)
	img.blobs[sha(small)] = fakeBlob{layerType, small}
	img.blobs[sha(big)] = fakeBlob{layerType, big}
	img.big, img.bigSize = sha(big), int64(len(big))

	cfg, _ := json.Marshal(map[string]any{
		"architecture": "amd64", "os": "linux",
		"config": map[string]any{"Cmd": []string{"/big.bin"}},
		"rootfs": map[string]any{"type": "layers", "diff_ids": []string{sha(smallRaw), sha(bigRaw)}},
	})
	if arch := platformArch(nil); arch != "amd64" {
		cfg = bytes.Replace(cfg, []byte(`"amd64"`), []byte(`"`+arch+`"`), 1)
	}
	img.blobs[sha(cfg)] = fakeBlob{"application/vnd.oci.image.config.v1+json", cfg}
	desc := func(mt string, b []byte, extra map[string]any) map[string]any {
		m := map[string]any{"mediaType": mt, "digest": sha(b), "size": len(b)}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	man, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": desc("application/vnd.oci.image.config.v1+json", cfg, nil),
		"layers": []any{desc(layerType, small, nil), desc(layerType, big, nil)},
	})
	img.manifest[sha(man)] = fakeBlob{"application/vnd.oci.image.manifest.v1+json", man}
	manifests := []any{desc("application/vnd.oci.image.manifest.v1+json", man,
		map[string]any{"platform": map[string]string{"architecture": platformArch(nil), "os": "linux"}})}
	if attest {
		stmt := []byte(`{"_type":"https://in-toto.io/Statement/v0.1","predicateType":"https://slsa.dev/provenance/v0.2","subject":[]}`)
		img.blobs[sha(stmt)] = fakeBlob{"application/vnd.in-toto+json", stmt}
		acfg, _ := json.Marshal(map[string]any{"architecture": "unknown", "os": "unknown", "config": map[string]any{},
			"rootfs": map[string]any{"type": "layers", "diff_ids": []string{sha(stmt)}}})
		img.blobs[sha(acfg)] = fakeBlob{"application/vnd.oci.image.config.v1+json", acfg}
		aman, _ := json.Marshal(map[string]any{
			"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
			"config": desc("application/vnd.oci.image.config.v1+json", acfg, nil),
			"layers": []any{desc("application/vnd.in-toto+json", stmt,
				map[string]any{"annotations": map[string]string{"in-toto.io/predicate-type": "https://slsa.dev/provenance/v0.2"}})},
		})
		img.manifest[sha(aman)] = fakeBlob{"application/vnd.oci.image.manifest.v1+json", aman}
		manifests = append(manifests, desc("application/vnd.oci.image.manifest.v1+json", aman, map[string]any{
			"platform": map[string]string{"architecture": "unknown", "os": "unknown"},
			"annotations": map[string]string{
				"vnd.docker.reference.digest": sha(man),
				"vnd.docker.reference.type":   "attestation-manifest",
			},
		}))
	}
	idx, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": manifests,
	})
	img.index = sha(idx)
	img.manifest[img.index] = fakeBlob{"application/vnd.oci.image.index.v1+json", idx}
	return img
}

// regBehavior is how one fake registry sends the big layer: at rate bytes
// per second (0: as fast as it can), and with stallAt > 0 it stops sending
// once the layer reaches that offset (the connection stays open).
type regBehavior struct {
	rate    int64
	stallAt int64
}

type itRegistry struct {
	img  *fakeImage
	srv  *httptest.Server
	host string
	beh  atomic.Pointer[regBehavior]
	sent atomic.Int64 // bytes of the big layer sent
	log  func(format string, a ...any)
}

func startITRegistry(t *testing.T, img *fakeImage, b regBehavior) *itRegistry {
	t.Helper()
	r := &itRegistry{img: img, log: t.Logf}
	r.beh.Store(&b)
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	r.host = strings.TrimPrefix(r.srv.URL, "http://")
	return r
}

func (r *itRegistry) serve(w http.ResponseWriter, req *http.Request) {
	p := req.URL.Path
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	if p == "/v2/" || p == "/v2" {
		w.WriteHeader(http.StatusOK)
		return
	}
	pre := "/v2/" + r.img.repo + "/"
	if !strings.HasPrefix(p, pre) {
		http.NotFound(w, req)
		return
	}
	rest := strings.TrimPrefix(p, pre)
	switch {
	case strings.HasPrefix(rest, "manifests/"):
		m, ok := r.img.manifest[strings.TrimPrefix(rest, "manifests/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", m.mediaType)
		w.Header().Set("Docker-Content-Digest", sha(m.data))
		w.Header().Set("Content-Length", strconv.Itoa(len(m.data)))
		if req.Method != http.MethodHead {
			_, _ = w.Write(m.data)
		}
	case strings.HasPrefix(rest, "referrers/"):
		w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
		_, _ = w.Write([]byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`))
	case strings.HasPrefix(rest, "blobs/"):
		d := strings.TrimPrefix(rest, "blobs/")
		b, ok := r.img.blobs[d]
		if !ok {
			http.NotFound(w, req)
			return
		}
		r.blob(w, req, d, b)
	default:
		http.NotFound(w, req)
	}
}

func (r *itRegistry) blob(w http.ResponseWriter, req *http.Request, d string, b fakeBlob) {
	data := b.data
	from := int64(0)
	if rg := req.Header.Get("Range"); strings.HasPrefix(rg, "bytes=") {
		s, _, _ := strings.Cut(strings.TrimPrefix(rg, "bytes="), "-")
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n < int64(len(data)) {
			from = n
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", d)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(int64(len(data))-from, 10))
	if from > 0 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
	}
	if req.Method == http.MethodHead {
		return
	}
	if d != r.img.big {
		_, _ = w.Write(data[from:])
		return
	}
	r.log("%s: big layer %s from offset %d", r.host, req.Method, from)
	const chunk = 32 << 10
	t0 := time.Now()
	sent := int64(0)
	for off := from; off < int64(len(data)); {
		beh := r.beh.Load()
		if beh.stallAt > 0 && off >= beh.stallAt {
			<-req.Context().Done() // stop sending, keep the connection
			return
		}
		end := min(off+chunk, int64(len(data)))
		if beh.stallAt > 0 && off < beh.stallAt {
			end = min(end, beh.stallAt)
		}
		if _, err := w.Write(data[off:end]); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		sent += end - off
		r.sent.Add(end - off)
		off = end
		if beh.rate > 0 {
			if ahead := time.Duration(float64(sent)/float64(beh.rate)*float64(time.Second)) - time.Since(t0); ahead > 0 {
				select {
				case <-time.After(ahead):
				case <-req.Context().Done():
					return
				}
			}
		}
	}
}

// removeFakeImage removes what the pulls left under the fake registries'
// names.
func removeFakeImage(d *Docker, img *fakeImage, regs ...*itRegistry) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, r := range regs {
		_, _ = d.c().ImageRemove(ctx, r.host+"/"+img.repo+"@"+img.index, client.ImageRemoveOptions{Force: true})
	}
	_, _ = d.c().ImageRemove(ctx, "docker.io/"+img.repo+"@"+img.index, client.ImageRemoveOptions{Force: true})
}

// shortPullRules shortens the stall and slow rules so a mid-layer switch
// happens within a test.
func shortPullRules(t *testing.T, stall time.Duration) {
	t.Helper()
	g, w, c, sw, fl, st := slowGrace, slowWindow, slowCheckEvery, slowSwitchKBps, slowFloorKBps, pullStall
	slowGrace, slowWindow, slowCheckEvery, slowSwitchKBps, slowFloorKBps, pullStall = 4*time.Second, 5*time.Second, time.Second, 100, 16, stall
	t.Cleanup(func() {
		slowGrace, slowWindow, slowCheckEvery, slowSwitchKBps, slowFloorKBps, pullStall = g, w, c, sw, fl, st
	})
}

// TestPullSwitchMidLayerIT: the first source stops sending in the middle of
// the big layer; the pull gives it up and the next source, which is slow but
// moving, must be waited for until the image is there. After the switch
// Docker may send no progress at all for the layer that was half done (the
// diagnostic below shows when), so this is the case of the field report
// where a moving pull was taken for "45 seconds without progress".
func TestPullSwitchMidLayerIT(t *testing.T) {
	d := newDockerIT(t)
	shortPullRules(t, 8*time.Second)
	img := newFakeImage(t, fmt.Sprintf("afxit/switch%d", time.Now().UnixNano()), 24, true)
	a := startITRegistry(t, img, regBehavior{rate: 4 << 20, stallAt: img.bigSize * 2 / 5})
	b := startITRegistry(t, img, regBehavior{rate: 600 << 10})
	t.Cleanup(func() { removeFakeImage(d, img, a, b) })

	d.SetRegistry(Registry{China: true, Mirrors: []string{a.host, b.host}})
	// No probe: keep the table's order (the fake registries speak plain HTTP).
	d.probeHook = func(context.Context, []ImageSource, string, string) map[string]ProbeResult {
		return map[string]ProbeResult{}
	}
	var logBuf syncBuf
	old := pullLog
	pullLog = &logBuf
	t.Cleanup(func() { pullLog = old })

	ref := "docker.io/" + img.repo + "@" + img.index
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	t0 := time.Now()
	local, used, err := d.pullFallback(ctx, ref, nil)
	t.Logf("runner log:\n%s", logBuf.String())
	if err != nil {
		t.Fatalf("pull after the switch failed after %v: %v", time.Since(t0).Round(time.Second), err)
	}
	if used.Host != b.host {
		t.Fatalf("pulled from %s, want the second source %s", used.Host, b.host)
	}
	if a.sent.Load() == 0 || b.sent.Load() == 0 {
		t.Fatalf("big layer bytes sent: first %d, second %d", a.sent.Load(), b.sent.Load())
	}
	if _, err := d.c().ImageInspect(ctx, local); err != nil {
		t.Fatalf("image %s not there: %v", local, err)
	}
	t.Logf("pulled in %v from %s (blind %v); big layer bytes: first source %d, second %d of %d",
		time.Since(t0).Round(time.Second), used.Host, used.Blind, a.sent.Load(), b.sent.Load(), img.bigSize)
}

// TestPullStallStillDetectedIT: a source that stops sending is still given
// up within a few stall windows when the machine is otherwise quiet.
func TestPullStallStillDetectedIT(t *testing.T) {
	d := newDockerIT(t)
	shortPullRules(t, 8*time.Second)
	img := newFakeImage(t, fmt.Sprintf("afxit/stall%d", time.Now().UnixNano()), 16, true)
	a := startITRegistry(t, img, regBehavior{rate: 4 << 20, stallAt: img.bigSize / 3})
	t.Cleanup(func() { removeFakeImage(d, img, a) })
	d.SetRegistry(Registry{China: true, Mirrors: []string{a.host}})
	d.probeHook = func(context.Context, []ImageSource, string, string) map[string]ProbeResult {
		return map[string]ProbeResult{}
	}
	old := pullLog
	pullLog = nil
	t.Cleanup(func() { pullLog = old })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	t0 := time.Now()
	st, err := d.pullOne(ctx, a.host+"/"+img.repo+"@"+img.index, nil, pullWatch{})
	el := time.Since(t0)
	if err == nil {
		t.Fatalf("a source that stopped sending was not given up (%+v)", st)
	}
	if el > 60*time.Second {
		t.Fatalf("stall noticed only after %v: %v", el.Round(time.Second), err)
	}
	t.Logf("given up after %v: %v", el.Round(time.Second), err)
}

// TestPullMessagesAfterSwitchDiag (ACORNFOX_PULL_DIAG=1 as well) only logs
// which progress messages Docker sends when a pull from a second registry
// continues a layer the first one left half done, with and without an
// attestation manifest in the index. It asserts nothing about Docker.
func TestPullMessagesAfterSwitchDiag(t *testing.T) {
	d := newDockerIT(t)
	if os.Getenv("ACORNFOX_PULL_DIAG") != "1" {
		t.Skip("set ACORNFOX_PULL_DIAG=1 to log Docker's pull messages")
	}
	for _, attest := range []bool{true, false} {
		t.Run(fmt.Sprintf("attestation=%v", attest), func(t *testing.T) {
			img := newFakeImage(t, fmt.Sprintf("afxit/diag%d", time.Now().UnixNano()), 16, attest)
			a := startITRegistry(t, img, regBehavior{rate: 4 << 20, stallAt: img.bigSize / 2})
			b := startITRegistry(t, img, regBehavior{rate: 2 << 20})
			t.Cleanup(func() { removeFakeImage(d, img, a, b) })
			dump := func(ref string, stopAfter time.Duration) {
				ctx, cancel := context.WithTimeout(context.Background(), stopAfter)
				defer cancel()
				resp, err := d.c().ImagePull(ctx, ref, client.ImagePullOptions{})
				if err != nil {
					t.Logf("%s: %v", ref, err)
					return
				}
				t0 := time.Now()
				counts := map[string]int{}
				var last string
				for msg, err := range resp.JSONMessages(ctx) {
					if err != nil {
						t.Logf("  %5.1fs stream ends: %v", time.Since(t0).Seconds(), err)
						break
					}
					key := msg.Status
					if msg.ID != "" && layerStatus(msg.Status) {
						key = "layer " + msg.Status
					}
					counts[key]++
					if line := fmt.Sprintf("%s %s", msg.ID, msg.Status); line != last {
						t.Logf("  %5.1fs %s", time.Since(t0).Seconds(), line)
						last = line
					}
				}
				t.Logf("  %s: %.1fs, message counts %v", ref, time.Since(t0).Seconds(), counts)
			}
			t.Logf("first registry, stops at half the big layer; given up after 6 s")
			dump(a.host+"/"+img.repo+"@"+img.index, 6*time.Second)
			time.Sleep(time.Second)
			t.Logf("second registry, 2 MB/s")
			dump(b.host+"/"+img.repo+"@"+img.index, 60*time.Second)
			t.Logf("big layer bytes: first %d, second %d of %d", a.sent.Load(), b.sent.Load(), img.bigSize)
		})
	}
}

// syncBuf is a bytes.Buffer safe for the pull's log lines.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
