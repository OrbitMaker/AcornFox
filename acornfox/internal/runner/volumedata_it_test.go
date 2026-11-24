package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// tarFiles lists the regular files of a tar stream with their content, mode
// and owner.
func tarFiles(t *testing.T, b []byte) map[string]string {
	t.Helper()
	out := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(b))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		body, _ := io.ReadAll(tr)
		out[strings.TrimPrefix(h.Name, "./")] = string(body) + "|" + modeOwner(h)
	}
}

func modeOwner(h *tar.Header) string {
	return strings.Join([]string{itoa(int(h.Mode & 0o777)), itoa(h.Uid), itoa(h.Gid)}, ":")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// TestDockerVolumeExportImport copies a volume out, removes it, makes it again
// and copies the files back: contents, modes and owners must survive, nothing
// of a previous content may be left, and the helper container must go.
func TestDockerVolumeExportImport(t *testing.T) {
	d := newDockerIT(t)
	ctx := context.Background()
	const app, dep = "afitexp", "0123456789ab"
	tag := ImageTag(app, dep)
	vol := VolumePrefix(app) + "1"

	ctxPath := writeGzipTar(t, t.TempDir(), map[string]string{"Dockerfile": "FROM " + itBaseImage + "\nCMD [\"sleep\",\"1\"]\n"})
	if resp, err := d.Build(ctx, BuildRequest{App: app, DeploymentID: dep, ContextPath: ctxPath}); err != nil || !resp.OK {
		t.Fatalf("build: %v %+v", err, resp)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = d.RemoveVolume(cctx, app, vol)
		_ = d.RemoveImage(cctx, app, tag)
	})
	_ = d.RemoveVolume(ctx, app, vol)
	if err := d.EnsureVolume(ctx, app, vol); err != nil {
		t.Fatal(err)
	}
	if _, err := d.WriteVolumeFiles(ctx, WriteFilesRequest{App: app, Volume: vol, MountPath: "/data", Image: tag, Files: []VolumeFile{
		{Path: "a.txt", Content: []byte("alpha"), Mode: 0o600, UID: 1000, GID: 1000, Overwrite: true},
		{Path: "sub/dir/b.txt", Content: []byte("beta"), Mode: 0o644, UID: 70, GID: 70, Overwrite: true},
	}}); err != nil {
		t.Fatal(err)
	}

	req := VolumeDataRequest{App: app, Volume: vol, MountPath: "/data", Image: tag}
	var first bytes.Buffer
	if err := d.ExportVolume(ctx, req, &first); err != nil {
		t.Fatalf("export: %v", err)
	}
	want := tarFiles(t, first.Bytes())
	if want["a.txt"] != "alpha|384:1000:1000" || want["sub/dir/b.txt"] != "beta|420:70:70" {
		t.Fatalf("exported files: %v", want)
	}

	sizes, err := d.VolumeSizes(ctx, app)
	if err != nil || len(sizes) != 1 || sizes[0].Name != vol || sizes[0].Bytes < 0 {
		t.Fatalf("sizes: %v %v", err, sizes)
	}

	// A new volume holding something else is not what it was.
	if err := d.RemoveVolume(ctx, app, vol); err != nil {
		t.Fatal(err)
	}
	if err := d.EnsureVolume(ctx, app, vol); err != nil {
		t.Fatal(err)
	}
	if _, err := d.WriteVolumeFiles(ctx, WriteFilesRequest{App: app, Volume: vol, MountPath: "/data", Image: tag, Files: []VolumeFile{{Path: "stale.txt", Content: []byte("x"), Mode: 0o644, Overwrite: true}}}); err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveVolume(ctx, app, vol); err != nil {
		t.Fatal(err)
	}
	if err := d.EnsureVolume(ctx, app, vol); err != nil {
		t.Fatal(err)
	}
	if err := d.ImportVolume(ctx, req, bytes.NewReader(first.Bytes())); err != nil {
		t.Fatalf("import: %v", err)
	}
	var second bytes.Buffer
	if err := d.ExportVolume(ctx, req, &second); err != nil {
		t.Fatal(err)
	}
	got := tarFiles(t, second.Bytes())
	if len(got) != len(want) || got["a.txt"] != want["a.txt"] || got["sub/dir/b.txt"] != want["sub/dir/b.txt"] {
		t.Fatalf("after import: %v want %v", got, want)
	}

	// An image of another app is refused, and no helper container stays.
	other := req
	other.Image = "alpine:3.22"
	var re *RemoteError
	if err := d.ExportVolume(ctx, other, io.Discard); !errors.As(err, &re) || re.Code != "refused" {
		t.Fatalf("foreign image: %v", err)
	}
	cs, err := d.ListContainers(ctx, app)
	if err != nil || len(cs) != 0 {
		t.Fatalf("containers left: %v %v", err, cs)
	}
}
