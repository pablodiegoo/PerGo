# Session Key Caching & Instance Router

## Requirements

- Must cache active socket/protocol sessions in memory to bypass database reads on the hot path (`POST /messages`).
- Cache must support thread-safe read/write operations under high concurrency.
- Disconnected sessions must be evicted immediately to prevent routing to dead connections.

## How to Build It

1. **Dual Index Session Cache:** Maintain a cache indexed by both Connection ID and Sender Identity:
   ```go
   type SessionCache struct {
       mu         sync.RWMutex
       byID       map[uuid.UUID]*ActiveSession
       byIdentity map[string]*ActiveSession
   }
   ```

2. **Lock-Free Read Path:** Retrieve using read locks first, querying the DB only on cache miss:
   ```go
   func (c *SessionCache) GetByID(ctx context.Context, id uuid.UUID, dbFallback func(uuid.UUID) (*ActiveSession, error)) (*ActiveSession, error) {
       c.mu.RLock()
       sess, exists := c.byID[id]
       c.mu.RUnlock()

       if exists {
           return sess, nil
       }

       dbSess, err := dbFallback(id)
       if err != nil {
           return nil, err
       }

       c.Set(dbSess)
       return dbSess, nil
   }
   ```

3. **Atomic Set/Delete operations:** Wrap mutation actions in write locks.

## What to Avoid

- **Stale Cache Entries:** Always hook into connection disconnect/heartbeat timeout handlers to call `Delete(connectionID)` and evict stale states.
- **Race Conditions:** Never update individual fields of a cached `ActiveSession` struct directly without a lock. Treat cached objects as immutable or clone them on write.

## Constraints

- Memory growth is negligible since only active connection references are cached.

## Origin

Synthesized from spikes: 019
Source files available in: sources/019-session-caching-router/
