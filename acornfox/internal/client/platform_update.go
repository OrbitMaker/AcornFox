package client

import (
	"context"
	"net/http"

	"github.com/acornfox/acornfox/internal/platformupdate"
)

// Platform new-version reminder (GET /v1/platform/update, POST
// /v1/platform/update/check; the same object is GET /v1/status's
// platform_update). A server older than these routes answers with its
// router's plain 404/405: every method turns that into server_too_old.

// PlatformUpdate is the object of the new-version check.
type PlatformUpdate = platformupdate.Status

// PlatformUpdateAPI is implemented by *Client (a separate interface so the
// existing test fakes keep compiling).
type PlatformUpdateAPI interface {
	PlatformUpdate(ctx context.Context) (PlatformUpdate, error)      // GET  /v1/platform/update
	CheckPlatformUpdate(ctx context.Context) (PlatformUpdate, error) // POST /v1/platform/update/check
}

const platformUpdateWhat = "平台新版本提醒"

// PlatformUpdate calls GET /v1/platform/update.
func (c *Client) PlatformUpdate(ctx context.Context) (PlatformUpdate, error) {
	var v PlatformUpdate
	_, err := c.doJSON(ctx, http.MethodGet, "/v1/platform/update", nil, &v)
	return v, tooOld(err, platformUpdateWhat)
}

// CheckPlatformUpdate calls POST /v1/platform/update/check.
func (c *Client) CheckPlatformUpdate(ctx context.Context) (PlatformUpdate, error) {
	var v PlatformUpdate
	_, err := c.doJSON(ctx, http.MethodPost, "/v1/platform/update/check", nil, &v)
	return v, tooOld(err, platformUpdateWhat)
}

var _ PlatformUpdateAPI = (*Client)(nil)
