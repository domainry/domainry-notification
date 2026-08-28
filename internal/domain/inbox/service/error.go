package inbox

import (
	"strings"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

func invalid(code string, params ...string) error {
	return inboxError(notification.ErrorInvalid, code, nil, params...)
}

func notFound(code string, params ...string) error {
	return inboxError(notification.ErrorNotFound, code, nil, params...)
}

func conflict(code string, params ...string) error {
	return inboxError(notification.ErrorConflict, code, nil, params...)
}

func forbidden(code string, params ...string) error {
	return inboxError(notification.ErrorForbidden, code, nil, params...)
}

func unavailable(code string, cause error, params ...string) error {
	return inboxError(notification.ErrorUnavailable, code, cause, params...)
}

func inboxError(kind notification.ErrorKind, code string, cause error, params ...string) error {
	values := map[string]any{}
	for index := 0; index+1 < len(params); index += 2 {
		if key := strings.TrimSpace(params[index]); key != "" {
			values[key] = params[index+1]
		}
	}
	return notification.NewError(kind, code, cause, values)
}
