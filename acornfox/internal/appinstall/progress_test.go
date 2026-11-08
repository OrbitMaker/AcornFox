package appinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/state"
)

// While the image archive comes through the download gateway, the image
// step carries the progress (bytes, total, speed) and a message saying it;
// after the download it says the image is being loaded; once the step is
// over the progress is gone.
func TestGatewayDownloadProgress(t *testing.T) {
	old := progressInterval
	progressInterval = 15 * time.Millisecond
	t.Cleanup(func() { progressInterval = old })

	const chunks, chunk = 12, 4096
	body := []byte(strings.Repeat("x", chunks*chunk))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < chunks; i++ {
			_, _ = w.Write(body[i*chunk : (i+1)*chunk])
			w.(http.Flusher).Flush()
			time.Sleep(25 * time.Millisecond)
		}
	}))
	t.Cleanup(srv.Close)

	v := setup(t)
	sum := sha256.Sum256(body)
	v.e.cfg.Gateway = &fakeGateway{issued: gatewayclient.Issued{FileID: "hermes-agent-v0.23.0-image", URL: srv.URL + "/f", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}}
	a, err := v.e.cfg.Catalog.App(context.Background(), "hermes-agent")
	if err != nil {
		t.Fatal(err)
	}
	m := *a.Manifest
	m.Release.Images = append(m.Release.Images[:0:0], m.Release.Images...)
	m.Release.Images[0].ArtifactSHA256, m.Release.Images[0].ArtifactSize = hex.EncodeToString(sum[:]), int64(len(body))
	app := *a
	app.Manifest = &m
	p := &planned{app: &app, name: "hermes-agent"}
	tr := &taskRun{e: v.e, id: "0123456789ab", view: TaskView{ID: "0123456789ab", Status: state.TaskRunning,
		Apps:  []AppResult{{App: "hermes-agent"}},
		Steps: []StepView{{App: "hermes-agent", Name: "image", Label: "下载镜像", Status: StepRunning}},
	}}
	if _, err := v.w.st.CreateInstallTask(context.Background(), state.InstallTask{ID: tr.id, Kind: "app", Target: "hermes-agent", Status: state.TaskRunning, Data: "{}"}); err != nil {
		t.Fatal(err)
	}

	errc := make(chan error, 1)
	go func() { _, _, err := tr.image(context.Background(), p, v.w); errc <- err }()
	var seen []StepView
	deadline := time.After(10 * time.Second)
	for running := true; running; {
		select {
		case err := <-errc:
			if err != nil {
				t.Fatal(err)
			}
			running = false
		case <-deadline:
			t.Fatal("download did not finish")
		case <-time.After(10 * time.Millisecond):
			if s := tr.snapshot().Steps[0]; s.Progress != nil && s.Progress.Phase == "download" {
				seen = append(seen, s)
			}
		}
	}
	if len(seen) < 2 {
		t.Fatalf("saw %d progress updates", len(seen))
	}
	mid := seen[len(seen)/2]
	if mid.Progress.TotalBytes != int64(len(body)) || mid.Progress.DoneBytes <= 0 || mid.Progress.DoneBytes > int64(len(body)) ||
		!strings.HasPrefix(mid.Message, "从下载网关下载 ") || !strings.Contains(mid.Message, "%") {
		t.Fatalf("progress %+v %q", mid.Progress, mid.Message)
	}
	if seen[len(seen)-1].Progress.DoneBytes < seen[0].Progress.DoneBytes {
		t.Fatal("progress went backwards")
	}
	// after the download: loading
	s := tr.snapshot().Steps[0]
	if s.Progress == nil || s.Progress.Phase != "load" || !strings.Contains(s.Message, "正在导入镜像") {
		t.Fatalf("after download: %+v %q", s.Progress, s.Message)
	}
	// the saved task (what the console and the CLI poll) has it too
	tv, err := v.e.Task(context.Background(), tr.id)
	if err != nil || len(tv.Steps) != 1 || tv.Steps[0].Progress == nil {
		t.Fatalf("saved task %+v %v", tv.Steps, err)
	}
	tr.step(context.Background(), "hermes-agent", "image", StepDone, "从下载网关下载")
	if s := tr.snapshot().Steps[0]; s.Progress != nil {
		t.Fatalf("progress kept after the step: %+v", s.Progress)
	}
}

func TestDownloadText(t *testing.T) {
	cases := []struct {
		p    StepProgress
		want string
	}{
		{StepProgress{DoneBytes: 312 << 20, TotalBytes: 1120227840, BytesPerSec: 25 << 20, ETASeconds: 30}, "从下载网关下载 312 MB / 1.0 GB（29%，25 MB/s，还要约 30 秒）"},
		{StepProgress{DoneBytes: 5 << 20, TotalBytes: 1120227840, BytesPerSec: 1 << 20, ETASeconds: 1063}, "从下载网关下载 5 MB / 1.0 GB（0%，1 MB/s，还要约 17 分 43 秒）"},
		{StepProgress{DoneBytes: 0, TotalBytes: 100 << 20}, "从下载网关下载 0 MB / 100 MB（0%）"},
	}
	for _, c := range cases {
		if got := downloadText(&c.p); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
}
