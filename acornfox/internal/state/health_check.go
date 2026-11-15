package state

import (
	"encoding/json"
	"fmt"
)

// encodeHealthCheck is the apps.health_check column of h: "" for nil (the
// HTTP check), else its JSON. It refuses a check that is not exactly one of
// exec and tcp, or whose time-out is out of range.
func encodeHealthCheck(h *HealthCheck) (string, error) {
	if h == nil {
		return "", nil
	}
	if (len(h.Exec) > 0) == h.TCP {
		return "", fmt.Errorf("%w: health check must be exactly one of exec and tcp", ErrInvalid)
	}
	if h.ExpectStdout != "" && len(h.Exec) == 0 {
		return "", fmt.Errorf("%w: expect_stdout goes with exec only", ErrInvalid)
	}
	if h.TimeoutS < 0 || h.TimeoutS > MaxHealthTimeoutS {
		return "", fmt.Errorf("%w: health check timeout_s %d", ErrInvalid, h.TimeoutS)
	}
	b, err := json.Marshal(h)
	if err != nil {
		return "", fmt.Errorf("%w: encode health check: %v", ErrInvalid, err)
	}
	return string(b), nil
}

func decodeHealthCheck(s string) (*HealthCheck, error) {
	if s == "" {
		return nil, nil
	}
	var h HealthCheck
	if err := json.Unmarshal([]byte(s), &h); err != nil {
		return nil, err
	}
	return &h, nil
}
