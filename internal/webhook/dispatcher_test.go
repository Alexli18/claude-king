package webhook_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/webhook"
)

func TestDispatcher_DeliversPayload(t *testing.T) {
	received := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf [4096]byte
		n, _ := r.Body.Read(buf[:])
		received <- buf[:n]
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := []config.WebhookConfig{{URL: srv.URL, On: []string{"error"}}}
	d := webhook.NewDispatcher(cfg, "my-kingdom", nil)
	d.Start()
	defer d.Stop()

	d.Send(webhook.Payload{
		Kingdom:  "my-kingdom",
		Event:    "test_event",
		Severity: "error",
		Summary:  "hello",
	})

	select {
	case body := <-received:
		var p map[string]any
		if err := json.Unmarshal(body, &p); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if p["severity"] != "error" {
			t.Errorf("expected severity=error, got %v", p["severity"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook not delivered within 3s")
	}
}

func TestDispatcher_FiltersOnSeverity(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := []config.WebhookConfig{{URL: srv.URL, On: []string{"critical"}}}
	d := webhook.NewDispatcher(cfg, "k", nil)
	d.Start()
	defer d.Stop()

	d.Send(webhook.Payload{Severity: "error", Event: "x"})   // filtered out
	d.Send(webhook.Payload{Severity: "critical", Event: "y"}) // delivered

	time.Sleep(500 * time.Millisecond)
	if calls != 1 {
		t.Errorf("expected 1 delivery (filtered), got %d", calls)
	}
}

func TestDispatcher_HMACSignature(t *testing.T) {
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-King-Signature")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := []config.WebhookConfig{{URL: srv.URL, Secret: "mysecret", On: []string{"error"}}}
	d := webhook.NewDispatcher(cfg, "k", nil)
	d.Start()
	defer d.Stop()

	d.Send(webhook.Payload{Severity: "error", Event: "x"})
	time.Sleep(500 * time.Millisecond)

	if gotSig == "" {
		t.Fatal("expected X-King-Signature header, got none")
	}
	if len(gotSig) < 10 {
		t.Errorf("signature looks too short: %q", gotSig)
	}
}

func TestDispatcher_RetriesOnFailure(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := []config.WebhookConfig{{URL: srv.URL, On: []string{"error"}, MaxRetries: 3}}
	d := webhook.NewDispatcher(cfg, "k", nil)
	d.Start()
	defer d.Stop()

	d.Send(webhook.Payload{Severity: "error", Event: "x"})
	time.Sleep(3 * time.Second)

	if attempts < 3 {
		t.Errorf("expected at least 3 attempts (with retry), got %d", attempts)
	}
}
