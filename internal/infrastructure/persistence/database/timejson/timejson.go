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

// Marshal encodes absolute-instant fields as UTC Unix-millisecond numbers.
// It recognizes typed time.Time values and owned JSON fields whose names carry
// the Notification time contract. json.RawMessage remains an opaque adapter
// payload and is not rewritten.
func Marshal(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	document, err := decodeDocument(raw)
	if err != nil {
		return nil, err
	}
	document, err = encode(reflect.ValueOf(value), document, "")
	if err != nil {
		return nil, err
	}
	return json.Marshal(document)
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

func encode(value reflect.Value, document any, name string) (any, error) {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return document, nil
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return document, nil
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
	switch value.Kind() {
	case reflect.Struct:
		object, ok := document.(map[string]any)
		if !ok {
			return document, nil
		}
		valueType := value.Type()
		for index := 0; index < value.NumField(); index++ {
			field := valueType.Field(index)
			if field.PkgPath != "" {
				continue
			}
			if field.Anonymous && field.Tag.Get("json") == "" {
				normalized, err := encode(value.Field(index), object, "")
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
			normalized, err := encode(value.Field(index), child, fieldName)
			if err != nil {
				return nil, err
			}
			object[fieldName] = normalized
		}
		return object, nil
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return document, nil
		}
		items, ok := document.([]any)
		if !ok {
			return document, nil
		}
		for index := 0; index < value.Len() && index < len(items); index++ {
			normalized, err := encode(value.Index(index), items[index], "")
			if err != nil {
				return nil, err
			}
			items[index] = normalized
		}
		return items, nil
	case reflect.Map:
		object, ok := document.(map[string]any)
		if !ok || value.Type().Key().Kind() != reflect.String {
			return document, nil
		}
		iterator := value.MapRange()
		for iterator.Next() {
			key := iterator.Key().String()
			child, exists := object[key]
			if !exists {
				continue
			}
			normalized, err := encode(iterator.Value(), child, key)
			if err != nil {
				return nil, err
			}
			object[key] = normalized
		}
		return object, nil
	default:
		return document, nil
	}
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
	case reflect.Map:
		object, ok := document.(map[string]any)
		if !ok || target.Key().Kind() != reflect.String {
			return document, nil
		}
		for key, child := range object {
			if instantField(key) {
				if child == nil {
					object[key] = ""
					continue
				}
				value, err := millis(child, key)
				if err != nil {
					return nil, err
				}
				object[key] = time.UnixMilli(value).UTC().Format(timestampLayout)
				continue
			}
			normalized, err := decode(target.Elem(), child, "")
			if err != nil {
				return nil, err
			}
			object[key] = normalized
		}
		return object, nil
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
