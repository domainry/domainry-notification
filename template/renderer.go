package template

import "github.com/domainry/domainry-notification"

type RenderRequest struct {
	WorkspaceID notification.WorkspaceID
	TemplateKey string
	Locale      string
	Recipients  []notification.UserID
	Variables   map[string]any
	Metadata    map[string]any
}
