package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"github.com/acornfox/acornfox/internal/peer"
)

// Volume data: the contents of one named volume as a tar stream, for app
// data backups (internal/appbackup). Like WriteVolumeFiles it goes through a
// never-started helper container created from the app's own image (an
// add-on's pinned image for an add-on volume): nothing runs on the host and
// nothing runs in the helper, and no host path is ever mounted.
//
//   - ExportVolume: the volume's files as a tar stream (the entries are named
//     "./…", owner, mode and times kept). The caller stops the app first for
//     a consistent copy; the runner does not.
//   - ImportVolume: extracts a tar stream (as ExportVolume made it) into the
//     volume, on top of what is there. A restore removes the volume first and
//     creates it again (RemoveVolume, EnsureVolume), so nothing of the old
//     contents is left.
//   - VolumeSizes: how many bytes each of an app's volumes uses (-1 when
//     Docker does not say), for the disk check before a backup.
//
// The routes carry the stream in the body: export answers 200 with the tar;
// a failure after the first byte aborts the connection (the caller sees an
// unexpected end, never a clean one), so a cut-off archive is never taken for
// a whole one. Import takes its parameters in the query string and the tar in
// the request body.
const (
	PathVolumeExport = "/v1/volumes/export" // POST VolumeDataRequest -> application/x-tar
	PathVolumeImport = "/v1/volumes/import" // POST ?app&volume&mount_path&image&addon, body tar -> OK
	PathVolumeSizes  = "/v1/volumes/sizes"  // POST AppRef -> VolumeSizesResponse
)

// VolumeDataAPI is implemented by *Docker and *Client (an optional interface,
// like CatalogAPI: an older runner answers 404 and the server reports that).
type VolumeDataAPI interface {
	ExportVolume(ctx context.Context, req VolumeDataRequest, w io.Writer) error
	ImportVolume(ctx context.Context, req VolumeDataRequest, r io.Reader) error
	VolumeSizes(ctx context.Context, app string) ([]VolumeSize, error)
}

// VolumeDataRequest names one volume of App. For an app's own volume
// (Addon "") MountPath is where the app mounts it and Image an image of the
// app (acornfox/<app>:<id> or its ID). For an add-on's volume Addon is the
// kind: the volume must be AddonVolumeName(App, Addon), and the mount path
// and image are the add-on spec's (MountPath and Image are ignored).
type VolumeDataRequest struct {
	App       string `json:"app"`
	Volume    string `json:"volume"` // must start with VolumePrefix(app)
	MountPath string `json:"mount_path,omitempty"`
	Image     string `json:"image,omitempty"`
	Addon     string `json:"addon,omitempty"`
}

// VolumeSize is the space one volume uses; Bytes -1 when unknown.
type VolumeSize struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

// VolumeSizesResponse is the answer of PathVolumeSizes.
type VolumeSizesResponse struct {
	Volumes []VolumeSize `json:"volumes"`
}

// volumeDataBound is the largest stream the client takes (the body is
// copied to a file, never held in memory).
const volumeDataBound = int64(1) << 50

// volumeDataTimeout bounds one export or import over the peer socket.
const volumeDataTimeout = 12 * time.Hour

// ValidateVolumeData checks a request the same way on both sides and fills
// the add-on's mount path and image in.
func ValidateVolumeData(req VolumeDataRequest) (VolumeDataRequest, error) {
	if !ValidApp(req.App) {
		return req, errors.New("invalid app name")
	}
	if !strings.HasPrefix(req.Volume, VolumePrefix(req.App)) {
		return req, errors.New("volume must start with " + VolumePrefix(req.App))
	}
	if req.Addon != "" {
		spec, ok := AddonSpecFor(req.Addon)
		if !ok {
			return req, fmt.Errorf("unknown add-on %q", req.Addon)
		}
		if req.Volume != AddonVolumeName(req.App, req.Addon) {
			return req, fmt.Errorf("add-on %s data volume is %s", req.Addon, AddonVolumeName(req.App, req.Addon))
		}
		req.MountPath, req.Image = spec.DataPath, spec.Image
		return req, nil
	}
	if !isAbsClean(req.MountPath) || req.MountPath == "/" {
		return req, errors.New("mount_path must be absolute, clean and not /")
	}
	if req.Image == "" {
		return req, errors.New("image is required")
	}
	return req, nil
}

// ---- Docker ----

// volumeHelper creates the never-started helper container with the volume
// mounted at req.MountPath and returns its ID and a function that removes it.
func (d *Docker) volumeHelper(ctx context.Context, req VolumeDataRequest) (string, func(), error) {
	var imageID string
	if req.Addon != "" {
		if err := d.ensureAddonImage(ctx, req.Image); err != nil {
			return "", nil, err
		}
		res, err := d.c().ImageInspect(ctx, req.Image)
		if err != nil {
			return "", nil, err
		}
		imageID = res.ID
	} else {
		img, err := d.inspectImage(ctx, req.Image, req.App)
		if err != nil {
			if cerrdefs.IsNotFound(err) {
				return "", nil, &RemoteError{Status: http.StatusForbidden, ErrorResponse: ErrorResponse{Code: "refused", Message: "image is not an image of this app"}}
			}
			return "", nil, err
		}
		imageID = img.ID
	}
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", nil, err
	}
	helperID := hex.EncodeToString(rnd[:])
	created, err := d.c().ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: ContainerName(req.App, helperID),
		Config: &container.Config{
			Image:           imageID,
			Labels:          map[string]string{LabelManaged: "1", LabelApp: req.App, LabelDeployment: helperID, LabelRole: RoleFiles},
			NetworkDisabled: true,
		},
		HostConfig: &container.HostConfig{
			NetworkMode: "none",
			SecurityOpt: []string{"no-new-privileges"},
			Mounts:      []mount.Mount{{Type: mount.TypeVolume, Source: req.Volume, Target: req.MountPath}},
		},
	})
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, _ = d.c().ContainerRemove(rctx, created.ID, client.ContainerRemoveOptions{Force: true})
	}
	return created.ID, cleanup, nil
}

// ExportVolume implements VolumeDataAPI.
func (d *Docker) ExportVolume(ctx context.Context, req VolumeDataRequest, w io.Writer) error {
	req, err := ValidateVolumeData(req)
	if err != nil {
		return &RemoteError{Status: http.StatusBadRequest, ErrorResponse: ErrorResponse{Code: "invalid_request", Message: err.Error()}}
	}
	id, cleanup, err := d.volumeHelper(ctx, req)
	if err != nil {
		return err
	}
	defer cleanup()
	// "<dir>/." is the directory's contents, not the directory itself.
	rc, err := d.c().CopyFromContainer(ctx, id, client.CopyFromContainerOptions{SourcePath: req.MountPath + "/."})
	if err != nil {
		return err
	}
	defer rc.Content.Close()
	_, err = io.Copy(w, rc.Content)
	return err
}

// ImportVolume implements VolumeDataAPI.
func (d *Docker) ImportVolume(ctx context.Context, req VolumeDataRequest, r io.Reader) error {
	req, err := ValidateVolumeData(req)
	if err != nil {
		return &RemoteError{Status: http.StatusBadRequest, ErrorResponse: ErrorResponse{Code: "invalid_request", Message: err.Error()}}
	}
	id, cleanup, err := d.volumeHelper(ctx, req)
	if err != nil {
		return err
	}
	defer cleanup()
	_, err = d.c().CopyToContainer(ctx, id, client.CopyToContainerOptions{DestinationPath: req.MountPath, Content: r})
	return err
}

// VolumeSizes implements VolumeDataAPI: the managed volumes of app and the
// bytes each uses, as Docker's disk usage reports them.
func (d *Docker) VolumeSizes(ctx context.Context, app string) ([]VolumeSize, error) {
	if !ValidApp(app) {
		return nil, &RemoteError{Status: http.StatusBadRequest, ErrorResponse: ErrorResponse{Code: "invalid_request", Message: "invalid app name"}}
	}
	res, err := d.c().DiskUsage(ctx, client.DiskUsageOptions{Volumes: true, Verbose: true})
	if err != nil {
		return nil, err
	}
	var out []VolumeSize
	for _, v := range res.Volumes.Items {
		if v.Labels[LabelManaged] != "1" || v.Labels[LabelApp] != app {
			continue
		}
		size := int64(-1)
		if v.UsageData != nil && v.UsageData.Size >= 0 {
			size = v.UsageData.Size
		}
		out = append(out, VolumeSize{Name: v.Name, Bytes: size})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ---- Client ----

// bulkClient is the peer client for volume streams (a Client built as a
// struct literal in tests has none: the long one then).
func (c *Client) bulkClient() *peer.Client {
	if c.bulk != nil {
		return c.bulk
	}
	return c.long
}

// remoteFailure turns a non-2xx answer into the error call() would give.
func remoteFailure(resp *http.Response) error {
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var er ErrorResponse
	_ = json.Unmarshal(payload, &er)
	if resp.StatusCode == http.StatusNotFound && er.Code == "not_found" {
		return ErrNotFound
	}
	if er.Code == "" {
		er.Message = fmt.Sprintf("runner returned status %d", resp.StatusCode)
	}
	return &RemoteError{Status: resp.StatusCode, ErrorResponse: er}
}

// ExportVolume implements VolumeDataAPI: the tar stream is copied to w.
func (c *Client) ExportVolume(ctx context.Context, req VolumeDataRequest, w io.Writer) error {
	body, err := marshalJSON(req)
	if err != nil {
		return err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+PathVolumeExport, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := c.bulkClient().DoStream(hreq, volumeDataBound)
	if err != nil {
		if isDialError(err) {
			return ErrUnavailable
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return remoteFailure(resp)
	}
	_, err = io.Copy(w, resp.Body)
	if err != nil && isDialError(err) {
		return ErrUnavailable
	}
	return err
}

// ImportVolume implements VolumeDataAPI: the tar stream r is the request
// body.
func (c *Client) ImportVolume(ctx context.Context, req VolumeDataRequest, r io.Reader) error {
	q := url.Values{}
	q.Set("app", req.App)
	q.Set("volume", req.Volume)
	if req.MountPath != "" {
		q.Set("mount_path", req.MountPath)
	}
	if req.Image != "" {
		q.Set("image", req.Image)
	}
	if req.Addon != "" {
		q.Set("addon", req.Addon)
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+PathVolumeImport+"?"+q.Encode(), r)
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/x-tar")
	resp, err := c.bulkClient().DoStream(hreq, clientDefaultBound)
	if err != nil {
		if isDialError(err) {
			return ErrUnavailable
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return remoteFailure(resp)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, clientDefaultBound))
	return nil
}

// VolumeSizes implements VolumeDataAPI.
func (c *Client) VolumeSizes(ctx context.Context, app string) ([]VolumeSize, error) {
	var out VolumeSizesResponse
	err := c.call(ctx, c.long, PathVolumeSizes, AppRef{App: app}, clientLargeBound, &out)
	return out.Volumes, err
}

// ---- Server ----

func (s *Server) volumeDataAPI(w http.ResponseWriter) (VolumeDataAPI, bool) {
	v, ok := s.api.(VolumeDataAPI)
	if !ok {
		writeError(w, http.StatusNotImplemented, "unsupported", "this runner cannot export or import volume data")
	}
	return v, ok
}

// headerOnWrite sends the 200 header with the first byte, so an error before
// any byte can still be a proper error answer.
type headerOnWrite struct {
	w       http.ResponseWriter
	started bool
}

func (h *headerOnWrite) Write(p []byte) (int, error) {
	if !h.started {
		h.started = true
		h.w.Header().Set("Content-Type", "application/x-tar")
		h.w.WriteHeader(http.StatusOK)
	}
	return h.w.Write(p)
}

func (s *Server) handleVolumeExport(w http.ResponseWriter, r *http.Request) {
	var req VolumeDataRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}
	req, err := ValidateVolumeData(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	v, ok := s.volumeDataAPI(w)
	if !ok {
		return
	}
	hw := &headerOnWrite{w: w}
	if err := v.ExportVolume(r.Context(), req, hw); err != nil {
		if !hw.started {
			writeAPIError(w, err)
			return
		}
		// The stream is already on its way: end the connection without the
		// normal ending so the caller sees a broken stream, not a short one.
		panic(http.ErrAbortHandler)
	}
	if !hw.started {
		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) handleVolumeImport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := VolumeDataRequest{
		App: q.Get("app"), Volume: q.Get("volume"), MountPath: q.Get("mount_path"),
		Image: q.Get("image"), Addon: q.Get("addon"),
	}
	req, err := ValidateVolumeData(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	v, ok := s.volumeDataAPI(w)
	if !ok {
		return
	}
	if err := v.ImportVolume(r.Context(), req, r.Body); err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{OK: true})
}

func (s *Server) handleVolumeSizes(w http.ResponseWriter, r *http.Request) {
	var req AppRef
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}
	if !validAppName(w, req.App) {
		return
	}
	v, ok := s.volumeDataAPI(w)
	if !ok {
		return
	}
	sizes, err := v.VolumeSizes(r.Context(), req.App)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, VolumeSizesResponse{Volumes: sizes})
}

// ensure Docker and Client satisfy VolumeDataAPI at compile time.
var (
	_ VolumeDataAPI = (*Client)(nil)
	_ VolumeDataAPI = (*Docker)(nil)
)
