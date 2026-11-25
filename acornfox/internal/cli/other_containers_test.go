package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/runner"
)

// othersFake is the fake API with the listing of containers AcornFox does
// not manage.
type othersFake struct {
	*fakeAPI
	view  client.OtherContainersView
	err   error
	calls int
}

func (o *othersFake) OtherContainers(context.Context) (client.OtherContainersView, error) {
	o.calls++
	if o.err != nil {
		return client.OtherContainersView{}, o.err
	}
	v := o.view
	v.Raw, _ = json.Marshal(v)
	return v, nil
}

func sampleOthersView() client.OtherContainersView {
	return client.OtherContainersView{
		ObservedAt: "2026-11-25T10:00:00Z", Running: 1, Stopped: 1, MemoryMB: 12.3, CPUPercent: 0.4,
		Containers: []runner.OtherContainer{
			{ID: "a1", Name: "redis", Image: "redis:7", State: "running", Running: true, Ports: []runner.OtherPort{{Host: 16379, Container: 6379, Proto: "tcp"}}, StatsAvailable: true, CPUAvailable: true, CPUPercent: 0.4, MemoryUsageMB: 12.3},
			{ID: "b2", Name: "old-db", Image: "mysql:5.7", State: "exited", Ports: []runner.OtherPort{}},
		},
	}
}

func newOthersHarness(t *testing.T) (*harness, *othersFake) {
	t.Helper()
	h := newHarness(t)
	of := &othersFake{fakeAPI: h.api, view: sampleOthersView()}
	h.connect = func(ctx context.Context, tgt client.Target) (client.API, error) { return of, nil }
	return h, of
}

func TestOtherContainersCommand(t *testing.T) {
	h, of := newOthersHarness(t)
	h.addTarget("n7", "http://127.0.0.1:18800")
	code, out, errOut := h.run("--json", "other-containers")
	if code != exitOK {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	m := decodeJSON(t, out)
	cs, _ := m["containers"].([]any)
	if m["ok"] != true || len(cs) != 2 || m["running"] != float64(1) || m["memory_mb"] != 12.3 {
		t.Fatalf("%s", out)
	}
	if of.calls != 1 {
		t.Fatalf("只读一次：%d", of.calls)
	}
	code, out, _ = h.run("other-containers")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, out)
	}
	for _, want := range []string{"2 个", "redis", "在跑", "16379→6379", "内存 12.3 MB", "old-db", "停了", "只能看，不能在这里管", "Windows、Mac"} {
		if !strings.Contains(out, want) {
			t.Errorf("文字里缺“%s”：\n%s", want, out)
		}
	}
}

func TestOtherContainersCommandNoneAndRefusals(t *testing.T) {
	h, of := newOthersHarness(t)
	h.addTarget("n7", "http://127.0.0.1:18800")
	of.view = client.OtherContainersView{Containers: []runner.OtherContainer{}}
	code, out, _ := h.run("other-containers")
	if code != exitOK || !strings.Contains(out, "没有不是从 AcornFox 装的容器") {
		t.Fatalf("没有其他容器也是正常：%d %s", code, out)
	}
	if code, _, _ = h.run("--json", "other-containers", "extra"); code != exitUsage {
		t.Fatalf("多余参数 exit %d", code)
	}
	// There is nothing to start, stop or delete from here.
	if code, _, _ = h.run("--json", "other-containers", "--stop"); code != exitUsage {
		t.Fatalf("没有管理参数 exit %d", code)
	}
	// Docker cannot be read: the server's fault code comes through.
	of.err = &client.Error{Status: 503, Diag: client.Diagnosis{Stage: "server", Code: "MACHINE-04", Message: "读不到这台机器上其他容器：Docker 没有回应，应用不受影响"}}
	code, out, _ = h.run("--json", "other-containers")
	if code != exitServerError || decodeJSON(t, out)["diagnosis"].(map[string]any)["code"] != "MACHINE-04" {
		t.Fatalf("MACHINE-04：%d %s", code, out)
	}
	// An older server: server_too_old.
	h2 := newHarness(t)
	h2.addTarget("n7", "http://127.0.0.1:18800")
	code, out, _ = h2.run("--json", "other-containers")
	if code != exitServerError || decodeJSON(t, out)["diagnosis"].(map[string]any)["code"] != client.CodeServerTooOld {
		t.Fatalf("太旧：%d %s", code, out)
	}
}
