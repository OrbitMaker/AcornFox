package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Rollback blocked reasons, as returned in RollbackStatus.Blocked. "" means
// the deployment can be rolled back to.
const (
	BlockedLive           = "live"            // it is the version serving now
	BlockedFailed         = "failed"          // it never passed its health check
	BlockedSuperseded     = "superseded"      // a newer submission replaced it before it went live
	BlockedPending        = "pending"         // it is still being deployed
	BlockedImageGone      = "image_gone"      // its image is outside the kept window or no longer on this machine
	BlockedSameAsLive     = "same_as_live"    // its image is the one serving now
	BlockedOlderDuplicate = "older_duplicate" // a newer retired deployment has the same image
)

// Rollback error codes (RollbackError.Code). The API maps them to HTTP
// statuses; they are never renamed.
const (
	RollbackVersionNotFound        = "version_not_found"
	RollbackVersionIsLive          = "version_is_live"
	RollbackVersionSameAsLive      = "version_same_as_live"
	RollbackVersionNotRollbackable = "version_not_rollbackable"
	RollbackVersionImageGone       = "version_image_gone"
	RollbackDeployInProgress       = "deploy_in_progress"
	RollbackNoTarget               = "no_rollback_target"
)

// imageIDPrefix marks a source_ref that is a local image ID rather than a
// pullable reference.
const imageIDPrefix = "sha256:"

// RollbackStatus says whether one deployment can be rolled back to.
type RollbackStatus struct {
	// CanRollback is true for the deployments a version list offers a
	// rollback button on: only the newest retired deployment of each kept,
	// present image that differs from the live one.
	CanRollback bool
	// Blocked is "" or one of the Blocked* values.
	Blocked string
	// SameImageAs is the live seq for BlockedSameAsLive, the seq of the newest
	// retired deployment with the same image for BlockedOlderDuplicate, else 0.
	SameImageAs int
}

// RollbackPlan classifies every deployment of an app for rollback.
type RollbackPlan struct {
	TargetSeq int                       // where a rollback without a version goes; 0 = nowhere
	ByID      map[string]RollbackStatus // keyed by deployment ID
}

// AllowsExplicit reports whether a rollback that names this version is
// allowed: everything CanRollback plus older records of a kept image.
func (st RollbackStatus) AllowsExplicit() bool {
	return st.Blocked == "" || st.Blocked == BlockedOlderDuplicate
}

// keptRetired returns the retired deployments whose images are kept for
// rollback: scanning retired deployments with an image from newest to oldest,
// it skips images already seen (the live image counts as seen) and stops after
// KeepVersions distinct images. Each kept image is represented by its newest
// retired deployment. deps may be in any order.
func keptRetired(deps []Deployment, liveImage string) []Deployment {
	retired := make([]Deployment, 0, len(deps))
	for _, d := range deps {
		if d.Status == StatusRetired && d.ImageID != "" {
			retired = append(retired, d)
		}
	}
	sort.Slice(retired, func(i, j int) bool { return retired[i].Seq > retired[j].Seq })
	seen := map[string]bool{}
	if liveImage != "" {
		seen[liveImage] = true
	}
	var out []Deployment
	for _, d := range retired {
		if len(out) >= KeepVersions {
			break
		}
		if seen[d.ImageID] {
			continue
		}
		seen[d.ImageID] = true
		out = append(out, d)
	}
	return out
}

// keptImageDeployments is the pure form of Store.KeptImageDeployments: the
// live deployment, keptRetired, and the newest deployment when it failed with
// a built image.
func keptImageDeployments(deps []Deployment, liveID string) []Deployment {
	var out []Deployment
	seen := map[string]bool{}
	liveImage := ""
	for _, d := range deps {
		if liveID != "" && d.ID == liveID {
			out = append(out, d)
			seen[d.ID] = true
			liveImage = d.ImageID
			break
		}
	}
	for _, d := range keptRetired(deps, liveImage) {
		if !seen[d.ID] {
			out = append(out, d)
			seen[d.ID] = true
		}
	}
	// The newest deployment, when it failed after its image was built, is
	// kept too: adding the missing database from its diagnosis redeploys
	// exactly that image. Older failed images are still collected.
	var newest *Deployment
	for i := range deps {
		if newest == nil || deps[i].Seq > newest.Seq {
			newest = &deps[i]
		}
	}
	if newest != nil && newest.Status == StatusFailed && newest.ImageID != "" && !seen[newest.ID] {
		out = append(out, *newest)
	}
	return out
}

// ClassifyRollback decides, for every deployment of an app, whether it can be
// rolled back to. It touches no database so the version list and the rollback
// endpoint share one rule, and that rule is the same window image GC keeps.
//
// deps is every deployment of the app in any order; liveID is
// apps.current_deployment; present is the set of image IDs actually on this
// machine, or nil when unknown (treated as all present).
func ClassifyRollback(deps []Deployment, liveID string, present map[string]bool) RollbackPlan {
	plan := RollbackPlan{ByID: make(map[string]RollbackStatus, len(deps))}

	liveSeq, liveImage := 0, ""
	for _, d := range deps {
		if liveID != "" && d.ID == liveID {
			liveSeq, liveImage = d.Seq, d.ImageID
			break
		}
	}
	window := map[string]bool{}
	for _, d := range keptRetired(deps, liveImage) {
		window[d.ImageID] = true
	}
	// newestRetired maps an image to the seq of its newest retired deployment.
	newestRetired := map[string]int{}
	for _, d := range deps {
		if d.Status == StatusRetired && d.ImageID != "" && d.Seq > newestRetired[d.ImageID] {
			newestRetired[d.ImageID] = d.Seq
		}
	}

	for _, d := range deps {
		var st RollbackStatus
		switch {
		case (liveID != "" && d.ID == liveID) || d.Status == StatusLive:
			st.Blocked = BlockedLive
		case d.Status == StatusFailed:
			st.Blocked = BlockedFailed
		case d.Status == StatusSuperseded:
			st.Blocked = BlockedSuperseded
		case d.Status != StatusRetired:
			st.Blocked = BlockedPending
		case d.ImageID != "" && d.ImageID == liveImage:
			st.Blocked, st.SameImageAs = BlockedSameAsLive, liveSeq
		case d.ImageID == "" || !window[d.ImageID] || (present != nil && !present[d.ImageID]):
			st.Blocked = BlockedImageGone
		case newestRetired[d.ImageID] > d.Seq:
			st.Blocked, st.SameImageAs = BlockedOlderDuplicate, newestRetired[d.ImageID]
		default:
			st.CanRollback = true
			if d.Seq > plan.TargetSeq {
				plan.TargetSeq = d.Seq
			}
		}
		plan.ByID[d.ID] = st
	}
	return plan
}

// RollbackRequest is the input to Store.CreateRollbackDeployment.
type RollbackRequest struct {
	App           string
	ToSeq         int             // 0 = roll back to RollbackPlan.TargetSeq
	Present       map[string]bool // image IDs on this machine; nil = unknown (see ClassifyRollback)
	RequestKey    string          // client Idempotency-Key, may be empty
	CancelPending bool            // create the rollback even while another deployment is in progress
}

// RollbackResult is the output of Store.CreateRollbackDeployment.
type RollbackResult struct {
	Deployment    Deployment
	Created       bool // false when an existing deployment was returned (idempotent retry)
	TargetSeq     int  // the version rolled back to (recorded as based_on_seq)
	SupersededSeq int  // the in-progress version the reconciler will supersede, 0 = none
}

// RollbackError is the refusal of a rollback. errors.As retrieves it; the API
// maps Code to an HTTP status.
type RollbackError struct {
	Code         string // Rollback* code
	Seq          int    // the version concerned; for deploy_in_progress, the in-progress one
	LiveSeq      int    // the version serving now, 0 = none
	Alternatives []int  // versions that can be rolled back to, newest first
}

// Error returns one user-facing sentence in Chinese.
func (e *RollbackError) Error() string {
	switch e.Code {
	case RollbackVersionNotFound:
		return fmt.Sprintf("没有第 %d 版", e.Seq)
	case RollbackVersionIsLive:
		return fmt.Sprintf("第 %d 版就是正在服务的版本", e.Seq)
	case RollbackVersionSameAsLive:
		return fmt.Sprintf("第 %d 版和正在服务的第 %d 版是同一个程序，不需要回退", e.Seq, e.LiveSeq)
	case RollbackVersionNotRollbackable:
		return fmt.Sprintf("第 %d 版没有成功上线过，不能回退到它", e.Seq)
	case RollbackVersionImageGone:
		msg := fmt.Sprintf("第 %d 版太旧，已经不能一键回退（只保留最近 %d 个版本）", e.Seq, KeepVersions)
		if len(e.Alternatives) == 0 {
			return msg + "。没有其他可回退的版本"
		}
		parts := make([]string, len(e.Alternatives))
		for i, s := range e.Alternatives {
			parts[i] = strconv.Itoa(s)
		}
		return msg + "。可以回到第 " + strings.Join(parts, "、") + " 版"
	case RollbackDeployInProgress:
		return fmt.Sprintf("第 %d 版正在部署，回退会取消它", e.Seq)
	case RollbackNoTarget:
		return "没有可回退的历史版本"
	}
	return "无法回退：" + e.Code
}

// CreateRollbackDeployment checks and creates a rollback deployment in one
// transaction, so the check uses exactly the rows the insert sees and image
// GC (which keeps pending deployments' images) cannot remove the target in
// between.
//
// Order: a pending-or-live deployment with the same RequestKey is returned as
// is; then the target is chosen and validated with ClassifyRollback; then a
// pending deployment already rolling back to the same image is returned as is;
// then any other pending deployment refuses the rollback unless CancelPending,
// which marks every pending deployment superseded in the same transaction and
// reports the newest one (SupersededSeq).
//
// Refusals are *RollbackError; a missing app is ErrNotFound.
func (s *Store) CreateRollbackDeployment(ctx context.Context, req RollbackRequest) (RollbackResult, error) {
	if !ValidAppName(req.App) {
		return RollbackResult{}, fmt.Errorf("%w: app name %q", ErrInvalid, req.App)
	}
	if req.ToSeq < 0 {
		return RollbackResult{}, fmt.Errorf("%w: version %d", ErrInvalid, req.ToSeq)
	}
	var res RollbackResult
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		a, err := scanAppTx(ctx, tx, req.App)
		if err != nil {
			return err
		}
		if req.RequestKey != "" {
			existing, err := scanDeploymentByRequestKeyTx(ctx, tx, req.App, req.RequestKey)
			switch {
			case err == nil && !deploymentEnded(existing.Status):
				res = RollbackResult{Deployment: existing, TargetSeq: existing.BasedOnSeq}
				return nil
			case err == nil:
				// Same rule as CreateDeployment: an ended deployment releases
				// its key, so the same request means "do it again".
				if _, err := tx.ExecContext(ctx, `UPDATE deployments SET request_key='' WHERE id=?`, existing.ID); err != nil {
					return fmt.Errorf("release request key: %w", err)
				}
			case !errors.Is(err, ErrNotFound):
				return err
			}
		}

		deps, err := listDeploymentsTx(ctx, tx, req.App)
		if err != nil {
			return err
		}
		plan := ClassifyRollback(deps, a.CurrentDeployment, req.Present)
		bySeq := make(map[int]Deployment, len(deps))
		liveSeq := 0
		var newestPending *Deployment
		for i, d := range deps {
			bySeq[d.Seq] = d
			if a.CurrentDeployment != "" && d.ID == a.CurrentDeployment {
				liveSeq = d.Seq
			}
			if IsPending(d.Status) && (newestPending == nil || d.Seq > newestPending.Seq) {
				newestPending = &deps[i]
			}
		}
		refuse := func(code string, seq int) error {
			return &RollbackError{Code: code, Seq: seq, LiveSeq: liveSeq, Alternatives: rollbackAlternatives(deps, plan)}
		}

		toSeq := req.ToSeq
		if toSeq == 0 {
			if plan.TargetSeq == 0 {
				return refuse(RollbackNoTarget, 0)
			}
			toSeq = plan.TargetSeq
		}
		target, ok := bySeq[toSeq]
		if !ok {
			return refuse(RollbackVersionNotFound, toSeq)
		}
		st := plan.ByID[target.ID]
		switch st.Blocked {
		case "", BlockedOlderDuplicate:
		case BlockedLive:
			return refuse(RollbackVersionIsLive, toSeq)
		case BlockedSameAsLive:
			return refuse(RollbackVersionSameAsLive, toSeq)
		case BlockedImageGone:
			return refuse(RollbackVersionImageGone, toSeq)
		default: // failed, superseded, pending
			return refuse(RollbackVersionNotRollbackable, toSeq)
		}

		if newestPending != nil {
			// A second click on the same rollback returns the first one.
			if newestPending.Reason == ReasonRollback && newestPending.SourceDigest == target.ImageID {
				res = RollbackResult{Deployment: *newestPending, TargetSeq: newestPending.BasedOnSeq}
				return nil
			}
			if !req.CancelPending {
				return refuse(RollbackDeployInProgress, newestPending.Seq)
			}
			// Cancel now rather than on the reconciler's next round: a build
			// already in progress would otherwise run on and go live first.
			// Its later status writes hit a terminal row and stop; converge
			// removes whatever container it left.
			nowText := formatTime(s.nowUTC())
			for _, d := range deps {
				if !IsPending(d.Status) {
					continue
				}
				if _, err := tx.ExecContext(ctx, `UPDATE deployments
					SET status=?, updated_at=?, finished_at=COALESCE(finished_at, ?)
					WHERE id=?`, StatusSuperseded, nowText, nowText, d.ID); err != nil {
					return fmt.Errorf("cancel pending: %w", err)
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO events (app, deployment_id, at, stage, message)
					VALUES (?, ?, ?, ?, ?)`, req.App, d.ID, nowText, "deploy", "已因回退取消"); err != nil {
					return fmt.Errorf("cancel pending event: %w", err)
				}
			}
			res.SupersededSeq = newestPending.Seq
		}

		origin := target.OriginKind
		if origin == "" {
			origin = target.SourceKind
		}
		dep, err := insertDeploymentTx(ctx, tx, NewDeployment{
			App:          req.App,
			SourceKind:   SourceImage,
			SourceRef:    target.ImageID,
			SourceDigest: target.ImageID,
			RequestKey:   req.RequestKey,
			BasedOnSeq:   target.Seq,
			OriginKind:   origin,
			Reason:       ReasonRollback,
		})
		if err != nil {
			return err
		}
		res.Deployment, res.Created, res.TargetSeq = dep, true, target.Seq
		return nil
	})
	if err != nil {
		return RollbackResult{}, err
	}
	return res, nil
}

// rollbackAlternatives lists the versions with CanRollback, newest first.
func rollbackAlternatives(deps []Deployment, plan RollbackPlan) []int {
	var out []int
	for _, d := range deps {
		if plan.ByID[d.ID].CanRollback {
			out = append(out, d.Seq)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

// KeptImageIDs returns the image IDs that image GC must keep for app, computed
// in one transaction: the images of KeptImageDeployments, the image of every
// pending deployment, and the sha256: source_ref of pending image-source
// deployments (a queued rollback whose image is not resolved yet).
func (s *Store) KeptImageIDs(ctx context.Context, app string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		a, err := scanAppTx(ctx, tx, app)
		if err != nil {
			return err
		}
		deps, err := listDeploymentsTx(ctx, tx, app)
		if err != nil {
			return err
		}
		for _, d := range keptImageDeployments(deps, a.CurrentDeployment) {
			if d.ImageID != "" {
				out[d.ImageID] = struct{}{}
			}
		}
		for _, d := range deps {
			if !IsPending(d.Status) {
				continue
			}
			if d.ImageID != "" {
				out[d.ImageID] = struct{}{}
			}
			if d.SourceKind == SourceImage && strings.HasPrefix(d.SourceRef, imageIDPrefix) {
				out[d.SourceRef] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// liveEventPrefix is the start of the reconciler's "went live" event message.
const liveEventPrefix = "已上线"

// liveTimesChunk keeps each IN (...) list well under SQLite's parameter limit.
const liveTimesChunk = 500

// LiveTimes returns, for each given deployment ID that has one, the time of
// its first "went live" event (stage deploy, message starting with 已上线).
// IDs without such an event are absent from the map.
func (s *Store) LiveTimes(ctx context.Context, deploymentIDs []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	ids := make([]string, 0, len(deploymentIDs))
	seen := map[string]bool{}
	for _, id := range deploymentIDs {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for start := 0; start < len(ids); start += liveTimesChunk {
		end := min(start+liveTimesChunk, len(ids))
		chunk := ids[start:end]
		args := make([]any, 0, len(chunk)+2)
		for _, id := range chunk {
			args = append(args, id)
		}
		args = append(args, "deploy", liveEventPrefix+"%")
		q := `SELECT deployment_id, MIN(at) FROM events
			WHERE deployment_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",") + `)
			AND stage=? AND message LIKE ?
			GROUP BY deployment_id`
		if err := func() error {
			rows, err := s.db.QueryContext(ctx, q, args...)
			if err != nil {
				return fmt.Errorf("live times: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var id, atText string
				if err := rows.Scan(&id, &atText); err != nil {
					return fmt.Errorf("scan live time: %w", err)
				}
				at, err := parseTime(atText)
				if err != nil {
					return err
				}
				out[id] = at
			}
			return rows.Err()
		}(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// listDeploymentsTx returns every deployment of app, newest first.
func listDeploymentsTx(ctx context.Context, tx *sql.Tx, app string) ([]Deployment, error) {
	rows, err := tx.QueryContext(ctx, deploymentSelect+` FROM deployments WHERE app=? ORDER BY seq DESC`, app)
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	defer rows.Close()
	return scanDeploymentRows(rows)
}
