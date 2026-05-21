package sync

import (
	"context"
	"sync"
	"testing"
	"time"
)

type blockingProvider struct {
	mu       sync.Mutex
	started  map[string]struct{}
	startCh  chan string
	releaseC chan struct{}
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{
		started:  map[string]struct{}{},
		startCh:  make(chan string, 2),
		releaseC: make(chan struct{}),
	}
}

func (p *blockingProvider) Fetch(ctx context.Context, resourceURI string) (map[string]any, error) {
	p.mu.Lock()
	p.started[resourceURI] = struct{}{}
	p.mu.Unlock()
	p.startCh <- resourceURI

	select {
	case <-p.releaseC:
		return map[string]any{"ok": true}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestRunCycle_FetchesDistinctPathsConcurrently(t *testing.T) {
	t.Parallel()

	p := newBlockingProvider()
	coord := NewCoordinator(p)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resultCh := make(chan CycleResult, 1)
	go func() {
		resultCh <- coord.RunCycle(ctx, []string{"app/base", "db/primary"})
	}()

	seen := map[string]struct{}{}
	for len(seen) < 2 {
		select {
		case path := <-p.startCh:
			seen[path] = struct{}{}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("expected both paths to start fetch concurrently, got starts: %v", seen)
		}
	}

	close(p.releaseC)

	select {
	case res := <-resultCh:
		if res.Status != CycleStatusSuccess {
			t.Fatalf("expected success status, got %q", res.Status)
		}
		if len(res.FetchedPaths) != 2 {
			t.Fatalf("expected 2 fetched paths, got %d", len(res.FetchedPaths))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("cycle did not complete after releasing provider")
	}
}
