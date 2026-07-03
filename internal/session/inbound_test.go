package session_test

import (
	"testing"

	"github.com/pablojhp.pergo/internal/session"
)

func TestWhatsAppInbound(t *testing.T) {
	m := session.NewManager(
		nil,
		nil,
		nil,
		nil,
		"2.3000.1025000000",
		nil,
	)

	if m == nil {
		t.Fatal("expected manager")
	}
}
