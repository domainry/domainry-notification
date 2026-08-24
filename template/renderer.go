package template

import (
	"context"

	"github.com/domainry/domainry-notification"
)

type RenderRequest struct {
	WorkspaceID notification.WorkspaceID
	TemplateKey string
	Locale      string
	Recipients  []notification.UserID
	Variables   map[string]any
	Metadata    map[string]any
}

// Renderer compiles a published template into an immutable delivery snapshot.
type Renderer interface {
	Render(context.Context, RenderRequest) (Rendered, error)
}
