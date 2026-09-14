package mcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/pablojhp.pergo/internal/platform/netpolicy"
	"github.com/pablojhp.pergo/internal/webhook"
)

// WebhookDispatchOptions contains parameters for executing a webhook HTTP dispatch.
type WebhookDispatchOptions struct {
	TargetURL    string
	Payload      []byte
	Secret       []byte
	EventType    string
	TraceID      string
	MessageID    string
	ExtraHeaders map[string]string
	Client       *http.Client
}

// WebhookDispatchResult captures the HTTP execution telemetry of a webhook dispatch.
type WebhookDispatchResult struct {
	StatusCode   int
	LatencyMS    int64
	Success      bool
	ResponseBody string
	Error        string
}

// executeWebhookDispatch executes an HTTP POST dispatch to the target URL with safe client, signing, and telemetry.
func executeWebhookDispatch(ctx context.Context, opts WebhookDispatchOptions) (*WebhookDispatchResult, error) {
	u, err := url.ParseRequestURI(opts.TargetURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid destination URL %q: scheme must be http or https", opts.TargetURL)
	}

	var signature string
	if len(opts.Secret) > 0 {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		signature = webhook.SignPayload(opts.Payload, opts.Secret, timestamp)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.TargetURL, bytes.NewReader(opts.Payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if signature != "" {
		req.Header.Set("X-PerGo-Signature", signature)
	}
	for k, v := range opts.ExtraHeaders {
		req.Header.Set(k, v)
	}
	if opts.EventType != "" {
		req.Header.Set("X-PerGo-Event", opts.EventType)
	}
	if opts.TraceID != "" {
		req.Header.Set("X-Trace-ID", opts.TraceID)
	}
	if opts.MessageID != "" {
		req.Header.Set("X-Message-ID", opts.MessageID)
	}

	client := opts.Client
	if client == nil {
		client = netpolicy.NewSafeClient(netpolicy.WithTimeout(10 * time.Second))
	}

	start := time.Now()
	resp, reqErr := client.Do(req)
	latency := time.Since(start).Milliseconds()

	result := &WebhookDispatchResult{
		LatencyMS: latency,
	}

	if reqErr != nil {
		result.Success = false
		result.Error = fmt.Sprintf("dispatch error: %v", reqErr)
		return result, nil
	}

	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if len(respBody) > 0 {
		result.ResponseBody = string(respBody)
	}
	if !result.Success {
		result.Error = fmt.Sprintf("HTTP %s", resp.Status)
	}

	return result, nil
}
