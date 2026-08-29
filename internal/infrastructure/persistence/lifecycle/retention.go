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

	"github.com/domainry/domainry-notification-sdk/contract"
)

type retentionSpec struct {
	policyKey, table, idColumn, workspaceColumn, timeColumn, statusColumn string
	eligibleStatuses                                                      []string
	additionalWhere                                                       string
	referenceTable, referenceColumn                                       string
}

func (s *Store) retentionSpecs(policyKey string) []retentionSpec {
	id := s.Renderer.Identifier
	table := s.Renderer.Table
	specs := []retentionSpec{
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "notification_inbox_items", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "updated_at", additionalWhere: id("action_state") + " <> 'open' AND " + id("alert_state") + " <> 'firing'"},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "notification_alert_groups", idColumn: "last_event_id", workspaceColumn: "workspace_id", timeColumn: "updated_at", statusColumn: "state", eligibleStatuses: []string{"resolved"}},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "notification_channel_plans", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "updated_at", statusColumn: "status", eligibleStatuses: []string{"planned", "failed", "cancelled"}},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "notification_events", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "updated_at", statusColumn: "status", eligibleStatuses: []string{"materialized", "failed"}},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "notification_event_failures", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "occurred_at"},
		{policyKey: contract.NotificationRetentionHistoryPolicy, table: "notification_delivery_reservations", idColumn: "id", workspaceColumn: "workspace_id", timeColumn: "created_at"},
		{policyKey: contract.NotificationRetentionPublicationPolicy, table: "notification_template_versions", idColumn: "id", timeColumn: "published_at", additionalWhere: table("notification_template_versions") + "." + id("version") + " < COALESCE((SELECT " + id("published_version") + " FROM " + table("notification_template_records") + " WHERE " + table("notification_template_records") + "." + id("template_key") + " = " + table("notification_template_versions") + "." + id("template_key") + "), " + table("notification_template_versions") + "." + id("version") + ")"},
		{policyKey: contract.NotificationRetentionPublicationPolicy, table: "notification_template_publication_requests", idColumn: "id", timeColumn: "updated_at", statusColumn: "status", eligibleStatuses: []string{"published", "rejected", "failed", "cancelled"}, referenceTable: "notification_template_publication_locks", referenceColumn: "request_id"},
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
		where, args := s.retentionWhere(spec, request.WorkspaceID, request.Now.Add(-time.Duration(request.Policy.DefaultRetentionSeconds)*time.Second), 1)
		where, args = s.excludeArchived(where, args, spec, request.WorkspaceID, request.Policy.Key)
		query := "SELECT COUNT(*), MIN(" + s.Renderer.Identifier(spec.timeColumn) + ") FROM " + s.Renderer.Table(spec.table) + " WHERE " + where
		var count int64
		var oldest sql.NullString
		if err := s.Database.QueryRowContext(ctx, query, args...).Scan(&count, &oldest); err != nil {
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
	where, args := s.retentionWhere(spec, request.WorkspaceID, request.Now.Add(-time.Duration(request.Policy.DefaultRetentionSeconds)*time.Second), 1)
	if request.Operation == "archive" {
		where, args = s.excludeArchived(where, args, spec, request.WorkspaceID, request.Policy.Key)
	}
	args = append(args, limit)
	query := "SELECT " + s.Renderer.Identifier(spec.idColumn) + ", " + s.Renderer.Identifier(spec.timeColumn) + " FROM " + s.Renderer.Table(spec.table) + " WHERE " + where + " ORDER BY " + s.Renderer.Identifier(spec.timeColumn) + ", " + s.Renderer.Identifier(spec.idColumn) + " LIMIT " + s.Renderer.Placeholder(len(args))
	rows, err := s.Database.QueryContext(ctx, query, args...)
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
			query := "SELECT COUNT(*) FROM " + s.Renderer.Table(spec.referenceTable) + " WHERE " + s.Renderer.Identifier(spec.referenceColumn) + " = " + s.Renderer.Placeholder(1)
			if err := s.Database.QueryRowContext(ctx, query, candidate.id).Scan(&references); err != nil {
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

func (s *Store) retentionWhere(spec retentionSpec, workspaceID string, cutoff time.Time, start int) (string, []any) {
	position := start
	parts, args := []string{}, []any{}
	if spec.workspaceColumn != "" {
		parts = append(parts, s.Renderer.Identifier(spec.workspaceColumn)+" = "+s.Renderer.Placeholder(position))
		args = append(args, workspaceID)
		position++
	}
	parts = append(parts, s.Renderer.Identifier(spec.timeColumn)+" <> ''", s.Renderer.Identifier(spec.timeColumn)+" <= "+s.Renderer.Placeholder(position))
	args = append(args, cutoff.UTC().Format(time.RFC3339Nano))
	position++
	if len(spec.eligibleStatuses) > 0 {
		placeholders := make([]string, len(spec.eligibleStatuses))
		for index, status := range spec.eligibleStatuses {
			placeholders[index] = s.Renderer.Placeholder(position)
			args = append(args, status)
			position++
		}
		parts = append(parts, s.Renderer.Identifier(spec.statusColumn)+" IN ("+strings.Join(placeholders, ", ")+")")
	}
	if spec.additionalWhere != "" {
		parts = append(parts, spec.additionalWhere)
	}
	return strings.Join(parts, " AND "), args
}

func (s *Store) excludeArchived(where string, args []any, spec retentionSpec, workspaceID, policyKey string) (string, []any) {
	args = append(args, workspaceID, spec.table, policyKey)
	start := len(args) - 2
	where += " AND NOT EXISTS (SELECT 1 FROM " + s.Renderer.Table("notification_retention_archive") + " a WHERE a." + s.Renderer.Identifier("workspace_id") + " = " + s.Renderer.Placeholder(start) + " AND a." + s.Renderer.Identifier("source_table") + " = " + s.Renderer.Placeholder(start+1) + " AND a." + s.Renderer.Identifier("policy_key") + " = " + s.Renderer.Placeholder(start+2) + " AND a." + s.Renderer.Identifier("resource_id") + " = " + s.Renderer.Table(spec.table) + "." + s.Renderer.Identifier(spec.idColumn) + ")"
	return where, args
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
	check := "SELECT COUNT(*) FROM " + s.Renderer.Table("notification_retention_archive") + " WHERE " + s.Renderer.Identifier("workspace_id") + " = " + s.Renderer.Placeholder(1) + " AND " + s.Renderer.Identifier("policy_key") + " = " + s.Renderer.Placeholder(2) + " AND " + s.Renderer.Identifier("source_table") + " = " + s.Renderer.Placeholder(3) + " AND " + s.Renderer.Identifier("resource_id") + " = " + s.Renderer.Placeholder(4)
	if err := s.Database.QueryRowContext(ctx, check, request.WorkspaceID, request.Policy.Key, spec.table, resourceID).Scan(&exists); err != nil || exists > 0 {
		return false, err
	}
	where, args := s.Renderer.Identifier(spec.idColumn)+" = "+s.Renderer.Placeholder(1), []any{resourceID}
	if spec.workspaceColumn != "" {
		where = s.Renderer.Identifier(spec.workspaceColumn) + " = " + s.Renderer.Placeholder(1) + " AND " + s.Renderer.Identifier(spec.idColumn) + " = " + s.Renderer.Placeholder(2)
		args = []any{request.WorkspaceID, resourceID}
	}
	rows, err := s.Database.QueryContext(ctx, "SELECT * FROM "+s.Renderer.Table(spec.table)+" WHERE "+where, args...)
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
	_, err = s.Insert(ctx, s.Database, "notification_retention_archive", columnsToInsert, hex.EncodeToString(identity[:]), request.WorkspaceID, request.Policy.Key, request.Policy.Version, request.JobID, spec.table, resourceID, hex.EncodeToString(digest[:]), string(raw), request.Now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, fmt.Errorf("archive notification retention candidate %s/%s: %w", spec.table, resourceID, err)
	}
	return true, nil
}

func (s *Store) deleteRetentionCandidate(ctx context.Context, workspaceID string, spec retentionSpec, resourceID string) (int64, error) {
	where, args := s.Renderer.Identifier(spec.idColumn)+" = "+s.Renderer.Placeholder(1), []any{resourceID}
	if spec.workspaceColumn != "" {
		where = s.Renderer.Identifier(spec.workspaceColumn) + " = " + s.Renderer.Placeholder(1) + " AND " + s.Renderer.Identifier(spec.idColumn) + " = " + s.Renderer.Placeholder(2)
		args = []any{workspaceID, resourceID}
	}
	result, err := s.Database.ExecContext(ctx, "DELETE FROM "+s.Renderer.Table(spec.table)+" WHERE "+where, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
