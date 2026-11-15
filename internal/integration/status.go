package integration

import (
	"sync"
	"time"
)

type Status struct {
	Enabled       bool       `json:"enabled"`
	State         string     `json:"state"`
	CheckedAt     *time.Time `json:"checked_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
}
type Snapshot struct {
	Telegram Status `json:"telegram"`
	Webhook  Status `json:"webhook"`
}

// Tracker records outcomes of actual integration operations without exposing credentials.
type Tracker struct {
	mu     sync.RWMutex
	status Status
}

func NewTracker(enabled bool) *Tracker {
	state := "disabled"
	if enabled {
		state = "pending"
	}
	return &Tracker{status: Status{Enabled: enabled, State: state}}
}
func (t *Tracker) Record(success bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.status.Enabled {
		return
	}
	now := time.Now().UTC()
	t.status.CheckedAt = &now
	if success {
		t.status.State = "connected"
		t.status.LastSuccessAt = &now
	} else {
		t.status.State = "error"
	}
}
func (t *Tracker) Snapshot() Status {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := t.status
	// Return independent timestamp values to keep the snapshot immutable for callers.
	if result.CheckedAt != nil {
		value := *result.CheckedAt
		result.CheckedAt = &value
	}
	if result.LastSuccessAt != nil {
		value := *result.LastSuccessAt
		result.LastSuccessAt = &value
	}
	return result
}
