package repository

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestInboundDeduplicate(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	repo := NewInboundDedupRepository(pool)
	ctx := context.Background()

	// Setup clean workspace
	wsRepo := NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "dedup_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	t.Run("Insert and check uniqueness", func(t *testing.T) {
		providerMsgID := "msg_unique_123"
		channelName := "whatsapp"

		// First attempt should return true (unique)
		inserted, err := repo.InsertAndCheck(ctx, ws.ID, channelName, providerMsgID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !inserted {
			t.Fatal("expected message to be unique")
		}

		// Second attempt should return false (duplicate)
		inserted, err = repo.InsertAndCheck(ctx, ws.ID, channelName, providerMsgID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if inserted {
			t.Fatal("expected message to be classified as duplicate")
		}
	})

	t.Run("Concurrent insertions", func(t *testing.T) {
		providerMsgID := "msg_concurrent_999"
		channelName := "telegram"

		const goroutines = 10
		var wg sync.WaitGroup
		results := make(chan bool, goroutines)
		errorsChan := make(chan error, goroutines)

		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				inserted, err := repo.InsertAndCheck(ctx, ws.ID, channelName, providerMsgID)
				if err != nil {
					errorsChan <- err
					return
				}
				results <- inserted
			}()
		}

		wg.Wait()
		close(results)
		close(errorsChan)

		for err := range errorsChan {
			t.Fatalf("unexpected concurrent error: %v", err)
		}

		insertedCount := 0
		for res := range results {
			if res {
				insertedCount++
			}
		}

		if insertedCount != 1 {
			t.Errorf("expected exactly 1 insertion to succeed, but got %d", insertedCount)
		}
	})
}
