package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// MonitoredStreams lists the JetStream streams checked by inspect_queue_health.
var MonitoredStreams = []string{
	"MESSAGES",
	"WEBHOOKS",
	"WEBHOOK_DELIVERIES",
	"INBOUND",
}

// QueueHealthReport contains the aggregated queue health status and per-stream metrics.
type QueueHealthReport struct {
	Status        string               `json:"status"` // "healthy", "degraded", "backpressure", "offline"
	Timestamp     time.Time            `json:"timestamp"`
	WorkspaceID   string               `json:"workspace_id,omitempty"`
	TotalMessages uint64               `json:"total_messages"`
	TotalBytes    uint64               `json:"total_bytes"`
	Streams       []StreamHealthReport `json:"streams"`
	Details       string               `json:"details,omitempty"`
}

// StreamHealthReport contains statistics and consumer health metrics for a single stream.
type StreamHealthReport struct {
	Name          string                 `json:"name"`
	Status        string                 `json:"status"` // "healthy", "degraded", "backpressure", "not_found", "error", "offline"
	Msgs          uint64                 `json:"msgs"`
	Bytes         uint64                 `json:"bytes"`
	Consumers     int                    `json:"consumers"`
	FirstSeq      uint64                 `json:"first_seq"`
	LastSeq       uint64                 `json:"last_seq"`
	ConsumerStats []ConsumerHealthReport `json:"consumer_stats"`
	Error         string                 `json:"error,omitempty"`
}

// ConsumerHealthReport contains lag and ack metrics for an individual consumer.
type ConsumerHealthReport struct {
	Name          string `json:"name"`
	Stream        string `json:"stream"`
	NumPending    uint64 `json:"num_pending"`
	NumAckPending int    `json:"num_ack_pending"`
	NumWaiting    int    `json:"num_waiting"`
}

// QueueInspector is the port interface for inspecting message queue health and telemetry.
type QueueInspector interface {
	InspectQueueHealth(ctx context.Context, workspaceID *uuid.UUID) (*QueueHealthReport, error)
}

// jetstreamQueueInspector is the default QueueInspector adapter inspecting NATS JetStream.
type jetstreamQueueInspector struct {
	js jetstream.JetStream
	nc *nats.Conn
}

// NewJetStreamQueueInspector returns a QueueInspector backed by NATS JetStream.
func NewJetStreamQueueInspector(js jetstream.JetStream, nc *nats.Conn) QueueInspector {
	return &jetstreamQueueInspector{
		js: js,
		nc: nc,
	}
}

// extractConsumerInfo maps a jetstream.ConsumerInfo to a ConsumerHealthReport.
func extractConsumerInfo(cInfo *jetstream.ConsumerInfo, streamName string) ConsumerHealthReport {
	return ConsumerHealthReport{
		Name:          cInfo.Name,
		Stream:        streamName,
		NumPending:    cInfo.NumPending,
		NumAckPending: cInfo.NumAckPending,
		NumWaiting:    cInfo.NumWaiting,
	}
}

func (j *jetstreamQueueInspector) InspectQueueHealth(ctx context.Context, workspaceID *uuid.UUID) (*QueueHealthReport, error) {
	var wsIDStr string
	if workspaceID != nil {
		wsIDStr = workspaceID.String()
	}

	js := j.js
	if js == nil && j.nc != nil && !j.nc.IsClosed() {
		if newJS, err := jetstream.New(j.nc); err == nil {
			js = newJS
		}
	}

	// If JetStream is not configured or connection is closed, return graceful offline report.
	if js == nil || (j.nc != nil && j.nc.IsClosed()) {
		report := &QueueHealthReport{
			Status:        "offline",
			Timestamp:     time.Now().UTC(),
			WorkspaceID:   wsIDStr,
			TotalMessages: 0,
			TotalBytes:    0,
			Streams:       make([]StreamHealthReport, 0, len(MonitoredStreams)),
			Details:       "JetStream is not configured or unavailable",
		}
		for _, streamName := range MonitoredStreams {
			report.Streams = append(report.Streams, StreamHealthReport{
				Name:          streamName,
				Status:        "offline",
				ConsumerStats: make([]ConsumerHealthReport, 0),
			})
		}
		return report, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	report := &QueueHealthReport{
		Timestamp:     time.Now().UTC(),
		WorkspaceID:   wsIDStr,
		Streams:       make([]StreamHealthReport, 0, len(MonitoredStreams)),
		TotalMessages: 0,
		TotalBytes:    0,
	}

	for _, streamName := range MonitoredStreams {
		streamRep := StreamHealthReport{
			Name:          streamName,
			ConsumerStats: make([]ConsumerHealthReport, 0),
		}

		stream, err := js.Stream(ctx, streamName)
		if err != nil {
			if errors.Is(err, jetstream.ErrStreamNotFound) || strings.Contains(strings.ToLower(err.Error()), "stream not found") {
				streamRep.Status = "not_found"
			} else {
				streamRep.Status = "error"
				streamRep.Error = err.Error()
			}
			report.Streams = append(report.Streams, streamRep)
			continue
		}

		info, err := stream.Info(ctx)
		if err != nil {
			streamRep.Status = "error"
			streamRep.Error = err.Error()
			report.Streams = append(report.Streams, streamRep)
			continue
		}

		streamRep.Msgs = info.State.Msgs
		streamRep.Bytes = info.State.Bytes
		streamRep.Consumers = info.State.Consumers
		streamRep.FirstSeq = info.State.FirstSeq
		streamRep.LastSeq = info.State.LastSeq

		report.TotalMessages += info.State.Msgs
		report.TotalBytes += info.State.Bytes

		// Query consumers
		if lister := stream.ListConsumers(ctx); lister != nil {
			for cInfo := range lister.Info() {
				if cInfo != nil {
					streamRep.ConsumerStats = append(streamRep.ConsumerStats, extractConsumerInfo(cInfo, streamName))
				}
			}
		}

		// Fallback if ListConsumers returned 0 but stream reported consumers > 0
		if len(streamRep.ConsumerStats) == 0 && info.State.Consumers > 0 {
			if namesLister := stream.ConsumerNames(ctx); namesLister != nil {
				for cName := range namesLister.Name() {
					if c, err := stream.Consumer(ctx, cName); err == nil {
						if cInfo, err := c.Info(ctx); err == nil && cInfo != nil {
							streamRep.ConsumerStats = append(streamRep.ConsumerStats, extractConsumerInfo(cInfo, streamName))
						}
					}
				}
			}
		}

		// Evaluate individual stream status
		sStatus := "healthy"
		for _, cs := range streamRep.ConsumerStats {
			if cs.NumPending > 1000 || cs.NumAckPending > 500 {
				sStatus = "backpressure"
				break
			} else if cs.NumPending > 500 || cs.NumAckPending > 200 {
				if sStatus != "backpressure" {
					sStatus = "degraded"
				}
			}
		}
		if streamRep.Msgs >= 1000 && sStatus != "backpressure" {
			sStatus = "backpressure"
		} else if streamRep.Msgs > 500 && sStatus == "healthy" {
			sStatus = "degraded"
		}
		streamRep.Status = sStatus
		report.Streams = append(report.Streams, streamRep)
	}

	// Synthesize global health status
	hasBackpressure := false
	hasDegraded := false
	hasError := false
	allNotFound := true

	for _, sRep := range report.Streams {
		if sRep.Status != "not_found" {
			allNotFound = false
		}
		switch sRep.Status {
		case "backpressure":
			hasBackpressure = true
		case "degraded":
			hasDegraded = true
		case "error":
			hasError = true
		}
	}

	if hasBackpressure {
		report.Status = "backpressure"
		report.Details = "One or more streams/consumers are experiencing backpressure (backlog or unacknowledged messages exceeded safe limits)"
	} else if hasDegraded || hasError {
		report.Status = "degraded"
		report.Details = "One or more streams/consumers are degraded (elevated backlog, pending ACKs, or stream errors)"
	} else {
		report.Status = "healthy"
		if allNotFound {
			report.Details = "JetStream is operational; monitored streams are not yet provisioned"
		} else {
			report.Details = "All monitored streams and consumers are healthy and within operating limits"
		}
	}

	return report, nil
}

func (s *Server) handleInspectQueueHealth(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	var wsID *uuid.UUID
	if wsIDStr != "" {
		parsed, err := uuid.Parse(wsIDStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id: %v", err)), nil
		}
		wsID = &parsed
	}

	inspector := s.queueInspector
	if inspector == nil {
		inspector = NewJetStreamQueueInspector(s.js, s.nc)
	}

	report, err := inspector.InspectQueueHealth(ctx, wsID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to inspect queue health: %v", err)), nil
	}

	resBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal queue health report: %v", err)), nil
	}
	return mcp.NewToolResultText(string(resBytes)), nil
}
