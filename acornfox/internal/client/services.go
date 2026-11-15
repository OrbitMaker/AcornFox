package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// Services (GET/POST/PATCH/DELETE /v1/services…): what the server can offer
// its apps. A server older than these routes answers with its router's plain
// 404/405; every method turns that into server_too_old ("服务器上的 AcornFox
// 版本较旧，不支持“服务”"). The server's own errors (CAT-SVC-*, not_found)
// pass through unchanged.

// ServicesAPI is implemented by *Client (a separate interface so existing
// test fakes keep compiling).
type ServicesAPI interface {
	ServiceKinds(ctx context.Context) (ServiceKinds, error)                                   // GET    /v1/services/kinds
	Services(ctx context.Context) (ServiceList, error)                                        // GET    /v1/services
	Service(ctx context.Context, name string) (Service, error)                                // GET    /v1/services/{name}
	AddService(ctx context.Context, req AddServiceRequest) (Service, error)                   // POST   /v1/services
	PatchService(ctx context.Context, name string, values map[string]string) (Service, error) // PATCH  /v1/services/{name}
	TestService(ctx context.Context, name string) (ServiceTest, error)                        // POST   /v1/services/{name}/test
	RemoveService(ctx context.Context, name string) ([]string, error)                         // DELETE /v1/services/{name}: the notes
}

// ServiceKinds mirrors GET /v1/services/kinds.
type ServiceKinds struct {
	Kinds []ServiceKind   `json:"kinds"`
	Raw   json.RawMessage `json:"-"`
}

// ServiceKind is one service type.
type ServiceKind struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Summary string `json:"summary"`
	Fields  []struct {
		Key      string   `json:"key"`
		Label    string   `json:"label"`
		Type     string   `json:"type"`
		Required bool     `json:"required"`
		Secret   bool     `json:"secret"`
		Default  string   `json:"default"`
		Options  []string `json:"options"`
		Hint     string   `json:"hint"`
	} `json:"fields"`
	HasTest bool `json:"has_test"`
}

// ServiceList mirrors GET /v1/services.
type ServiceList struct {
	Services   []Service       `json:"services"`
	Unreadable []string        `json:"unreadable,omitempty"`
	Raw        json.RawMessage `json:"-"`
}

// Service mirrors one service (GET /v1/services/{name}, and the answer to
// POST and PATCH; Notes only from PATCH). Secret values are masked by the
// server.
type Service struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	KindLabel   string   `json:"kind_label"`
	Source      string   `json:"source"` // user | app
	App         string   `json:"app"`
	Status      string   `json:"status"` // ok | incomplete | provider_stopped | provider_gone | not_ready
	StatusLabel string   `json:"status_label"`
	Missing     []string `json:"missing"`
	Values      []struct {
		Key    string `json:"key"`
		Label  string `json:"label"`
		Value  string `json:"value"`
		Secret bool   `json:"secret"`
		Set    bool   `json:"set"`
		Ask    string `json:"ask"`
	} `json:"values"`
	IssuedPerConsumer []string `json:"issued_per_consumer"`
	UsedBy            []struct {
		App string `json:"app"`
		Use string `json:"use"`
		// Gone: deleted with its data kept; it does not keep the service
		// from being removed (its binding goes with it).
		Gone bool `json:"gone,omitempty"`
		// Dedicated: this need holds the service for itself.
		Dedicated bool `json:"dedicated,omitempty"`
	} `json:"used_by"`
	// DedicatedTo: the app holding the service for itself ("" none).
	DedicatedTo string `json:"dedicated_to"`
	// MaxConsumers: how many apps may use it at once (0: no limit).
	MaxConsumers int `json:"max_consumers"`
	// SlotsLeft: how many more apps can be plugged in now (nil: no limit).
	SlotsLeft *int `json:"slots_left"`
	// ReadySay, ReadyDetail: status not_ready, the provider's sentence and
	// what failed.
	ReadySay    string          `json:"ready_say,omitempty"`
	ReadyDetail string          `json:"ready_detail,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Notes       []string        `json:"notes,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// AddServiceRequest mirrors POST /v1/services; every value is text.
type AddServiceRequest struct {
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Values map[string]string `json:"values"`
}

// ServiceTest mirrors POST /v1/services/{name}/test. OK: got through;
// Skipped: nothing could be tried (Detail says why); Code is CAT-SVC-06 when
// it was tried and did not get through.
type ServiceTest struct {
	OK      bool            `json:"ok"`
	Detail  string          `json:"detail"`
	Code    string          `json:"code,omitempty"`
	Target  string          `json:"target,omitempty"`
	Skipped bool            `json:"skipped,omitempty"`
	Raw     json.RawMessage `json:"-"`
}

const servicesWhat = "服务"

func servicePath(name string) string { return "/v1/services/" + url.PathEscape(name) }

// ServiceKinds calls GET /v1/services/kinds.
func (c *Client) ServiceKinds(ctx context.Context) (ServiceKinds, error) {
	var v ServiceKinds
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/services/kinds", nil, &v)
	v.Raw = raw
	return v, tooOld(err, servicesWhat)
}

// Services calls GET /v1/services.
func (c *Client) Services(ctx context.Context) (ServiceList, error) {
	var v ServiceList
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/services", nil, &v)
	v.Raw = raw
	return v, tooOld(err, servicesWhat)
}

// Service calls GET /v1/services/{name}.
func (c *Client) Service(ctx context.Context, name string) (Service, error) {
	var v Service
	raw, err := c.getRaw(ctx, http.MethodGet, servicePath(name), nil, &v)
	v.Raw = raw
	return v, tooOld(err, servicesWhat)
}

// AddService calls POST /v1/services.
func (c *Client) AddService(ctx context.Context, req AddServiceRequest) (Service, error) {
	if req.Values == nil {
		req.Values = map[string]string{}
	}
	var v Service
	raw, err := c.getRaw(ctx, http.MethodPost, "/v1/services", req, &v)
	v.Raw = raw
	return v, tooOld(err, servicesWhat)
}

// PatchService calls PATCH /v1/services/{name}; "" clears an item.
func (c *Client) PatchService(ctx context.Context, name string, values map[string]string) (Service, error) {
	var v Service
	raw, err := c.getRaw(ctx, http.MethodPatch, servicePath(name), map[string]any{"values": values}, &v)
	v.Raw = raw
	return v, tooOld(err, servicesWhat)
}

// TestService calls POST /v1/services/{name}/test.
func (c *Client) TestService(ctx context.Context, name string) (ServiceTest, error) {
	var v ServiceTest
	raw, err := c.getRaw(ctx, http.MethodPost, servicePath(name)+"/test", struct{}{}, &v)
	v.Raw = raw
	return v, tooOld(err, servicesWhat)
}

// RemoveService calls DELETE /v1/services/{name} and returns what the
// server says about it (apps deleted earlier whose bindings went with it);
// none from a 204.
func (c *Client) RemoveService(ctx context.Context, name string) ([]string, error) {
	var out struct {
		Notes []string `json:"notes"`
	}
	if _, err := c.doJSON(ctx, http.MethodDelete, servicePath(name), nil, &out); err != nil {
		return nil, tooOld(err, servicesWhat)
	}
	return out.Notes, nil
}

var _ ServicesAPI = (*Client)(nil)
