//go:build windows

package apiserver

import "context"

// On Windows Docker runs in a VM; "acornfox serve" supplies a HostProvider
// that reads the VM from the Docker API (host_dockervm.go). This fallback
// only reports that /proc is not available.
type procHostProvider struct{}

func newProcHostProvider(string) *procHostProvider { return &procHostProvider{} }

func (*procHostProvider) Host(context.Context) HostView { return HostView{Available: false} }
