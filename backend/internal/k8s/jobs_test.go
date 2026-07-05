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
	if secretKeys["DEEPSEEK_API_KEY"] != "DEEPSEEK_API_KEY" {
		t.Errorf("DEEPSEEK_API_KEY should come from secret, got %+v", secretKeys)
	}
	if secretKeys["GITHUB_TOKEN"] != "WORKER_GITHUB_TOKEN" {
		t.Errorf("GITHUB_TOKEN should map to WORKER_GITHUB_TOKEN secret key, got %+v", secretKeys)
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
