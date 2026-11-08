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
func (c *catalogFake) WriteVolumeFiles(ctx context.Context, req WriteFilesRequest) error {
	c.wrote = append(c.wrote, req)
	return nil
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
	if err := cli.WriteVolumeFiles(ctx, bad); !errors.As(err, &re) || re.Status != http.StatusBadRequest {
		t.Fatalf("bad write: %v", err)
	}
	if err := cli.WriteVolumeFiles(ctx, goodWrite()); err != nil || len(api.wrote) != 1 {
		t.Fatalf("write: %v %d", err, len(api.wrote))
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
	if err := plain.WriteVolumeFiles(ctx, goodWrite()); !errors.As(err, &re) || re.Status != http.StatusNotImplemented {
		t.Fatalf("unsupported: %v", err)
	}
}
