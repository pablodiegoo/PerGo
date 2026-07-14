package cache

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// ActiveSession represents the live, authenticated in-memory socket client session.
type ActiveSession struct {
	ConnectionID   uuid.UUID
	SenderIdentity string
	ClientState    string // e.g. "connected", "connecting"
}

// SessionCache manages live active client sessions in-memory.
type SessionCache struct {
	mu           sync.RWMutex
	byID         map[uuid.UUID]*ActiveSession
	byIdentity   map[string]*ActiveSession
	dbQueryCount int64 // Track simulated DB queries
}

func NewSessionCache() *SessionCache {
	return &SessionCache{
		byID:       make(map[uuid.UUID]*ActiveSession),
		byIdentity: make(map[string]*ActiveSession),
	}
}

// Set stores or updates a session in the cache.
func (c *SessionCache) Set(sess *ActiveSession) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.byID[sess.ConnectionID] = sess
	c.byIdentity[sess.SenderIdentity] = sess
}

// GetByID retrieves from cache or queries the mock DB on miss.
func (c *SessionCache) GetByID(ctx context.Context, id uuid.UUID, dbFallback func(uuid.UUID) (*ActiveSession, error)) (*ActiveSession, error) {
	// 1. Lock-free read check first
	c.mu.RLock()
	sess, exists := c.byID[id]
	c.mu.RUnlock()

	if exists {
		return sess, nil
	}

	// 2. Cache miss, query DB and update cache
	atomic.AddInt64(&c.dbQueryCount, 1)
	dbSess, err := dbFallback(id)
	if err != nil {
		return nil, err
	}

	c.Set(dbSess)
	return dbSess, nil
}

// GetByIdentity retrieves from cache.
func (c *SessionCache) GetByIdentity(identity string) (*ActiveSession, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	sess, exists := c.byIdentity[identity]
	return sess, exists
}

// Delete evicts a session on disconnect.
func (c *SessionCache) Delete(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if sess, exists := c.byID[id]; exists {
		delete(c.byIdentity, sess.SenderIdentity)
		delete(c.byID, id)
	}
}

func TestSessionCache(t *testing.T) {
	cache := NewSessionCache()

	connID := uuid.New()
	sender := "+5511999990000"

	// Mock DB finder function
	dbMock := func(id uuid.UUID) (*ActiveSession, error) {
		if id == connID {
			return &ActiveSession{
				ConnectionID:   connID,
				SenderIdentity: sender,
				ClientState:    "connected",
			}, nil
		}
		return nil, fmt.Errorf("connection %v not found in DB", id)
	}

	t.Run("Bypasses database queries on subsequent calls", func(t *testing.T) {
		ctx := context.Background()

		// First call is a miss (hits database)
		sess1, err := cache.GetByID(ctx, connID, dbMock)
		if err != nil {
			t.Fatalf("first fetch failed: %v", err)
		}
		if atomic.LoadInt64(&cache.dbQueryCount) != 1 {
			t.Errorf("expected 1 DB query, got: %d", cache.dbQueryCount)
		}

		// Second call is a hit (bypasses database)
		sess2, err := cache.GetByID(ctx, connID, dbMock)
		if err != nil {
			t.Fatalf("second fetch failed: %v", err)
		}
		if sess1.ClientState != sess2.ClientState {
			t.Errorf("states do not match")
		}
		if atomic.LoadInt64(&cache.dbQueryCount) != 1 {
			t.Errorf("expected still 1 DB query, got: %d", cache.dbQueryCount)
		}
	})

	t.Run("Evicts session cleanly on Delete", func(t *testing.T) {
		cache.Delete(connID)

		_, exists := cache.GetByIdentity(sender)
		if exists {
			t.Errorf("session should be evicted from identity map")
		}
	})

	t.Run("Supports concurrent safe reads/writes", func(t *testing.T) {
		var wg sync.WaitGroup
		workers := 50
		wg.Add(workers)

		for i := 0; i < workers; i++ {
			go func(idx int) {
				defer wg.Done()
				id := uuid.New()
				ident := fmt.Sprintf("+55119%08d", idx)

				// Concurrent writes
				cache.Set(&ActiveSession{
					ConnectionID:   id,
					SenderIdentity: ident,
					ClientState:    "connected",
				})

				// Concurrent reads
				_, _ = cache.GetByIdentity(ident)
			}(i)
		}
		wg.Wait()
	})
}
