package apiserver

import "net/http"

// HostView is the wire shape of GET /v1/host. On any error the provider returns
// Available=false and the numeric fields are omitted.
type HostView struct {
	MemoryPercent *float64 `json:"memory_percent,omitempty"`
	DiskPercent   *float64 `json:"disk_percent,omitempty"`
	Available     bool     `json:"available"`
	CPUPercent    *float64 `json:"cpu_percent,omitempty"`
	MemoryUsed    *uint64  `json:"memory_used,omitempty"`
	MemoryTotal   *uint64  `json:"memory_total,omitempty"`
	DiskUsed      *uint64  `json:"disk_used,omitempty"`
	DiskTotal     *uint64  `json:"disk_total,omitempty"`
	Load1         *float64 `json:"load1,omitempty"`
	UptimeSeconds *float64 `json:"uptime_seconds,omitempty"`
	CPUCores      *int     `json:"cpu_cores,omitempty"` // logical cores available to the server process
	// Source is "docker_vm" on macOS and Windows, where the numbers describe
	// the Docker virtual machine (from the Docker API), not the computer.
	Source string `json:"source,omitempty"`
}

// getHost implements GET /v1/host.
func (s *server) getHost(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.hostMetrics.Host(r.Context()))
}
