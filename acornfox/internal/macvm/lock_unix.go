//go:build !windows

package macvm

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// LockVM takes the lock every program that starts, stops or runs something in
// the virtual machine holds while it does (the resident forwarder and acornfox
// mac stop|start|doctor). Why: `container machine run` boots a machine that
// is stopped, so a look inside that races with a stop boots it again while the
// old instance is still being torn down, and Apple's container is then left
// with a half-started instance ("hostname(s) already exist") that refuses
// every later start (seen in testing). Take the lock, look at the state,
// act, unlock.
func LockVM(p Paths) (func(), error) {
	if err := os.MkdirAll(p.Root, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p.join("vm.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) || time.Now().After(deadline) {
			_ = f.Close()
			return nil, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}
