package deepseek

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantHarmful bool
		wantErr     bool
	}{
		{
			name:        "plain safe",
			content:     `{"harmful": false, "reason": "adds a button"}`,
			wantHarmful: false,
		},
		{
			name:        "plain harmful",
			content:     `{"harmful": true, "reason": "steals credentials"}`,
			wantHarmful: true,
		},
		{
			name:        "wrapped in code fence",
			content:     "```json\n{\"harmful\": false, \"reason\": \"ok\"}\n```",
			wantHarmful: false,
		},
		{
			name:        "wrapped in prose",
			content:     `Here is my verdict: {"harmful": true, "reason": "malware"} — be careful.`,
			wantHarmful: true,
		},
		{
			name:    "no json",
			content: "I cannot determine this.",
			wantErr: true,
		},
		{
			name:    "empty",
			content: "   ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classify(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Harmful != tt.wantHarmful {
				t.Errorf("harmful = %v, want %v", got.Harmful, tt.wantHarmful)
			}
			if got.Reason == "" {
				t.Errorf("reason should be defaulted when empty")
			}
		})
	}
}

func TestClassifyDefaultsReason(t *testing.T) {
	got, err := classify(`{"harmful": true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Reason == "" {
		t.Errorf("reason should be defaulted for harmful verdict")
	}
}

func TestConfigured(t *testing.T) {
	if New("", "", "").Configured() {
		t.Error("client without api key should not be configured")
	}
	if !New("key", "", "").Configured() {
		t.Error("client with api key should be configured")
	}
	var nilClient *Client
	if nilClient.Configured() {
		t.Error("nil client should not be configured")
	}
}
