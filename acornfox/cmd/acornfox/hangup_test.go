//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func hasSignal(sigs []os.Signal, want os.Signal) bool {
	for _, s := range sigs {
		if s == want {
			return true
		}
	}
	return false
}

// Under nohup SIGHUP is ignored and must stay ignored; otherwise a dropped
// terminal or SSH session cancels the command like Ctrl+C does. The signal
// state is faked: signal.Ignore in this process would leak into the helper
// processes started by the tests below.
func TestCLIStopSignalsRespectNohup(t *testing.T) {
	underNohup := cliStopSignals(func(s os.Signal) bool { return s == syscall.SIGHUP })
	if hasSignal(underNohup, syscall.SIGHUP) {
		t.Fatal("SIGHUP ignored (nohup) must not be registered")
	}
	normal := cliStopSignals(func(os.Signal) bool { return false })
	for _, sigs := range [][]os.Signal{underNohup, normal} {
		if !hasSignal(sigs, os.Interrupt) || !hasSignal(sigs, syscall.SIGTERM) {
			t.Fatalf("interrupt and SIGTERM must always cancel: %v", sigs)
		}
	}
	if !hasSignal(normal, syscall.SIGHUP) {
		t.Fatal("SIGHUP must cancel the wait when it is not ignored")
	}
}

func TestCLIHangupHelper(t *testing.T) {
	if os.Getenv("ACORNFOX_TEST_HANGUP_HELPER") != "1" {
		return
	}
	if os.Getenv("ACORNFOX_TEST_HANGUP_NOHUP") == "1" {
		signal.Ignore(syscall.SIGHUP) // what nohup does before exec
	}
	os.Exit(run([]string{"--json", "--app", "sample", "deploy", "--image", "nginx:1.27-alpine", "--port", "80"}, os.Stdin, os.Stdout, os.Stderr))
}

// startDeployHelper runs `acornfox --json deploy --image …` in a child process
// against a fake server whose deployment never finishes, and returns once the
// child is polling.
func startDeployHelper(t *testing.T, nohup bool) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer, chan error) {
	t.Helper()
	polling := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/apps/sample/deployments":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "dep123", "app": "sample", "seq": 1, "status": "building"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/deployments/dep123":
			_ = json.NewEncoder(w).Encode(map[string]any{"deployment": map[string]any{"id": "dep123", "app": "sample", "seq": 1, "status": "building"}, "events": []any{}})
			once.Do(func() { close(polling) })
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	home := t.TempDir()
	config := filepath.Join(home, "config")
	macConfig := filepath.Join(home, "Library", "Application Support", "acornfox")
	data, err := json.Marshal(map[string]any{"default": "test", "targets": map[string]any{
		"test": map[string]any{"name": "test", "url": server.URL},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(config, "acornfox"), macConfig} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "targets.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIHangupHelper$")
	cmd.Dir = t.TempDir() // keep .acornfox out of the package directory
	cmd.Env = append(os.Environ(), "ACORNFOX_TEST_HANGUP_HELPER=1", "HOME="+home, "XDG_CONFIG_HOME="+config)
	if nohup {
		cmd.Env = append(cmd.Env, "ACORNFOX_TEST_HANGUP_NOHUP=1")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() { done <- cmd.Wait(); close(exited) }()
	t.Cleanup(func() {
		select {
		case <-exited:
		default:
			_ = cmd.Process.Kill()
			<-exited
		}
	})
	select {
	case <-polling:
	case err := <-done:
		t.Fatalf("CLI exited before polling: %v; stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatal("CLI never reached deployment polling")
	}
	return cmd, &stdout, &stderr, done
}

func assertCancelledJSON(t *testing.T, cmd *exec.Cmd, stdout, stderr *bytes.Buffer) {
	t.Helper()
	if code := cmd.ProcessState.ExitCode(); code != 1 {
		t.Fatalf("exit = %d want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var m struct {
		OK        bool              `json:"ok"`
		Diagnosis map[string]string `json:"diagnosis"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &m); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout.String())
	}
	if m.OK || m.Diagnosis["code"] != "cancelled" || !strings.Contains(m.Diagnosis["hint"], "acornfox status dep123") {
		t.Fatalf("diagnosis = %+v", m)
	}
}

// A hangup (terminal closed, SSH dropped) must leave a JSON diagnosis behind
// instead of killing the CLI silently with an empty output file.
func TestCLIHangupStillWritesJSON(t *testing.T) {
	if signal.Ignored(syscall.SIGHUP) {
		t.Skip("go test was started with SIGHUP ignored; the helper would inherit it")
	}
	cmd, stdout, stderr, done := startDeployHelper(t, false)
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("CLI did not exit after SIGHUP")
	}
	assertCancelledJSON(t, cmd, stdout, stderr)
}

// Under nohup the hangup is ignored and the CLI keeps waiting.
func TestCLIHangupIgnoredUnderNohup(t *testing.T) {
	cmd, stdout, stderr, done := startDeployHelper(t, true)
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("CLI exited on SIGHUP under nohup: %v; stdout=%s", err, stdout.String())
	case <-time.After(2500 * time.Millisecond):
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("CLI did not exit after SIGTERM")
	}
	assertCancelledJSON(t, cmd, stdout, stderr)
}
