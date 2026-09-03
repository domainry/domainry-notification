package base

import (
	"errors"
	"fmt"
	"strings"
	"time"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	identityevaluator "github.com/domainry/domainry-identity-sdk/authorization/evaluator"
	"github.com/domainry/domainry-orm/query"
)

// ErrExactPermissionDenied is deliberately fail-closed: Notification requires
// an exact function grant and an exact same-key canonical `all` data policy.
var ErrExactPermissionDenied = errors.New("notification exact permission data scope denied")

// ExactPermission is request-local proof that Identity granted one exact
// Notification permission with unrestricted data scope. Notification has only
// workspace-owned data, so it intentionally implements no second RecordScope
// protocol and no owner/org row filtering.
type ExactPermission struct{ key string }

func NewExactPermission(principal identitysdk.Principal, permissionKey, workspaceID string, now time.Time) (*ExactPermission, error) {
	permissionKey, workspaceID = strings.TrimSpace(permissionKey), strings.TrimSpace(workspaceID)
	separator := strings.LastIndexByte(permissionKey, '.')
	if separator <= 0 || separator == len(permissionKey)-1 || principal.AccessBundle == nil || !principal.HasPermission(permissionKey) {
		return nil, ErrExactPermissionDenied
	}
	if !principal.Known || strings.TrimSpace(principal.WorkspaceID) != workspaceID || strings.TrimSpace(string(principal.AccessBundle.Subject.WorkspaceID)) != workspaceID {
		return nil, ErrExactPermissionDenied
	}
	resource, action := identitysdk.ResourceType(permissionKey[:separator]), identitysdk.Action(permissionKey[separator+1:])
	matchingPolicies := 0
	for _, policy := range principal.AccessBundle.DataPolicies {
		if policy.Resource != resource || policy.Action != action {
			continue
		}
		matchingPolicies++
		if policy.Effect != identitysdk.EffectAllow || len(policy.DataScopes) != 1 || policy.DataScopes[0] != identitysdk.DataScopeAll {
			return nil, ErrExactPermissionDenied
		}
	}
	if matchingPolicies == 0 {
		return nil, ErrExactPermissionDenied
	}
	filter, err := identityevaluator.CompileRecordFilter(*principal.AccessBundle, resource, action, now)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrExactPermissionDenied, err)
	}
	if !filter.Unrestricted || len(filter.Deny) != 0 {
		return nil, ErrExactPermissionDenied
	}
	return &ExactPermission{key: permissionKey}, nil
}

func (a *ExactPermission) Key() string {
	if a == nil {
		return ""
	}
	return a.key
}

// Predicate is nil by design: canonical `all` adds no data WHERE clause.
// Callers independently use domainry-orm workspace builders for tenant scope.
func (*ExactPermission) Predicate(map[string]string) (query.Predicate, error) { return nil, nil }

func (*ExactPermission) ValidateColumns(map[string]string) error { return nil }

func (*ExactPermission) AllowsCreate(identitysdk.ResourceFacts) (bool, error) { return true, nil }
