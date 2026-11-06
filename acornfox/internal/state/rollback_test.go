package state

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// rollbackFixture builds deployment histories on a real store.
type rollbackFixture struct {
	t   *testing.T
	s   *Store
	app string
	n   int
}

func newRollbackFixture(t *testing.T) *rollbackFixture {
	t.Helper()
	s := openTest(t)
	if _, _, err := s.EnsureApp(context.Background(), "web"); err != nil {
		t.Fatal(err)
	}
	return &rollbackFixture{t: t, s: s, app: "web"}
}

// upload creates a fresh upload deployment that ends in status with image.
func (f *rollbackFixture) upload(image, status string) Deployment {
	f.t.Helper()
	f.n++
	return f.finish(NewDeployment{App: f.app, SourceKind: SourceUpload, SourceRef: "/u", SourceDigest: fmt.Sprintf("digest-%d", f.n)}, image, status)
}

// reuse creates a deployment that re-uses an existing image (redeploy, addon
// change, rollback), as the API does.
func (f *rollbackFixture) reuse(base Deployment, reason, status string) Deployment {
	f.t.Helper()
	origin := base.OriginKind
	if origin == "" {
		origin = base.SourceKind
	}
	return f.finish(NewDeployment{App: f.app, SourceKind: SourceImage, SourceRef: base.ImageID, SourceDigest: base.ImageID,
		BypassDedup: true, BasedOnSeq: base.Seq, OriginKind: origin, Reason: reason}, base.ImageID, status)
}

// fill uploads KeepVersions-3 more distinct live images. The histories below
// are written for a window of three; a fill placed among them keeps their
// shape (which versions are just inside or outside the window) for any
// KeepVersions.
func (f *rollbackFixture) fill() []Deployment {
	f.t.Helper()
	var out []Deployment
	for i := 1; i <= KeepVersions-3; i++ {
		out = append(out, f.upload(fmt.Sprintf("sha256:fill-%d", i), StatusLive))
	}
	return out
}

func (f *rollbackFixture) finish(in NewDeployment, image, status string) Deployment {
	f.t.Helper()
	ctx := context.Background()
	d, created, err := f.s.CreateDeployment(ctx, in)
	if err != nil || !created {
		f.t.Fatalf("create deployment: created=%v err=%v", created, err)
	}
	next := status
	if status == StatusLive {
		next = StatusRouting
	}
	if status != StatusQueued {
		if _, err := f.s.UpdateDeployment(ctx, d.ID, func(x *Deployment) error {
			x.Status, x.ImageID = next, image
			return nil
		}); err != nil {
			f.t.Fatal(err)
		}
	}
	if status == StatusLive {
		if err := f.s.SetCurrentDeployment(ctx, f.app, d.ID); err != nil {
			f.t.Fatal(err)
		}
	}
	d, err = f.s.GetDeployment(ctx, d.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return d
}

func (f *rollbackFixture) plan(present map[string]bool) (RollbackPlan, map[int]Deployment) {
	f.t.Helper()
	ctx := context.Background()
	deps, err := f.s.ListDeployments(ctx, f.app, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	a, err := f.s.GetApp(ctx, f.app)
	if err != nil {
		f.t.Fatal(err)
	}
	bySeq := map[int]Deployment{}
	for _, d := range deps {
		bySeq[d.Seq] = d
	}
	return ClassifyRollback(deps, a.CurrentDeployment, present), bySeq
}

func (f *rollbackFixture) keptSeqs() map[int]bool {
	f.t.Helper()
	kept, err := f.s.KeptImageDeployments(context.Background(), f.app)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[int]bool{}
	for _, d := range kept {
		out[d.Seq] = true
	}
	return out
}

func wantStatus(t *testing.T, plan RollbackPlan, d Deployment, can bool, blocked string, same int) {
	t.Helper()
	got := plan.ByID[d.ID]
	if got.CanRollback != can || got.Blocked != blocked || got.SameImageAs != same {
		t.Errorf("seq %d: got %+v, want can=%v blocked=%q same=%d", d.Seq, got, can, blocked, same)
	}
}

// Redeploys and addon changes of one image must not use up the kept window:
// the window counts distinct images, and each image is offered once.
func TestRollbackWindowCountsDistinctImages(t *testing.T) {
	f := newRollbackFixture(t)
	a := f.upload("sha256:a", StatusLive)
	b := f.upload("sha256:b", StatusLive)
	fill := f.fill()
	c := f.upload("sha256:c", StatusLive)
	c4 := f.reuse(c, ReasonRedeploy, StatusLive)
	c5 := f.reuse(c4, ReasonAddonAdd+":postgres", StatusLive)
	d := f.upload("sha256:d", StatusLive)

	plan, _ := f.plan(nil)
	wantStatus(t, plan, d, false, BlockedLive, 0)
	for _, x := range fill {
		wantStatus(t, plan, x, true, "", 0)
	}
	wantStatus(t, plan, c5, true, "", 0)
	wantStatus(t, plan, c4, false, BlockedOlderDuplicate, c5.Seq)
	wantStatus(t, plan, c, false, BlockedOlderDuplicate, c5.Seq)
	wantStatus(t, plan, b, true, "", 0)
	wantStatus(t, plan, a, true, "", 0)
	if plan.TargetSeq != c5.Seq {
		t.Fatalf("TargetSeq = %d, want %d", plan.TargetSeq, c5.Seq)
	}
	// The old per-record rule kept only c, c4, c5; now a and b are kept too.
	kept := f.keptSeqs()
	for _, x := range append(fill, d, c5, b, a) {
		if seq := x.Seq; !kept[seq] {
			t.Errorf("seq %d not kept: %v", seq, kept)
		}
	}
	if kept[c.Seq] || kept[c4.Seq] {
		t.Errorf("older records of a kept image are returned once only: %v", kept)
	}

	// One more distinct image pushes the oldest out of the window.
	e := f.upload("sha256:e", StatusLive)
	plan, _ = f.plan(nil)
	wantStatus(t, plan, e, false, BlockedLive, 0)
	wantStatus(t, plan, d, true, "", 0)
	wantStatus(t, plan, b, true, "", 0)
	wantStatus(t, plan, a, false, BlockedImageGone, 0)
	if kept := f.keptSeqs(); kept[a.Seq] || len(kept) != 1+KeepVersions {
		t.Errorf("kept after e = %v", kept)
	}

	// Rolling back to c makes c's records the same program as the live one.
	back := f.reuse(c5, ReasonRollback, StatusLive)
	plan, _ = f.plan(nil)
	wantStatus(t, plan, c5, false, BlockedSameAsLive, back.Seq)
	wantStatus(t, plan, c, false, BlockedSameAsLive, back.Seq)
	wantStatus(t, plan, e, true, "", 0)
	// The live image does not count towards the window, so a is back in it.
	wantStatus(t, plan, a, false, BlockedImageGone, 0)
	wantStatus(t, plan, b, true, "", 0)
	if plan.TargetSeq != e.Seq {
		t.Fatalf("TargetSeq = %d, want %d", plan.TargetSeq, e.Seq)
	}
}

// Images removed before this rule existed (or by hand) are only visible
// through present; records outside the window are never offered.
func TestRollbackPresentAndLegacyRecords(t *testing.T) {
	f := newRollbackFixture(t)
	old := f.upload("sha256:old", StatusLive)
	noImage := f.upload("", StatusLive) // a record from before images were tracked
	b := f.upload("sha256:b", StatusLive)
	c := f.upload("sha256:c", StatusLive)
	live := f.upload("sha256:live", StatusLive)

	plan, _ := f.plan(nil)
	wantStatus(t, plan, c, true, "", 0)
	wantStatus(t, plan, b, true, "", 0)
	wantStatus(t, plan, noImage, false, BlockedImageGone, 0)
	wantStatus(t, plan, old, true, "", 0)
	wantStatus(t, plan, live, false, BlockedLive, 0)

	// The old image was pruned by the previous rule; c was removed by hand.
	present := map[string]bool{"sha256:b": true, "sha256:live": true}
	plan, _ = f.plan(present)
	wantStatus(t, plan, old, false, BlockedImageGone, 0)
	wantStatus(t, plan, c, false, BlockedImageGone, 0)
	wantStatus(t, plan, b, true, "", 0)
	if plan.TargetSeq != b.Seq {
		t.Fatalf("TargetSeq = %d, want %d", plan.TargetSeq, b.Seq)
	}
	// An empty non-nil set means "nothing is present".
	plan, _ = f.plan(map[string]bool{})
	if plan.TargetSeq != 0 {
		t.Fatalf("TargetSeq with no images = %d", plan.TargetSeq)
	}
}

func TestClassifyRollbackStatusesAnyOrder(t *testing.T) {
	deps := []Deployment{
		{ID: "000000000005", Seq: 5, Status: StatusBuilding},
		{ID: "000000000001", Seq: 1, Status: StatusRetired, ImageID: "sha256:a"},
		{ID: "000000000004", Seq: 4, Status: StatusSuperseded},
		{ID: "000000000003", Seq: 3, Status: StatusFailed, ImageID: "sha256:f"},
		{ID: "000000000002", Seq: 2, Status: StatusLive, ImageID: "sha256:b"},
		{ID: "000000000006", Seq: 6, Status: StatusQueued},
	}
	plan := ClassifyRollback(deps, "000000000002", nil)
	want := map[string]string{
		"000000000001": "",
		"000000000002": BlockedLive,
		"000000000003": BlockedFailed,
		"000000000004": BlockedSuperseded,
		"000000000005": BlockedPending,
		"000000000006": BlockedPending,
	}
	for id, blocked := range want {
		if got := plan.ByID[id]; got.Blocked != blocked || got.CanRollback != (blocked == "") {
			t.Errorf("%s: %+v, want blocked %q", id, got, blocked)
		}
	}
	if plan.TargetSeq != 1 {
		t.Fatalf("TargetSeq = %d", plan.TargetSeq)
	}
	if !plan.ByID["000000000001"].AllowsExplicit() || plan.ByID["000000000003"].AllowsExplicit() {
		t.Fatal("AllowsExplicit")
	}
	if (RollbackStatus{Blocked: BlockedOlderDuplicate}).AllowsExplicit() != true {
		t.Fatal("older duplicates may be named explicitly")
	}
	if empty := ClassifyRollback(nil, "", nil); empty.TargetSeq != 0 || len(empty.ByID) != 0 {
		t.Fatalf("empty plan = %+v", empty)
	}
}

func rollbackCode(t *testing.T, err error) *RollbackError {
	t.Helper()
	var re *RollbackError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want *RollbackError", err)
	}
	return re
}

func TestCreateRollbackDeploymentValidation(t *testing.T) {
	ctx := context.Background()
	f := newRollbackFixture(t)
	a := f.upload("sha256:a", StatusLive)
	b := f.upload("sha256:b", StatusLive)
	b3 := f.reuse(b, ReasonRedeploy, StatusLive)
	fill := f.fill()
	c := f.upload("sha256:c", StatusLive)
	d := f.upload("sha256:d", StatusLive)
	failed := f.upload("sha256:f", StatusFailed)
	e := f.upload("sha256:e", StatusLive)
	same := f.reuse(d, ReasonAddonAdd+":redis", StatusLive) // same image as d

	// The window, newest first: e, c, the fill images, b3 (d is live).
	alts := []string{fmt.Sprint(e.Seq), fmt.Sprint(c.Seq)}
	for i := len(fill) - 1; i >= 0; i-- {
		alts = append(alts, fmt.Sprint(fill[i].Seq))
	}
	alts = append(alts, fmt.Sprint(b3.Seq))

	before, _ := f.s.ListDeployments(ctx, "web", 0)
	cases := []struct {
		name  string
		to    int
		code  string
		seq   int
		inMsg string
	}{
		{"missing", 99, RollbackVersionNotFound, 99, "没有第 99 版"},
		{"live", same.Seq, RollbackVersionIsLive, same.Seq, "就是正在服务的版本"},
		{"same image as live", d.Seq, RollbackVersionSameAsLive, d.Seq, fmt.Sprintf("和正在服务的第 %d 版是同一个程序", same.Seq)},
		{"failed", failed.Seq, RollbackVersionNotRollbackable, failed.Seq, "没有成功上线过"},
		{"outside window", a.Seq, RollbackVersionImageGone, a.Seq, fmt.Sprintf("（只保留最近 %d 个版本）。可以回到第 %s 版", KeepVersions, strings.Join(alts, "、"))},
	}
	for _, tc := range cases {
		_, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: tc.to})
		re := rollbackCode(t, err)
		if re.Code != tc.code || re.Seq != tc.seq || re.LiveSeq != same.Seq {
			t.Errorf("%s: %+v", tc.name, re)
		}
		if !strings.Contains(re.Error(), tc.inMsg) {
			t.Errorf("%s: message %q lacks %q", tc.name, re.Error(), tc.inMsg)
		}
	}
	if after, _ := f.s.ListDeployments(ctx, "web", 0); len(after) != len(before) {
		t.Fatalf("refused rollbacks created deployments: %d -> %d", len(before), len(after))
	}

	// A version present in the window but missing on disk.
	_, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: c.Seq,
		Present: map[string]bool{"sha256:d": true, "sha256:e": true, "sha256:b": true}})
	if re := rollbackCode(t, err); re.Code != RollbackVersionImageGone || len(re.Alternatives) != 2 {
		t.Fatalf("present filter: %+v", re)
	}

	// An older record of a kept image may be named; it is recorded as the base.
	res, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: b.Seq})
	if err != nil || !res.Created || res.TargetSeq != b.Seq || res.SupersededSeq != 0 {
		t.Fatalf("older duplicate rollback: %+v err=%v", res, err)
	}
	got := res.Deployment
	if got.SourceKind != SourceImage || got.SourceRef != "sha256:b" || got.SourceDigest != "sha256:b" ||
		got.BasedOnSeq != b.Seq || got.OriginKind != SourceUpload || got.Reason != ReasonRollback ||
		got.Status != StatusQueued || got.Seq != same.Seq+1 {
		t.Fatalf("rollback deployment = %+v", got)
	}
	_ = b3

	if _, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "nope", ToSeq: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing app err = %v", err)
	}
	if _, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: -1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative version err = %v", err)
	}
}

func TestCreateRollbackDeploymentDefaultTarget(t *testing.T) {
	ctx := context.Background()
	f := newRollbackFixture(t)
	first := f.upload("sha256:a", StatusLive)
	_, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web"})
	if re := rollbackCode(t, err); re.Code != RollbackNoTarget || re.Error() != "没有可回退的历史版本" {
		t.Fatalf("no target: %+v", re)
	}
	// Same image redeployed: nothing different to go back to.
	f.reuse(first, ReasonRedeploy, StatusLive)
	_, err = f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web"})
	if re := rollbackCode(t, err); re.Code != RollbackNoTarget {
		t.Fatalf("redeploy only: %+v", re)
	}

	b := f.upload("sha256:b", StatusLive)
	f.upload("sha256:f", StatusFailed) // a failed newest version is never a target
	res, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web"})
	if err != nil || !res.Created {
		t.Fatalf("default rollback: %+v err=%v", res, err)
	}
	if res.TargetSeq != 2 || res.Deployment.BasedOnSeq != 2 || res.Deployment.SourceRef != "sha256:a" {
		t.Fatalf("default rollback went to %+v (live %d)", res, b.Seq)
	}
}

func TestCreateRollbackDeploymentPendingAndIdempotency(t *testing.T) {
	ctx := context.Background()
	f := newRollbackFixture(t)
	a := f.upload("sha256:a", StatusLive)
	b := f.upload("sha256:b", StatusLive)
	c := f.upload("sha256:c", StatusLive)
	building := f.upload("", StatusBuilding)

	_, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: b.Seq})
	if re := rollbackCode(t, err); re.Code != RollbackDeployInProgress || re.Seq != building.Seq || re.LiveSeq != c.Seq ||
		re.Error() != fmt.Sprintf("第 %d 版正在部署，回退会取消它", building.Seq) {
		t.Fatalf("in progress: %+v %q", re, re.Error())
	}

	res, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: b.Seq, CancelPending: true, RequestKey: "k1"})
	if err != nil || !res.Created || res.SupersededSeq != building.Seq || res.TargetSeq != b.Seq {
		t.Fatalf("cancel pending: %+v err=%v", res, err)
	}
	rb := res.Deployment
	if rb.RequestKey != "k1" {
		t.Fatalf("request key not stored: %+v", rb)
	}
	// The cancelled deployment is superseded at once and can no longer go
	// live, even if a reconciler round was still driving it.
	cancelled, _ := f.s.GetDeployment(ctx, building.ID)
	if cancelled.Status != StatusSuperseded || cancelled.FinishedAt == nil {
		t.Fatalf("cancelled deployment: %+v", cancelled)
	}
	if err := f.s.SetCurrentDeployment(ctx, "web", building.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("superseded deployment promoted: %v", err)
	}
	if evs, _ := f.s.ListEvents(ctx, "web", building.ID, 0, 10); len(evs) == 0 || evs[len(evs)-1].Message != "已因回退取消" {
		t.Fatalf("cancel event: %+v", evs)
	}

	// Retrying with the same key returns the same deployment.
	again, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: b.Seq, CancelPending: true, RequestKey: "k1"})
	if err != nil || again.Created || again.Deployment.ID != rb.ID || again.TargetSeq != b.Seq {
		t.Fatalf("same key: %+v err=%v", again, err)
	}
	// A second click without a key also returns the pending rollback, even
	// without cancel_pending, and with the default target.
	again, err = f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: b.Seq})
	if err != nil || again.Created || again.Deployment.ID != rb.ID {
		t.Fatalf("double click: %+v err=%v", again, err)
	}
	// A different target while the rollback is pending is refused.
	_, err = f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: a.Seq})
	if re := rollbackCode(t, err); re.Code != RollbackDeployInProgress || re.Seq != rb.Seq {
		t.Fatalf("other target while pending: %+v", re)
	}

	// Once the rollback has failed, the same key starts a new one.
	if _, err := f.s.UpdateDeployment(ctx, rb.ID, func(d *Deployment) error { d.Status = StatusFailed; return nil }); err != nil {
		t.Fatal(err)
	}
	next, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: b.Seq, RequestKey: "k1"})
	if err != nil || !next.Created || next.Deployment.ID == rb.ID || next.SupersededSeq != 0 {
		t.Fatalf("retry after failure: %+v err=%v", next, err)
	}
	old, _ := f.s.GetDeployment(ctx, rb.ID)
	if old.RequestKey != "" {
		t.Fatalf("ended deployment kept its key: %q", old.RequestKey)
	}

	// When the rollback has gone live, the same key still returns it, while a
	// fresh request to the same version is now the live program.
	if _, err := f.s.UpdateDeployment(ctx, next.Deployment.ID, func(d *Deployment) error {
		d.Status, d.ImageID = StatusRouting, "sha256:b"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetCurrentDeployment(ctx, "web", next.Deployment.ID); err != nil {
		t.Fatal(err)
	}
	same, err := f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: b.Seq, RequestKey: "k1"})
	if err != nil || same.Created || same.Deployment.ID != next.Deployment.ID {
		t.Fatalf("key after live: %+v err=%v", same, err)
	}
	_, err = f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: b.Seq})
	if re := rollbackCode(t, err); re.Code != RollbackVersionSameAsLive || re.LiveSeq != next.Deployment.Seq {
		t.Fatalf("again after live: %+v", re)
	}
	_, err = f.s.CreateRollbackDeployment(ctx, RollbackRequest{App: "web", ToSeq: next.Deployment.Seq})
	if re := rollbackCode(t, err); re.Code != RollbackVersionIsLive {
		t.Fatalf("rollback to live: %+v", re)
	}
}

func TestKeptImageIDsIncludesPending(t *testing.T) {
	ctx := context.Background()
	f := newRollbackFixture(t)
	// a, then KeepVersions newer distinct images, then e live: a is just
	// outside the window.
	f.upload("sha256:a", StatusLive)
	window := []string{}
	for i := 1; i <= KeepVersions; i++ {
		img := fmt.Sprintf("sha256:kept-%d", i)
		f.upload(img, StatusLive)
		window = append(window, img)
	}
	f.upload("sha256:e", StatusLive)
	f.upload("sha256:old-failed", StatusFailed)
	// A queued rollback to an image outside the window (as created before the
	// window moved), a pending build with its image, and a pending pull by name.
	f.finish(NewDeployment{App: "web", SourceKind: SourceImage, SourceRef: "sha256:a", SourceDigest: "sha256:a", BypassDedup: true}, "", StatusQueued)
	f.finish(NewDeployment{App: "web", SourceKind: SourceUpload, SourceRef: "/u", SourceDigest: "x"}, "sha256:building", StatusStarting)
	f.finish(NewDeployment{App: "web", SourceKind: SourceImage, SourceRef: "nginx:1.27", SourceDigest: "nginx:1.27"}, "", StatusQueued)

	ids, err := f.s.KeptImageIDs(ctx, "web")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range append(window, "sha256:e", "sha256:a", "sha256:building") {
		if _, ok := ids[want]; !ok {
			t.Errorf("missing %s in %v", want, ids)
		}
	}
	for _, not := range []string{"sha256:old-failed", "nginx:1.27", ""} {
		if _, ok := ids[not]; ok {
			t.Errorf("unexpected %q in %v", not, ids)
		}
	}
	if len(ids) != KeepVersions+3 {
		t.Errorf("ids = %v", ids)
	}
	if _, err := f.s.KeptImageIDs(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing app err = %v", err)
	}
}

func TestLiveTimes(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	base := time.Date(2026, 11, 3, 2, 0, 0, 0, time.UTC)
	add := func(dep, stage, msg string, offset time.Duration) {
		t.Helper()
		if err := s.AddEvent(ctx, Event{App: "web", DeploymentID: dep, At: base.Add(offset), Stage: stage, Message: msg}); err != nil {
			t.Fatal(err)
		}
	}
	add("aaaaaaaaaaaa", "build", "构建开始", 0)
	add("aaaaaaaaaaaa", "deploy", "已上线 http://203.0.113.10:18810", 63*time.Second)
	add("aaaaaaaaaaaa", "deploy", "已上线 http://203.0.113.10:18810", 5*time.Minute) // re-route later
	add("bbbbbbbbbbbb", "deploy", "被更新的提交取代", 10*time.Second)
	add("cccccccccccc", "health", "已上线（不是部署事件）", 10*time.Second)

	// Pad the request past one IN (...) chunk; the hit sits in the second chunk.
	ids := []string{"bbbbbbbbbbbb", "cccccccccccc", "", "aaaaaaaaaaaa"}
	for i := 0; i < liveTimesChunk; i++ {
		ids = append([]string{fmt.Sprintf("%012x", i+1)}, ids...)
	}
	got, err := s.LiveTimes(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got["aaaaaaaaaaaa"].Equal(base.Add(63*time.Second)) {
		t.Fatalf("LiveTimes = %v", got)
	}
	if empty, err := s.LiveTimes(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty = %v err=%v", empty, err)
	}
}
