package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// GetResourceLimits returns the server-wide resource caps. Before anything has
// been set it returns the zero value, which means automatic.
func (s *Store) GetResourceLimits(ctx context.Context) (ResourceLimits, error) {
	var l ResourceLimits
	var updated string
	err := s.db.QueryRowContext(ctx, `SELECT memory_mb, cpu_milli, updated_at FROM resource_limits WHERE id = 1`).
		Scan(&l.MemoryMB, &l.CPUMilli, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ResourceLimits{}, nil
	}
	if err != nil {
		return ResourceLimits{}, fmt.Errorf("get resource limits: %w", err)
	}
	t, err := parseTime(updated)
	if err != nil {
		return ResourceLimits{}, err
	}
	l.UpdatedAt = t
	return l, nil
}

// SetResourceLimits stores the server-wide resource caps; 0 resets a cap to
// automatic. Bounds against the host and current allocation are checked by the
// caller, which knows the host size.
func (s *Store) SetResourceLimits(ctx context.Context, l ResourceLimits) error {
	if l.MemoryMB < 0 || l.CPUMilli < 0 {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO resource_limits (id, memory_mb, cpu_milli, updated_at) VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET memory_mb = excluded.memory_mb, cpu_milli = excluded.cpu_milli, updated_at = excluded.updated_at`,
		l.MemoryMB, l.CPUMilli, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("set resource limits: %w", err)
	}
	return nil
}
