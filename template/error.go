package template

import (
	"strings"

	"github.com/domainry/domainry-notification"
)

func invalid(code string, params ...string) error {
	values := map[string]any{}
	for index := 0; index+1 < len(params); index += 2 {
		if key := strings.TrimSpace(params[index]); key != "" {
			values[key] = params[index+1]
		}
	}
	return notification.NewError(notification.ErrorInvalid, code, nil, values)
}
