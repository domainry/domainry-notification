package lifecyclestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/query"
)

func (s *Store) PreviewSubject(ctx context.Context, workspaceID, subjectID string) (json.RawMessage, error) {
	if s == nil || s.Database == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(subjectID) == "" {
		return nil, fmt.Errorf("notification subject scope is required")
	}
	counts := map[string]int64{}
	for key, tableColumn := range map[string][2]string{
		"inbox_items": {"_notification_inbox_items", "recipient_user_id"}, "preferences": {"_notification_recipient_preferences", "recipient_key"},
		"delivery_reservations": {"_notification_delivery_reservations", "recipient_key"}, "saved_views": {"_notification_inbox_saved_views", "recipient_user_id"},
	} {
		queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, tableColumn[0], workspaceID).Projections(query.Project(query.CountAll())).Where(query.Equal(tableColumn[1], subjectID)).Build()
		if err != nil {
			return nil, err
		}
		var count int64
		if err := s.Database.QueryRowContext(ctx, queryValue, args...).Scan(&count); err != nil {
			return nil, err
		}
		counts[key] = count
	}
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_delegations", workspaceID).Projections(query.Project(query.CountAll())).Where(query.Or(query.Equal("owner_user_id", subjectID), query.Equal("delegate_user_id", subjectID))).Build()
	if err != nil {
		return nil, err
	}
	var delegations int64
	if err := s.Database.QueryRowContext(ctx, queryValue, args...).Scan(&delegations); err != nil {
		return nil, err
	}
	counts["delegations"] = delegations
	return json.Marshal(counts)
}

func (s *Store) ExportSubject(ctx context.Context, workspaceID, subjectID string) (json.RawMessage, error) {
	if s == nil || s.Database == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(subjectID) == "" {
		return nil, fmt.Errorf("notification subject scope is required")
	}
	columns := []string{"id", "surface", "event_type", "source", "category", "severity", "title", "body", "action_state", "alert_state", "first_occurred_at", "last_occurred_at", "read_at", "archived_at"}
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_items", workspaceID).Columns(columns...).Where(query.Equal("recipient_user_id", subjectID)).OrderBy(query.Ascending("created_at")).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, queryValue, args...)
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
	if s == nil || s.Database == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(subjectID) == "" {
		return nil, fmt.Errorf("notification subject scope is required")
	}
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	anonymous := anonymousSubject(workspaceID, subjectID)
	changed := map[string]int64{}
	if changed["inbox_items"], err = s.anonymizeInboxItems(ctx, tx, workspaceID, subjectID, anonymous); err != nil {
		return nil, err
	}
	if changed["events"], err = s.rewritePayloads(ctx, tx, "_notification_events", workspaceID, func(raw []byte) ([]byte, bool, error) {
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
	if changed["channel_plans"], err = s.rewritePayloads(ctx, tx, "_notification_channel_plans", workspaceID, func(raw []byte) ([]byte, bool, error) {
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
	for _, update := range []struct{ key, table, column string }{{"alert_groups", "_notification_alert_groups", "recipient_user_id"}, {"delivery_reservations", "_notification_delivery_reservations", "recipient_key"}} {
		queryValue, args, buildErr := query.NewWorkspaceUpdateBuilder(s.Renderer, update.table, workspaceID).Set(update.column, anonymous).Where(query.Equal(update.column, subjectID)).Build()
		if buildErr != nil {
			return nil, buildErr
		}
		result, updateErr := tx.ExecContext(ctx, queryValue, args...)
		if updateErr != nil {
			return nil, updateErr
		}
		changed[update.key], _ = result.RowsAffected()
	}
	queryValue, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_alert_groups", workspaceID).Set("acknowledged_by", anonymous).Where(query.Equal("acknowledged_by", subjectID)).Build()
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return nil, err
	}
	changed["alert_acknowledgements"], _ = result.RowsAffected()
	for key, tableColumn := range map[string][2]string{"preferences": {"_notification_recipient_preferences", "recipient_key"}, "saved_views": {"_notification_inbox_saved_views", "recipient_user_id"}, "delegations": {"_notification_inbox_delegations", "owner_user_id"}} {
		predicate := query.Predicate(query.Equal(tableColumn[1], subjectID))
		if key == "delegations" {
			predicate = query.Or(predicate, query.Equal("delegate_user_id", subjectID))
		}
		statement, deleteArgs, buildErr := query.NewWorkspaceDeleteBuilder(s.Renderer, tableColumn[0], workspaceID).Where(predicate).Build()
		if buildErr != nil {
			return nil, buildErr
		}
		result, deleteErr := tx.ExecContext(ctx, statement, deleteArgs...)
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
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_items", workspaceID).Columns("id", "payload_json").Where(query.Equal("recipient_user_id", subjectID)).Build()
	if err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, queryValue, args...)
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
		statement, updateArgs, err := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_inbox_items", workspaceID).Set("recipient_user_id", anonymous).Set("title", "[erased]").Set("body", "[erased]").Set("search_text", "").Set("payload_json", string(raw)).Set("subject_id", "").Where(query.Equal("id", value[0])).Build()
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, statement, updateArgs...); err != nil {
			return 0, err
		}
	}
	return int64(len(values)), nil
}

func (s *Store) rewritePayloads(ctx context.Context, tx *sql.Tx, table, workspaceID string, rewrite func([]byte) ([]byte, bool, error)) (int64, error) {
	statement, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, table, workspaceID).Columns("id", "payload_json").Build()
	if err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, statement, args...)
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
			statement, updateArgs, err := query.NewWorkspaceUpdateBuilder(s.Renderer, table, workspaceID).Set("payload_json", string(raw)).Where(query.Equal("id", value[0])).Build()
			if err != nil {
				return changed, err
			}
			if _, err := tx.ExecContext(ctx, statement, updateArgs...); err != nil {
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
