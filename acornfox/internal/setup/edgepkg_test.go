package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/edge"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestEdgePackageFile(t *testing.T) {
	if got := EdgePackageFile("arm64"); got != "acornfox-edge_"+edge.ImageVersion+"_linux_arm64.tar.gz" {
		t.Fatalf("file = %q", got)
	}
}

// The order follows the network, and ACORNFOX_RELEASE_BASE replaces both.
func TestReleaseBases(t *testing.T) {
	cn := releaseBases("v0.6.0", true, env(nil))
	if len(cn) != 2 || !strings.HasPrefix(cn[0], "https://gitee.com/VIP13390/AcornFox/") || !strings.HasPrefix(cn[1], "https://github.com/OrbitMaker/AcornFox/") {
		t.Fatalf("china = %v", cn)
	}
	gl := releaseBases("v0.6.0", false, env(map[string]string{"ACORNFOX_REPO": "o/r"}))
	if !strings.HasPrefix(gl[0], "https://github.com/o/r/releases/download/v0.6.0") || !strings.Contains(gl[1], "gitee.com") {
		t.Fatalf("global = %v", gl)
	}
	own := releaseBases("v0.6.0", true, env(map[string]string{"ACORNFOX_RELEASE_BASE": "http://192.0.2.1:8000/"}))
	if len(own) != 1 || own[0] != "http://192.0.2.1:8000/releases/download/v0.6.0" {
		t.Fatalf("own = %v", own)
	}
	if releaseTag("0.6.0") != "v0.6.0" || releaseTag("v0.6.0") != "v0.6.0" {
		t.Fatal("tag")
	}
}

// The package is fetched from the release and checked against its
// SHA256SUMS; a release whose file does not match is refused.
func TestFetchEdgePackage(t *testing.T) {
	file := EdgePackageFile("amd64")
	body := []byte("entry package")
	sum := sha256.Sum256(body)
	sums := hex.EncodeToString(sum[:]) + "  " + file + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/download/v0.6.0/SHA256SUMS":
			_, _ = w.Write([]byte(sums))
		case "/releases/download/v0.6.0/" + file:
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	d := &Downloader{Dir: t.TempDir()}
	e := env(map[string]string{"ACORNFOX_RELEASE_BASE": srv.URL})
	path, err := d.FetchEdgePackage(context.Background(), "0.6.0", "amd64", false, e, func(Item) {})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(body) {
		t.Fatalf("got %q", b)
	}
	// A release without the file (an older version, a dev build).
	_, err = d.FetchEdgePackage(context.Background(), "0.5.1", "amd64", false, e, func(Item) {})
	var f *Fault
	if !errors.As(err, &f) || f.Code != "SETUP-NET-01" {
		t.Fatalf("err = %v", err)
	}
	// A wrong file.
	sums = strings.Repeat("0", 64) + "  " + file + "\n"
	d2 := &Downloader{Dir: t.TempDir()}
	_, err = d2.FetchEdgePackage(context.Background(), "0.6.0", "amd64", false, e, func(Item) {})
	if !errors.As(err, &f) || f.Code != "SETUP-SUM-01" {
		t.Fatalf("err = %v", err)
	}
}
