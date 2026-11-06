package runner

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/build"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// buildMemoryBytes bounds the memory available to a build; large enough for
// typical language toolchains, small enough to protect the host.
const buildMemoryBytes = 2 << 30

// ansiPattern strips terminal color/cursor escape codes from log lines.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// Build classification heuristics.
var (
	missingModulePattern = regexp.MustCompile(`(?i)(ModuleNotFoundError|Cannot find module|no required module|not found: )`)
	pullFailurePattern   = regexp.MustCompile(`(?i)(TLS handshake timeout|i/o timeout|dial tcp|context deadline exceeded|no such host|connection reset|connection refused|EOF|unexpected status from (GET|HEAD) request|httpReadSeeker: failed open|toomanyrequests)`)
	// Package-manager downloads during the build (npm / yarn / pnpm, pip, Go
	// modules, apk, apt). Checked before pullFailurePattern, whose generic
	// network errors would otherwise blame the base image.
	registryTimeoutPattern = regexp.MustCompile(`(?i)(npm (ERR!|error) (code |errno )?(ETIMEDOUT|ECONNRESET|ECONNREFUSED|EAI_AGAIN|ENOTFOUND)|network request to https?://\S*registry|ERR_PNPM_META_FETCH_FAIL|trouble with your network connection|Read timed out|ConnectTimeoutError|Could not fetch URL|Retrying \(Retry\(total=|proxy\.golang\.org|sum\.golang\.org|dl-cdn\.alpinelinux\.org|Failed to fetch https?://\S*(debian|ubuntu)|Could not connect to \S*(debian|ubuntu))`)
)

// Docker implements API against a local Docker daemon. It only ever touches
// objects carrying LabelManaged=1 and never creates privileged, host-network
// or bind-mounted containers.
type Docker struct {
	// cli is swapped by SetClient when the desktop runtime changes (setup
	// installed the built-in runtime, or Docker moved to another address).
	cli atomic.Pointer[client.Client]

	// registry is how Docker Hub images are pulled (pull_fallback.go):
	// mirror first for users in mainland China, Docker Hub first otherwise.
	registry Registry

	// mu guards inflight, the concurrent-build dedupe map.
	mu       sync.Mutex
	inflight map[string]*buildCall
}

// buildCall is one shared, in-flight build keyed by image tag (singleflight).
type buildCall struct {
	done chan struct{}
	resp BuildResponse
	err  error
}

// NewDocker returns a Docker-backed API. The caller owns cli.
func NewDocker(cli *client.Client) *Docker {
	d := &Docker{inflight: map[string]*buildCall{}}
	d.cli.Store(cli)
	return d
}

func (d *Docker) c() *client.Client { return d.cli.Load() }

// Client returns the Docker client in use now.
func (d *Docker) Client() *client.Client { return d.cli.Load() }

// SetClient points the runner at another Docker daemon and returns the
// previous client (the caller closes it). Calls already in flight finish on
// the old client.
func (d *Docker) SetClient(cli *client.Client) *client.Client { return d.cli.Swap(cli) }

// Ping reports the Docker API and server versions.
func (d *Docker) Ping(ctx context.Context) (PingResponse, error) {
	ping, err := d.c().Ping(ctx, client.PingOptions{})
	if err != nil {
		return PingResponse{}, err
	}
	resp := PingResponse{DockerAPIVersion: ping.APIVersion}
	if ver, err := d.c().ServerVersion(ctx, client.ServerVersionOptions{}); err == nil {
		resp.ServerVersion = ver.Version
		if resp.DockerAPIVersion == "" {
			resp.DockerAPIVersion = ver.APIVersion
		}
	}
	return resp, nil
}

// Build builds ImageTag(App, DeploymentID) from a tar or tar.gz on disk. If the
// tag already exists it is returned without building. Concurrent identical
// requests share a single build.
func (d *Docker) Build(ctx context.Context, req BuildRequest) (BuildResponse, error) {
	tag := ImageTag(req.App, req.DeploymentID)

	// Fast path: a previous build already produced this tag.
	if info, err := d.inspectImage(ctx, tag, req.App); err == nil {
		return BuildResponse{OK: true, Image: &info}, nil
	} else if !cerrdefs.IsNotFound(err) {
		return BuildResponse{}, err
	}

	// Deduplicate concurrent identical builds by tag.
	d.mu.Lock()
	if call, ok := d.inflight[tag]; ok {
		d.mu.Unlock()
		select {
		case <-call.done:
			return call.resp, call.err
		case <-ctx.Done():
			return BuildResponse{}, ctx.Err()
		}
	}
	call := &buildCall{done: make(chan struct{})}
	d.inflight[tag] = call
	d.mu.Unlock()

	call.resp, call.err = d.runBuild(ctx, req, tag)
	close(call.done)

	d.mu.Lock()
	delete(d.inflight, tag)
	d.mu.Unlock()

	return call.resp, call.err
}

// runBuild performs a single build: it scans the upload once as a stream to
// validate it (tar or tar.gz, bounded uncompressed size, a Dockerfile at the
// context root), then streams the same file to Docker, which decompresses gzip
// itself. The upload is never held in memory. User-caused failures come back
// as BuildResponse{OK:false}; daemon or transport failures as an error.
func (d *Docker) runBuild(ctx context.Context, req BuildRequest, tag string) (BuildResponse, error) {
	f, err := os.Open(req.ContextPath)
	if err != nil {
		return BuildResponse{OK: false, Failure: &Failure{
			Stage: "upload", Code: "upload_invalid",
			Message: "无法读取上传的项目包",
			Hint:    "使用 acornfox deploy 重新上传项目目录",
		}}, nil
	}
	defer f.Close()

	if failure := scanContext(f); failure != nil {
		return BuildResponse{OK: false, Failure: failure}, nil
	}
	// Second pass: the same open file, so what Docker builds is what was scanned.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return BuildResponse{}, err
	}

	labels := map[string]string{
		LabelManaged:    "1",
		LabelApp:        req.App,
		LabelDeployment: req.DeploymentID,
	}
	result, err := d.c().ImageBuild(ctx, f, client.ImageBuildOptions{
		Tags:        []string{tag},
		Remove:      true,
		ForceRemove: true,
		Labels:      labels,
		Version:     build.BuilderV1,
		Memory:      buildMemoryBytes,
	})
	if err != nil {
		return BuildResponse{}, err
	}
	defer result.Body.Close()

	out, buildErr := drainBuild(result.Body)
	log := tailText(out)
	if buildErr != nil {
		failure := classifyBuild(buildErr, logLines(log))
		// The log ends with the error, as docker build prints it; the
		// excerpt (computed above) keeps its existing rules.
		return BuildResponse{OK: false, Failure: failure, Log: appendLogLine(log, buildErr.Error())}, nil
	}

	info, err := d.inspectImage(ctx, tag, req.App)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return BuildResponse{OK: false, Log: log, Failure: &Failure{
				Stage: "build", Code: "build_failed",
				Message: "构建结束但没有生成镜像",
				Hint:    "检查 Dockerfile 是否成功产出镜像后重新部署",
			}}, nil
		}
		return BuildResponse{}, err
	}
	return BuildResponse{OK: true, Image: &info, Log: log}, nil
}

// buildMessage is one line of the BuilderV1 JSON stream.
type buildMessage struct {
	Stream      string `json:"stream"`
	Error       string `json:"error"`
	ErrorDetail struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
}

// drainBuild consumes the build stream into a BuildLogMaxBytes ring (ANSI
// codes removed) and returns the first error the daemon reports.
func drainBuild(r io.Reader) (*tailBuffer, error) {
	out := newTailBuffer(BuildLogMaxBytes)
	dec := json.NewDecoder(r)
	for {
		var m buildMessage
		if err := dec.Decode(&m); err == io.EOF {
			break
		} else if err != nil {
			return out, err
		}
		if m.Stream != "" {
			_, _ = out.Write([]byte(ansiPattern.ReplaceAllString(m.Stream, "")))
		}
		if m.Error != "" {
			return out, errors.New(m.Error)
		}
	}
	return out, nil
}

// appendLogLine adds line (ANSI removed, capped like an excerpt line) to a
// build log, keeping the log body within BuildLogMaxBytes.
func appendLogLine(log, line string) string {
	if log != "" && !strings.HasSuffix(log, "\n") {
		log += "\n"
	}
	log += truncateLine(ansiPattern.ReplaceAllString(line, ""), maxExcerptLineBytes) + "\n"
	return shrinkLog(log, BuildLogMaxBytes)
}

// logLines splits a build log into its non-blank lines, without the omitted
// header line.
func logLines(log string) []string {
	if m := omittedPattern.FindString(log); m != "" {
		log = log[len(m):]
	}
	var lines []string
	for _, l := range strings.Split(log, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, strings.TrimRight(l, "\r"))
		}
	}
	return lines
}

// maxExcerptLineBytes caps each line of a failure excerpt.
const maxExcerptLineBytes = 2 << 10

// buildExcerpt keeps the output of the failing step, stripped of Docker's step
// bookkeeping and terminal color codes, capped at MaxLogExcerptLine lines of
// at most maxExcerptLineBytes each.
func buildExcerpt(lines []string) string {
	start := 0
	for i, l := range lines {
		if strings.HasPrefix(l, "Step ") {
			start = i
		}
	}
	var out []string
	for _, l := range lines[start:] {
		l = strings.TrimRight(ansiPattern.ReplaceAllString(l, ""), " ")
		if strings.HasPrefix(strings.TrimSpace(l), "--->") || strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
	}
	if len(out) > MaxLogExcerptLine {
		out = out[len(out)-MaxLogExcerptLine:]
	}
	for i, l := range out {
		out[i] = truncateLine(l, maxExcerptLineBytes)
	}
	return strings.Join(out, "\n")
}

// classifyBuild turns a build error and its logs into a structured Failure.
func classifyBuild(err error, lines []string) *Failure {
	excerpt := buildExcerpt(lines)
	msg := truncateLine(ansiPattern.ReplaceAllString(err.Error(), ""), maxExcerptLineBytes)
	all := msg + "\n" + excerpt
	f := &Failure{
		Stage:      "build",
		Code:       "build_failed",
		Message:    "镜像构建失败: " + msg,
		LogExcerpt: excerpt,
		Hint:       "根据日志修改 Dockerfile 或依赖后重新部署",
	}
	switch {
	case registryTimeoutPattern.MatchString(all):
		f.Code = "registry_timeout"
		f.Hint = "构建时访问软件包源超时。服务器在中国大陆时，在 Dockerfile 中改用国内镜像源后重新部署：npm 加 --registry=https://registry.npmmirror.com；pip 加 -i https://mirrors.huaweicloud.com/repository/pypi/simple；Go 设 ENV GOPROXY=https://goproxy.cn,direct；Alpine 用 sed -i 's/dl-cdn.alpinelinux.org/mirrors.huaweicloud.com/g' /etc/apk/repositories"
	case pullFailurePattern.MatchString(all):
		f.Code = "base_image_not_found"
		f.Hint = "基础镜像拉取失败：先检查 FROM 里的镜像名；日志里是网络错误或 502、503 时，多半是镜像加速地址暂时不可用，稍后直接重新部署即可，反复失败就在 /etc/docker/daemon.json 换用可用的加速地址（阿里云服务器用控制台“容器镜像服务 → 镜像加速器”里的专属地址）"
	case missingModulePattern.MatchString(all):
		f.Code = "dependency_missing"
		f.Hint = "构建缺少依赖：把依赖写进依赖清单（如 requirements.txt / package.json）并在 Dockerfile 中安装"
	}
	return f
}

// maxContextBytes bounds the uncompressed size of a build context (tar bytes
// after gunzip). A variable so tests can lower it.
var maxContextBytes int64 = 1 << 30

// errContextTooLarge is returned by limitCounter past maxContextBytes.
var errContextTooLarge = errors.New("build context too large")

// limitCounter fails reads once more than limit bytes have passed through.
type limitCounter struct {
	r     io.Reader
	n     int64
	limit int64
}

func (l *limitCounter) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	l.n += int64(n)
	if l.n > l.limit {
		return n, errContextTooLarge
	}
	return n, err
}

// scanContext reads the upload once as a stream (gunzipping when it starts
// with the gzip magic bytes) and returns a Failure when it is not a valid tar,
// exceeds maxContextBytes uncompressed, or has no Dockerfile at the context
// root. It never holds more than a read buffer in memory.
func scanContext(r io.Reader) *Failure {
	invalid := &Failure{
		Stage: "upload", Code: "upload_invalid",
		Message: "上传的内容不是有效的 tar 或 tar.gz 目录包",
		Hint:    "使用 acornfox deploy 上传项目目录",
	}
	br := bufio.NewReader(r)
	var src io.Reader = br
	if magic, _ := br.Peek(2); isGzip(magic) {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return invalid
		}
		defer zr.Close()
		src = zr
	}
	lc := &limitCounter{r: src, limit: maxContextBytes}
	has, err := scanTarForRootDockerfile(lc)
	if err == nil {
		// Read past the tar end so a truncated or corrupt gzip stream is
		// caught here, not as a confusing build failure.
		_, err = io.Copy(io.Discard, lc)
	}
	if errors.Is(err, errContextTooLarge) {
		return &Failure{
			Stage: "upload", Code: "context_too_large",
			Message: "项目包解压后超过 " + humanBytes(maxContextBytes),
			Hint:    "在 .dockerignore 里排除依赖目录（如 node_modules）、构建产物、数据和大文件后重新部署",
		}
	}
	if err != nil {
		return invalid
	}
	if !has {
		return &Failure{
			Stage: "upload", Code: "dockerfile_missing",
			Message: "项目根目录没有 Dockerfile",
			Hint:    "在项目根目录添加 Dockerfile（写明 EXPOSE 端口）后重新部署",
		}
	}
	return nil
}

// humanBytes formats a size limit as GB, MB or KB.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30 && n%(1<<30) == 0:
		return strconv.FormatInt(n>>30, 10) + " GB"
	case n >= 1<<20:
		return strconv.FormatInt(n>>20, 10) + " MB"
	default:
		return strconv.FormatInt((n+1023)>>10, 10) + " KB"
	}
}

// isGzip reports whether b starts with the gzip magic bytes.
func isGzip(b []byte) bool {
	return len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b
}

// ImageInspect returns the managed image identified by ref (a tag or ID).
func (d *Docker) ImageInspect(ctx context.Context, app, ref string) (ImageInfo, error) {
	info, err := d.inspectImage(ctx, ref, app)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return ImageInfo{}, ErrNotFound
		}
		return ImageInfo{}, err
	}
	return info, nil
}

// PullImage pulls an external image Ref and tags it ImageTag(App, DeploymentID).
// If that tag already exists the existing image is returned without pulling.
// A pull that fails for a user-visible reason (image not found, timeout,
// registry error) comes back as PullResponse{OK:false} with a classified
// Failure; daemon/transport problems come back as an error.
func (d *Docker) PullImage(ctx context.Context, app, deploymentID, ref string) (PullResponse, error) {
	tag := ImageTag(app, deploymentID)

	// Fast path: this deployment's tag already exists (idempotent pull).
	if info, err := d.inspectByTag(ctx, app, tag); err == nil {
		return PullResponse{OK: true, Image: &info}, nil
	} else if !cerrdefs.IsNotFound(err) {
		return PullResponse{}, err
	}

	// Bound the pull to pullImageTimeout regardless of the caller's context.
	pctx, cancel := context.WithTimeout(ctx, pullImageTimeout)
	defer cancel()

	emulated := false
	local, werr := d.pull(pctx, ref, nil)
	if werr != nil && noMatchingPlatform(werr) && d.hostIsARM(ctx) {
		// Apple silicon: the image has no arm64 variant. Docker can run the
		// amd64 one under emulation; allow it and mark it as possibly slow.
		amd64 := []ocispec.Platform{{OS: "linux", Architecture: "amd64"}}
		if local, werr = d.pull(pctx, ref, amd64); werr == nil {
			emulated = true
		}
	}
	if werr != nil {
		if f := classifyPull(pctx, werr); f != nil {
			return PullResponse{OK: false, Failure: f}, nil
		}
		return PullResponse{}, werr
	}

	// Tag the pulled image as acornfox/<app>:<id> so it is owned by this app.
	if _, err := d.c().ImageTag(ctx, client.ImageTagOptions{Source: local, Target: tag}); err != nil {
		return PullResponse{}, err
	}

	info, err := d.inspectByTag(ctx, app, tag)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return PullResponse{OK: false, Failure: &Failure{
				Stage: "image", Code: "pull_failed",
				Message: "拉取结束但镜像不可用",
				Hint:    "检查镜像引用后重试",
			}}, nil
		}
		return PullResponse{}, err
	}
	return PullResponse{OK: true, Image: &info, Emulated: emulated}, nil
}

// pull pulls ref (optionally for the given platforms) and waits for it,
// falling back to registry mirrors when Docker Hub stalls (pull_fallback.go).
// It returns the local reference that holds the image: ref itself, or the
// mirror's name for an image pinned by digest.
func (d *Docker) pull(ctx context.Context, ref string, platforms []ocispec.Platform) (string, error) {
	return d.pullFallback(ctx, ref, platforms)
}

// noMatchingPlatform reports a pull that failed only because the image has no
// variant for the Docker host's platform.
func noMatchingPlatform(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no matching manifest") || strings.Contains(s, "does not provide the specified platform")
}

// hostIsARM reports whether the Docker host runs on 64-bit ARM.
func (d *Docker) hostIsARM(ctx context.Context) bool {
	info, err := d.c().Info(ctx, client.InfoOptions{})
	if err != nil {
		return false
	}
	a := strings.ToLower(info.Info.Architecture)
	return a == "aarch64" || a == "arm64"
}

// pullImageTimeout bounds a single image pull (contract: 10 minutes).
const pullImageTimeout = 10 * time.Minute

// pullNotFoundPattern matches "image not found / access denied" pull errors.
// Registry mirrors (e.g. DaoCloud) answer 403 for images they do not carry, so
// 403/404 from a registry count as "not found or not available".
var pullNotFoundPattern = regexp.MustCompile(`(?i)(manifest unknown|manifest for .* not found|not found|no such image|repository does not exist|pull access denied|unauthorized|access to the resource is denied|403 forbidden|404 not found)`)

// classifyPull turns a pull error into a user-facing Failure, or nil when the
// error is a daemon/transport problem the caller should surface as an error.
func classifyPull(ctx context.Context, err error) *Failure {
	if err == nil {
		return nil
	}
	// Timeout: our own deadline elapsed, or the daemon reported a timeout.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &Failure{
			Stage: "image", Code: "pull_timeout",
			Message: "拉取镜像超时",
			Hint:    "国内服务器拉取公共镜像常超时，请为 Docker 配置镜像加速后重试",
		}
	}
	msg := ansiPattern.ReplaceAllString(err.Error(), "")
	if cerrdefs.IsNotFound(err) || cerrdefs.IsUnauthorized(err) || pullNotFoundPattern.MatchString(msg) {
		return &Failure{
			Stage: "image", Code: "image_not_found",
			Message: "找不到镜像，或镜像加速源不提供该镜像：" + msg,
			Hint:    "检查镜像名与标签是否正确；若镜像确实存在，可能是加速源未收录，换用其他加速源或自行构建",
		}
	}
	if pullFailurePattern.MatchString(msg) {
		return &Failure{
			Stage: "image", Code: "pull_timeout",
			Message: "拉取镜像网络异常：" + msg,
			Hint:    "为 Docker 配置镜像加速后重试",
		}
	}
	// Any other daemon-reported pull error is a user-visible pull_failed.
	return &Failure{
		Stage: "image", Code: "pull_failed",
		Message: "拉取镜像失败：" + msg,
		Hint:    "检查镜像引用或稍后重试",
	}
}

// inspectByTag inspects the image carrying tag and confirms tag ownership: a
// pulled image has no acornfox labels, so ownership is established by the
// acornfox/<app>:<id> tag itself. It returns cerrdefs.ErrNotFound when the tag
// is absent or the image does not carry it.
func (d *Docker) inspectByTag(ctx context.Context, app, tag string) (ImageInfo, error) {
	res, err := d.c().ImageInspect(ctx, tag)
	if err != nil {
		return ImageInfo{}, err
	}
	if !hasTag(res.RepoTags, tag) {
		return ImageInfo{}, cerrdefs.ErrNotFound
	}
	info := imageInfoFromInspect(res)
	// Pulled images have no acornfox labels; derive app/deployment from the tag.
	info.App = app
	if _, id, ok := parseAppTag(tag); ok {
		info.DeploymentID = id
	}
	return info, nil
}

// hasTag reports whether tag is present in tags.
func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// appTagPattern matches acornfox/<app>:<deployment-id> tags.
var appTagPattern = regexp.MustCompile(`^acornfox/([a-z][a-z0-9-]{0,38}[a-z0-9]):([0-9a-f]{12})$`)

// parseAppTag extracts (app, deploymentID) from an acornfox/<app>:<id> tag.
func parseAppTag(tag string) (app, deploymentID string, ok bool) {
	m := appTagPattern.FindStringSubmatch(tag)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// ownsByTag reports whether any RepoTag is an acornfox/<app>: tag for app,
// establishing ownership of images we pulled (which carry no labels).
func ownsByTag(tags []string, app string) (deploymentID string, ok bool) {
	for _, t := range tags {
		if a, id, matched := parseAppTag(t); matched && a == app {
			return id, true
		}
	}
	return "", false
}

// inspectImage inspects ref and verifies it is a managed image of app.
// Ownership holds when the image carries our labels (built images) or an
// acornfox/<app>: RepoTag (pulled images have no labels, only the tag).
func (d *Docker) inspectImage(ctx context.Context, ref, app string) (ImageInfo, error) {
	res, err := d.c().ImageInspect(ctx, ref)
	if err != nil {
		return ImageInfo{}, err
	}
	info := imageInfoFromInspect(res)
	if info.App == app {
		return info, nil
	}
	// Pulled images have no labels: fall back to tag ownership.
	if id, ok := ownsByTag(res.RepoTags, app); ok {
		info.App = app
		if info.DeploymentID == "" {
			info.DeploymentID = id
		}
		return info, nil
	}
	// Not a managed image of this app: treat as absent.
	return ImageInfo{}, cerrdefs.ErrNotFound
}

// imageInfoFromInspect maps a Docker image inspect into ImageInfo.
func imageInfoFromInspect(res client.ImageInspectResult) ImageInfo {
	info := ImageInfo{
		ID:   res.ID,
		Tags: res.RepoTags,
		Size: res.Size,
	}
	if ts, err := time.Parse(time.RFC3339Nano, res.Created); err == nil {
		info.Created = ts.Unix()
	}
	if res.Config != nil {
		labels := res.Config.Labels
		info.App = labels[LabelApp]
		info.DeploymentID = labels[LabelDeployment]
		info.ExposedPorts = sortedTCPPorts(res.Config.ExposedPorts)
		info.Volumes = sortedKeys(res.Config.Volumes)
	}
	return info
}

// sortedTCPPorts returns ascending TCP port numbers from an ExposedPorts set.
func sortedTCPPorts(ports map[string]struct{}) []int {
	var out []int
	for spec := range ports {
		p, err := network.ParsePort(spec)
		if err != nil || p.Proto() != network.TCP {
			continue
		}
		out = append(out, int(p.Num()))
	}
	sort.Ints(out)
	return out
}

// sortedKeys returns the sorted keys of a set.
func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ListImages lists managed images, optionally filtered to one app. It returns
// both label-managed images (built) and images owned only by an
// acornfox/<app>: RepoTag (pulled).
func (d *Docker) ListImages(ctx context.Context, app string) ([]ImageInfo, error) {
	seen := map[string]ImageInfo{}

	// Label-managed images (built by us).
	labelFilters := client.Filters{}.Add("label", LabelManaged+"=1")
	if app != "" {
		labelFilters = labelFilters.Add("label", LabelApp+"="+app)
	}
	res, err := d.c().ImageList(ctx, client.ImageListOptions{Filters: labelFilters})
	if err != nil {
		return nil, err
	}
	for _, summary := range res.Items {
		if summary.Labels[LabelManaged] != "1" {
			continue
		}
		info, err := d.inspectImage(ctx, summary.ID, summary.Labels[LabelApp])
		if err != nil {
			continue
		}
		seen[info.ID] = info
	}

	// Tag-owned images (pulled, no labels): match acornfox/<app> references.
	ref := "acornfox/"
	if app != "" {
		ref = "acornfox/" + app
	}
	refFilters := client.Filters{}.Add("reference", ref+"*")
	tagged, err := d.c().ImageList(ctx, client.ImageListOptions{Filters: refFilters})
	if err != nil {
		return nil, err
	}
	for _, summary := range tagged.Items {
		id, owner, ok := ownerFromTags(summary.RepoTags, app)
		if !ok {
			continue
		}
		if _, dup := seen[summary.ID]; dup {
			continue
		}
		info, err := d.inspectImage(ctx, summary.ID, owner)
		if err != nil {
			continue
		}
		info.DeploymentID = id
		seen[summary.ID] = info
	}

	out := make([]ImageInfo, 0, len(seen))
	for _, info := range seen {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}

// ownerFromTags finds the acornfox app owning any of tags. When app is "" it
// accepts any acornfox app; otherwise it requires a match.
func ownerFromTags(tags []string, app string) (deploymentID, owner string, ok bool) {
	for _, t := range tags {
		a, id, matched := parseAppTag(t)
		if !matched {
			continue
		}
		if app == "" || a == app {
			return id, a, true
		}
	}
	return "", "", false
}

// RemoveImage removes a managed image of app. Missing is success.
func (d *Docker) RemoveImage(ctx context.Context, app, ref string) error {
	if _, err := d.inspectImage(ctx, ref, app); err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return err
	}
	_, err := d.c().ImageRemove(ctx, ref, client.ImageRemoveOptions{PruneChildren: true})
	if err != nil && cerrdefs.IsNotFound(err) {
		return nil
	}
	return err
}

// EnsureContainer creates the container if absent (idempotent by name), starts
// it if not running, and returns its observed state.
func (d *Docker) EnsureContainer(ctx context.Context, req EnsureContainerRequest) (ContainerInfo, error) {
	name := ContainerName(req.App, req.DeploymentID)

	role := req.Role
	if role == "" {
		role = RoleApp
	}
	var addon *AddonSpec
	switch role {
	case RoleApp:
	case RoleAddon:
		// Re-checked here (not only at the socket) so no caller of Docker can
		// run anything but the pinned add-on spec under the add-on role.
		spec, err := validateAddonRequest(req)
		if err != nil {
			return ContainerInfo{}, err
		}
		addon = &spec
	default:
		return ContainerInfo{}, fmt.Errorf("invalid role %q", req.Role)
	}

	// If it already exists, reuse it as-is (name is the idempotency key).
	inspect, err := d.c().ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err == nil {
		if err := d.startIfNeeded(ctx, name, inspect); err != nil {
			return ContainerInfo{}, err
		}
		return d.containerInfo(ctx, name)
	}
	if !cerrdefs.IsNotFound(err) {
		return ContainerInfo{}, err
	}

	if err := d.ensureNetwork(ctx, req.App); err != nil {
		return ContainerInfo{}, err
	}

	containerPort, ok := network.PortFrom(uint16(req.Port), network.TCP)
	if !ok {
		return ContainerInfo{}, fmt.Errorf("invalid container port %d", req.Port)
	}

	mounts, err := buildMounts(req.App, req.Mounts)
	if err != nil {
		return ContainerInfo{}, err
	}

	labels := map[string]string{
		LabelManaged:    "1",
		LabelApp:        req.App,
		LabelDeployment: req.DeploymentID,
		LabelRole:       role,
	}
	pids := int64(DefaultPidsLimit)
	memory := int64(req.MemoryMB) * (1 << 20)
	nanoCPUs := int64(req.CPUMilli) * 1e6

	cfg := &container.Config{
		Image:        req.Image,
		Labels:       labels,
		Env:          envSlice(req.Env),
		ExposedPorts: network.PortSet{containerPort: {}},
	}
	// App containers publish only on loopback; Caddy is the public entry point.
	// Add-ons publish nothing: they are reachable only by name inside
	// NetworkName(app).
	var bindings network.PortMap
	if addon == nil {
		bindings = network.PortMap{containerPort: {{
			HostIP:   netip.MustParseAddr("127.0.0.1"),
			HostPort: "0",
		}}}
	} else {
		if err := d.ensureAddonImage(ctx, addon.Image); err != nil {
			return ContainerInfo{}, err
		}
		if addon.Entrypoint != nil {
			cfg.Entrypoint = addon.Entrypoint
		}
		if hc := addon.Health; hc != nil {
			cfg.Healthcheck = &container.HealthConfig{
				Test:        hc.Test,
				Interval:    hc.Interval,
				Timeout:     hc.Timeout,
				StartPeriod: hc.StartPeriod,
				Retries:     hc.Retries,
			}
		}
	}

	created, err := d.c().ContainerCreate(ctx, client.ContainerCreateOptions{
		Name:   name,
		Config: cfg,
		HostConfig: &container.HostConfig{
			NetworkMode:   container.NetworkMode(NetworkName(req.App)),
			PortBindings:  bindings,
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
			SecurityOpt:   []string{"no-new-privileges"},
			LogConfig: container.LogConfig{
				Type:   "json-file",
				Config: map[string]string{"max-size": LogMaxSize, "max-file": LogMaxFiles},
			},
			Resources: container.Resources{
				Memory:     memory,
				MemorySwap: memory, // equal to Memory: no swap, so the limit is real and OOM is detectable
				NanoCPUs:   nanoCPUs,
				PidsLimit:  &pids,
			},
			Privileged: false,
			Mounts:     mounts,
		},
	})
	if err != nil {
		// A concurrent create may have won the race; reuse the existing one.
		if cerrdefs.IsConflict(err) {
			if _, ierr := d.c().ContainerInspect(ctx, name, client.ContainerInspectOptions{}); ierr == nil {
				if serr := d.startByName(ctx, name); serr != nil {
					return ContainerInfo{}, serr
				}
				return d.containerInfo(ctx, name)
			}
		}
		return ContainerInfo{}, err
	}

	if _, err := d.c().ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return ContainerInfo{}, err
	}
	return d.containerInfo(ctx, name)
}

// ensureAddonImage pulls a pinned add-on image if it is not present locally.
// The image is left untagged by AcornFox, so per-app image GC never removes it.
func (d *Docker) ensureAddonImage(ctx context.Context, ref string) error {
	if _, err := d.c().ImageInspect(ctx, ref); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return err
	}
	pctx, cancel := context.WithTimeout(ctx, pullImageTimeout)
	defer cancel()
	if _, err := d.pullFallback(pctx, ref, nil); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrPullFailed, ref, err)
	}
	return nil
}

// startIfNeeded starts an existing container only if it is not already running.
func (d *Docker) startIfNeeded(ctx context.Context, name string, inspect client.ContainerInspectResult) error {
	if inspect.Container.State != nil && inspect.Container.State.Running {
		return nil
	}
	return d.startByName(ctx, name)
}

// startByName starts a container by name, treating an already-started container
// as success.
func (d *Docker) startByName(ctx context.Context, name string) error {
	_, err := d.c().ContainerStart(ctx, name, client.ContainerStartOptions{})
	if err != nil && cerrdefs.IsConflict(err) {
		return nil
	}
	return err
}

// ensureNetwork creates the app's bridge network with managed labels if it does
// not already exist.
func (d *Docker) ensureNetwork(ctx context.Context, app string) error {
	name := NetworkName(app)
	if res, err := d.c().NetworkInspect(ctx, name, client.NetworkInspectOptions{}); err == nil {
		return checkNetworkOwner(app, res.Network)
	} else if !cerrdefs.IsNotFound(err) {
		return err
	}
	_, err := d.c().NetworkCreate(ctx, name, client.NetworkCreateOptions{
		Driver: "bridge",
		Labels: map[string]string{LabelManaged: "1", LabelApp: app},
	})
	// A concurrent creator may have claimed the name; re-check ownership.
	if err != nil && cerrdefs.IsConflict(err) {
		res, inspectErr := d.c().NetworkInspect(ctx, name, client.NetworkInspectOptions{})
		if inspectErr != nil {
			return inspectErr
		}
		return checkNetworkOwner(app, res.Network)
	}
	return err
}

// buildMounts converts request mounts into named-volume mounts, refusing any
// volume whose name is not prefixed with VolumePrefix(app).
func buildMounts(app string, in []Mount) ([]mount.Mount, error) {
	prefix := VolumePrefix(app)
	var out []mount.Mount
	for _, m := range in {
		if !strings.HasPrefix(m.Volume, prefix) {
			return nil, fmt.Errorf("mount volume %q must start with %q", m.Volume, prefix)
		}
		if !path.IsAbs(m.Path) || path.Clean(m.Path) != m.Path {
			return nil, fmt.Errorf("mount path %q must be absolute and clean", m.Path)
		}
		out = append(out, mount.Mount{
			Type:   mount.TypeVolume,
			Source: m.Volume,
			Target: m.Path,
		})
	}
	return out, nil
}

// envSlice converts an env map into a sorted KEY=VALUE slice.
func envSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(env))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

// ListContainers lists managed containers, optionally filtered to one app.
func (d *Docker) ListContainers(ctx context.Context, app string) ([]ContainerInfo, error) {
	filters := client.Filters{}.Add("label", LabelManaged+"=1")
	if app != "" {
		filters = filters.Add("label", LabelApp+"="+app)
	}
	res, err := d.c().ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return nil, err
	}
	var out []ContainerInfo
	for _, summary := range res.Items {
		if summary.Labels[LabelManaged] != "1" {
			continue
		}
		info, err := d.containerInfoByID(ctx, summary.ID)
		if err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// containerInfo inspects a container by name and maps it to ContainerInfo.
func (d *Docker) containerInfo(ctx context.Context, name string) (ContainerInfo, error) {
	return d.containerInfoByID(ctx, name)
}

// containerInfoByID inspects a container and maps its observed state.
func (d *Docker) containerInfoByID(ctx context.Context, id string) (ContainerInfo, error) {
	res, err := d.c().ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return ContainerInfo{}, err
	}
	c := res.Container
	info := ContainerInfo{
		ID:           c.ID,
		Name:         strings.TrimPrefix(c.Name, "/"),
		Image:        c.Image,
		RestartCount: c.RestartCount,
	}
	if c.Config != nil {
		info.App = c.Config.Labels[LabelApp]
		info.DeploymentID = c.Config.Labels[LabelDeployment]
		info.Role = c.Config.Labels[LabelRole]
	}
	if c.State != nil {
		info.State = string(c.State.Status)
		info.Running = c.State.Running
		info.Restarting = c.State.Restarting
		info.ExitCode = c.State.ExitCode
		info.OOMKilled = c.State.OOMKilled
		info.StartedAt = c.State.StartedAt
		if c.State.Health != nil && c.State.Health.Status != container.NoHealthcheck {
			info.Health = string(c.State.Health.Status)
		}
	}
	info.HostPort, info.Port = firstHostPort(c.NetworkSettings)
	return info, nil
}

// firstHostPort returns the lowest host port published for the container and
// the container port it maps to, or zeros when none is published.
func firstHostPort(ns *container.NetworkSettings) (int, int) {
	if ns == nil {
		return 0, 0
	}
	best, port := 0, 0
	for cport, bindings := range ns.Ports {
		for _, b := range bindings {
			p, err := strconv.Atoi(b.HostPort)
			if err != nil || p <= 0 {
				continue
			}
			if best == 0 || p < best {
				best, port = p, int(cport.Num())
			}
		}
	}
	return best, port
}

// StopContainer stops a managed container. Missing is success.
func (d *Docker) StopContainer(ctx context.Context, app, name string) error {
	c, err := d.managedContainer(ctx, app, name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = d.c().ContainerStop(ctx, c.ID, client.ContainerStopOptions{})
	if cerrdefs.IsNotFound(err) {
		return nil
	}
	return err
}

// UpdateResources changes the memory and CPU limits of a labeled container of
// app in place. Swap stays equal to memory, as at creation, so the limit stays
// real. Docker refuses a memory limit below current usage; that error is
// returned as-is.
func (d *Docker) UpdateResources(ctx context.Context, req UpdateResourcesRequest) error {
	c, err := d.managedContainer(ctx, req.App, req.Name)
	if err != nil {
		return err
	}
	memory := int64(req.MemoryMB) * (1 << 20)
	_, err = d.c().ContainerUpdate(ctx, c.ID, client.ContainerUpdateOptions{
		Resources: &container.Resources{
			Memory:     memory,
			MemorySwap: memory,
			NanoCPUs:   int64(req.CPUMilli) * 1e6,
		},
	})
	if cerrdefs.IsNotFound(err) {
		return ErrNotFound
	}
	return err
}

// StartContainer starts only a labeled container of app.
func (d *Docker) StartContainer(ctx context.Context, app, name string) (ContainerInfo, error) {
	c, err := d.managedContainer(ctx, app, name)
	if err != nil {
		return ContainerInfo{}, err
	}
	if err := d.startByName(ctx, c.ID); err != nil {
		if cerrdefs.IsNotFound(err) {
			return ContainerInfo{}, ErrNotFound
		}
		return ContainerInfo{}, err
	}
	return d.containerInfoByID(ctx, c.ID)
}

// RemoveContainer force-removes only a labeled container of app. Missing is success.
func (d *Docker) RemoveContainer(ctx context.Context, app, name string) error {
	c, err := d.managedContainer(ctx, app, name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = d.c().ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true})
	if cerrdefs.IsNotFound(err) {
		return nil
	}
	return err
}

// logsMaxBytes bounds the raw output Logs keeps (the tail is what matters),
// so that the reply stays within the client's response bound.
const logsMaxBytes = 2 << 20

// Logs returns the last tail lines (default 100, at most 1000) of a managed
// container, stdout and stderr interleaved by timestamp, ANSI removed. Docker
// sends only those lines, and they pass through a bounded ring, so a container
// with a large log costs no more memory than a small one.
func (d *Docker) Logs(ctx context.Context, app, name string, tail int) ([]string, error) {
	c, err := d.managedContainer(ctx, app, name)
	if err != nil {
		return nil, err
	}
	if tail <= 0 {
		tail = 100
	}
	if tail > MaxLogLines {
		tail = MaxLogLines
	}
	rc, err := d.c().ContainerLogs(ctx, c.ID, client.ContainerLogsOptions{
		ShowStdout: true, ShowStderr: true, Timestamps: true, Tail: strconv.Itoa(tail),
	})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer rc.Close()
	out := newTailBuffer(logsMaxBytes)
	if c.Config.Tty {
		_, err = io.Copy(out, rc)
	} else {
		_, err = stdcopy.StdCopy(out, out, rc)
	}
	if err != nil {
		return nil, err
	}
	data, _ := alignTail(out.Bytes(), out.Dropped())
	return timestampedLines(data, tail), nil
}

// timestampedLines turns "<RFC3339Nano> <text>" lines into their texts,
// ordered by timestamp (stable, so equal stamps keep their order), ANSI and
// trailing CR removed, keeping the last tail. A line without a valid stamp
// takes the stamp of the line before it.
func timestampedLines(data []byte, tail int) []string {
	type rec struct {
		at   time.Time
		text string
	}
	raw := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(data) == 0 {
		raw = nil
	}
	recs := make([]rec, 0, len(raw))
	var last time.Time
	for _, l := range raw {
		text := l
		if ts, rest, ok := strings.Cut(l, " "); ok {
			if at, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				last, text = at, rest
			}
		}
		recs = append(recs, rec{at: last, text: ansiPattern.ReplaceAllString(strings.TrimSuffix(text, "\r"), "")})
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].at.Before(recs[j].at) })
	if len(recs) > tail {
		recs = recs[len(recs)-tail:]
	}
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.text)
	}
	return out
}

// ContainerStats never queries unowned or stopped containers.
func (d *Docker) ContainerStats(ctx context.Context, app, name string) (StatsResponse, error) {
	c, err := d.managedContainer(ctx, app, name)
	if err != nil {
		return StatsResponse{}, err
	}
	if c.State == nil || !c.State.Running {
		return StatsResponse{}, ErrStopped
	}
	stats, err := d.c().ContainerStats(ctx, c.ID, client.ContainerStatsOptions{Stream: false, IncludePreviousSample: true})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return StatsResponse{}, ErrNotFound
		}
		return StatsResponse{}, err
	}
	defer stats.Body.Close()
	var v container.StatsResponse
	if err := json.NewDecoder(stats.Body).Decode(&v); err != nil {
		return StatsResponse{}, err
	}
	return containerStatsView(v), nil
}

// dbFilePattern matches database-like filenames in the container diff.
var dbFilePattern = regexp.MustCompile(`(?i)\.(db|sqlite|sqlite3|db-wal|db-journal)$`)

// Diff returns database-like files added or changed in the writable layer,
// excluding paths under the container's mounts.
func (d *Docker) Diff(ctx context.Context, app, name string) ([]string, error) {
	owned, err := d.managedContainer(ctx, app, name)
	if err != nil {
		return nil, err
	}
	var mountTargets []string
	for _, mp := range owned.Mounts {
		if mp.Destination != "" {
			mountTargets = append(mountTargets, path.Clean(mp.Destination))
		}
	}

	res, err := d.c().ContainerDiff(ctx, owned.ID, client.ContainerDiffOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var out []string
	for _, ch := range res.Changes {
		clean := path.Clean(ch.Path)
		base := path.Base(clean)
		if !dbFilePattern.MatchString(base) {
			continue
		}
		if underAnyMount(clean, mountTargets) {
			continue
		}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out, nil
}

// underAnyMount reports whether p is at or under any mount target.
func underAnyMount(p string, targets []string) bool {
	for _, t := range targets {
		if p == t || strings.HasPrefix(p, t+"/") {
			return true
		}
	}
	return false
}

// EnsureVolume creates the named volume if absent. The name must be prefixed
// with VolumePrefix(app).
func (d *Docker) EnsureVolume(ctx context.Context, app, name string) error {
	if !strings.HasPrefix(name, VolumePrefix(app)) {
		return fmt.Errorf("volume %q must start with %q", name, VolumePrefix(app))
	}
	_, err := d.c().VolumeCreate(ctx, client.VolumeCreateOptions{
		Name:   name,
		Driver: "local",
		Labels: map[string]string{LabelManaged: "1", LabelApp: app},
	})
	return err
}

// RemoveVolume deletes a named volume that is managed and labeled for app.
// A missing volume is success. A volume still attached to a container is an
// error (it is never force-removed). The label check matters because volume
// name prefixes alone can overlap between apps ("af-a-" vs "af-a-b-").
func (d *Docker) RemoveVolume(ctx context.Context, app, name string) error {
	if !strings.HasPrefix(name, VolumePrefix(app)) {
		return fmt.Errorf("volume %q must start with %q", name, VolumePrefix(app))
	}
	res, err := d.c().VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return err
	}
	if res.Volume.Labels[LabelManaged] != "1" || res.Volume.Labels[LabelApp] != app {
		return fmt.Errorf("volume %q is not managed for app %q", name, app)
	}
	if _, err := d.c().VolumeRemove(ctx, name, client.VolumeRemoveOptions{}); err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// ListVolumes lists managed volumes, optionally filtered to one app.
func (d *Docker) ListVolumes(ctx context.Context, app string) ([]VolumeInfo, error) {
	filters := client.Filters{}.Add("label", LabelManaged+"=1")
	if app != "" {
		filters = filters.Add("label", LabelApp+"="+app)
	}
	res, err := d.c().VolumeList(ctx, client.VolumeListOptions{Filters: filters})
	if err != nil {
		return nil, err
	}
	var out []VolumeInfo
	for _, v := range res.Items {
		if v.Labels[LabelManaged] != "1" {
			continue
		}
		out = append(out, VolumeInfo{
			Name:    v.Name,
			App:     v.Labels[LabelApp],
			Created: v.CreatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// scanTarForRootDockerfile reads the whole tar stream (so that its size is
// counted and its framing validated) and reports whether it contains a
// Dockerfile at the context root (./Dockerfile or Dockerfile).
func scanTarForRootDockerfile(r io.Reader) (bool, error) {
	tr := tar.NewReader(r)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return found, nil
		}
		if err != nil {
			return false, err
		}
		if strings.TrimPrefix(h.Name, "./") == "Dockerfile" {
			found = true
		}
	}
}
