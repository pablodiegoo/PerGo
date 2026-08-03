package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestTagRepository(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up
	_, _ = pool.Exec(ctx, "DELETE FROM contact_tags")
	_, _ = pool.Exec(ctx, "DELETE FROM tags")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	// Create workspace
	wsID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, $2)`, wsID, "Test Workspace Tags")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	t.Run("CreateTag and ListTags", func(t *testing.T) {
		tag1, err := tagRepo.CreateTag(ctx, wsID, "VIP", "#FF0000")
		if err != nil {
			t.Fatalf("failed to create tag1: %v", err)
		}
		if tag1.Name != "VIP" || tag1.Color != "#FF0000" {
			t.Errorf("unexpected tag: %+v", tag1)
		}

		tag2, err := tagRepo.CreateTag(ctx, wsID, "Lead", "#00FF00")
		if err != nil {
			t.Fatalf("failed to create tag2: %v", err)
		}

		// Duplicate tag name check
		_, err = tagRepo.CreateTag(ctx, wsID, "VIP", "#111111")
		if err == nil {
			t.Errorf("expected duplicate tag error, got nil")
		}

		tags, err := tagRepo.ListTags(ctx, wsID)
		if err != nil {
			t.Fatalf("failed to list tags: %v", err)
		}
		if len(tags) != 2 {
			t.Fatalf("expected 2 tags, got %d", len(tags))
		}
		// Ordered by name ASC: Lead, VIP
		if tags[0].ID != tag2.ID || tags[1].ID != tag1.ID {
			t.Errorf("unexpected tags list order: %+v", tags)
		}
	})

	t.Run("AddTagToContact, GetContactTags, and ListContactsByTag", func(t *testing.T) {
		contact, err := contactRepo.ResolveContact(ctx, wsID, "whatsapp", "5511999998888", "John Tagged", "", "")
		if err != nil {
			t.Fatalf("failed to resolve contact: %v", err)
		}

		tags, err := tagRepo.ListTags(ctx, wsID)
		if err != nil || len(tags) == 0 {
			t.Fatalf("expected tags to exist")
		}

		vipTag := tags[1] // VIP

		err = tagRepo.AddTagToContact(ctx, wsID, contact.ID, vipTag.ID)
		if err != nil {
			t.Fatalf("failed to add tag to contact: %v", err)
		}

		cTags, err := tagRepo.GetContactTags(ctx, wsID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get contact tags: %v", err)
		}
		if len(cTags) != 1 || cTags[0].ID != vipTag.ID {
			t.Errorf("expected VIP tag on contact, got %+v", cTags)
		}

		contactsByTag, err := tagRepo.ListContactsByTag(ctx, wsID, vipTag.ID)
		if err != nil {
			t.Fatalf("failed to list contacts by tag: %v", err)
		}
		if len(contactsByTag) != 1 || contactsByTag[0].ID != contact.ID {
			t.Errorf("expected 1 contact by VIP tag, got %+v", contactsByTag)
		}

		// Remove tag
		err = tagRepo.RemoveTagFromContact(ctx, wsID, contact.ID, vipTag.ID)
		if err != nil {
			t.Fatalf("failed to remove tag from contact: %v", err)
		}

		cTagsAfter, err := tagRepo.GetContactTags(ctx, wsID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get contact tags after removal: %v", err)
		}
		if len(cTagsAfter) != 0 {
			t.Errorf("expected 0 contact tags, got %d", len(cTagsAfter))
		}
	})

	t.Run("DeleteTag", func(t *testing.T) {
		tags, err := tagRepo.ListTags(ctx, wsID)
		if err != nil || len(tags) == 0 {
			t.Fatalf("expected tags to exist")
		}

		targetTag := tags[0]
		err = tagRepo.DeleteTag(ctx, wsID, targetTag.ID)
		if err != nil {
			t.Fatalf("failed to delete tag: %v", err)
		}

		_, err = tagRepo.GetTagByID(ctx, wsID, targetTag.ID)
		if err == nil {
			t.Errorf("expected tag to be deleted, got nil error")
		}
	})
}
