package session

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.omnigo/internal/repository"
)

type mockSessionReader struct {
	getFn func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string) (*repository.RecipientSession, error)
}

func (m *mockSessionReader) Get(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string) (*repository.RecipientSession, error) {
	return m.getFn(ctx, workspaceID, recipientPhone, channel)
}

func TestWindowChecker_IsWindowOpen(t *testing.T) {
	wsID := uuid.New()
	phone := "+1234567890"
	channelName := "whatsapp_cloud"

	tests := []struct {
		name     string
		mockGet  func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string) (*repository.RecipientSession, error)
		wantOpen bool
		wantErr  bool
	}{
		{
			name: "Session not found (expired/missing)",
			mockGet: func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string) (*repository.RecipientSession, error) {
				return nil, repository.ErrSessionNotFound
			},
			wantOpen: false,
			wantErr:  false,
		},
		{
			name: "DB error",
			mockGet: func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string) (*repository.RecipientSession, error) {
				return nil, repository.ErrCredentialsNotFound // some other error
			},
			wantOpen: false,
			wantErr:  true,
		},
		{
			name: "Window open (last inbound 1 hour ago)",
			mockGet: func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string) (*repository.RecipientSession, error) {
				return &repository.RecipientSession{
					WorkspaceID:    workspaceID,
					RecipientPhone: recipientPhone,
					Channel:        channel,
					LastInboundAt:  time.Now().Add(-1 * time.Hour),
				}, nil
			},
			wantOpen: true,
			wantErr:  false,
		},
		{
			name: "Window closed (last inbound 25 hours ago)",
			mockGet: func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string) (*repository.RecipientSession, error) {
				return &repository.RecipientSession{
					WorkspaceID:    workspaceID,
					RecipientPhone: recipientPhone,
					Channel:        channel,
					LastInboundAt:  time.Now().Add(-25 * time.Hour),
				}, nil
			},
			wantOpen: false,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockReader := &mockSessionReader{getFn: tt.mockGet}
			checker := NewWindowChecker(mockReader)

			open, err := checker.IsWindowOpen(context.Background(), wsID, phone, channelName)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsWindowOpen() error = %v, wantErr %v", err, tt.wantErr)
			}
			if open != tt.wantOpen {
				t.Errorf("IsWindowOpen() open = %v, wantOpen %v", open, tt.wantOpen)
			}
		})
	}
}
