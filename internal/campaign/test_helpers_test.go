package campaign

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/postgres"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://admin:admin@localhost:5432/pergo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available at %s: %v", dsn, err)
	}

	if err := pool.Ping(ctx); err != nil {
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

type mockTagLister struct {
	contactsByTag map[uuid.UUID][]domain.Contact
	err           error
}

func newMockTagLister() *mockTagLister {
	return &mockTagLister{
		contactsByTag: make(map[uuid.UUID][]domain.Contact),
	}
}

func (m *mockTagLister) SetContacts(tagID uuid.UUID, contacts []domain.Contact) {
	m.contactsByTag[tagID] = contacts
}

func (m *mockTagLister) ListContactsByTag(ctx context.Context, workspaceID, tagID uuid.UUID) ([]domain.Contact, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.contactsByTag[tagID], nil
}

type fakeAuditWriter struct {
	mu     sync.Mutex
	events []audit.Event
}

func newFakeAuditWriter() *fakeAuditWriter {
	return &fakeAuditWriter{}
}

func (f *fakeAuditWriter) Write(e audit.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *fakeAuditWriter) Close() error {
	return nil
}

func (f *fakeAuditWriter) EnsurePartitions(ctx context.Context) error {
	return nil
}

func (f *fakeAuditWriter) Events() []audit.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]audit.Event, len(f.events))
	copy(cp, f.events)
	return cp
}

func (f *fakeAuditWriter) EventsByType(eventType string) []audit.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []audit.Event
	for _, e := range f.events {
		if e.EventType == eventType {
			out = append(out, e)
		}
	}
	return out
}

func (f *fakeAuditWriter) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = nil
}
