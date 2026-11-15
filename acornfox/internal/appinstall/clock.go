package appinstall

import (
	"context"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/runner"
)

// The install task's time limit and how it is counted.
//
// installTaskLimit (60 minutes) is for every step of a task except pulling an
// image from the public sources: while a pull runs the task's clock stands
// still. A pull has a limit of its own instead: the runner stops it after
// runner.PullMaxTotal (50 minutes for one image over all its sources, as long
// as it keeps moving) and answers why; the engine waits pullStepLimit for that
// answer at most. So a slow network that pulls a recipe's two large images
// for an hour or more still gets them installed, and the longest a task can
// take is still known: MaxTaskDuration(number of images pulled).
//
// Downloading an image archive through the download gateway is not such a
// pull: it has no total limit of its own and stays inside the 60 minutes.
//
// When the clock runs out the task's context ends with the same error as a
// plain deadline (context.DeadlineExceeded), so every step reports it as it
// did when the 60 minutes were a deadline.

// installTaskLimit bounds one install task's steps other than the pulls from
// the public sources (a variable so tests can shorten it).
var installTaskLimit = 60 * time.Minute

// pullStepLimit bounds the engine's wait on one pull from the public sources:
// the runner's own limit and two minutes for its answer (the runner client
// gives up a minute after the limit as well). A variable for tests.
var pullStepLimit = runner.PullMaxTotal + 2*time.Minute

// taskContext makes an install task's context (tests put the plain deadline
// of earlier versions in its place to compare).
var taskContext = newTaskClock

// MaxTaskDuration is the longest an install task that pulls this many images
// from the public sources can take: the task's limit plus each pull's.
func MaxTaskDuration(pulls int) time.Duration {
	return installTaskLimit + time.Duration(pulls)*pullStepLimit
}

// taskClock is an install task's clock: limit of running time, standing still
// while paused.
type taskClock struct {
	mu      sync.Mutex
	left    time.Duration // time left when the clock last stopped
	since   time.Time     // when it last started running (while not paused)
	paused  int
	expired bool
	timer   *time.Timer
	end     func(error) // ends the task's context with this error
	// pausedFor is how long the clock has stood still in all (the pulls).
	pausedFor time.Duration
	pausedAt  time.Time
}

type taskClockKey struct{}

// clockCtx is the task's context. It is a context type of its own (not a
// wrapped cancel context) so that a context derived from it ends with its
// Err: context.DeadlineExceeded when the clock ran out, like a deadline's,
// or the parent's error when the parent ended.
type clockCtx struct {
	parent context.Context
	c      *taskClock
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

func (x *clockCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (x *clockCtx) Done() <-chan struct{}       { return x.done }

func (x *clockCtx) Err() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.err
}

func (x *clockCtx) Value(key any) any {
	if key == (taskClockKey{}) {
		return x.c
	}
	return x.parent.Value(key)
}

func (x *clockCtx) finish(err error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.err == nil {
		x.err = err
		close(x.done)
	}
}

// newTaskClock starts a clock of limit under parent. The context has no
// Deadline (it can move), so a runner call made under it sends none.
func newTaskClock(parent context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	x := &clockCtx{parent: parent, done: make(chan struct{})}
	c := &taskClock{left: limit, since: time.Now(), end: x.finish}
	x.c = c
	c.mu.Lock()
	c.timer = time.AfterFunc(limit, c.fire)
	c.mu.Unlock()
	stopParent := context.AfterFunc(parent, func() { x.finish(parent.Err()) })
	return x, func() {
		c.mu.Lock()
		c.timer.Stop()
		c.mu.Unlock()
		stopParent()
		x.finish(context.Canceled)
	}
}

func (c *taskClock) fire() {
	c.mu.Lock()
	if c.paused > 0 || c.expired {
		c.mu.Unlock()
		return
	}
	// A pause and resume that came just as the timer fired keep their time.
	if left := c.left - time.Since(c.since); left > 0 {
		c.timer.Reset(left)
		c.mu.Unlock()
		return
	}
	c.expired = true
	c.mu.Unlock()
	c.end(context.DeadlineExceeded)
}

// pauseClock stops the clock of ctx's task until the returned func is called
// (nothing when ctx has none). Pauses may nest.
func pauseClock(ctx context.Context) (resume func()) {
	c, _ := ctx.Value(taskClockKey{}).(*taskClock)
	if c == nil {
		return func() {}
	}
	c.mu.Lock()
	if c.paused == 0 && !c.expired {
		c.timer.Stop()
		c.left -= time.Since(c.since)
		c.pausedAt = time.Now()
	}
	c.paused++
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.paused--
			if c.paused > 0 || c.expired {
				c.mu.Unlock()
				return
			}
			c.pausedFor += time.Since(c.pausedAt)
			c.since = time.Now()
			if c.left <= 0 {
				c.expired = true
				c.mu.Unlock()
				c.end(context.DeadlineExceeded)
				return
			}
			c.timer.Reset(c.left)
			c.mu.Unlock()
		})
	}
}

// clockLeft is the running time ctx's task has left and how long its clock
// has stood still (ok false: ctx has no task clock).
func clockLeft(ctx context.Context) (left, paused time.Duration, ok bool) {
	c, _ := ctx.Value(taskClockKey{}).(*taskClock)
	if c == nil {
		return 0, 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	left, paused = c.left, c.pausedFor
	if c.paused == 0 {
		left -= time.Since(c.since)
	} else {
		paused += time.Since(c.pausedAt)
	}
	return max(left, 0), paused, true
}
