package telegram_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/channel/telegram"
	"github.com/pablojhp.pergo/internal/client"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeConnectionLister struct {
	conns []*repository.Connection
}

func (f *fakeConnectionLister) ListAll(ctx context.Context) ([]*repository.Connection, error) {
	return f.conns, nil
}

type fakeInboundProcessor struct {
	mu     sync.Mutex
	events []*inbound.InboundEvent
	notify chan struct{}
}

func (f *fakeInboundProcessor) Process(ctx context.Context, ev *inbound.InboundEvent) error {
	f.mu.Lock()
	f.events = append(f.events, ev)
	f.mu.Unlock()
	select {
	case f.notify <- struct{}{}:
	default:
	}
	return nil
}

func TestPollerManager_LongPollingEndToEnd(t *testing.T) {
	var deleteWebhookCount atomic.Int32
	var pollCalls atomic.Int32
	var seenSecondOffset atomic.Value

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/botsolo_bot_token/deleteWebhook":
			deleteWebhookCount.Add(1)
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		case "/botsolo_bot_token/getUpdates":
			callNum := pollCalls.Add(1)
			if callNum == 1 {
				_, _ = w.Write([]byte(`{
					"ok": true,
					"result": [{
						"update_id": 700,
						"message": {
							"message_id": 11,
							"date": 1700000100,
							"from": {"id": 555, "first_name": "Ana", "username": "ana_dev"},
							"chat": {"id": 555, "type": "private"},
							"voice": {"file_id": "voice_1", "mime_type": "audio/ogg"}
						}
					}]
				}`))
				return
			}
			seenSecondOffset.Store(r.URL.Query().Get("offset"))
			time.Sleep(30 * time.Millisecond)
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		case "/botsolo_bot_token/getFile":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"voice/note1.ogg"}}`))
		case "/file/botsolo_bot_token/voice/note1.ogg":
			w.Header().Set("Content-Type", "audio/ogg")
			_, _ = w.Write([]byte("OggS-telegram-voice-payload"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	wsID := uuid.New()
	connID := uuid.New()
	lister := &fakeConnectionLister{
		conns: []*repository.Connection{
			{
				ID:             connID,
				WorkspaceID:    wsID,
				Channel:        "telegram",
				Status:         "active",
				SenderIdentity: "@solo_bot",
				Credentials:    []byte(`{"token":"solo_bot_token","secret_token":"sec_xyz","bot_username":"@solo_bot"}`),
			},
		},
	}

	proc := &fakeInboundProcessor{notify: make(chan struct{}, 4)}
	botClient := client.NewTelegramBotClient(ts.Client(), ts.URL)
	dlEngine := media.NewEngineWithStorage(nil)

	mgr := telegram.NewPollerManager(
		lister,
		botClient,
		proc,
		dlEngine,
		telegram.WithDiscoveryInterval(50*time.Millisecond),
		telegram.WithPollTimeoutSeconds(1),
		telegram.WithErrorBackoff(20*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr.Start(ctx)

	select {
	case <-proc.notify:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for TelegramPollerManager to process inbound update")
	}

	// Wait briefly for second poll call to verify offset progression and single deleteWebhook
	require.Eventually(t, func() bool {
		return pollCalls.Load() >= 2
	}, 2*time.Second, 20*time.Millisecond)

	mgr.Stop()

	assert.Equal(t, int32(1), deleteWebhookCount.Load(), "deleteWebhook should be called exactly once per active connection")
	assert.Equal(t, "701", seenSecondOffset.Load(), "subsequent getUpdates call should advance offset to update_id + 1")

	proc.mu.Lock()
	defer proc.mu.Unlock()
	require.Len(t, proc.events, 1)
	ev := proc.events[0]
	assert.Equal(t, wsID, ev.WorkspaceID)
	assert.Equal(t, connID, ev.ConnectionID)
	assert.Equal(t, "telegram", ev.Channel)
	assert.Equal(t, "555", ev.From)
	assert.Equal(t, "Ana", ev.SenderName)
	require.NotNil(t, ev.Media)
	assert.Equal(t, "voice", ev.Media.MediaType)
	assert.Equal(t, []byte("OggS-telegram-voice-payload"), ev.Media.Bytes)
}
