package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/acornfox/acornfox/internal/faults"
	"github.com/acornfox/acornfox/internal/publicconsole"
)

// PublicConsole is what the API needs from the public console;
// *publicconsole.Manager implements it.
type PublicConsole interface {
	Status(ctx context.Context) (publicconsole.Status, error)
	StartVerify(ctx context.Context, domain string) (publicconsole.VerifyInfo, error)
	// VerifyHere starts a verification and checks it from the server itself
	// (the console page has no command line on the user's side to do it).
	VerifyHere(ctx context.Context, domain string) (publicconsole.VerifyInfo, publicconsole.Diagnosis, error)
	Enable(ctx context.Context, domain, verifyID string, resetPassword bool) (publicconsole.EnableResult, error)
	Disable(ctx context.Context) (bool, error)
}

// Who may open and close the public console, and what they are asked
// (写死在这里，不靠页面自觉):
//
//   - the trusted entry (the unix socket: the command line on the server or
//     over SSH): everything, no password, unchanged;
//   - a browser that logged in with `acornfox open` (one-time link over the
//     SSH tunnel, straight on the loopback console listener): open and
//     close, no password (whoever can do that can run commands on the
//     server anyway);
//   - a browser that logged in on the LAN console: open and close after
//     typing the LAN console password again;
//   - a browser that logged in on the public console itself: close only,
//     after typing the public console password again (changing the address
//     from there would end the very session doing it).
//
// Wrong passwords count towards the same lockout as that entry's login page
// (PUBCON-02). A machine on a local network (no public address) cannot open
// it from the page (PUBCON-03); closing always works. Opening always makes
// a new password, returned once in "password" and never again.

// PUBCON fault codes raised here.
const (
	codePubconPassword = "PUBCON-01" // console password missing or wrong
	codePubconLocked   = "PUBCON-02" // too many wrong passwords, wait
	codePubconLAN      = "PUBCON-03" // a local-network machine cannot open it
	codePubconFromSelf = "PUBCON-04" // logged in on the public console: close only
)

// Entry names in GET /v1/public-console "entry".
const (
	consoleEntryServer = "server" // trusted entry
	consoleEntryTunnel = "tunnel" // acornfox open over SSH
)

// publicConsoleView is GET /v1/public-console: the state plus what this
// login may do.
type publicConsoleView struct {
	publicconsole.Status
	// Entry is how the caller reached the API: server, tunnel, lan, public.
	Entry string `json:"entry"`
	// CanToggle: this login may open or close it on the page (for "public",
	// close only).
	CanToggle bool `json:"can_toggle"`
	// NeedsPassword: opening and closing ask for the console password again.
	NeedsPassword bool `json:"needs_password"`
	// SuggestedDomain is console.<platform domain>, "" without one.
	SuggestedDomain string           `json:"suggested_domain"`
	Unsupported     *unsupportedView `json:"unsupported,omitempty"`
}

type unsupportedView struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// pubconErrorBody is an error with the fault's next step as hint.
type pubconErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint,omitempty"`
	} `json:"error"`
}

func writePubconError(w http.ResponseWriter, status int, code, message, hint string) {
	var b pubconErrorBody
	b.Error.Code, b.Error.Message, b.Error.Hint = code, message, hint
	if b.Error.Hint == "" {
		if f, ok := faults.Find(code); ok {
			b.Error.Hint = f.Next
		}
	}
	writeJSON(w, status, b)
}

// consoleEntry names the way the request reached the API and returns the
// gateway entry (nil unless lan or public).
func consoleEntry(ctx context.Context) (string, *publicconsole.Entry) {
	if _, ok := sessionFromContext(ctx); !ok {
		return consoleEntryServer, nil
	}
	if e := publicconsole.EntryFrom(ctx); e != nil {
		return e.Kind, e
	}
	return consoleEntryTunnel, nil
}

// publicConsoleUnsupported is set on a machine on a local network: it has
// no public address, so a domain cannot reach it.
func (s *server) publicConsoleUnsupported() *unsupportedView {
	if s.serverLAN == nil || s.serverLAN() == "" {
		return nil
	}
	return &unsupportedView{Code: codePubconLAN, Text: "这台机器在局域网里，没有公网地址，开不了公网控制台"}
}

func (s *server) suggestedConsoleDomain(ctx context.Context) string {
	if s.publicConsoleBase == nil {
		return ""
	}
	if base := s.publicConsoleBase(ctx); base != "" {
		if d, ok := publicconsole.NormalizeDomain("console." + base); ok {
			return d
		}
	}
	return ""
}

// checkConsolePassword asks a gateway login for its console password again.
// It writes the refusal and returns false when the password is missing or
// wrong or the client is locked out; logins without a password (tunnel,
// trusted) pass. The password is never logged or echoed.
func (s *server) checkConsolePassword(w http.ResponseWriter, entry *publicconsole.Entry, password string) bool {
	if entry == nil {
		return true
	}
	if password == "" {
		writePubconError(w, http.StatusForbidden, codePubconPassword, "要再输一次现在的控制台密码", "")
		return false
	}
	ok, wait := entry.CheckPassword(password)
	if wait > 0 {
		mins := int((wait + time.Minute - time.Nanosecond) / time.Minute)
		writePubconError(w, http.StatusTooManyRequests, codePubconLocked, fmt.Sprintf("密码试错次数太多，请 %d 分钟后再试", mins), "")
		return false
	}
	if !ok {
		writePubconError(w, http.StatusForbidden, codePubconPassword, "控制台密码不对", "")
		return false
	}
	return true
}

func (s *server) publicConsoleUnavailable(w http.ResponseWriter) bool {
	if s.publicConsole == nil {
		writeError(w, http.StatusNotFound, "not_found", "该服务端不支持公网控制台")
		return true
	}
	return false
}

func (s *server) writePublicConsoleError(w http.ResponseWriter, err error) {
	var pe *publicconsole.Error
	if errors.As(err, &pe) {
		writeError(w, pe.Status, pe.Code, pe.Message)
		return
	}
	s.log.Error("public console", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "内部错误")
}

// publicConsoleStatus implements GET /v1/public-console (trusted and console
// entries): the state, and what this login may do on the page.
func (s *server) publicConsoleStatus(w http.ResponseWriter, r *http.Request) {
	if s.publicConsoleUnavailable(w) {
		return
	}
	ctx := r.Context()
	st, err := s.publicConsole.Status(ctx)
	if err != nil {
		s.writePublicConsoleError(w, err)
		return
	}
	kind, entry := consoleEntry(ctx)
	writeJSON(w, http.StatusOK, publicConsoleView{
		Status:          st,
		Entry:           kind,
		CanToggle:       true,
		NeedsPassword:   entry != nil,
		SuggestedDomain: s.suggestedConsoleDomain(ctx),
		Unsupported:     s.publicConsoleUnsupported(),
	})
}

// consolePublicConsoleEnable implements POST /v1/public-console/enable on
// the console entry, {"domain","password"}: check the password of the entry
// this browser logged in through, check the domain from the server (DNS,
// then the HTTP challenge on port 80) and turn it on with a new password,
// returned once in "password". When it is already on for that domain nothing
// changes ("already_enabled", no password).
func (s *server) consolePublicConsoleEnable(w http.ResponseWriter, r *http.Request) {
	if s.publicConsoleUnavailable(w) {
		return
	}
	var req struct {
		Domain   string `json:"domain"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法的 JSON")
		return
	}
	ctx := r.Context()
	kind, entry := consoleEntry(ctx)
	if kind == publicconsole.EntryPublic {
		writePubconError(w, http.StatusForbidden, codePubconFromSelf, "现在是从公网控制台登录的，这里只能关闭公网控制台，不能开启或换地址", "")
		return
	}
	if u := s.publicConsoleUnsupported(); u != nil {
		writePubconError(w, http.StatusConflict, u.Code, u.Text, "")
		return
	}
	if !s.checkConsolePassword(w, entry, req.Password) {
		return
	}
	domain, ok := publicconsole.NormalizeDomain(req.Domain)
	if !ok {
		s.writePublicConsoleError(w, &publicconsole.Error{Status: http.StatusBadRequest, Code: "invalid_domain",
			Message: fmt.Sprintf("域名 %q 不合法：请填写完整的域名，例如 panel.example.com（不要带 http:// 或端口）", req.Domain)})
		return
	}
	verifyID := ""
	if st, err := s.publicConsole.Status(ctx); err != nil {
		s.writePublicConsoleError(w, err)
		return
	} else if !st.Enabled || st.Domain != domain {
		info, diag, err := s.publicConsole.VerifyHere(ctx, domain)
		if err != nil {
			s.writePublicConsoleError(w, err)
			return
		}
		if !diag.OK() {
			writePubconError(w, http.StatusConflict, diag.Code, diag.Message, diag.Hint)
			return
		}
		verifyID = info.VerifyID
	}
	res, err := s.publicConsole.Enable(ctx, domain, verifyID, false)
	if err != nil {
		s.writePublicConsoleError(w, err)
		return
	}
	client := ""
	if entry != nil {
		client = entry.Client
	}
	s.log.Info("public console: opened from the console page", "entry", kind, "client", client, "domain", res.Domain, "already_enabled", res.AlreadyEnabled)
	writeJSON(w, http.StatusOK, res)
}

// consolePublicConsoleDisable implements DELETE /v1/public-console on the
// console entry, {"password"} (no body needed after acornfox open). Closing
// is allowed from every login, the public console itself included, and on a
// local-network machine too.
func (s *server) consolePublicConsoleDisable(w http.ResponseWriter, r *http.Request) {
	if s.publicConsoleUnavailable(w) {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法的 JSON")
		return
	}
	ctx := r.Context()
	kind, entry := consoleEntry(ctx)
	if !s.checkConsolePassword(w, entry, req.Password) {
		return
	}
	was, err := s.publicConsole.Disable(ctx)
	if err != nil {
		s.writePublicConsoleError(w, err)
		return
	}
	client := ""
	if entry != nil {
		client = entry.Client
	}
	s.log.Info("public console: closed from the console page", "entry", kind, "client", client, "was_enabled", was)
	writeJSON(w, http.StatusOK, map[string]any{"disabled": true, "was_enabled": was})
}

// publicConsoleVerify implements POST /v1/public-console/verify {"domain"}.
func (s *server) publicConsoleVerify(w http.ResponseWriter, r *http.Request) {
	if s.publicConsoleUnavailable(w) {
		return
	}
	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法的 JSON")
		return
	}
	info, err := s.publicConsole.StartVerify(r.Context(), req.Domain)
	if err != nil {
		s.writePublicConsoleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// publicConsoleEnable implements POST /v1/public-console/enable
// {"domain","verify_id","reset_password"}. The reply carries the password
// once; it is never stored in clear or returned again.
func (s *server) publicConsoleEnable(w http.ResponseWriter, r *http.Request) {
	if s.publicConsoleUnavailable(w) {
		return
	}
	var req struct {
		Domain        string `json:"domain"`
		VerifyID      string `json:"verify_id"`
		ResetPassword bool   `json:"reset_password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法的 JSON")
		return
	}
	res, err := s.publicConsole.Enable(r.Context(), req.Domain, req.VerifyID, req.ResetPassword)
	if err != nil {
		s.writePublicConsoleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// publicConsoleDisable implements DELETE /v1/public-console.
func (s *server) publicConsoleDisable(w http.ResponseWriter, r *http.Request) {
	if s.publicConsoleUnavailable(w) {
		return
	}
	was, err := s.publicConsole.Disable(r.Context())
	if err != nil {
		s.writePublicConsoleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disabled": true, "was_enabled": was})
}

// publicConsoleDomain returns the domain the public console uses, or "".
func (s *server) publicConsoleDomain(ctx context.Context) string {
	if s.publicConsole == nil {
		return ""
	}
	st, err := s.publicConsole.Status(ctx)
	if err != nil || !st.Enabled {
		return ""
	}
	return st.Domain
}
