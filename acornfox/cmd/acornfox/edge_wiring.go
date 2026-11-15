package main

// The one place the program picks the entry proxy driver. Everything else
// (the reconciler, the public console, the self checks) sees only
// edge.Proxy, so a different driver changes this file only.

import (
	"fmt"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/edgeclient"
)

// newEdgeProxy returns the entry proxy for o: the adapter in the entry proxy
// container, on its own management endpoint 127.0.0.1:o.EdgeAdminPort, and
// the function that points it at another port (the runner reports one after
// switching between two entry containers).
func newEdgeProxy(o serverOptions, token string) (edge.Proxy, func(port int)) {
	d := edgeclient.New(edgeAdminURL(o.EdgeAdminPort), token)
	return d, func(port int) { d.SetBase(edgeAdminURL(port)) }
}

func edgeAdminURL(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }
