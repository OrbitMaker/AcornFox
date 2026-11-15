// Package reconcile drives every app from its actual state (Docker, read via
// the runner; the entry proxy, through the edge.Proxy interface) to its
// desired state (state.Store).
package reconcile
