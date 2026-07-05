// Package deepseek implements the harmful-content gate that every feature
// request must pass before a coding job is spawned. It talks to DeepSeek's
// OpenAI-compatible chat-completions endpoint and asks the model to classify
// the request as harmful or safe, returning structured JSON.
package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const systemPrompt = `You are a strict security and safety classifier for an ` +
	`autonomous software-engineering platform. You are given a feature request ` +
	`that a user wants an AI coding agent to implement in a source repository. ` +
	`Decide whether fulfilling the request would be HARMFUL.

Treat a request as harmful if it asks to build, enable, or assist with any of: ` +
	`malware, ransomware, spyware, or backdoors; credential theft, exfiltration, ` +
	`or unauthorized data access; disabling or evading security controls, ` +
	`authentication, or logging; denial-of-service or mass-targeting abuse; ` +
	`illegal activity, fraud, or clearly unethical behaviour; or intentionally ` +
	`sabotaging the codebase.

A normal software feature (UI, API, tests, refactor, bugfix, docs, integration) ` +
	`is NOT harmful. When genuinely unsure, prefer "harmful": false for ordinary ` +
	`engineering work.

Respond with ONLY a single-line JSON object and nothing else:
{"harmful": <true|false>, "reason": "<short explanation>"}`

// Result is the classifier verdict.
type Result struct {
	Harmful bool   `json:"harmful"`
	Reason  string `json:"reason"`
}

// Client calls the DeepSeek chat API. The zero value is "not configured";
// callers check Configured() before use.
type Client struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

// New builds a client. An empty apiKey yields a non-configured client.
func New(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	if model == "" {
		model = "deepseek-chat"
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Configured reports whether the client can make calls.
func (c *Client) Configured() bool { return c != nil && c.apiKey != "" }

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Check classifies a feature request. A network/API failure is returned as an
// error so the caller can fail closed (reject the job).
func (c *Client) Check(ctx context.Context, feature string) (Result, error) {
	if !c.Configured() {
		return Result{}, fmt.Errorf("deepseek: not configured")
	}

	body, err := json.Marshal(chatRequest{
		Model:       c.model,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: "Feature request:\n\n" + feature},
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("deepseek: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("deepseek: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("deepseek: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("deepseek: api status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return Result{}, fmt.Errorf("deepseek: decode response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return Result{}, fmt.Errorf("deepseek: empty response")
	}
	return classify(cr.Choices[0].Message.Content)
}

var jsonObjectRe = regexp.MustCompile(`(?s)\{.*\}`)

// classify extracts the verdict JSON from the model's message content. It is
// tolerant of the model wrapping the object in prose or code fences, which is
// why it is a separate, unit-tested function.
func classify(content string) (Result, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Result{}, fmt.Errorf("deepseek: empty classification content")
	}
	match := jsonObjectRe.FindString(content)
	if match == "" {
		return Result{}, fmt.Errorf("deepseek: no JSON object in classification: %q", content)
	}
	var r Result
	if err := json.Unmarshal([]byte(match), &r); err != nil {
		return Result{}, fmt.Errorf("deepseek: parse classification %q: %w", match, err)
	}
	if strings.TrimSpace(r.Reason) == "" {
		if r.Harmful {
			r.Reason = "The request was classified as harmful."
		} else {
			r.Reason = "The request looks like ordinary engineering work."
		}
	}
	return r, nil
}
