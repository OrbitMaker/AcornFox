package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

type catalogFake struct {
	*fakeAPI
	wrote  []WriteFilesRequest
	loaded []LoadImageRequest
}

func (c *catalogFake) AdoptImage(ctx context.Context, req AdoptImageRequest) (ImageInfo, error) {
	return ImageInfo{ID: req.Digest, App: req.App}, nil
}
func (c *catalogFake) LoadImage(ctx context.Context, req LoadImageRequest) (LoadImageResponse, error) {
	c.loaded = append(c.loaded, req)
	return LoadImageResponse{Loaded: []string{"x:y"}}, nil
}
func (c *catalogFake) WriteVolumeFiles(ctx context.Context, req WriteFilesRequest) (WriteFilesResult, error) {
	c.wrote = append(c.wrote, req)
	res := WriteFilesResult{}
	for _, f := range req.Files {
		res.Written = append(res.Written, f.Path)
	}
	return res, nil
}

func goodWrite() WriteFilesRequest {
	return WriteFilesRequest{App: "hermes", Volume: "af-hermes-1", MountPath: "/opt/data", Image: "acornfox/hermes:0123456789ab",
		Files: []VolumeFile{{Path: "config.yaml", Content: []byte("a: 1\n"), Mode: 0o600, UID: 10000, GID: 10000, Overwrite: true}}}
}

func TestValidateWriteFiles(t *testing.T) {
	if err := ValidateWriteFiles(goodWrite()); err != nil {
		t.Fatalf("good request refused: %v", err)
	}
	bad := []func(*WriteFilesRequest){
		func(r *WriteFilesRequest) { r.Volume = "af-other-1" },
		func(r *WriteFilesRequest) { r.MountPath = "opt/data" },
		func(r *WriteFilesRequest) { r.MountPath = "/" },
		func(r *WriteFilesRequest) { r.Files[0].Path = "../etc/passwd" },
		func(r *WriteFilesRequest) { r.Files[0].Path = "/abs" },
		func(r *WriteFilesRequest) { r.Files[0].Path = "a/../b" },
		func(r *WriteFilesRequest) { r.Files[0].Mode = 0o4755 },
		func(r *WriteFilesRequest) { r.Files[0].UID = -1 },
		func(r *WriteFilesRequest) { r.Files = nil },
		func(r *WriteFilesRequest) { r.Files[0].Content = make([]byte, MaxVolumeFileBytes+1) },
		func(r *WriteFilesRequest) { r.Image = "" },
		// a backup must be a clean relative path of its own, and only with Overwrite
		func(r *WriteFilesRequest) { r.Files[0].Backup = "../config.yaml.bak" },
		func(r *WriteFilesRequest) { r.Files[0].Backup = "config.yaml" },
		func(r *WriteFilesRequest) { r.Files[0].Backup = "x.bak"; r.Files[0].Overwrite = false },
		func(r *WriteFilesRequest) {
			r.Files[0].Backup = "a.bak"
			r.Files = append(r.Files, VolumeFile{Path: "b.yaml", Overwrite: true, Backup: "a.bak"})
		},
		func(r *WriteFilesRequest) {
			r.Files[0].Backup = "b.yaml"
			r.Files = append(r.Files, VolumeFile{Path: "b.yaml", Overwrite: true})
		},
	}
	for i, mut := range bad {
		r := goodWrite()
		r.Files = append([]VolumeFile(nil), r.Files...)
		mut(&r)
		if ValidateWriteFiles(r) == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestValidateWriteFilesBackup(t *testing.T) {
	r := goodWrite()
	r.Files[0].Backup = "config.yaml.bak-20261111-010203"
	if err := ValidateWriteFiles(r); err != nil {
		t.Fatalf("good backup refused: %v", err)
	}
}

// The copy of an existing file keeps its content, mode and owner and goes to
// the backup path; what is not a regular file, or too large, is not copied.
func TestBackupFromTar(t *testing.T) {
	mk := func(h tar.Header, body string) []byte {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		h.Size = int64(len(body))
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
		_ = tw.Close()
		return buf.Bytes()
	}
	f := VolumeFile{Path: "config.yaml", Backup: "config.yaml.bak-1", Overwrite: true, UID: 1, GID: 1}
	b, ok, err := backupFromTar(bytes.NewReader(mk(tar.Header{Typeflag: tar.TypeReg, Name: "config.yaml", Mode: 0o100600, Uid: 10000, Gid: 10000}, "model: x\n# mine\n")), f, 1<<20)
	if err != nil || !ok {
		t.Fatalf("regular file not copied: %v %v", ok, err)
	}
	if b.Path != "config.yaml.bak-1" || string(b.Content) != "model: x\n# mine\n" || b.Mode != 0o600 || b.UID != 10000 || b.GID != 10000 || !b.Overwrite {
		t.Fatalf("copy %+v", b)
	}
	if _, ok, err := backupFromTar(bytes.NewReader(mk(tar.Header{Typeflag: tar.TypeSymlink, Name: "config.yaml", Linkname: "/etc/passwd"}, "")), f, 1<<20); ok || err != nil {
		t.Fatalf("symlink copied: %v %v", ok, err)
	}
	if _, ok, err := backupFromTar(bytes.NewReader(mk(tar.Header{Typeflag: tar.TypeReg, Name: "config.yaml", Mode: 0o644}, "0123456789")), f, 4); ok || err != nil {
		t.Fatalf("too large copied: %v %v", ok, err)
	}
	if _, _, err := backupFromTar(bytes.NewReader(nil), f, 4); err == nil {
		t.Fatal("empty archive accepted")
	}
}

func TestVolumeFilesTarOwnerModeDirs(t *testing.T) {
	b, err := volumeFilesTar([]VolumeFile{{Path: "conf/app.yaml", Content: []byte("x"), Mode: 0o600, UID: 10000, GID: 10001}}, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(b))
	var got []*tar.Header
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, h)
	}
	if len(got) != 2 || got[0].Name != "conf/" || got[0].Typeflag != tar.TypeDir || got[1].Name != "conf/app.yaml" {
		t.Fatalf("entries %+v", got)
	}
	if got[1].Mode != 0o600 || got[1].Uid != 10000 || got[1].Gid != 10001 || got[0].Uid != 10000 {
		t.Fatalf("owner/mode %+v %+v", got[0], got[1])
	}
}

func TestImageMatchesDigest(t *testing.T) {
	d := "sha256:" + string(bytes.Repeat([]byte("a"), 64))
	if !imageMatchesDigest(d, nil, d) {
		t.Fatal("id match")
	}
	if !imageMatchesDigest("sha256:other", []string{"docker.io/x/y@" + d}, d) {
		t.Fatal("repo digest match")
	}
	if imageMatchesDigest("sha256:other", []string{"x/y@sha256:" + string(bytes.Repeat([]byte("b"), 64))}, d) {
		t.Fatal("mismatch accepted")
	}
}

func TestImageArchFits(t *testing.T) {
	for _, c := range []struct {
		have, want string
		ok         bool
	}{
		{"amd64", "amd64", true},
		{"amd64", "arm64", false},
		{"arm64", "amd64", false},
		{"amd64", "", true}, // an older server does not say
		{"", "arm64", true}, // an image that does not say
	} {
		if imageArchFits(c.have, c.want) != c.ok {
			t.Errorf("%q on %q: want %v", c.have, c.want, c.ok)
		}
	}
}

func TestValidateAdoptArch(t *testing.T) {
	d := "sha256:" + string(bytes.Repeat([]byte("a"), 64))
	good := AdoptImageRequest{App: "hermes", DeploymentID: "0123456789ab", Refs: []string{"x/y:1"}, Digest: d}
	for _, arch := range []string{"", "amd64", "arm64", "riscv64"} {
		r := good
		r.Arch = arch
		if err := validateAdopt(r); err != nil {
			t.Errorf("arch %q refused: %v", arch, err)
		}
	}
	for _, arch := range []string{"AMD64", "linux/arm64", "a", "arm64 ", "x86-64", string(bytes.Repeat([]byte("a"), 17))} {
		r := good
		r.Arch = arch
		if validateAdopt(r) == nil {
			t.Errorf("arch %q accepted", arch)
		}
	}
}

func TestServerCatalogRoutes(t *testing.T) {
	api := &catalogFake{fakeAPI: &fakeAPI{}}
	cli := startTestServer(t, NewServer(api, "/var/lib/acornfox/uploads"))
	ctx := context.Background()

	_, err := cli.LoadImage(ctx, LoadImageRequest{App: "hermes", Path: "/etc/passwd"})
	var re *RemoteError
	if !errors.As(err, &re) || re.Status != http.StatusBadRequest || re.Code != "refused" {
		t.Fatalf("load outside uploads: %v", err)
	}
	if _, err := cli.LoadImage(ctx, LoadImageRequest{App: "hermes", Path: "/var/lib/acornfox/uploads/catalog-1.tar"}); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := cli.LoadImage(ctx, LoadImageRequest{App: "hermes", Path: "/var/lib/acornfox/uploads/catalog-1.tar", DeploymentID: "../x"}); !errors.As(err, &re) || re.Status != http.StatusBadRequest {
		t.Fatalf("bad deployment id: %v", err)
	}
	if _, err := cli.LoadImage(ctx, LoadImageRequest{App: "hermes", Path: "/var/lib/acornfox/uploads/catalog-1.tar", DeploymentID: "0123456789ab"}); err != nil {
		t.Fatalf("load with tag: %v", err)
	}
	if n := len(api.loaded); n != 2 || api.loaded[1].DeploymentID != "0123456789ab" {
		t.Fatalf("loaded %+v", api.loaded)
	}
	bad := goodWrite()
	bad.Files[0].Path = "../x"
	if _, err := cli.WriteVolumeFiles(ctx, bad); !errors.As(err, &re) || re.Status != http.StatusBadRequest {
		t.Fatalf("bad write: %v", err)
	}
	if res, err := cli.WriteVolumeFiles(ctx, goodWrite()); err != nil || len(api.wrote) != 1 || !res.OK || len(res.Written) != 1 || res.Written[0] != "config.yaml" {
		t.Fatalf("write: %v %d %+v", err, len(api.wrote), res)
	}
	if _, err := cli.AdoptImage(ctx, AdoptImageRequest{App: "hermes", DeploymentID: "0123456789ab", Refs: []string{"a/b:c"}, Digest: "sha256:short"}); !errors.As(err, &re) {
		t.Fatalf("bad digest: %v", err)
	}
	_, err = cli.EnsureContainer(ctx, EnsureContainerRequest{App: "hermes", DeploymentID: "0123456789ab", Image: "x", Port: 9119,
		MemoryMB: 256, CPUMilli: 1000, Links: []string{"Bad_Name"}})
	if !errors.As(err, &re) || re.Code != "refused" {
		t.Fatalf("bad link: %v", err)
	}
	_, err = cli.EnsureContainer(ctx, EnsureContainerRequest{App: "hermes", DeploymentID: "0123456789ab", Image: "x", Port: 9119,
		MemoryMB: 256, CPUMilli: 1000, PidsLimit: MaxPidsLimit + 1})
	if !errors.As(err, &re) || re.Status != http.StatusBadRequest {
		t.Fatalf("bad pids: %v", err)
	}

	// A runner API without the app-center operations answers 501.
	plain := startTestServer(t, NewServer(&fakeAPI{}, "/var/lib/acornfox/uploads"))
	if _, err := plain.WriteVolumeFiles(ctx, goodWrite()); !errors.As(err, &re) || re.Status != http.StatusNotImplemented {
		t.Fatalf("unsupported: %v", err)
	}
}
