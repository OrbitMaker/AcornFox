package runner

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

// App-center operations (installing an app from the catalog). They live in
// their own interface so older runners and the many test fakes of API keep
// working; the install engine checks for it and reports CAT-INST-20 when the
// runner lacks it.
//
//   - AdoptImage: an image already on this machine whose identity matches the
//     catalog's pinned digest (a pre-loaded or just-loaded image) is tagged
//     acornfox/<app>:<id> so it belongs to the app, without any pull. When
//     the request names a CPU architecture, an image built for another one
//     is passed over (for example an amd64 archive an older version loaded
//     on an arm64 machine), so the server goes on to fetch the right one.
//   - LoadImage: `docker load` of an image archive the server downloaded into
//     its upload directory (a large image from the download gateway). The
//     server loads only archives it has checked against the catalog's signed
//     checksum; with DeploymentID the loaded image is tagged
//     acornfox/<app>:<id> and every other name the archive brought is
//     dropped, so the image belongs to the app alone and goes with its tag.
//     This works on both image stores (on the classic overlay2 store a loaded
//     image has no repo digest to compare with the catalog's).
//   - WriteVolumeFiles: write rendered config files into one of the app's
//     named volumes, with mode and owner, through a never-started helper
//     container created from the app's own image. Nothing runs on the host and
//     nothing runs in the helper. A file may be kept when it exists
//     (Overwrite false), or copied aside before it is replaced (Backup); the
//     answer says which files were written, kept and copied aside.
const (
	PathImageAdopt  = "/v1/images/adopt"  // POST AdoptImageRequest -> ImageInfo (404 when no local image matches)
	PathImageLoad   = "/v1/images/load"   // POST LoadImageRequest -> LoadImageResponse (long)
	PathVolumeWrite = "/v1/volumes/write" // POST WriteFilesRequest -> OK
)

// CatalogAPI is implemented by *Docker and *Client.
type CatalogAPI interface {
	AdoptImage(ctx context.Context, req AdoptImageRequest) (ImageInfo, error)
	LoadImage(ctx context.Context, req LoadImageRequest) (LoadImageResponse, error)
	WriteVolumeFiles(ctx context.Context, req WriteFilesRequest) (WriteFilesResult, error)
}

// AdoptImageRequest names candidate local references of one image (for
// example "repo:tag" and "repo@sha256:…") and the digest it must have: the
// image's ID or one of its repo digests must equal Digest. With Arch, the
// image's architecture must also be Arch (an image that does not say which
// it is built for is taken); without it (an older server) any is taken.
type AdoptImageRequest struct {
	App          string   `json:"app"`
	DeploymentID string   `json:"deployment_id"` // tag acornfox/<app>:<id> to create
	Refs         []string `json:"refs"`
	Digest       string   `json:"digest"`         // sha256:<64 hex>
	Arch         string   `json:"arch,omitempty"` // Go/Docker name: amd64, arm64; empty = not checked
}

// LoadImageRequest loads the archive at Path (under the upload directory).
// With DeploymentID the loaded image is tagged acornfox/<app>:<id> and the
// names the archive carried are removed (see LoadImage).
type LoadImageRequest struct {
	App          string `json:"app"`
	Path         string `json:"path"`
	DeploymentID string `json:"deployment_id,omitempty"`
}

// LoadImageResponse lists what Docker reported as loaded ("repo:tag" or
// "sha256:…") and, with DeploymentID, the image now owned by the app.
type LoadImageResponse struct {
	Loaded []string   `json:"loaded"`
	Image  *ImageInfo `json:"image,omitempty"`
}

func validateLoad(req LoadImageRequest) error {
	if !ValidApp(req.App) || !isAbsClean(req.Path) {
		return errors.New("invalid app or path")
	}
	if req.DeploymentID != "" && !ValidDeploymentID(req.DeploymentID) {
		return errors.New("invalid deployment id")
	}
	return nil
}

// VolumeFile is one file to write, relative to the volume root.
type VolumeFile struct {
	Path      string `json:"path"`      // relative, clean, no ".."
	Content   []byte `json:"content"`   // base64 on the wire
	Mode      int    `json:"mode"`      // permission bits, e.g. 0o600
	UID       int    `json:"uid"`       // owner inside the volume
	GID       int    `json:"gid"`       // group inside the volume
	Overwrite bool   `json:"overwrite"` // false: keep an existing file as is
	// Backup (with Overwrite) is where an existing file is copied, with its
	// own mode and owner, before it is replaced: relative like Path and not
	// one of the request's paths. An existing file that cannot be copied
	// (not a regular file, or larger than MaxBackupFileBytes) is then kept
	// as it is instead of being replaced. "" replaces without a copy. An
	// older runner ignores the field (and replaces the file).
	Backup string `json:"backup,omitempty"`
}

// WriteFilesResult says what WriteVolumeFiles did with each file of the
// request, by path. An older runner answers only {"ok": true}: every list is
// then empty and the caller cannot tell written from kept.
type WriteFilesResult struct {
	OK      bool         `json:"ok"`
	Written []string     `json:"written,omitempty"` // written (new, or replacing an existing file)
	Kept    []string     `json:"kept,omitempty"`    // existing and left as they were
	Backups []FileBackup `json:"backups,omitempty"` // existing, copied aside, then replaced
}

// FileBackup is one existing file copied aside before it was replaced.
type FileBackup struct {
	Path   string `json:"path"`
	Backup string `json:"backup"`
}

// WriteFilesRequest writes Files into Volume, mounted at MountPath (the path
// the app itself mounts it at, so a fresh volume is first populated from the
// image exactly as the app would see it).
type WriteFilesRequest struct {
	App       string       `json:"app"`
	Volume    string       `json:"volume"`     // must start with VolumePrefix(app)
	MountPath string       `json:"mount_path"` // absolute, clean
	Image     string       `json:"image"`      // an image of the app (tag acornfox/<app>:<id> or its ID)
	Files     []VolumeFile `json:"files"`
}

// Bounds on WriteFilesRequest.
const (
	MaxVolumeFiles     = 32
	MaxVolumeFileBytes = 256 << 10 // all files together
	// MaxBackupFileBytes is the largest existing file Backup copies aside
	// (read into memory); MaxBackupBytes bounds all copies of one request.
	MaxBackupFileBytes = 2 << 20
	MaxBackupBytes     = 8 << 20
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// archPattern is a Go/Docker architecture name (amd64, arm64, riscv64, …).
var archPattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}$`)

// ValidateWriteFiles checks a request the same way on both sides.
func ValidateWriteFiles(req WriteFilesRequest) error {
	if !ValidApp(req.App) {
		return errors.New("invalid app name")
	}
	if !strings.HasPrefix(req.Volume, VolumePrefix(req.App)) {
		return errors.New("volume must start with " + VolumePrefix(req.App))
	}
	if !isAbsClean(req.MountPath) || req.MountPath == "/" {
		return errors.New("mount_path must be absolute, clean and not /")
	}
	if req.Image == "" {
		return errors.New("image is required")
	}
	if len(req.Files) == 0 || len(req.Files) > MaxVolumeFiles {
		return fmt.Errorf("need 1..%d files", MaxVolumeFiles)
	}
	total := 0
	paths := map[string]bool{}
	for _, f := range req.Files {
		if !validRelPath(f.Path) {
			return fmt.Errorf("invalid file path %q", f.Path)
		}
		paths[f.Path] = true
	}
	for _, f := range req.Files {
		if f.Backup != "" {
			if !f.Overwrite || !validRelPath(f.Backup) || paths[f.Backup] {
				return fmt.Errorf("invalid backup path %q for %s", f.Backup, f.Path)
			}
			paths[f.Backup] = true
		}
		if f.Mode < 0 || f.Mode > 0o777 {
			return fmt.Errorf("invalid mode for %s", f.Path)
		}
		if f.UID < 0 || f.GID < 0 || f.UID > 1<<30 || f.GID > 1<<30 {
			return fmt.Errorf("invalid owner for %s", f.Path)
		}
		total += len(f.Content)
	}
	if total > MaxVolumeFileBytes {
		return fmt.Errorf("files exceed %d bytes", MaxVolumeFileBytes)
	}
	return nil
}

func validRelPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "." {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "" {
			return false
		}
	}
	return true
}

func validateAdopt(req AdoptImageRequest) error {
	if !ValidApp(req.App) {
		return errors.New("invalid app name")
	}
	if !ValidDeploymentID(req.DeploymentID) {
		return errors.New("invalid deployment id")
	}
	if !digestPattern.MatchString(req.Digest) {
		return errors.New("digest must be sha256:<64 hex>")
	}
	if req.Arch != "" && !archPattern.MatchString(req.Arch) {
		return errors.New("invalid arch")
	}
	if len(req.Refs) == 0 || len(req.Refs) > 8 {
		return errors.New("need 1..8 refs")
	}
	for _, r := range req.Refs {
		if r == "" || strings.ContainsAny(r, " \t\n") || len(r) > 512 {
			return errors.New("invalid ref")
		}
	}
	return nil
}

// ---- Docker ----

// AdoptImage implements CatalogAPI.
func (d *Docker) AdoptImage(ctx context.Context, req AdoptImageRequest) (ImageInfo, error) {
	if err := validateAdopt(req); err != nil {
		return ImageInfo{}, &RemoteError{Status: http.StatusBadRequest, ErrorResponse: ErrorResponse{Code: "invalid_request", Message: err.Error()}}
	}
	tag := ImageTag(req.App, req.DeploymentID)
	if info, err := d.inspectByTag(ctx, req.App, tag); err == nil {
		return info, nil
	}
	refs := append(append([]string(nil), req.Refs...), req.Digest)
	for _, ref := range refs {
		res, err := d.c().ImageInspect(ctx, ref)
		if err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			return ImageInfo{}, err
		}
		if !imageMatchesDigest(res.ID, res.RepoDigests, req.Digest) {
			continue
		}
		if !imageArchFits(res.Architecture, req.Arch) {
			// The same digest, built for another CPU: on the containerd image
			// store an image's ID is its index digest, so an archive of one
			// architecture matches a multi-architecture digest. Taking it
			// would start a container that does not run here.
			continue
		}
		if _, err := d.c().ImageTag(ctx, client.ImageTagOptions{Source: res.ID, Target: tag}); err != nil {
			return ImageInfo{}, err
		}
		return d.inspectByTag(ctx, req.App, tag)
	}
	return ImageInfo{}, ErrNotFound
}

// imageMatchesDigest: the image ID (containerd image store: the index or
// manifest digest) or a repo digest equals digest.
func imageMatchesDigest(id string, repoDigests []string, digest string) bool {
	if id == digest {
		return true
	}
	for _, rd := range repoDigests {
		if strings.HasSuffix(rd, "@"+digest) {
			return true
		}
	}
	return false
}

// imageArchFits: want empty (not asked) or an image that does not say its
// architecture fits; otherwise the two must be equal.
func imageArchFits(have, want string) bool {
	return want == "" || have == "" || have == want
}

// LoadImage implements CatalogAPI.
func (d *Docker) LoadImage(ctx context.Context, req LoadImageRequest) (LoadImageResponse, error) {
	if err := validateLoad(req); err != nil {
		return LoadImageResponse{}, &RemoteError{Status: http.StatusBadRequest, ErrorResponse: ErrorResponse{Code: "invalid_request", Message: err.Error()}}
	}
	f, err := os.Open(req.Path)
	if err != nil {
		return LoadImageResponse{}, err
	}
	defer f.Close()
	res, err := d.c().ImageLoad(ctx, f, client.ImageLoadWithQuiet(true))
	if err != nil {
		return LoadImageResponse{}, err
	}
	defer res.Close()
	var out LoadImageResponse
	sc := bufio.NewScanner(res)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var msg struct {
			Stream      string `json:"stream"`
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		if msg.Error != "" {
			return LoadImageResponse{}, errors.New(msg.Error)
		}
		for _, line := range strings.Split(msg.Stream, "\n") {
			line = strings.TrimSpace(line)
			for _, p := range []string{"Loaded image ID: ", "Loaded image: "} {
				if strings.HasPrefix(line, p) {
					out.Loaded = append(out.Loaded, strings.TrimPrefix(line, p))
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return LoadImageResponse{}, err
	}
	if req.DeploymentID == "" {
		return out, nil
	}
	info, err := d.ownLoaded(ctx, req.App, req.DeploymentID, out.Loaded)
	if err != nil {
		return LoadImageResponse{}, err
	}
	out.Image = &info
	return out, nil
}

// ownLoaded tags the first loaded image acornfox/<app>:<id> and drops every
// other name the load created (a name of the same image is untagged; another
// image the archive carried is removed). On failure the loaded names are
// removed, so a rejected archive leaves nothing behind.
func (d *Docker) ownLoaded(ctx context.Context, app, id string, loaded []string) (ImageInfo, error) {
	drop := func() {
		for _, ref := range loaded {
			_, _ = d.c().ImageRemove(ctx, ref, client.ImageRemoveOptions{PruneChildren: true})
		}
	}
	if len(loaded) == 0 {
		return ImageInfo{}, errors.New("docker load reported no image")
	}
	res, err := d.c().ImageInspect(ctx, loaded[0])
	if err != nil {
		drop()
		return ImageInfo{}, err
	}
	tag := ImageTag(app, id)
	if _, err := d.c().ImageTag(ctx, client.ImageTagOptions{Source: res.ID, Target: tag}); err != nil {
		drop()
		return ImageInfo{}, err
	}
	// Names of other images the archive carried are removed with their
	// image; names of the app's image only lose the name (the app's tag
	// keeps the image).
	for _, ref := range loaded {
		if ref == res.ID {
			continue
		}
		other, err := d.c().ImageInspect(ctx, ref)
		if err != nil {
			continue
		}
		if other.ID == res.ID {
			_, _ = d.c().ImageRemove(ctx, ref, client.ImageRemoveOptions{})
		} else {
			_, _ = d.c().ImageRemove(ctx, ref, client.ImageRemoveOptions{PruneChildren: true})
		}
	}
	return d.inspectByTag(ctx, app, tag)
}

// LabelRoleFiles marks the never-started helper container of WriteVolumeFiles.
const RoleFiles = "files"

// WriteVolumeFiles implements CatalogAPI.
func (d *Docker) WriteVolumeFiles(ctx context.Context, req WriteFilesRequest) (WriteFilesResult, error) {
	if err := ValidateWriteFiles(req); err != nil {
		return WriteFilesResult{}, &RemoteError{Status: http.StatusBadRequest, ErrorResponse: ErrorResponse{Code: "invalid_request", Message: err.Error()}}
	}
	img, err := d.inspectImage(ctx, req.Image, req.App)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return WriteFilesResult{}, &RemoteError{Status: http.StatusForbidden, ErrorResponse: ErrorResponse{Code: "refused", Message: "image is not an image of this app"}}
		}
		return WriteFilesResult{}, err
	}
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return WriteFilesResult{}, err
	}
	helperID := hex.EncodeToString(rnd[:])
	name := ContainerName(req.App, helperID)
	created, err := d.c().ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Image:           img.ID,
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
		return WriteFilesResult{}, err
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, _ = d.c().ContainerRemove(rctx, created.ID, client.ContainerRemoveOptions{Force: true})
	}()

	res := WriteFilesResult{OK: true}
	var files []VolumeFile
	copied := 0
	for _, f := range req.Files {
		if !f.Overwrite || f.Backup != "" {
			target := path.Join(req.MountPath, f.Path)
			st, serr := d.c().ContainerStatPath(ctx, created.ID, client.ContainerStatPathOptions{Path: target})
			switch {
			case serr == nil && !f.Overwrite:
				res.Kept = append(res.Kept, f.Path) // keep the user's file
				continue
			case serr == nil:
				b, ok, err := d.backupFile(ctx, created.ID, target, st.Stat, f, MaxBackupBytes-copied)
				if err != nil {
					return WriteFilesResult{}, err
				}
				if !ok {
					// Not copyable: replacing it would lose it, so keep it.
					res.Kept = append(res.Kept, f.Path)
					continue
				}
				copied += len(b.Content)
				files = append(files, b)
				res.Backups = append(res.Backups, FileBackup{Path: f.Path, Backup: f.Backup})
			case !cerrdefs.IsNotFound(serr):
				return WriteFilesResult{}, serr
			}
		}
		files = append(files, f)
		res.Written = append(res.Written, f.Path)
	}
	if len(files) == 0 {
		return res, nil
	}
	archive, err := volumeFilesTar(files, time.Now())
	if err != nil {
		return WriteFilesResult{}, err
	}
	if _, err := d.c().CopyToContainer(ctx, created.ID, client.CopyToContainerOptions{
		DestinationPath: req.MountPath,
		Content:         bytes.NewReader(archive),
	}); err != nil {
		return WriteFilesResult{}, err
	}
	return res, nil
}

// backupFile reads the existing file at target (in the helper container) and
// returns it as the file to write at f.Backup, with its own mode and owner.
// ok is false when it is not a regular file or is larger than
// MaxBackupFileBytes or room: it is then not copied (and must not be
// replaced).
func (d *Docker) backupFile(ctx context.Context, id, target string, st container.PathStat, f VolumeFile, room int) (VolumeFile, bool, error) {
	limit := int64(MaxBackupFileBytes)
	if int64(room) < limit {
		limit = int64(room)
	}
	if !st.Mode.IsRegular() || st.Size > limit {
		return VolumeFile{}, false, nil
	}
	rc, err := d.c().CopyFromContainer(ctx, id, client.CopyFromContainerOptions{SourcePath: target})
	if err != nil {
		return VolumeFile{}, false, err
	}
	defer rc.Content.Close()
	return backupFromTar(rc.Content, f, limit)
}

// backupFromTar takes the single regular file of a CopyFromContainer
// archive as the copy of f, at f.Backup.
func backupFromTar(r io.Reader, f VolumeFile, limit int64) (VolumeFile, bool, error) {
	tr := tar.NewReader(r)
	h, err := tr.Next()
	if err != nil {
		return VolumeFile{}, false, fmt.Errorf("read %s: %w", f.Path, err)
	}
	if h.Typeflag != tar.TypeReg || h.Size > limit {
		return VolumeFile{}, false, nil
	}
	content, err := io.ReadAll(io.LimitReader(tr, limit+1))
	if err != nil {
		return VolumeFile{}, false, fmt.Errorf("read %s: %w", f.Path, err)
	}
	if int64(len(content)) > limit {
		return VolumeFile{}, false, nil
	}
	uid, gid := h.Uid, h.Gid
	if uid < 0 || gid < 0 || uid > 1<<30 || gid > 1<<30 {
		uid, gid = f.UID, f.GID
	}
	return VolumeFile{Path: f.Backup, Content: content, Mode: int(h.Mode) & 0o777, UID: uid, GID: gid, Overwrite: true}, true, nil
}

// volumeFilesTar builds the archive: parent directories (0755, same owner as
// the file) and the files with their mode and owner.
func volumeFilesTar(files []VolumeFile, now time.Time) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	dirs := map[string]bool{}
	for _, f := range files {
		parts := strings.Split(f.Path, "/")
		for i := 1; i < len(parts); i++ {
			dir := strings.Join(parts[:i], "/")
			if dirs[dir] {
				continue
			}
			dirs[dir] = true
			if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: dir + "/", Mode: 0o755, Uid: f.UID, Gid: f.GID, ModTime: now}); err != nil {
				return nil, err
			}
		}
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: f.Path, Mode: int64(f.Mode), Uid: f.UID, Gid: f.GID, Size: int64(len(f.Content)), ModTime: now}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(f.Content); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- Client ----

// AdoptImage implements CatalogAPI.
func (c *Client) AdoptImage(ctx context.Context, req AdoptImageRequest) (ImageInfo, error) {
	var out ImageInfo
	err := c.call(ctx, c.standard, PathImageAdopt, req, clientDefaultBound, &out)
	return out, err
}

// LoadImage implements CatalogAPI. Loading a large archive takes minutes.
func (c *Client) LoadImage(ctx context.Context, req LoadImageRequest) (LoadImageResponse, error) {
	var out LoadImageResponse
	err := c.call(ctx, c.long, PathImageLoad, req, clientDefaultBound, &out)
	return out, err
}

// WriteVolumeFiles implements CatalogAPI. An older runner answers without
// the lists (see WriteFilesResult).
func (c *Client) WriteVolumeFiles(ctx context.Context, req WriteFilesRequest) (WriteFilesResult, error) {
	var out WriteFilesResult
	err := c.call(ctx, c.standard, PathVolumeWrite, req, clientDefaultBound, &out)
	return out, err
}

// ---- Server ----

func (s *Server) catalogAPI(w http.ResponseWriter) (CatalogAPI, bool) {
	c, ok := s.api.(CatalogAPI)
	if !ok {
		writeError(w, http.StatusNotImplemented, "unsupported", "this runner cannot install catalog apps")
	}
	return c, ok
}

func (s *Server) handleImageAdopt(w http.ResponseWriter, r *http.Request) {
	var req AdoptImageRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}
	if err := validateAdopt(req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	c, ok := s.catalogAPI(w)
	if !ok {
		return
	}
	info, err := c.AdoptImage(r.Context(), req)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleImageLoad(w http.ResponseWriter, r *http.Request) {
	var req LoadImageRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}
	if !validAppName(w, req.App) {
		return
	}
	if req.DeploymentID != "" && !ValidDeploymentID(req.DeploymentID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid deployment id")
		return
	}
	if !isAbsClean(req.Path) || !s.underUploadDir(req.Path) || req.Path == s.uploadDir {
		writeError(w, http.StatusBadRequest, "refused", "path must be a file under the upload directory")
		return
	}
	c, ok := s.catalogAPI(w)
	if !ok {
		return
	}
	out, err := c.LoadImage(r.Context(), req)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleVolumeWrite(w http.ResponseWriter, r *http.Request) {
	var req WriteFilesRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}
	if err := ValidateWriteFiles(req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	c, ok := s.catalogAPI(w)
	if !ok {
		return
	}
	res, err := c.WriteVolumeFiles(r.Context(), req)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	res.OK = true
	writeJSON(w, http.StatusOK, res)
}
