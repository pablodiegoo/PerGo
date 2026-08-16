package components_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/components"
)

func TestNewChatModal_TemplateAttributes(t *testing.T) {
	templates := []repository.WABATemplate{
		{
			ID:       uuid.New(),
			Name:     "order_update",
			Language: "pt_BR",
			Components: json.RawMessage(`[
				{"type":"BODY","text":"Olá {{1}}, seu pedido {{2}} foi atualizado!"}
			]`),
		},
		{
			ID:       uuid.New(),
			Name:     "welcome_static",
			Language: "en_US",
			Components: json.RawMessage(`[
				{"type":"BODY","text":"Welcome to our support team!"}
			]`),
		},
	}

	t.Run("renders template options with data-language and data-components", func(t *testing.T) {
		var buf bytes.Buffer
		err := components.NewChatModal(templates, "+5511999990001", true, "whatsapp_cloud", "+5511999990000").Render(context.Background(), &buf)
		if err != nil {
			t.Fatalf("NewChatModal.Render failed: %v", err)
		}

		html := buf.String()
		if !strings.Contains(html, `data-language="pt_BR"`) {
			t.Errorf("expected option to contain data-language=\"pt_BR\", got:\n%s", html)
		}
		if !strings.Contains(html, `data-language="en_US"`) {
			t.Errorf("expected option to contain data-language=\"en_US\", got:\n%s", html)
		}
		if !strings.Contains(html, `data-components=`) {
			t.Errorf("expected option to contain data-components attribute")
		}
		if !strings.Contains(html, `name="language"`) {
			t.Errorf("expected form to contain input for name=\"language\"")
		}
	})

	t.Run("renders dynamic template fields container for Novo Chat", func(t *testing.T) {
		var buf bytes.Buffer
		err := components.NewChatModal(templates, "", false, "whatsapp", "").Render(context.Background(), &buf)
		if err != nil {
			t.Fatalf("NewChatModal.Render failed: %v", err)
		}

		html := buf.String()
		if !strings.Contains(html, `data-language="pt_BR"`) {
			t.Errorf("expected option to contain data-language=\"pt_BR\"")
		}
		if !strings.Contains(html, `name="language"`) {
			t.Errorf("expected form to contain input for name=\"language\"")
		}
	})
}
