package lifecyclestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/domainry/domainry-notification-sdk/contract"
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
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "_notification_channel_plans", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "updated_at", statusColumn: "status", eligibleStatuses: []string{"planned", "failed", "cancelled"}},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "_notification_events", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "updated_at", statusColumn: "status", eligibleStatuses: []string{"materialized", "failed"}},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "_notification_event_failures", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "occurred_at"},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "_notification_delivery_reservations", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "created_at"},
		{policyKey: contract.NotificationRetentionPublicationPolicy, table: "_notification_template_versions", idColumn: "id", timeColumn: "published_at", additionalPredicate: query.LessThanExpressions(query.TableColumn("_notification_template_versions", "version"), query.Coalesce(query.ScalarSubquery("_notification_templates", query.Column("published_version"), query.EqualExpressions(query.TableColumn("_notification_templates", "template_key"), query.TableColumn("_notification_template_versions", "template_key"))), query.TableColumn("_notification_template_versions", "version")))},
		{policyKey: contract.NotificationRetentionPublicationPolicy, table: "_notification_template_publication_requests", idColumn: "id", timeColumn: "updated_at", statusColumn: "status", eligibleStatuses: []string{"published", "rejected", "failed", "cancelled"}, referenceTable: "_notification_template_publication_locks", referenceColumn: "request_id"},
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
		predicate := s.excludeArchived(s.retentionPredicate(spec, request.Now.Add(-time.Duration(request.Policy.DefaultRetentionSeconds)*time.Second)), spec, request.WorkspaceID, request.Policy.Key)
		queryBuilder := query.NewSelectBuilder(s.Renderer, spec.table)
		if spec.workspaceColumn != "" {
			queryBuilder = query.NewWorkspaceSelectBuilder(s.Renderer, spec.table, request.WorkspaceID)
		}
		queryValue, args, err := queryBuilder.Projections(query.Project(query.CountAll()), query.Project(query.Min(query.Column(spec.timeColumn)))).Where(predicate).Build()
		if err != nil {
			return contract.NotificationRetentionPreview{}, err
		}
		var count int64
		var oldest sql.NullString
		if err := s.Database.QueryRowContext(ctx, queryValue, args...).Scan(&count, &oldest); err != nil {
			return contract.NotificationRetentionPreview{}, err
		}
		result.Rows += count
		if oldest.Valid {
			parsed, _ := time.Parse(time.RFC3339Nano, oldest.String)
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
	if request.Operation == "archive" {
		predicate = s.excludeArchived(predicate, spec, request.WorkspaceID, request.Policy.Key)
	}
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
	type candidate struct{ id, timestamp string }
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
		result.Checkpoint = spec.table + ":" + candidate.id
		result.Scanned++
		parsed, _ := time.Parse(time.RFC3339Nano, candidate.timestamp)
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
	predicates = append(predicates, query.NotEqual(spec.timeColumn, ""), query.LessThanOrEqual(spec.timeColumn, cutoff.UTC().Format(time.RFC3339Nano)))
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

func (s *Store) excludeArchived(predicate query.Predicate, spec retentionSpec, workspaceID, policyKey string) query.Predicate {
	archive := query.And(query.Equal("workspace_id", workspaceID), query.Equal("source_table", spec.table), query.Equal("policy_key", policyKey), query.EqualExpressions(query.TableColumn("_notification_retention_archive_entries", "resource_id"), query.TableColumn(spec.table, spec.idColumn)))
	return query.And(predicate, query.NotExists("_notification_retention_archive_entries", archive))
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
	var exists int
	check, checkArgs, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_retention_archive_entries", request.WorkspaceID).Projections(query.Project(query.CountAll())).Where(query.And(query.Equal("policy_key", request.Policy.Key), query.Equal("source_table", spec.table), query.Equal("resource_id", resourceID))).Build()
	if err != nil {
		return false, err
	}
	if err := s.Database.QueryRowContext(ctx, check, checkArgs...).Scan(&exists); err != nil || exists > 0 {
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
	digest := sha256.Sum256(raw)
	identity := sha256.Sum256([]byte(request.WorkspaceID + "\x00" + request.Policy.Key + "\x00" + spec.table + "\x00" + resourceID))
	columnsToInsert := []string{"id", "workspace_id", "policy_key", "policy_version", "job_id", "source_table", "resource_id", "payload_hash", "payload_json", "archived_at"}
	_, err = s.WorkspaceInsert(ctx, s.Database, request.WorkspaceID, "_notification_retention_archive_entries", columnsToInsert, hex.EncodeToString(identity[:]), request.WorkspaceID, request.Policy.Key, request.Policy.Version, request.JobID, spec.table, resourceID, hex.EncodeToString(digest[:]), string(raw), request.Now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, fmt.Errorf("archive notification retention candidate %s/%s: %w", spec.table, resourceID, err)
	}
	return true, nil
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
