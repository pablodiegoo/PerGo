package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/pablojhp.pergo/templates/components"
)

func TestConfirmModal_A11y(t *testing.T) {
	var buf bytes.Buffer
	err := components.ConfirmModal("Excluir Canal", "Deseja realmente remover?", "/admin/devices/123").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("ConfirmModal.Render failed: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `role="dialog"`) {
		t.Errorf("expected ConfirmModal to have role=\"dialog\", got:\n%s", html)
	}
	if !strings.Contains(html, `aria-modal="true"`) {
		t.Errorf("expected ConfirmModal to have aria-modal=\"true\", got:\n%s", html)
	}
	if !strings.Contains(html, `aria-labelledby="confirm-modal-title"`) {
		t.Errorf("expected ConfirmModal to have aria-labelledby=\"confirm-modal-title\", got:\n%s", html)
	}
	if !strings.Contains(html, `id="confirm-modal-title"`) {
		t.Errorf("expected ConfirmModal heading to have id=\"confirm-modal-title\", got:\n%s", html)
	}
}
