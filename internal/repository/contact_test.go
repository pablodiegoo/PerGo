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

		// Attach tags to primary and secondary before merge
		tagRepo := repository.NewTagRepository(pool)
		if _, err := tagRepo.AddTagsByName(ctx, ws.ID, primary.ID, []string{"PrimaryTag", "SharedTag"}); err != nil {
			t.Fatalf("failed to tag primary contact: %v", err)
		}
		if _, err := tagRepo.AddTagsByName(ctx, ws.ID, secondary.ID, []string{"SecondaryTag", "sharedtag"}); err != nil {
			t.Fatalf("failed to tag secondary contact: %v", err)
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

		// Verify primary has secondary's identities and merged tags
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

		if len(updatedPrimary.Tags) != 3 || updatedPrimary.Tags[0] != "PrimaryTag" || updatedPrimary.Tags[1] != "SecondaryTag" || updatedPrimary.Tags[2] != "SharedTag" {
			t.Errorf("expected merged primary tags [PrimaryTag SecondaryTag SharedTag], got %v", updatedPrimary.Tags)
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

	t.Run("TagsLifecycle", func(t *testing.T) {
		// 1. Check newly created contact has empty tags
		contact, err := repo.ResolveContact(ctx, ws.ID, "telegram", "tags-test-tg", "Tags User", "", "")
		if err != nil {
			t.Fatalf("failed to create contact: %v", err)
		}

		if len(contact.Tags) != 0 {
			t.Errorf("expected empty tags, got %v", contact.Tags)
		}

		// 2. Add tags via TagRepository and verify projection on GetByID and GetByIDs
		tagRepo := repository.NewTagRepository(pool)
		_, err = tagRepo.AddTagsByName(ctx, ws.ID, contact.ID, []string{"vip", "support", "vip"})
		if err != nil {
			t.Fatalf("failed to add tags: %v", err)
		}

		contact, err = repo.GetByID(ctx, ws.ID, contact.ID)
		if err != nil {
			t.Fatalf("failed to reload contact: %v", err)
		}

		if len(contact.Tags) != 2 || contact.Tags[0] != "support" || contact.Tags[1] != "vip" {
			t.Errorf("expected sorted tags [support vip], got %v", contact.Tags)
		}

		batchMap, err := repo.GetByIDs(ctx, ws.ID, []uuid.UUID{contact.ID})
		if err != nil {
			t.Fatalf("GetByIDs failed: %v", err)
		}
		if batchContact := batchMap[contact.ID]; batchContact == nil || len(batchContact.Tags) != 2 || batchContact.Tags[0] != "support" || batchContact.Tags[1] != "vip" {
			t.Errorf("expected GetByIDs contact tags [support vip], got %+v", batchContact)
		}
	})

	t.Run("CustomAttributes_CRUD", func(t *testing.T) {
		email := "carlos@example.com"
		initialAttrs := map[string]string{
			"plan": "Pro",
			"city": "Belo Horizonte",
		}

		// 1. Create contact with attributes
		created, err := repo.CreateContact(ctx, ws.ID, "Carlos Attributes", &email, initialAttrs)
		if err != nil {
			t.Fatalf("failed to create contact: %v", err)
		}
		if created.Attributes["plan"] != "Pro" || created.Attributes["city"] != "Belo Horizonte" {
			t.Errorf("unexpected created attributes: %v", created.Attributes)
		}

		// 2. GetByID should return attributes
		loaded, err := repo.GetByID(ctx, ws.ID, created.ID)
		if err != nil {
			t.Fatalf("failed to get contact by ID: %v", err)
		}
		if loaded.Attributes["plan"] != "Pro" || loaded.Attributes["city"] != "Belo Horizonte" {
			t.Errorf("unexpected loaded attributes: %v", loaded.Attributes)
		}

		// 3. UpdateAttributes
		updatedAttrs := map[string]string{
			"plan":    "Enterprise",
			"city":    "Belo Horizonte",
			"country": "Brazil",
		}
		err = repo.UpdateAttributes(ctx, ws.ID, created.ID, updatedAttrs)
		if err != nil {
			t.Fatalf("failed to update attributes: %v", err)
		}

		loadedAfterUpdate, err := repo.GetByID(ctx, ws.ID, created.ID)
		if err != nil {
			t.Fatalf("failed to get contact after update: %v", err)
		}
		if loadedAfterUpdate.Attributes["plan"] != "Enterprise" || loadedAfterUpdate.Attributes["country"] != "Brazil" {
			t.Errorf("unexpected attributes after update: %v", loadedAfterUpdate.Attributes)
		}

		// 4. UpdateContact
		newName := "Carlos Renamed"
		loadedAfterUpdate.Name = newName
		loadedAfterUpdate.Attributes["vip"] = "true"
		err = repo.UpdateContact(ctx, ws.ID, loadedAfterUpdate)
		if err != nil {
			t.Fatalf("failed to update contact: %v", err)
		}

		loadedAfterFullUpdate, err := repo.GetByID(ctx, ws.ID, created.ID)
		if err != nil {
			t.Fatalf("failed to get contact after full update: %v", err)
		}
		if loadedAfterFullUpdate.Name != newName || loadedAfterFullUpdate.Attributes["vip"] != "true" {
			t.Errorf("unexpected contact after full update: %+v", loadedAfterFullUpdate)
		}

		// 5. SearchContacts should include attributes
		searchRes, err := repo.SearchContacts(ctx, ws.ID, "Carlos", uuid.Nil, 10)
		if err != nil {
			t.Fatalf("failed to search contacts: %v", err)
		}
		if len(searchRes) == 0 || searchRes[0].Attributes["plan"] != "Enterprise" {
			t.Errorf("expected search result with attributes, got %v", searchRes)
		}

		// 6. DeleteContact
		err = repo.DeleteContact(ctx, ws.ID, created.ID)
		if err != nil {
			t.Fatalf("failed to delete contact: %v", err)
		}
		_, err = repo.GetByID(ctx, ws.ID, created.ID)
		if err == nil || err != repository.ErrContactNotFound {
			t.Errorf("expected ErrContactNotFound after deletion, got %v", err)
		}
	})

	t.Run("MergeContacts_Attributes", func(t *testing.T) {
		pEmail := "prim@example.com"
		sEmail := "sec@example.com"
		primary, err := repo.CreateContact(ctx, ws.ID, "Primary Merge", &pEmail, map[string]string{
			"city":   "SP",
			"tier":   "Gold",
			"shared": "from_primary",
		})
		if err != nil {
			t.Fatalf("failed to create primary: %v", err)
		}

		secondary, err := repo.CreateContact(ctx, ws.ID, "Secondary Merge", &sEmail, map[string]string{
			"city":     "RJ",
			"country":  "Brazil",
			"shared":   "from_secondary",
			"discount": "10%",
		})
		if err != nil {
			t.Fatalf("failed to create secondary: %v", err)
		}

		err = repo.MergeContacts(ctx, ws.ID, primary.ID, secondary.ID)
		if err != nil {
			t.Fatalf("failed to merge contacts: %v", err)
		}

		mergedPrimary, err := repo.GetByID(ctx, ws.ID, primary.ID)
		if err != nil {
			t.Fatalf("failed to get merged primary: %v", err)
		}

		// Primary values should take precedence, secondary unique keys should be merged in
		if mergedPrimary.Attributes["city"] != "SP" {
			t.Errorf("expected primary 'city' to remain 'SP', got %s", mergedPrimary.Attributes["city"])
		}
		if mergedPrimary.Attributes["shared"] != "from_primary" {
			t.Errorf("expected primary 'shared' to remain 'from_primary', got %s", mergedPrimary.Attributes["shared"])
		}
		if mergedPrimary.Attributes["tier"] != "Gold" {
			t.Errorf("expected primary 'tier' to be 'Gold', got %s", mergedPrimary.Attributes["tier"])
		}
		if mergedPrimary.Attributes["country"] != "Brazil" {
			t.Errorf("expected merged 'country' to be 'Brazil', got %s", mergedPrimary.Attributes["country"])
		}
		if mergedPrimary.Attributes["discount"] != "10%" {
			t.Errorf("expected merged 'discount' to be '10%%', got %s", mergedPrimary.Attributes["discount"])
		}
	})
}

func TestFindIdentityForChannel(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	repo := repository.NewContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "find_id_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	t.Run("Direct Format Detection", func(t *testing.T) {
		// WhatsApp valid phone
		id, ok, err := repo.FindIdentityForChannel(ctx, ws.ID, "+5511999998888", "whatsapp")
		if err != nil || !ok || id != "5511999998888" {
			t.Errorf("expected phone 5511999998888, got %s (ok=%v, err=%v)", id, ok, err)
		}

		// Email address
		id, ok, err = repo.FindIdentityForChannel(ctx, ws.ID, "test@example.com", "email")
		if err != nil || !ok || id != "test@example.com" {
			t.Errorf("expected email test@example.com, got %s (ok=%v, err=%v)", id, ok, err)
		}

		// Telegram numeric chat ID
		id, ok, err = repo.FindIdentityForChannel(ctx, ws.ID, "987654321", "telegram")
		if err != nil || !ok || id != "987654321" {
			t.Errorf("expected chat ID 987654321, got %s (ok=%v, err=%v)", id, ok, err)
		}
	})

	t.Run("Cross-Channel Resolution", func(t *testing.T) {
		email := "omni@example.com"
		contact, err := repo.CreateContact(ctx, ws.ID, "Omni Contact", &email, nil)
		if err != nil {
			t.Fatalf("failed to create contact: %v", err)
		}

		// Link WhatsApp and Telegram identities to this contact
		_, err = pool.Exec(ctx, `
			INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity)
			VALUES ($1, $2, 'whatsapp', '5511998887766'),
			       ($1, $2, 'telegram', 'tg_omni_chat_42')
		`, contact.ID, ws.ID)
		if err != nil {
			t.Fatalf("failed to link identities: %v", err)
		}

		// 1. From WhatsApp phone -> Email
		target, ok, err := repo.FindIdentityForChannel(ctx, ws.ID, "5511998887766", "email")
		if err != nil || !ok || target != "omni@example.com" {
			t.Errorf("expected email omni@example.com, got %s (ok=%v, err=%v)", target, ok, err)
		}

		// 2. From WhatsApp phone -> Telegram
		target, ok, err = repo.FindIdentityForChannel(ctx, ws.ID, "5511998887766", "telegram")
		if err != nil || !ok || target != "tg_omni_chat_42" {
			t.Errorf("expected telegram chat tg_omni_chat_42, got %s (ok=%v, err=%v)", target, ok, err)
		}

		// 3. From Email -> WhatsApp
		target, ok, err = repo.FindIdentityForChannel(ctx, ws.ID, "omni@example.com", "whatsapp_cloud")
		if err != nil || !ok || target != "5511998887766" {
			t.Errorf("expected whatsapp 5511998887766, got %s (ok=%v, err=%v)", target, ok, err)
		}

		// 4. Missing channel identity returns false without error
		target, ok, err = repo.FindIdentityForChannel(ctx, ws.ID, "5511998887766", "instagram")
		if err != nil || ok || target != "" {
			t.Errorf("expected missing identity for instagram, got %s (ok=%v, err=%v)", target, ok, err)
		}
	})
}
