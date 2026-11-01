package state

import (
	"context"
	"errors"
	"testing"
)

func TestResourceLimitsDefaultSetAndReset(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	l, err := s.GetResourceLimits(ctx)
	if err != nil {
		t.Fatalf("GetResourceLimits: %v", err)
	}
	if l.MemoryMB != 0 || l.CPUMilli != 0 || !l.UpdatedAt.IsZero() {
		t.Fatalf("never set = %+v, want zero (automatic)", l)
	}

	if err := s.SetResourceLimits(ctx, ResourceLimits{MemoryMB: 4096, CPUMilli: 2000}); err != nil {
		t.Fatalf("SetResourceLimits: %v", err)
	}
	l, _ = s.GetResourceLimits(ctx)
	if l.MemoryMB != 4096 || l.CPUMilli != 2000 || l.UpdatedAt.IsZero() {
		t.Fatalf("after set = %+v", l)
	}

	// 0 resets one cap to automatic; the row is updated in place.
	if err := s.SetResourceLimits(ctx, ResourceLimits{MemoryMB: 0, CPUMilli: 2000}); err != nil {
		t.Fatalf("reset memory: %v", err)
	}
	l, _ = s.GetResourceLimits(ctx)
	if l.MemoryMB != 0 || l.CPUMilli != 2000 {
		t.Fatalf("after reset = %+v", l)
	}

	if err := s.SetResourceLimits(ctx, ResourceLimits{MemoryMB: -1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative memory err = %v, want ErrInvalid", err)
	}
}
