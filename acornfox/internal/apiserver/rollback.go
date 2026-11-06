package apiserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/state"
)

// maxRollbackBody bounds the optional rollback request body.
const maxRollbackBody = 4 << 10

// rollbackRequest is the optional body of both rollback routes: empty, {} or
// {"cancel_pending": true}. Unknown fields are refused, so a version number
// sent in the body is never silently ignored.
type rollbackRequest struct {
	CancelPending bool `json:"cancel_pending"`
}

// rollbackReply is the 202 (new) or 200 (same request seen again) reply: the
// deployment view plus the version it rolls back to and, when an in-progress
// deployment will be cancelled, which one.
type rollbackReply struct {
	deploymentOut
	RollbackTargetSeq int `json:"rollback_target_seq"`
	SupersededSeq     int `json:"superseded_seq,omitempty"`
}

// rollback implements POST /v1/apps/{app}/rollback: roll back to the newest
// version that can be rolled back to (the app view's rollback_target_seq).
func (s *server) rollback(w http.ResponseWriter, r *http.Request) {
	s.handleRollback(w, r, 0)
}

// rollbackTo implements POST /v1/apps/{app}/versions/{seq}/rollback. It is a
// separate path, not a body field of the old route, so a server without this
// feature answers 404 instead of rolling back to some other version.
func (s *server) rollbackTo(w http.ResponseWriter, r *http.Request) {
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil || seq < 1 {
		writeError(w, http.StatusBadRequest, "invalid_version", "版本号不对：应为正整数（第几版）")
		return
	}
	s.handleRollback(w, r, seq)
}

// handleRollback checks and creates the rollback deployment in one store
// transaction, after asking the runner which images are actually on this
// machine (a runner failure falls back to the kept window alone).
func (s *server) handleRollback(w http.ResponseWriter, r *http.Request, toSeq int) {
	ctx := r.Context()
	app := r.PathValue("app")
	if !state.ValidAppName(app) {
		writeError(w, http.StatusBadRequest, "invalid_app", "应用名不合法")
		return
	}
	body, err := decodeRollbackBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_version", "请求体不合法：只接受空内容、{} 或 {\"cancel_pending\": true}")
		return
	}
	a, err := s.store.GetApp(ctx, app)
	if err != nil {
		s.mapStoreError(w, err)
		return
	}
	res, err := s.store.CreateRollbackDeployment(ctx, state.RollbackRequest{
		App:           app,
		ToSeq:         toSeq,
		Present:       s.presentImages(ctx, app),
		RequestKey:    r.Header.Get("Idempotency-Key"),
		CancelPending: body.CancelPending,
	})
	if err != nil {
		var rbErr *state.RollbackError
		switch {
		case errors.As(err, &rbErr):
			writeRollbackError(w, rbErr)
		case errors.Is(err, state.ErrInvalid):
			writeError(w, http.StatusBadRequest, "invalid_version", "版本号不对")
		default:
			s.mapStoreError(w, err)
		}
		return
	}
	if res.Created {
		s.kicker.Kick(app)
	}
	if a2, err := s.store.GetApp(ctx, app); err == nil {
		a = a2
	}
	reply := rollbackReply{
		deploymentOut:     s.newDeploymentViewer(ctx, a, []state.Deployment{res.Deployment}, false).view(res.Deployment),
		RollbackTargetSeq: res.TargetSeq,
		SupersededSeq:     res.SupersededSeq,
	}
	status := http.StatusAccepted
	if !res.Created {
		status = http.StatusOK
	}
	writeJSON(w, status, reply)
}

// decodeRollbackBody accepts an empty body, {} or {"cancel_pending":bool}.
func decodeRollbackBody(r *http.Request) (rollbackRequest, error) {
	var body rollbackRequest
	if r.Body == nil {
		return body, nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxRollbackBody+1))
	if err != nil {
		return body, err
	}
	if len(data) > maxRollbackBody {
		return body, errors.New("body too large")
	}
	if strings.TrimSpace(string(data)) == "" {
		return body, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return body, err
	}
	if dec.More() {
		return body, errors.New("trailing data")
	}
	return body, nil
}

// rollbackErrorBody extends the standard error body with the versions
// involved, so a page can offer "cancel version M and roll back" or list the
// alternatives without parsing the message.
type rollbackErrorBody struct {
	Error struct {
		Code         string `json:"code"`
		Message      string `json:"message"`
		Seq          int    `json:"seq,omitempty"`
		LiveSeq      int    `json:"live_seq,omitempty"`
		Alternatives []int  `json:"alternatives"`
	} `json:"error"`
}

// rollbackHTTPStatus maps a rollback refusal to its HTTP status.
func rollbackHTTPStatus(code string) int {
	if code == state.RollbackVersionNotFound {
		return http.StatusNotFound
	}
	return http.StatusConflict
}

func writeRollbackError(w http.ResponseWriter, e *state.RollbackError) {
	var body rollbackErrorBody
	body.Error.Code = e.Code
	body.Error.Message = e.Error()
	body.Error.Seq = e.Seq
	body.Error.LiveSeq = e.LiveSeq
	body.Error.Alternatives = e.Alternatives
	if body.Error.Alternatives == nil {
		body.Error.Alternatives = []int{}
	}
	writeJSON(w, rollbackHTTPStatus(e.Code), body)
}
