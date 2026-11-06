//go:build !acornfox_maclocal

package setup

// MacLocalEnabled turns on the Mac local mode (install, serve and the
// built-in runtime on the Mac). Off in this release; see maclocal.go.
const MacLocalEnabled = false
