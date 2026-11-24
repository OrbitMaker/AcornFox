package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// volumeDataFake keeps what ExportVolume streams and what ImportVolume got.
type volumeDataFake struct {
	*fakeAPI
	mu       sync.Mutex
	exported VolumeDataRequest
	imported VolumeDataRequest
	body     []byte
	data     string // what export streams
	cutAfter int    // > 0: stream that many bytes, then fail
	exportEr error
	sizes    []VolumeSize
}

func (v *volumeDataFake) ExportVolume(_ context.Context, req VolumeDataRequest, w io.Writer) error {
	v.mu.Lock()
	v.exported = req
	v.mu.Unlock()
	if v.exportEr != nil {
		return v.exportEr
	}
	if v.cutAfter > 0 {
		_, _ = w.Write([]byte(v.data[:v.cutAfter]))
		return errors.New("docker went away")
	}
	_, err := io.WriteString(w, v.data)
	return err
}

func (v *volumeDataFake) ImportVolume(_ context.Context, req VolumeDataRequest, r io.Reader) error {
	b, err := io.ReadAll(r)
	v.mu.Lock()
	v.imported, v.body = req, b
	v.mu.Unlock()
	return err
}

func (v *volumeDataFake) VolumeSizes(_ context.Context, app string) ([]VolumeSize, error) {
	return v.sizes, nil
}

func TestValidateVolumeData(t *testing.T) {
	good := VolumeDataRequest{App: "hello", Volume: "af-hello-1", MountPath: "/data", Image: "sha256:abc"}
	if _, err := ValidateVolumeData(good); err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(*VolumeDataRequest){
		"bad app":        func(r *VolumeDataRequest) { r.App = "Bad_App" },
		"other volume":   func(r *VolumeDataRequest) { r.Volume = "af-other-1" },
		"relative mount": func(r *VolumeDataRequest) { r.MountPath = "data" },
		"root mount":     func(r *VolumeDataRequest) { r.MountPath = "/" },
		"dirty mount":    func(r *VolumeDataRequest) { r.MountPath = "/data/../etc" },
		"no image":       func(r *VolumeDataRequest) { r.Image = "" },
	} {
		r := good
		mod(&r)
		if _, err := ValidateVolumeData(r); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// An add-on's volume takes the spec's path and image whatever is sent.
	r, err := ValidateVolumeData(VolumeDataRequest{App: "hello", Volume: AddonVolumeName("hello", "postgres"), Addon: "postgres", MountPath: "/etc", Image: "evil"})
	if err != nil || r.MountPath != "/var/lib/postgresql/data" || r.Image != "postgres:16-alpine" {
		t.Fatalf("addon: %v %+v", err, r)
	}
	if _, err := ValidateVolumeData(VolumeDataRequest{App: "hello", Volume: "af-hello-1", Addon: "postgres"}); err == nil {
		t.Error("an add-on request for another volume was accepted")
	}
	if _, err := ValidateVolumeData(VolumeDataRequest{App: "hello", Volume: "af-hello-addon-x-data", Addon: "x"}); err == nil {
		t.Error("an unknown add-on was accepted")
	}
}

func TestServerVolumeDataRoutes(t *testing.T) {
	api := &volumeDataFake{fakeAPI: &fakeAPI{}, data: strings.Repeat("tar-bytes-", 5000), sizes: []VolumeSize{{Name: "af-hello-1", Bytes: 42}}}
	cli := startTestServer(t, NewServer(api, "/var/lib/acornfox/uploads"))
	ctx := context.Background()
	req := VolumeDataRequest{App: "hello", Volume: "af-hello-1", MountPath: "/data", Image: "sha256:abc"}

	var out bytes.Buffer
	if err := cli.ExportVolume(ctx, req, &out); err != nil || out.String() != api.data {
		t.Fatalf("export: %v (%d bytes)", err, out.Len())
	}
	if api.exported != req {
		t.Fatalf("export request: %+v", api.exported)
	}

	if err := cli.ImportVolume(ctx, req, strings.NewReader(api.data)); err != nil {
		t.Fatalf("import: %v", err)
	}
	if string(api.body) != api.data || api.imported != req {
		t.Fatalf("import got %d bytes, request %+v", len(api.body), api.imported)
	}

	sizes, err := cli.VolumeSizes(ctx, "hello")
	if err != nil || len(sizes) != 1 || sizes[0].Bytes != 42 {
		t.Fatalf("sizes: %v %v", err, sizes)
	}

	// Refused before the runner is asked.
	var re *RemoteError
	bad := req
	bad.Volume = "af-other-1"
	if err := cli.ExportVolume(ctx, bad, &out); !errors.As(err, &re) || re.Status != http.StatusBadRequest {
		t.Fatalf("export of another app's volume: %v", err)
	}
	if err := cli.ImportVolume(ctx, bad, strings.NewReader("x")); !errors.As(err, &re) || re.Status != http.StatusBadRequest {
		t.Fatalf("import into another app's volume: %v", err)
	}

	// A failure before the first byte is an error answer.
	api.exportEr = &RemoteError{Status: http.StatusForbidden, ErrorResponse: ErrorResponse{Code: "refused", Message: "image is not an image of this app"}}
	if err := cli.ExportVolume(ctx, req, &out); !errors.As(err, &re) || re.Status != http.StatusForbidden || re.Code != "refused" {
		t.Fatalf("export refused: %v", err)
	}
	api.exportEr = nil

	// A failure in the middle of the stream is never a clean end.
	api.cutAfter = 1000
	var cut bytes.Buffer
	if err := cli.ExportVolume(ctx, req, &cut); err == nil {
		t.Fatalf("a cut-off stream ended cleanly with %d bytes", cut.Len())
	}

	// A runner API without volume data answers 501.
	plain := startTestServer(t, NewServer(&fakeAPI{}, "/var/lib/acornfox/uploads"))
	if err := plain.ExportVolume(ctx, req, &out); !errors.As(err, &re) || re.Status != http.StatusNotImplemented {
		t.Fatalf("unsupported export: %v", err)
	}
	if _, err := plain.VolumeSizes(ctx, "hello"); !errors.As(err, &re) || re.Status != http.StatusNotImplemented {
		t.Fatalf("unsupported sizes: %v", err)
	}
}
