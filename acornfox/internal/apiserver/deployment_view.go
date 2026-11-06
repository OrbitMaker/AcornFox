package apiserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/deploylog"
	"github.com/acornfox/acornfox/internal/state"
)

// deploymentOut is a deployment as returned by the API: every stored field
// (none is ever removed) plus the display texts and rollback facts computed
// by the server, so the console and the CLI never keep their own tables.
type deploymentOut struct {
	state.Deployment
	StatusText string `json:"status_text"`
	SourceText string `json:"source_text"`
	// DurationSeconds is how long the deployment took: until it went live
	// (live, retired) or until it failed. null when unknown, e.g. records
	// from before go-live events were kept, or still in progress.
	DurationSeconds *int `json:"duration_seconds"`
	// CanRollback is true on the one record per kept image that a version
	// list offers "roll back" on; RollbackBlocked says why not otherwise
	// (state.Blocked*), SameImageAs which version has the same program.
	CanRollback     bool   `json:"can_rollback"`
	RollbackBlocked string `json:"rollback_blocked"`
	SameImageAs     int    `json:"same_image_as"`
	// RollbackText is the one-line rollback note shown next to a version
	// ("可回退", "太旧，不能一键回退", ...), empty when there is nothing to say.
	RollbackText string `json:"rollback_text,omitempty"`
	// FailureText is "停在<阶段>：<原因>" for a failed deployment.
	FailureText string         `json:"failure_text,omitempty"`
	Logs        deploymentLogs `json:"logs"`
}

// deploymentLogs says which logs GET /v1/deployments/{id}/logs can return.
type deploymentLogs struct {
	Build bool `json:"build"`
	Run   bool `json:"run"`
}

// deploymentViewer computes deploymentOut for deployments of one app. Its zero
// value is valid: without a plan every deployment is classified by its status
// alone and no duration or archived log is reported.
type deploymentViewer struct {
	liveID    string
	plan      state.RollbackPlan
	liveTimes map[string]time.Time
	logs      *deploylog.Store
	// imageCheck is "ok" when the images on this machine were checked, or
	// "unknown" when the runner could not list them (only the kept window
	// was applied).
	imageCheck string
}

// newDeploymentViewer reads what the views of show (deployments of app a)
// need: every deployment of the app for the rollback plan, the go-live times
// of show, and when checkImages is set, the images actually on this machine.
// Every read is best-effort: a failure leaves that part of the view empty
// instead of failing the request.
func (s *server) newDeploymentViewer(ctx context.Context, a state.App, show []state.Deployment, checkImages bool) *deploymentViewer {
	v := &deploymentViewer{liveID: a.CurrentDeployment, logs: s.deployLogs}
	var present map[string]bool
	if checkImages {
		present = s.presentImages(ctx, a.Name)
		v.imageCheck = "ok"
		if present == nil {
			v.imageCheck = "unknown"
		}
	}
	all, err := s.store.ListDeployments(ctx, a.Name, 0)
	if err != nil {
		s.log.Warn("list deployments for view", "app", a.Name, "err", err)
		all = show
	}
	v.plan = state.ClassifyRollback(all, a.CurrentDeployment, present)
	ids := make([]string, 0, len(show))
	for _, d := range show {
		if d.Status == state.StatusLive || d.Status == state.StatusRetired {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) > 0 {
		times, err := s.store.LiveTimes(ctx, ids)
		if err != nil {
			s.log.Warn("live times", "app", a.Name, "err", err)
		}
		v.liveTimes = times
	}
	return v
}

// presentImages lists the app's images on this machine, or returns nil when
// the runner cannot be asked (nil means "unknown" to state.ClassifyRollback).
func (s *server) presentImages(ctx context.Context, app string) map[string]bool {
	images, err := s.runner.ListImages(ctx, app)
	if err != nil {
		s.log.Warn("list images", "app", app, "err", err)
		return nil
	}
	present := make(map[string]bool, len(images))
	for _, img := range images {
		if img.ID != "" {
			present[img.ID] = true
		}
	}
	return present
}

// view returns the wire form of d.
func (v *deploymentViewer) view(d state.Deployment) deploymentOut {
	out := deploymentOut{
		Deployment: d,
		StatusText: deployStatusText(d.Status),
		SourceText: deploymentSourceText(d),
	}
	st, ok := v.plan.ByID[d.ID]
	if !ok {
		st = statusOnlyRollback(d, v.liveID)
	}
	out.CanRollback, out.RollbackBlocked, out.SameImageAs = st.CanRollback, st.Blocked, st.SameImageAs
	out.RollbackText = rollbackText(st)
	if d.Status == state.StatusFailed && d.Diagnosis != nil {
		out.FailureText = "停在" + stageName(d.Diagnosis.Stage) + "：" + d.Diagnosis.Message
	}
	out.DurationSeconds = v.duration(d)
	if v.logs != nil {
		out.Logs.Build, out.Logs.Run = v.logs.Has(d.App, d.ID)
		if readsLiveContainer(d, v.liveID) {
			out.Logs.Run = true
		}
	}
	return out
}

// views converts a list, keeping its order.
func (v *deploymentViewer) views(ds []state.Deployment) []deploymentOut {
	out := make([]deploymentOut, 0, len(ds))
	for _, d := range ds {
		out = append(out, v.view(d))
	}
	return out
}

// duration is go-live time minus creation for live and retired deployments,
// finish time minus creation for failed ones, else nil. It includes the time
// spent queued, which is a few seconds on a single machine.
func (v *deploymentViewer) duration(d state.Deployment) *int {
	var end time.Time
	switch d.Status {
	case state.StatusLive, state.StatusRetired:
		at, ok := v.liveTimes[d.ID]
		if !ok {
			return nil
		}
		end = at
	case state.StatusFailed:
		if d.FinishedAt == nil {
			return nil
		}
		end = *d.FinishedAt
	default:
		return nil
	}
	if d.CreatedAt.IsZero() {
		return nil
	}
	secs := int(end.Sub(d.CreatedAt).Round(time.Second) / time.Second)
	if secs < 0 {
		secs = 0
	}
	return &secs
}

// statusOnlyRollback classifies a deployment missing from the plan (created
// after the plan was read, or no plan at all) by its status alone; it never
// offers a rollback without the plan.
func statusOnlyRollback(d state.Deployment, liveID string) state.RollbackStatus {
	switch {
	case (liveID != "" && d.ID == liveID) || d.Status == state.StatusLive:
		return state.RollbackStatus{Blocked: state.BlockedLive}
	case d.Status == state.StatusFailed:
		return state.RollbackStatus{Blocked: state.BlockedFailed}
	case d.Status == state.StatusSuperseded:
		return state.RollbackStatus{Blocked: state.BlockedSuperseded}
	case d.Status == state.StatusRetired:
		return state.RollbackStatus{Blocked: state.BlockedImageGone}
	default:
		return state.RollbackStatus{Blocked: state.BlockedPending}
	}
}

// readsLiveContainer reports whether the run log of d is read from its
// container (the serving version, or one being started or checked) rather
// than from the archive.
func readsLiveContainer(d state.Deployment, liveID string) bool {
	if liveID != "" && d.ID == liveID {
		return true
	}
	switch d.Status {
	case state.StatusLive, state.StatusStarting, state.StatusChecking, state.StatusRouting:
		return true
	}
	return false
}

// The display texts below are the console's wording; the console and the CLI
// show these fields instead of keeping their own copies.

var deployStatusTexts = map[string]string{
	state.StatusLive:       "正在服务",
	state.StatusRetired:    "已被新版本替换",
	state.StatusFailed:     "部署失败",
	state.StatusSuperseded: "已被更新的提交取代",
	state.StatusQueued:     "排队中",
	state.StatusBuilding:   "构建中",
	state.StatusStarting:   "启动中",
	state.StatusChecking:   "健康检查中",
	state.StatusRouting:    "切换流量中",
}

func deployStatusText(status string) string {
	if t, ok := deployStatusTexts[status]; ok {
		return t
	}
	return status
}

// sourceLabel names a source kind.
func sourceLabel(kind string) string {
	switch kind {
	case state.SourceImage:
		return "镜像"
	case state.SourceGit:
		return "Git"
	}
	return "上传"
}

var addonNames = map[string]string{
	state.AddonPostgres: "PostgreSQL",
	state.AddonMySQL:    "MySQL",
	state.AddonRedis:    "Redis",
}

// reasonText explains why a deployment re-using an image exists, e.g.
// "添加 PostgreSQL（基于第 1 版）". Empty for a fresh source.
func reasonText(d state.Deployment) string {
	if d.Reason == "" {
		return ""
	}
	kind, addon, _ := strings.Cut(d.Reason, ":")
	addonName := addonNames[addon]
	if addonName == "" {
		addonName = addon
	}
	if addonName == "" {
		addonName = "数据库"
	}
	var what string
	switch kind {
	case state.ReasonRedeploy:
		what = "重新部署"
	case state.ReasonRollback:
		what = "回退"
	case state.ReasonAddonAdd:
		what = "添加 " + addonName
	case state.ReasonAddonRemove:
		what = "移除 " + addonName
	default:
		what = d.Reason
	}
	if d.BasedOnSeq > 0 {
		return fmt.Sprintf("%s（基于第 %d 版）", what, d.BasedOnSeq)
	}
	return what
}

// deploymentSourceText is the original source plus the reason, e.g.
// "上传 · 回退（基于第 5 版）". Redeploying an upload is still "上传".
func deploymentSourceText(d state.Deployment) string {
	kind := d.OriginKind
	if kind == "" {
		kind = d.SourceKind
	}
	parts := []string{sourceLabel(kind)}
	if r := reasonText(d); r != "" {
		parts = append(parts, r)
	}
	return strings.Join(parts, " · ")
}

// stageNames maps a diagnosis stage to the deployment step it belongs to.
var stageNames = map[string]string{
	"upload": "上传",
	"build":  "构建",
	"runner": "构建",
	"start":  "启动",
	"addon":  "启动",
	"health": "健康检查",
	"data":   "健康检查",
	"route":  "切换流量",
}

func stageName(stage string) string {
	if n, ok := stageNames[stage]; ok {
		return n
	}
	return "部署"
}

// rollbackText is the note shown next to a version in a version list.
func rollbackText(st state.RollbackStatus) string {
	switch {
	case st.CanRollback:
		return "可回退"
	case st.Blocked == state.BlockedImageGone:
		return "太旧，不能一键回退"
	case st.Blocked == state.BlockedSameAsLive, st.Blocked == state.BlockedOlderDuplicate:
		if st.SameImageAs > 0 {
			return fmt.Sprintf("与第 %d 版是同一个程序", st.SameImageAs)
		}
	}
	return ""
}
