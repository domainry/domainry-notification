package inboxstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	"github.com/domainry/domainry-orm/query"
	"github.com/domainry/domainry-orm/sqlhost"
)

var inboxItemReadColumns = []string{"payload_json", "event_id", "occurrence_count", "first_occurred_at", "last_occurred_at", "read_at", "archived_at", "alert_state", "updated_at"}

func (s *Store) ListItems(ctx context.Context, queryValue inbox.Query) ([]inbox.Item, bool, error) {
	queryValue, err := normalizeMailboxStoreQuery(queryValue)
	if err != nil {
		return nil, false, err
	}
	if err := s.requireWorkspace(queryValue.WorkspaceID); err != nil {
		return nil, false, err
	}
	ctx = s.workspaceScope.Context(ctx, queryValue.WorkspaceID)
	predicate := mailboxPredicate(queryValue, true)
	statement, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_items", queryValue.WorkspaceID.String()).Columns(inboxItemReadColumns...).Where(predicate).OrderBy(query.Descending("updated_at"), query.Descending("id")).Limit(queryValue.Limit + 1).Build()
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
	hasMore := len(values) > queryValue.Limit
	if hasMore {
		values = values[:queryValue.Limit]
	}
	return values, hasMore, nil
}

func (s *Store) GetItem(ctx context.Context, queryValue inbox.Query, itemID string) (inbox.Item, bool, error) {
	queryValue, err := normalizeMailboxStoreQuery(queryValue)
	if err != nil {
		return inbox.Item{}, false, err
	}
	if err := s.requireWorkspace(queryValue.WorkspaceID); err != nil {
		return inbox.Item{}, false, err
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return inbox.Item{}, false, fmt.Errorf("notification inbox item id is required")
	}
	ctx = s.workspaceScope.Context(ctx, queryValue.WorkspaceID)
	return s.getInboxItem(ctx, s.Database, queryValue, itemID)
}

func (s *Store) CountFacets(ctx context.Context, queryValue inbox.Query) (inbox.Facets, error) {
	queryValue, err := normalizeMailboxStoreQuery(queryValue)
	if err != nil {
		return inbox.Facets{}, err
	}
	if err := s.requireWorkspace(queryValue.WorkspaceID); err != nil {
		return inbox.Facets{}, err
	}
	ctx = s.workspaceScope.Context(ctx, queryValue.WorkspaceID)
	predicate := mailboxPredicate(queryValue, false)
	result := inbox.Facets{}
	unreadSQL, unreadArgs, buildErr := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_items", queryValue.WorkspaceID.String()).Projections(query.Project(query.CountAll())).Where(query.And(predicate, query.Equal("read_at", ""))).Build()
	if buildErr != nil {
		return result, buildErr
	}
	if err := s.Database.QueryRowContext(ctx, unreadSQL, unreadArgs...).Scan(&result.Unread); err != nil {
		return result, fmt.Errorf("count notification inbox unread: %w", err)
	}
	actionSQL, actionArgs, buildErr := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_items", queryValue.WorkspaceID.String()).Projections(query.Project(query.CountAll())).Where(query.And(predicate, query.Equal("action_state", "open"))).Build()
	if buildErr != nil {
		return result, buildErr
	}
	if err := s.Database.QueryRowContext(ctx, actionSQL, actionArgs...).Scan(&result.ActionRequired); err != nil {
		return result, fmt.Errorf("count notification inbox actions: %w", err)
	}
	if result.Categories, err = s.mailboxFacetRows(ctx, queryValue.WorkspaceID.String(), predicate, "category"); err != nil {
		return result, err
	}
	if result.Sources, err = s.mailboxFacetRows(ctx, queryValue.WorkspaceID.String(), predicate, "source"); err != nil {
		return result, err
	}
	if result.Severities, err = s.mailboxFacetRows(ctx, queryValue.WorkspaceID.String(), predicate, "severity"); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Store) mailboxFacetRows(ctx context.Context, workspaceID string, predicate query.Predicate, column string) ([]inbox.Facet, error) {
	statement, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_items", workspaceID).Projections(query.Project(query.Column(column)), query.Project(query.CountAll())).Where(predicate).GroupBy(query.Column(column)).OrderBy(query.DescendingExpression(query.CountAll()), query.Ascending(column)).Build()
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

func (s *Store) getInboxItem(ctx context.Context, queryer sqlhost.Queryer, queryValue inbox.Query, itemID string) (inbox.Item, bool, error) {
	predicate := query.And(mailboxAccessPredicate(queryValue), query.Equal("id", itemID))
	statement, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_items", queryValue.WorkspaceID.String()).Columns(inboxItemReadColumns...).Where(predicate).Build()
	if err != nil {
		return inbox.Item{}, false, err
	}
	value, err := scanInboxItem(queryer.QueryRowContext(ctx, statement, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return inbox.Item{}, false, nil
	}
	return value, err == nil, err
}

func mailboxPredicate(queryValue inbox.Query, includeCursor bool) query.Predicate {
	predicates := []query.Predicate{mailboxAccessPredicate(queryValue)}
	switch queryValue.Mailbox {
	case inbox.MailboxUnread:
		predicates = append(predicates, query.Equal("archived_at", ""), query.Equal("read_at", ""))
	case inbox.MailboxActionRequired:
		predicates = append(predicates, query.Equal("archived_at", ""), query.Equal("action_state", "open"))
	case inbox.MailboxArchived:
		predicates = append(predicates, query.NotEqual("archived_at", ""))
	default:
		predicates = append(predicates, query.Equal("archived_at", ""))
	}
	if queryValue.Query != "" {
		predicates = append(predicates, query.LikeValue(query.Lower(query.Column("search_text")), "%"+strings.ToLower(queryValue.Query)+"%"))
	}
	predicates = appendStringPredicate(predicates, "category", queryValue.Categories)
	predicates = appendStringPredicate(predicates, "source", queryValue.Sources)
	predicates = appendStringPredicate(predicates, "severity", queryValue.Severities)
	actions := make([]string, len(queryValue.ActionStates))
	for index, value := range queryValue.ActionStates {
		actions[index] = string(value)
	}
	predicates = appendStringPredicate(predicates, "action_state", actions)
	if queryValue.From != "" {
		predicates = append(predicates, query.GreaterThanOrEqual("last_occurred_at", queryValue.From))
	}
	if queryValue.To != "" {
		predicates = append(predicates, query.LessThanOrEqual("last_occurred_at", queryValue.To))
	}
	if includeCursor && queryValue.BeforeUpdatedAt != "" && queryValue.BeforeID != "" {
		predicates = append(predicates, query.Or(query.LessThan("updated_at", queryValue.BeforeUpdatedAt), query.And(query.Equal("updated_at", queryValue.BeforeUpdatedAt), query.LessThan("id", queryValue.BeforeID))))
	}
	return query.And(predicates...)
}

func mailboxAccessPredicate(queryValue inbox.Query) query.Predicate {
	predicates := []query.Predicate{query.Equal("workspace_id", queryValue.WorkspaceID.String()), query.Equal("surface", string(queryValue.Surface))}
	recipients := make([]string, len(queryValue.RecipientUserIDs))
	for index, recipient := range queryValue.RecipientUserIDs {
		recipients[index] = recipient.String()
	}
	return query.And(appendStringPredicate(predicates, "recipient_user_id", recipients)...)
}

func appendStringPredicate(predicates []query.Predicate, column string, values []string) []query.Predicate {
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
	return append(predicates, query.In(column, items...))
}

func normalizeMailboxStoreQuery(queryValue inbox.Query) (inbox.Query, error) {
	if queryValue.WorkspaceID == "" || queryValue.Surface == "" || len(queryValue.RecipientUserIDs) == 0 {
		return queryValue, fmt.Errorf("notification mailbox query requires an explicit workspace, surface, and recipient boundary")
	}
	if queryValue.Limit <= 0 {
		queryValue.Limit = 50
	}
	if queryValue.Limit > 100 {
		queryValue.Limit = 100
	}
	return queryValue, nil
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
