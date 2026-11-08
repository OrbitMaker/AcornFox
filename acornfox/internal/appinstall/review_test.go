package appinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/state"
)

// Tests for leftover volumes, gateway tokens kept across restarts and app
// deletion, the download-gateway image path, recipes that pin the model
// source, and init calls that must not be sent twice.

// A name whose earlier app was deleted with its volumes kept is refused, and
// the kept volume is not touched.
func TestOldVolumesRefused(t *testing.T) {
	v := setup(t)
	v.w.volumes["new-api"] = []string{"af-new-api-1"}
	_, err := v.e.StartInstall(context.Background(), Request{App: "new-api"})
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeOldVolumes || !strings.Contains(f.Message, "af-new-api-1") || f.HTTPStatus() != http.StatusConflict {
		t.Fatalf("want %s, got %v", CodeOldVolumes, err)
	}
	if !v.w.hasVolume("new-api", "af-new-api-1") || len(v.w.removedVols) != 0 {
		t.Fatal("kept volume touched")
	}
	// another name installs fine
	if tv := v.install(t, Request{App: "new-api", Name: "new-api-2"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("other name %+v", tv.Fault)
	}
}

// A failed install removes the volumes it created, by name, and leaves any
// other volume labeled for the app (one that appeared after the request was
// accepted) alone.
func TestCleanupRemovesOnlyCreatedVolumes(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	v.newapi.failSetup = true
	v.w.mu.Lock()
	v.w.withAppHold = make(chan struct{})
	v.w.mu.Unlock()
	tv, err := v.e.StartInstall(ctx, Request{App: "new-api"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !v.w.hasVolume("new-api", "af-new-api-1") {
		if time.Now().After(deadline) {
			t.Fatal("volume not created")
		}
		time.Sleep(5 * time.Millisecond)
	}
	v.w.mu.Lock()
	v.w.volumes["new-api"] = append(v.w.volumes["new-api"], "user-kept-data")
	hold := v.w.withAppHold
	v.w.withAppHold = nil
	v.w.mu.Unlock()
	close(hold)
	tv = v.wait(t, tv.ID)
	if tv.Fault == nil || tv.Fault.Code != CodeInit {
		t.Fatalf("task %+v", tv.Fault)
	}
	if v.w.hasVolume("new-api", "af-new-api-1") || !v.w.hasVolume("new-api", "user-kept-data") {
		t.Fatalf("volumes after cleanup %v (removed %v)", v.w.volumes, v.w.removedVols)
	}
}

// installHermes installs New API and Hermes on the local gateway.
func installHermes(t *testing.T, v *env) TaskView {
	t.Helper()
	if tv := v.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("new-api %+v", tv.Fault)
	}
	v.w.adopted["sha256:adb7890916de63245523492be8f68f9d2095fecbec4e2a6dab7960381f0fc002"] = true
	tv := v.install(t, Request{App: "hermes-agent", Params: map[string]any{"llm": "local"}})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("hermes %+v", tv.Fault)
	}
	return tv
}

// The token issued for Hermes is recorded (id only) in the task and with the
// app, and deleting the app revokes it.
func TestGatewayTokenRecordedAndRevoked(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	tv := installHermes(t, v)
	if tok := tv.Apps[0].GatewayToken; tok == nil || tok.Provider != "new-api" || tok.ID != 1 {
		t.Fatalf("task token %+v", tv.Apps[0])
	}
	raw, _ := v.w.st.GetInstallTask(ctx, tv.ID)
	if strings.Contains(raw.Data, "sk-K1") {
		t.Fatal("token value in the task record")
	}
	toks, err := v.e.GatewayTokens(ctx, "hermes-agent")
	if err != nil || len(toks) != 1 || toks[0] != (TokenRef{Provider: "new-api", ID: 1}) {
		t.Fatalf("saved tokens %+v %v", toks, err)
	}
	if none, err := v.e.GatewayTokens(ctx, "new-api"); err != nil || len(none) != 0 {
		t.Fatalf("new-api has no tokens: %v %v", none, err)
	}
	if err := v.e.RevokeGatewayTokens(ctx, "hermes-agent", toks); err != nil {
		t.Fatal(err)
	}
	if len(v.newapi.deleted) != 1 || v.newapi.deleted[0] != 1 || len(v.newapi.tokens) != 0 {
		t.Fatalf("not revoked: %v %v", v.newapi.deleted, v.newapi.tokens)
	}
	// the gateway gone: the failure names the token to remove by hand
	err = (&Engine{cfg: v.e.cfg, box: v.e.box, adapter: v.e.adapter}).RevokeGatewayTokens(ctx, "hermes-agent", []TokenRef{{Provider: "nope", ID: 7}})
	if err == nil || !strings.Contains(err.Error(), "编号 7") {
		t.Fatalf("missing gateway: %v", err)
	}
}

// After a restart, recover removes what the interrupted task recorded: its
// gateway token and its volumes.
func TestRecoverUsesRecordedResources(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	if tv := v.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	v.newapi.mu.Lock()
	if v.newapi.tokens == nil {
		v.newapi.tokens = map[int]string{}
	}
	v.newapi.tokens[5] = "acornfox-hermes-agent"
	v.newapi.mu.Unlock()
	if _, _, err := v.w.st.EnsureApp(ctx, "hermes-agent"); err != nil {
		t.Fatal(err)
	}
	v.w.volumes["hermes-agent"] = []string{"af-hermes-agent-1", "older"}
	data, _ := json.Marshal(TaskView{Apps: []AppResult{{App: "hermes-agent", Created: true, Volumes: []string{"af-hermes-agent-1"},
		ImageTag: "acornfox/hermes-agent:0123456789ab", GatewayToken: &TokenRef{Provider: "new-api", ID: 5}}}})
	if _, err := v.w.st.CreateInstallTask(ctx, state.InstallTask{ID: "0123456789cd", Kind: "app", Target: "hermes-agent", Status: state.TaskRunning, Data: string(data)}); err != nil {
		t.Fatal(err)
	}
	v.e.recover(ctx)
	if len(v.newapi.deleted) != 1 || v.newapi.deleted[0] != 5 {
		t.Fatalf("token not removed: %v", v.newapi.deleted)
	}
	if v.w.hasVolume("hermes-agent", "af-hermes-agent-1") || !v.w.hasVolume("hermes-agent", "older") {
		t.Fatalf("volumes %v", v.w.volumes)
	}
	if len(v.w.removed) == 0 || v.w.removed[len(v.w.removed)-1] != "acornfox/hermes-agent:0123456789ab" {
		t.Fatalf("image tag not removed: %v", v.w.removed)
	}
}

// A recipe that links the model to its gateway refuses an explicit own key
// instead of dropping it.
func TestRecipeRefusesOtherLLMSource(t *testing.T) {
	v := setup(t)
	_, err := v.e.StartInstall(context.Background(), Request{Recipe: "hermes-with-new-api", Params: map[string]any{
		"hermes-agent.llm": map[string]any{"source": "own", "base_url": "https://x/v1", "api_key": "k", "model": "m"}}})
	if code(err) != CodeBadParams || !strings.Contains(err.Error(), "只能改 model") {
		t.Fatalf("want %s, got %v", CodeBadParams, err)
	}
}

// An init call that got an answer (here a 502) is not sent again.
func TestInitNotResentAfterAnswer(t *testing.T) {
	v := setup(t)
	v.newapi.setup5xx = true
	tv := v.install(t, Request{App: "new-api"})
	if tv.Fault == nil || tv.Fault.Code != CodeInit {
		t.Fatalf("task %+v", tv.Fault)
	}
	if v.newapi.setups != 1 {
		t.Fatalf("setup sent %d times", v.newapi.setups)
	}
}

func TestNotSent(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	_, _, err = (&Engine{cfg: Config{HTTP: http.DefaultClient}}).httpJSON(context.Background(), http.MethodGet, "http://"+addr+"/", nil)
	if err == nil || !notSent(err) {
		t.Fatalf("refused connection should count as not sent: %v", err)
	}
	if notSent(context.DeadlineExceeded) {
		t.Fatal("timeout counted as not sent")
	}
}

// ---- download gateway ----

type fakeGateway struct {
	mu     sync.Mutex
	calls  int
	issued gatewayclient.Issued
	err    error
}

func (g *fakeGateway) Issue(ctx context.Context, fileID, purpose string) (gatewayclient.Issued, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if purpose != "app" || fileID != "hermes-agent-v0.23.0-image" {
		return gatewayclient.Issued{}, errors.New("wrong request")
	}
	return g.issued, g.err
}

type gwCase struct {
	v      *env
	gw     *fakeGateway
	hits   *int
	served []byte
	pinned []byte // what the catalog's checksum is computed from
}

// gwSetup returns a run of the image step for Hermes whose manifest pins
// the archive (pinned) while the download server serves served.
func gwSetup(t *testing.T, served, pinned []byte, noPin bool) (*gwCase, func() (string, string, error)) {
	t.Helper()
	v := setup(t)
	hits := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		if r.URL.Query().Get("sig") != "secret-signature" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write(served)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(pinned)
	gw := &fakeGateway{issued: gatewayclient.Issued{FileID: "hermes-agent-v0.23.0-image", URL: srv.URL + "/f?sig=secret-signature", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(pinned))}}
	v.e.cfg.Gateway = gw
	a, err := v.e.cfg.Catalog.App(context.Background(), "hermes-agent")
	if err != nil {
		t.Fatal(err)
	}
	m := *a.Manifest
	m.Release.Images = append(m.Release.Images[:0:0], m.Release.Images...)
	if noPin {
		m.Release.Images[0].ArtifactSHA256 = ""
		m.Release.Images[0].ArtifactSize = 0
	} else {
		m.Release.Images[0].ArtifactSHA256 = hex.EncodeToString(sum[:])
		m.Release.Images[0].ArtifactSize = int64(len(pinned))
	}
	app := *a
	app.Manifest = &m
	p := &planned{app: &app, name: "hermes-agent"}
	tr := &taskRun{e: v.e, id: "0123456789ef", view: TaskView{Apps: []AppResult{{App: "hermes-agent"}}}}
	if _, err := v.w.st.CreateInstallTask(context.Background(), state.InstallTask{ID: tr.id, Kind: "app", Target: "hermes-agent", Status: state.TaskRunning, Data: "{}"}); err != nil {
		t.Fatal(err)
	}
	c := &gwCase{v: v, gw: gw, hits: &hits, served: served, pinned: pinned}
	return c, func() (string, string, error) {
		return tr.image(context.Background(), p, v.w)
	}
}

func (c *gwCase) uploadsLeft(t *testing.T) int {
	t.Helper()
	ents, err := os.ReadDir(c.v.e.cfg.UploadDir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "catalog-") {
			n++
		}
	}
	return n
}

func TestGatewayImageLoaded(t *testing.T) {
	body := []byte("image archive bytes")
	c, run := gwSetup(t, body, body, false)
	id, msg, err := run()
	if err != nil || msg != "从下载网关下载" || id != "sha256:"+strings.Repeat("2", 64) {
		t.Fatalf("id=%s msg=%s err=%v", id, msg, err)
	}
	if len(c.v.w.loads) != 1 || c.v.w.loads[0].DeploymentID == "" || c.v.w.loads[0].App != "hermes-agent" {
		t.Fatalf("loads %+v", c.v.w.loads)
	}
	if len(c.v.w.pulled) != 0 {
		t.Fatalf("pulled %v", c.v.w.pulled)
	}
	if c.uploadsLeft(t) != 0 {
		t.Fatal("downloaded archive left in the upload directory")
	}
}

func TestGatewayImageChecksumMismatchNotLoaded(t *testing.T) {
	c, run := gwSetup(t, []byte("tampered archive!!!"), []byte("image archive bytes"), false)
	_, msg, err := run()
	if err != nil || !strings.HasPrefix(msg, "从公共源拉取（下载网关") {
		t.Fatalf("should fall back to public and say why: %s %v", msg, err)
	}
	if len(c.v.w.loads) != 0 || len(c.v.w.pulled) != 1 || c.uploadsLeft(t) != 0 {
		t.Fatalf("loads=%d pulled=%v left=%d", len(c.v.w.loads), c.v.w.pulled, c.uploadsLeft(t))
	}
}

func TestGatewayReportedChecksumDiffersNotDownloaded(t *testing.T) {
	body := []byte("image archive bytes")
	c, run := gwSetup(t, body, body, false)
	c.gw.issued.SHA256 = strings.Repeat("0", 64)
	if _, msg, err := run(); err != nil || !strings.HasPrefix(msg, "从公共源拉取（下载网关") {
		t.Fatalf("%s %v", msg, err)
	}
	if *c.hits != 0 || len(c.v.w.loads) != 0 {
		t.Fatalf("downloaded %d, loads %d", *c.hits, len(c.v.w.loads))
	}
}

func TestGatewayNotUsedWithoutPinnedChecksum(t *testing.T) {
	body := []byte("image archive bytes")
	c, run := gwSetup(t, body, body, true)
	if _, msg, err := run(); err != nil || msg != "从公共源拉取（下载网关：清单没有镜像包的校验值，不走网关）" {
		t.Fatalf("%s %v", msg, err)
	}
	if c.gw.calls != 0 || len(c.v.w.loads) != 0 {
		t.Fatalf("gateway calls %d loads %d", c.gw.calls, len(c.v.w.loads))
	}
}

func TestGatewayIssueOrLoadErrorFallsBack(t *testing.T) {
	body := []byte("image archive bytes")
	c, run := gwSetup(t, body, body, false)
	c.gw.err = errors.New("GW-LIM-01")
	if _, msg, err := run(); err != nil || msg != "从公共源拉取（下载网关：GW-LIM-01）" || len(c.v.w.loads) != 0 {
		t.Fatalf("issue error: %s %v", msg, err)
	}
	c.gw.err = nil
	c.v.w.loadErr = errors.New("docker load failed")
	if _, msg, err := run(); err != nil || !strings.HasPrefix(msg, "从公共源拉取（下载网关") || len(c.v.w.loads) != 1 {
		t.Fatalf("load error: %s %v loads %d", msg, err, len(c.v.w.loads))
	}
}

// When every source fails, the fault carries the gateway's reason but never
// the signed download address.
func TestGatewayAddressNotInFault(t *testing.T) {
	body := []byte("image archive bytes")
	c, run := gwSetup(t, body, body, false)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := ln.Addr().String()
	ln.Close()
	c.gw.issued.URL = "http://" + dead + "/f?sig=secret-signature"
	c.v.w.failPull = true
	_, _, err = run()
	f := AsFault(err)
	if f.Code != CodePull || !strings.Contains(f.Message, "下载网关") {
		t.Fatalf("fault %+v", f)
	}
	// the backend's host may show (it is no secret); the signed part may not
	if strings.Contains(f.Error(), "secret-signature") || strings.Contains(f.Error(), "/f?") {
		t.Fatalf("address leaked: %s", f.Error())
	}
}
