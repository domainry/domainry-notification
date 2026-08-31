package template

import (
	"encoding/json"
	"strings"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	templatemodel "github.com/domainry/domainry-notification/internal/domain/template/model"
	templaterepository "github.com/domainry/domainry-notification/internal/domain/template/repository"
	templateservice "github.com/domainry/domainry-notification/internal/domain/template/service"
)

type Template = templatemodel.Template
type PublicationRequest = templatemodel.PublicationRequest
type PublicationStatus = templatemodel.PublicationStatus
type PublicationTransition = templatemodel.PublicationTransition
type Store = templaterepository.Store
type Manager = templateservice.Manager

var ErrPublicationConflict = templaterepository.ErrPublicationConflict

const (
	PublicationPending    = templatemodel.PublicationPending
	PublicationScheduled  = templatemodel.PublicationScheduled
	PublicationPublishing = templatemodel.PublicationPublishing
	PublicationPublished  = templatemodel.PublicationPublished
	PublicationRejected   = templatemodel.PublicationRejected
	PublicationCancelled  = templatemodel.PublicationCancelled
	PublicationFailed     = templatemodel.PublicationFailed
	PublicationSuperseded = templatemodel.PublicationSuperseded
)

func cloneTemplate(value Template) Template {
	raw, _ := json.Marshal(value)
	var cloned Template
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}

func invalid(code string, params ...string) error {
	values := map[string]any{}
	for index := 0; index+1 < len(params); index += 2 {
		if key := strings.TrimSpace(params[index]); key != "" {
			values[key] = params[index+1]
		}
	}
	return notification.NewError(notification.ErrorInvalid, code, nil, values)
}
