package layout

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestBase_ModalContainer(t *testing.T) {
	dummyContent := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "<div>Dummy Content</div>")
		return err
	})

	var buf bytes.Buffer
	err := Base("Test Page", dummyContent).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Base.Render failed: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `id="modal-container"`) {
		t.Errorf("expected Base layout to contain id=\"modal-container\", got: %s", html)
	}
}
