package runner

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// PathContainerLogTail: POST logTailRequest -> LogTailResult.
const PathContainerLogTail = "/v1/containers/log-tail"

// LogTailMaxBytes caps LogTail's maxBytes.
const LogTailMaxBytes = 1 << 20

// logTailLines is how many lines Docker is asked to send at most; the ring
// buffer then keeps the last maxBytes of them.
const logTailLines = 20000

// logTailTimeout bounds a LogTail call on the client side.
const logTailTimeout = 15 * time.Second

// LogTailer is optional, like LogBatchReader: it is not part of API so that
// other API implementations need not provide it. Docker and Client implement
// it; a Server whose API lacks it answers 501 unsupported.
type LogTailer interface {
	// LogTail reads the last maxBytes bytes (at most LogTailMaxBytes; <= 0
	// means LogTailMaxBytes) of a managed container's log, running or
	// stopped, each line prefixed with its Docker timestamp, and returns the
	// container's environment (KEY=VALUE) so the caller can redact values
	// that only that container knew.
	LogTail(ctx context.Context, app, name string, maxBytes int) (LogTailResult, error)
}

// LogTailResult is the raw log tail. Data starts at a line boundary when
// Truncated is true (earlier output was cut, by the byte bound or because
// Docker only sent the last logTailLines lines).
type LogTailResult struct {
	Data      []byte   `json:"data"` // base64 in JSON
	Truncated bool     `json:"truncated"`
	Env       []string `json:"env"`
}

type logTailRequest struct {
	App      string `json:"app"`
	Name     string `json:"name"`
	MaxBytes int    `json:"max_bytes"`
}

func clampLogTailBytes(n int) int {
	if n <= 0 || n > LogTailMaxBytes {
		return LogTailMaxBytes
	}
	return n
}

// LogTail implements LogTailer. Docker sends at most logTailLines lines,
// which stream through a maxBytes ring; nothing is read whole into memory.
func (d *Docker) LogTail(ctx context.Context, app, name string, maxBytes int) (LogTailResult, error) {
	maxBytes = clampLogTailBytes(maxBytes)
	c, err := d.managedContainer(ctx, app, name)
	if err != nil {
		return LogTailResult{}, err
	}
	rc, err := d.c().ContainerLogs(ctx, c.ID, client.ContainerLogsOptions{
		ShowStdout: true, ShowStderr: true, Timestamps: true, Tail: strconv.Itoa(logTailLines),
	})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return LogTailResult{}, ErrNotFound
		}
		return LogTailResult{}, err
	}
	defer rc.Close()
	out := newTailBuffer(maxBytes)
	if c.Config.Tty {
		_, err = io.Copy(out, rc)
	} else {
		_, err = stdcopy.StdCopy(out, out, rc)
	}
	if err != nil {
		return LogTailResult{}, err
	}
	return logTailResult(out, d.containerOwnEnv(ctx, c.Image, c.Config.Env)), nil
}

// containerOwnEnv drops the entries the image itself sets (same KEY=VALUE),
// leaving what AcornFox injected or the user overrode, so callers do not
// mistake base-image settings like NODE_ENV=production for old secrets.
// When the image cannot be read the full environment is returned: masking
// too much is safer than too little.
func (d *Docker) containerOwnEnv(ctx context.Context, image string, env []string) []string {
	res, err := d.c().ImageInspect(ctx, image)
	if err != nil || res.Config == nil {
		return env
	}
	fromImage := make(map[string]bool, len(res.Config.Env))
	for _, kv := range res.Config.Env {
		fromImage[kv] = true
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !fromImage[kv] {
			out = append(out, kv)
		}
	}
	return out
}

// logTailResult builds the reply from the ring: aligned to a line start when
// bytes were dropped, Truncated also when Docker's line cap may have cut.
func logTailResult(out *tailBuffer, env []string) LogTailResult {
	data, omitted := alignTail(out.Bytes(), out.Dropped())
	return LogTailResult{
		Data:      data,
		Truncated: omitted > 0 || out.Newlines() >= logTailLines,
		Env:       append(make([]string, 0, len(env)), env...),
	}
}

// LogTail implements LogTailer over the socket. A runner without the route
// (older version) or whose API lacks the capability yields an error matching
// ErrUnsupported.
func (c *Client) LogTail(ctx context.Context, app, name string, maxBytes int) (LogTailResult, error) {
	ctx, cancel := context.WithTimeout(ctx, logTailTimeout)
	defer cancel()
	var out LogTailResult
	err := c.call(ctx, c.standard, PathContainerLogTail, logTailRequest{App: app, Name: name, MaxBytes: maxBytes}, clientLargeBound, &out)
	return out, unsupportedRoute(err)
}

// unsupportedRoute maps "the runner does not know this route or capability"
// to an error matching ErrUnsupported (keeping the *RemoteError reachable).
// A 404 carrying the runner's own not_found code is a missing object, not a
// missing route, and is already ErrNotFound.
func unsupportedRoute(err error) error {
	var re *RemoteError
	if !errors.As(err, &re) {
		return err
	}
	switch {
	case re.Status == http.StatusNotImplemented && re.Code == "unsupported",
		(re.Status == http.StatusNotFound || re.Status == http.StatusMethodNotAllowed) && re.Code == "":
		return errors.Join(ErrUnsupported, err)
	}
	return err
}

func (s *Server) handleLogTail(w http.ResponseWriter, r *http.Request) {
	var req logTailRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}
	if !validContainerName(w, req.App, req.Name) {
		return
	}
	tailer, ok := s.api.(LogTailer)
	if !ok {
		writeError(w, http.StatusNotImplemented, "unsupported", "log tail not supported")
		return
	}
	res, err := tailer.LogTail(r.Context(), req.App, req.Name, clampLogTailBytes(req.MaxBytes))
	if err != nil {
		writeAPIError(w, err)
		return
	}
	if res.Data == nil {
		res.Data = []byte{}
	}
	if res.Env == nil {
		res.Env = []string{}
	}
	writeJSON(w, http.StatusOK, res)
}

var (
	_ LogTailer = (*Client)(nil)
	_ LogTailer = (*Docker)(nil)
)
