package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Concurrent misses for the same key must collapse to a single fetch.
func TestPodLogCache_CoalescesConcurrent(t *testing.T) {
	c := newPodLogCache()
	var calls atomic.Int64
	release := make(chan struct{})

	const n = 20
	var wg sync.WaitGroup
	results := make([]string, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			logs, err := c.get("job-a", func(context.Context) (string, error) {
				calls.Add(1)
				<-release // hold the fetch open so all callers pile onto it
				return "tail", nil
			})
			if err != nil {
				t.Errorf("get: unexpected error: %v", err)
			}
			results[i] = logs
		}(i)
	}
	// Give the goroutines time to queue on the singleflight before releasing.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch called %d times, want 1 (coalescing failed)", got)
	}
	for i, r := range results {
		if r != "tail" {
			t.Fatalf("result[%d] = %q, want %q", i, r, "tail")
		}
	}
}

// A second call within the TTL must be served from cache, not re-fetched.
func TestPodLogCache_ServesWithinTTL(t *testing.T) {
	c := newPodLogCache()
	var calls atomic.Int64
	fetch := func(context.Context) (string, error) {
		calls.Add(1)
		return "v", nil
	}
	if _, err := c.get("job-b", fetch); err != nil {
		t.Fatalf("first get: %v", err)
	}
	if _, err := c.get("job-b", fetch); err != nil {
		t.Fatalf("second get: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch called %d times, want 1 (cache miss within TTL)", got)
	}
}

// Errors are cached for the TTL too, so an API blip does not become a retry storm.
func TestPodLogCache_CachesErrors(t *testing.T) {
	c := newPodLogCache()
	var calls atomic.Int64
	wantErr := errors.New("boom")
	fetch := func(context.Context) (string, error) {
		calls.Add(1)
		return "", wantErr
	}
	if _, err := c.get("job-c", fetch); !errors.Is(err, wantErr) {
		t.Fatalf("first get err = %v, want %v", err, wantErr)
	}
	if _, err := c.get("job-c", fetch); !errors.Is(err, wantErr) {
		t.Fatalf("second get err = %v, want %v", err, wantErr)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch called %d times, want 1 (error not cached)", got)
	}
}

// reap drops entries older than podLogReapAge and keeps fresh ones.
func TestPodLogCache_ReapEvictsIdle(t *testing.T) {
	c := newPodLogCache()
	c.items["stale"] = podLogEntry{logs: "old", fetched: time.Now().Add(-2 * podLogReapAge)}
	c.items["fresh"] = podLogEntry{logs: "new", fetched: time.Now()}

	c.reap()

	if _, ok := c.items["stale"]; ok {
		t.Fatal("stale entry survived reap")
	}
	if _, ok := c.items["fresh"]; !ok {
		t.Fatal("fresh entry was wrongly reaped")
	}
}
