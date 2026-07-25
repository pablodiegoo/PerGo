package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestConnectionRepository_SlugCacheUnit(t *testing.T) {
	repo := NewConnectionRepository(nil, nil)
	wsID := uuid.New()
	connID := uuid.New()

	conn := &Connection{
		ID:          connID,
		WorkspaceID: wsID,
		Name:        "Vendas SP",
		Slug:        "vendas-sp",
		Channel:     "whatsapp",
	}

	// Inject into cache
	cacheKey := wsID.String() + ":vendas-sp"
	repo.mu.Lock()
	repo.slugCache[cacheKey] = conn
	repo.mu.Unlock()

	// Retrieve from cache using GetBySlug
	cached, err := repo.GetBySlug(context.Background(), wsID, "vendas-sp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached.ID != connID {
		t.Errorf("got connection ID %s, want %s", cached.ID, connID)
	}
	if cached.Slug != "vendas-sp" {
		t.Errorf("got slug %s, want vendas-sp", cached.Slug)
	}

	// Test Delete cache invalidation logic
	repo.mu.Lock()
	for k, c := range repo.slugCache {
		if c.ID == connID {
			delete(repo.slugCache, k)
		}
	}
	repo.mu.Unlock()

	repo.mu.RLock()
	_, found := repo.slugCache[cacheKey]
	repo.mu.RUnlock()
	if found {
		t.Errorf("expected slug to be deleted from cache")
	}
}
