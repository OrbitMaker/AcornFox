package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// The edge is the one entry proxy container in front of every app
// (three-platform architecture). acornfox manages it like an app container
// but it is not an app: it carries LabelEdge instead of LabelManaged, so app
// listings, orphan sweeps and per-app cleanup never see it.
//
// The container's first process is the adapter (edge.AdapterBinary). It
// starts the proxy software, keeps the last configuration on the container's
// settings volume and serves the acornfox program on its own management
// endpoint, which requires the token (edge.TokenHeader). The runner only
// creates the container from the local entry proxy image (edge.Image, loaded
// by the install and upgrade scripts with docker import); it never pulls it.
//
// Two network modes:
//
//   - host (Linux): the proxy shares the host network, listens on the apps'
//     public ports and 80/443 directly and proxies to the containers' loopback
//     ports. Publishing 90 app ports through docker-proxy would cost a process
//     per port, and a published port list cannot change without recreating
//     the container. The management endpoint is 127.0.0.1:<admin port>.
//   - bridge (macOS, Windows): Docker runs in a VM, so the proxy sits on the
//     "acornfox-edge" network, joins each app's network and proxies to the
//     app container by name. The whole app port range is published once
//     (reserved ports), twice: 127.0.0.1:P -> P and
//     <LAN address>:P -> P+10000. An app reachable only locally gets a
//     listener on P; a LAN app on P and P+10000. Adding or removing apps and
//     changing exposure are route changes only; the container is recreated
//     only when the LAN address (or the range) changes. Docker Desktop does
//     not keep the client address, so the proxy cannot tell local from LAN
//     traffic by source; the bind address does it instead. The management
//     endpoint is container port edge.BridgeAdminPort published on
//     127.0.0.1:<admin port>.
const (
	EdgeContainerName = "acornfox-edge"
	EdgeNetwork       = "acornfox-edge"
	LabelEdge         = "acornfox.edge"            // "1" on the edge container and network
	LabelEdgeHash     = "acornfox.edge.hash"       // hash of the spec the container was created from
	LabelEdgeImage    = "acornfox.edge.version"    // edge.ImageVersion the container was created from
	LabelEdgeAdmin    = "acornfox.edge.admin-port" // host loopback port of its management endpoint
	// LabelEdgeReplaced is the switch record on the container itself: the ID
	// of the entry container this one replaced (stopped, kept until the
	// upgrade script removes it or switches back to it).
	LabelEdgeReplaced = "acornfox.edge.replaced"
	// EdgeOldPrefix names an old entry container that had EdgeContainerName
	// while a new one takes the name (switchEdge).
	EdgeOldPrefix = "acornfox-edge-old-"

	EdgeDataVolume     = "acornfox-edge-data"     // certificates, ACME accounts (edge.DataMount)
	EdgeSettingsVolume = "acornfox-edge-settings" // the adapter's saved configuration (edge.SettingsMount)
	// EdgeSettingsVolumeB is the settings volume of a new entry container
	// switching from one that uses EdgeSettingsVolume: the two never share
	// one, so the old one keeps its own configuration if the switch fails.
	EdgeSettingsVolumeB = "acornfox-edge-settings-b"

	EdgeModeHost   = "host"
	EdgeModeBridge = "bridge"

	PathEdgeEnsure  = "/v1/edge/ensure"  // POST EdgeSpec -> EdgeInfo
	PathEdgeConnect = "/v1/edge/connect" // POST AppRef -> OK
)

// ProbeImage is the public image the download source speed test (acornfox
// sources --probe, doctor) samples: a well mirrored official image of a
// typical size. It has nothing to do with the entry proxy, which never comes
// from a registry.
const ProbeImage = "nginx:1.27-alpine"

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
	AdminPort  int        `json:"admin_port"`  // loopback port of the adapter's management endpoint on the host
	AdminToken string     `json:"admin_token"` // 32..128 hex characters
	Ports      []EdgePort `json:"ports"`       // bridge only
	// Config is the whole configuration the caller wants in effect. A new
	// container gets it in its settings volume before it starts (it serves
	// every route at once); switching from an old container requires it.
	// It is data for the adapter, which checks it; it never changes the
	// container (not part of the hash).
	Config *edge.Config `json:"config,omitempty"`
	// Recreate rebuilds the container in place (same volumes, the token and
	// Config copied in again) even when it was created from this very spec.
	// The reconciler sets it when the running container refuses the token
	// in the data directory (EDGE-03: someone changed the token file inside
	// the container), which the spec cannot show: the label carries a hash
	// of the spec, the container's file is not read. Not part of the hash.
	Recreate bool `json:"recreate,omitempty"`
}

// EdgeInfo is the observed edge container.
type EdgeInfo struct {
	ID        string `json:"id"`
	Image     string `json:"image"`
	Running   bool   `json:"running"`
	Recreated bool   `json:"recreated"` // true when this call created or replaced the container
	// AdminPort is the host port the container's management endpoint
	// listens on (spec.AdminPort, or the other one of the pair after a
	// switch from a container that held it).
	AdminPort int `json:"admin_port,omitempty"`
	// Replaced and ReplacedName name the entry container this call switched
	// away from: stopped, not removed (the switch record).
	Replaced     string `json:"replaced,omitempty"`
	ReplacedName string `json:"replaced_name,omitempty"`
	// Note is a sentence for the log about something that did not go fully
	// to plan without being an error.
	Note string `json:"note,omitempty"`
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
	targetSeen := map[int]bool{edge.BridgeAdminPort: true}
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

// edgeCommand is the container's command: the adapter with its management
// endpoint. The token is a file in the container (edge.TokenFile), never an
// argument.
func edgeCommand(s EdgeSpec) []string {
	listen := ":" + strconv.Itoa(edge.BridgeAdminPort)
	if s.Mode == EdgeModeHost {
		listen = "127.0.0.1:" + strconv.Itoa(s.AdminPort)
	}
	return []string{edge.AdapterBinary, "run", "--admin-listen", listen}
}

// edgeHash identifies the container configuration; a different hash means
// the container must be recreated.
func edgeHash(s EdgeSpec, image string) string {
	ports := append([]EdgePort(nil), s.Ports...)
	sort.Slice(ports, func(i, j int) bool { return ports[i].target() < ports[j].target() })
	var b strings.Builder
	fmt.Fprintf(&b, "v3|%s|%s|%d|%s", image, s.Mode, s.AdminPort, s.AdminToken)
	for _, p := range ports {
		fmt.Fprintf(&b, "|%s:%d>%d", p.HostIP, p.Port, p.target())
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:12])
}

// edgeHealthcheck lets Docker run the adapter's own health check (the same
// one the acornfox program execs while it switches containers).
var edgeHealthcheck = &container.HealthConfig{
	Test:        []string{"CMD", edge.AdapterBinary, "health"},
	Interval:    30 * time.Second,
	Timeout:     10 * time.Second,
	StartPeriod: 30 * time.Second,
	Retries:     3,
}

// EnsureEdge makes the edge container match spec. Data volumes survive every
// recreation and switch.
//
//   - The running container named EdgeContainerName created from the same
//     spec is reused (started when it was stopped), unless spec.Recreate.
//   - The same entry image with another spec (bridge ports, token), or the
//     same spec with spec.Recreate, is recreated in place: the old one is
//     removed first, then the new one starts on the same volumes (a short
//     gap; only a spec change or spec.Recreate does this).
//   - An entry container from an older entry image, or one an older version
//     created under another name (it carries LabelEdge), is switched to a new
//     one without a gap (switchEdge). This is what the first start of a new
//     version after an upgrade does.
//   - Otherwise a new one is created.
//
// A container is never pulled: the image comes from the release's entry
// package (edgeImage).
func (d *Docker) EnsureEdge(ctx context.Context, spec EdgeSpec) (EdgeInfo, error) {
	if err := ValidateEdgeSpec(spec); err != nil {
		return EdgeInfo{}, &RemoteError{Status: http.StatusBadRequest, ErrorResponse: ErrorResponse{Code: "invalid_request", Message: err.Error()}}
	}
	image, err := d.edgeImage(ctx)
	if err != nil {
		return EdgeInfo{}, err
	}
	edges, err := d.listEdges(ctx)
	if err != nil {
		return EdgeInfo{}, err
	}
	var cur *container.Summary
	var legacy []container.Summary
	for i := range edges {
		c := edges[i]
		if containerNamed(c, EdgeContainerName) {
			cur = &edges[i]
		} else if c.State == container.StateRunning {
			legacy = append(legacy, c)
		}
	}
	if cur == nil {
		// Same name but not ours (no label): never touch it.
		if res, err := d.c().ContainerInspect(ctx, EdgeContainerName, client.ContainerInspectOptions{}); err == nil {
			if res.Container.Config == nil || res.Container.Config.Labels[LabelEdge] != "1" {
				return EdgeInfo{}, &RemoteError{Status: http.StatusConflict, ErrorResponse: ErrorResponse{
					Code: "refused", Message: "已有同名容器 " + EdgeContainerName + " 但不是 AcornFox 创建的，请先改名或删除",
				}}
			}
		} else if !cerrdefs.IsNotFound(err) {
			return EdgeInfo{}, err
		}
	}

	if cur != nil {
		port := edgeAdminPortOf(*cur, spec.AdminPort)
		at := spec
		at.AdminPort = port
		hash := edgeHash(at, image)
		if cur.Labels[LabelEdgeHash] == hash && !spec.Recreate {
			if cur.State != container.StateRunning {
				if err := d.startByName(ctx, cur.ID); err != nil {
					return EdgeInfo{}, err
				}
			}
			return EdgeInfo{ID: cur.ID, Image: image, Running: true, AdminPort: port}, nil
		}
		if cur.Labels[LabelEdgeImage] == edge.ImageVersion || cur.State != container.StateRunning {
			// Same entry image, other spec or asked to (spec.Recreate; the
			// same spec means the same image), or nothing running to switch
			// from: recreate in place on the same volumes.
			if _, err := d.c().ContainerRemove(ctx, cur.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
				return EdgeInfo{}, err
			}
			return d.createAndStartEdge(ctx, at, image, edgeVolumes{data: mountVolume(*cur, edge.DataMount, EdgeDataVolume), settings: mountVolume(*cur, edge.SettingsMount, EdgeSettingsVolume)})
		}
		return d.switchEdge(ctx, spec, image, *cur)
	}
	if len(legacy) > 0 {
		return d.switchEdge(ctx, spec, image, legacy[0])
	}
	return d.createAndStartEdge(ctx, spec, image, edgeVolumes{data: EdgeDataVolume, settings: EdgeSettingsVolume})
}

// listEdges lists every container carrying LabelEdge, running or not.
func (d *Docker) listEdges(ctx context.Context) ([]container.Summary, error) {
	res, err := d.c().ContainerList(ctx, client.ContainerListOptions{All: true, Filters: client.Filters{}.Add("label", LabelEdge+"=1")})
	if err != nil {
		return nil, err
	}
	out := res.Items[:0]
	for _, c := range res.Items {
		if c.Labels[LabelEdge] == "1" {
			out = append(out, c)
		}
	}
	return out, nil
}

func containerNamed(c container.Summary, name string) bool {
	for _, n := range c.Names {
		if strings.TrimPrefix(n, "/") == name {
			return true
		}
	}
	return false
}

func containerName(c container.Summary) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return c.ID
}

// mountVolume is the volume c has at target, or def when it has none there.
func mountVolume(c container.Summary, target, def string) string {
	for _, m := range c.Mounts {
		if m.Destination == target && m.Type == mount.TypeVolume && m.Name != "" {
			return m.Name
		}
	}
	return def
}

// edgeAdminPortOf is the host port c's management endpoint listens on
// (LabelEdgeAdmin); def for a container without the label.
func edgeAdminPortOf(c container.Summary, def int) int {
	if p, err := strconv.Atoi(c.Labels[LabelEdgeAdmin]); err == nil && p > 0 {
		return p
	}
	return def
}

// edgeVolumes are the volumes a new entry container mounts.
type edgeVolumes struct {
	data, settings string
	replaced       string // ID of the container it replaces (LabelEdgeReplaced), "" for none
}

// createEdge creates (does not start) the entry container named name from
// spec, with the token file and, when spec.Config is set, the saved
// configuration already in its settings volume.
func (d *Docker) createEdge(ctx context.Context, name string, spec EdgeSpec, image string, vols edgeVolumes) (string, error) {
	hash := edgeHash(spec, image)
	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		SecurityOpt:   []string{"no-new-privileges"},
		LogConfig: container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": LogMaxSize, "max-file": LogMaxFiles},
		},
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: vols.data, Target: edge.DataMount},
			{Type: mount.TypeVolume, Source: vols.settings, Target: edge.SettingsMount},
		},
	}
	pids := int64(DefaultPidsLimit)
	hostCfg.Resources = container.Resources{Memory: 256 << 20, MemorySwap: 256 << 20, PidsLimit: &pids}
	labels := map[string]string{LabelEdge: "1", LabelEdgeHash: hash, LabelEdgeImage: edge.ImageVersion, LabelEdgeAdmin: strconv.Itoa(spec.AdminPort)}
	if vols.replaced != "" {
		labels[LabelEdgeReplaced] = vols.replaced
	}
	cfg := &container.Config{
		Image:       image,
		Cmd:         edgeCommand(spec),
		Env:         []string{"XDG_CONFIG_HOME=" + edge.SettingsMount, "XDG_DATA_HOME=" + edge.DataMount, "HOME=" + edge.DataMount},
		Labels:      labels,
		Healthcheck: edgeHealthcheck,
	}
	if spec.Mode == EdgeModeHost {
		hostCfg.NetworkMode = "host"
	} else {
		if err := d.ensureEdgeNetwork(ctx); err != nil {
			return "", err
		}
		hostCfg.NetworkMode = container.NetworkMode(EdgeNetwork)
		exposed := network.PortSet{}
		bindings := network.PortMap{}
		admin, _ := network.PortFrom(uint16(edge.BridgeAdminPort), network.TCP)
		exposed[admin] = struct{}{}
		bindings[admin] = []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: strconv.Itoa(spec.AdminPort)}}
		for _, p := range spec.Ports {
			cp, ok := network.PortFrom(uint16(p.target()), network.TCP)
			if !ok {
				return "", fmt.Errorf("invalid port %d", p.target())
			}
			exposed[cp] = struct{}{}
			bindings[cp] = append(bindings[cp], network.PortBinding{HostIP: netip.MustParseAddr(p.HostIP), HostPort: strconv.Itoa(p.Port)})
		}
		cfg.ExposedPorts = exposed
		hostCfg.PortBindings = bindings
	}
	created, err := d.c().ContainerCreate(ctx, client.ContainerCreateOptions{Name: name, Config: cfg, HostConfig: hostCfg})
	if err != nil {
		return "", err
	}
	if err := d.copyEdgeFiles(ctx, created.ID, spec); err != nil {
		_, _ = d.c().ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
		return "", err
	}
	return created.ID, nil
}

// createAndStartEdge creates the entry container under EdgeContainerName and
// starts it.
func (d *Docker) createAndStartEdge(ctx context.Context, spec EdgeSpec, image string, vols edgeVolumes) (EdgeInfo, error) {
	id, err := d.createEdge(ctx, EdgeContainerName, spec, image, vols)
	if err != nil {
		// Another caller (a second server process) created it first: use
		// theirs when it carries the same spec.
		if cerrdefs.IsConflict(err) {
			if res, ierr := d.c().ContainerInspect(ctx, EdgeContainerName, client.ContainerInspectOptions{}); ierr == nil &&
				res.Container.Config != nil && res.Container.Config.Labels[LabelEdgeHash] == edgeHash(spec, image) {
				if serr := d.startByName(ctx, EdgeContainerName); serr != nil {
					return EdgeInfo{}, serr
				}
				return EdgeInfo{ID: res.Container.ID, Image: image, Running: true, Recreated: true, AdminPort: spec.AdminPort}, nil
			}
		}
		return EdgeInfo{}, edgeStartError(err)
	}
	if _, err := d.c().ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		return EdgeInfo{}, edgeStartError(err)
	}
	return EdgeInfo{ID: id, Image: image, Running: true, Recreated: true, AdminPort: spec.AdminPort}, nil
}

// edgeStartError keeps Docker's reason and adds the fault code of an entry
// container that does not run. Errors the reconciler handles by their text
// (a LAN address Docker cannot bind) keep it.
func edgeStartError(err error) error {
	var re *RemoteError
	if errors.As(err, &re) {
		return err
	}
	return fmt.Errorf("%w（故障编号 %s）", err, edge.FaultNotRunning)
}

// Switching from an old entry container to a new one.
//
// Linux (host network): both containers can listen on the same ports at the
// same time, so the new one starts next to the old one and the old one is
// stopped only after the new one is healthy. Measured: no new connection
// fails; an idle keep-alive connection to the old one is closed once.
//
//  1. The new container mounts the old one's data volume (certificates and
//     ACME accounts carry over, nothing is signed again) and a settings
//     volume of its own with the whole configuration (spec.Config) already
//     in it, so it serves every route from its first second.
//  2. Its management endpoint is its own (a port the old one does not use):
//     while both run, nothing is sent to the old one.
//  3. Health is asked inside the new container (afx-edge health): from the
//     host a port cannot tell which container listens on it. Up to
//     edgeSwitchTimeout.
//  4. Healthy: the old container is stopped gracefully (docker stop, 10 s)
//     and kept; the new one carries the old one's ID in LabelEdgeReplaced
//     (the switch record), and the caller writes the record file. The
//     upgrade script removes the stopped old one by that ID once the new
//     version is confirmed, or switches back to it when the upgrade is
//     undone.
//  5. Not healthy: the new container is removed, the old one never stopped
//     and keeps serving; the error carries edge.FaultSwitchFailed.
//
// macOS and Windows (bridge): Docker cannot publish one port from two
// containers, so the old one is stopped first and started again when the
// new one fails (a short gap either way).
var (
	edgeSwitchTimeout = 30 * time.Second
	edgeSwitchPoll    = 300 * time.Millisecond
	edgeStopSeconds   = 10
)

func (d *Docker) switchEdge(ctx context.Context, spec EdgeSpec, image string, old container.Summary) (EdgeInfo, error) {
	if spec.Config == nil {
		return EdgeInfo{}, &RemoteError{Status: http.StatusBadRequest, ErrorResponse: ErrorResponse{Code: "invalid_request",
			Message: "换入口容器要带上整份设置（新容器起来就要能服务全部应用）"}}
	}
	oldName := containerName(old)
	fail := func(reason string, err error) (EdgeInfo, error) {
		msg := "换成新的入口容器没有成功，原来的入口容器 " + oldName + " 照常服务：" + reason
		if err != nil {
			msg += "：" + err.Error()
		}
		return EdgeInfo{}, &RemoteError{Status: http.StatusBadGateway, ErrorResponse: ErrorResponse{Code: edge.FaultSwitchFailed, Message: msg + "（故障编号 " + edge.FaultSwitchFailed + "）"}}
	}
	vols := edgeVolumes{data: mountVolume(old, edge.DataMount, EdgeDataVolume), settings: EdgeSettingsVolume, replaced: old.ID}
	if mountVolume(old, edge.SettingsMount, "") == EdgeSettingsVolume {
		vols.settings = EdgeSettingsVolumeB
	}
	at := spec
	if spec.Mode == EdgeModeHost && edgeAdminPortOf(old, 0) == spec.AdminPort {
		at.AdminPort = altAdminPort(spec.AdminPort)
	}
	bridge := spec.Mode == EdgeModeBridge
	renamed := false
	if containerNamed(old, EdgeContainerName) {
		// The new one takes the fixed name; the old one keeps running under
		// another (docker rename works on a running container).
		if _, err := d.c().ContainerRename(ctx, old.ID, client.ContainerRenameOptions{NewName: EdgeOldPrefix + shortID(old.ID)}); err != nil {
			return fail("给原来的入口容器改名没有成功", err)
		}
		renamed = true
	}
	undo := func(newID string) {
		// A fresh context: the caller's may be what ended.
		uctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if newID != "" {
			_, _ = d.c().ContainerRemove(uctx, newID, client.ContainerRemoveOptions{Force: true})
		}
		if renamed {
			_, _ = d.c().ContainerRename(uctx, old.ID, client.ContainerRenameOptions{NewName: oldName})
		}
		if bridge {
			_, _ = d.c().ContainerStart(uctx, old.ID, client.ContainerStartOptions{})
		}
	}
	id, err := d.createEdge(ctx, EdgeContainerName, at, image, vols)
	if err != nil {
		undo("")
		return fail("新的入口容器没能建出来", err)
	}
	if bridge {
		if err := d.stopEdge(ctx, old.ID); err != nil {
			undo(id)
			return fail("没能停下原来的入口容器", err)
		}
	}
	if _, err := d.c().ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		undo(id)
		return fail("新的入口容器起不来", err)
	}
	if ok, why := d.waitEdgeHealthy(ctx, id, spec.Config.Version()); !ok {
		undo(id)
		return fail(fmt.Sprintf("新的入口容器 %d 秒内没有通过健康检查（%s）", int(edgeSwitchTimeout/time.Second), why), nil)
	}
	if !bridge {
		if err := d.stopEdge(ctx, old.ID); err != nil {
			// The new one serves; the old one still listening too is
			// harmless (same routes) and the upgrade script stops and
			// removes it by the switch record.
			return EdgeInfo{ID: id, Image: image, Running: true, Recreated: true, AdminPort: at.AdminPort, Replaced: old.ID, ReplacedName: oldName,
				Note: "原来的入口容器没能停下：" + err.Error()}, nil
		}
	}
	return EdgeInfo{ID: id, Image: image, Running: true, Recreated: true, AdminPort: at.AdminPort, Replaced: old.ID, ReplacedName: oldName}, nil
}

// altAdminPort is the other management port two entry containers switch
// between, so a new adapter never needs the port the old one still holds.
func altAdminPort(p int) int {
	if p > 1024 {
		return p - 1
	}
	return p + 1
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// stopEdge stops an entry container gracefully (its proxy finishes running
// requests, up to edge.GracePeriodSeconds) and keeps it.
func (d *Docker) stopEdge(ctx context.Context, id string) error {
	t := edgeStopSeconds
	_, err := d.c().ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &t})
	if cerrdefs.IsNotFound(err) || cerrdefs.IsNotModified(err) {
		return nil
	}
	return err
}

// waitEdgeHealthy asks the container's own health check until it passes or
// edgeSwitchTimeout ends; why is the last answer when it did not pass.
func (d *Docker) waitEdgeHealthy(ctx context.Context, id, want string) (ok bool, why string) {
	deadline := time.Now().Add(edgeSwitchTimeout)
	for {
		ok, why = d.probeEdge(ctx, id, want)
		if ok {
			return true, ""
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			return false, why
		}
		select {
		case <-ctx.Done():
			return false, why
		case <-time.After(edgeSwitchPoll):
		}
	}
}

// edgeHealthReport is the part of `afx-edge health --json` the runner reads.
type edgeHealthReport struct {
	OK       bool     `json:"ok"`
	Version  string   `json:"version"`
	Problems []string `json:"problems"`
}

// probeEdge runs the adapter's health check in the entry container id once.
func (d *Docker) probeEdge(ctx context.Context, id, want string) (bool, string) {
	if d.edgeProbeHook != nil {
		return d.edgeProbeHook(ctx, id, want)
	}
	cmd := []string{edge.AdapterBinary, "health", "--json"}
	if want != "" {
		cmd = append(cmd, "--want", want)
	}
	created, err := d.c().ExecCreate(ctx, id, client.ExecCreateOptions{AttachStdout: true, AttachStderr: true, Cmd: cmd})
	if err != nil {
		return false, "在新容器里运行健康检查失败：" + err.Error()
	}
	deadline := time.Now().Add(15 * time.Second)
	hj, err := d.c().ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return false, "在新容器里运行健康检查失败：" + err.Error()
	}
	out, timedOut, err := collectExec(ctx, hijackedStream{hj.HijackedResponse}, nil, deadline)
	if err != nil {
		return false, err.Error()
	}
	if timedOut {
		return false, "健康检查超时"
	}
	var rep edgeHealthReport
	if err := json.Unmarshal(out.stdout, &rep); err != nil {
		msg := strings.TrimSpace(string(out.stderr))
		if msg == "" {
			msg = strings.TrimSpace(string(out.stdout))
		}
		return false, "读不懂健康检查的结果：" + msg
	}
	if !rep.OK {
		return false, strings.Join(rep.Problems, "；")
	}
	return true, ""
}

// edgeImage returns the local entry proxy image. It is never pulled: the
// install and upgrade scripts (and the desktop setup) load it from the
// release's entry package.
func (d *Docker) edgeImage(ctx context.Context) (string, error) {
	if _, err := d.c().ImageInspect(ctx, edge.Image); err != nil {
		if cerrdefs.IsNotFound(err) {
			return "", &RemoteError{Status: http.StatusFailedDependency, ErrorResponse: ErrorResponse{
				Code:    edge.FaultImageMissing,
				Message: "本机没有入口镜像 " + edge.Image + "（它由安装或升级脚本从发行版的入口包导入，重新运行安装或升级脚本即可补上；故障编号 " + edge.FaultImageMissing + "）",
			}}
		}
		return "", err
	}
	return edge.Image, nil
}

// ImportEdgeImage loads the entry package (a gzip compressed root file
// system, acornfox-edge_<version>_linux_<arch>.tar.gz) as edge.Image, the
// way the install script does with docker import. The desktop setup uses
// it; the Linux scripts call docker import themselves.
func (d *Docker) ImportEdgeImage(ctx context.Context, pkg io.Reader) error {
	res, err := d.c().ImageImport(ctx, client.ImageImportSource{Source: pkg, SourceName: "-"}, edge.Image, client.ImageImportOptions{
		Message: "AcornFox entry " + edge.ImageVersion,
	})
	if err != nil {
		return fmt.Errorf("导入入口镜像失败：%w（故障编号 %s）", err, edge.FaultImageMissing)
	}
	defer res.Close()
	if _, err := io.Copy(io.Discard, res); err != nil {
		return fmt.Errorf("导入入口镜像失败：%w（故障编号 %s）", err, edge.FaultImageMissing)
	}
	if _, err := d.c().ImageInspect(ctx, edge.Image); err != nil {
		return fmt.Errorf("导入以后找不到入口镜像 %s（故障编号 %s）", edge.Image, edge.FaultImageMissing)
	}
	return nil
}

func (d *Docker) ensureEdgeNetwork(ctx context.Context) error {
	if res, err := d.c().NetworkInspect(ctx, EdgeNetwork, client.NetworkInspectOptions{}); err == nil {
		if res.Network.Labels[LabelEdge] != "1" {
			return &RemoteError{Status: http.StatusConflict, ErrorResponse: ErrorResponse{Code: "refused", Message: "已有同名网络 " + EdgeNetwork + " 但不是 AcornFox 创建的"}}
		}
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return err
	}
	_, err := d.c().NetworkCreate(ctx, EdgeNetwork, client.NetworkCreateOptions{Driver: "bridge", Labels: map[string]string{LabelEdge: "1"}})
	if err != nil && cerrdefs.IsConflict(err) {
		return nil
	}
	return err
}

// copyEdgeFiles writes the management token (edge.TokenFile, readable by
// the container's root only) and, when spec.Config is set, the saved
// configuration (edge.StoreFile, the adapter's format) into the created, not
// yet started container. The adapter applies the configuration as soon as
// it starts.
func (d *Docker) copyEdgeFiles(ctx context.Context, id string, spec EdgeSpec) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	now := time.Now()
	add := func(name string, mode int64, content []byte) error {
		dir := path.Dir(name)
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: dir + "/", Mode: 0o700, ModTime: now}); err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: mode, Size: int64(len(content)), ModTime: now}); err != nil {
			return err
		}
		_, err := tw.Write(content)
		return err
	}
	if err := add(strings.TrimPrefix(edge.TokenFile, "/"), 0o600, []byte(spec.AdminToken+"\n")); err != nil {
		return err
	}
	if spec.Config != nil {
		b, err := json.MarshalIndent(edge.Stored{Version: spec.Config.Version(), SavedAt: now.UTC(), Config: *spec.Config}, "", "  ")
		if err != nil {
			return err
		}
		if err := add(strings.TrimPrefix(edge.StoreFile, "/"), 0o600, append(b, '\n')); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	_, err := d.c().CopyToContainer(ctx, id, client.CopyToContainerOptions{DestinationPath: "/", Content: &buf})
	return err
}

// ConnectEdge attaches the edge container to NetworkName(app) so the proxy can
// reach the app by container name. It does nothing in host mode, when the
// app network does not exist yet, or when already connected.
func (d *Docker) ConnectEdge(ctx context.Context, app string) error {
	if !ValidApp(app) {
		return &RemoteError{Status: http.StatusBadRequest, ErrorResponse: ErrorResponse{Code: "invalid_request", Message: "应用名不合法"}}
	}
	res, err := d.c().ContainerInspect(ctx, EdgeContainerName, client.ContainerInspectOptions{})
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
	nres, err := d.c().NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := checkNetworkOwner(app, nres.Network); err != nil {
		return err
	}
	_, err = d.c().NetworkConnect(ctx, nres.Network.ID, client.NetworkConnectOptions{Container: c.ID})
	if err != nil && (cerrdefs.IsConflict(err) || strings.Contains(err.Error(), "already exists")) {
		return nil
	}
	return err
}

// disconnectEdge detaches the edge from an app network before the network is
// removed. Missing container or endpoint is success.
func (d *Docker) disconnectEdge(ctx context.Context, networkID string) {
	_, _ = d.c().NetworkDisconnect(ctx, networkID, client.NetworkDisconnectOptions{Container: EdgeContainerName, Force: true})
}

// RemoveEdge stops and removes the edge container (volumes stay). Used by
// uninstall; missing is success.
func (d *Docker) RemoveEdge(ctx context.Context) error {
	res, err := d.c().ContainerInspect(ctx, EdgeContainerName, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return err
	}
	if res.Container.Config == nil || res.Container.Config.Labels[LabelEdge] != "1" {
		return nil
	}
	_, err = d.c().ContainerRemove(ctx, res.Container.ID, client.ContainerRemoveOptions{Force: true})
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
