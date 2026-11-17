package jobs

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrentStartAndClose(t *testing.T) {
	var group Group
	var callers sync.WaitGroup
	var accepted, completed atomic.Int64
	for range 100 {
		callers.Go(func() {
			if group.Start("test", func() {
				completed.Add(1)
			}) {
				accepted.Add(1)
			}
		})
	}
	callers.Go(group.CloseAndWait)
	callers.Wait()
	if group.Start("late", func() {
		t.Error("job ran after shutdown")
	}) {
		t.Fatal("job accepted after shutdown")
	}
	if accepted.Load() != completed.Load() {
		t.Fatal("shutdown missed an accepted job")
	}
}
