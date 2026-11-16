package client

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Platform domain (GET/PUT/DELETE /v1/platform/domain, POST
// /v1/platform/domain/check): one domain for the whole machine; public apps
// also open at https://<app>.<platform domain>. A server older than these
// routes answers with its router's plain 404/405: every method turns that
// into server_too_old. The server's own errors (PDOMAIN-*) pass through.

// PlatformDomainAPI is implemented by *Client (a separate interface so the
// existing test fakes keep compiling).
type PlatformDomainAPI interface {
	PlatformDomain(ctx context.Context) (PlatformDomain, error)                                       // GET    /v1/platform/domain
	CheckPlatformDomain(ctx context.Context) (PlatformDomain, error)                                  // POST   /v1/platform/domain/check
	SetPlatformDomain(ctx context.Context, domain string, confirm, plan bool) (PlatformDomain, error) // PUT    /v1/platform/domain
	ClearPlatformDomain(ctx context.Context, confirm, plan bool) (PlatformDomain, error)              // DELETE /v1/platform/domain
}

// PlatformDomain mirrors every answer of the platform domain routes.
type PlatformDomain struct {
	Domain      string          `json:"domain"`
	State       string          `json:"state"` // unset | ok | problem
	Supported   bool            `json:"supported"`
	Unsupported *PDomainProblem `json:"unsupported,omitempty"`
	PublicIP    string          `json:"public_ip,omitempty"`
	Record      PDomainRecord   `json:"record"`
	Checks      []PDomainCheck  `json:"checks"`
	Problem     *PDomainProblem `json:"problem,omitempty"`
	ConsoleHost string          `json:"console_host,omitempty"`
	SetAt       *time.Time      `json:"set_at,omitempty"`
	CheckedAt   *time.Time      `json:"checked_at,omitempty"`
	Apps        []PDomainApp    `json:"apps"`
	Plan        *PDomainPlan    `json:"plan,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// PDomainRecord is the DNS record to add at the domain provider.
type PDomainRecord struct {
	Name  string `json:"name,omitempty"`
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
	Full  string `json:"full,omitempty"`
}

// PDomainCheck is one of the checks dns, ports, cert.
type PDomainCheck struct {
	Key     string `json:"key"`
	OK      bool   `json:"ok"`
	Waiting bool   `json:"waiting,omitempty"`
	Text    string `json:"text"`
	Code    string `json:"code,omitempty"`
}

// PDomainProblem is a fault in words.
type PDomainProblem struct {
	Code   string `json:"code"`
	Text   string `json:"text"`
	Detail string `json:"detail,omitempty"`
	Next   string `json:"next,omitempty"`
}

// PDomainApp is one app's name under the platform domain.
type PDomainApp struct {
	App      string `json:"app"`
	Host     string `json:"host"`
	Active   bool   `json:"active"`
	Note     string `json:"note,omitempty"`
	URL      string `json:"url"`
	URLAfter string `json:"url_after,omitempty"`
}

// PDomainPlan is what a set or a clear would do.
type PDomainPlan struct {
	Change       string `json:"change"` // set | replace | clear | none
	From         string `json:"from"`
	To           string `json:"to"`
	NeedsConfirm bool   `json:"needs_confirm"`
	Say          string `json:"say"`
}

const platformDomainWhat = "平台域名"

func (c *Client) platformDomainCall(ctx context.Context, method, path string, body any) (PlatformDomain, error) {
	var v PlatformDomain
	raw, err := c.getRaw(ctx, method, path, body, &v)
	v.Raw = raw
	return v, tooOld(err, platformDomainWhat)
}

// PlatformDomain calls GET /v1/platform/domain.
func (c *Client) PlatformDomain(ctx context.Context) (PlatformDomain, error) {
	return c.platformDomainCall(ctx, http.MethodGet, "/v1/platform/domain", nil)
}

// CheckPlatformDomain calls POST /v1/platform/domain/check.
func (c *Client) CheckPlatformDomain(ctx context.Context) (PlatformDomain, error) {
	return c.platformDomainCall(ctx, http.MethodPost, "/v1/platform/domain/check", nil)
}

// SetPlatformDomain calls PUT /v1/platform/domain.
func (c *Client) SetPlatformDomain(ctx context.Context, domain string, confirm, plan bool) (PlatformDomain, error) {
	return c.platformDomainCall(ctx, http.MethodPut, "/v1/platform/domain", map[string]any{"domain": domain, "confirm": confirm, "plan": plan})
}

// ClearPlatformDomain calls DELETE /v1/platform/domain.
func (c *Client) ClearPlatformDomain(ctx context.Context, confirm, plan bool) (PlatformDomain, error) {
	return c.platformDomainCall(ctx, http.MethodDelete, "/v1/platform/domain", map[string]any{"confirm": confirm, "plan": plan})
}

var _ PlatformDomainAPI = (*Client)(nil)
