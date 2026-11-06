package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/hostcheck"
	"github.com/acornfox/acornfox/internal/setup"
)

func TestDoctorDiskLevelsAndGrouping(t *testing.T) {
	data := t.TempDir()
	docker := t.TempDir()
	for _, c := range []struct {
		name     string
		free     map[string]uint64
		sameDev  bool
		wantOK   bool
		wantLvls string
	}{
		{"same partition, plenty", map[string]uint64{data: 40 << 30}, true, true, "ok"},
		{"separate, one low", map[string]uint64{data: 40 << 30, docker: 8 << 30}, false, true, "ok warn"},
		{"separate, one full", map[string]uint64{data: 1 << 30, docker: 40 << 30}, false, false, "fail ok"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := doctorReport{OK: true}
			dev := func(p string) uint64 {
				if c.sameDev || p == data {
					return 1
				}
				return 2
			}
			free := func(p string) (uint64, error) { return c.free[p], nil }
			doctorDisk(&r, data, docker, free, dev)
			var lv []string
			for _, it := range r.Items {
				lv = append(lv, it.Level)
				if it.Level != "ok" && it.Code != "SETUP-DISK-01" {
					t.Fatalf("code %+v", it)
				}
			}
			if strings.Join(lv, " ") != c.wantLvls || r.OK != c.wantOK {
				t.Fatalf("levels %v ok %v: %+v", lv, r.OK, r.Items)
			}
			if c.sameDev && !strings.Contains(r.Items[0].Text, "数据目录 "+data+"、Docker 数据目录 "+docker) {
				t.Fatalf("same partition should be one line: %s", r.Items[0].Text)
			}
		})
	}
	r := doctorReport{OK: true}
	doctorDisk(&r, data, "", func(string) (uint64, error) { return 0, errors.New("boom") }, func(string) uint64 { return 0 })
	if len(r.Items) != 2 || r.Items[0].Level != "warn" || !r.OK || !strings.Contains(r.Items[1].Text, defaultDockerRoot) {
		t.Fatalf("%+v", r.Items)
	}
}

const tcpHdr = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

func tcpRow(local, inode string) string {
	return "   0: " + local + " 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 " + inode + " 1 0000000000000000 100 0 0 10 0\n"
}

func writeFakeProc(t *testing.T, rows string, procs map[string][2]string) string {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "net"), 0o755))
	must(os.WriteFile(filepath.Join(root, "net", "tcp"), []byte(tcpHdr+rows), 0o644))
	for pid, v := range procs { // v = comm, inode
		dir := filepath.Join(root, pid)
		must(os.MkdirAll(filepath.Join(dir, "fd"), 0o755))
		must(os.WriteFile(filepath.Join(dir, "comm"), []byte(v[0]+"\n"), 0o644))
		must(os.WriteFile(filepath.Join(dir, "cgroup"), []byte("0::/system.slice/x.service\n"), 0o644))
		must(os.Symlink("socket:["+v[1]+"]", filepath.Join(dir, "fd", "3")))
	}
	return root
}

func TestDoctorPorts(t *testing.T) {
	root := writeFakeProc(t,
		tcpRow("00000000:0050", "1")+ // 80 nginx
			tcpRow("0100007F:4978", "2")+ // 18808 redis
			tcpRow("00000000:4990", "3")+ // 18832 node
			tcpRow("00000000:4972", "4")+ // 18802 python
			tcpRow("00000000:4991", "5")+ // 18833 acornfox itself
			tcpRow("00000000:4992", "6"), // 18834 unknown owner
		map[string][2]string{"10": {"nginx", "1"}, "11": {"redis-server", "2"}, "12": {"node", "3"}, "13": {"python3", "4"}, "14": {"acornfox", "5"}})
	r := doctorReport{OK: true}
	doctorPorts(&r, root, []hostcheck.Container{}, false, serverPortUse{Known: true, Users: []string{"域名 a.example.com"}})
	codes := map[string]doctorItem{}
	for _, it := range r.Items {
		codes[it.Code] = it
		if _, ok := setup.FaultByCode(it.Code); !ok {
			t.Fatalf("code %q not in the fault catalog", it.Code)
		}
	}
	for code, lvl := range map[string]string{"SETUP-PORT-01": "fail", "SETUP-PORT-02": "fail", "SETUP-PORT-03": "fail", "SETUP-PORT-04": "warn", "SETUP-PORT-05": "warn"} {
		if codes[code].Level != lvl {
			t.Fatalf("%s level %q: %+v", code, codes[code].Level, r.Items)
		}
	}
	if r.OK {
		t.Fatal("ports held by others must fail doctor")
	}
	if !strings.Contains(codes["SETUP-PORT-01"].Text, "80 被 nginx（PID 10）占用") ||
		!strings.Contains(codes["SETUP-PORT-02"].Text, "18808（127.0.0.1）被 redis-server（PID 11）占用") ||
		!strings.Contains(codes["SETUP-PORT-03"].Text, "18832 被 node（PID 12）占用") ||
		strings.Contains(codes["SETUP-PORT-03"].Text, "18833") ||
		!strings.Contains(codes["SETUP-PORT-05"].Text, "18834") ||
		!strings.Contains(codes["SETUP-PORT-05"].Text, "sudo") {
		t.Fatalf("%+v", r.Items)
	}
	// The JSON shape stays {ok, items:[{ok, level, text, code}]}.
	b, _ := json.Marshal(r)
	var v struct {
		OK    bool `json:"ok"`
		Items []struct {
			OK    bool   `json:"ok"`
			Level string `json:"level"`
			Text  string `json:"text"`
			Code  string `json:"code"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &v); err != nil || v.OK || len(v.Items) != len(r.Items) {
		t.Fatalf("%s %v", b, err)
	}

	if !strings.Contains(codes["SETUP-PORT-01"].Text, "域名 a.example.com") {
		t.Fatalf("%+v", codes["SETUP-PORT-01"])
	}

	// 80/443 shared with another web server and nothing of ours needs them
	// (installed with --allow-busy-80-443): a warning, doctor still passes.
	busyWeb := writeFakeProc(t, tcpRow("00000000:0050", "1")+tcpRow("00000000:01BB", "2"),
		map[string][2]string{"10": {"nginx", "1"}, "11": {"nginx", "2"}})
	for _, web := range []serverPortUse{{Known: true}, {}} {
		r = doctorReport{OK: true}
		doctorPorts(&r, busyWeb, []hostcheck.Container{}, true, web)
		if len(r.Items) != 1 || r.Items[0].Level != "warn" || r.Items[0].Code != "SETUP-PORT-01" || !r.OK ||
			!strings.Contains(r.Items[0].Text, "http://服务器IP:端口") {
			t.Fatalf("known=%v: %+v", web.Known, r.Items)
		}
	}
	r = doctorReport{OK: true}
	doctorPorts(&r, busyWeb, []hostcheck.Container{}, true, serverPortUse{Known: true, Users: []string{"公网控制台 panel.example.com"}})
	if len(r.Items) != 1 || r.Items[0].Level != "fail" || r.OK || !strings.Contains(r.Items[0].Text, "公网控制台 panel.example.com") {
		t.Fatalf("%+v", r.Items)
	}

	// Nothing in the way: one ok line.
	clean := writeFakeProc(t, tcpRow("00000000:0016", "1"), map[string][2]string{"1": {"sshd", "1"}})
	r = doctorReport{OK: true}
	doctorPorts(&r, clean, []hostcheck.Container{}, true, serverPortUse{Known: true})
	if len(r.Items) != 1 || r.Items[0].Level != "ok" || !r.OK || !strings.Contains(r.Items[0].Text, "18810–18899") {
		t.Fatalf("%+v", r.Items)
	}
}

func TestDoctorService(t *testing.T) {
	for _, c := range []struct {
		name     string
		runner   string
		err      error
		level    string
		code     string
		failed   bool
		answered bool
		hint     string
	}{
		{"running", "ok", nil, "ok", "", false, true, ""},
		{"runner unavailable", "unavailable", nil, "fail", "SETUP-SVC-01", true, true, "systemctl restart acornfox-runner acornfox-server"},
		{"down", "", errors.New(`dial unix /run/acornfox/api.sock: connect: no such file or directory`), "fail", "SETUP-SVC-01", true, false, "systemctl restart acornfox-runner acornfox-server"},
		{"no permission", "", errors.New(`dial unix /run/acornfox/api.sock: connect: permission denied`), "warn", "SETUP-SVC-01", false, false, "sudo"},
		{"no permission (wrapped)", "", fmt.Errorf("connect: %w", os.ErrPermission), "warn", "SETUP-SVC-01", false, false, "sudo"},
		{"no permission (client diagnosis)", "", &client.Error{Diag: client.Diagnosis{Code: "permission_denied", Message: "无权访问 acornfox 服务"}}, "warn", "SETUP-SVC-01", false, false, "sudo"},
		{"down (client diagnosis)", "", &client.Error{Diag: client.Diagnosis{Code: "local_server_down", Message: "x"}}, "fail", "SETUP-SVC-01", true, false, "systemctl restart"},
	} {
		r := doctorReport{OK: true}
		answered := doctorService(&r, func() (client.Status, error) { return client.Status{Apps: 2, Runner: c.runner}, c.err })
		if len(r.Items) != 1 || r.Items[0].Level != c.level || r.Items[0].Code != c.code || r.OK == c.failed || answered != c.answered ||
			!strings.Contains(r.Items[0].Text, c.hint) {
			t.Fatalf("%s: %+v ok=%v answered=%v", c.name, r.Items, r.OK, answered)
		}
	}
}

func TestDockerViaService(t *testing.T) {
	denied := errors.New("permission denied while trying to connect to the docker API at unix:///var/run/docker.sock")
	refused := errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?")
	healthy := serviceProbe{st: client.Status{Runner: "ok", DockerVersion: "28.5.1", Apps: 1}}
	for _, c := range []struct {
		name    string
		err     error
		svc     serviceProbe
		handled bool
		want    string
	}{
		{"denied, service healthy", denied, healthy, true, "（Docker 28.5.1）"},
		{"denied (wrapped), service healthy, no version", fmt.Errorf("info: %w", os.ErrPermission), serviceProbe{st: client.Status{Runner: "ok"}}, true, "Docker：由后台服务访问 Docker，当前用户不需要 Docker 权限"},
		{"denied, service unreachable", denied, serviceProbe{err: errors.New("dial unix /run/acornfox/api.sock: connect: permission denied")}, false, ""},
		{"denied, runner down", denied, serviceProbe{st: client.Status{Runner: "unavailable"}}, false, ""},
		{"docker down, service healthy", refused, healthy, false, ""},
	} {
		r := doctorReport{OK: true}
		got := dockerViaService(&r, c.err, c.svc)
		if got != c.handled || !r.OK {
			t.Fatalf("%s: handled=%v ok=%v %+v", c.name, got, r.OK, r.Items)
		}
		if !c.handled {
			if len(r.Items) != 0 {
				t.Fatalf("%s: must add nothing so the old error stays: %+v", c.name, r.Items)
			}
			continue
		}
		if len(r.Items) != 1 || r.Items[0].Level != "ok" || r.Items[0].Code != "" ||
			!strings.Contains(r.Items[0].Text, "当前用户不需要 Docker 权限") || !strings.Contains(r.Items[0].Text, c.want) {
			t.Fatalf("%s: %+v", c.name, r.Items)
		}
	}
}

// A user who is not root sees no owners: AcornFox's own ports (the admin
// port, app ports, the LAN console when open) are accepted while the service
// is healthy; other unknown ports warn with the sudo hint. Root is unchanged.
func TestDoctorPortsNotRoot(t *testing.T) {
	rows := tcpRow("0100007F:4978", "1") + // 18808 admin (edge)
		tcpRow("00000000:4974", "2") + // 18804 not checked
		tcpRow("00000000:4972", "3") + // 18802 LAN console
		tcpRow("00000000:4984", "4") // 18820 an app's port
	unknownRow := tcpRow("00000000:4992", "5") // 18834 someone else
	own := map[int]bool{defaultEdgeAdminPort: true, 18802: true, 18820: true}

	// Everything is ours: one ok line naming them.
	r := doctorReport{OK: true}
	doctorPorts(&r, writeFakeProc(t, rows, nil), nil, false, serverPortUse{Known: true, Own: own})
	if len(r.Items) != 1 || r.Items[0].Level != "ok" || !r.OK ||
		!strings.Contains(r.Items[0].Text, "18802、18808、18820 是 AcornFox 自己在用") {
		t.Fatalf("%+v", r.Items)
	}

	// One port nobody of ours uses: only that one warns, with the new wording.
	r = doctorReport{OK: true}
	doctorPorts(&r, writeFakeProc(t, rows+unknownRow, nil), nil, false, serverPortUse{Known: true, Own: own})
	if len(r.Items) != 1 || r.Items[0].Level != "warn" || r.Items[0].Code != "SETUP-PORT-05" || !r.OK ||
		!strings.Contains(r.Items[0].Text, "端口：18834 有程序在监听，不是 root，看不到是谁占用；用 sudo acornfox doctor 可确认") {
		t.Fatalf("%+v", r.Items)
	}

	// Service not healthy (no Own): every unknown port warns.
	r = doctorReport{OK: true}
	doctorPorts(&r, writeFakeProc(t, rows, nil), nil, false, serverPortUse{})
	if len(r.Items) != 1 || r.Items[0].Level != "warn" || !strings.Contains(r.Items[0].Text, "18802、18808、18820 有程序在监听，不是 root") {
		t.Fatalf("%+v", r.Items)
	}

	// LAN console closed: 18802 is not ours.
	r = doctorReport{OK: true}
	doctorPorts(&r, writeFakeProc(t, rows, nil), nil, false, serverPortUse{Known: true, Own: map[int]bool{defaultEdgeAdminPort: true, 18820: true}})
	if len(r.Items) != 1 || r.Items[0].Level != "warn" || !strings.Contains(r.Items[0].Text, "端口：18802 有程序") {
		t.Fatalf("%+v", r.Items)
	}

	// Root: Own is not used and the wording stays as before.
	r = doctorReport{OK: true}
	doctorPorts(&r, writeFakeProc(t, rows, nil), nil, true, serverPortUse{Known: true, Own: own})
	if len(r.Items) != 1 || r.Items[0].Level != "warn" ||
		r.Items[0].Text != "端口：18802、18808、18820 有程序在监听，分不清是不是 AcornFox 自己的（可能是容器里的进程，Docker 没有应答所以分不清）" {
		t.Fatalf("%+v", r.Items)
	}
	r = doctorReport{OK: true}
	doctorPorts(&r, writeFakeProc(t, rows, nil), []hostcheck.Container{}, true, serverPortUse{Known: true, Own: own})
	if len(r.Items) != 1 || !strings.Contains(r.Items[0].Text, "（进程可能刚退出，过一会儿再查）") {
		t.Fatalf("%+v", r.Items)
	}
}

func TestOwnServerPortsOnlyServedApps(t *testing.T) {
	apps := []client.App{
		{Name: "web", Desired: "running", CurrentDeployment: "d1", PublicPort: 18820},
		{Name: "off", Desired: "stopped", CurrentDeployment: "d2", PublicPort: 18834}, // stopped: gateway does not listen
		{Name: "new", Desired: "running", PublicPort: 18835},                          // never went live
		{Name: "noport", Desired: "running", CurrentDeployment: "d3"},
	}
	own := ownServerPorts(apps, 0)
	want := map[int]bool{defaultEdgeAdminPort: true, 18820: true}
	if fmt.Sprint(own) != fmt.Sprint(want) {
		t.Fatalf("own %v want %v", own, want)
	}
	if own := ownServerPorts(nil, 18802); !own[18802] || !own[defaultEdgeAdminPort] || len(own) != 2 {
		t.Fatalf("LAN console open: %v", own)
	}

	// The stopped app's port held by someone else is not counted as ours:
	// not root, it warns instead of passing.
	rows := tcpRow("0100007F:4978", "1") + // 18808 admin (edge)
		tcpRow("00000000:4984", "2") + // 18820 web
		tcpRow("00000000:4992", "3") // 18834 the stopped app's port, held by another program
	r := doctorReport{OK: true}
	doctorPorts(&r, writeFakeProc(t, rows, nil), nil, false, serverPortUse{Known: true, Own: ownServerPorts(apps, 0)})
	if len(r.Items) != 1 || r.Items[0].Level != "warn" || r.Items[0].Code != "SETUP-PORT-05" ||
		!strings.Contains(r.Items[0].Text, "端口：18834 有程序在监听，不是 root") {
		t.Fatalf("%+v", r.Items)
	}
}
