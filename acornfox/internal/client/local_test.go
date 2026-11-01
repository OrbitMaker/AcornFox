package client

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// A local (unix socket) target whose service is not running must say so,
// not "SSH 连接失败".
func TestLocalTargetDownDiagnosis(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "missing.sock")
	c, err := Connect(context.Background(), Target{URL: "unix://" + sock})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Status(context.Background())
	var e *Error
	if !errors.As(err, &e) || e.Diag.Code != codeLocalDown {
		t.Fatalf("err = %v, want code %s", err, codeLocalDown)
	}
}
