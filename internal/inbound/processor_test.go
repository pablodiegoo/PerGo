package inbound_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

// getTestPool connects to a local test PostgreSQL database.
func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available at %s: %v", dsn, err)
	}

	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed at %s: %v", dsn, err)
	}

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to wrap pool as sql.DB: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		pool.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	return pool
}

// fakePublisher records published messages.
type fakePublisher struct {
	published []struct {
		subject string
		data    []byte
		traceID string
	}
	err error
}

func (f *fakePublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, struct {
		subject string
		data    []byte
		traceID string
	}{subject, data, traceID})
	return nil
}

// fakeMediaEngine records uploads and processes.
type fakeMediaEngine struct {
	downloadFn        func(ctx context.Context, url string, headers map[string]string, maxBytes int64) (*media.DownloadResult, error)
	uploadFn          func(ctx context.Context, key string, data []byte, contentType string) error
	processOutboundFn func(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error)
	processInboundFn  func(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error)

	inboundCalls []struct {
		workspaceID uuid.UUID
		mediaType   string
		data        []byte
	}
	outboundCalls []struct {
		workspaceID uuid.UUID
		mediaURL    string
	}
}

func (f *fakeMediaEngine) Download(ctx context.Context, url string, headers map[string]string, maxBytes int64) (*media.DownloadResult, error) {
	if f.downloadFn != nil {
		return f.downloadFn(ctx, url, headers, maxBytes)
	}
	return nil, nil
}

func (f *fakeMediaEngine) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	if f.uploadFn != nil {
		return f.uploadFn(ctx, key, data, contentType)
	}
	return nil
}

func (f *fakeMediaEngine) ProcessOutbound(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error) {
	f.outboundCalls = append(f.outboundCalls, struct {
		workspaceID uuid.UUID
		mediaURL    string
	}{workspaceID, mediaURL})
	if f.processOutboundFn != nil {
		return f.processOutboundFn(ctx, workspaceID, mediaURL)
	}
	return "", nil
}

func (f *fakeMediaEngine) ProcessInbound(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error) {
	f.inboundCalls = append(f.inboundCalls, struct {
		workspaceID uuid.UUID
		mediaType   string
		data        []byte
	}{workspaceID, mediaType, data})
	if f.processInboundFn != nil {
		return f.processInboundFn(ctx, workspaceID, mediaType, data)
	}
	return "", nil
}

func (f *fakeMediaEngine) ExtractAudioTelemetry(data []byte, contentType string) (*media.AudioTelemetry, error) {
	return media.ExtractAudioTelemetry(data, contentType)
}

// fakeAuditWriter records audit events.
type fakeAuditWriter struct {
	events []audit.Event
	err    error
}

func (f *fakeAuditWriter) Write(event audit.Event) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, event)
	return nil
}

func (f *fakeAuditWriter) Close() error {
	return nil
}

func (f *fakeAuditWriter) EnsurePartitions(ctx context.Context) error {
	return nil
}

func TestInboundProcessor_Process(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	// Create test workspace
	wsName := "inbound_proc_test_" + uuid.New().String()
	ws, err := wsRepo.Create(ctx, wsName)
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	t.Run("Standard text message ingestion", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-msg-123",
			Channel:     "telegram",
			From:        "user-chat-99",
			To:          "@test_bot",
			Body:        "Hello PerGo!",
		}

		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		// Verify NATS Publish
		if len(pub.published) != 1 {
			t.Fatalf("expected 1 published event, got %d", len(pub.published))
		}
		pubEvent := pub.published[0]
		if pubEvent.subject != "inbound.events."+ws.ID.String() {
			t.Errorf("got NATS subject %s, want inbound.events.%s", pubEvent.subject, ws.ID.String())
		}

		var payload inbound.InboundEventPayload
		if err := json.Unmarshal(pubEvent.data, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		if payload.Body != "Hello PerGo!" {
			t.Errorf("got payload body %q, want %q", payload.Body, "Hello PerGo!")
		}
		if payload.Channel != "telegram" {
			t.Errorf("got payload channel %q, want %q", payload.Channel, "telegram")
		}

		// Verify Recipient Session
		sess, err := sessRepo.Get(ctx, ws.ID, "user-chat-99", "telegram", "@test_bot")
		if err != nil {
			t.Fatalf("failed to get recipient session: %v", err)
		}
		if sess.RecipientPhone != "user-chat-99" {
			t.Errorf("got session recipient %q, want %q", sess.RecipientPhone, "user-chat-99")
		}
	})

	t.Run("Deduplication prevents double processing", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-msg-dup",
			Channel:     "whatsapp",
			From:        "5511999990000",
			To:          "5511888880000",
			Body:        "First attempt",
		}

		// 1st run: unique
		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed on first run: %v", err)
		}

		// 2nd run: duplicate
		event.Body = "Second attempt"
		err = proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed on second run: %v", err)
		}

		// Verify only 1 message was published
		if len(pub.published) != 1 {
			t.Errorf("expected 1 published event, got %d", len(pub.published))
		}
	})

	t.Run("Media upload and mapping", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{
			processInboundFn: func(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error) {
				return "/media/" + workspaceID.String() + "/abcde12345.jpg", nil
			},
		}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-media-1",
			Channel:     "whatsapp_cloud",
			From:        "sender-123",
			To:          "receiver-456",
			Media: &inbound.InboundMedia{
				Bytes:     []byte("fake-image-bytes"),
				MediaType: "image",
				Filename:  "picture.jpg",
				Caption:   "Beautiful sunset",
			},
		}

		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		// Verify Media Engine Inbound Call
		if len(me.inboundCalls) != 1 {
			t.Fatalf("expected 1 ProcessInbound call, got %d", len(me.inboundCalls))
		}
		call := me.inboundCalls[0]
		if call.mediaType != "image" {
			t.Errorf("got mediaType %s, want image", call.mediaType)
		}
		if string(call.data) != "fake-image-bytes" {
			t.Errorf("got data %s, want fake-image-bytes", string(call.data))
		}

		// Verify Media URL in payload
		var payload inbound.InboundEventPayload
		json.Unmarshal(pub.published[0].data, &payload)
		if payload.Media == nil {
			t.Fatal("expected media in payload")
		}
		if payload.Media.MediaType != "image" {
			t.Errorf("got media type %s, want image", payload.Media.MediaType)
		}
		if !strings.HasPrefix(payload.Media.MediaURL, "/media/"+ws.ID.String()+"/") {
			t.Errorf("invalid media URL format: %s", payload.Media.MediaURL)
		}
	})

	t.Run("Audio voice note enriched with acoustic telemetry RMS and duration", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{
			processInboundFn: func(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error) {
				return "/media/" + workspaceID.String() + "/voice_note.ogg", nil
			},
		}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

		// Create a synthetic WAV or Ogg audio byte payload
		sine := make([]int16, 16000) // 1 second at 16kHz
		for i := range sine {
			sine[i] = 16384 // non-zero amplitude
		}
		buf := new(bytes.Buffer)
		buf.WriteString("RIFF")
		binary.Write(buf, binary.LittleEndian, uint32(36+len(sine)*2))
		buf.WriteString("WAVEfmt ")
		binary.Write(buf, binary.LittleEndian, uint32(16))
		binary.Write(buf, binary.LittleEndian, uint16(1))
		binary.Write(buf, binary.LittleEndian, uint16(1))
		binary.Write(buf, binary.LittleEndian, uint32(16000))
		binary.Write(buf, binary.LittleEndian, uint32(32000))
		binary.Write(buf, binary.LittleEndian, uint16(2))
		binary.Write(buf, binary.LittleEndian, uint16(16))
		buf.WriteString("data")
		binary.Write(buf, binary.LittleEndian, uint32(len(sine)*2))
		for _, s := range sine {
			binary.Write(buf, binary.LittleEndian, s)
		}
		audioBytes := buf.Bytes()

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-audio-msg-" + uuid.New().String(),
			Channel:     "whatsapp_cloud",
			From:        "+5511999999999",
			To:          "+5511888888888",
			Media: &inbound.InboundMedia{
				Bytes:     audioBytes,
				MediaType: "audio",
				Filename:  "voice.ogg",
			},
		}

		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		if len(pub.published) == 0 {
			t.Fatal("expected message to be published to NATS")
		}

		var payload inbound.InboundEventPayload
		if err := json.Unmarshal(pub.published[0].data, &payload); err != nil {
			t.Fatalf("failed to unmarshal published payload: %v", err)
		}

		if payload.Media == nil {
			t.Fatal("expected payload.Media to be present")
		}
		if payload.Media.MediaType != "audio" {
			t.Errorf("expected media_type = audio, got %s", payload.Media.MediaType)
		}
		if payload.Media.DurationMS != 1000 {
			t.Errorf("expected duration_ms = 1000, got %d", payload.Media.DurationMS)
		}
		if payload.Media.RMSEnergy == nil || *payload.Media.RMSEnergy <= 0.0 {
			t.Errorf("expected valid rms_energy, got %v", payload.Media.RMSEnergy)
		}
		if payload.Media.Telemetry == nil {
			t.Fatal("expected payload.Media.Telemetry to be populated")
		}
		if payload.Media.Telemetry.DurationMS != 1000 {
			t.Errorf("expected telemetry duration_ms = 1000, got %d", payload.Media.Telemetry.DurationMS)
		}
		if payload.Media.Telemetry.SampleRate != 16000 {
			t.Errorf("expected telemetry sample_rate = 16000, got %d", payload.Media.Telemetry.SampleRate)
		}
	})

	t.Run("Response latency and timing telemetry extracted from prior outbound message", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

		recipient := "+5511999991111"
		senderIdentity := "+5511888882222"
		outboundTime := time.Now().UTC().Add(-2500 * time.Millisecond)

		// Seed prior outbound message
		err := sessRepo.RecordOutbound(ctx, ws.ID, recipient, "whatsapp_cloud", senderIdentity, outboundTime)
		if err != nil {
			t.Fatalf("failed to seed outbound session: %v", err)
		}

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-timing-msg-1",
			Channel:     "whatsapp_cloud",
			From:        recipient,
			To:          senderIdentity,
			Body:        "Sim, concordo com a pesquisa.",
		}

		err = proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		if len(pub.published) != 1 {
			t.Fatalf("expected 1 published event, got %d", len(pub.published))
		}

		var payload inbound.InboundEventPayload
		if err := json.Unmarshal(pub.published[0].data, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		if payload.Timing == nil {
			t.Fatal("expected payload.Timing to be present")
		}
		if payload.Timing.ResponseLatencyMS == nil {
			t.Fatal("expected payload.Timing.ResponseLatencyMS to be present")
		}
		latency := *payload.Timing.ResponseLatencyMS
		if latency < 2400 || latency > 3500 {
			t.Errorf("expected latency ~2500ms, got %d", latency)
		}
		if payload.Timing.LastOutboundAt == "" {
			t.Error("expected payload.Timing.LastOutboundAt to be populated")
		}
	})

	t.Run("Timing telemetry uses OccurredAt provider timestamp when present", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

		recipient := "+5511999992222"
		senderIdentity := "+5511888883333"
		outboundTime := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
		occurredAt := time.Date(2026, 8, 28, 10, 0, 3, 500000000, time.UTC) // exactly 3500ms later

		err := sessRepo.RecordOutbound(ctx, ws.ID, recipient, "whatsapp_cloud", senderIdentity, outboundTime)
		if err != nil {
			t.Fatalf("failed to seed outbound session: %v", err)
		}

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-timing-msg-provider-ts",
			Channel:     "whatsapp_cloud",
			From:        recipient,
			To:          senderIdentity,
			Body:        "Sim, confirmo.",
			OccurredAt:  occurredAt,
		}

		err = proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		if len(pub.published) != 1 {
			t.Fatalf("expected 1 published event, got %d", len(pub.published))
		}

		var payload inbound.InboundEventPayload
		if err := json.Unmarshal(pub.published[0].data, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		if payload.Timing == nil {
			t.Fatal("expected payload.Timing to be present")
		}
		if payload.Timing.ResponseLatencyMS == nil {
			t.Fatal("expected payload.Timing.ResponseLatencyMS to be present")
		}
		latency := *payload.Timing.ResponseLatencyMS
		if latency != 3500 {
			t.Errorf("expected latency exactly 3500ms, got %d", latency)
		}
		if payload.Timestamp != occurredAt.Format(time.RFC3339) {
			t.Errorf("expected payload.Timestamp = %s, got %s", occurredAt.Format(time.RFC3339), payload.Timestamp)
		}
	})

	t.Run("Timing telemetry omitted when no prior outbound message exists", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-no-timing-msg",
			Channel:     "whatsapp_cloud",
			From:        "+5511999993333",
			To:          "+5511888884444",
			Body:        "Primeira mensagem de lead",
		}

		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		if len(pub.published) != 1 {
			t.Fatalf("expected 1 published event, got %d", len(pub.published))
		}

		var payload inbound.InboundEventPayload
		if err := json.Unmarshal(pub.published[0].data, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		if payload.Timing != nil {
			t.Errorf("expected payload.Timing to be nil for initial message, got %+v", payload.Timing)
		}

		// Also verify raw JSON does not contain "timing" field
		var rawMap map[string]any
		if err := json.Unmarshal(pub.published[0].data, &rawMap); err != nil {
			t.Fatalf("failed to unmarshal raw map: %v", err)
		}
		if _, ok := rawMap["timing"]; ok {
			t.Errorf("expected 'timing' field to be omitted from JSON, got %v", rawMap["timing"])
		}
	})

	t.Run("PII Opt-in opt-out filters", func(t *testing.T) {
		// Update workspace PIIOptIn to false
		_, err = pool.Exec(ctx, "UPDATE workspaces SET pii_opt_in = FALSE WHERE id = $1", ws.ID)
		if err != nil {
			t.Fatalf("failed to update workspace PII: %v", err)
		}

		pub := &fakePublisher{}
		me := &fakeMediaEngine{}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-pii-opt-out",
			Channel:     "telegram",
			From:        "sender-pii",
			To:          "receiver-pii",
			Body:        "PII Message",
			Location: &inbound.InboundLocation{
				Latitude:  12.34,
				Longitude: 56.78,
				Name:      "Secret Base",
			},
			Contacts: []inbound.InboundContact{
				{Name: "John Doe", Phone: "+1234"},
			},
		}

		err = proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		var payload inbound.InboundEventPayload
		json.Unmarshal(pub.published[0].data, &payload)

		// Verification: Location and Contacts should be filtered (nil/empty)
		if payload.Location != nil {
			t.Errorf("expected Location to be filtered (nil), got %+v", payload.Location)
		}
		if len(payload.Contacts) != 0 {
			t.Errorf("expected Contacts to be filtered (empty), got %+v", payload.Contacts)
		}

		// Update workspace PIIOptIn to true
		_, err = pool.Exec(ctx, "UPDATE workspaces SET pii_opt_in = TRUE WHERE id = $1", ws.ID)
		if err != nil {
			t.Fatalf("failed to update workspace PII: %v", err)
		}

		pub2 := &fakePublisher{}
		proc2 := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub2, aud, sessRepo, contactRepo, dispatchRepo, nil)
		event.MessageID = "test-pii-opt-in" // fresh message ID to bypass dedup

		err = proc2.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		var payload2 inbound.InboundEventPayload
		json.Unmarshal(pub2.published[0].data, &payload2)

		// Verification: Location and Contacts should be retained
		if payload2.Location == nil {
			t.Fatal("expected Location to be retained")
		}
		if payload2.Location.Latitude != 12.34 {
			t.Errorf("got latitude %f, want 12.34", payload2.Location.Latitude)
		}
		if len(payload2.Contacts) != 1 {
			t.Fatalf("expected 1 contact, got %d", len(payload2.Contacts))
		}
	})

	t.Run("Non-fatal S3 upload failure does not stop message ingestion", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{
			processInboundFn: func(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error) {
				return "", errors.New("S3 server timeout")
			},
		}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-s3-fail",
			Channel:     "whatsapp",
			From:        "sender-s3",
			To:          "receiver-s3",
			Body:        "Text still goes through",
			Media: &inbound.InboundMedia{
				Bytes:     []byte("some-bytes"),
				MediaType: "document",
			},
		}

		err = proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process should not fail: %v", err)
		}

		// Verify message was still published to NATS
		if len(pub.published) != 1 {
			t.Fatalf("expected 1 published event, got %d", len(pub.published))
		}
		var payload inbound.InboundEventPayload
		json.Unmarshal(pub.published[0].data, &payload)

		if payload.Body != "Text still goes through" {
			t.Errorf("got body %q, want %q", payload.Body, "Text still goes through")
		}
		// Media should be nil since S3 upload failed
		if payload.Media != nil {
			t.Errorf("expected media to be nil on S3 fail, got %+v", payload.Media)
		}
	})
}

func TestProcess_StatusUpdate(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "status_update_test_ws_" + uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	traceID := uuid.New().String()
	d, err := dispatchRepo.GetOrCreateDispatch(ctx, ws.ID, traceID, "whatsapp_cloud", nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create dispatch: %v", err)
	}

	providerID := "wamid.test_status_update_999"
	err = dispatchRepo.UpdateProviderMessageID(ctx, d.ID, providerID)
	if err != nil {
		t.Fatalf("failed to update provider message id: %v", err)
	}

	// Initial dispatch status should be "queued"
	if d.Status != "queued" {
		t.Errorf("expected initial status queued, got %s", d.Status)
	}

	// Count initial contacts
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM contacts WHERE workspace_id = $1", ws.ID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count contacts: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 contacts initially, got %d", count)
	}

	pub := &fakePublisher{}
	me := &fakeMediaEngine{}
	aud := &fakeAuditWriter{}

	proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

	event := &inbound.InboundEvent{
		WorkspaceID: ws.ID,
		MessageID:   providerID,
		Channel:     "whatsapp_cloud",
		From:        "5511999990000",
		To:          "5511888880000",
		Body:        "delivered",
		Metadata:    map[string]string{"type": "status_update"},
	}

	err = proc.Process(ctx, event)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// 1. Verify dispatch record status is updated to "delivered"
	updated, err := dispatchRepo.GetByProviderMessageID(ctx, providerID)
	if err != nil {
		t.Fatalf("failed to retrieve updated dispatch: %v", err)
	}
	if updated.Status != "delivered" {
		t.Errorf("expected status 'delivered', got %q", updated.Status)
	}

	// 2. Verify contact resolution was not executed (0 contacts in DB)
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM contacts WHERE workspace_id = $1", ws.ID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count contacts: %v", err)
	}
	if count != 0 {
		t.Errorf("expected contact resolution to be bypassed, but contact was created (count = %d)", count)
	}

	// 3. Verify NATS publish on messages.status_updated
	if len(pub.published) != 1 {
		t.Fatalf("expected 1 NATS publish, got %d", len(pub.published))
	}
	pubEvent := pub.published[0]
	if pubEvent.subject != "messages.status_updated" {
		t.Errorf("expected subject 'messages.status_updated', got %q", pubEvent.subject)
	}

	var payload inbound.MessageStatusUpdatedPayload
	if err := json.Unmarshal(pubEvent.data, &payload); err != nil {
		t.Fatalf("failed to unmarshal NATS payload: %v", err)
	}
	if payload.WorkspaceID != ws.ID.String() {
		t.Errorf("expected workspace ID %s, got %s", ws.ID.String(), payload.WorkspaceID)
	}
	if payload.DispatchID != d.ID.String() {
		t.Errorf("expected dispatch ID %s, got %s", d.ID.String(), payload.DispatchID)
	}
	if payload.MessageID != providerID {
		t.Errorf("expected message ID %s, got %s", providerID, payload.MessageID)
	}
	if payload.Status != "delivered" {
		t.Errorf("expected status 'delivered', got %s", payload.Status)
	}
}

type fakeChatwootSyncer struct {
	called  chan struct{}
	contact *domain.Contact
	event   *inbound.InboundEvent
}

func (f *fakeChatwootSyncer) SyncInboundMessage(ctx context.Context, contact *domain.Contact, ev *inbound.InboundEvent) error {
	f.contact = contact
	f.event = ev
	close(f.called)
	return nil
}

func TestInboundProcessor_ChatwootSyncer(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "chatwoot_syncer_test_ws_" + uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	pub := &fakePublisher{}
	me := &fakeMediaEngine{}
	aud := &fakeAuditWriter{}

	syncer := &fakeChatwootSyncer{
		called: make(chan struct{}),
	}
	router := inbound.NewDefaultRouter(syncer, nil)
	proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, router)

	event := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: uuid.New(),
		MessageID:    "msg_unique_12345",
		Channel:      "telegram",
		From:         "5511999990000",
		To:           "@test_bot",
		Body:         "Hello to Chatwoot",
	}

	err = proc.Process(ctx, event)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	select {
	case <-syncer.called:
		if syncer.contact == nil {
			t.Error("expected contact to be populated")
		}
		if syncer.event.Body != "Hello to Chatwoot" {
			t.Errorf("expected body 'Hello to Chatwoot', got %q", syncer.event.Body)
		}
		if syncer.event.ConnectionID != event.ConnectionID {
			t.Errorf("expected connection ID %s, got %s", event.ConnectionID, syncer.event.ConnectionID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for ChatwootSyncer to be called")
	}
}

func TestInboundProcessor_BotCooldown(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "cooldown_test_ws_" + uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	pub := &fakePublisher{}
	me := &fakeMediaEngine{}
	aud := &fakeAuditWriter{}
	proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

	t.Run("Reset bot when paused for > 12 hours", func(t *testing.T) {
		// 1. Resolve contact
		contact, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "cooldown-gt-12", "Cooldown Greater", "", "")
		if err != nil {
			t.Fatalf("failed to resolve contact: %v", err)
		}

		// 2. Set bot paused state to 13 hours ago
		pausedAt := time.Now().UTC().Add(-13 * time.Hour)
		err = contactRepo.UpdateBotState(ctx, ws.ID, contact.ID, false, &pausedAt)
		if err != nil {
			t.Fatalf("failed to update bot state: %v", err)
		}

		// 3. Process an inbound event
		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "msg-cooldown-gt-12",
			Channel:     "telegram",
			From:        "cooldown-gt-12",
			To:          "@test_bot",
			Body:        "Hello, is bot active?",
		}

		err = proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		// 4. Verify contact in DB has bot_active = true and bot_paused_at = nil
		updated, err := contactRepo.GetByID(ctx, ws.ID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get contact: %v", err)
		}

		if !updated.BotActive {
			t.Error("expected bot_active to be reset to true")
		}
		if updated.BotPausedAt != nil {
			t.Errorf("expected bot_paused_at to be nil, got %v", updated.BotPausedAt)
		}
	})

	t.Run("Do NOT reset bot when paused for < 12 hours", func(t *testing.T) {
		// 1. Resolve contact
		contact, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "cooldown-lt-12", "Cooldown Less", "", "")
		if err != nil {
			t.Fatalf("failed to resolve contact: %v", err)
		}

		// 2. Set bot paused state to 1 hour ago
		pausedAt := time.Now().UTC().Add(-1 * time.Hour)
		err = contactRepo.UpdateBotState(ctx, ws.ID, contact.ID, false, &pausedAt)
		if err != nil {
			t.Fatalf("failed to update bot state: %v", err)
		}

		// 3. Process an inbound event
		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "msg-cooldown-lt-12",
			Channel:     "telegram",
			From:        "cooldown-lt-12",
			To:          "@test_bot",
			Body:        "Hello, is bot active?",
		}

		err = proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		// 4. Verify contact in DB still has bot_active = false and bot_paused_at set
		updated, err := contactRepo.GetByID(ctx, ws.ID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get contact: %v", err)
		}

		if updated.BotActive {
			t.Error("expected bot_active to remain false")
		}
		if updated.BotPausedAt == nil {
			t.Error("expected bot_paused_at to remain set")
		}
	})
}

func TestInboundProcessor_WhatsAppCloudSessionTracking(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "waba_session_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	pub := &fakePublisher{}
	me := &fakeMediaEngine{}
	aud := &fakeAuditWriter{}
	proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

	t.Run("records standard 24h recipient session", func(t *testing.T) {
		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "wamid.standard_sess_1",
			Channel:     "whatsapp_cloud",
			From:        "5511999990001",
			To:          "+5511888880001",
			Body:        "Hi there",
			Metadata: map[string]string{
				"entry_point_type": "standard",
			},
		}

		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		sess, err := sessRepo.Get(ctx, ws.ID, "5511999990001", "whatsapp_cloud", "+5511888880001")
		if err != nil {
			t.Fatalf("failed to retrieve recipient session: %v", err)
		}
		if sess.RecipientPhone != "5511999990001" {
			t.Errorf("expected RecipientPhone 5511999990001, got %s", sess.RecipientPhone)
		}
		if sess.RecipientIdentity != "+5511888880001" {
			t.Errorf("expected RecipientIdentity +5511888880001, got %s", sess.RecipientIdentity)
		}
		if sess.EntryPointType != "standard" {
			t.Errorf("expected EntryPointType standard, got %s", sess.EntryPointType)
		}
		if sess.LastInboundAt.IsZero() || time.Since(sess.LastInboundAt) > 10*time.Second {
			t.Errorf("expected LastInboundAt to be recent, got %v", sess.LastInboundAt)
		}
	})

	t.Run("records ctwa 72h recipient session", func(t *testing.T) {
		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "wamid.ctwa_sess_1",
			Channel:     "whatsapp_cloud",
			From:        "5511999990002",
			To:          "+5511888880001",
			Body:        "Clicked ad",
			Metadata: map[string]string{
				"entry_point_type": "ctwa",
			},
		}

		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		sess, err := sessRepo.Get(ctx, ws.ID, "5511999990002", "whatsapp_cloud", "+5511888880001")
		if err != nil {
			t.Fatalf("failed to retrieve recipient session: %v", err)
		}
		if sess.EntryPointType != "ctwa" {
			t.Errorf("expected EntryPointType ctwa, got %s", sess.EntryPointType)
		}
	})
}

func TestInboundProcessor_GroupMessageProcessing(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "group_msg_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	pub := &fakePublisher{}
	me := &fakeMediaEngine{
		processInboundFn: func(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error) {
			return "/media/" + workspaceID.String() + "/group_img.jpg", nil
		},
	}
	aud := &fakeAuditWriter{}

	proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

	groupJID := "120363024823904@g.us"
	participantJID := "5511999991234@s.whatsapp.net"
	pushName := "Alice GroupMember"

	event := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: uuid.New(),
		MessageID:    "wamid.group_msg_1",
		Channel:      "whatsapp",
		From:         groupJID,
		To:           "+5511888880001",
		Body:         "Hello from the group!",
		SenderName:   pushName,
		Media: &inbound.InboundMedia{
			Bytes:     []byte("group-image-data"),
			MediaType: "image",
			Filename:  "group_pic.jpg",
			Caption:   "Our group photo",
		},
		Metadata: map[string]string{
			"is_group":         "true",
			"participant":      participantJID,
			"chat_jid":         groupJID,
			"sender_push_name": pushName,
		},
	}

	err = proc.Process(ctx, event)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(pub.published))
	}

	pubEvent := pub.published[0]
	if pubEvent.subject != "inbound.events."+ws.ID.String() {
		t.Errorf("got subject %s, want inbound.events.%s", pubEvent.subject, ws.ID.String())
	}

	var payload inbound.InboundEventPayload
	if err := json.Unmarshal(pubEvent.data, &payload); err != nil {
		t.Fatalf("failed to unmarshal published payload: %v", err)
	}

	if payload.From != groupJID {
		t.Errorf("expected payload From %q, got %q", groupJID, payload.From)
	}
	if payload.SenderName != pushName {
		t.Errorf("expected payload SenderName %q, got %q", pushName, payload.SenderName)
	}
	if payload.Metadata == nil {
		t.Fatal("expected payload Metadata to be populated")
	}
	if payload.Metadata["is_group"] != "true" {
		t.Errorf("expected Metadata[is_group] == 'true', got %q", payload.Metadata["is_group"])
	}
	if payload.Metadata["participant"] != participantJID {
		t.Errorf("expected Metadata[participant] == %q, got %q", participantJID, payload.Metadata["participant"])
	}
	if payload.Metadata["chat_jid"] != groupJID {
		t.Errorf("expected Metadata[chat_jid] == %q, got %q", groupJID, payload.Metadata["chat_jid"])
	}
	if payload.Metadata["sender_push_name"] != pushName {
		t.Errorf("expected Metadata[sender_push_name] == %q, got %q", pushName, payload.Metadata["sender_push_name"])
	}
	if payload.Media == nil {
		t.Fatal("expected payload Media to be populated")
	}
	if payload.Media.MediaURL != "/media/"+ws.ID.String()+"/group_img.jpg" {
		t.Errorf("expected MediaURL %s, got %s", "/media/"+ws.ID.String()+"/group_img.jpg", payload.Media.MediaURL)
	}
	if payload.Media.Caption != "Our group photo" {
		t.Errorf("expected Media Caption 'Our group photo', got %q", payload.Media.Caption)
	}

	// Verify Contact Profile resolution
	var contactID uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT contact_id FROM contact_identities 
		WHERE workspace_id = $1 AND channel = 'whatsapp' AND sender_identity = $2
	`, ws.ID, groupJID).Scan(&contactID)
	if err != nil {
		t.Fatalf("expected contact identity for group JID %s: %v", groupJID, err)
	}

	// Verify no invalid 'phone' identity was created for the group JID
	var phoneIdentityCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM contact_identities 
		WHERE workspace_id = $1 AND channel = 'phone' AND sender_identity = $2
	`, ws.ID, groupJID).Scan(&phoneIdentityCount)
	if err != nil {
		t.Fatalf("failed to query phone identities: %v", err)
	}
	if phoneIdentityCount != 0 {
		t.Errorf("expected 0 'phone' identities for group JID, got %d", phoneIdentityCount)
	}
}

func TestInboundProcessor_GroupContactResolution_DistinctGroupsAndIdempotent(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "group_contact_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	pub := &fakePublisher{}
	proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, nil, pub, nil, sessRepo, contactRepo, dispatchRepo, nil)

	group1JID := "120363024823901@g.us"
	group2JID := "120363024823902@g.us"

	// 1. First event from Group 1
	ev1 := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: uuid.New(),
		MessageID:    "wamid.group1_msg_1",
		Channel:      "whatsapp",
		From:         group1JID,
		To:           "+5511888880001",
		Body:         "Hello Group 1",
		SenderName:   "Group One Name",
		Metadata: map[string]string{
			"is_group":    "true",
			"participant": "5511999990001@s.whatsapp.net",
		},
	}
	if err := proc.Process(ctx, ev1); err != nil {
		t.Fatalf("Process ev1 failed: %v", err)
	}

	var contact1ID uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT contact_id FROM contact_identities 
		WHERE workspace_id = $1 AND channel = 'whatsapp' AND sender_identity = $2
	`, ws.ID, group1JID).Scan(&contact1ID)
	if err != nil {
		t.Fatalf("failed to get contact for group 1: %v", err)
	}

	// 2. Second event from same Group 1 (idempotent resolution)
	ev1Second := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: uuid.New(),
		MessageID:    "wamid.group1_msg_2",
		Channel:      "whatsapp",
		From:         group1JID,
		To:           "+5511888880001",
		Body:         "Second message Group 1",
		SenderName:   "Alice PushName",
		Metadata: map[string]string{
			"is_group":    "true",
			"participant": "5511999990002@s.whatsapp.net",
		},
	}
	if err := proc.Process(ctx, ev1Second); err != nil {
		t.Fatalf("Process ev1Second failed: %v", err)
	}

	var contact1SecondID uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT contact_id FROM contact_identities 
		WHERE workspace_id = $1 AND channel = 'whatsapp' AND sender_identity = $2
	`, ws.ID, group1JID).Scan(&contact1SecondID)
	if err != nil {
		t.Fatalf("failed to get contact for group 1 second time: %v", err)
	}
	if contact1ID != contact1SecondID {
		t.Errorf("expected idempotent contact ID %s, got %s", contact1ID, contact1SecondID)
	}

	// 3. Event from distinct Group 2
	ev2 := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: uuid.New(),
		MessageID:    "wamid.group2_msg_1",
		Channel:      "whatsapp",
		From:         group2JID,
		To:           "+5511888880001",
		Body:         "Hello Group 2",
		SenderName:   "Group Two Name",
		Metadata: map[string]string{
			"is_group":    "true",
			"participant": "5511999990003@s.whatsapp.net",
		},
	}
	if err := proc.Process(ctx, ev2); err != nil {
		t.Fatalf("Process ev2 failed: %v", err)
	}

	var contact2ID uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT contact_id FROM contact_identities 
		WHERE workspace_id = $1 AND channel = 'whatsapp' AND sender_identity = $2
	`, ws.ID, group2JID).Scan(&contact2ID)
	if err != nil {
		t.Fatalf("failed to get contact for group 2: %v", err)
	}
	if contact1ID == contact2ID {
		t.Errorf("expected distinct contact IDs for distinct groups, but got same ID %s", contact1ID)
	}
}

type spyTypebotForwarder struct {
	invoked chan struct{}
	called  bool
	contact *domain.Contact
	event   *inbound.InboundEvent
}

func (s *spyTypebotForwarder) SyncInboundMessage(ctx context.Context, contact *domain.Contact, ev *inbound.InboundEvent) error {
	s.called = true
	s.contact = contact
	s.event = ev
	if ev.Metadata != nil && ev.Metadata["is_group"] == "true" {
		return nil
	}
	close(s.invoked)
	return nil
}

func TestInboundProcessor_GroupMessage_BypassesTypebot_RoutesToChatwoot(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "router_governance_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	pub := &fakePublisher{}
	cwSyncer := &fakeChatwootSyncer{called: make(chan struct{})}
	tbForwarder := &spyTypebotForwarder{invoked: make(chan struct{})}

	router := inbound.NewDefaultRouter(cwSyncer, tbForwarder)
	proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, nil, pub, nil, sessRepo, contactRepo, dispatchRepo, router)

	groupJID := "120363024823904@g.us"
	connID := uuid.New()

	// 1. Group Inbound Event
	groupEv := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "wamid.group_governance_1",
		Channel:      "whatsapp",
		From:         groupJID,
		To:           "+5511888880001",
		Body:         "Group discussion item",
		SenderName:   "Alice Participant",
		Metadata: map[string]string{
			"is_group":         "true",
			"participant":      "5511999991234@s.whatsapp.net",
			"chat_jid":         groupJID,
			"sender_push_name": "Alice Participant",
		},
	}

	err = proc.Process(ctx, groupEv)
	if err != nil {
		t.Fatalf("Process group event failed: %v", err)
	}

	// Verify Chatwoot received the group event
	select {
	case <-cwSyncer.called:
		if cwSyncer.event.From != groupJID {
			t.Errorf("expected Chatwoot event From %q, got %q", groupJID, cwSyncer.event.From)
		}
		if cwSyncer.contact == nil {
			t.Fatal("expected Chatwoot contact to be populated")
		}
		var groupIdentityFound bool
		for _, ident := range cwSyncer.contact.Identities {
			if ident.SenderIdentity == groupJID {
				groupIdentityFound = true
			}
		}
		if !groupIdentityFound {
			t.Errorf("expected group identity %s in resolved contact identities", groupJID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for ChatwootSyncer on group message")
	}

	// Verify Typebot was not invoked for bot actions on group message
	select {
	case <-tbForwarder.invoked:
		t.Fatal("TypebotForwarder should not execute bot flow for group messages")
	case <-time.After(100 * time.Millisecond):
		// Expected: bot flow was skipped
	}

	// 2. Direct 1-on-1 Inbound Event
	cwSyncer2 := &fakeChatwootSyncer{called: make(chan struct{})}
	tbForwarder2 := &spyTypebotForwarder{invoked: make(chan struct{})}
	router2 := inbound.NewDefaultRouter(cwSyncer2, tbForwarder2)
	proc2 := inbound.NewInboundProcessor(dedupRepo, wsRepo, nil, pub, nil, sessRepo, contactRepo, dispatchRepo, router2)

	directEv := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "wamid.direct_governance_1",
		Channel:      "whatsapp",
		From:         "5511999995555",
		To:           "+5511888880001",
		Body:         "Hello 1-on-1 bot",
		SenderName:   "Bob Direct",
	}

	err = proc2.Process(ctx, directEv)
	if err != nil {
		t.Fatalf("Process direct event failed: %v", err)
	}

	// Both Chatwoot and Typebot should be invoked for direct messages
	select {
	case <-cwSyncer2.called:
		if cwSyncer2.event.From != "5511999995555" {
			t.Errorf("expected Chatwoot direct event From 5511999995555, got %q", cwSyncer2.event.From)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for ChatwootSyncer on direct message")
	}

	select {
	case <-tbForwarder2.invoked:
		if tbForwarder2.event.From != "5511999995555" {
			t.Errorf("expected Typebot direct event From 5511999995555, got %q", tbForwarder2.event.From)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for TypebotForwarder on direct message")
	}
}

func TestInboundProcessor_InteractivePayloads_DomainEvents(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "interactive_events_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	pub := &fakePublisher{}
	me := &fakeMediaEngine{}
	aud := &fakeAuditWriter{}
	proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo, nil)

	t.Run("flow completion publishes flow.completed event and inbound_message", func(t *testing.T) {
		pub.published = nil

		event := &inbound.InboundEvent{
			WorkspaceID:  ws.ID,
			ConnectionID: uuid.New(),
			MessageID:    "wamid.flow_comp_001",
			Channel:      "whatsapp_cloud",
			From:         "5511988887777",
			To:           "+5511888880001",
			Body:         "📄 *Form Submitted*\nScreen: SURVEY_1\n- rating: 5",
			Interactive: &inbound.InboundInteractive{
				Type: "nfm_reply",
				NFMReply: &inbound.InboundNFMReply{
					Name:         "customer_survey",
					Body:         "Sent",
					ResponseJSON: `{"flow_token":"tok_123","screen":"SURVEY_1","data":{"rating":5}}`,
					FlowToken:    "tok_123",
					Screen:       "SURVEY_1",
					Data: map[string]interface{}{
						"rating": 5,
					},
				},
			},
		}

		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		// Should have published both inbound_message and flow.completed
		var hasInboundMsg, hasFlowCompleted bool
		for _, p := range pub.published {
			var raw map[string]interface{}
			_ = json.Unmarshal(p.data, &raw)
			if raw["event"] == "inbound_message" {
				hasInboundMsg = true
			}
			if raw["event"] == string(domain.EventTypeFlowCompleted) {
				hasFlowCompleted = true
				if raw["screen"] != "SURVEY_1" {
					t.Errorf("expected screen SURVEY_1, got %v", raw["screen"])
				}
				if raw["flow_token"] != "tok_123" {
					t.Errorf("expected flow_token tok_123, got %v", raw["flow_token"])
				}
				if raw["contact_id"] != "5511988887777" {
					t.Errorf("expected contact_id 5511988887777, got %v", raw["contact_id"])
				}
			}
		}

		if !hasInboundMsg {
			t.Errorf("expected inbound_message event to be published")
		}
		if !hasFlowCompleted {
			t.Errorf("expected flow.completed event to be published")
		}
	})

	t.Run("order message publishes order.created event and inbound_message", func(t *testing.T) {
		pub.published = nil

		event := &inbound.InboundEvent{
			WorkspaceID:  ws.ID,
			ConnectionID: uuid.New(),
			MessageID:    "wamid.order_proc_001",
			Channel:      "whatsapp_cloud",
			From:         "5511988887777",
			To:           "+5511888880001",
			Body:         "🛒 Order Received (Catalog: cat_123)\nTotal: 100.00 USD",
			Interactive: &inbound.InboundInteractive{
				Type: "order",
				Order: &inbound.InboundOrder{
					CatalogID: "cat_123",
					Text:      "fast delivery",
					ProductItems: []domain.OrderProductItem{
						{
							ProductRetailerID: "ITEM-A",
							Quantity:          2,
							ItemPrice:         50.00,
							Currency:          "USD",
						},
					},
					TotalPrice: 100.00,
					Currency:   "USD",
				},
			},
		}

		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		var hasInboundMsg, hasOrderCreated bool
		for _, p := range pub.published {
			var raw map[string]interface{}
			_ = json.Unmarshal(p.data, &raw)
			if raw["event"] == "inbound_message" {
				hasInboundMsg = true
			}
			if raw["event"] == string(domain.EventTypeOrderCreated) {
				hasOrderCreated = true
				if raw["catalog_id"] != "cat_123" {
					t.Errorf("expected catalog_id cat_123, got %v", raw["catalog_id"])
				}
				if raw["total_price"] != 100.0 {
					t.Errorf("expected total_price 100, got %v", raw["total_price"])
				}
				if raw["currency"] != "USD" {
					t.Errorf("expected currency USD, got %v", raw["currency"])
				}
			}
		}

		if !hasInboundMsg {
			t.Errorf("expected inbound_message event to be published")
		}
		if !hasOrderCreated {
			t.Errorf("expected order.created event to be published")
		}
	})
}



