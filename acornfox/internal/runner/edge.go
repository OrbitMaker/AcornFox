package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// The edge is the one Caddy container in front of every app (three-platform
// architecture, 2026-11-01). It replaces the Caddy that v0.3.x installed on
// the Linux host. acornfox manages it like an app container but it is not an
// app: it carries LabelEdge instead of LabelManaged, so app listings, orphan
// sweeps and per-app cleanup never see it.
//
// Two network modes:
//
//   - host (Linux): Caddy shares the host network, listens on the apps'
//     public ports and 80/443 directly and proxies to the containers' loopback
//     ports, exactly like the host Caddy did. Publishing 90 app ports through
//     docker-proxy would cost a process per port, and a published port list
//     cannot change without recreating the container.
//   - bridge (macOS, Windows): Docker runs in a VM, so Caddy sits on the
//     "acornfox-edge" network, joins each app's network and proxies to the
//     app container by name. The whole app port range is published once
//     (reserved ports), twice: 127.0.0.1:P -> P and
//     <LAN address>:P -> P+10000. An app reachable only locally gets a Caddy
//     server on P; a LAN app on P and P+10000. Adding or removing apps and
//     changing exposure are route changes only; the container is recreated
//     only when the LAN address (or the range) changes. Docker Desktop does
//     not keep the client address, so Caddy cannot tell local from LAN
//     traffic by source; the bind address does it instead.
//
// Caddy's admin API listens on a unix socket inside the container only.
// acornfox reaches it through a small Caddy site on a loopback port that
// requires the X-AcornFox-Token header and proxies to that socket.
const (
	EdgeContainerName = "acornfox-caddy"
	EdgeNetwork       = "acornfox-edge"
	LabelEdge         = "acornfox.edge"      // "1" on the edge container and network
	LabelEdgeHash     = "acornfox.edge.hash" // hash of the spec the container was created from

	// EdgeImageLocal is built by install.sh from the Caddy release archive
	// (docker import, no registry needed); EdgeImagePublic is pulled when the
	// local image is absent (macOS, Windows, or Linux without the archive).
	EdgeImageLocal  = "acornfox/caddy:2.8.4"
	EdgeImagePublic = "caddy:2.8.4-alpine"

	EdgeDataVolume   = "acornfox-caddy-data"   // certificates, ACME accounts (/data)
	EdgeConfigVolume = "acornfox-caddy-config" // Caddy's autosave dir (/config)

	EdgeModeHost   = "host"
	EdgeModeBridge = "bridge"

	// edgeAdminContainerPort is the admin gateway port inside the container in
	// bridge mode; host mode listens on AdminPort directly.
	edgeAdminContainerPort = 2020
	EdgeTokenHeader        = "X-AcornFox-Token"

	PathEdgeEnsure  = "/v1/edge/ensure"  // POST EdgeSpec -> EdgeInfo
	PathEdgeConnect = "/v1/edge/connect" // POST AppRef -> OK
)

// EdgePort is one published port of the edge container (bridge mode).
type EdgePort struct {
	HostIP        string `json:"host_ip"`                  // "127.0.0.1", "0.0.0.0" or a private IPv4 (the LAN address)
	Port          int    `json:"port"`                     // host port
	ContainerPort int    `json:"container_port,omitempty"` // port inside the container; 0 = same as Port
}

func (p EdgePort) target() int {
	if p.ContainerPort > 0 {
		return p.ContainerPort
	}
	return p.Port
}

// EdgeSpec is everything the caller decides about the edge. The runner turns
// it into a container; it never accepts images, mounts or commands from the
// caller.
type EdgeSpec struct {
	Mode       string     `json:"mode"`        // EdgeModeHost | EdgeModeBridge
	AdminPort  int        `json:"admin_port"`  // loopback port of the admin gateway on the host
	AdminToken string     `json:"admin_token"` // 32..128 hex characters
	Ports      []EdgePort `json:"ports"`       // bridge only
}

// EdgeInfo is the observed edge container.
type EdgeInfo struct {
	ID        string `json:"id"`
	Image     string `json:"image"`
	Running   bool   `json:"running"`
	Recreated bool   `json:"recreated"` // true when this call created or replaced the container
}

// EdgeManager is the optional runner capability for the edge container.
// Docker implements it directly; Client implements it over the peer socket.
type EdgeManager interface {
	EnsureEdge(ctx context.Context, spec EdgeSpec) (EdgeInfo, error)
	ConnectEdge(ctx context.Context, app string) error
}

var hexToken = regexp.MustCompile(`^[0-9a-f]{32,128}$`)

// ValidateEdgeSpec rejects anything outside the contract.
func ValidateEdgeSpec(s EdgeSpec) error {
	switch s.Mode {
	case EdgeModeHost:
		if len(s.Ports) != 0 {
			return errors.New("host 模式不发布端口")
		}
	case EdgeModeBridge:
	default:
		return fmt.Errorf("未知的入口模式 %q", s.Mode)
	}
	if s.AdminPort < 1024 || s.AdminPort > 65535 {
		return fmt.Errorf("管理端口 %d 不合法", s.AdminPort)
	}
	if !hexToken.MatchString(s.AdminToken) {
		return errors.New("管理令牌必须是 32～128 位十六进制")
	}
	if len(s.Ports) > 256 {
		return errors.New("端口太多")
	}
	// host port -> bind addresses already used; 0.0.0.0 overlaps every address.
	hostSeen := map[int][]string{s.AdminPort: {"127.0.0.1"}}
	clash := func(port int, ip string) bool {
		for _, used := range hostSeen[port] {
			if used == ip || used == "0.0.0.0" || ip == "0.0.0.0" {
				return true
			}
		}
		return false
	}
	targetSeen := map[int]bool{edgeAdminContainerPort: true}
	for _, p := range s.Ports {
		if p.Port < 1 || p.Port > 65535 || p.target() < 1 || p.target() > 65535 {
			return fmt.Errorf("端口 %d 不合法", p.Port)
		}
		ip, err := netip.ParseAddr(p.HostIP)
		if err != nil || !ip.Is4() || !(ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate()) {
			return fmt.Errorf("端口 %d 的绑定地址 %q 不合法（只允许 127.0.0.1、0.0.0.0 或局域网地址）", p.Port, p.HostIP)
		}
		if clash(p.Port, p.HostIP) || targetSeen[p.target()] {
			return fmt.Errorf("端口 %s:%d 重复", p.HostIP, p.Port)
		}
		hostSeen[p.Port] = append(hostSeen[p.Port], p.HostIP)
		targetSeen[p.target()] = true
	}
	return nil
}

// EdgeCaddyfile is the initial Caddy configuration: the admin API on a
// container-private socket and the token-checked admin gateway. App routes
// are added later through that gateway (persist_config off: a restarted
// container comes back with just this file and acornfox pushes routes again).
func EdgeCaddyfile(s EdgeSpec) string {
	listen := "http://:" + strconv.Itoa(edgeAdminContainerPort)
	bind := ""
	if s.Mode == EdgeModeHost {
		listen = "http://:" + strconv.Itoa(s.AdminPort)
		bind = "\tbind 127.0.0.1\n"
	}
	return `{
	admin unix//tmp/caddy-admin.sock {
		origins caddy
	}
	persist_config off
}

` + listen + ` {
` + bind + `	@authorized header ` + EdgeTokenHeader + ` "` + s.AdminToken + `"
	handle @authorized {
		reverse_proxy unix//tmp/caddy-admin.sock {
			header_up Host caddy
		}
	}
	handle {
		respond "forbidden" 403
	}
}
`
}

// edgeHash identifies the container configuration; a different hash means
// the container must be recreated.
func edgeHash(s EdgeSpec, image string) string {
	ports := append([]EdgePort(nil), s.Ports...)
	sort.Slice(ports, func(i, j int) bool { return ports[i].target() < ports[j].target() })
	var b strings.Builder
	fmt.Fprintf(&b, "v2|%s|%s|%d|%s", image, s.Mode, s.AdminPort, s.AdminToken)
	for _, p := range ports {
		fmt.Fprintf(&b, "|%s:%d>%d", p.HostIP, p.Port, p.target())
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:12])
}

// EnsureEdge makes the edge container match spec: it reuses a running
// container created from the same spec, and otherwise (re)creates it. Data
// volumes survive recreation.
func (d *Docker) EnsureEdge(ctx context.Context, spec EdgeSpec) (EdgeInfo, error) {
	if err := ValidateEdgeSpec(spec); err != nil {
		return EdgeInfo{}, &RemoteError{Status: http.StatusBadRequest, ErrorResponse: ErrorResponse{Code: "invalid_request", Message: err.Error()}}
	}
	image, err := d.edgeImage(ctx)
	if err != nil {
		return EdgeInfo{}, err
	}
	hash := edgeHash(spec, image)

	if res, err := d.cli.ContainerInspect(ctx, EdgeContainerName, client.ContainerInspectOptions{}); err == nil {
		c := res.Container
		if c.Config == nil || c.Config.Labels[LabelEdge] != "1" {
			return EdgeInfo{}, &RemoteError{Status: http.StatusConflict, ErrorResponse: ErrorResponse{
				Code: "refused", Message: "已有同名容器 " + EdgeContainerName + " 但不是 AcornFox 创建的，请先改名或删除",
			}}
		}
		if c.Config.Labels[LabelEdgeHash] == hash {
			if c.State == nil || !c.State.Running {
				if err := d.startByName(ctx, EdgeContainerName); err != nil {
					return EdgeInfo{}, err
				}
			}
			return EdgeInfo{ID: c.ID, Image: image, Running: true}, nil
		}
		if _, err := d.cli.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
			return EdgeInfo{}, err
		}
	} else if !cerrdefs.IsNotFound(err) {
		return EdgeInfo{}, err
	}

	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		SecurityOpt:   []string{"no-new-privileges"},
		LogConfig: container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": LogMaxSize, "max-file": LogMaxFiles},
		},
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: EdgeDataVolume, Target: "/data"},
			{Type: mount.TypeVolume, Source: EdgeConfigVolume, Target: "/config"},
		},
	}
	pids := int64(DefaultPidsLimit)
	hostCfg.Resources = container.Resources{Memory: 256 << 20, MemorySwap: 256 << 20, PidsLimit: &pids}
	cfg := &container.Config{
		Image:  image,
		Cmd:    []string{"/usr/bin/caddy", "run", "--config", "/etc/acornfox/Caddyfile", "--adapter", "caddyfile"},
		Env:    []string{"XDG_CONFIG_HOME=/config", "XDG_DATA_HOME=/data", "HOME=/data"},
		Labels: map[string]string{LabelEdge: "1", LabelEdgeHash: hash},
	}
	if spec.Mode == EdgeModeHost {
		hostCfg.NetworkMode = "host"
	} else {
		if err := d.ensureEdgeNetwork(ctx); err != nil {
			return EdgeInfo{}, err
		}
		hostCfg.NetworkMode = container.NetworkMode(EdgeNetwork)
		exposed := network.PortSet{}
		bindings := network.PortMap{}
		admin, _ := network.PortFrom(uint16(edgeAdminContainerPort), network.TCP)
		exposed[admin] = struct{}{}
		bindings[admin] = []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: strconv.Itoa(spec.AdminPort)}}
		for _, p := range spec.Ports {
			cp, ok := network.PortFrom(uint16(p.target()), network.TCP)
			if !ok {
				return EdgeInfo{}, fmt.Errorf("invalid port %d", p.target())
			}
			exposed[cp] = struct{}{}
			bindings[cp] = append(bindings[cp], network.PortBinding{HostIP: netip.MustParseAddr(p.HostIP), HostPort: strconv.Itoa(p.Port)})
		}
		cfg.ExposedPorts = exposed
		hostCfg.PortBindings = bindings
	}
	created, err := d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{Name: EdgeContainerName, Config: cfg, HostConfig: hostCfg})
	if err != nil {
		// Another caller (a second server process during an upgrade) created
		// it first: use theirs when it carries the same spec.
		if cerrdefs.IsConflict(err) {
			if res, ierr := d.cli.ContainerInspect(ctx, EdgeContainerName, client.ContainerInspectOptions{}); ierr == nil &&
				res.Container.Config != nil && res.Container.Config.Labels[LabelEdgeHash] == hash {
				if serr := d.startByName(ctx, EdgeContainerName); serr != nil {
					return EdgeInfo{}, serr
				}
				return EdgeInfo{ID: res.Container.ID, Image: image, Running: true, Recreated: true}, nil
			}
		}
		return EdgeInfo{}, err
	}
	if err := d.copyCaddyfile(ctx, created.ID, EdgeCaddyfile(spec)); err != nil {
		_, _ = d.cli.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
		return EdgeInfo{}, err
	}
	if _, err := d.cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return EdgeInfo{}, err
	}
	return EdgeInfo{ID: created.ID, Image: image, Running: true, Recreated: true}, nil
}

// edgeImage prefers the locally imported image and pulls the public one
// otherwise.
func (d *Docker) edgeImage(ctx context.Context) (string, error) {
	if _, err := d.cli.ImageInspect(ctx, EdgeImageLocal); err == nil {
		return EdgeImageLocal, nil
	}
	if err := d.ensureAddonImage(ctx, EdgeImagePublic); err != nil {
		return "", err
	}
	return EdgeImagePublic, nil
}

func (d *Docker) ensureEdgeNetwork(ctx context.Context) error {
	if res, err := d.cli.NetworkInspect(ctx, EdgeNetwork, client.NetworkInspectOptions{}); err == nil {
		if res.Network.Labels[LabelEdge] != "1" {
			return &RemoteError{Status: http.StatusConflict, ErrorResponse: ErrorResponse{Code: "refused", Message: "已有同名网络 " + EdgeNetwork + " 但不是 AcornFox 创建的"}}
		}
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return err
	}
	_, err := d.cli.NetworkCreate(ctx, EdgeNetwork, client.NetworkCreateOptions{Driver: "bridge", Labels: map[string]string{LabelEdge: "1"}})
	if err != nil && cerrdefs.IsConflict(err) {
		return nil
	}
	return err
}

func (d *Docker) copyCaddyfile(ctx context.Context, id, content string) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	now := time.Now()
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "acornfox/", Mode: 0o755, ModTime: now}); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "acornfox/Caddyfile", Mode: 0o600, Size: int64(len(content)), ModTime: now}); err != nil {
		return err
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	_, err := d.cli.CopyToContainer(ctx, id, client.CopyToContainerOptions{DestinationPath: "/etc", Content: &buf})
	return err
}

// ConnectEdge attaches the edge container to NetworkName(app) so Caddy can
// reach the app by container name. It does nothing in host mode, when the
// app network does not exist yet, or when already connected.
func (d *Docker) ConnectEdge(ctx context.Context, app string) error {
	if !ValidApp(app) {
		return &RemoteError{Status: http.StatusBadRequest, ErrorResponse: ErrorResponse{Code: "invalid_request", Message: "应用名不合法"}}
	}
	res, err := d.cli.ContainerInspect(ctx, EdgeContainerName, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return err
	}
	c := res.Container
	if c.Config == nil || c.Config.Labels[LabelEdge] != "1" {
		return nil
	}
	if c.HostConfig != nil && c.HostConfig.NetworkMode == "host" {
		return nil
	}
	name := NetworkName(app)
	if c.NetworkSettings != nil {
		if _, ok := c.NetworkSettings.Networks[name]; ok {
			return nil
		}
	}
	nres, err := d.cli.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := checkNetworkOwner(app, nres.Network); err != nil {
		return err
	}
	_, err = d.cli.NetworkConnect(ctx, nres.Network.ID, client.NetworkConnectOptions{Container: c.ID})
	if err != nil && (cerrdefs.IsConflict(err) || strings.Contains(err.Error(), "already exists")) {
		return nil
	}
	return err
}

// disconnectEdge detaches the edge from an app network before the network is
// removed. Missing container or endpoint is success.
func (d *Docker) disconnectEdge(ctx context.Context, networkID string) {
	_, _ = d.cli.NetworkDisconnect(ctx, networkID, client.NetworkDisconnectOptions{Container: EdgeContainerName, Force: true})
}

// RemoveEdge stops and removes the edge container (volumes stay). Used by
// uninstall; missing is success.
func (d *Docker) RemoveEdge(ctx context.Context) error {
	res, err := d.cli.ContainerInspect(ctx, EdgeContainerName, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return err
	}
	if res.Container.Config == nil || res.Container.Config.Labels[LabelEdge] != "1" {
		return nil
	}
	_, err = d.cli.ContainerRemove(ctx, res.Container.ID, client.ContainerRemoveOptions{Force: true})
	if cerrdefs.IsNotFound(err) {
		return nil
	}
	return err
}

// Client side over the peer socket.

func (c *Client) EnsureEdge(ctx context.Context, spec EdgeSpec) (EdgeInfo, error) {
	var out EdgeInfo
	err := c.call(ctx, c.long, PathEdgeEnsure, spec, clientDefaultBound, &out)
	return out, err
}

func (c *Client) ConnectEdge(ctx context.Context, app string) error {
	return c.call(ctx, c.standard, PathEdgeConnect, AppRef{App: app}, clientDefaultBound, nil)
}

func (s *Server) handleEdgeEnsure(w http.ResponseWriter, r *http.Request) {
	var spec EdgeSpec
	if err := decode(r, &spec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求体格式不合法")
		return
	}
	m, ok := s.api.(EdgeManager)
	if !ok {
		writeError(w, http.StatusNotImplemented, "unsupported", "执行器不支持入口容器")
		return
	}
	info, err := m.EnsureEdge(r.Context(), spec)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleEdgeConnect(w http.ResponseWriter, r *http.Request) {
	var ref AppRef
	if err := decode(r, &ref); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求体格式不合法")
		return
	}
	if !validAppName(w, ref.App) {
		return
	}
	m, ok := s.api.(EdgeManager)
	if !ok {
		writeError(w, http.StatusNotImplemented, "unsupported", "执行器不支持入口容器")
		return
	}
	if err := m.ConnectEdge(r.Context(), ref.App); err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{OK: true})
}

var (
	_ EdgeManager = (*Client)(nil)
	_ EdgeManager = (*Docker)(nil)
)
