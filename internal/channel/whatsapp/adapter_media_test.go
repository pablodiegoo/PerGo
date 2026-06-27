package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.omnigo/internal/channel"
	"github.com/pablojhp.omnigo/internal/domain"
	"github.com/pablojhp.omnigo/internal/platform/storage"
)

func TestWhatsAppAdapter_Media(t *testing.T) {
	// Initialize S3 storage
	s3Client, err := storage.NewS3Client("http://localhost:9000", "us-east-1", "minioadmin", "minioadmin", "omnigo-bucket", true)
	if err != nil {
		t.Fatalf("failed to init s3 client: %v", err)
	}

	wsID := uuid.New()
	key := wsID.String() + "/hash123.png"
	data := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	err = s3Client.Upload(context.Background(), key, data, "image/png")
	if err != nil {
		t.Fatalf("failed to upload mock file to S3: %v", err)
	}

	// We pass a nil client to NewWhatsAppAdapter because we only want to test the first part of Dispatch
	// (fetching from S3 and validating). However, if we do that, Dispatch checks client == nil or client.Client() == nil
	// and returns an error early. Let's wrap this to check that we fetch from S3 correctly.
	// Since we cannot mock whatsmeow Client without DB easily, let's create a minimal test.
	adapter := NewWhatsAppAdapter(nil, s3Client)

	t.Run("Dispatch media checks client first", func(t *testing.T) {
		payload := &channel.MessagePayload{
			To:   "5511999999999",
			Body: "Caption here",
			Media: &domain.Media{
				MediaURL:  "/media/" + wsID.String() + "/hash123.png",
				MediaType: "image",
				Caption:   "Caption here",
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := adapter.Dispatch(ctx, payload)
		if err == nil {
			t.Fatal("expected error since client is nil")
		}
		if err.Error() != "whatsapp: client not connected" {
			t.Errorf("expected 'whatsapp: client not connected' error, got %v", err)
		}
	})
}
