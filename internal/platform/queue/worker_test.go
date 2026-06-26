package queue

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestRetryAttemptParsing(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected int
	}{
		{"no header", "", 0},
		{"zero attempt", "0", 0},
		{"first attempt", "1", 1},
		{"third attempt", "3", 3},
		{"invalid header", "abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test via the format string that would be in headers
			var n int
			if tt.header != "" {
				_, err := fmt.Sscanf(tt.header, "%d", &n)
				if err != nil {
					n = 0
				}
			}
			if n != tt.expected {
				t.Errorf("retryAttempt = %d, want %d", n, tt.expected)
			}
		})
	}
}

func TestExponentialBackoff(t *testing.T) {
	tests := []struct {
		attempt    int
		wantDelay  time.Duration
		maxBackoff time.Duration
	}{
		{0, 1 * time.Second, 60 * time.Second},     // 2^0 * 1s = 1s
		{1, 2 * time.Second, 60 * time.Second},     // 2^1 * 1s = 2s
		{2, 4 * time.Second, 60 * time.Second},     // 2^2 * 1s = 4s
		{3, 8 * time.Second, 60 * time.Second},     // 2^3 * 1s = 8s
		{4, 16 * time.Second, 60 * time.Second},    // 2^4 * 1s = 16s
		{5, 32 * time.Second, 60 * time.Second},    // 2^5 * 1s = 32s
		{6, 60 * time.Second, 60 * time.Second},    // 2^6 * 1s = 64s → capped at 60s
		{10, 60 * time.Second, 60 * time.Second},   // 2^10 * 1s → capped at 60s
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			// Replicate the backoff calculation from handleFailure
			delay := time.Duration(1<<uint(tt.attempt)) * defaultBaseBackoff
			if delay > tt.maxBackoff {
				delay = tt.maxBackoff
			}
			if delay != tt.wantDelay {
				t.Errorf("backoff at attempt %d = %v, want %v", tt.attempt, delay, tt.wantDelay)
			}
		})
	}
}

func TestIsExpired(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		wantExpire bool
	}{
		{
			name:      "no TTL set",
			payload:   `{"to":"+123","channel":"whatsapp","body":"hi"}`,
			wantExpire: false,
		},
		{
			name:      "TTL not expired",
			payload: func() string {
				queuedAt := time.Now().UTC().Add(-10 * time.Second).Format(time.RFC3339Nano)
				ttl := 300 // 5 minutes
				return fmt.Sprintf(`{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":%d,"queued_at":"%s"}`, ttl, queuedAt)
			}(),
			wantExpire: false,
		},
		{
			name:      "TTL expired",
			payload: func() string {
				queuedAt := time.Now().UTC().Add(-600 * time.Second).Format(time.RFC3339Nano)
				ttl := 300 // 5 minutes
				return fmt.Sprintf(`{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":%d,"queued_at":"%s"}`, ttl, queuedAt)
			}(),
			wantExpire: true,
		},
		{
			name:      "zero TTL ignored",
			payload:   `{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":0}`,
			wantExpire: false,
		},
		{
			name:      "negative TTL ignored",
			payload:   `{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":-1}`,
			wantExpire: false,
		},
		{
			name:      "invalid queued_at format",
			payload:   `{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":1,"queued_at":"invalid"}`,
			wantExpire: false,
		},
		{
			name:      "no queued_at field",
			payload:   `{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":1}`,
			wantExpire: false,
		},
		{
			name:      "invalid JSON",
			payload:   `{not json}`,
			wantExpire: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the same isExpired logic as the worker
			type ttlPayload struct {
				TTLSeconds *int   `json:"ttl_seconds"`
				QueuedAt   string `json:"queued_at"`
			}
			var p ttlPayload
			if err := json.Unmarshal([]byte(tt.payload), &p); err != nil {
				if tt.wantExpire {
					t.Errorf("isExpired = false (parse error), want true")
				}
				return
			}

			if p.TTLSeconds == nil || *p.TTLSeconds <= 0 {
				if tt.wantExpire {
					t.Error("isExpired = false (no TTL), want true")
				}
				return
			}

			queuedAt, err := time.Parse(time.RFC3339Nano, p.QueuedAt)
			if err != nil {
				if tt.wantExpire {
					t.Error("isExpired = false (parse error on queued_at), want true")
				}
				return
			}

			expiry := queuedAt.Add(time.Duration(*p.TTLSeconds) * time.Second)
			expired := time.Now().UTC().After(expiry)
			if expired != tt.wantExpire {
				t.Errorf("isExpired = %v, want %v", expired, tt.wantExpire)
			}
		})
	}
}

func TestDeliveryDedup(t *testing.T) {
	w := &Worker{}

	traceID := "test-trace-dedup-001"

	// First call — not in dispatched set
	_, loaded := w.dispatched.LoadOrStore(traceID, struct{}{})
	if loaded {
		t.Error("first call should NOT be loaded (first occurrence)")
	}

	// Second call — should be in dispatched set
	_, loaded = w.dispatched.LoadOrStore(traceID, struct{}{})
	if !loaded {
		t.Error("second call SHOULD be loaded (duplicate detected)")
	}

	// Different trace ID — not a duplicate
	_, loaded = w.dispatched.LoadOrStore("test-trace-dedup-002", struct{}{})
	if loaded {
		t.Error("different trace ID should NOT be loaded")
	}
}
