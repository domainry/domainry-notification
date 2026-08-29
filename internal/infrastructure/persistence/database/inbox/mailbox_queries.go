package inboxstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	"github.com/domainry/domainry-orm/builder"
	"github.com/domainry/domainry-orm/sqlhost"
)

var inboxItemReadColumns = []string{"payload_json", "event_id", "occurrence_count", "first_occurred_at", "last_occurred_at", "read_at", "archived_at", "alert_state", "updated_at"}

func (s *Store) ListItems(ctx context.Context, query inbox.Query) ([]inbox.Item, bool, error) {
	query, err := normalizeMailboxStoreQuery(query)
	if err != nil {
		return nil, false, err
	}
	ctx = s.workspaceScope.Context(ctx, query.WorkspaceID)
	statement, args, err := builder.NewWorkspaceSelectBuilder(s.Renderer, "notification_inbox_items", query.WorkspaceID.String()).Columns(inboxItemReadColumns...).Where(mailboxPredicate(query, true)).OrderBy(builder.Descending("updated_at"), builder.Descending("id")).Limit(query.Limit + 1).Build()
	if err != nil {
		return nil, false, err
	}
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
	predicate := mailboxPredicate(query, false)
	result := inbox.Facets{}
	unreadSQL, unreadArgs, buildErr := builder.NewWorkspaceSelectBuilder(s.Renderer, "notification_inbox_items", query.WorkspaceID.String()).Projections(builder.Project(builder.CountAll())).Where(builder.And(predicate, builder.Equal("read_at", ""))).Build()
	if buildErr != nil {
		return result, buildErr
	}
	if err := s.Database.QueryRowContext(ctx, unreadSQL, unreadArgs...).Scan(&result.Unread); err != nil {
		return result, fmt.Errorf("count notification inbox unread: %w", err)
	}
	actionSQL, actionArgs, buildErr := builder.NewWorkspaceSelectBuilder(s.Renderer, "notification_inbox_items", query.WorkspaceID.String()).Projections(builder.Project(builder.CountAll())).Where(builder.And(predicate, builder.Equal("action_state", "open"))).Build()
	if buildErr != nil {
		return result, buildErr
	}
	if err := s.Database.QueryRowContext(ctx, actionSQL, actionArgs...).Scan(&result.ActionRequired); err != nil {
		return result, fmt.Errorf("count notification inbox actions: %w", err)
	}
	if result.Categories, err = s.mailboxFacetRows(ctx, query.WorkspaceID.String(), predicate, "category"); err != nil {
		return result, err
	}
	if result.Sources, err = s.mailboxFacetRows(ctx, query.WorkspaceID.String(), predicate, "source"); err != nil {
		return result, err
	}
	if result.Severities, err = s.mailboxFacetRows(ctx, query.WorkspaceID.String(), predicate, "severity"); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Store) mailboxFacetRows(ctx context.Context, workspaceID string, predicate builder.Predicate, column string) ([]inbox.Facet, error) {
	statement, args, err := builder.NewWorkspaceSelectBuilder(s.Renderer, "notification_inbox_items", workspaceID).Projections(builder.Project(builder.Column(column)), builder.Project(builder.CountAll())).Where(predicate).GroupBy(builder.Column(column)).OrderBy(builder.DescendingExpression(builder.CountAll()), builder.Ascending(column)).Build()
	if err != nil {
		return nil, err
	}
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
	statement, args, err := builder.NewWorkspaceSelectBuilder(s.Renderer, "notification_inbox_items", query.WorkspaceID.String()).Columns(inboxItemReadColumns...).Where(builder.And(mailboxAccessPredicate(query), builder.Equal("id", itemID))).Build()
	if err != nil {
		return inbox.Item{}, false, err
	}
	value, err := scanInboxItem(queryer.QueryRowContext(ctx, statement, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return inbox.Item{}, false, nil
	}
	return value, err == nil, err
}

func mailboxPredicate(query inbox.Query, includeCursor bool) builder.Predicate {
	predicates := []builder.Predicate{mailboxAccessPredicate(query)}
	switch query.Mailbox {
	case inbox.MailboxUnread:
		predicates = append(predicates, builder.Equal("archived_at", ""), builder.Equal("read_at", ""))
	case inbox.MailboxActionRequired:
		predicates = append(predicates, builder.Equal("archived_at", ""), builder.Equal("action_state", "open"))
	case inbox.MailboxArchived:
		predicates = append(predicates, builder.NotEqual("archived_at", ""))
	default:
		predicates = append(predicates, builder.Equal("archived_at", ""))
	}
	if query.Query != "" {
		predicates = append(predicates, builder.LikeValue(builder.Lower(builder.Column("search_text")), "%"+strings.ToLower(query.Query)+"%"))
	}
	predicates = appendStringPredicate(predicates, "category", query.Categories)
	predicates = appendStringPredicate(predicates, "source", query.Sources)
	predicates = appendStringPredicate(predicates, "severity", query.Severities)
	actions := make([]string, len(query.ActionStates))
	for index, value := range query.ActionStates {
		actions[index] = string(value)
	}
	predicates = appendStringPredicate(predicates, "action_state", actions)
	if query.From != "" {
		predicates = append(predicates, builder.GreaterThanOrEqual("last_occurred_at", query.From))
	}
	if query.To != "" {
		predicates = append(predicates, builder.LessThanOrEqual("last_occurred_at", query.To))
	}
	if includeCursor && query.BeforeUpdatedAt != "" && query.BeforeID != "" {
		predicates = append(predicates, builder.Or(builder.LessThan("updated_at", query.BeforeUpdatedAt), builder.And(builder.Equal("updated_at", query.BeforeUpdatedAt), builder.LessThan("id", query.BeforeID))))
	}
	return builder.And(predicates...)
}

func mailboxAccessPredicate(query inbox.Query) builder.Predicate {
	predicates := []builder.Predicate{builder.Equal("workspace_id", query.WorkspaceID.String()), builder.Equal("surface", string(query.Surface))}
	recipients := make([]string, len(query.RecipientUserIDs))
	for index, recipient := range query.RecipientUserIDs {
		recipients[index] = recipient.String()
	}
	return builder.And(appendStringPredicate(predicates, "recipient_user_id", recipients)...)
}

func appendStringPredicate(predicates []builder.Predicate, column string, values []string) []builder.Predicate {
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
		return predicates
	}
	items := make([]any, len(clean))
	for index, value := range clean {
		items[index] = value
	}
	return append(predicates, builder.In(column, items...))
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
