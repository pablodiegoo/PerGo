package session

import (
	"sync"
	"sync/atomic"

	"go.mau.fi/whatsmeow/types"

	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
)

// Session represents an active WhatsApp device session managed by the server.
// It tracks the client, adapter, and per-session metrics.
type Session struct {
	DeviceID     string
	JID          types.JID
	Client       *whatsapp.WhatsAppClient
	Cancel       func()
	MessagesSent atomic.Int64
}

// ActiveSession tracks connected WhatsApp sessions in memory.
type ActiveSession struct {
	mu       sync.RWMutex
	sessions map[string]*Session // keyed by JID string
}

// NewActiveSession creates an empty session registry.
func NewActiveSession() *ActiveSession {
	return &ActiveSession{
		sessions: make(map[string]*Session),
	}
}

// Add registers a session. Replaces any existing session for the same JID.
func (r *ActiveSession) Add(s *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := s.JID.String()
	if old, ok := r.sessions[key]; ok && old.Cancel != nil {
		old.Cancel()
	}
	r.sessions[key] = s
}

// Remove removes a session by JID. Does not cancel the session's context.
func (r *ActiveSession) Remove(jid types.JID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, jid.String())
}

// Get returns the session for a JID, or nil.
func (r *ActiveSession) Get(jid types.JID) *Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[jid.String()]
}

// All returns a snapshot of all active sessions.
func (r *ActiveSession) All() []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	return out
}

// Len returns the number of active sessions.
func (r *ActiveSession) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

// StopAll cancels all session contexts and clears the registry.
func (r *ActiveSession) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if s.Cancel != nil {
			s.Cancel()
		}
	}
	r.sessions = make(map[string]*Session)
}

// ForEach iterates over sessions with a read lock.
func (r *ActiveSession) ForEach(fn func(*Session)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.sessions {
		fn(s)
	}
}

// DisconnectByJID cancels a session by JID string and removes it from the registry.
// No-op if the JID is not found.
func (r *ActiveSession) DisconnectByJID(jid string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[jid]
	if !ok {
		return
	}
	if s.Cancel != nil {
		s.Cancel()
	}
	delete(r.sessions, jid)
}

// GetClient retrieves the WhatsAppClient for a given JID string.
func (r *ActiveSession) GetClient(jid string) *whatsapp.WhatsAppClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[jid]
	if !ok {
		return nil
	}
	return s.Client
}

