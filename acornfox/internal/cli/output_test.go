package cli

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// Without --json a refused command still names its fault code, so a user
// who passes the output on (often to an AI, whose Skill runs `acornfox
// faults 编号`) passes the code on too. v0.5.0 printed only the message and
// the hint.
func TestHumanFailureNamesTheFaultCode(t *testing.T) {
	h, f := varsHarness(t)
	f.installErr = &client.Error{Status: http.StatusConflict, Diag: client.Diagnosis{Stage: "server", Code: "CAT-INST-24",
		Message: "hermes-agent 删除时留下了数据", Hint: "要接上旧数据就加 --reuse-data"}}
	code, _, errOut := h.run("install", "hermes-agent", "--no-wait")
	if code != exitServerError {
		t.Fatalf("exit %d", code)
	}
	want := "故障编号：CAT-INST-24（运行 acornfox faults CAT-INST-24 查处理办法）"
	if !strings.Contains(errOut, want) {
		t.Fatalf("stderr lacks %q:\n%s", want, errOut)
	}
	fail, num, hint := strings.Index(errOut, "失败："), strings.Index(errOut, want), strings.Index(errOut, "建议：")
	if fail < 0 || hint < 0 || !(fail < num && num < hint) {
		t.Fatalf("order must be 失败, 故障编号, 建议:\n%s", errOut)
	}
	// --json keeps its shape; the code is in diagnosis.code as before.
	_, out, _ := h.run("--json", "install", "hermes-agent", "--no-wait")
	if d, _ := decodeJSON(t, out)["diagnosis"].(map[string]any); d["code"] != "CAT-INST-24" || strings.Contains(out, "故障编号") {
		t.Fatalf("json: %s", out)
	}
}

func TestCodeLine(t *testing.T) {
	for _, c := range []struct{ code, want string }{
		{"CAT-INST-24", "故障编号：CAT-INST-24（运行 acornfox faults CAT-INST-24 查处理办法）"},
		{"cat-inst-24", "故障编号：CAT-INST-24（运行 acornfox faults CAT-INST-24 查处理办法）"},
		{"build_failed", "故障编号：build_failed（运行 acornfox faults build_failed 查处理办法）"},
		// The API's generic error codes are not faults: named, no faults hint.
		{"not_found", "错误码：not_found"},
		{"http_502", "错误码：http_502"},
		// The CLI's own fallback is no code at all.
		{"unknown", ""},
		{"", ""},
	} {
		if got := codeLine(c.code); got != c.want {
			t.Errorf("codeLine(%q) = %q, want %q", c.code, got, c.want)
		}
	}
	var errb bytes.Buffer
	o := &outputWriter{stdout: io.Discard, stderr: &errb}
	o.printDiagnosis(client.Diagnosis{Stage: "server", Code: "unknown", Message: "出错了"})
	if strings.Contains(errb.String(), "编号") || strings.Contains(errb.String(), "错误码") {
		t.Fatalf("no code, no line:\n%s", errb.String())
	}
}
