package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/oglimmer/coding-agent/backend/internal/k8s"
)

const minFeatureLength = 20

// promptTemplate wraps the raw user request with quality gates. The tests
// requirement is ALWAYS included, even when the user did not mention it — this
// is a hard product requirement. It also tells the agent about the downstream
// GitHub Action review loop so it writes review-friendly changes.
const promptTemplate = `You are an autonomous senior software engineer working on the GitHub repository "%s".

Implement the following feature, requested by user "%s":

--- FEATURE REQUEST ---
%s
--- END FEATURE REQUEST ---

Work in this order:
1. UNDERSTAND: restate the request in one sentence and decide WHERE it belongs.
   Identify the exact files that own that behaviour (e.g. a user-visible change
   belongs in the UI layer, an API change in the corresponding handler). If you
   cannot find the right place, look harder — do NOT implement something
   adjacent in files you happen to see. Changing unrelated code instead of the
   requested behaviour is the worst possible outcome and will be rejected.
2. PLAN: list the files you will change and what each change is, then do exactly
   that.
3. IMPLEMENT end to end. Do not stop at scaffolding, config, or stubs — the
   change must actually work when the app runs.
4. TESTS ARE MANDATORY. Even though the request may not mention tests, you MUST
   add or modify at least one automated test that exercises the behaviour you
   built and would FAIL if your change were reverted. A test of unrelated code
   does not count.
5. Follow the conventions already present in the repository (language, style,
   directory layout, framework idioms). Keep the change minimal and focused; do
   not refactor unrelated code. The project must still build and its existing
   test suite must still pass.

Your diff will be independently reviewed against the feature request before and
after the pull request opens; only a change that implements the request in the
right place, with a meaningful test, will be accepted.`

// buildPrompt renders the enhanced prompt for a feature request.
func buildPrompt(repoFullName, username, feature string) string {
	return fmt.Sprintf(promptTemplate, repoFullName, username, strings.TrimSpace(feature))
}

func randomSuffix() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%d-%s", time.Now().Unix(), hex.EncodeToString(b))
}

// handleCreateJob runs the harmful-content gate then spawns a worker Job.
func (a *App) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	var req struct {
		RepoID  string `json:"repoId"`
		Feature string `json:"feature"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Feature = strings.TrimSpace(req.Feature)
	if len(req.Feature) < minFeatureLength {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("please describe the feature in at least %d characters", minFeatureLength))
		return
	}

	repo, err := a.Store.RepoByID(r.Context(), req.RepoID)
	if err != nil {
		if err == ErrNotFound {
			writeErr(w, http.StatusNotFound, "repository not found")
			return
		}
		a.serverErr(w, r, err, "")
		return
	}

	// Rate limits and concurrency.
	if remaining := a.cooldownRemaining(u.ID); remaining > 0 {
		writeErr(w, http.StatusTooManyRequests, fmt.Sprintf("please wait %s before starting another job", remaining.Round(time.Second)))
		return
	}
	active, err := a.Store.CountActiveJobs(r.Context())
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}
	if active >= a.Cfg.MaxConcurrentJobs {
		writeErr(w, http.StatusTooManyRequests, "the maximum number of concurrent jobs is already running; try again shortly")
		return
	}

	// Persist the job in 'checking' so it is visible while the gate runs.
	job, err := a.Store.CreateJob(r.Context(), repo.ID, u.ID, req.Feature, "checking")
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}

	// --- harmful-content gate (fail closed) --- runs first, before any compute.
	if !a.DeepSeek.Configured() {
		_ = a.Store.MarkJobRejected(r.Context(), job.ID, "harmful-content check is not configured")
		job.Status, job.Reason = "rejected", "harmful-content check is not configured"
		writeJSON(w, http.StatusServiceUnavailable, job)
		return
	}
	verdict, err := a.DeepSeek.Check(r.Context(), req.Feature)
	if err != nil {
		reason := "harmful-content check failed: " + err.Error()
		_ = a.Store.MarkJobRejected(r.Context(), job.ID, reason)
		job.Status, job.Reason = "rejected", reason
		writeJSON(w, http.StatusBadGateway, job)
		return
	}
	if verdict.Harmful {
		_ = a.Store.MarkJobRejected(r.Context(), job.ID, verdict.Reason)
		job.Status, job.Reason = "rejected", verdict.Reason
		writeJSON(w, http.StatusUnprocessableEntity, job)
		return
	}

	// The request is safe. Now we need a cluster to actually run it.
	if a.K8s == nil {
		reason := "the worker cluster is not configured"
		_ = a.Store.FinishJob(r.Context(), job.ID, "failed", "", reason)
		job.Status, job.Reason = "failed", reason
		writeJSON(w, http.StatusServiceUnavailable, job)
		return
	}

	// --- spawn the worker Job ---
	suffix := randomSuffix()
	spec := k8s.JobSpec{
		JobName:    "coding-agent-" + suffix,
		Repo:       repo.FullName(),
		BaseBranch: repo.BaseBranch,
		Branch:     fmt.Sprintf("agent/%s-%s", suffix, sanitizeSlug(req.Feature, 30)),
		Prompt:     buildPrompt(repo.FullName(), u.Name, req.Feature),
		Feature:    req.Feature,
		PRTitle:    "feat: " + truncate(req.Feature, 60),
	}
	if err := a.K8s.Create(r.Context(), spec); err != nil {
		reason := "failed to start the worker job"
		_ = a.Store.FinishJob(r.Context(), job.ID, "failed", "", reason)
		log.Printf("ERROR jobs: create k8s job for %s: %v", job.ID, err)
		job.Status, job.Reason = "failed", reason
		writeJSON(w, http.StatusInternalServerError, job)
		return
	}
	if err := a.Store.MarkJobRunning(r.Context(), job.ID, spec.JobName, spec.Branch); err != nil {
		a.serverErr(w, r, err, "")
		return
	}
	a.markRun(u.ID)

	updated, err := a.Store.JobByID(r.Context(), job.ID)
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusAccepted, updated)
}

// handleListJobs returns the caller's jobs, or all jobs for admins.
func (a *App) handleListJobs(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	filter := u.ID
	if u.IsAdmin && r.URL.Query().Get("all") == "true" {
		filter = ""
	}
	jobs, err := a.Store.ListJobs(r.Context(), filter, 100)
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}
	if jobs == nil {
		jobs = []Job{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

// handleGetJob returns one job. Non-admins may only see their own.
func (a *App) handleGetJob(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	job, err := a.Store.JobByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if err == ErrNotFound {
			writeErr(w, http.StatusNotFound, "job not found")
			return
		}
		a.serverErr(w, r, err, "")
		return
	}
	if job.UserID != u.ID && !u.IsAdmin {
		writeErr(w, http.StatusForbidden, "not your job")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// handleDeleteJob removes a job (and best-effort deletes its k8s Job). Owner or
// admin only. Useful to clean up finished/failed jobs.
func (a *App) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	job, err := a.Store.JobByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if err == ErrNotFound {
			writeErr(w, http.StatusNotFound, "job not found")
			return
		}
		a.serverErr(w, r, err, "")
		return
	}
	if job.UserID != u.ID && !u.IsAdmin {
		writeErr(w, http.StatusForbidden, "not your job")
		return
	}

	// Best-effort: remove the k8s Job if it is still around. A running Job is
	// cancelled by the delete; a finished one may already be TTL-cleaned.
	if a.K8s != nil && job.K8sJobName != "" {
		if err := a.K8s.Delete(r.Context(), job.K8sJobName); err != nil {
			log.Printf("WARN jobs: delete k8s job %s for %s: %v", job.K8sJobName, job.ID, err)
		}
	}

	if err := a.Store.DeleteJob(r.Context(), job.ID); err != nil {
		if err == ErrNotFound {
			writeErr(w, http.StatusNotFound, "job not found")
			return
		}
		a.serverErr(w, r, err, "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- rate limiting -----------------------------------------------------------

func (a *App) cooldownRemaining(userID string) time.Duration {
	a.cooldownMu.Lock()
	defer a.cooldownMu.Unlock()
	last, ok := a.lastRunPerUser[userID]
	if !ok {
		return 0
	}
	elapsed := time.Since(last)
	if elapsed >= a.Cfg.JobCooldown {
		return 0
	}
	return a.Cfg.JobCooldown - elapsed
}

func (a *App) markRun(userID string) {
	a.cooldownMu.Lock()
	a.lastRunPerUser[userID] = time.Now()
	a.cooldownMu.Unlock()
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n])
}
