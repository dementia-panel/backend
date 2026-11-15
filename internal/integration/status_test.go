package integration

import (
	"sync"
	"testing"
)

func TestTrackerLifecycle(t *testing.T) {
	tracker := NewTracker(true)
	if tracker.Snapshot().State != "pending" {
		t.Fatal("enabled integration must await an actual operation")
	}
	tracker.Record(true)
	success := tracker.Snapshot()
	if success.State != "connected" || success.CheckedAt == nil || success.LastSuccessAt == nil {
		t.Fatal("successful operation not recorded")
	}
	copyTime := *success.CheckedAt
	*success.CheckedAt = success.CheckedAt.AddDate(1, 0, 0)
	if !tracker.Snapshot().CheckedAt.Equal(copyTime) {
		t.Fatal("snapshot timestamp aliases internal state")
	}
	tracker.Record(false)
	failed := tracker.Snapshot()
	if failed.State != "error" || failed.LastSuccessAt == nil {
		t.Fatal("failed operation lost delivery history")
	}
	disabled := NewTracker(false)
	disabled.Record(true)
	if disabled.Snapshot().State != "disabled" {
		t.Fatal("disabled integration cannot become connected")
	}
}
func TestTrackerConcurrentSnapshots(t *testing.T) {
	tracker := NewTracker(true)
	var workers sync.WaitGroup
	for i := range 8 {
		workers.Go(func() {
			for range 100 {
				tracker.Record(i%2 == 0)
				_ = tracker.Snapshot()
			}
		})
	}
	workers.Wait()
	if tracker.Snapshot().CheckedAt == nil {
		t.Fatal("missing operation time")
	}
}
