package pages_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

func TestDevicesTempl_PairForm_A11yAndLoading(t *testing.T) {
	var buf bytes.Buffer
	err := pages.PairForm().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("PairForm.Render failed: %v", err)
	}

	html := buf.String()

	// A11y dialog semantics
	if !strings.Contains(html, `role="dialog"`) {
		t.Errorf("expected PairForm to have role=\"dialog\", got:\n%s", html)
	}
	if !strings.Contains(html, `aria-modal="true"`) {
		t.Errorf("expected PairForm to have aria-modal=\"true\", got:\n%s", html)
	}
	if !strings.Contains(html, `aria-labelledby="pair-modal-title"`) {
		t.Errorf("expected PairForm to have aria-labelledby=\"pair-modal-title\", got:\n%s", html)
	}
	if !strings.Contains(html, `id="pair-modal-title"`) {
		t.Errorf("expected PairForm to contain id=\"pair-modal-title\", got:\n%s", html)
	}

	// Loading state indicator
	if !strings.Contains(html, `hx-indicator=`) {
		t.Errorf("expected form to have hx-indicator attribute")
	}
	if !strings.Contains(html, `htmx-indicator`) {
		t.Errorf("expected submit button to contain htmx-indicator spinner")
	}
}

func TestDevicesTempl_QRFragment_A11yLiveRegions(t *testing.T) {
	t.Run("pending state has polite live region and alt text", func(t *testing.T) {
		var buf bytes.Buffer
		err := pages.QRFragment("qr-raw-code", "data:image/png;base64,iVBORw0KGgo=", "+5511999990001", "pending", "Aponte a câmera").Render(context.Background(), &buf)
		if err != nil {
			t.Fatalf("QRFragment.Render failed: %v", err)
		}
		html := buf.String()
		if !strings.Contains(html, `role="status"`) {
			t.Errorf("expected pending QRFragment to have role=\"status\"")
		}
		if !strings.Contains(html, `aria-live="polite"`) {
			t.Errorf("expected pending QRFragment to have aria-live=\"polite\"")
		}
		if !strings.Contains(html, `alt="WhatsApp QR Code`) {
			t.Errorf("expected img to have descriptive alt text")
		}
	})

	t.Run("paired state has role status and polite live region", func(t *testing.T) {
		var buf bytes.Buffer
		err := pages.QRFragment("", "", "+5511999990001", "paired", "").Render(context.Background(), &buf)
		if err != nil {
			t.Fatalf("QRFragment.Render failed: %v", err)
		}
		html := buf.String()
		if !strings.Contains(html, `role="status"`) {
			t.Errorf("expected paired QRFragment to have role=\"status\"")
		}
		if !strings.Contains(html, `aria-live="polite"`) {
			t.Errorf("expected paired QRFragment to have aria-live=\"polite\"")
		}
	})

	t.Run("error state has role alert and assertive live region", func(t *testing.T) {
		var buf bytes.Buffer
		err := pages.QRFragment("", "", "+5511999990001", "error", "Tempo limite excedido").Render(context.Background(), &buf)
		if err != nil {
			t.Fatalf("QRFragment.Render failed: %v", err)
		}
		html := buf.String()
		if !strings.Contains(html, `role="alert"`) {
			t.Errorf("expected error QRFragment to have role=\"alert\"")
		}
		if !strings.Contains(html, `aria-live="assertive"`) {
			t.Errorf("expected error QRFragment to have aria-live=\"assertive\"")
		}
	})
}

func TestDevicesTempl_ConnectionRow_SlugA11y(t *testing.T) {
	conn := &repository.Connection{
		ID:             uuid.New(),
		Name:           "Suporte WhatsApp",
		Slug:           "suporte-wa",
		Channel:        "whatsapp",
		Status:         "connected",
		SenderIdentity: "+5511999990001",
	}

	var buf bytes.Buffer
	err := pages.ConnectionRow(conn).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("ConnectionRow.Render failed: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `aria-label=`) {
		t.Errorf("expected slug input to have aria-label attribute for accessibility, got:\n%s", html)
	}
}

func TestDevicesTempl_TestConnectionModal_A11y(t *testing.T) {
	conn := &repository.Connection{
		ID:             uuid.New(),
		Name:           "WABA Oficial",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "+5511999990002",
	}

	var buf bytes.Buffer
	err := pages.TestConnectionModal(conn, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("TestConnectionModal.Render failed: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `role="dialog"`) {
		t.Errorf("expected TestConnectionModal to have role=\"dialog\"")
	}
	if !strings.Contains(html, `aria-modal="true"`) {
		t.Errorf("expected TestConnectionModal to have aria-modal=\"true\"")
	}
	if !strings.Contains(html, `aria-labelledby="test-modal-title"`) {
		t.Errorf("expected TestConnectionModal to have aria-labelledby=\"test-modal-title\"")
	}
	if !strings.Contains(html, `role="log"`) {
		t.Errorf("expected activity feed to have role=\"log\"")
	}
}
