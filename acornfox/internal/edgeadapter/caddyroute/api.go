// Package caddyroute translates an edge.Config into Caddy's admin API. It
// runs inside the entry proxy container only: the adapter (cmd/afx-edge,
// package edgeadapter) uses it against the Caddy it supervises, over that
// Caddy's container-private admin socket.
// The adapter loads the whole configuration at once (Replace, Render).
// Sync, the older incremental way, is kept for its tests and the integration
// test against a real Caddy.
//
// AcornFox owns only the HTTP servers named "af-<app>", the shared
// "af-domains" server, its "af-tls-policy" TLS policy and the temporary
// "acornfox-verify" server; every other part of the Caddy configuration is
// left untouched.
//
// The acornfox program never links this package: it talks to the adapter
// through edge.Proxy (package edgeclient). A guard test in package edge keeps
// it that way.
package caddyroute

import "github.com/acornfox/acornfox/internal/edge"

// Route is one route of the configuration (see edge.Route). A route with
// PublicPort 0 is domain-only: it gets no per-app server and is served solely
// through the shared "af-domains" server (used for the public console).
type Route = edge.Route

// HTTPSConfig configures the shared "af-domains" server (see edge.HTTPS).
// Domains of all routes are host-matched there; Caddy obtains certificates
// automatically.
type HTTPSConfig = edge.HTTPS

// The Caddy driver implements the whole entry proxy interface.
var _ edge.Proxy = (*Driver)(nil)
