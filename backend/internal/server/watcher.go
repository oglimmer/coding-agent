package server

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/oglimmer/coding-agent/backend/internal/k8s"
)

const workerLogTailLines = 400

// maxStoredLogBytes caps the log we persist per job. A normal run is well under
// this; a runaway loop could be larger, so we keep the tail (where the failure
// and result marker live) and note the truncation.
const maxStoredLogBytes = 1 << 20 // 1 MiB

// StartWatcher runs a background loop that reconciles running jobs with their
// Kubernetes Job status, parses the worker result marker from pod logs, and
// records the terminal outcome. It follows the guideline background-job pattern:
// guard on config, track via WaitGroup, immediate first pass, then tick.
func (a *App) StartWatcher(ctx context.Context, wg *sync.WaitGroup) {
	if a.K8s == nil {
		log.Printf("INFO watcher: k8s not configured, job watcher disabled")
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(a.Cfg.WatchInterval)
		defer ticker.Stop()
		a.reconcileJobs(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.reconcileJobs(ctx)
			}
		}
	}()
}

func (a *App) reconcileJobs(ctx context.Context) {
	jobs, err := a.Store.RunningJobs(ctx)
	if err != nil {
		log.Printf("ERROR watcher: list running jobs: %v", err)
		return
	}
	for _, job := range jobs {
		if job.K8sJobName == "" {
			continue
		}
		a.reconcileOne(ctx, job)
	}
}

func (a *App) reconcileOne(ctx context.Context, job Job) {
	phase, err := a.K8s.Status(ctx, job.K8sJobName)
	if err != nil {
		// A missing Job (TTL-expired before we observed it) can't be recovered;
		// leave it running for a human to inspect rather than guessing.
		log.Printf("WARN watcher: status for job %s (%s): %v", job.ID, job.K8sJobName, err)
		return
	}
	if phase == k8s.PhaseRunning {
		// The Job is still active, but its pod may be wedged in a state it will
		// never recover from (image can't be pulled, secret missing). Surface
		// that now instead of waiting for the activeDeadline to fire.
		if reason, stuck := a.K8s.PodTrouble(ctx, job.K8sJobName); stuck {
			if err := a.Store.FinishJob(ctx, job.ID, "failed", "", "worker could not start: "+reason); err != nil {
				log.Printf("ERROR watcher: finish stuck job %s: %v", job.ID, err)
				return
			}
			log.Printf("INFO watcher: job %s failed to start: %s", job.ID, reason)
		}
		return
	}

	// Fetch the FULL log (tail <= 0) once, now, while the pod still exists: it is
	// both what we parse the result from and what we persist for later analysis,
	// since the pod is TTL-cleaned shortly after finishing.
	logs, err := a.K8s.PodLogs(ctx, job.K8sJobName, 0)
	if err != nil {
		log.Printf("WARN watcher: logs for job %s: %v", job.ID, err)
	}
	if logs != "" {
		if err := a.Store.SetJobLog(ctx, job.ID, capLog(logs)); err != nil {
			log.Printf("WARN watcher: persist log for job %s: %v", job.ID, err)
		}
	}
	result, found := k8s.ParseResult(logs)

	status, prURL, reason := "failed", "", "the coding agent did not report success"
	switch {
	case found && result.Status == "success":
		status, prURL = "success", result.PRURL
		if result.Merged {
			reason = "PR reviewed and auto-merged"
		} else {
			reason = "PR opened and reviewed"
		}
	case found && result.Reason != "":
		reason = result.Reason
	case phase == k8s.PhaseFailed:
		reason = "the worker job failed; see cluster logs"
	}

	if err := a.Store.FinishJob(ctx, job.ID, status, prURL, reason); err != nil {
		log.Printf("ERROR watcher: finish job %s: %v", job.ID, err)
		return
	}
	log.Printf("INFO watcher: job %s finished status=%s pr=%s", job.ID, status, prURL)
}

// capLog bounds a persisted log to maxStoredLogBytes, keeping the tail (the end
// carries the failure and the CODING_AGENT_RESULT marker) with a truncation note.
func capLog(logs string) string {
	if len(logs) <= maxStoredLogBytes {
		return logs
	}
	dropped := len(logs) - maxStoredLogBytes
	return "[… " + strconv.Itoa(dropped) + " earlier bytes truncated …]\n" + logs[len(logs)-maxStoredLogBytes:]
}
