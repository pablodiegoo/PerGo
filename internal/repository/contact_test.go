package repository_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestContactRepository(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up contacts and workspace data
	_, _ = pool.Exec(ctx, "DELETE FROM contact_identities")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	repo := repository.NewContactRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "contact_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	t.Run("ResolveContact_New", func(t *testing.T) {
		contact, err := repo.ResolveContact(ctx, ws.ID, "telegram", "12345", "John Doe", "johndoe", "+1234567")
		if err != nil {
			t.Fatalf("failed to resolve new contact: %v", err)
		}

		if contact.Name != "John Doe" {
			t.Errorf("expected name John Doe, got %s", contact.Name)
		}

		if len(contact.Identities) != 3 {
			t.Errorf("expected 3 identities (telegram, telegram_username, phone), got %d", len(contact.Identities))
		}

		// Resolve again - should return the same contact
		resolved, err := repo.ResolveContact(ctx, ws.ID, "telegram", "12345", "John Doe Updated", "johndoe", "+1234567")
		if err != nil {
			t.Fatalf("failed to resolve existing contact: %v", err)
		}

		if resolved.ID != contact.ID {
			t.Errorf("expected same contact ID, got different: %s vs %s", contact.ID, resolved.ID)
		}
	})

	t.Run("ResolveContact_Concurrent", func(t *testing.T) {
		const numGoroutines = 10
		var wg sync.WaitGroup
		errorsChan := make(chan error, numGoroutines)
		resolvedContacts := make([]uuid.UUID, numGoroutines)

		senderID := "concurrent-sender-99"

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				c, err := repo.ResolveContact(ctx, ws.ID, "whatsapp", senderID, "Concurrent User", "", "")
				if err != nil {
					errorsChan <- err
					return
				}
				resolvedContacts[idx] = c.ID
			}(i)
		}

		wg.Wait()
		close(errorsChan)

		for err := range errorsChan {
			t.Errorf("concurrent ResolveContact error: %v", err)
		}

		// Ensure all returned the same contact ID
		var firstID uuid.UUID
		for _, id := range resolvedContacts {
			if id == uuid.Nil {
				continue
			}
			if firstID == uuid.Nil {
				firstID = id
			} else if firstID != id {
				t.Errorf("concurrency check: expected all goroutines to get the same contact ID, but got %s and %s", firstID, id)
			}
		}
	})

	t.Run("ResolveContact_CrossLinking", func(t *testing.T) {
		// 1. Create a contact with telegram username
		c1, err := repo.ResolveContact(ctx, ws.ID, "telegram", "tg-999", "Alice", "alice_username", "")
		if err != nil {
			t.Fatalf("failed to resolve c1: %v", err)
		}

		// 2. Resolve a different channel (e.g. whatsapp) but with the same phone, creating c2
		c2, err := repo.ResolveContact(ctx, ws.ID, "whatsapp", "wa-999", "Alice WA", "", "+99999")
		if err != nil {
			t.Fatalf("failed to resolve c2: %v", err)
		}

		// 3. Resolve using telegram but same username as c1, should match c1
		matchedC1, err := repo.ResolveContact(ctx, ws.ID, "telegram", "tg-999-new", "Alice Match Username", "alice_username", "")
		if err != nil {
			t.Fatalf("failed to resolve username match: %v", err)
		}
		if matchedC1.ID != c1.ID {
			t.Errorf("expected matched contact to be c1 (%s), got %s", c1.ID, matchedC1.ID)
		}

		// 4. Resolve using whatsapp but same phone as c2, should match c2
		matchedC2, err := repo.ResolveContact(ctx, ws.ID, "telegram", "tg-phone-match", "Alice Match Phone", "", "+99999")
		if err != nil {
			t.Fatalf("failed to resolve phone match: %v", err)
		}
		if matchedC2.ID != c2.ID {
			t.Errorf("expected matched contact to be c2 (%s), got %s", c2.ID, matchedC2.ID)
		}
	})

	t.Run("MergeContacts", func(t *testing.T) {
		// Create primary
		primary, err := repo.ResolveContact(ctx, ws.ID, "telegram", "prim-tg", "Primary Name", "prim-user", "+1111")
		if err != nil {
			t.Fatalf("failed to create primary contact: %v", err)
		}

		// Create secondary
		secondary, err := repo.ResolveContact(ctx, ws.ID, "whatsapp", "sec-wa", "Secondary Name", "", "+2222")
		if err != nil {
			t.Fatalf("failed to create secondary contact: %v", err)
		}

		// Merge them
		err = repo.MergeContacts(ctx, ws.ID, primary.ID, secondary.ID)
		if err != nil {
			t.Fatalf("failed to merge contacts: %v", err)
		}

		// Verify secondary is deleted
		_, err = repo.GetByID(ctx, ws.ID, secondary.ID)
		if err == nil || err.Error() != repository.ErrContactNotFound.Error() {
			t.Errorf("expected secondary contact to be deleted, but found it or got error: %v", err)
		}

		// Verify primary has secondary's identities
		updatedPrimary, err := repo.GetByID(ctx, ws.ID, primary.ID)
		if err != nil {
			t.Fatalf("failed to load updated primary contact: %v", err)
		}

		hasSecWA := false
		for _, identity := range updatedPrimary.Identities {
			if identity.Channel == "whatsapp" && identity.SenderIdentity == "sec-wa" {
				hasSecWA = true
			}
		}

		if !hasSecWA {
			t.Error("primary contact did not acquire secondary's whatsapp identity")
		}

		// Test merging two distinct contacts
		c3, err := repo.ResolveContact(ctx, ws.ID, "telegram", "c3-tg", "Contact 3", "", "")
		if err != nil {
			t.Fatalf("failed to create c3: %v", err)
		}

		err = repo.MergeContacts(ctx, ws.ID, primary.ID, c3.ID)
		if err != nil {
			t.Fatalf("failed to merge c3: %v", err)
		}
	})

	t.Run("SearchContacts", func(t *testing.T) {
		_, _ = repo.ResolveContact(ctx, ws.ID, "telegram", "search-tg-1", "Searchable Alice", "", "")
		_, _ = repo.ResolveContact(ctx, ws.ID, "whatsapp", "search-wa-1", "Bob Search", "", "")

		results, err := repo.SearchContacts(ctx, ws.ID, "Search", uuid.Nil, 10)
		if err != nil {
			t.Fatalf("SearchContacts failed: %v", err)
		}

		if len(results) != 2 {
			t.Errorf("expected 2 search results, got %d", len(results))
		}
	})

	t.Run("ResolveTelegramChatID", func(t *testing.T) {
		// Numeric ID should be returned as is
		res, err := repo.ResolveTelegramChatID(ctx, ws.ID, "123456")
		if err != nil || res != "123456" {
			t.Errorf("expected '123456', got %q with error: %v", res, err)
		}

		resNegative, err := repo.ResolveTelegramChatID(ctx, ws.ID, "-100123456")
		if err != nil || resNegative != "-100123456" {
			t.Errorf("expected '-100123456', got %q with error: %v", resNegative, err)
		}

		// Create a contact to test resolve by username/phone
		_, err = repo.ResolveContact(ctx, ws.ID, "telegram", "987654", "Telegram User", "testusername", "+987654321")
		if err != nil {
			t.Fatalf("failed to create telegram contact: %v", err)
		}

		// Resolve by username (with @)
		resUser, err := repo.ResolveTelegramChatID(ctx, ws.ID, "@testusername")
		if err != nil || resUser != "987654" {
			t.Errorf("expected '987654', got %q with error: %v", resUser, err)
		}

		// Resolve by username (without @)
		resUserNoAt, err := repo.ResolveTelegramChatID(ctx, ws.ID, "testusername")
		if err != nil || resUserNoAt != "987654" {
			t.Errorf("expected '987654', got %q with error: %v", resUserNoAt, err)
		}

		// Resolve by phone
		resPhone, err := repo.ResolveTelegramChatID(ctx, ws.ID, "+987654321")
		if err != nil || resPhone != "987654" {
			t.Errorf("expected '987654', got %q with error: %v", resPhone, err)
		}
	})
}
