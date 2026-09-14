package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// mockJetStream implements jetstream.JetStream for unit tests.
type mockJetStream struct {
	jetstream.JetStream
	streams   map[string]jetstream.Stream
	streamErr error
}

func (m *mockJetStream) Stream(ctx context.Context, name string) (jetstream.Stream, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	s, ok := m.streams[name]
	if !ok {
		return nil, jetstream.ErrStreamNotFound
	}
	return s, nil
}

// mockStream implements jetstream.Stream for unit tests.
type mockStream struct {
	jetstream.Stream
	info      *jetstream.StreamInfo
	infoErr   error
	consumers []*jetstream.ConsumerInfo
	emptyList bool
}

func (m *mockStream) Info(ctx context.Context, opts ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error) {
	if m.infoErr != nil {
		return nil, m.infoErr
	}
	return m.info, nil
}

func (m *mockStream) ListConsumers(ctx context.Context) jetstream.ConsumerInfoLister {
	if m.emptyList {
		ch := make(chan *jetstream.ConsumerInfo)
		close(ch)
		return &mockConsumerInfoLister{ch: ch}
	}
	ch := make(chan *jetstream.ConsumerInfo, len(m.consumers))
	for _, c := range m.consumers {
		ch <- c
	}
	close(ch)
	return &mockConsumerInfoLister{ch: ch}
}

func (m *mockStream) ConsumerNames(ctx context.Context) jetstream.ConsumerNameLister {
	ch := make(chan string, len(m.consumers))
	for _, c := range m.consumers {
		ch <- c.Name
	}
	close(ch)
	return &mockConsumerNameLister{ch: ch}
}

func (m *mockStream) Consumer(ctx context.Context, name string) (jetstream.Consumer, error) {
	for _, c := range m.consumers {
		if c.Name == name {
			return &mockConsumer{info: c}, nil
		}
	}
	return nil, jetstream.ErrConsumerNotFound
}

type mockConsumerInfoLister struct {
	ch chan *jetstream.ConsumerInfo
}

func (m *mockConsumerInfoLister) Info() <-chan *jetstream.ConsumerInfo {
	return m.ch
}

func (m *mockConsumerInfoLister) Err() error {
	return nil
}

type mockConsumerNameLister struct {
	ch chan string
}

func (m *mockConsumerNameLister) Name() <-chan string {
	return m.ch
}

func (m *mockConsumerNameLister) Err() error {
	return nil
}

type mockConsumer struct {
	jetstream.Consumer
	info *jetstream.ConsumerInfo
}

func (m *mockConsumer) Info(ctx context.Context) (*jetstream.ConsumerInfo, error) {
	return m.info, nil
}

func parseQueueHealthReport(t *testing.T, res *mcp.CallToolResult) QueueHealthReport {
	t.Helper()
	if res.IsError {
		t.Fatalf("expected successful result, got error: %+v", res.Content)
	}
	text := extractQueueHealthText(t, res)
	var report QueueHealthReport
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatalf("failed to unmarshal QueueHealthReport: %v, raw text: %s", err, text)
	}
	return report
}

func extractQueueHealthText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatalf("tool result content is empty")
	}
	if tc, ok := mcp.AsTextContent(res.Content[0]); ok {
		return tc.Text
	}
	t.Fatalf("expected text content in tool result")
	return ""
}

func TestQueueHealth_ToolRegistration(t *testing.T) {
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	tools := srv.MCPServer.ListTools()

	var found bool
	for _, tool := range tools {
		if tool.Tool.Name == "inspect_queue_health" {
			found = true
			if tool.Tool.InputSchema.Type != "object" {
				t.Errorf("expected InputSchema.Type 'object', got %q", tool.Tool.InputSchema.Type)
			}
			if _, ok := tool.Tool.InputSchema.Properties["workspace_id"]; !ok {
				t.Errorf("expected 'workspace_id' property in InputSchema")
			}
			break
		}
	}
	if !found {
		t.Fatalf("inspect_queue_health tool was not registered in MCPServer")
	}
}

func TestQueueHealth_UnconfiguredJetStreamFallback(t *testing.T) {
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	ctx := context.Background()

	t.Run("without workspace_id", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		res, err := srv.handleInspectQueueHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleInspectQueueHealth returned unexpected error: %v", err)
		}

		report := parseQueueHealthReport(t, res)
		if report.Status != "offline" {
			t.Errorf("expected status 'offline', got %q", report.Status)
		}
		if len(report.Streams) != len(MonitoredStreams) {
			t.Errorf("expected %d streams, got %d", len(MonitoredStreams), len(report.Streams))
		}
		for _, s := range report.Streams {
			if s.Status != "offline" {
				t.Errorf("stream %s status = %q, want 'offline'", s.Name, s.Status)
			}
		}
	})

	t.Run("with workspace_id", func(t *testing.T) {
		wsID := uuid.New().String()
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": wsID,
		}

		res, err := srv.handleInspectQueueHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleInspectQueueHealth returned unexpected error: %v", err)
		}

		report := parseQueueHealthReport(t, res)
		if report.Status != "offline" {
			t.Errorf("expected status 'offline', got %q", report.Status)
		}
		if report.WorkspaceID != wsID {
			t.Errorf("expected workspace_id %q, got %q", wsID, report.WorkspaceID)
		}
	})
}

func TestQueueHealth_ValidationErrors(t *testing.T) {
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	ctx := context.Background()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"workspace_id": "not-a-valid-uuid",
	}

	res, err := srv.handleInspectQueueHealth(ctx, req)
	if err != nil {
		t.Fatalf("handleInspectQueueHealth returned error instead of CallToolResult: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected error result for invalid workspace_id, got success")
	}
}

func TestQueueHealth_StatusEvaluation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		setupStreams   func() map[string]jetstream.Stream
		wantStatus     string
		wantTotalMsgs  uint64
		wantTotalBytes uint64
	}{
		{
			name: "All healthy with normal limits",
			setupStreams: func() map[string]jetstream.Stream {
				return map[string]jetstream.Stream{
					"MESSAGES": &mockStream{
						info: &jetstream.StreamInfo{
							Config: jetstream.StreamConfig{Name: "MESSAGES"},
							State: jetstream.StreamState{
								Msgs:      50,
								Bytes:     2048,
								Consumers: 1,
								FirstSeq:  1,
								LastSeq:   50,
							},
						},
						consumers: []*jetstream.ConsumerInfo{
							{
								Name:          "outbound-worker",
								Stream:        "MESSAGES",
								NumPending:    10,
								NumAckPending: 5,
								NumWaiting:    2,
							},
						},
					},
					"WEBHOOKS": &mockStream{
						info: &jetstream.StreamInfo{
							Config: jetstream.StreamConfig{Name: "WEBHOOKS"},
							State: jetstream.StreamState{
								Msgs:      0,
								Bytes:     0,
								Consumers: 0,
							},
						},
					},
					"WEBHOOK_DELIVERIES": &mockStream{
						info: &jetstream.StreamInfo{
							Config: jetstream.StreamConfig{Name: "WEBHOOK_DELIVERIES"},
							State: jetstream.StreamState{
								Msgs:      20,
								Bytes:     1024,
								Consumers: 1,
								FirstSeq:  1,
								LastSeq:   20,
							},
						},
						consumers: []*jetstream.ConsumerInfo{
							{
								Name:          "webhook-worker",
								Stream:        "WEBHOOK_DELIVERIES",
								NumPending:    2,
								NumAckPending: 0,
								NumWaiting:    1,
							},
						},
					},
					"INBOUND": &mockStream{
						info: &jetstream.StreamInfo{
							Config: jetstream.StreamConfig{Name: "INBOUND"},
							State: jetstream.StreamState{
								Msgs:      5,
								Bytes:     500,
								Consumers: 1,
								FirstSeq:  1,
								LastSeq:   5,
							},
						},
						consumers: []*jetstream.ConsumerInfo{
							{
								Name:          "inbound-worker",
								Stream:        "INBOUND",
								NumPending:    0,
								NumAckPending: 0,
								NumWaiting:    1,
							},
						},
					},
				}
			},
			wantStatus:     "healthy",
			wantTotalMsgs:  75,
			wantTotalBytes: 3572,
		},
		{
			name: "Degraded status by consumer pending > 500",
			setupStreams: func() map[string]jetstream.Stream {
				return map[string]jetstream.Stream{
					"MESSAGES": &mockStream{
						info: &jetstream.StreamInfo{
							Config: jetstream.StreamConfig{Name: "MESSAGES"},
							State: jetstream.StreamState{
								Msgs:      600,
								Bytes:     10000,
								Consumers: 1,
							},
						},
						consumers: []*jetstream.ConsumerInfo{
							{
								Name:          "outbound-worker",
								Stream:        "MESSAGES",
								NumPending:    600, // > 500
								NumAckPending: 50,
							},
						},
					},
				}
			},
			wantStatus:     "degraded",
			wantTotalMsgs:  600,
			wantTotalBytes: 10000,
		},
		{
			name: "Degraded status by consumer ack pending > 200",
			setupStreams: func() map[string]jetstream.Stream {
				return map[string]jetstream.Stream{
					"WEBHOOK_DELIVERIES": &mockStream{
						info: &jetstream.StreamInfo{
							Config: jetstream.StreamConfig{Name: "WEBHOOK_DELIVERIES"},
							State: jetstream.StreamState{
								Msgs:      300,
								Bytes:     5000,
								Consumers: 1,
							},
						},
						consumers: []*jetstream.ConsumerInfo{
							{
								Name:          "webhook-worker",
								Stream:        "WEBHOOK_DELIVERIES",
								NumPending:    100,
								NumAckPending: 250, // > 200
							},
						},
					},
				}
			},
			wantStatus:     "degraded",
			wantTotalMsgs:  300,
			wantTotalBytes: 5000,
		},
		{
			name: "Backpressure by consumer pending > 1000",
			setupStreams: func() map[string]jetstream.Stream {
				return map[string]jetstream.Stream{
					"MESSAGES": &mockStream{
						info: &jetstream.StreamInfo{
							Config: jetstream.StreamConfig{Name: "MESSAGES"},
							State: jetstream.StreamState{
								Msgs:      1200,
								Bytes:     50000,
								Consumers: 1,
							},
						},
						consumers: []*jetstream.ConsumerInfo{
							{
								Name:          "outbound-worker",
								Stream:        "MESSAGES",
								NumPending:    1100, // > 1000
								NumAckPending: 50,
							},
						},
					},
				}
			},
			wantStatus:     "backpressure",
			wantTotalMsgs:  1200,
			wantTotalBytes: 50000,
		},
		{
			name: "Backpressure by consumer ack pending > 500",
			setupStreams: func() map[string]jetstream.Stream {
				return map[string]jetstream.Stream{
					"MESSAGES": &mockStream{
						info: &jetstream.StreamInfo{
							Config: jetstream.StreamConfig{Name: "MESSAGES"},
							State: jetstream.StreamState{
								Msgs:      800,
								Bytes:     30000,
								Consumers: 1,
							},
						},
						consumers: []*jetstream.ConsumerInfo{
							{
								Name:          "outbound-worker",
								Stream:        "MESSAGES",
								NumPending:    100,
								NumAckPending: 550, // > 500
							},
						},
					},
				}
			},
			wantStatus:     "backpressure",
			wantTotalMsgs:  800,
			wantTotalBytes: 30000,
		},
		{
			name: "Backpressure by stream message count >= 1000",
			setupStreams: func() map[string]jetstream.Stream {
				return map[string]jetstream.Stream{
					"MESSAGES": &mockStream{
						info: &jetstream.StreamInfo{
							Config: jetstream.StreamConfig{Name: "MESSAGES"},
							State: jetstream.StreamState{
								Msgs:      1000, // >= 1000 (MaxQueueDepth)
								Bytes:     40000,
								Consumers: 0,
							},
						},
					},
				}
			},
			wantStatus:     "backpressure",
			wantTotalMsgs:  1000,
			wantTotalBytes: 40000,
		},
		{
			name: "Partial streams found and others not found",
			setupStreams: func() map[string]jetstream.Stream {
				return map[string]jetstream.Stream{
					"MESSAGES": &mockStream{
						info: &jetstream.StreamInfo{
							Config: jetstream.StreamConfig{Name: "MESSAGES"},
							State: jetstream.StreamState{
								Msgs:      10,
								Bytes:     500,
								Consumers: 0,
							},
						},
					},
				}
			},
			wantStatus:     "healthy",
			wantTotalMsgs:  10,
			wantTotalBytes: 500,
		},
		{
			name: "Stream error degrades status",
			setupStreams: func() map[string]jetstream.Stream {
				return map[string]jetstream.Stream{
					"MESSAGES": &mockStream{
						infoErr: errors.New("internal nats cluster partition"),
					},
				}
			},
			wantStatus:     "degraded",
			wantTotalMsgs:  0,
			wantTotalBytes: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockJS := &mockJetStream{
				streams: tc.setupStreams(),
			}

			srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "", WithJetStream(mockJS))

			req := mcp.CallToolRequest{}
			res, err := srv.handleInspectQueueHealth(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			report := parseQueueHealthReport(t, res)
			if report.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", report.Status, tc.wantStatus)
			}
			if report.TotalMessages != tc.wantTotalMsgs {
				t.Errorf("total_messages = %d, want %d", report.TotalMessages, tc.wantTotalMsgs)
			}
			if report.TotalBytes != tc.wantTotalBytes {
				t.Errorf("total_bytes = %d, want %d", report.TotalBytes, tc.wantTotalBytes)
			}
		})
	}
}

func TestQueueHealth_ConsumerNamesFallback(t *testing.T) {
	ctx := context.Background()

	// Stream that returns empty channel from ListConsumers, but has ConsumerNames
	stream := &mockStream{
		emptyList: true,
		info: &jetstream.StreamInfo{
			Config: jetstream.StreamConfig{Name: "MESSAGES"},
			State: jetstream.StreamState{
				Msgs:      25,
				Bytes:     1000,
				Consumers: 1,
			},
		},
		consumers: []*jetstream.ConsumerInfo{
			{
				Name:          "fallback-consumer",
				Stream:        "MESSAGES",
				NumPending:    15,
				NumAckPending: 2,
				NumWaiting:    3,
			},
		},
	}

	mockJS := &mockJetStream{
		streams: map[string]jetstream.Stream{
			"MESSAGES": stream,
		},
	}

	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "", WithJetStream(mockJS))

	req := mcp.CallToolRequest{}
	res, err := srv.handleInspectQueueHealth(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	report := parseQueueHealthReport(t, res)
	if report.Status != "healthy" {
		t.Errorf("status = %q, want 'healthy'", report.Status)
	}

	var foundConsumer bool
	for _, s := range report.Streams {
		if s.Name == "MESSAGES" {
			for _, c := range s.ConsumerStats {
				if c.Name == "fallback-consumer" {
					foundConsumer = true
					if c.NumPending != 15 {
						t.Errorf("num_pending = %d, want 15", c.NumPending)
					}
					if c.NumAckPending != 2 {
						t.Errorf("num_ack_pending = %d, want 2", c.NumAckPending)
					}
					if c.NumWaiting != 3 {
						t.Errorf("num_waiting = %d, want 3", c.NumWaiting)
					}
				}
			}
		}
	}
	if !foundConsumer {
		t.Fatalf("expected fallback-consumer to be resolved via ConsumerNames")
	}
}

func TestQueueHealth_LiveNATSIntegration(t *testing.T) {
	nc, err := nats.Connect(nats.DefaultURL, nats.Timeout(2*time.Second))
	if err != nil {
		t.Skipf("NATS server unavailable at %s: %v", nats.DefaultURL, err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("failed to initialize jetstream: %v", err)
	}

	ctx := context.Background()

	// Clean slate: delete existing stream before test
	_ = js.DeleteStream(ctx, "MESSAGES")
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      "MESSAGES",
		Subjects:  []string{"messages.>"},
		Retention: jetstream.WorkQueuePolicy,
		MaxMsgs:   1000,
	})
	if err != nil {
		t.Fatalf("failed to create/update MESSAGES stream: %v", err)
	}
	defer func() { _ = js.DeleteStream(ctx, "MESSAGES") }()

	_, err = stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:   "test-inspect-consumer",
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("failed to create consumer: %v", err)
	}

	// Test with WithNATSConn
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "", WithNATSConn(nc))

	req := mcp.CallToolRequest{}
	res, err := srv.handleInspectQueueHealth(ctx, req)
	if err != nil {
		t.Fatalf("handleInspectQueueHealth failed on live NATS: %v", err)
	}

	report := parseQueueHealthReport(t, res)
	if report.Status != "healthy" {
		t.Errorf("live NATS expected 'healthy', got %q", report.Status)
	}

	var foundStream bool
	for _, s := range report.Streams {
		if s.Name == "MESSAGES" {
			foundStream = true
			if s.Status != "healthy" {
				t.Errorf("stream MESSAGES status = %q, want 'healthy'", s.Status)
			}
		}
	}
	if !foundStream {
		t.Errorf("stream MESSAGES not found in live report")
	}
}
