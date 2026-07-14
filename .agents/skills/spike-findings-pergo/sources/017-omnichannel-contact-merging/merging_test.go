package merging

import (
	"testing"

	"github.com/google/uuid"
)

// Contact represents the unified customer profile.
type Contact struct {
	ID    uuid.UUID
	Name  string
	Email string
}

// ContactIdentity links a contact to a specific channel identifier.
type ContactIdentity struct {
	ID             uuid.UUID
	ContactID      uuid.UUID
	Channel        string // e.g. "whatsapp", "telegram"
	SenderIdentity string // e.g. "+5511999990000" or "@username"
}

// Conversation represents a thread on a channel.
type Conversation struct {
	ID        uuid.UUID
	ContactID uuid.UUID
	Channel   string
}

// MemoryStore is an in-memory representation of our DB tables.
type MemoryStore struct {
	Contacts      map[uuid.UUID]*Contact
	Identities    map[uuid.UUID]*ContactIdentity
	Conversations map[uuid.UUID]*Conversation
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		Contacts:      make(map[uuid.UUID]*Contact),
		Identities:    make(map[uuid.UUID]*ContactIdentity),
		Conversations: make(map[uuid.UUID]*Conversation),
	}
}

// ResolveContact locates a contact by channel identity or creates a new one.
func (s *MemoryStore) ResolveContact(channel, senderIdentity, name string) (*Contact, error) {
	// Find existing identity
	for _, ident := range s.Identities {
		if ident.Channel == channel && ident.SenderIdentity == senderIdentity {
			return s.Contacts[ident.ContactID], nil
		}
	}

	// Not found, create new contact & link identity
	c := &Contact{
		ID:   uuid.New(),
		Name: name,
	}
	s.Contacts[c.ID] = c

	ident := &ContactIdentity{
		ID:             uuid.New(),
		ContactID:      c.ID,
		Channel:        channel,
		SenderIdentity: senderIdentity,
	}
	s.Identities[ident.ID] = ident

	return c, nil
}

// MergeContacts unifies two contacts, updating all identities and conversations.
func (s *MemoryStore) MergeContacts(primaryID, secondaryID uuid.UUID) error {
	if _, ok := s.Contacts[primaryID]; !ok {
		return tError("primary contact not found")
	}
	if _, ok := s.Contacts[secondaryID]; !ok {
		return tError("secondary contact not found")
	}

	// 1. Update identities associated with the secondary contact
	for _, ident := range s.Identities {
		if ident.ContactID == secondaryID {
			ident.ContactID = primaryID
		}
	}

	// 2. Update conversations associated with the secondary contact
	for _, conv := range s.Conversations {
		if conv.ContactID == secondaryID {
			conv.ContactID = primaryID
		}
	}

	// 3. Delete secondary contact
	delete(s.Contacts, secondaryID)

	return nil
}

func tError(msg string) error {
	return &testError{msg}
}

type testError struct {
	msg string
}

func (e *testError) Error() string { return e.msg }

func TestContactMerging(t *testing.T) {
	store := NewMemoryStore()

	t.Run("Resolves existing contact and creates missing identities", func(t *testing.T) {
		// 1. Inbound WhatsApp message from +5511999990000
		c1, _ := store.ResolveContact("whatsapp", "+5511999990000", "Carlos")
		if c1.Name != "Carlos" {
			t.Errorf("got name %s, want Carlos", c1.Name)
		}

		// 2. Subsequent message from same identity resolves to same contact
		c2, _ := store.ResolveContact("whatsapp", "+5511999990000", "Carlos Novaes")
		if c1.ID != c2.ID {
			t.Errorf("expected same contact ID, got c1: %v, c2: %v", c1.ID, c2.ID)
		}
	})

	t.Run("Merges two channel contacts and unifies histories", func(t *testing.T) {
		// 1. Resolve Carlos on WhatsApp
		cWA, _ := store.ResolveContact("whatsapp", "+5511999990000", "Carlos WA")

		// 2. Resolve Carlos on Telegram (currently separate contacts)
		cTG, _ := store.ResolveContact("telegram", "@carlos_tg", "Carlos TG")

		if cWA.ID == cTG.ID {
			t.Fatalf("expected different contacts before merge")
		}

		// Create conversations for both
		convWA := &Conversation{ID: uuid.New(), ContactID: cWA.ID, Channel: "whatsapp"}
		convTG := &Conversation{ID: uuid.New(), ContactID: cTG.ID, Channel: "telegram"}
		store.Conversations[convWA.ID] = convWA
		store.Conversations[convTG.ID] = convTG

		// 3. Merge Telegram contact into WhatsApp contact
		err := store.MergeContacts(cWA.ID, cTG.ID)
		if err != nil {
			t.Fatalf("merge failed: %v", err)
		}

		// 4. Assert TG contact is deleted
		if _, exists := store.Contacts[cTG.ID]; exists {
			t.Errorf("secondary contact should be deleted")
		}

		// 5. Assert conversations are linked to primary contact
		if store.Conversations[convTG.ID].ContactID != cWA.ID {
			t.Errorf("telegram conversation should now belong to primary contact")
		}

		// 6. Resolving Telegram identity now yields primary contact (Carlos WA)
		cResolved, _ := store.ResolveContact("telegram", "@carlos_tg", "Carlos")
		if cResolved.ID != cWA.ID {
			t.Errorf("resolving telegram identity should return merged primary contact")
		}
	})
}
