package lifecyclestore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-orm/query"
)

type retentionSpec struct {
	policyKey, table, idColumn, workspaceColumn, timeColumn, statusColumn string
	eligibleStatuses                                                      []string
	additionalPredicate                                                   query.Predicate
	referenceTable, referenceColumn                                       string
}

func (s *Store) retentionSpecs(policyKey string) []retentionSpec {
	specs := []retentionSpec{
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "_notification_inbox_items", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "updated_at", additionalPredicate: query.And(query.NotEqual("action_state", "open"), query.NotEqual("alert_state", "firing"))},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "_notification_alert_groups", idColumn: "last_event_id", workspaceColumn: "workspace_id", timeColumn: "updated_at", statusColumn: "state", eligibleStatuses: []string{"resolved"}},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "_notification_deliveries", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "updated_at", statusColumn: "status", eligibleStatuses: []string{"planned", "failed", "cancelled", "reserved"}},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "_notification_events", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "updated_at", statusColumn: "status", eligibleStatuses: []string{"materialized", "failed"}},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "_notification_delivery_attempts", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "occurred_at"},
	}
	result := []retentionSpec{}
	for _, spec := range specs {
		if spec.policyKey == policyKey {
			result = append(result, spec)
		}
	}
	return result
}

func (s *Store) PreviewRetention(ctx context.Context, request contract.NotificationRetentionPreviewRequest) (contract.NotificationRetentionPreview, error) {
	if err := request.Validate(); err != nil {
		return contract.NotificationRetentionPreview{}, err
	}
	specs := s.retentionSpecs(request.Policy.Key)
	if len(specs) == 0 {
		return contract.NotificationRetentionPreview{}, fmt.Errorf("notification retention policy %q is unsupported", request.Policy.Key)
	}
	result := contract.NotificationRetentionPreview{}
	for _, spec := range specs {
		predicate := s.retentionPredicate(spec, request.Now.Add(-time.Duration(request.Policy.DefaultRetentionSeconds)*time.Second))
		queryBuilder := query.NewSelectBuilder(s.Renderer, spec.table)
		if spec.workspaceColumn != "" {
			queryBuilder = query.NewWorkspaceSelectBuilder(s.Renderer, spec.table, request.WorkspaceID)
		}
		queryValue, args, err := queryBuilder.Columns(spec.idColumn, spec.timeColumn).Where(predicate).OrderBy(query.Ascending(spec.timeColumn), query.Ascending(spec.idColumn)).Build()
		if err != nil {
			return contract.NotificationRetentionPreview{}, err
		}
		rows, err := s.Database.QueryContext(ctx, queryValue, args...)
		if err != nil {
			return contract.NotificationRetentionPreview{}, err
		}
		type candidate struct {
			id        string
			timestamp int64
		}
		candidates := []candidate{}
		for rows.Next() {
			var value candidate
			if err := rows.Scan(&value.id, &value.timestamp); err != nil {
				_ = rows.Close()
				return contract.NotificationRetentionPreview{}, err
			}
			candidates = append(candidates, value)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return contract.NotificationRetentionPreview{}, err
		}
		if err := rows.Close(); err != nil {
			return contract.NotificationRetentionPreview{}, err
		}
		for _, candidate := range candidates {
			archived, err := s.retentionArchived(ctx, request.WorkspaceID, spec.table, candidate.id, request.Policy.Key)
			if err != nil {
				return contract.NotificationRetentionPreview{}, err
			}
			if archived {
				continue
			}
			result.Rows++
			parsed := time.UnixMilli(candidate.timestamp).UTC()
			if !parsed.IsZero() && (result.OldestEligible.IsZero() || parsed.Before(result.OldestEligible)) {
				result.OldestEligible = parsed
			}
		}
	}
	result.Bytes = result.Rows * 1024
	return result, nil
}

func (s *Store) ProcessRetentionBatch(ctx context.Context, request contract.NotificationRetentionBatchRequest) (contract.NotificationRetentionBatchResult, error) {
	if err := request.Validate(); err != nil {
		return contract.NotificationRetentionBatchResult{}, err
	}
	specs := s.retentionSpecs(request.Policy.Key)
	if len(specs) == 0 {
		return contract.NotificationRetentionBatchResult{}, fmt.Errorf("notification retention policy %q is unsupported", request.Policy.Key)
	}
	result := contract.NotificationRetentionBatchResult{Done: true}
	remaining := request.Limit
	for _, spec := range specs {
		if remaining <= 0 {
			result.Done = false
			break
		}
		batch, err := s.processRetentionSpec(ctx, request, spec, remaining)
		if err != nil {
			return result, err
		}
		result.Checkpoint = batch.Checkpoint
		result.Scanned += batch.Scanned
		result.Archived += batch.Archived
		result.Purged += batch.Purged
		result.Skipped += batch.Skipped
		result.Failed += batch.Failed
		if !batch.OldestEligible.IsZero() && (result.OldestEligible.IsZero() || batch.OldestEligible.Before(result.OldestEligible)) {
			result.OldestEligible = batch.OldestEligible
		}
		remaining -= int(batch.Scanned)
		result.Done = result.Done && batch.Done
	}
	return result, nil
}

func (s *Store) processRetentionSpec(ctx context.Context, request contract.NotificationRetentionBatchRequest, spec retentionSpec, limit int) (contract.NotificationRetentionBatchResult, error) {
	predicate := s.retentionPredicate(spec, request.Now.Add(-time.Duration(request.Policy.DefaultRetentionSeconds)*time.Second))
	queryBuilder := query.NewSelectBuilder(s.Renderer, spec.table)
	if spec.workspaceColumn != "" {
		queryBuilder = query.NewWorkspaceSelectBuilder(s.Renderer, spec.table, request.WorkspaceID)
	}
	queryValue, args, err := queryBuilder.Columns(spec.idColumn, spec.timeColumn).Where(predicate).OrderBy(query.Ascending(spec.timeColumn), query.Ascending(spec.idColumn)).Limit(limit).Build()
	if err != nil {
		return contract.NotificationRetentionBatchResult{}, err
	}
	rows, err := s.Database.QueryContext(ctx, queryValue, args...)
	if err != nil {
		return contract.NotificationRetentionBatchResult{}, err
	}
	type candidate struct {
		id        string
		timestamp int64
	}
	candidates := []candidate{}
	for rows.Next() {
		var value candidate
		if err := rows.Scan(&value.id, &value.timestamp); err != nil {
			_ = rows.Close()
			return contract.NotificationRetentionBatchResult{}, err
		}
		candidates = append(candidates, value)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return contract.NotificationRetentionBatchResult{}, err
	}
	_ = rows.Close()
	result := contract.NotificationRetentionBatchResult{Done: len(candidates) < limit}
	for _, candidate := range candidates {
		alreadyArchived, err := s.retentionArchived(ctx, request.WorkspaceID, spec.table, candidate.id, request.Policy.Key)
		if err != nil {
			return result, err
		}
		if request.Operation == "archive" && alreadyArchived {
			continue
		}
		result.Checkpoint = spec.table + ":" + candidate.id
		result.Scanned++
		parsed := time.UnixMilli(candidate.timestamp).UTC()
		if !parsed.IsZero() && (result.OldestEligible.IsZero() || parsed.Before(result.OldestEligible)) {
			result.OldestEligible = parsed
		}
		if retentionHeld(request.Holds, spec.table, candidate.id, request.Now) {
			result.Skipped++
			continue
		}
		if spec.referenceTable != "" && request.Operation == "purge" {
			var references int
			queryValue, args, err := query.NewSelectBuilder(s.Renderer, spec.referenceTable).Projections(query.Project(query.CountAll())).Where(query.Equal(spec.referenceColumn, candidate.id)).Build()
			if err != nil {
				return result, err
			}
			if err := s.Database.QueryRowContext(ctx, queryValue, args...).Scan(&references); err != nil {
				return result, err
			}
			if references > 0 {
				result.Skipped++
				continue
			}
		}
		if request.DryRun {
			continue
		}
		archived, err := s.archiveRetentionCandidate(ctx, request, spec, candidate.id)
		if err != nil {
			result.Failed++
			return result, err
		}
		if archived {
			result.Archived++
		}
		if request.Operation != "purge" {
			continue
		}
		deleted, err := s.deleteRetentionCandidate(ctx, request.WorkspaceID, spec, candidate.id)
		if err != nil {
			result.Failed++
			return result, err
		}
		result.Purged += deleted
	}
	return result, nil
}

func (s *Store) retentionPredicate(spec retentionSpec, cutoff time.Time) query.Predicate {
	predicates := []query.Predicate{}
	predicates = append(predicates, query.NotEqual(spec.timeColumn, int64(0)), query.LessThanOrEqual(spec.timeColumn, cutoff.UTC().UnixMilli()))
	if len(spec.eligibleStatuses) > 0 {
		values := make([]any, len(spec.eligibleStatuses))
		for index, status := range spec.eligibleStatuses {
			values[index] = status
		}
		predicates = append(predicates, query.In(spec.statusColumn, values...))
	}
	if spec.additionalPredicate != nil {
		predicates = append(predicates, spec.additionalPredicate)
	}
	return query.And(predicates...)
}

func retentionHeld(holds []contract.NotificationRetentionHold, table, resourceID string, now time.Time) bool {
	for _, hold := range holds {
		if now.Before(hold.StartsAt) || (hold.EndsAt != nil && !now.Before(*hold.EndsAt)) {
			continue
		}
		if (hold.Owner == "" || hold.Owner == "notification") && (hold.ResourceType == "" || hold.ResourceType == table) && (hold.ResourceID == "" || hold.ResourceID == resourceID) {
			return true
		}
	}
	return false
}

func (s *Store) archiveRetentionCandidate(ctx context.Context, request contract.NotificationRetentionBatchRequest, spec retentionSpec, resourceID string) (bool, error) {
	if archived, err := s.retentionArchived(ctx, request.WorkspaceID, spec.table, resourceID, request.Policy.Key); err != nil || archived {
		return false, err
	}
	predicate := query.Predicate(query.Equal(spec.idColumn, resourceID))
	queryBuilder := query.NewSelectBuilder(s.Renderer, spec.table)
	if spec.workspaceColumn != "" {
		queryBuilder = query.NewWorkspaceSelectBuilder(s.Renderer, spec.table, request.WorkspaceID)
	}
	statement, args, err := queryBuilder.Projections(query.Project(query.AllColumns())).Where(predicate).Build()
	if err != nil {
		return false, err
	}
	rows, err := s.Database.QueryContext(ctx, statement, args...)
	if err != nil {
		return false, err
	}
	columns, err := rows.Columns()
	if err != nil || !rows.Next() {
		_ = rows.Close()
		return false, err
	}
	values, pointers := make([]any, len(columns)), make([]any, len(columns))
	for index := range values {
		pointers[index] = &values[index]
	}
	if err := rows.Scan(pointers...); err != nil {
		_ = rows.Close()
		return false, err
	}
	_ = rows.Close()
	payload := map[string]any{}
	for index, column := range columns {
		if bytes, ok := values[index].([]byte); ok {
			payload[column] = string(bytes)
		} else {
			payload[column] = values[index]
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	archived, err := s.archives.ArchivePayload(ctx, modulehost.RetentionArchiveOwnerNotification,
		modulehost.RetentionArchiveJob{ID: request.JobID, WorkspaceID: request.WorkspaceID, ArchivedAt: request.Now},
		modulehost.RetentionArchivePolicy{Key: request.Policy.Key, Version: request.Policy.Version},
		spec.table, resourceID, raw)
	if err != nil {
		return false, fmt.Errorf("archive notification retention candidate %s/%s: %w", spec.table, resourceID, err)
	}
	return archived, nil
}

func (s *Store) retentionArchived(ctx context.Context, workspaceID, sourceTable, resourceID, policyKey string) (bool, error) {
	if s == nil || s.archives == nil {
		return false, fmt.Errorf("shared Lifecycle retention archive store is unavailable")
	}
	return s.archives.Archived(ctx, workspaceID, sourceTable, resourceID, policyKey)
}

func (s *Store) deleteRetentionCandidate(ctx context.Context, workspaceID string, spec retentionSpec, resourceID string) (int64, error) {
	predicate := query.Predicate(query.Equal(spec.idColumn, resourceID))
	deleteBuilder := query.NewDeleteBuilder(s.Renderer, spec.table)
	if spec.workspaceColumn != "" {
		deleteBuilder = query.NewWorkspaceDeleteBuilder(s.Renderer, spec.table, workspaceID)
	}
	statement, args, err := deleteBuilder.Where(predicate).Build()
	if err != nil {
		return 0, err
	}
	result, err := s.Database.ExecContext(ctx, statement, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
