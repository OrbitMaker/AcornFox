// Package edgeclient is the acornfox program's driver of the entry proxy
// interface (package edge): it talks to the adapter inside the entry proxy
// container over the adapter's own management endpoint (the protocol in
// edge/wire.go). It knows nothing about the proxy software behind the
// adapter; translating the configuration happens inside the container.
//
// The management endpoint belongs to one container only. It is not the
// loopback gateway older installs used, so while an old and a new entry
// container run side by side the program reaches exactly the one it means.
package edgeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
)

// Timeouts of one call. Apply may rebuild many listeners, the others are
// small reads.
const (
	applyTimeout = 60 * time.Second
	callTimeout  = 10 * time.Second
)

// Driver implements edge.Proxy against the adapter at base
// ("http://127.0.0.1:<port>").
type Driver struct {
	base   atomic.Pointer[string]
	token  string
	client *http.Client
}

var _ edge.Proxy = (*Driver)(nil)

// New returns a driver for the adapter's management endpoint at base with the
// management token.
func New(base, token string) *Driver {
	d := &Driver{token: token, client: &http.Client{}}
	d.SetBase(base)
	return d
}

// SetBase points the driver at another management endpoint. The runner
// moves a new entry container to the other port of a pair when the old one
// still holds the usual one (switching between two adapter versions); the
// reconciler then calls this with the port the runner reports.
func (d *Driver) SetBase(base string) {
	b := strings.TrimRight(base, "/")
	d.base.Store(&b)
}

// Base is the management endpoint in use.
func (d *Driver) Base() string { return *d.base.Load() }

// Apply implements edge.Proxy.
func (d *Driver) Apply(ctx context.Context, cfg edge.Config) (string, error) {
	var out edge.VersionReply
	if err := d.call(ctx, applyTimeout, http.MethodPut, edge.PathConfig, cfg, &out); err != nil {
		return "", fmt.Errorf("入口代理没有接受设置：%w", err)
	}
	if out.Version == "" {
		return "", errors.New("入口代理接受了设置，但没有返回版本号")
	}
	return out.Version, nil
}

// Version implements edge.Proxy.
func (d *Driver) Version(ctx context.Context) (string, error) {
	var out edge.VersionReply
	if err := d.call(ctx, callTimeout, http.MethodGet, edge.PathVersion, nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// SetChallenge implements edge.Challenger.
func (d *Driver) SetChallenge(ctx context.Context, host, path, body string) error {
	return d.call(ctx, callTimeout, http.MethodPut, edge.PathChallenge, edge.ChallengeRequest{Host: host, Path: path, Body: body}, nil)
}

// ClearChallenge implements edge.Challenger.
func (d *Driver) ClearChallenge(ctx context.Context) error {
	return d.call(ctx, callTimeout, http.MethodDelete, edge.PathChallenge, nil, nil)
}

// Health implements edge.Proxy. It first makes sure the management endpoint
// refuses a request without the token (contract 7), then asks the adapter
// with the token for its own view.
func (d *Driver) Health(ctx context.Context) edge.Health {
	status, _, err := d.raw(ctx, callTimeout, http.MethodGet, edge.PathHealth, nil, false)
	if err != nil {
		return edge.Health{State: edge.StateError, Cause: edge.CauseUnreachable, Reason: "入口代理的管理口没有应答（故障编号 " + edge.FaultAdmin + "）：" + err.Error()}
	}
	if status != http.StatusUnauthorized {
		return edge.Health{State: edge.StateError, Cause: edge.CauseExposed, Reason: fmt.Sprintf("入口代理的管理口不带令牌也应答了（%d，应为 401）", status)}
	}
	status, body, err := d.raw(ctx, callTimeout, http.MethodGet, edge.PathHealth, nil, true)
	switch {
	case err != nil:
		return edge.Health{State: edge.StateError, Cause: edge.CauseUnreachable, Reason: "入口代理的管理口没有应答（故障编号 " + edge.FaultAdmin + "）：" + err.Error()}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return edge.Health{State: edge.StateError, Cause: edge.CauseAuth, Reason: fmt.Sprintf("入口代理的管理口拒绝了令牌（%d，故障编号 %s）", status, edge.FaultAdmin)}
	case status != http.StatusOK:
		return edge.Health{State: edge.StateError, Cause: edge.CauseOther, Reason: fmt.Sprintf("入口代理的管理口返回 %d：%s", status, errorText(body))}
	}
	var h edge.Health
	if err := json.Unmarshal(body, &h); err != nil || h.State == "" {
		return edge.Health{State: edge.StateError, Cause: edge.CauseOther, Reason: "入口代理的健康结果看不懂"}
	}
	return h
}

// call sends one authenticated request and decodes a 2xx JSON answer into out
// (when out is non-nil).
func (d *Driver) call(ctx context.Context, timeout time.Duration, method, path string, in, out any) error {
	var payload []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		payload = b
	}
	status, body, err := d.raw(ctx, timeout, method, path, payload, true)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("入口代理的管理口拒绝了令牌（%d，故障编号 %s）", status, edge.FaultAdmin)
	}
	if status < 200 || status >= 300 {
		return &StatusError{Status: status, Message: errorText(body)}
	}
	if out != nil && len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("入口代理的应答看不懂：%w", err)
		}
	}
	return nil
}

func (d *Driver) raw(ctx context.Context, timeout time.Duration, method, path string, payload []byte, withToken bool) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.Base()+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if withToken {
		req.Header.Set(edge.TokenHeader, d.token)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// StatusError is a non-2xx answer of the adapter.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("入口代理返回 %d", e.Status)
	}
	return fmt.Sprintf("入口代理返回 %d：%s", e.Status, e.Message)
}

func errorText(body []byte) string {
	var er edge.ErrorReply
	if json.Unmarshal(body, &er) == nil && er.Error != "" {
		return er.Error
	}
	t := strings.TrimSpace(string(body))
	if len(t) > 200 {
		t = t[:200]
	}
	return t
}
