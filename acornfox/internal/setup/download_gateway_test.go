package setup

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/identity"
)

// fakeIssuer answers Issue from a function and counts calls.
type fakeIssuer struct {
	calls atomic.Int32
	fn    func(n int, fileID string) (gatewayclient.Issued, error)
}

func (f *fakeIssuer) Issue(_ context.Context, fileID, purpose string) (gatewayclient.Issued, error) {
	n := int(f.calls.Add(1))
	if purpose != "runtime" {
		return gatewayclient.Issued{}, errors.New("purpose " + purpose)
	}
	return f.fn(n, fileID)
}

// fileServer serves payload; requests carrying a token in reject get 403.
type fileServer struct {
	srv     *httptest.Server
	payload []byte
	mu      sync.Mutex
	ranges  []string
	reject  map[string]bool
	hits    atomic.Int32
}

func newFileServer(t *testing.T, payload []byte) *fileServer {
	fs := &fileServer{payload: payload, reject: map[string]bool{}}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.hits.Add(1)
		fs.mu.Lock()
		fs.ranges = append(fs.ranges, r.Header.Get("Range"))
		bad := fs.reject[r.URL.Query().Get("token")]
		fs.mu.Unlock()
		if bad {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.ServeContent(w, r, "f", time.Time{}, strings.NewReader(string(fs.payload)))
	}))
	t.Cleanup(fs.srv.Close)
	return fs
}

func issued(url, sha string) gatewayclient.Issued {
	return gatewayclient.Issued{URL: url, SHA256: sha, ExpiresAt: time.Now().Add(time.Hour), Backend: "senluopan"}
}

func gwArtifact(payload []byte, mirror, upstream string) Artifact {
	a := Artifact{ID: "wsl-package", Title: "WSL 安装包", File: "g.bin", SHA256: sum(payload), Size: int64(len(payload)), GatewayID: "wsl-x64-3.0.3"}
	if mirror != "" {
		a.Mirrors = []string{mirror}
	}
	if upstream != "" {
		a.Upstream = []string{upstream}
	}
	return a
}

func names(s Sources, a Artifact) string {
	var out []string
	for _, src := range s.list(a) {
		if src.gateway {
			out = append(out, "gw")
		} else {
			out = append(out, src.url)
		}
	}
	return strings.Join(out, " ")
}

func TestSourcesOrderWithGateway(t *testing.T) {
	a := Artifact{File: "f", GatewayID: "x", Mirrors: []string{"https://m/f"}, Upstream: []string{"https://u/f"}}
	gw := &fakeIssuer{}
	if got := names(Sources{Base: "https://b", China: true, Gateway: gw}, a); got != "https://b/runtime/f gw https://m/f https://u/f" {
		t.Fatal(got)
	}
	if got := names(Sources{Gateway: gw}, a); got != "gw https://u/f https://m/f" {
		t.Fatal(got)
	}
	// No gateway ID: the gateway is not a source for this file.
	b := a
	b.GatewayID = ""
	if got := names(Sources{China: true, Gateway: gw}, b); got != "https://m/f https://u/f" {
		t.Fatal(got)
	}
	// Gateway off: the same list as before the gateway existed.
	for _, china := range []bool{true, false} {
		off := Sources{Base: "https://b", China: china}
		if strings.Join(off.URLs(a), " ") != names(off, a) {
			t.Fatal("gateway off changed the order")
		}
	}
	// URLs never contains the gateway.
	if got := (Sources{China: true, Gateway: gw}).URLs(a); strings.Join(got, " ") != "https://m/f https://u/f" {
		t.Fatal(got)
	}
}

func TestGatewaySourceUsedFirst(t *testing.T) {
	payload := []byte(strings.Repeat("gateway-", 5000))
	fs := newFileServer(t, payload)
	mirror := newFileServer(t, payload)
	gw := &fakeIssuer{fn: func(int, string) (gatewayclient.Issued, error) {
		return issued(fs.srv.URL+"/f?token=ok", sum(payload)), nil
	}}
	d := &Downloader{Dir: t.TempDir(), Sources: Sources{China: true, Gateway: gw}}
	var items []Item
	p, err := d.Fetch(context.Background(), gwArtifact(payload, mirror.srv.URL+"/f", ""), func(it Item) { items = append(items, it) })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); sum(b) != sum(payload) {
		t.Fatal("content")
	}
	if gw.calls.Load() != 1 || mirror.hits.Load() != 0 {
		t.Fatalf("issue calls %d, mirror hits %d", gw.calls.Load(), mirror.hits.Load())
	}
	last := items[len(items)-1]
	if last.State != StateDone || last.Source != GatewaySourceName {
		t.Fatalf("last %+v", last)
	}
	// Already on disk: the gateway is not asked again.
	if _, err := d.Fetch(context.Background(), gwArtifact(payload, mirror.srv.URL+"/f", ""), func(Item) {}); err != nil || gw.calls.Load() != 1 {
		t.Fatalf("second fetch: %v, calls %d", err, gw.calls.Load())
	}
}

func TestGatewayErrorsFallBack(t *testing.T) {
	payload := []byte(strings.Repeat("fallback-", 3000))
	mirror := newFileServer(t, payload)
	for _, code := range []string{"GW-REQ-01", "GW-REG-01", "GW-AUTH-01", "GW-AUTH-03", "GW-LIM-01", "GW-LIM-02", "GW-OFF-01", "GW-ART-01", "GW-BE-01", "GW-NET-01", "GW-RESP-01"} {
		gw := &fakeIssuer{fn: func(int, string) (gatewayclient.Issued, error) {
			return gatewayclient.Issued{}, &gatewayclient.Error{Code: code, Message: "测试"}
		}}
		d := &Downloader{Dir: t.TempDir(), Sources: Sources{China: true, Gateway: gw}}
		var detail string
		_, err := d.Fetch(context.Background(), gwArtifact(payload, mirror.srv.URL+"/f", ""), func(it Item) {
			if strings.HasPrefix(it.Detail, "换下一个下载源") && it.Source == GatewaySourceName {
				detail = it.Detail
			}
		})
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if !strings.Contains(detail, code) {
			t.Fatalf("%s: switch message %q", code, detail)
		}
	}
	// A plain error (gateway unreachable before any code) falls back too.
	gw := &fakeIssuer{fn: func(int, string) (gatewayclient.Issued, error) { return gatewayclient.Issued{}, errors.New("boom") }}
	d := &Downloader{Dir: t.TempDir(), Sources: Sources{China: true, Gateway: gw}}
	if _, err := d.Fetch(context.Background(), gwArtifact(payload, mirror.srv.URL+"/f", ""), func(Item) {}); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayChecksumMismatchSkipped(t *testing.T) {
	payload := []byte(strings.Repeat("sum-", 3000))
	fs := newFileServer(t, payload)
	mirror := newFileServer(t, payload)
	gw := &fakeIssuer{fn: func(int, string) (gatewayclient.Issued, error) {
		return issued(fs.srv.URL+"/f?token=ok", strings.Repeat("0", 64)), nil
	}}
	d := &Downloader{Dir: t.TempDir(), Sources: Sources{China: true, Gateway: gw}}
	if _, err := d.Fetch(context.Background(), gwArtifact(payload, mirror.srv.URL+"/f", ""), func(Item) {}); err != nil {
		t.Fatal(err)
	}
	if fs.hits.Load() != 0 || mirror.hits.Load() == 0 {
		t.Fatalf("gateway file fetched %d times, mirror %d", fs.hits.Load(), mirror.hits.Load())
	}
	// The gateway was the only source, plus a tampered mirror: every
	// source failed its checksum => SETUP-SUM-01.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("tampered")) }))
	defer bad.Close()
	d = &Downloader{Dir: t.TempDir(), Sources: Sources{China: true, Gateway: gw}}
	_, err := d.Fetch(context.Background(), gwArtifact(payload, bad.URL, ""), func(Item) {})
	var f *Fault
	if !errors.As(err, &f) || f.Code != "SETUP-SUM-01" || !strings.Contains(f.Message, "2 个下载源") {
		t.Fatalf("want SETUP-SUM-01 over 2 sources, got %v", err)
	}
}

func TestGatewayExpiredAddressReissued(t *testing.T) {
	payload := []byte(strings.Repeat("resume-", 4000))
	fs := newFileServer(t, payload)
	fs.reject["old"] = true
	gw := &fakeIssuer{fn: func(n int, _ string) (gatewayclient.Issued, error) {
		if n == 1 {
			return issued(fs.srv.URL+"/f?token=old", sum(payload)), nil
		}
		return issued(fs.srv.URL+"/f?token=new", sum(payload)), nil
	}}
	dir := t.TempDir()
	// A partial file from an earlier run is continued with the new address.
	if err := os.WriteFile(filepath.Join(dir, "g.bin.part"), payload[:1000], 0o600); err != nil {
		t.Fatal(err)
	}
	d := &Downloader{Dir: dir, Sources: Sources{China: true, Gateway: gw}}
	var reissueSeen bool
	p, err := d.Fetch(context.Background(), gwArtifact(payload, "", ""), func(it Item) {
		if strings.Contains(it.Detail, "重新要") {
			reissueSeen = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); sum(b) != sum(payload) {
		t.Fatal("content")
	}
	if gw.calls.Load() != 2 || !reissueSeen {
		t.Fatalf("calls %d, reissue reported %v", gw.calls.Load(), reissueSeen)
	}
	fs.mu.Lock()
	last := fs.ranges[len(fs.ranges)-1]
	fs.mu.Unlock()
	if last != "bytes=1000-" {
		t.Fatalf("did not resume: %v", fs.ranges)
	}

	// Always expired: two re-issues (three addresses), then the next source.
	payload2 := []byte(strings.Repeat("again-", 2000))
	fs2 := newFileServer(t, payload2)
	fs2.reject["x"] = true
	mirror := newFileServer(t, payload2)
	gw2 := &fakeIssuer{fn: func(int, string) (gatewayclient.Issued, error) {
		return issued(fs2.srv.URL+"/f?token=x", sum(payload2)), nil
	}}
	d = &Downloader{Dir: t.TempDir(), Sources: Sources{China: true, Gateway: gw2}}
	a := gwArtifact(payload2, mirror.srv.URL+"/f", "")
	if _, err := d.Fetch(context.Background(), a, func(Item) {}); err != nil {
		t.Fatal(err)
	}
	if gw2.calls.Load() != 3 || mirror.hits.Load() != 1 {
		t.Fatalf("calls %d, mirror hits %d", gw2.calls.Load(), mirror.hits.Load())
	}
}

// The signed address and the storage host never reach what the setup shows.
func TestGatewayAddressNotShown(t *testing.T) {
	payload := []byte(strings.Repeat("hide-", 2000))
	mirror := newFileServer(t, payload)
	gw := &fakeIssuer{fn: func(int, string) (gatewayclient.Issued, error) {
		return issued("https://storage.netdisk.example/f?sig=SECRET123", sum(payload)), nil
	}}
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if strings.Contains(addr, "netdisk.example") {
			return nil, errors.New("dial " + addr + ": refused")
		}
		var dl net.Dialer
		return dl.DialContext(ctx, network, addr)
	}}}
	d := &Downloader{Dir: t.TempDir(), Client: client, Sources: Sources{China: true, Gateway: gw}}
	var seen []string
	if _, err := d.Fetch(context.Background(), gwArtifact(payload, mirror.srv.URL+"/f", ""), func(it Item) {
		b, _ := json.Marshal(it)
		seen = append(seen, string(b))
	}); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(seen, "\n")
	if strings.Contains(all, "SECRET123") || strings.Contains(all, "netdisk.example") {
		t.Fatalf("leaked:\n%s", all)
	}
	if !strings.Contains(all, GatewaySourceName) {
		t.Fatal("gateway source name not shown")
	}
}

func TestAllSourcesFailCountsGateway(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	defer down.Close()
	gw := &fakeIssuer{fn: func(int, string) (gatewayclient.Issued, error) {
		return gatewayclient.Issued{}, &gatewayclient.Error{Code: "GW-OFF-01", Message: "停发"}
	}}
	d := &Downloader{Dir: t.TempDir(), Sources: Sources{China: true, Gateway: gw}}
	payload := []byte("x")
	_, err := d.Fetch(context.Background(), gwArtifact(payload, down.URL, down.URL), func(Item) {})
	var f *Fault
	if !errors.As(err, &f) || f.Code != "SETUP-NET-01" || !strings.Contains(f.Message, "3 个下载源") {
		t.Fatalf("got %v", err)
	}
}

// Gateway off (nil): a file with a gateway ID downloads exactly as before.
func TestGatewayOffUnchanged(t *testing.T) {
	payload := []byte(strings.Repeat("off-", 3000))
	mirror := newFileServer(t, payload)
	d := &Downloader{Dir: t.TempDir(), Sources: Sources{China: true}}
	var sources []string
	if _, err := d.Fetch(context.Background(), gwArtifact(payload, mirror.srv.URL+"/f", ""), func(it Item) { sources = append(sources, it.Source) }); err != nil {
		t.Fatal(err)
	}
	for _, s := range sources {
		if s == GatewaySourceName {
			t.Fatal("gateway shown while off")
		}
	}
}

// End to end through the real client: the first issue is refused as not
// registered, the client registers and retries, the key is created only
// when the gateway source is reached.
func TestLazyGatewayRegistersAndDownloads(t *testing.T) {
	payload := []byte(strings.Repeat("e2e-", 3000))
	fs := newFileServer(t, payload)
	var registered atomic.Bool
	var issues atomic.Int32
	gwSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-AFX-Signature") == "" || !strings.HasPrefix(r.Header.Get("X-AFX-Install"), "afx-") {
			w.WriteHeader(400)
			w.Write([]byte(`{"ok":false,"code":"GW-REQ-01","message":"缺请求头"}`))
			return
		}
		switch r.URL.Path {
		case "/v1/register":
			registered.Store(true)
			w.Write([]byte(`{"ok":true,"install_id":"` + r.Header.Get("X-AFX-Install") + `","limits":{"per_day":20}}`))
		case "/v1/issue":
			issues.Add(1)
			if !registered.Load() {
				w.WriteHeader(401)
				w.Write([]byte(`{"ok":false,"code":"GW-AUTH-02","message":"还没登记"}`))
				return
			}
			w.Write([]byte(`{"ok":true,"file_id":"wsl-x64-3.0.3","url":"` + fs.srv.URL + `/f?token=ok","expires_at":"2026-11-05T02:24:27+08:00","sha256":"` + sum(payload) + `","size":1,"backend":"senluopan"}`))
		}
	}))
	defer gwSrv.Close()
	dataDir := t.TempDir()
	d := &Downloader{Dir: filepath.Join(dataDir, "runtime", "downloads"),
		Sources: Sources{China: true, Gateway: newLazyGateway(gwSrv.URL, dataDir, "")}}
	if _, err := d.Fetch(context.Background(), gwArtifact(payload, "", ""), func(Item) {}); err != nil {
		t.Fatal(err)
	}
	if !registered.Load() || issues.Load() != 2 {
		t.Fatalf("registered %v issues %d", registered.Load(), issues.Load())
	}
	if _, err := identity.Load(dataDir); err != nil {
		t.Fatal("key not created:", err)
	}

	// A file already on disk never reaches the gateway: no key is made.
	other := t.TempDir()
	dl := &Downloader{Dir: d.Dir, Sources: Sources{China: true, Gateway: newLazyGateway(gwSrv.URL, other, "")}}
	if _, err := dl.Fetch(context.Background(), gwArtifact(payload, "", ""), func(Item) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Load(other); !errors.Is(err, identity.ErrNoKey) {
		t.Fatalf("key created without need: %v", err)
	}
}

func TestManifestGatewayIDs(t *testing.T) {
	if id := wslPackage().GatewayID; id != "wsl-x64-3.0.3" {
		t.Fatal(id)
	}
	for _, arch := range []string{"arm64", "amd64"} {
		for _, a := range []Artifact{colimaBinary(arch), limaTarball(arch), colimaImage(arch)} {
			if !strings.Contains(a.GatewayID, arch) || !strings.Contains(a.GatewayID, "-v") {
				t.Fatalf("%s: %q", a.ID, a.GatewayID)
			}
		}
	}
	if colimaBinary("arm64").GatewayID != "colima-darwin-arm64-v0.10.3" {
		t.Fatal(colimaBinary("arm64").GatewayID)
	}
	if alpineRootfs().GatewayID != "" {
		t.Fatal("alpine comes from public mirrors")
	}
}
