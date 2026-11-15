package jobs

import (
	"log/slog"
	"runtime/debug"
	"sync"
)

// Group closes job admission before waiting for existing jobs.
type Group struct {
	mu      sync.Mutex
	closed  bool
	workers sync.WaitGroup
}

func (g *Group) Start(name string, run func()) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.workers.Go(func() {
		defer func() {
			if recover() != nil {
				slog.Error("background job panicked", "job", name, "stack", string(debug.Stack()))
			}
		}()
		run()
	})
	return true
}

func (g *Group) CloseAndWait() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
	g.workers.Wait()
}
