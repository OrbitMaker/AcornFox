package apiserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/acornfox/acornfox/internal/faults"
	"github.com/acornfox/acornfox/internal/setup"
)

// Setup is what the API needs from the runtime setup (*setup.Manager).
type Setup interface {
	Status() setup.Status
	Events(ctx context.Context, since int64, wait time.Duration) ([]setup.Event, bool)
	Start(opts setup.StartOptions) error
	Resume() error
	Pause() bool
}

// maxSetupWait bounds GET /v1/setup/events?wait=.
const maxSetupWait = 25 * time.Second

// GET /v1/setup: the whole setup state (steps, items, need, checks, error).
func (s *server) getSetup(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeError(w, http.StatusNotFound, "not_found", "这台服务器没有运行环境准备流程")
		return
	}
	writeJSON(w, http.StatusOK, s.setup.Status())
}

// GET /v1/setup/events?since=N&wait=25: events after N; waits up to wait
// seconds for new ones. {"events":[...],"resync":bool,"seq":N}.
func (s *server) getSetupEvents(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeError(w, http.StatusNotFound, "not_found", "这台服务器没有运行环境准备流程")
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	wait := time.Duration(0)
	if v, err := strconv.Atoi(r.URL.Query().Get("wait")); err == nil && v > 0 {
		wait = time.Duration(v) * time.Second
		if wait > maxSetupWait {
			wait = maxSetupWait
		}
	}
	evs, resync := s.setup.Events(r.Context(), since, wait)
	if evs == nil {
		evs = []setup.Event{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": evs, "resync": resync, "seq": s.setup.Status().Seq})
}

// POST /v1/setup/start {"runtime","mirror","confirm_admin"}.
func (s *server) postSetupStart(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeError(w, http.StatusNotFound, "not_found", "这台服务器没有运行环境准备流程")
		return
	}
	var opts setup.StartOptions
	if err := decodeJSON(r, &opts); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不对："+err.Error())
		return
	}
	switch opts.Runtime {
	case "", setup.RuntimeExisting, setup.RuntimeBuiltin, setup.RuntimeDockerDesktop:
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "runtime 只能是 builtin、docker-desktop、existing 或留空")
		return
	}
	if opts.Runtime == setup.RuntimeExisting {
		opts.Runtime = ""
	}
	s.setupAction(w, s.setup.Start(opts))
}

// POST /v1/setup/resume: repeat the last start (after a failure or pause).
func (s *server) postSetupResume(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeError(w, http.StatusNotFound, "not_found", "这台服务器没有运行环境准备流程")
		return
	}
	s.setupAction(w, s.setup.Resume())
}

// POST /v1/setup/pause: stop the current run; partial downloads are kept.
func (s *server) postSetupPause(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeError(w, http.StatusNotFound, "not_found", "这台服务器没有运行环境准备流程")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paused": s.setup.Pause()})
}

func (s *server) setupAction(w http.ResponseWriter, err error) {
	if errors.Is(err, setup.ErrBusy) {
		writeError(w, http.StatusConflict, "busy", "运行环境正在准备中，等它结束或先暂停")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, s.setup.Status())
}

// GET /v1/setup/faults: every fault code in the short shape the console's
// runtime page joins its check results to (code, title, causes, next,
// needs_user). The setup codes read exactly as internal/setup Faults; the
// full catalog is internal/faults (GET /v1/faults).
func (s *server) getSetupFaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"faults": faults.SetupDefs()})
}
