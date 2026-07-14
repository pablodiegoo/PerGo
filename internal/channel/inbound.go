package channel

import (
	"context"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
)

// InboundAdapter defines the seam for parsing and translating channel-specific 
// inbound webhook payloads into a unified InboundEvent.
type InboundAdapter interface {
	Parse(ctx context.Context, payload []byte, headers map[string]string, conn *repository.Connection) ([]*inbound.InboundEvent, error)
}
