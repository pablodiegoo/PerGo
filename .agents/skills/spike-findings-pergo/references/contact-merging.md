# Omnichannel Contact Merging

## Requirements

- Must support linking multiple channel-specific sender identities (e.g., Telegram handle, WhatsApp phone number) to a single unified customer profile.
- Must support merging two customer profiles to consolidate identity links and conversation threads.
- Message history queries must resolve the primary profile to show a consolidated chat history.

## How to Build It

1. **Relational Models:** Map contact details and identities in separate tables:
   ```sql
   CREATE TABLE contacts (
       id UUID PRIMARY KEY,
       name VARCHAR(100) NOT NULL,
       email VARCHAR(100)
   );

   CREATE TABLE contact_identities (
       id UUID PRIMARY KEY,
       contact_id UUID REFERENCES contacts(id) ON DELETE CASCADE,
       channel VARCHAR(50) NOT NULL,
       sender_identity VARCHAR(100) NOT NULL,
       UNIQUE (channel, sender_identity)
   );
   ```

2. **Identity Resolution:** On incoming messages, query the identity registry first:
   ```go
   func (s *Store) ResolveContact(channel, senderIdentity, name string) (*Contact, error) {
       // Look up identity. If found, return s.Contacts[ident.ContactID].
       // If not found, create Contact, create ContactIdentity mapping, and link them.
   }
   ```

3. **Contact Merging Action:** Unify secondary contact records under a single primary ID:
   ```go
   func (s *Store) MergeContacts(primaryID, secondaryID uuid.UUID) error {
       // 1. Update all identities pointing to secondaryID to primaryID.
       // 2. Update all conversations pointing to secondaryID to primaryID.
       // 3. Delete secondary contact.
   }
   ```

## What to Avoid

- **Duplicate Identity Mappings:** Ensure a unique constraint on `(channel, sender_identity)` prevents registering a single account address to multiple contacts.
- **Dangling Conversations:** Always wrap the contact merge in a database transaction to prevent conversations or identities from being orphaned if a step fails.

## Constraints

- Deleting a contact must safely clean up identity registry records via cascade.

## Origin

Synthesized from spikes: 017
Source files available in: sources/017-omnichannel-contact-merging/
