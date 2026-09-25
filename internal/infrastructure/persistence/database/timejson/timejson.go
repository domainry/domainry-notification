// Package timejson owns the durable JSON representation of Notification state.
package timejson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

var timeType = reflect.TypeOf(time.Time{})

const timestampLayout = "2006-01-02T15:04:05.000000000Z"

// Marshal projects declared Notification fields before JSON serialization.
// Maps and raw messages belong to their producers and are never inspected for
// time-like keys.
func Marshal(value any) ([]byte, error) {
	projected, err := encode(reflect.ValueOf(value), "")
	if err != nil {
		return nil, err
	}
	return json.Marshal(projected)
}

// Unmarshal is intentionally strict: owned instant fields accept only integer
// Unix milliseconds (or null for an absent optional value), never RFC3339 text.
func Unmarshal(raw []byte, destination any) error {
	if destination == nil || reflect.TypeOf(destination).Kind() != reflect.Pointer {
		return fmt.Errorf("durable Notification JSON destination must be a pointer")
	}
	document, err := decodeDocument(raw)
	if err != nil {
		return err
	}
	document, err = decode(reflect.TypeOf(destination).Elem(), document, "")
	if err != nil {
		return err
	}
	normalized, err := json.Marshal(document)
	if err != nil {
		return err
	}
	return json.Unmarshal(normalized, destination)
}

func decodeDocument(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	return document, nil
}

func encode(value reflect.Value, name string) (any, error) {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return nil, nil
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return nil, nil
	}
	if value.Type() == timeType {
		instant := value.Interface().(time.Time)
		if instant.IsZero() {
			return nil, nil
		}
		return instant.UTC().UnixMilli(), nil
	}
	if instantField(name) && value.Kind() == reflect.String {
		text := strings.TrimSpace(value.String())
		if text == "" {
			return nil, nil
		}
		instant, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return nil, fmt.Errorf("durable Notification time field %s is invalid: %w", name, err)
		}
		return instant.UTC().UnixMilli(), nil
	}
	if value.Type().Implements(reflect.TypeOf((*json.Marshaler)(nil)).Elem()) {
		return value.Interface(), nil
	}
	switch value.Kind() {
	case reflect.Struct:
		object := make(map[string]any)
		valueType := value.Type()
		for index := 0; index < value.NumField(); index++ {
			field := valueType.Field(index)
			if field.PkgPath != "" {
				continue
			}
			if field.Anonymous && field.Tag.Get("json") == "" {
				normalized, err := encode(value.Field(index), "")
				if err != nil {
					return nil, err
				}
				if embedded, ok := normalized.(map[string]any); ok {
					for key, child := range embedded {
						object[key] = child
					}
				}
				continue
			}
			fieldName, included := fieldName(field)
			if !included || omitEmpty(field, value.Field(index)) {
				continue
			}
			normalized, err := encode(value.Field(index), fieldName)
			if err != nil {
				return nil, err
			}
			object[fieldName] = normalized
		}
		return object, nil
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return value.Interface(), nil
		}
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil, nil
		}
		items := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			normalized, err := encode(value.Index(index), "")
			if err != nil {
				return nil, err
			}
			items[index] = normalized
		}
		return items, nil
	default:
		return value.Interface(), nil
	}
}

func omitEmpty(field reflect.StructField, value reflect.Value) bool {
	for _, option := range strings.Split(field.Tag.Get("json"), ",")[1:] {
		if option == "omitempty" || option == "omitzero" {
			switch value.Kind() {
			case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
				return value.Len() == 0
			default:
				return value.IsZero()
			}
		}
	}
	return false
}

func decode(target reflect.Type, document any, name string) (any, error) {
	for target.Kind() == reflect.Pointer {
		if document == nil {
			return nil, nil
		}
		target = target.Elem()
	}
	if target == timeType {
		millis, err := millis(document, name)
		if err != nil {
			return nil, err
		}
		if millis == 0 {
			return "0001-01-01T00:00:00Z", nil
		}
		return time.UnixMilli(millis).UTC().Format(timestampLayout), nil
	}
	if instantField(name) && target.Kind() == reflect.String {
		if document == nil {
			return "", nil
		}
		value, err := millis(document, name)
		if err != nil {
			return nil, err
		}
		if value == 0 {
			return "", nil
		}
		return time.UnixMilli(value).UTC().Format(timestampLayout), nil
	}
	switch target.Kind() {
	case reflect.Struct:
		object, ok := document.(map[string]any)
		if !ok {
			return document, nil
		}
		for index := 0; index < target.NumField(); index++ {
			field := target.Field(index)
			if field.PkgPath != "" {
				continue
			}
			if field.Anonymous && field.Tag.Get("json") == "" {
				normalized, err := decode(field.Type, object, "")
				if err != nil {
					return nil, err
				}
				if updated, ok := normalized.(map[string]any); ok {
					object = updated
				}
				continue
			}
			fieldName, included := fieldName(field)
			if !included {
				continue
			}
			child, exists := object[fieldName]
			if !exists {
				continue
			}
			normalized, err := decode(field.Type, child, fieldName)
			if err != nil {
				return nil, fmt.Errorf("decode durable Notification JSON field %s: %w", fieldName, err)
			}
			object[fieldName] = normalized
		}
		return object, nil
	case reflect.Slice, reflect.Array:
		if target.Elem().Kind() == reflect.Uint8 {
			return document, nil
		}
		items, ok := document.([]any)
		if !ok {
			return document, nil
		}
		for index := range items {
			normalized, err := decode(target.Elem(), items[index], "")
			if err != nil {
				return nil, err
			}
			items[index] = normalized
		}
		return items, nil
	default:
		return document, nil
	}
}

func millis(document any, name string) (int64, error) {
	number, ok := document.(json.Number)
	if !ok {
		return 0, fmt.Errorf("durable Notification time field %s must be a Unix-millisecond number", name)
	}
	value, err := number.Int64()
	if err != nil {
		return 0, fmt.Errorf("durable Notification time field %s must be an integer: %w", name, err)
	}
	return value, nil
}

func instantField(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	return strings.HasSuffix(name, "_at") || strings.HasSuffix(name, "_timestamp") || name == "timestamp" || name == "scheduled_for" || name == "deliver_after"
}

func fieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		name = field.Name
	}
	return name, true
}
