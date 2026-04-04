package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexli18/claude-king/internal/config"
	"github.com/google/uuid"
)

// Payload is the JSON body sent to each webhook endpoint.
type Payload struct {
	Kingdom    string `json:"kingdom"`
	Vassal     string `json:"vassal,omitempty"`
	Event      string `json:"event"`
	Severity   string `json:"severity"`
	Summary    string `json:"summary,omitempty"`
	Timestamp  string `json:"timestamp"`
	DeliveryID string `json:"delivery_id"`
}

// Dispatcher fans out events to configured webhook endpoints.
type Dispatcher struct {
	hooks   []config.WebhookConfig
	kingdom string
	logger  *slog.Logger
	queue   chan Payload
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewDispatcher creates a Dispatcher. logger may be nil (uses slog.Default).
func NewDispatcher(hooks []config.WebhookConfig, kingdom string, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		hooks:   hooks,
		kingdom: kingdom,
		logger:  logger,
		queue:   make(chan Payload, 256),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Start launches the background delivery goroutine.
func (d *Dispatcher) Start() {
	go d.worker()
}

// Stop drains the queue and waits for the worker to exit.
func (d *Dispatcher) Stop() {
	close(d.stopCh)
	<-d.doneCh
}

// Send enqueues a payload for delivery. Non-blocking; drops if queue is full.
func (d *Dispatcher) Send(p Payload) {
	if p.Timestamp == "" {
		p.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if p.DeliveryID == "" {
		p.DeliveryID = uuid.New().String()
	}
	if p.Kingdom == "" {
		p.Kingdom = d.kingdom
	}
	select {
	case d.queue <- p:
	default:
		d.logger.Warn("webhook queue full, dropping event", "event", p.Event)
	}
}

// Test sends a synthetic test payload to all configured hooks, synchronously.
// Used by `king webhook test` CLI command.
func (d *Dispatcher) Test() error {
	p := Payload{
		Kingdom:    d.kingdom,
		Event:      "webhook_test",
		Severity:   "info",
		Summary:    "King webhook connectivity test",
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		DeliveryID: uuid.New().String(),
	}
	body, _ := json.Marshal(p)
	for i := range d.hooks {
		if err := d.post(&d.hooks[i], body, 10); err != nil {
			return fmt.Errorf("hook[%d] %s: %w", i, d.hooks[i].URL, err)
		}
	}
	return nil
}

func (d *Dispatcher) worker() {
	defer close(d.doneCh)
	for {
		select {
		case p := <-d.queue:
			for i := range d.hooks {
				d.deliver(&d.hooks[i], p)
			}
		case <-d.stopCh:
			// Drain remaining items before exit.
			for {
				select {
				case p := <-d.queue:
					for i := range d.hooks {
						d.deliver(&d.hooks[i], p)
					}
				default:
					return
				}
			}
		}
	}
}

// matches returns true if this payload should be delivered to the given hook.
// Empty On list means "deliver everything".
func matches(hook *config.WebhookConfig, p Payload) bool {
	if len(hook.On) == 0 {
		return true
	}
	for _, filter := range hook.On {
		if filter == p.Severity || filter == p.Event {
			return true
		}
	}
	return false
}

func (d *Dispatcher) deliver(hook *config.WebhookConfig, p Payload) {
	if !matches(hook, p) {
		return
	}

	body, err := json.Marshal(p)
	if err != nil {
		d.logger.Error("webhook marshal error", "err", err)
		return
	}

	maxRetries := hook.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	timeoutSec := hook.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 10
	}

	backoff := 500 * time.Millisecond
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := d.post(hook, body, timeoutSec); err == nil {
			return
		} else {
			d.logger.Warn("webhook delivery failed",
				"url", hook.URL,
				"attempt", attempt,
				"max", maxRetries,
				"err", err,
			)
		}
		if attempt < maxRetries {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	d.logger.Error("webhook delivery permanently failed", "url", hook.URL, "delivery_id", p.DeliveryID)
}

func (d *Dispatcher) post(hook *config.WebhookConfig, body []byte, timeoutSec int) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-king/webhook")

	if hook.Secret != "" {
		mac := hmac.New(sha256.New, []byte(hook.Secret))
		mac.Write(body)
		req.Header.Set("X-King-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	for k, v := range hook.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
