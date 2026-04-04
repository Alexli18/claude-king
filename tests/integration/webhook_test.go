//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// webhookKingdom returns a kingdom.yml with a sieve pattern matching
// "KING_WEBHOOK_TEST" (severity=error) and a webhook configured to fire on "error".
func webhookKingdom(webhookURL string) string {
	return fmt.Sprintf(`name: integration-webhook-test
vassals:
  - name: shell
    command: %s
    autostart: true
patterns:
  - name: webhook-test-pattern
    regex: 'KING_WEBHOOK_TEST'
    severity: error
    summary_template: 'Webhook test pattern matched in {vassal}'
settings:
  sovereign_approval: false
  webhooks:
    - url: %s
      on: ["error"]
      timeout_sec: 5
      max_retries: 1
`, shellBin(), webhookURL)
}

func TestWebhook_FiresOnSieveMatch(t *testing.T) {
	// 1. Start a real HTTP server that captures incoming POST requests.
	received := make(chan []byte, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Logf("webhook server: read body error: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		select {
		case received <- body:
		default:
		}
	}))
	defer srv.Close()

	// 2. Start daemon with kingdom config that has the sieve pattern and webhook.
	td := startDaemon(t, webhookKingdom(srv.URL))

	// Give the PTY session a moment to start.
	time.Sleep(300 * time.Millisecond)

	// 3. Execute echo to trigger the sieve pattern.
	td.call(t, "exec_in", map[string]interface{}{
		"target":          "shell",
		"command":         "echo KING_WEBHOOK_TEST",
		"timeout_seconds": 5,
	})

	// 4. Wait for the webhook POST to arrive (timeout 5s).
	var body []byte
	select {
	case body = <-received:
		// got it
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for webhook POST (5s)")
	}

	// 5. Unmarshal and assert.
	var payload struct {
		Event    string `json:"event"`
		Severity string `json:"severity"`
		Kingdom  string `json:"kingdom"`
		Vassal   string `json:"vassal"`
		Summary  string `json:"summary"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("failed to unmarshal webhook payload: %v (raw: %s)", err, body)
	}

	t.Logf("received webhook payload: event=%q severity=%q kingdom=%q vassal=%q summary=%q",
		payload.Event, payload.Severity, payload.Kingdom, payload.Vassal, payload.Summary)

	// event field is set to e.Pattern (the pattern name) by the sieve subscriber in daemon.go
	if payload.Event != "webhook-test-pattern" {
		t.Errorf("expected event=%q, got %q", "webhook-test-pattern", payload.Event)
	}
	if payload.Severity != "error" {
		t.Errorf("expected severity=%q, got %q", "error", payload.Severity)
	}
	if payload.Kingdom == "" {
		t.Error("expected non-empty kingdom field in webhook payload")
	}
}
