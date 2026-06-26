package session

import (
	"sync"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestActiveSessionAddGet(t *testing.T) {
	r := NewActiveSession()
	jid := types.NewJID("1234567890", "s.whatsapp.net")

	s := &Session{DeviceID: "dev-1", JID: jid}
	r.Add(s)

	got := r.Get(jid)
	if got == nil || got.DeviceID != "dev-1" {
		t.Error("expected to find session")
	}
	if n := r.Len(); n != 1 {
		t.Errorf("expected 1 session, got %d", n)
	}
}

func TestActiveSessionReplace(t *testing.T) {
	r := NewActiveSession()
	jid := types.NewJID("1234567890", "s.whatsapp.net")

	cancelled := false
	r.Add(&Session{JID: jid, Cancel: func() { cancelled = true }})
	r.Add(&Session{JID: jid, Cancel: func() {}})

	if !cancelled {
		t.Error("expected old session to be cancelled on replace")
	}
	if n := r.Len(); n != 1 {
		t.Errorf("expected 1 session after replace, got %d", n)
	}
}

func TestActiveSessionRemove(t *testing.T) {
	r := NewActiveSession()
	jid := types.NewJID("1234567890", "s.whatsapp.net")

	r.Add(&Session{JID: jid})
	r.Remove(jid)

	if n := r.Len(); n != 0 {
		t.Errorf("expected 0 sessions after remove, got %d", n)
	}
}

func TestActiveSessionAll(t *testing.T) {
	r := NewActiveSession()
	r.Add(&Session{JID: types.NewJID("111", "s.whatsapp.net")})
	r.Add(&Session{JID: types.NewJID("222", "s.whatsapp.net")})

	all := r.All()
	if len(all) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(all))
	}
}

func TestActiveSessionStopAll(t *testing.T) {
	r := NewActiveSession()
	var cancelled int
	cancelFn := func() { cancelled++ }

	r.Add(&Session{JID: types.NewJID("111", "s.whatsapp.net"), Cancel: cancelFn})
	r.Add(&Session{JID: types.NewJID("222", "s.whatsapp.net"), Cancel: cancelFn})

	r.StopAll()

	if cancelled != 2 {
		t.Errorf("expected 2 cancellations, got %d", cancelled)
	}
	if n := r.Len(); n != 0 {
		t.Errorf("expected 0 sessions after StopAll, got %d", n)
	}
}

func TestActiveSessionConcurrentAccess(t *testing.T) {
	r := NewActiveSession()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			r.Add(&Session{JID: types.NewJID(string(rune('0'+i%10)), "s.whatsapp.net")})
		}(i)
		go func(i int) {
			defer wg.Done()
			r.Get(types.NewJID(string(rune('0'+i%10)), "s.whatsapp.net"))
		}(i)
	}
	wg.Wait()
	// No crash = pass
}

func TestSessionMessagesSent(t *testing.T) {
	s := &Session{}
	if v := s.MessagesSent.Load(); v != 0 {
		t.Errorf("expected 0, got %d", v)
	}
	s.MessagesSent.Add(5)
	if v := s.MessagesSent.Load(); v != 5 {
		t.Errorf("expected 5, got %d", v)
	}
}
