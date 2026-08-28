package sqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/domainry/domainry-notification"
	"github.com/domainry/domainry-notification/delivery"
	"github.com/domainry/domainry-notification/inbox"
)

func (s *Store) PreviewSubject(ctx context.Context, workspaceID, subjectID string) (json.RawMessage, error) {
	if s == nil || s.database == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(subjectID) == "" {
		return nil, fmt.Errorf("notification subject scope is required")
	}
	counts := map[string]int64{}
	for key, tableColumn := range map[string][2]string{
		"inbox_items": {"notification_inbox_items", "recipient_user_id"}, "preferences": {"notification_recipient_preferences", "recipient_key"},
		"delivery_reservations": {"notification_delivery_reservations", "recipient_key"}, "saved_views": {"notification_inbox_saved_views", "recipient_user_id"},
	} {
		query := "SELECT COUNT(*) FROM " + s.dialect.Table(tableColumn[0]) + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier(tableColumn[1]) + " = " + s.dialect.Placeholder(2)
		var count int64
		if err := s.database.QueryRowContext(ctx, query, workspaceID, subjectID).Scan(&count); err != nil {
			return nil, err
		}
		counts[key] = count
	}
	query := "SELECT COUNT(*) FROM " + s.dialect.Table("notification_inbox_delegations") + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND (" + s.dialect.Identifier("owner_user_id") + " = " + s.dialect.Placeholder(2) + " OR " + s.dialect.Identifier("delegate_user_id") + " = " + s.dialect.Placeholder(3) + ")"
	var delegations int64
	if err := s.database.QueryRowContext(ctx, query, workspaceID, subjectID, subjectID).Scan(&delegations); err != nil {
		return nil, err
	}
	counts["delegations"] = delegations
	return json.Marshal(counts)
}

func (s *Store) ExportSubject(ctx context.Context, workspaceID, subjectID string) (json.RawMessage, error) {
	if s == nil || s.database == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(subjectID) == "" {
		return nil, fmt.Errorf("notification subject scope is required")
	}
	columns := []string{"id", "surface", "event_type", "source", "category", "severity", "title", "body", "action_state", "alert_state", "first_occurred_at", "last_occurred_at", "read_at", "archived_at"}
	quoted := make([]string, len(columns))
	for i := range columns {
		quoted[i] = s.dialect.Identifier(columns[i])
	}
	query := "SELECT " + strings.Join(quoted, ", ") + " FROM " + s.dialect.Table("notification_inbox_items") + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("recipient_user_id") + " = " + s.dialect.Placeholder(2) + " ORDER BY " + s.dialect.Identifier("created_at")
	rows, err := s.database.QueryContext(ctx, query, workspaceID, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		values := make([]string, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		item := make(map[string]string, len(columns))
		for i := range columns {
			item[columns[i]] = values[i]
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"inbox_items": items})
}

func (s *Store) EraseSubject(ctx context.Context, workspaceID, subjectID string, _ json.RawMessage) (json.RawMessage, error) {
	if s == nil || s.database == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(subjectID) == "" {
		return nil, fmt.Errorf("notification subject scope is required")
	}
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	anonymous := anonymousSubject(workspaceID, subjectID)
	changed := map[string]int64{}
	if changed["inbox_items"], err = s.anonymizeInboxItems(ctx, tx, workspaceID, subjectID, anonymous); err != nil {
		return nil, err
	}
	if changed["events"], err = s.rewritePayloads(ctx, tx, "notification_events", workspaceID, func(raw []byte) ([]byte, bool, error) {
		var event inbox.Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, false, err
		}
		if !replaceRecipients(event.RecipientUserIDs, subjectID, anonymous) {
			return raw, false, nil
		}
		event.Snapshot.Title, event.Snapshot.Body, event.Snapshot.Facts, event.Snapshot.Actions = "[erased]", "[erased]", nil, nil
		event.LocalizedSnapshots, event.CorrelationID, event.TraceID = nil, "", ""
		for i := range event.ChannelPlans {
			replaceRecipients(event.ChannelPlans[i].RecipientUserIDs, subjectID, anonymous)
			event.ChannelPlans[i].Variables = nil
		}
		value, marshalErr := json.Marshal(event)
		return value, true, marshalErr
	}); err != nil {
		return nil, err
	}
	if changed["channel_plans"], err = s.rewritePayloads(ctx, tx, "notification_channel_plans", workspaceID, func(raw []byte) ([]byte, bool, error) {
		var plan delivery.Plan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return nil, false, err
		}
		if !replaceRecipients(plan.RecipientUserIDs, subjectID, anonymous) {
			return raw, false, nil
		}
		plan.Variables, plan.DigestItemTitle, plan.DigestItemBody = nil, "[erased]", "[erased]"
		value, marshalErr := json.Marshal(plan)
		return value, true, marshalErr
	}); err != nil {
		return nil, err
	}
	for _, update := range []struct{ key, table, column string }{{"alert_groups", "notification_alert_groups", "recipient_user_id"}, {"delivery_reservations", "notification_delivery_reservations", "recipient_key"}} {
		query := "UPDATE " + s.dialect.Table(update.table) + " SET " + s.dialect.Identifier(update.column) + " = " + s.dialect.Placeholder(1) + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(2) + " AND " + s.dialect.Identifier(update.column) + " = " + s.dialect.Placeholder(3)
		result, updateErr := tx.ExecContext(ctx, query, anonymous, workspaceID, subjectID)
		if updateErr != nil {
			return nil, updateErr
		}
		changed[update.key], _ = result.RowsAffected()
	}
	result, err := tx.ExecContext(ctx, "UPDATE "+s.dialect.Table("notification_alert_groups")+" SET "+s.dialect.Identifier("acknowledged_by")+" = "+s.dialect.Placeholder(1)+" WHERE "+s.dialect.Identifier("workspace_id")+" = "+s.dialect.Placeholder(2)+" AND "+s.dialect.Identifier("acknowledged_by")+" = "+s.dialect.Placeholder(3), anonymous, workspaceID, subjectID)
	if err != nil {
		return nil, err
	}
	changed["alert_acknowledgements"], _ = result.RowsAffected()
	for key, tableColumn := range map[string][2]string{"preferences": {"notification_recipient_preferences", "recipient_key"}, "saved_views": {"notification_inbox_saved_views", "recipient_user_id"}, "delegations": {"notification_inbox_delegations", "owner_user_id"}} {
		condition := s.dialect.Identifier(tableColumn[1]) + " = " + s.dialect.Placeholder(2)
		args := []any{workspaceID, subjectID}
		if key == "delegations" {
			condition = "(" + condition + " OR " + s.dialect.Identifier("delegate_user_id") + " = " + s.dialect.Placeholder(3) + ")"
			args = append(args, subjectID)
		}
		result, deleteErr := tx.ExecContext(ctx, "DELETE FROM "+s.dialect.Table(tableColumn[0])+" WHERE "+s.dialect.Identifier("workspace_id")+" = "+s.dialect.Placeholder(1)+" AND "+condition, args...)
		if deleteErr != nil {
			return nil, deleteErr
		}
		changed[key], _ = result.RowsAffected()
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"anonymous_subject": anonymous, "changed": changed, "content_redacted": true, "at": time.Now().UTC()})
}

func (s *Store) anonymizeInboxItems(ctx context.Context, tx *sql.Tx, workspaceID, subjectID, anonymous string) (int64, error) {
	query := "SELECT " + s.dialect.Identifier("id") + ", " + s.dialect.Identifier("payload_json") + " FROM " + s.dialect.Table("notification_inbox_items") + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("recipient_user_id") + " = " + s.dialect.Placeholder(2)
	rows, err := tx.QueryContext(ctx, query, workspaceID, subjectID)
	if err != nil {
		return 0, err
	}
	values := [][2]string{}
	for rows.Next() {
		var value [2]string
		if err := rows.Scan(&value[0], &value[1]); err != nil {
			rows.Close()
			return 0, err
		}
		values = append(values, value)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, value := range values {
		var item inbox.Item
		if err := json.Unmarshal([]byte(value[1]), &item); err != nil {
			return 0, err
		}
		item.RecipientUserID, item.Title, item.Body, item.Facts, item.Actions = notification.UserID(anonymous), "[erased]", "[erased]", nil, nil
		item.SubjectID, item.SubjectVersion, item.TemplateContentHash = "", "", ""
		raw, _ := json.Marshal(item)
		statement := "UPDATE " + s.dialect.Table("notification_inbox_items") + " SET " + s.dialect.Identifier("recipient_user_id") + " = " + s.dialect.Placeholder(1) + ", " + s.dialect.Identifier("title") + " = '[erased]', " + s.dialect.Identifier("body") + " = '[erased]', " + s.dialect.Identifier("search_text") + " = '', " + s.dialect.Identifier("payload_json") + " = " + s.dialect.Placeholder(2) + ", " + s.dialect.Identifier("subject_id") + " = '' WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(3) + " AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(4)
		if _, err := tx.ExecContext(ctx, statement, anonymous, string(raw), workspaceID, value[0]); err != nil {
			return 0, err
		}
	}
	return int64(len(values)), nil
}

func (s *Store) rewritePayloads(ctx context.Context, tx *sql.Tx, table, workspaceID string, rewrite func([]byte) ([]byte, bool, error)) (int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+s.dialect.Identifier("id")+", "+s.dialect.Identifier("payload_json")+" FROM "+s.dialect.Table(table)+" WHERE "+s.dialect.Identifier("workspace_id")+" = "+s.dialect.Placeholder(1), workspaceID)
	if err != nil {
		return 0, err
	}
	values := [][2]string{}
	for rows.Next() {
		var value [2]string
		if err := rows.Scan(&value[0], &value[1]); err != nil {
			rows.Close()
			return 0, err
		}
		values = append(values, value)
	}
	rows.Close()
	var changed int64
	for _, value := range values {
		raw, matched, rewriteErr := rewrite([]byte(value[1]))
		if rewriteErr != nil {
			return changed, rewriteErr
		}
		if matched {
			if _, err := tx.ExecContext(ctx, "UPDATE "+s.dialect.Table(table)+" SET "+s.dialect.Identifier("payload_json")+" = "+s.dialect.Placeholder(1)+" WHERE "+s.dialect.Identifier("workspace_id")+" = "+s.dialect.Placeholder(2)+" AND "+s.dialect.Identifier("id")+" = "+s.dialect.Placeholder(3), string(raw), workspaceID, value[0]); err != nil {
				return changed, err
			}
			changed++
		}
	}
	return changed, nil
}

func anonymousSubject(workspaceID, subjectID string) string {
	sum := sha256.Sum256([]byte(workspaceID + "\x00" + subjectID))
	return "erased-" + hex.EncodeToString(sum[:12])
}

func replaceRecipients(values []notification.UserID, subjectID, anonymous string) bool {
	matched := false
	for i := range values {
		if values[i].String() == subjectID {
			values[i], matched = notification.UserID(anonymous), true
		}
	}
	return matched
}
