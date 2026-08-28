package template

import notification "github.com/domainry/domainry-notification/internal/domain/notification/model"

type RenderRequest struct {
	WorkspaceID notification.WorkspaceID
	TemplateKey string
	Locale      string
	Recipients  []notification.UserID
	Variables   map[string]any
	Metadata    map[string]any
}
