package main

// The entry proxy line of `acornfox doctor` on a Linux server (fault codes
// EDGE-01..04; the full entries are in internal/faults, `acornfox faults
// EDGE-01`). It looks at the entry container through Docker and asks the
// adapter in it, with the management token from the data directory, whether
// it works. Both need sudo; without Docker access the line is left out (the
// Docker line says why). Read only.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	dockerclient "github.com/moby/moby/client"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/edgeclient"
	"github.com/acornfox/acornfox/internal/runner"
)

// doctorEdgeHealth asks the adapter for its health (tests replace it).
var doctorEdgeHealth = func(ctx context.Context, port int, token string) edge.Health {
	return edgeclient.New(edgeAdminURL(port), token).Health(ctx)
}

func doctorEdge(ctx context.Context, r *doctorReport, dc *dockerclient.Client, dataDir string) {
	if dc == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	name := runner.EdgeContainerName
	if !runner.NewDocker(dc).EdgeImagePresent(ctx) {
		r.add(false, edge.FaultImageMissing, "入口代理：本机没有入口镜像 %s（它随发行版的入口包导入：重新运行一次升级命令即可补上）", edge.Image)
	}
	res, err := dc.ContainerInspect(ctx, name, dockerclient.ContainerInspectOptions{})
	if err != nil || res.Container.State == nil || !res.Container.State.Running {
		r.add(false, edge.FaultNotRunning, "入口代理：容器 %s 没有在运行，应用网址都打不开（看 sudo docker ps -a --filter label=%s=1 和 sudo docker logs --tail 50 %s）", name, runner.LabelEdge, name)
		return
	}
	labels := map[string]string{}
	if res.Container.Config != nil {
		labels = res.Container.Config.Labels
	}
	if h := res.Container.State.Health; h != nil && h.Status == "unhealthy" {
		last := ""
		if n := len(h.Log); n > 0 {
			last = strings.TrimSpace(h.Log[n-1].Output)
		}
		r.add(false, edge.FaultNotApplied, "入口代理：容器自己的健康检查没通过（%s）；查看 sudo docker exec %s %s health", firstLine(last), name, edge.AdapterBinary)
		return
	}
	port := defaultEdgeAdminPort
	if p, err := strconv.Atoi(labels[runner.LabelEdgeAdmin]); err == nil && p > 0 {
		port = p
	}
	b, err := os.ReadFile(filepath.Join(dataDir, edgeTokenFile))
	if err != nil {
		r.add(true, edge.FaultNotRunning, "入口代理：容器 %s 运行中（入口 %s）；读不到管理令牌，管理口没查，要查用 sudo acornfox doctor", name, labels[runner.LabelEdgeImage])
		return
	}
	if h := doctorEdgeHealth(ctx, port, strings.TrimSpace(string(b))); !h.OK() {
		// The client's reason already names the code for callers that show
		// it alone; doctor prints the code after the line, so drop it here.
		r.add(false, edge.FaultAdmin, "入口代理：%s", withoutCode(h.Reason, edge.FaultAdmin))
		return
	}
	r.add(true, edge.FaultNotRunning, "入口代理：容器 %s 运行中（入口 %s，管理口 127.0.0.1:%d 要令牌）", name, labels[runner.LabelEdgeImage], port)
}

// withoutCode takes "（故障编号 X）" and "，故障编号 X）" out of a reason, so a
// doctor line that already ends with （X） does not name X twice.
func withoutCode(reason, code string) string {
	return strings.NewReplacer("（故障编号 "+code+"）", "", "，故障编号 "+code+"）", "）").Replace(reason)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "没有输出"
	}
	return s
}
