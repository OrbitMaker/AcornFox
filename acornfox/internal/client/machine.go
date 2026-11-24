package client

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/acornfox/acornfox/internal/machine"
)

// Machine (GET /v1/machine): what the server can do right now (internal/
// machine). A server older than the route answers with its router's plain
// 404, which becomes server_too_old.

// MachineAPI is implemented by *Client (a separate interface so existing
// test fakes keep compiling).
type MachineAPI interface {
	Machine(ctx context.Context) (MachineView, error) // GET /v1/machine
}

// MachineView is GET /v1/machine: the view, and the raw answer for --json.
type MachineView struct {
	machine.View
	Raw json.RawMessage `json:"-"`
}

const machineWhat = "本机能力（machine）"

// Machine calls GET /v1/machine.
func (c *Client) Machine(ctx context.Context) (MachineView, error) {
	var v MachineView
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/machine", nil, &v.View)
	v.Raw = raw
	return v, tooOld(err, machineWhat)
}

var _ MachineAPI = (*Client)(nil)
