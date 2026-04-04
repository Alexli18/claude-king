package webhook_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
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
	if n := calls.Load(); n != 1 {
		t.Errorf("expected 1 delivery (filtered), got %d", n)
	}
}

func TestDispatcher_HMACSignature(t *testing.T) {
	var (
		mu     sync.Mutex
		gotSig string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotSig = r.Header.Get("X-King-Signature")
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := []config.WebhookConfig{{URL: srv.URL, Secret: "mysecret", On: []string{"error"}}}
	d := webhook.NewDispatcher(cfg, "k", nil)
	d.Start()
	defer d.Stop()

	d.Send(webhook.Payload{Severity: "error", Event: "x"})
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	sig := gotSig
	mu.Unlock()

	if sig == "" {
		t.Fatal("expected X-King-Signature header, got none")
	}
	if len(sig) < 10 {
		t.Errorf("signature looks too short: %q", sig)
	}
}

func TestDispatcher_RetriesOnFailure(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
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

	if n := attempts.Load(); n < 3 {
		t.Errorf("expected at least 3 attempts (with retry), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Dispatcher.Test
// ---------------------------------------------------------------------------

func TestDispatcher_Test_Success(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := []config.WebhookConfig{{URL: srv.URL}}
	d := webhook.NewDispatcher(cfg, "test-kingdom", nil)

	if err := d.Test(); err != nil {
		t.Fatalf("Test(): %v", err)
	}

	if received.Load() != 1 {
		t.Errorf("expected 1 request, got %d", received.Load())
	}
}

func TestDispatcher_Test_MultipleHooks(t *testing.T) {
	var mu sync.Mutex
	var urls []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		urls = append(urls, r.RequestURI)
		mu.Unlock()
		w.WriteHeader(200)
	})

	srv1 := httptest.NewServer(handler)
	defer srv1.Close()
	srv2 := httptest.NewServer(handler)
	defer srv2.Close()

	cfg := []config.WebhookConfig{
		{URL: srv1.URL},
		{URL: srv2.URL},
	}
	d := webhook.NewDispatcher(cfg, "test-kingdom", nil)

	if err := d.Test(); err != nil {
		t.Fatalf("Test() with 2 hooks: %v", err)
	}

	mu.Lock()
	n := len(urls)
	mu.Unlock()
	if n != 2 {
		t.Errorf("expected 2 requests (one per hook), got %d", n)
	}
}

func TestDispatcher_Test_FailsOnError(t *testing.T) {
	cfg := []config.WebhookConfig{{URL: "http://localhost:1"}} // nothing listening
	d := webhook.NewDispatcher(cfg, "test-kingdom", nil)

	if err := d.Test(); err == nil {
		t.Fatal("expected error when hook is unreachable, got nil")
	}
}
