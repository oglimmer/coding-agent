package k8s

import "testing"

func TestBuildJob(t *testing.T) {
	spec := JobSpec{
		JobName:    "coding-agent-abc",
		Repo:       "oglimmer/example",
		BaseBranch: "main",
		Branch:     "agent/abc-feature",
		Prompt:     "do the thing",
		Feature:    "the thing",
		PRTitle:    "feat: the thing",
		AutoMerge:  true,
	}
	opts := Options{
		Image:             "ghcr.io/oglimmer/coding-agent-worker:latest",
		Model:             "deepseek/deepseek-v4-pro",
		SecretName:        "coding-agent-secret",
		ImagePullSecret:   "regcred",
		ServiceAccount:    "coding-agent",
		GitHubBotLogin:    "coding-agent-bot",
		ReviewMaxRounds:   3,
		ActiveDeadlineSec: 5400,
		AiderTimeoutSec:   3600,
	}

	job := BuildJob(spec, opts)

	if job.Name != spec.JobName {
		t.Errorf("job name = %q, want %q", job.Name, spec.JobName)
	}
	if got := *job.Spec.BackoffLimit; got != 0 {
		t.Errorf("backoffLimit = %d, want 0", got)
	}
	if got := *job.Spec.ActiveDeadlineSeconds; got != 5400 {
		t.Errorf("activeDeadlineSeconds = %d, want 5400", got)
	}
	pod := job.Spec.Template.Spec
	if pod.RestartPolicy != "Never" {
		t.Errorf("restartPolicy = %q, want Never", pod.RestartPolicy)
	}
	if pod.SecurityContext == nil || pod.SecurityContext.RunAsNonRoot == nil || !*pod.SecurityContext.RunAsNonRoot {
		t.Error("pod should run as non-root")
	}
	if pod.ServiceAccountName != "coding-agent" {
		t.Errorf("serviceAccount = %q", pod.ServiceAccountName)
	}
	if len(pod.ImagePullSecrets) != 1 || pod.ImagePullSecrets[0].Name != "regcred" {
		t.Errorf("imagePullSecrets = %+v", pod.ImagePullSecrets)
	}
	if len(pod.Containers) != 1 {
		t.Fatalf("want 1 container, got %d", len(pod.Containers))
	}

	env := map[string]string{}
	secretKeys := map[string]string{}
	for _, e := range pod.Containers[0].Env {
		if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			secretKeys[e.Name] = e.ValueFrom.SecretKeyRef.Key
		} else {
			env[e.Name] = e.Value
		}
	}
	if env["AGENT_REPO"] != "oglimmer/example" {
		t.Errorf("AGENT_REPO = %q", env["AGENT_REPO"])
	}
	if env["AGENT_BRANCH"] != "agent/abc-feature" {
		t.Errorf("AGENT_BRANCH = %q", env["AGENT_BRANCH"])
	}
	if env["REVIEW_MAX_ROUNDS"] != "3" {
		t.Errorf("REVIEW_MAX_ROUNDS = %q", env["REVIEW_MAX_ROUNDS"])
	}
	if env["AIDER_TIMEOUT"] != "3600" {
		t.Errorf("AIDER_TIMEOUT = %q", env["AIDER_TIMEOUT"])
	}
	if env["AGENT_AUTO_MERGE"] != "true" {
		t.Errorf("AGENT_AUTO_MERGE = %q, want true", env["AGENT_AUTO_MERGE"])
	}
	// With no per-job model on the spec, the Options default is used.
	if env["AIDER_MODEL"] != "deepseek/deepseek-v4-pro" {
		t.Errorf("AIDER_MODEL = %q, want the Options default", env["AIDER_MODEL"])
	}
	if secretKeys["DEEPSEEK_API_KEY"] != "DEEPSEEK_API_KEY" {
		t.Errorf("DEEPSEEK_API_KEY should come from secret, got %+v", secretKeys)
	}
	if secretKeys["GITHUB_TOKEN"] != "WORKER_GITHUB_TOKEN" {
		t.Errorf("GITHUB_TOKEN should map to WORKER_GITHUB_TOKEN secret key, got %+v", secretKeys)
	}
	if secretKeys["ANTHROPIC_API_KEY"] != "ANTHROPIC_API_KEY" {
		t.Errorf("ANTHROPIC_API_KEY should come from secret, got %+v", secretKeys)
	}
	// ANTHROPIC_API_KEY must be optional so a DeepSeek-only secret still starts.
	for _, e := range pod.Containers[0].Env {
		if e.Name != "ANTHROPIC_API_KEY" {
			continue
		}
		ref := e.ValueFrom.SecretKeyRef
		if ref.Optional == nil || !*ref.Optional {
			t.Errorf("ANTHROPIC_API_KEY secret ref must be optional")
		}
	}
}

func TestBuildJobClaudeEngine(t *testing.T) {
	spec := JobSpec{
		JobName: "coding-agent-abc",
		Repo:    "oglimmer/example",
		Branch:  "agent/abc-feature",
		Engine:  EngineClaudeCode,
	}
	opts := Options{
		Image:            "ghcr.io/oglimmer/coding-agent-worker:latest",
		ClaudeImage:      "ghcr.io/oglimmer/coding-agent-worker-claude:latest",
		ClaudeModel:      "deepseek-v4-pro",
		ClaudeTimeoutSec: 3600,
		Model:            "deepseek/deepseek-v4-pro",
		SecretName:       "coding-agent-secret",
	}

	pod := BuildJob(spec, opts).Spec.Template.Spec
	if pod.Containers[0].Image != opts.ClaudeImage {
		t.Errorf("claude engine image = %q, want %q", pod.Containers[0].Image, opts.ClaudeImage)
	}

	env := map[string]string{}
	secretKeys := map[string]bool{}
	optionalSecret := map[string]bool{}
	for _, e := range pod.Containers[0].Env {
		if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			secretKeys[e.Name] = true
			if opt := e.ValueFrom.SecretKeyRef.Optional; opt != nil && *opt {
				optionalSecret[e.Name] = true
			}
		} else {
			env[e.Name] = e.Value
		}
	}
	if env["CLAUDE_MODEL"] != "deepseek-v4-pro" {
		t.Errorf("CLAUDE_MODEL = %q", env["CLAUDE_MODEL"])
	}
	if env["CLAUDE_TIMEOUT"] != "3600" {
		t.Errorf("CLAUDE_TIMEOUT = %q", env["CLAUDE_TIMEOUT"])
	}
	// The claude-code engine must not carry aider-only wiring.
	if _, ok := env["AIDER_MODEL"]; ok {
		t.Error("claude engine should not set AIDER_MODEL")
	}
	// The Anthropic key is wired for the switchable backend (a claude-* model
	// routes to the real Anthropic API), but must be OPTIONAL so a DeepSeek-only
	// secret still starts the pod for DeepSeek-model jobs.
	if !secretKeys["ANTHROPIC_API_KEY"] {
		t.Error("claude engine should wire ANTHROPIC_API_KEY for the switchable backend")
	}
	if !optionalSecret["ANTHROPIC_API_KEY"] {
		t.Error("ANTHROPIC_API_KEY secret ref must be optional for the claude engine")
	}
	// The DeepSeek key + GitHub token are still required.
	if !secretKeys["DEEPSEEK_API_KEY"] || !secretKeys["GITHUB_TOKEN"] {
		t.Errorf("claude engine missing required secrets, got %+v", secretKeys)
	}
}

// A job with auto-merge disabled renders AGENT_AUTO_MERGE=false so the worker
// leaves the approved PR open for a human to merge.
func TestBuildJobAutoMergeDisabled(t *testing.T) {
	spec := JobSpec{JobName: "coding-agent-abc", Repo: "oglimmer/example", Branch: "agent/abc", AutoMerge: false}
	pod := BuildJob(spec, Options{SecretName: "coding-agent-secret"}).Spec.Template.Spec
	for _, e := range pod.Containers[0].Env {
		if e.Name == "AGENT_AUTO_MERGE" {
			if e.Value != "false" {
				t.Errorf("AGENT_AUTO_MERGE = %q, want false", e.Value)
			}
			return
		}
	}
	t.Error("AGENT_AUTO_MERGE env var not set")
}

// A per-job model on the spec overrides the Options default for each engine.
func TestBuildJobPerJobModel(t *testing.T) {
	base := Options{
		Image:       "ghcr.io/oglimmer/coding-agent-worker:latest",
		ClaudeImage: "ghcr.io/oglimmer/coding-agent-worker-claude:latest",
		Model:       "deepseek/deepseek-v4-pro",
		EditorModel: "deepseek/deepseek-chat",
		ClaudeModel: "deepseek-v4-pro",
		SecretName:  "coding-agent-secret",
	}

	envOf := func(spec JobSpec) map[string]string {
		pod := BuildJob(spec, base).Spec.Template.Spec
		env := map[string]string{}
		for _, e := range pod.Containers[0].Env {
			if e.ValueFrom == nil {
				env[e.Name] = e.Value
			}
		}
		return env
	}

	aider := envOf(JobSpec{
		JobName:     "j",
		Engine:      EngineAider,
		Model:       "anthropic/claude-opus-4-8",
		EditorModel: "anthropic/claude-sonnet-5",
	})
	if aider["AIDER_MODEL"] != "anthropic/claude-opus-4-8" {
		t.Errorf("AIDER_MODEL = %q, want per-job override", aider["AIDER_MODEL"])
	}
	if aider["AIDER_EDITOR_MODEL"] != "anthropic/claude-sonnet-5" {
		t.Errorf("AIDER_EDITOR_MODEL = %q, want per-job override", aider["AIDER_EDITOR_MODEL"])
	}

	claude := envOf(JobSpec{
		JobName: "j",
		Engine:  EngineClaudeCode,
		Model:   "deepseek-v4-flash",
	})
	if claude["CLAUDE_MODEL"] != "deepseek-v4-flash" {
		t.Errorf("CLAUDE_MODEL = %q, want per-job override", claude["CLAUDE_MODEL"])
	}
}

func TestBuildJobPullPolicy(t *testing.T) {
	cases := map[string]string{
		"":             "Always",
		"Always":       "Always",
		"IfNotPresent": "IfNotPresent",
		"Never":        "Never",
		"bogus":        "Always",
	}
	for in, want := range cases {
		job := BuildJob(JobSpec{JobName: "j"}, Options{SecretName: "s", ImagePullPolicy: in})
		if got := string(job.Spec.Template.Spec.Containers[0].ImagePullPolicy); got != want {
			t.Errorf("ImagePullPolicy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildJobOmitsOptionalFields(t *testing.T) {
	job := BuildJob(JobSpec{JobName: "j"}, Options{SecretName: "s"})
	pod := job.Spec.Template.Spec
	if pod.ServiceAccountName != "" {
		t.Errorf("serviceAccount should be empty, got %q", pod.ServiceAccountName)
	}
	if pod.ImagePullSecrets != nil {
		t.Errorf("imagePullSecrets should be nil, got %+v", pod.ImagePullSecrets)
	}
}

func TestParseResult(t *testing.T) {
	tests := []struct {
		name       string
		logs       string
		wantFound  bool
		wantStatus string
		wantMerged bool
	}{
		{
			name:       "success on its own line",
			logs:       "cloning...\nCODING_AGENT_RESULT:{\"status\":\"success\",\"pr_url\":\"https://x/pr/1\",\"merged\":true}\n",
			wantFound:  true,
			wantStatus: "success",
			wantMerged: true,
		},
		{
			name:       "trailing junk after object",
			logs:       "CODING_AGENT_RESULT:{\"status\":\"failed\",\"reason\":\"nope\"} b'extra'",
			wantFound:  true,
			wantStatus: "failed",
		},
		{
			name:       "last marker wins",
			logs:       "CODING_AGENT_RESULT:{\"status\":\"failed\"}\nretry\nCODING_AGENT_RESULT:{\"status\":\"success\",\"merged\":true}",
			wantFound:  true,
			wantStatus: "success",
			wantMerged: true,
		},
		{
			name:      "no marker",
			logs:      "just some logs",
			wantFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := ParseResult(tt.logs)
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if !found {
				return
			}
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Merged != tt.wantMerged {
				t.Errorf("merged = %v, want %v", got.Merged, tt.wantMerged)
			}
		})
	}
}
