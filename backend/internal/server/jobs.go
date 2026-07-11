package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/oglimmer/coding-agent/backend/internal/buildinfo"
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

// jobMetadata captures the platform version and effective worker config a job
// runs under. Stored as JSON on the job so a run can be analysed after the fact.
// model/editorModel are the resolved per-job coding models (already defaulted to
// the engine's deployment default when the request left them empty).
func (a *App) jobMetadata(repo Repo, engine, model, editorModel string) map[string]any {
	m := map[string]any{
		"platformCommit":  buildinfo.Commit,
		"platformVersion": buildinfo.Version,
		"engine":          engine,
		"model":           model,
		"reviewMaxRounds": a.Cfg.ReviewMaxRounds,
		"deepseekBaseURL": a.Cfg.DeepSeekBaseURL,
		"baseBranch":      repo.BaseBranch,
		"verifyCommand":   repo.VerifyCommand,
		"testCommand":     repo.TestCommand,
	}
	if engine == k8s.EngineClaudeCode {
		m["workerImage"] = a.Cfg.WorkerClaudeImage
		m["aiderTimeoutSec"] = a.Cfg.ClaudeTimeoutSec
	} else {
		m["workerImage"] = a.Cfg.WorkerImage
		m["editorModel"] = editorModel
		m["aiderTimeoutSec"] = a.Cfg.AiderTimeoutSec
	}
	return m
}

// normalizeEngine validates the requested engine, defaulting an empty value to
// aider. It returns the canonical engine string and whether it was valid.
func normalizeEngine(engine string) (string, bool) {
	switch engine {
	case "", k8s.EngineAider:
		return k8s.EngineAider, true
	case k8s.EngineClaudeCode:
		return k8s.EngineClaudeCode, true
	default:
		return "", false
	}
}

// engineModels returns the allowlist and default coding models for an engine.
// For claude-code the editor model is unused, so defaultEditor is empty.
func (a *App) engineModels(engine string) (allowed []string, defaultModel, defaultEditor string) {
	if engine == k8s.EngineClaudeCode {
		return a.Cfg.WorkerClaudeModels, a.Cfg.WorkerClaudeModel, ""
	}
	return a.Cfg.WorkerAiderModels, a.Cfg.WorkerModel, a.Cfg.WorkerEditorModel
}

// resolveModels validates the requested per-job models against the engine's
// allowlist, defaulting empty requests to the deployment default. It returns the
// resolved (model, editorModel) — editorModel is always "" for claude-code — and
// an error message suitable for a 400 when a requested model is not allowed.
func (a *App) resolveModels(engine, reqModel, reqEditor string) (model, editorModel, errMsg string) {
	allowed, defModel, defEditor := a.engineModels(engine)

	model = strings.TrimSpace(reqModel)
	if model == "" {
		model = defModel
	} else if !contains(allowed, model) {
		return "", "", fmt.Sprintf("unknown model %q for engine %q", model, engine)
	}

	// The claude-code engine drives a single model; it has no editor split, so we
	// ignore any editor model the client sent rather than reject the request.
	if engine == k8s.EngineClaudeCode {
		return model, "", ""
	}

	editorModel = strings.TrimSpace(reqEditor)
	if editorModel == "" {
		editorModel = defEditor
	} else if !contains(allowed, editorModel) {
		return "", "", fmt.Sprintf("unknown editor model %q for engine %q", editorModel, engine)
	}
	return model, editorModel, ""
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// handleClientConfig serves the per-engine model catalog the New Job form needs
// to render its model dropdowns: the allowlist a job may choose from and the
// deployment default for each engine. Read-only; available to any authenticated
// user.
func (a *App) handleClientConfig(w http.ResponseWriter, r *http.Request) {
	type engineModels struct {
		Models             []string `json:"models"`
		DefaultModel       string   `json:"defaultModel"`
		DefaultEditorModel string   `json:"defaultEditorModel,omitempty"`
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"engines": map[string]engineModels{
			k8s.EngineAider: {
				Models:             a.Cfg.WorkerAiderModels,
				DefaultModel:       a.Cfg.WorkerModel,
				DefaultEditorModel: a.Cfg.WorkerEditorModel,
			},
			k8s.EngineClaudeCode: {
				Models:       a.Cfg.WorkerClaudeModels,
				DefaultModel: a.Cfg.WorkerClaudeModel,
			},
		},
	})
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
		RepoID      string `json:"repoId"`
		Feature     string `json:"feature"`
		Engine      string `json:"engine"`
		Model       string `json:"model"`
		EditorModel string `json:"editorModel"`
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
	engine, ok := normalizeEngine(req.Engine)
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown engine; choose 'aider' or 'claude-code'")
		return
	}
	model, editorModel, mErr := a.resolveModels(engine, req.Model, req.EditorModel)
	if mErr != "" {
		writeErr(w, http.StatusBadRequest, mErr)
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
	job, err := a.Store.CreateJob(r.Context(), repo.ID, u.ID, req.Feature, "checking", engine, model, editorModel)
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}

	// Snapshot the platform version + config this job runs under, for later
	// analysis (it survives independently of the worker pod and its logs).
	if raw, mErr := json.Marshal(a.jobMetadata(repo, engine, model, editorModel)); mErr == nil {
		if err := a.Store.SetJobMetadata(r.Context(), job.ID, raw); err != nil {
			log.Printf("WARN jobs: set metadata for %s: %v", job.ID, err)
		}
		job.Metadata = raw
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
		JobName:       "coding-agent-" + suffix,
		Repo:          repo.FullName(),
		BaseBranch:    repo.BaseBranch,
		Branch:        fmt.Sprintf("agent/%s-%s", suffix, sanitizeSlug(req.Feature, 30)),
		Prompt:        buildPrompt(repo.FullName(), u.Name, req.Feature),
		Feature:       req.Feature,
		PRTitle:       "feat: " + truncate(req.Feature, 60),
		Engine:        engine,
		Model:         model,
		EditorModel:   editorModel,
		VerifyCommand: repo.VerifyCommand,
		TestCommand:   repo.TestCommand,
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

// jobLogTailLines bounds how much of the worker pod's log tail the detail view
// streams. Enough to show the whole run for a typical job without shipping
// megabytes on every poll.
const jobLogTailLines = 2000

// handleJobLogs streams the worker pod's recent log output for a job so the UI
// can show live progress while the agent works. Owner or admin only, mirroring
// handleGetJob. A job that never spawned a pod (still checking, rejected, or no
// cluster configured) and a pod that has already been TTL-cleaned both return an
// empty body rather than an error, so the frontend can poll uniformly.
func (a *App) handleJobLogs(w http.ResponseWriter, r *http.Request) {
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
	if job.UserID != u.ID && !u.IsAdmin() {
		writeErr(w, http.StatusForbidden, "not your job")
		return
	}

	// Prefer the live pod tail (freshest, and the only source while the agent is
	// still working); fall back to the log we persisted when the job finished,
	// which outlives the pod's TTL. That fallback is what lets the UI show the
	// full run long after the worker is gone.
	stored := func() string {
		s, err := a.Store.JobLog(r.Context(), job.ID)
		if err != nil && err != ErrNotFound {
			log.Printf("WARN jobs: stored log for %s: %v", job.ID, err)
		}
		return s
	}

	if a.K8s == nil || job.K8sJobName == "" {
		writeJSON(w, http.StatusOK, map[string]any{"logs": stored(), "persisted": true})
		return
	}

	logs, err := a.K8s.PodLogs(r.Context(), job.K8sJobName, jobLogTailLines)
	if err != nil || logs == "" {
		// Pod not started yet, or cleaned up after finishing. Serve the persisted
		// log if we have one; otherwise report "nothing yet" so the UI keeps polling.
		if err != nil {
			log.Printf("WARN jobs: logs for %s (%s): %v", job.ID, job.K8sJobName, err)
		}
		if s := stored(); s != "" {
			writeJSON(w, http.StatusOK, map[string]any{"logs": s, "persisted": true})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"logs": "", "unavailable": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

// handleListJobs returns the caller's jobs, or all jobs for admins.
func (a *App) handleListJobs(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	filter := u.ID
	if u.IsAdmin() && r.URL.Query().Get("all") == "true" {
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
	if job.UserID != u.ID && !u.IsAdmin() {
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
	if job.UserID != u.ID && !u.IsAdmin() {
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
