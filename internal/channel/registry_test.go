package channel

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestRegistryGet(t *testing.T) {
	reg := NewRegistry(map[string]Dispatcher{
		"whatsapp": &mockDispatcher{},
	})

	d, ok := reg.Get("whatsapp")
	if !ok || d == nil {
		t.Error("expected to find whatsapp dispatcher")
	}

	_, ok = reg.Get("telegram")
	if ok {
		t.Error("expected telegram to be missing")
	}
}

func TestRegistryGetOrDefault(t *testing.T) {
	fallback := &mockDispatcher{}
	reg := NewRegistry(nil)

	d := reg.GetOrDefault("whatsapp", fallback)
	if d != fallback {
		t.Error("expected fallback for unknown channel")
	}

	reg.Register("whatsapp", &mockDispatcher{})
	d = reg.GetOrDefault("whatsapp", fallback)
	if d == fallback {
		t.Error("expected registered dispatcher, not fallback")
	}
}

func TestRegistryLen(t *testing.T) {
	reg := NewRegistry(map[string]Dispatcher{
		"a": &mockDispatcher{},
		"b": &mockDispatcher{},
		"c": &mockDispatcher{},
	})
	if n := reg.Len(); n != 3 {
		t.Errorf("expected 3, got %d", n)
	}
}

func TestRegistryRegister(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register("whatsapp", &mockDispatcher{})
	if n := reg.Len(); n != 1 {
		t.Errorf("expected 1 after register, got %d", n)
	}
}

func TestRegistryNames(t *testing.T) {
	reg := NewRegistry(map[string]Dispatcher{
		"whatsapp": &mockDispatcher{},
		"telegram": &mockDispatcher{},
	})
	names := reg.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	reg := NewRegistry(nil)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			reg.Register(string(rune('a'+i%26)), &mockDispatcher{})
		}(i)
		go func(i int) {
			defer wg.Done()
			reg.Get(string(rune('a' + i%26)))
		}(i)
	}
	wg.Wait()

	// No crash = pass
}

func TestNewRegistryCopiesMap(t *testing.T) {
	original := map[string]Dispatcher{
		"whatsapp": &mockDispatcher{},
	}
	reg := NewRegistry(original)
	original["telegram"] = &mockDispatcher{} // mutate caller's map

	if _, ok := reg.Get("telegram"); ok {
		t.Error("registry should not be affected by caller's map mutation")
	}
}

// mockDispatcher is a minimal Dispatcher implementation for testing.
type mockDispatcher struct {
	Err error
}

func (m *mockDispatcher) Dispatch(_ context.Context, _ *MessagePayload) (string, error) {
	return "", m.Err
}

// Ensure mockDispatcher satisfies the interface at compile time.
var _ Dispatcher = (*mockDispatcher)(nil)

func TestMockDispatcherTerminalError(t *testing.T) {
	reg := NewRegistry(map[string]Dispatcher{
		"whatsapp": &mockDispatcher{Err: NewTerminalError(errors.New("banned"))},
	})

	d, ok := reg.Get("whatsapp")
	if !ok {
		t.Fatal("expected whatsapp dispatcher")
	}
	_, err := d.Dispatch(context.Background(), &MessagePayload{To: "12345", Channel: "whatsapp", Body: "hello"})
	if err == nil {
		t.Error("expected error")
	}
	if !IsTerminal(err) {
		t.Error("expected terminal error")
	}
}
