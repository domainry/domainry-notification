package operationstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	"github.com/domainry/domainry-orm/query"
)

const tableName = "_operations"

var operationColumns = []string{
	"id", "workspace_id", "system_purpose", "owner", "kind", "action_key", "parent_id", "resource_type", "resource_id",
	"idempotency_key", "request_fingerprint", "requested_by", "reason", "reference", "status", "status_url", "result_json", "metadata_json",
	"error_code", "failure_class", "next_action", "related_ids_json", "correlation", "evidence_json", "lease_owner", "lease_expires_at",
	"fencing_token", "expires_at", "created_at", "started_at", "finished_at", "updated_at",
}

type Store struct{ *base.SQLStore }

func New(sqlStore *base.SQLStore) (*Store, error) {
	if sqlStore == nil || sqlStore.Database == nil || sqlStore.Renderer == nil {
		return nil, fmt.Errorf("shared managed Operation SQL store is incomplete")
	}
	return &Store{SQLStore: sqlStore}, nil
}

func (s *Store) Create(ctx context.Context, value modulehost.ManagedOperation) error {
	if err := value.Validate(); err != nil {
		return err
	}
	statement, args, err := query.NewInsertBuilder(s.Renderer, tableName).Columns(operationColumns...).Values(operationValues(value)...).Build()
	if err != nil {
		return err
	}
	executor := modulehost.OperationExecutorFromContext(ctx, s.Database)
	if _, err := executor.ExecContext(ctx, statement, args...); err != nil {
		if _, found, readErr := s.Get(ctx, managedIdentity(value)); readErr == nil && found {
			return modulehost.ErrManagedOperationIdentityConflict
		}
		return err
	}
	return nil
}

func (s *Store) Get(ctx context.Context, identity modulehost.ManagedOperationIdentity) (modulehost.ManagedOperation, bool, error) {
	if err := identity.Validate(); err != nil {
		return modulehost.ManagedOperation{}, false, err
	}
	predicate := identityPredicate(identity)
	statement, args, err := operationSelect(s, identity.Scope.WorkspaceID).Columns(operationColumns...).Where(predicate).Build()
	if err != nil {
		return modulehost.ManagedOperation{}, false, err
	}
	executor := modulehost.OperationExecutorFromContext(ctx, s.Database)
	value, err := scanOperation(executor.QueryRowContext(ctx, statement, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return modulehost.ManagedOperation{}, false, nil
	}
	return value, err == nil, err
}

func (s *Store) List(ctx context.Context, filter modulehost.ManagedOperationQuery) ([]modulehost.ManagedOperation, error) {
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	predicate := scopePredicate(filter.Scope)
	predicate = and(predicate, query.Equal("owner", strings.TrimSpace(filter.Owner)), query.Equal("kind", strings.TrimSpace(filter.Kind)))
	if resourceID := strings.TrimSpace(filter.ResourceID); resourceID != "" {
		predicate = and(predicate, query.Equal("resource_id", resourceID))
	}
	if len(filter.Statuses) != 0 {
		values := make([]any, len(filter.Statuses))
		for index, status := range filter.Statuses {
			values[index] = strings.TrimSpace(status)
		}
		predicate = and(predicate, query.In("status", values...))
	}
	if value := strings.TrimSpace(filter.NextActionBefore); value != "" {
		predicate = and(predicate, query.NotEqual("next_action", ""), query.LessThanOrEqual("next_action", value))
	}
	if value := strings.TrimSpace(filter.LeaseExpiresBefore); value != "" {
		predicate = and(predicate, query.NotEqual("lease_expires_at", ""), query.LessThanOrEqual("lease_expires_at", value))
	}
	builder := operationSelect(s, filter.Scope.WorkspaceID).Columns(operationColumns...).Where(predicate).Limit(filter.Limit)
	if filter.OldestFirst {
		builder = builder.OrderBy(query.Ascending("next_action"), query.Ascending("created_at"), query.Ascending("id"))
	} else {
		builder = builder.OrderBy(query.Descending("created_at"), query.Descending("id"))
	}
	statement, args, err := builder.Build()
	if err != nil {
		return nil, err
	}
	executor := modulehost.OperationExecutorFromContext(ctx, s.Database)
	rows, err := executor.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []modulehost.ManagedOperation{}
	for rows.Next() {
		value, scanErr := scanOperation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) Transition(ctx context.Context, transition modulehost.ManagedOperationTransition) (modulehost.ManagedOperation, bool, error) {
	if err := transition.Validate(); err != nil {
		return modulehost.ManagedOperation{}, false, err
	}
	predicate := and(identityPredicate(transition.Identity), query.Equal("status", strings.TrimSpace(transition.ExpectedStatus)))
	if transition.ExpectedFencingToken > 0 {
		predicate = and(predicate, query.Equal("lease_owner", strings.TrimSpace(transition.ExpectedLeaseOwner)), query.Equal("fencing_token", transition.ExpectedFencingToken))
	}
	builder := operationUpdate(s, transition.Identity.Scope.WorkspaceID).
		Set("status", strings.TrimSpace(transition.Status)).Set("metadata_json", string(transition.Metadata)).Set("result_json", string(transition.Result)).
		Set("error_code", strings.TrimSpace(transition.ErrorCode)).Set("next_action", strings.TrimSpace(transition.NextAction)).
		Set("updated_at", transition.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if transition.ClearLease {
		builder = builder.Set("lease_owner", "").Set("lease_expires_at", "")
	}
	statement, args, err := builder.Where(predicate).Build()
	if err != nil {
		return modulehost.ManagedOperation{}, false, err
	}
	executor := modulehost.OperationExecutorFromContext(ctx, s.Database)
	result, err := executor.ExecContext(ctx, statement, args...)
	if err != nil {
		return modulehost.ManagedOperation{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return modulehost.ManagedOperation{}, false, err
	}
	value, found, err := s.Get(ctx, transition.Identity)
	return value, found, err
}

func (s *Store) Claim(ctx context.Context, claim modulehost.ManagedOperationClaim) (modulehost.ManagedOperation, bool, error) {
	if err := claim.Validate(); err != nil {
		return modulehost.ManagedOperation{}, false, err
	}
	due := query.Or(
		query.And(query.Equal("status", strings.TrimSpace(claim.DueStatus)), query.NotEqual("next_action", ""), query.LessThanOrEqual("next_action", strings.TrimSpace(claim.Now))),
		query.And(query.Equal("status", strings.TrimSpace(claim.ReclaimStatus)), query.NotEqual("lease_expires_at", ""), query.LessThanOrEqual("lease_expires_at", strings.TrimSpace(claim.Now))),
	)
	predicate := and(identityPredicate(claim.Identity), due)
	statement, args, err := operationUpdate(s, claim.Identity.Scope.WorkspaceID).
		Set("status", strings.TrimSpace(claim.Status)).Set("lease_owner", strings.TrimSpace(claim.LeaseOwner)).
		Set("lease_expires_at", strings.TrimSpace(claim.LeaseExpiresAt)).SetExpression("fencing_token", query.Add(query.Column("fencing_token"), query.Value(1))).
		Set("updated_at", claim.UpdatedAt.UTC().Format(time.RFC3339Nano)).Where(predicate).Build()
	if err != nil {
		return modulehost.ManagedOperation{}, false, err
	}
	executor := modulehost.OperationExecutorFromContext(ctx, s.Database)
	result, err := executor.ExecContext(ctx, statement, args...)
	if err != nil {
		return modulehost.ManagedOperation{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return modulehost.ManagedOperation{}, false, err
	}
	value, found, err := s.Get(ctx, claim.Identity)
	return value, found, err
}

func managedIdentity(value modulehost.ManagedOperation) modulehost.ManagedOperationIdentity {
	return modulehost.ManagedOperationIdentity{ID: value.Command.ID, Scope: value.Command.Scope, Owner: value.Command.Owner, Kind: value.Command.Kind}
}

func identityPredicate(identity modulehost.ManagedOperationIdentity) query.Predicate {
	return and(scopePredicate(identity.Scope), query.Equal("id", strings.TrimSpace(identity.ID)), query.Equal("owner", strings.TrimSpace(identity.Owner)), query.Equal("kind", strings.TrimSpace(identity.Kind)))
}

func scopePredicate(scope modulehost.OperationScope) query.Predicate {
	if workspaceID := strings.TrimSpace(scope.WorkspaceID); workspaceID != "" {
		return query.Equal("workspace_id", workspaceID)
	}
	return query.Equal("system_purpose", strings.TrimSpace(scope.SystemPurpose))
}

func and(predicates ...query.Predicate) query.Predicate {
	values := make([]query.Predicate, 0, len(predicates))
	for _, predicate := range predicates {
		if predicate != nil {
			values = append(values, predicate)
		}
	}
	return query.And(values...)
}

func operationSelect(s *Store, workspaceID string) *query.SelectBuilder {
	if strings.TrimSpace(workspaceID) != "" {
		return query.NewWorkspaceSelectBuilder(s.Renderer, tableName, strings.TrimSpace(workspaceID))
	}
	return query.NewSelectBuilder(s.Renderer, tableName)
}

func operationUpdate(s *Store, workspaceID string) *query.UpdateBuilder {
	if strings.TrimSpace(workspaceID) != "" {
		return query.NewWorkspaceUpdateBuilder(s.Renderer, tableName, strings.TrimSpace(workspaceID))
	}
	return query.NewUpdateBuilder(s.Renderer, tableName)
}

func operationValues(value modulehost.ManagedOperation) []any {
	command := value.Command
	return []any{
		command.ID, command.Scope.WorkspaceID, command.Scope.SystemPurpose, command.Owner, command.Kind, command.ActionKey, "", command.Scope.ResourceType, command.Scope.ResourceID,
		command.IdempotencyKey, command.RequestFingerprint, command.RequestedBy, command.Reason, command.Reference, value.Status, command.StatusURL,
		string(value.Result), string(value.Metadata), value.ErrorCode, "", value.NextAction, `[]`, "", `[]`, value.LeaseOwner, value.LeaseExpiresAt,
		value.FencingToken, "", command.CreatedAt.UTC().Format(time.RFC3339Nano), "", "", value.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

type scanner interface{ Scan(...any) error }

func scanOperation(row scanner) (modulehost.ManagedOperation, error) {
	var value modulehost.ManagedOperation
	var workspaceID, systemPurpose, parentID, status, resultJSON, metadataJSON, failureClass, relatedIDs, correlation, evidence, expiresAt, createdAt, startedAt, finishedAt, updatedAt string
	command := &value.Command
	err := row.Scan(
		&command.ID, &workspaceID, &systemPurpose, &command.Owner, &command.Kind, &command.ActionKey, &parentID, &command.Scope.ResourceType, &command.Scope.ResourceID,
		&command.IdempotencyKey, &command.RequestFingerprint, &command.RequestedBy, &command.Reason, &command.Reference, &status, &command.StatusURL,
		&resultJSON, &metadataJSON, &value.ErrorCode, &failureClass, &value.NextAction, &relatedIDs, &correlation, &evidence, &value.LeaseOwner,
		&value.LeaseExpiresAt, &value.FencingToken, &expiresAt, &createdAt, &startedAt, &finishedAt, &updatedAt,
	)
	if err != nil {
		return value, err
	}
	command.Scope.WorkspaceID, command.Scope.SystemPurpose = workspaceID, systemPurpose
	value.Status, value.Result, value.Metadata = status, json.RawMessage(resultJSON), json.RawMessage(metadataJSON)
	command.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return value, err
	}
	value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	return value, err
}

var _ modulehost.ManagedOperationStore = (*Store)(nil)
