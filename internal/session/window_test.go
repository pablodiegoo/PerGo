package session

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

type mockSessionReader struct {
	getFn func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*repository.RecipientSession, error)
}

func (m *mockSessionReader) Get(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*repository.RecipientSession, error) {
	return m.getFn(ctx, workspaceID, recipientPhone, channel, recipientIdentity)
}

func TestWindowChecker_IsWindowOpen(t *testing.T) {
	wsID := uuid.New()
	phone := "+1234567890"
	channelName := "whatsapp_cloud"
	recIdentity := "5511999999999"

	tests := []struct {
		name         string
		safetyBuffer time.Duration
		mockGet      func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*repository.RecipientSession, error)
		wantOpen     bool
		wantDuration time.Duration
		wantType     string
		wantErr      bool
	}{
		{
			name:         "Session not found (expired/missing)",
			safetyBuffer: 0,
			mockGet: func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*repository.RecipientSession, error) {
				return nil, repository.ErrSessionNotFound
			},
			wantOpen: false,
			wantErr:  false,
		},
		{
			name:         "DB error",
			safetyBuffer: 0,
			mockGet: func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*repository.RecipientSession, error) {
				return nil, repository.ErrCredentialsNotFound
			},
			wantOpen: false,
			wantErr:  true,
		},
		{
			name:         "Standard window open (last inbound 1 hour ago)",
			safetyBuffer: 0,
			mockGet: func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*repository.RecipientSession, error) {
				return &repository.RecipientSession{
					WorkspaceID:    workspaceID,
					RecipientPhone: recipientPhone,
					Channel:        channel,
					LastInboundAt:  time.Now().Add(-1 * time.Hour),
					EntryPointType: "standard",
				}, nil
			},
			wantOpen:     true,
			wantDuration: 24 * time.Hour,
			wantType:     "standard",
			wantErr:      false,
		},
		{
			name:         "Standard window closed (last inbound 25 hours ago)",
			safetyBuffer: 0,
			mockGet: func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*repository.RecipientSession, error) {
				return &repository.RecipientSession{
					WorkspaceID:    workspaceID,
					RecipientPhone: recipientPhone,
					Channel:        channel,
					LastInboundAt:  time.Now().Add(-25 * time.Hour),
					EntryPointType: "standard",
				}, nil
			},
			wantOpen:     false,
			wantDuration: 24 * time.Hour,
			wantType:     "standard",
			wantErr:      false,
		},
		{
			name:         "Safety buffer early closure (23h58m since inbound, 5m buffer)",
			safetyBuffer: 5 * time.Minute,
			mockGet: func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*repository.RecipientSession, error) {
				return &repository.RecipientSession{
					WorkspaceID:    workspaceID,
					RecipientPhone: recipientPhone,
					Channel:        channel,
					LastInboundAt:  time.Now().Add(-23*time.Hour - 58*time.Minute),
					EntryPointType: "standard",
				}, nil
			},
			wantOpen:     false,
			wantDuration: 24 * time.Hour,
			wantType:     "standard",
			wantErr:      false,
		},
		{
			name:         "CTWA 72h window open (last inbound 48 hours ago)",
			safetyBuffer: 0,
			mockGet: func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*repository.RecipientSession, error) {
				return &repository.RecipientSession{
					WorkspaceID:    workspaceID,
					RecipientPhone: recipientPhone,
					Channel:        channel,
					LastInboundAt:  time.Now().Add(-48 * time.Hour),
					EntryPointType: "ctwa",
				}, nil
			},
			wantOpen:     true,
			wantDuration: 72 * time.Hour,
			wantType:     "ctwa",
			wantErr:      false,
		},
		{
			name:         "CTWA 72h window closed (last inbound 73 hours ago)",
			safetyBuffer: 0,
			mockGet: func(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*repository.RecipientSession, error) {
				return &repository.RecipientSession{
					WorkspaceID:    workspaceID,
					RecipientPhone: recipientPhone,
					Channel:        channel,
					LastInboundAt:  time.Now().Add(-73 * time.Hour),
					EntryPointType: "ctwa",
				}, nil
			},
			wantOpen:     false,
			wantDuration: 72 * time.Hour,
			wantType:     "ctwa",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockReader := &mockSessionReader{getFn: tt.mockGet}
			checker := NewWindowChecker(mockReader)

			status, err := checker.IsWindowOpen(context.Background(), wsID, phone, channelName, recIdentity, tt.safetyBuffer)
			if (err != nil) != tt.wantErr {
				t.Fatalf("IsWindowOpen() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if status.Open != tt.wantOpen {
				t.Errorf("IsWindowOpen() open = %v, wantOpen %v", status.Open, tt.wantOpen)
			}
			if tt.wantDuration != 0 && status.WindowDuration != tt.wantDuration {
				t.Errorf("IsWindowOpen() WindowDuration = %v, wantDuration %v", status.WindowDuration, tt.wantDuration)
			}
			if tt.wantType != "" && status.EntryPointType != tt.wantType {
				t.Errorf("IsWindowOpen() EntryPointType = %s, wantType %s", status.EntryPointType, tt.wantType)
			}
		})
	}
}
