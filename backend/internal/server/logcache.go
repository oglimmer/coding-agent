package server

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// podLogCacheTTL bounds how stale a served log tail may be. The job-detail view
// polls /jobs/{id}/logs every couple of seconds and multiple viewers (or browser
// tabs) multiply that; caching for this window collapses a burst of polls into a
// single k8s API call while keeping the live view effectively real-time.
const podLogCacheTTL = 2 * time.Second

// podLogFetchTimeout bounds the k8s API call made on a cache miss. It is
// deliberately decoupled from the triggering request's context so one viewer
// navigating away cannot cancel the fetch that other coalesced viewers await.
const podLogFetchTimeout = 15 * time.Second

// Idle entries are reaped so the cache cannot grow without bound: each entry
// holds a log tail (up to jobLogTailLines), and a long-lived process would
// otherwise accumulate one per distinct job ever viewed.
const (
	podLogReapAge      = 2 * time.Minute
	podLogReapInterval = time.Minute
)

type podLogEntry struct {
	logs    string
	err     error
	fetched time.Time
}

// podLogCache coalesces and briefly caches worker-pod log tails so the polling
// job-detail view does not hit the k8s API server on every request. Concurrent
// misses for the same job collapse to a single fetch via singleflight; results
// (and errors, to avoid retry storms during an API blip) are cached for
// podLogCacheTTL and evicted once a job stops being polled.
type podLogCache struct {
	group singleflight.Group

	mu    sync.Mutex
	items map[string]podLogEntry
}

func newPodLogCache() *podLogCache {
	return &podLogCache{items: make(map[string]podLogEntry)}
}

// get returns a fresh-enough cached tail for key, or invokes fetch exactly once
// across all concurrent callers and caches the outcome. fetch receives a
// bounded, request-independent context.
func (c *podLogCache) get(key string, fetch func(context.Context) (string, error)) (string, error) {
	if e, ok := c.lookup(key); ok {
		return e.logs, e.err
	}
	v, err, _ := c.group.Do(key, func() (any, error) {
		// A caller ahead of us in the singleflight queue may have just refreshed.
		if e, ok := c.lookup(key); ok {
			return e.logs, e.err
		}
		ctx, cancel := context.WithTimeout(context.Background(), podLogFetchTimeout)
		defer cancel()
		logs, ferr := fetch(ctx)
		c.mu.Lock()
		c.items[key] = podLogEntry{logs: logs, err: ferr, fetched: time.Now()}
		c.mu.Unlock()
		return logs, ferr
	})
	return v.(string), err
}

func (c *podLogCache) lookup(key string) (podLogEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if ok && time.Since(e.fetched) < podLogCacheTTL {
		return e, true
	}
	return podLogEntry{}, false
}

// reap drops entries whose last fetch is older than podLogReapAge.
func (c *podLogCache) reap() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.items {
		if time.Since(e.fetched) > podLogReapAge {
			delete(c.items, k)
		}
	}
}

// startReaper evicts idle entries on a ticker until ctx is done, tracked by wg
// so shutdown waits for it. Follows the same lifecycle pattern as StartWatcher.
func (c *podLogCache) startReaper(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(podLogReapInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.reap()
			}
		}
	}()
}
