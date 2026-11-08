package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/setup"
)

// On a Mac (local mode off in this release) install, serve and doctor stop
// with SETUP-MAC-01 and a stable JSON shape; client commands still run.
func TestMacLocalStop(t *testing.T) {
	if setup.MacLocalEnabled {
		t.Skip("built with the Mac local mode on")
	}
	old := platform.GOOS
	t.Cleanup(func() { platform.GOOS = old })
	platform.GOOS = "darwin"

	for _, args := range [][]string{{"install", "--json"}, {"--json", "install"}, {"serve", "--background", "--json"}} {
		var out, errBuf bytes.Buffer
		code := run(args, strings.NewReader(""), &out, &errBuf)
		var m struct {
			OK        bool `json:"ok"`
			Diagnosis struct {
				Stage, Code, Message, Hint string
			} `json:"diagnosis"`
		}
		if err := json.Unmarshal(out.Bytes(), &m); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
		if code != macLocalExit || m.OK || m.Diagnosis.Code != "SETUP-MAC-01" || m.Diagnosis.Stage != "local" ||
			!strings.Contains(m.Diagnosis.Message, "Mac 版只有命令行客户端") || !strings.Contains(m.Diagnosis.Hint, "acornfox target add") || errBuf.Len() != 0 {
			t.Fatalf("%v: exit %d %+v stderr %q", args, code, m, errBuf.String())
		}
	}

	// Human output goes to stderr with the fault code.
	var out, errBuf bytes.Buffer
	if code := run([]string{"install", "--china"}, strings.NewReader(""), &out, &errBuf); code != macLocalExit ||
		out.Len() != 0 || !strings.Contains(errBuf.String(), "故障编号 SETUP-MAC-01") {
		t.Fatalf("human install: %d %q %q", code, out.String(), errBuf.String())
	}

	// doctor keeps its own report shape.
	out.Reset()
	errBuf.Reset()
	code := run([]string{"doctor", "--json"}, strings.NewReader(""), &out, &errBuf)
	var r doctorReport
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err, out.String())
	}
	if code != 1 || r.OK || len(r.Items) != 1 || r.Items[0].Code != "SETUP-MAC-01" || r.Items[0].Level != "fail" {
		t.Fatalf("doctor: %d %+v", code, r)
	}

	// A client command is not stopped.
	if _, stopped := macLocalStop("deploy", false, &out, &errBuf); stopped {
		t.Fatal("deploy stopped")
	}
	if _, stopped := macLocalStop("uninstall", false, &out, &errBuf); stopped {
		t.Fatal("uninstall stopped")
	}

	// Linux and Windows: nothing is stopped.
	for _, goos := range []string{"linux", "windows"} {
		platform.GOOS = goos
		if _, stopped := macLocalStop("install", true, &out, &errBuf); stopped {
			t.Fatalf("%s: install stopped", goos)
		}
	}
}
