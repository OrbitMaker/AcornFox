package client

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/acornfox/acornfox/internal/runner"
)

// OtherContainers (GET /v1/host/containers): the containers on the server
// that AcornFox does not manage, read-only. A server older than the route
// answers with its router's plain 404, which becomes server_too_old.

// OtherContainersAPI is implemented by *Client (a separate interface so
// existing test fakes keep compiling).
type OtherContainersAPI interface {
	OtherContainers(ctx context.Context) (OtherContainersView, error) // GET /v1/host/containers
}

// OtherContainersView is GET /v1/host/containers, and the raw answer for --json.
type OtherContainersView struct {
	ObservedAt string                  `json:"observed_at"`
	Containers []runner.OtherContainer `json:"containers"`
	Running    int                     `json:"running"`
	Stopped    int                     `json:"stopped"`
	MemoryMB   float64                 `json:"memory_mb"`
	CPUPercent float64                 `json:"cpu_percent"`
	Truncated  bool                    `json:"truncated,omitempty"`
	Raw        json.RawMessage         `json:"-"`
}

const otherContainersWhat = "这台机器上的其他容器（other-containers）"

// OtherContainers calls GET /v1/host/containers?fresh=1 (a command line asks
// for the state now, not the one the pages share).
func (c *Client) OtherContainers(ctx context.Context) (OtherContainersView, error) {
	var v OtherContainersView
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/host/containers?fresh=1", nil, &v)
	v.Raw = raw
	return v, tooOld(err, otherContainersWhat)
}

var _ OtherContainersAPI = (*Client)(nil)
