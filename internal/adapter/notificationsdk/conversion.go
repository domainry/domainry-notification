// Package notificationsdk converts between Notification-owned domain values
// and the public SDK wire contract. Conversion belongs at adapter boundaries,
// never in domain packages.
package notificationsdk

import "encoding/json"

func Convert[To any, From any](value From) (To, error) {
	var result To
	encoded, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return result, err
	}
	return result, nil
}

func ConvertSlice[To any, From any](values []From) ([]To, error) {
	result := make([]To, len(values))
	for index := range values {
		value, err := Convert[To](values[index])
		if err != nil {
			return nil, err
		}
		result[index] = value
	}
	return result, nil
}
