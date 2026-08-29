package inboxstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	"github.com/domainry/domainry-orm/sqlhost"
)

var inboxItemReadColumns = []string{"payload_json", "event_id", "occurrence_count", "first_occurred_at", "last_occurred_at", "read_at", "archived_at", "alert_state", "updated_at"}

func (s *Store) ListItems(ctx context.Context, query inbox.Query) ([]inbox.Item, bool, error) {
	query, err := normalizeMailboxStoreQuery(query)
	if err != nil {
		return nil, false, err
	}
	ctx = s.workspaceScope.Context(ctx, query.WorkspaceID)
	where, args := s.mailboxWhere(query, true, 1)
	statement := "SELECT " + s.columns(inboxItemReadColumns) + " FROM " + s.Renderer.Table("notification_inbox_items") + " WHERE " + where +
		" ORDER BY " + s.Renderer.Identifier("updated_at") + " DESC, " + s.Renderer.Identifier("id") + " DESC LIMIT " + fmt.Sprint(query.Limit+1)
	rows, err := s.Database.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, false, fmt.Errorf("list notification inbox items: %w", err)
	}
	defer rows.Close()
	values := []inbox.Item{}
	for rows.Next() {
		value, scanErr := scanInboxItem(rows)
		if scanErr != nil {
			return nil, false, scanErr
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(values) > query.Limit
	if hasMore {
		values = values[:query.Limit]
	}
	return values, hasMore, nil
}

func (s *Store) GetItem(ctx context.Context, query inbox.Query, itemID string) (inbox.Item, bool, error) {
	query, err := normalizeMailboxStoreQuery(query)
	if err != nil {
		return inbox.Item{}, false, err
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return inbox.Item{}, false, fmt.Errorf("notification inbox item id is required")
	}
	ctx = s.workspaceScope.Context(ctx, query.WorkspaceID)
	return s.getInboxItem(ctx, s.Database, query, itemID)
}

func (s *Store) CountFacets(ctx context.Context, query inbox.Query) (inbox.Facets, error) {
	query, err := normalizeMailboxStoreQuery(query)
	if err != nil {
		return inbox.Facets{}, err
	}
	ctx = s.workspaceScope.Context(ctx, query.WorkspaceID)
	where, args := s.mailboxWhere(query, false, 1)
	result := inbox.Facets{}
	if err := s.Database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.Renderer.Table("notification_inbox_items")+" WHERE "+where+" AND "+s.Renderer.Identifier("read_at")+" = ''", args...).Scan(&result.Unread); err != nil {
		return result, fmt.Errorf("count notification inbox unread: %w", err)
	}
	if err := s.Database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.Renderer.Table("notification_inbox_items")+" WHERE "+where+" AND "+s.Renderer.Identifier("action_state")+" = 'open'", args...).Scan(&result.ActionRequired); err != nil {
		return result, fmt.Errorf("count notification inbox actions: %w", err)
	}
	if result.Categories, err = s.mailboxFacetRows(ctx, where, args, "category"); err != nil {
		return result, err
	}
	if result.Sources, err = s.mailboxFacetRows(ctx, where, args, "source"); err != nil {
		return result, err
	}
	if result.Severities, err = s.mailboxFacetRows(ctx, where, args, "severity"); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Store) mailboxFacetRows(ctx context.Context, where string, args []any, column string) ([]inbox.Facet, error) {
	statement := "SELECT " + s.Renderer.Identifier(column) + ", COUNT(*) FROM " + s.Renderer.Table("notification_inbox_items") + " WHERE " + where +
		" GROUP BY " + s.Renderer.Identifier(column) + " ORDER BY COUNT(*) DESC, " + s.Renderer.Identifier(column) + " ASC"
	rows, err := s.Database.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("count notification inbox %s facets: %w", column, err)
	}
	defer rows.Close()
	values := []inbox.Facet{}
	for rows.Next() {
		var value inbox.Facet
		if err := rows.Scan(&value.Key, &value.Count); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) getInboxItem(ctx context.Context, queryer sqlhost.Queryer, query inbox.Query, itemID string) (inbox.Item, bool, error) {
	clauses, args, position := s.mailboxAccessWhere(query, 1)
	clauses = append(clauses, s.Renderer.Identifier("id")+" = "+s.Renderer.Placeholder(position))
	args = append(args, itemID)
	statement := "SELECT " + s.columns(inboxItemReadColumns) + " FROM " + s.Renderer.Table("notification_inbox_items") + " WHERE " + strings.Join(clauses, " AND ")
	value, err := scanInboxItem(queryer.QueryRowContext(ctx, statement, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return inbox.Item{}, false, nil
	}
	return value, err == nil, err
}

func (s *Store) mailboxWhere(query inbox.Query, includeCursor bool, placeholderStart int) (string, []any) {
	clauses, args, position := s.mailboxAccessWhere(query, placeholderStart)
	switch query.Mailbox {
	case inbox.MailboxUnread:
		clauses = append(clauses, s.Renderer.Identifier("archived_at")+" = ''", s.Renderer.Identifier("read_at")+" = ''")
	case inbox.MailboxActionRequired:
		clauses = append(clauses, s.Renderer.Identifier("archived_at")+" = ''", s.Renderer.Identifier("action_state")+" = 'open'")
	case inbox.MailboxArchived:
		clauses = append(clauses, s.Renderer.Identifier("archived_at")+" <> ''")
	default:
		clauses = append(clauses, s.Renderer.Identifier("archived_at")+" = ''")
	}
	if query.Query != "" {
		clauses = append(clauses, "LOWER("+s.Renderer.Identifier("search_text")+") LIKE "+s.Renderer.Placeholder(position))
		args, position = append(args, "%"+strings.ToLower(query.Query)+"%"), position+1
	}
	clauses, args, position = appendStringFilter(s, clauses, args, position, "category", query.Categories)
	clauses, args, position = appendStringFilter(s, clauses, args, position, "source", query.Sources)
	clauses, args, position = appendStringFilter(s, clauses, args, position, "severity", query.Severities)
	actions := make([]string, len(query.ActionStates))
	for index, value := range query.ActionStates {
		actions[index] = string(value)
	}
	clauses, args, position = appendStringFilter(s, clauses, args, position, "action_state", actions)
	if query.From != "" {
		clauses = append(clauses, s.Renderer.Identifier("last_occurred_at")+" >= "+s.Renderer.Placeholder(position))
		args, position = append(args, query.From), position+1
	}
	if query.To != "" {
		clauses = append(clauses, s.Renderer.Identifier("last_occurred_at")+" <= "+s.Renderer.Placeholder(position))
		args, position = append(args, query.To), position+1
	}
	if includeCursor && query.BeforeUpdatedAt != "" && query.BeforeID != "" {
		clauses = append(clauses, "("+s.Renderer.Identifier("updated_at")+" < "+s.Renderer.Placeholder(position)+" OR ("+
			s.Renderer.Identifier("updated_at")+" = "+s.Renderer.Placeholder(position+1)+" AND "+s.Renderer.Identifier("id")+" < "+s.Renderer.Placeholder(position+2)+"))")
		args = append(args, query.BeforeUpdatedAt, query.BeforeUpdatedAt, query.BeforeID)
	}
	return strings.Join(clauses, " AND "), args
}

func (s *Store) mailboxAccessWhere(query inbox.Query, placeholderStart int) ([]string, []any, int) {
	position := placeholderStart
	clauses := []string{
		s.Renderer.Identifier("workspace_id") + " = " + s.Renderer.Placeholder(position),
		s.Renderer.Identifier("surface") + " = " + s.Renderer.Placeholder(position+1),
	}
	args := []any{query.WorkspaceID.String(), string(query.Surface)}
	position += 2
	recipients := make([]string, len(query.RecipientUserIDs))
	for index, recipient := range query.RecipientUserIDs {
		recipients[index] = recipient.String()
	}
	return appendStringFilter(s, clauses, args, position, "recipient_user_id", recipients)
}

func appendStringFilter(s *Store, clauses []string, args []any, position int, column string, values []string) ([]string, []any, int) {
	clean := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			clean = append(clean, value)
		}
	}
	if len(clean) == 0 {
		return clauses, args, position
	}
	placeholders := make([]string, len(clean))
	for index, value := range clean {
		placeholders[index] = s.Renderer.Placeholder(position)
		args, position = append(args, value), position+1
	}
	clauses = append(clauses, s.Renderer.Identifier(column)+" IN ("+strings.Join(placeholders, ", ")+")")
	return clauses, args, position
}

func normalizeMailboxStoreQuery(query inbox.Query) (inbox.Query, error) {
	if query.WorkspaceID == "" || query.Surface == "" || len(query.RecipientUserIDs) == 0 {
		return query, fmt.Errorf("notification mailbox query requires an explicit workspace, surface, and recipient boundary")
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Limit > 100 {
		query.Limit = 100
	}
	return query, nil
}

func scanInboxItem(row scanner) (inbox.Item, error) {
	var value inbox.Item
	var raw, eventID, firstOccurredAt, lastOccurredAt, readAt, archivedAt, alertState, updatedAt string
	var occurrenceCount int
	if err := row.Scan(&raw, &eventID, &occurrenceCount, &firstOccurredAt, &lastOccurredAt, &readAt, &archivedAt, &alertState, &updatedAt); err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return value, fmt.Errorf("decode notification inbox item: %w", err)
	}
	value.EventID, value.OccurrenceCount = eventID, occurrenceCount
	value.FirstOccurredAt, value.LastOccurredAt = firstOccurredAt, lastOccurredAt
	value.ReadAt, value.ArchivedAt, value.AlertState, value.UpdatedAt = readAt, archivedAt, inbox.AlertState(alertState), updatedAt
	return value, nil
}
