package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
)

type mockCredentialProvider struct{}

func (m *mockCredentialProvider) Encrypt(plaintext []byte) (ciphertext []byte, keyID string, keyVersion int, err error) {
	return plaintext, "key1", 1, nil
}

func (m *mockCredentialProvider) Decrypt(ciphertext []byte) (plaintext []byte, err error) {
	return ciphertext, nil
}

func TestRepository_StrictTenantIsolation_RejectsNilUUID(t *testing.T) {
	ctx := context.Background()
	nilID := uuid.Nil

	t.Run("WorkspaceRepository", func(t *testing.T) {
		repo := repository.NewWorkspaceRepository(nil)

		if _, err := repo.CreateWithID(ctx, nilID, "Test"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("CreateWithID: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GetByID(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetByID: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.SetWebhookSecret(ctx, nilID, "secret"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("SetWebhookSecret: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GenerateWebhookSecret(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GenerateWebhookSecret: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.Delete(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Delete: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("ConnectionRepository", func(t *testing.T) {
		repo := repository.NewConnectionRepository(nil, &mockCredentialProvider{})

		connNil := &repository.Connection{WorkspaceID: nilID, Name: "Test"}
		if err := repo.Create(ctx, connNil); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Create: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GetBySlug(ctx, nilID, "test-slug"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetBySlug: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GetBySenderIdentity(ctx, nilID, "123456"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetBySenderIdentity: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GetDefaultChannelConnection(ctx, nilID, "whatsapp"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetDefaultChannelConnection: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.ListByWorkspace(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ListByWorkspace: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.CountActiveByWorkspace(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("CountActiveByWorkspace: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("CampaignRepository", func(t *testing.T) {
		repo := repository.NewCampaignRepository(nil)

		camp := &domain.Campaign{WorkspaceID: nilID, Name: "Test"}
		if _, err := repo.Create(ctx, camp); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Create: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.ListByWorkspace(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ListByWorkspace: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("TagRepository", func(t *testing.T) {
		repo := repository.NewTagRepository(nil)

		if _, err := repo.CreateTag(ctx, nilID, "Tag", "#fff"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("CreateTag: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GetTagByID(ctx, nilID, uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetTagByID: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.ListTags(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ListTags: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.DeleteTag(ctx, nilID, uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("DeleteTag: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.AddTagToContact(ctx, nilID, uuid.New(), uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("AddTagToContact: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.RemoveTagFromContact(ctx, nilID, uuid.New(), uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("RemoveTagFromContact: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GetContactTags(ctx, nilID, uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetContactTags: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.ListContactsByTag(ctx, nilID, uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ListContactsByTag: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("ContactRepository", func(t *testing.T) {
		repo := repository.NewContactRepository(nil)

		if _, err := repo.GetByID(ctx, nilID, uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetByID: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.ResolveContact(ctx, nilID, "whatsapp", "123", "User", "", "123"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ResolveContact: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.CreateContact(ctx, nilID, "User", nil, nil); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("CreateContact: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.UpdateAttributes(ctx, nilID, uuid.New(), map[string]string{"a": "b"}); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("UpdateAttributes: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.UpdateContact(ctx, nilID, &domain.Contact{ID: uuid.New(), Name: "User"}); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("UpdateContact: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.DeleteContact(ctx, nilID, uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("DeleteContact: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.MergeContacts(ctx, nilID, uuid.New(), uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("MergeContacts: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.SearchContacts(ctx, nilID, "q", uuid.New(), 10); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("SearchContacts: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.ResolveTelegramChatID(ctx, nilID, "@user"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ResolveTelegramChatID: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.HasUnread(ctx, nilID, uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("HasUnread: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.AddTags(ctx, nilID, uuid.New(), []string{"tag"}); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("AddTags: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.CloseThread(ctx, nilID, uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("CloseThread: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.UpdateBotState(ctx, nilID, uuid.New(), true, nil); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("UpdateBotState: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, _, err := repo.FindIdentityForChannel(ctx, nilID, "test@example.com", "email"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("FindIdentityForChannel: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("WebhookSubscriptionRepository", func(t *testing.T) {
		repo := repository.NewWebhookSubscriptionRepository(nil, &mockCredentialProvider{})

		if _, err := repo.Create(ctx, nilID, "http://example.com", []string{"inbound"}, []byte("sec")); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Create: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.ListByWorkspace(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ListByWorkspace: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("WebhookDLQRepository", func(t *testing.T) {
		repo := repository.NewWebhookDLQRepository(nil, &mockCredentialProvider{})

		if err := repo.InsertDLQ(ctx, nilID, uuid.New(), "tr", "msg", "ev", []byte("{}"), "http://example.com", 1, nil); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("InsertDLQ: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.ListDLQ(ctx, nilID, 10, 0); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ListDLQ: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GetDLQBadgeCount(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetDLQBadgeCount: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("WABATemplateRepository", func(t *testing.T) {
		repo := repository.NewWABATemplateRepository(nil)

		tmpl := &repository.WABATemplate{WorkspaceID: nilID, Name: "tmpl"}
		if _, err := repo.Create(ctx, tmpl); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Create: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.Upsert(ctx, tmpl); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Upsert: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.ListByWorkspace(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ListByWorkspace: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("RecipientSessionRepository", func(t *testing.T) {
		repo := repository.NewRecipientSessionRepository(nil)

		now := time.Now()
		if err := repo.Upsert(ctx, nilID, "123", "whatsapp", "456", now, "standard"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Upsert: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.Get(ctx, nilID, "123", "whatsapp", "456"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Get: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.MarkNotifiedExpiring(ctx, nilID, "123", "whatsapp", "456", now); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("MarkNotifiedExpiring: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.UpdateLastReadAt(ctx, nilID, "123", "whatsapp", "456", now); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("UpdateLastReadAt: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.UpdateLastReadAtByContact(ctx, nilID, uuid.New(), now); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("UpdateLastReadAtByContact: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("APIKeyRepository", func(t *testing.T) {
		repo := repository.NewAPIKeyRepository(nil)

		if _, _, err := repo.Create(ctx, nilID, "key"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Create: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.ListByWorkspace(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ListByWorkspace: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.CountActive(ctx, nilID); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("CountActive: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("AuditRepository", func(t *testing.T) {
		repo := repository.NewAuditRepository(nil)

		if _, err := repo.ListConversations(ctx, nilID, ""); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ListConversations: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.ListThreadByContact(ctx, nilID, uuid.New(), nil); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ListThreadByContact: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("ChatwootMappingRepository", func(t *testing.T) {
		repo := repository.NewChatwootMappingRepository(nil)

		m := &repository.ChatwootMapping{WorkspaceID: nilID}
		if err := repo.Upsert(ctx, m); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Upsert: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GetByContactAndConnection(ctx, nilID, uuid.New(), uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetByContactAndConnection: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GetByConversationID(ctx, nilID, 123); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetByConversationID: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.Delete(ctx, nilID, uuid.New(), uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Delete: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("TypebotSessionRepository", func(t *testing.T) {
		repo := repository.NewTypebotSessionRepository(nil)

		if _, err := repo.GetSession(ctx, nilID, uuid.New(), uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetSession: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.UpsertSession(ctx, &repository.TypebotSession{WorkspaceID: nilID}); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("UpsertSession: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.DeleteSession(ctx, nilID, uuid.New(), uuid.New()); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("DeleteSession: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("UserActionLogRepository", func(t *testing.T) {
		repo := repository.NewUserActionLogRepository(nil)

		if err := repo.Insert(ctx, &repository.UserActionLog{WorkspaceID: nilID}); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Insert: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, _, err := repo.ListByWorkspace(ctx, nilID, 10, 0, "", ""); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ListByWorkspace: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("IntegrationRepository", func(t *testing.T) {
		repo := repository.NewIntegrationRepository(nil, &mockCredentialProvider{})

		if err := repo.Save(ctx, &repository.Integration{WorkspaceID: nilID}); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Save: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GetByProvider(ctx, nilID, "chatwoot"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetByProvider: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("CredentialsRepository", func(t *testing.T) {
		repo := repository.NewCredentialsRepository(nil, &mockCredentialProvider{})

		if err := repo.Save(ctx, nilID, "whatsapp", []byte("plain")); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Save: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.Get(ctx, nilID, "whatsapp"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Get: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.Delete(ctx, nilID, "whatsapp"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("Delete: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("MessageDispatchRepository", func(t *testing.T) {
		repo := repository.NewMessageDispatchRepository(nil)

		if _, err := repo.GetOrCreateDispatch(ctx, nilID, "tr", "whatsapp", nil, nil, nil); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetOrCreateDispatch: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("IdempotencyRepository", func(t *testing.T) {
		repo := repository.NewIdempotencyRepository(nil)

		if _, err := repo.CheckAndStore(ctx, nilID, "hash", "tr", time.Hour); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("CheckAndStore: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if _, err := repo.GetByIdempotencyKey(ctx, nilID, "hash"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("GetByIdempotencyKey: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.UpdateResponse(ctx, nilID, "hash", 200, []byte("{}"), nil); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("UpdateResponse: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.RecordLedger(ctx, &repository.IngressLedgerEntry{WorkspaceID: nilID}); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("RecordLedger: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.UpdateLedgerStatus(ctx, nilID, "tr", "delivered", nil); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("UpdateLedgerStatus: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("InboundDedupRepository", func(t *testing.T) {
		repo := repository.NewInboundDedupRepository(nil)

		if _, err := repo.InsertAndCheck(ctx, nilID, "whatsapp", "msg1"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("InsertAndCheck: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})

	t.Run("DeliveryClaimRepository", func(t *testing.T) {
		repo := repository.NewDeliveryClaimRepository(nil)

		if _, err := repo.CreateClaim(ctx, &repository.DeliveryClaim{WorkspaceID: nilID}); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("CreateClaim: expected ErrInvalidWorkspaceID, got %v", err)
		}
		if err := repo.ReleaseClaim(ctx, nilID, "tr"); !errors.Is(err, repository.ErrInvalidWorkspaceID) {
			t.Errorf("ReleaseClaim: expected ErrInvalidWorkspaceID, got %v", err)
		}
	})
}
