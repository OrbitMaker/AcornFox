package apiserver

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/deploylog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// deploymentLogsReply is the GET /v1/deployments/{id}/logs reply.
type deploymentLogsReply struct {
	DeploymentID string   `json:"deployment_id"`
	Version      int      `json:"version"`
	Kind         string   `json:"kind"`
	Lines        []string `json:"lines"`
	Truncated    bool     `json:"truncated"`
	// Source is "live" when read from the running container, "archive" when
	// read from the log saved before its container was removed.
	Source string `json:"source"`
}

// deploymentLogs implements GET /v1/deployments/{id}/logs?kind=run|build&tail=N.
//
// The run log of the serving version, and of a version being started or
// checked, is read from its container; every other log comes from the
// archive written by the reconciler. All lines are masked again with the
// app's current secrets; when those cannot be read nothing is returned.
func (s *server) deploymentLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if !deploylog.ValidDeploymentID(id) {
		writeError(w, http.StatusBadRequest, "invalid_deployment", "部署 ID 不合法：应为 12 位小写十六进制")
		return
	}
	kind := deploylog.Kind(r.URL.Query().Get("kind"))
	if kind == "" {
		kind = deploylog.KindRun
	}
	if kind != deploylog.KindRun && kind != deploylog.KindBuild {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind 只能是 run（运行日志）或 build（构建日志）")
		return
	}
	tail := deploylog.DefaultTail
	if raw := r.URL.Query().Get("tail"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			writeError(w, http.StatusBadRequest, "invalid_tail", "tail 必须是正整数")
			return
		}
		tail = min(v, deploylog.MaxTail)
	}

	dep, err := s.store.GetDeployment(ctx, id)
	if err != nil {
		s.mapStoreError(w, err)
		return
	}
	a, err := s.store.GetApp(ctx, dep.App)
	if err != nil {
		s.mapStoreError(w, err)
		return
	}
	if s.deployLogs == nil {
		writeError(w, http.StatusNotFound, "logs_not_kept", fmt.Sprintf("第 %d 版的日志没有保存：这台服务器没有开启保存部署日志", dep.Seq))
		return
	}
	switch {
	case kind == deploylog.KindBuild && (dep.Status == state.StatusQueued || dep.Status == state.StatusBuilding):
		writeError(w, http.StatusConflict, "logs_pending", fmt.Sprintf("第 %d 版还在构建，构建结束后才能看构建日志", dep.Seq))
		return
	case kind == deploylog.KindRun && (dep.Status == state.StatusQueued || dep.Status == state.StatusBuilding):
		writeError(w, http.StatusConflict, "logs_pending", fmt.Sprintf("第 %d 版还没有启动，启动后才能看运行日志", dep.Seq))
		return
	}

	secrets, err := s.logSecrets(ctx, dep.App)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "redaction_unavailable", "无法安全读取日志：密钥脱敏配置不可用")
		return
	}
	redactor := deploylog.NewRedactor(secrets...)
	reply := deploymentLogsReply{DeploymentID: dep.ID, Version: dep.Seq, Kind: string(kind)}

	if kind == deploylog.KindRun && readsLiveContainer(dep, a.CurrentDeployment) {
		liveTail := min(tail, runner.MaxLogLines)
		lines, err := s.runner.Logs(ctx, dep.App, runner.ContainerName(dep.App, dep.ID), liveTail)
		switch {
		case err == nil:
			reply.Lines = redactLines(lines, redactor)
			reply.Truncated = len(lines) >= liveTail
			reply.Source = "live"
			writeJSON(w, http.StatusOK, reply)
			return
		case !errors.Is(err, runner.ErrNotFound):
			writeError(w, http.StatusServiceUnavailable, "runner_unavailable", "无法读取容器日志，请稍后重试")
			return
		}
		// The container is gone (replaced in the meantime): try the archive.
	}

	lines, truncated, err := s.deployLogs.Read(dep.App, dep.ID, kind, tail)
	switch {
	case errors.Is(err, deploylog.ErrNotKept) && kind == deploylog.KindRun && state.IsPending(dep.Status):
		writeError(w, http.StatusConflict, "logs_pending", fmt.Sprintf("第 %d 版正在启动，容器就绪后才能看运行日志", dep.Seq))
		return
	case errors.Is(err, deploylog.ErrNotKept):
		writeError(w, http.StatusNotFound, "logs_not_kept", s.notKeptMessage(dep, kind))
		return
	case errors.Is(err, deploylog.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_deployment", "部署 ID 或应用名不合法")
		return
	case err != nil:
		s.log.Error("read deployment log", "deployment", dep.ID, "kind", kind, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "读取日志失败")
		return
	}
	reply.Lines = redactLines(lines, redactor)
	reply.Truncated = truncated
	reply.Source = "archive"
	writeJSON(w, http.StatusOK, reply)
}

// notKeptMessage explains why a log is missing: no rebuild happened, the
// deployment predates log keeping, or the log was pruned.
func (s *server) notKeptMessage(dep state.Deployment, kind deploylog.Kind) string {
	if kind == deploylog.KindBuild && dep.Reason != "" {
		if dep.BasedOnSeq > 0 {
			return fmt.Sprintf("第 %d 版复用第 %d 版的程序，没有重新构建", dep.Seq, dep.BasedOnSeq)
		}
		return fmt.Sprintf("第 %d 版复用已有的程序，没有重新构建", dep.Seq)
	}
	if dep.CreatedAt.Before(s.deployLogs.Since()) {
		return fmt.Sprintf("第 %d 版部署时还没有保存日志的功能", dep.Seq)
	}
	return fmt.Sprintf("第 %d 版的日志已超出保留范围，已删除", dep.Seq)
}

// redactLines masks secrets across line boundaries (a value may contain a
// newline) and always returns a non-nil slice.
func redactLines(lines []string, r *deploylog.Redactor) []string {
	if len(lines) == 0 {
		return []string{}
	}
	return strings.Split(r.Redact(strings.Join(lines, "\n")), "\n")
}
