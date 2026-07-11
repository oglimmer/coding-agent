// Package k8s spawns and observes the Kubernetes Jobs that run the coding-agent
// worker. It is a Go port of the discord bot's vibecode_service.py: build a Job
// manifest, poll it to completion, read the pod logs, and parse the single-line
// result marker the worker emits.
package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ResultMarker is the log prefix the worker prints its final JSON result with.
const ResultMarker = "CODING_AGENT_RESULT:"

const inClusterNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// Engine selects which coding-agent worker implements the request.
const (
	EngineAider      = "aider"       // aider + DeepSeek (the default worker image)
	EngineClaudeCode = "claude-code" // Claude Code CLI + DeepSeek backend
)

// JobSpec is everything the worker needs to run one feature request.
type JobSpec struct {
	JobName       string
	Repo          string // "owner/name"
	BaseBranch    string
	Branch        string
	Prompt        string
	Feature       string
	PRTitle       string
	Engine        string // "aider" | "claude-code"; empty = aider
	Model         string // per-job coding model (aider architect / claude-code primary); empty = Options default
	EditorModel   string // per-job aider editor model; empty = Options default; unused by claude-code
	VerifyCommand string // repo's build/lint/test gate; empty = worker detects one
	TestCommand   string // repo's fast inner-loop test cmd; empty = worker detects one
}

// Options configure how Jobs are built (image, secrets, resource wiring).
type Options struct {
	Image             string
	ImagePullPolicy   string // Always | IfNotPresent | Never (default Always)
	Model             string // aider architect/planning model
	EditorModel       string // aider model that turns the plan into edits
	ClaudeImage       string // image for the claude-code engine (separate from Image)
	ClaudeModel       string // Claude Code primary model (a DeepSeek model id)
	ClaudeTimeoutSec  int    // seconds per Claude Code round before it is killed
	DeepSeekBaseURL   string // helper-call API base for the worker (scope/self-review/judge)
	Namespace         string
	SecretName        string
	ImagePullSecret   string
	ServiceAccount    string
	GitHubBotLogin    string
	ReviewMaxRounds   int
	ActiveDeadlineSec int64
	AiderTimeoutSec   int // seconds per aider round before it is killed
}

// Result is the parsed worker outcome.
type Result struct {
	Status string `json:"status"` // success | failed
	PRURL  string `json:"pr_url"`
	Branch string `json:"branch"`
	Merged bool   `json:"merged"`
	Reason string `json:"reason"`
}

// Client wraps a Kubernetes clientset plus the Job build options.
type Client struct {
	cs        kubernetes.Interface
	namespace string
	opts      Options
}

// New loads in-cluster config, falling back to the local kubeconfig, and
// returns a ready client. Options.Namespace wins; otherwise the in-cluster
// namespace file, then "default".
func New(opts Options) (*Client, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		loading := clientcmd.NewDefaultClientConfigLoadingRules()
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("k8s: no in-cluster or local kubeconfig: %w", err)
		}
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s: build clientset: %w", err)
	}
	return &Client{cs: cs, namespace: resolveNamespace(opts.Namespace), opts: opts}, nil
}

// firstNonEmpty returns the first non-empty argument (the per-job override when
// set, otherwise the deployment default).
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func pullPolicy(p string) corev1.PullPolicy {
	switch p {
	case "IfNotPresent":
		return corev1.PullIfNotPresent
	case "Never":
		return corev1.PullNever
	default:
		return corev1.PullAlways
	}
}

func resolveNamespace(configured string) string {
	if configured != "" {
		return configured
	}
	if b, err := os.ReadFile(inClusterNamespaceFile); err == nil {
		if ns := strings.TrimSpace(string(b)); ns != "" {
			return ns
		}
	}
	return "default"
}

// Namespace returns the namespace worker Jobs run in.
func (c *Client) Namespace() string { return c.namespace }

// BuildJob constructs the Job manifest for a spec. It is a pure function of its
// inputs so it can be unit-tested without a cluster.
func (c *Client) BuildJob(spec JobSpec) *batchv1.Job {
	return BuildJob(spec, c.opts)
}

// BuildJob is the package-level, dependency-free manifest builder.
func BuildJob(spec JobSpec, opts Options) *batchv1.Job {
	secretRef := func(key string) *corev1.EnvVarSource {
		return &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: opts.SecretName},
				Key:                  key,
			},
		}
	}
	// optionalSecretRef tolerates the key being absent from the secret: the env
	// var is simply left unset instead of blocking the pod with a
	// CreateContainerConfigError. Used for ANTHROPIC_API_KEY so DeepSeek-only
	// deployments (whose secret has no Anthropic key) still start.
	optionalSecretRef := func(key string) *corev1.EnvVarSource {
		optional := true
		src := secretRef(key)
		src.SecretKeyRef.Optional = &optional
		return src
	}

	// Env common to both engines: the task, the GitHub/DeepSeek plumbing, and the
	// engine-agnostic review knobs.
	env := []corev1.EnvVar{
		{Name: "AGENT_REPO", Value: spec.Repo},
		{Name: "AGENT_BASE_BRANCH", Value: spec.BaseBranch},
		{Name: "AGENT_BRANCH", Value: spec.Branch},
		{Name: "AGENT_PROMPT", Value: spec.Prompt},
		{Name: "AGENT_FEATURE", Value: spec.Feature},
		{Name: "AGENT_PR_TITLE", Value: spec.PRTitle},
		{Name: "AGENT_VERIFY_CMD", Value: spec.VerifyCommand},
		{Name: "AGENT_TEST_CMD", Value: spec.TestCommand},
		{Name: "DEEPSEEK_BASE_URL", Value: opts.DeepSeekBaseURL},
		{Name: "GITHUB_BOT_LOGIN", Value: opts.GitHubBotLogin},
		{Name: "REVIEW_MAX_ROUNDS", Value: fmt.Sprintf("%d", opts.ReviewMaxRounds)},
		{Name: "DEEPSEEK_API_KEY", ValueFrom: secretRef("DEEPSEEK_API_KEY")},
		{Name: "GITHUB_TOKEN", ValueFrom: secretRef("WORKER_GITHUB_TOKEN")},
	}

	// Engine-specific: image + coding-model wiring. The claude-code worker picks
	// its backend from the chosen model: a DeepSeek model authenticates via
	// DEEPSEEK_API_KEY (as ANTHROPIC_AUTH_TOKEN, wired inside the worker script),
	// while a claude-* model routes to the real Anthropic API and uses
	// ANTHROPIC_API_KEY. The key is passed optionally so a DeepSeek-only secret
	// (no Anthropic key) still starts the pod for DeepSeek-model jobs.
	image := opts.Image
	if spec.Engine == EngineClaudeCode {
		image = opts.ClaudeImage
		env = append(env,
			corev1.EnvVar{Name: "CLAUDE_MODEL", Value: firstNonEmpty(spec.Model, opts.ClaudeModel)},
			corev1.EnvVar{Name: "CLAUDE_TIMEOUT", Value: fmt.Sprintf("%d", opts.ClaudeTimeoutSec)},
			corev1.EnvVar{Name: "ANTHROPIC_API_KEY", ValueFrom: optionalSecretRef("ANTHROPIC_API_KEY")},
		)
	} else {
		env = append(env,
			corev1.EnvVar{Name: "AIDER_MODEL", Value: firstNonEmpty(spec.Model, opts.Model)},
			corev1.EnvVar{Name: "AIDER_EDITOR_MODEL", Value: firstNonEmpty(spec.EditorModel, opts.EditorModel)},
			corev1.EnvVar{Name: "AIDER_TIMEOUT", Value: fmt.Sprintf("%d", opts.AiderTimeoutSec)},
			// Only consumed by aider when the model is a claude/ model; optional so a
			// DeepSeek-only secret doesn't break pod startup (see optionalSecretRef).
			corev1.EnvVar{Name: "ANTHROPIC_API_KEY", ValueFrom: optionalSecretRef("ANTHROPIC_API_KEY")},
		)
	}

	nonRoot := true
	var runAs int64 = 1000
	podSpec := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever,
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: &nonRoot,
			RunAsUser:    &runAs,
			RunAsGroup:   &runAs,
			FSGroup:      &runAs,
		},
		Containers: []corev1.Container{{
			Name:            "worker",
			Image:           image,
			ImagePullPolicy: pullPolicy(opts.ImagePullPolicy),
			Env:             env,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2000m"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
			},
		}},
	}
	if opts.ServiceAccount != "" {
		podSpec.ServiceAccountName = opts.ServiceAccount
	}
	if opts.ImagePullSecret != "" {
		podSpec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: opts.ImagePullSecret}}
	}

	var backoff int32 = 0
	deadline := opts.ActiveDeadlineSec
	var ttl int32 = 3600
	labels := map[string]string{"app": "coding-agent-worker"}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: spec.JobName, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			ActiveDeadlineSeconds:   &deadline,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       podSpec,
			},
		},
	}
}

// Create submits the Job to the cluster.
func (c *Client) Create(ctx context.Context, spec JobSpec) error {
	_, err := c.cs.BatchV1().Jobs(c.namespace).Create(ctx, c.BuildJob(spec), metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("k8s: create job %s: %w", spec.JobName, err)
	}
	return nil
}

// Delete removes a Job and its pods (background propagation). A missing Job is
// not an error — it may have been TTL-cleaned already.
func (c *Client) Delete(ctx context.Context, jobName string) error {
	policy := metav1.DeletePropagationBackground
	err := c.cs.BatchV1().Jobs(c.namespace).Delete(ctx, jobName, metav1.DeleteOptions{PropagationPolicy: &policy})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("k8s: delete job %s: %w", jobName, err)
	}
	return nil
}

// Phase reflects a Job's terminal state.
type Phase string

const (
	PhaseRunning   Phase = "running"
	PhaseSucceeded Phase = "succeeded"
	PhaseFailed    Phase = "failed"
)

// Status reads the Job via the main resource (not the status subresource, which
// some clusters lag on — see discord bot commit 562293a).
func (c *Client) Status(ctx context.Context, jobName string) (Phase, error) {
	job, err := c.cs.BatchV1().Jobs(c.namespace).Get(ctx, jobName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("k8s: read job %s: %w", jobName, err)
	}
	if job.Status.Succeeded > 0 {
		return PhaseSucceeded, nil
	}
	if job.Status.Failed > 0 {
		return PhaseFailed, nil
	}
	return PhaseRunning, nil
}

// fatalWaitingReasons are container "waiting" reasons a Job never recovers from:
// the image can't be pulled, or the pod can't be configured. A Job whose pod is
// stuck like this stays active until its deadline, so the watcher treats these
// as failures immediately rather than waiting ~90 minutes.
var fatalWaitingReasons = map[string]bool{
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"ErrImageNeverPull":          true,
	"InvalidImageName":           true,
	"CreateContainerConfigError": true, // e.g. the worker secret/key is missing
	"CreateContainerError":       true,
}

// PodTrouble reports a fatal container-startup reason for a Job's pod, if any
// (e.g. "ImagePullBackOff: ghcr.io/...: not found"). ok is false when the pod
// is starting or running normally.
func (c *Client) PodTrouble(ctx context.Context, jobName string) (reason string, ok bool) {
	pods, err := c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil || len(pods.Items) == 0 {
		return "", false
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		statuses := append(append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...)
		for _, cs := range statuses {
			w := cs.State.Waiting
			if w != nil && fatalWaitingReasons[w.Reason] {
				msg := strings.TrimSpace(w.Message)
				if msg == "" {
					return w.Reason, true
				}
				return w.Reason + ": " + msg, true
			}
		}
	}
	return "", false
}

// PodLogs returns the worker pod's log tail for a Job.
func (c *Client) PodLogs(ctx context.Context, jobName string, tailLines int64) (string, error) {
	pods, err := c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil {
		return "", fmt.Errorf("k8s: list pods for %s: %w", jobName, err)
	}
	if len(pods.Items) == 0 {
		return "", nil
	}
	// tailLines <= 0 means "the whole log" (nil TailLines), used when persisting a
	// finished job's complete output; a positive value tails for the live view.
	opts := &corev1.PodLogOptions{}
	if tailLines > 0 {
		opts.TailLines = &tailLines
	}
	req := c.cs.CoreV1().Pods(c.namespace).GetLogs(pods.Items[0].Name, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("k8s: stream logs for %s: %w", jobName, err)
	}
	defer stream.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return sb.String(), nil
}

var markerRe = regexp.MustCompile(regexp.QuoteMeta(ResultMarker))

// ParseResult extracts the last CODING_AGENT_RESULT JSON object from a log
// blob. It scans for the marker then decodes the following object, ignoring any
// trailing text — logs can arrive as one blob with escaped newlines.
func ParseResult(logs string) (Result, bool) {
	var out Result
	found := false
	for _, loc := range markerRe.FindAllStringIndex(logs, -1) {
		start := strings.IndexByte(logs[loc[1]:], '{')
		if start == -1 {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(logs[loc[1]+start:]))
		var r Result
		if err := dec.Decode(&r); err != nil {
			continue
		}
		out = r
		found = true // keep the last valid marker
	}
	return out, found
}
