package base

import (
	"testing"
	"time"

	identitysdk "github.com/domainry/domainry-identity-sdk"
)

func TestExactPermissionRequiresSameExactKeyAndCanonicalAll(t *testing.T) {
	now := time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC)
	principal := exactPermissionPrincipal(now, "notification.templates", "list", identitysdk.DataScopeAll)
	access, err := NewExactPermission(principal, "notification.templates.list", "workspace-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if access.Key() != "notification.templates.list" {
		t.Fatalf("key=%q", access.Key())
	}
	if predicate, err := access.Predicate(map[string]string{"owner_user_id": "updated_by"}); err != nil || predicate != nil {
		t.Fatalf("data_scope=all must add no data predicate: predicate=%#v err=%v", predicate, err)
	}
	for _, key := range []string{"notification.templates.get", "notification.template.list"} {
		if _, err := NewExactPermission(principal, key, "workspace-1", now); err == nil {
			t.Fatalf("non-exact key %q was accepted", key)
		}
	}
}

func TestExactPermissionRejectsMissingPolicyNonAllScopeAndWorkspaceMismatch(t *testing.T) {
	now := time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC)
	owner := exactPermissionPrincipal(now, "notification.templates", "list", identitysdk.DataScopeOwner)
	if _, err := NewExactPermission(owner, "notification.templates.list", "workspace-1", now); err == nil {
		t.Fatal("owner scope was accepted for workspace-wide Notification data")
	}
	missing := exactPermissionPrincipal(now, "notification.templates", "list", identitysdk.DataScopeAll)
	missing.AccessBundle.DataPolicies = nil
	if _, err := NewExactPermission(missing, "notification.templates.list", "workspace-1", now); err == nil {
		t.Fatal("function grant without same-key data policy was accepted")
	}
	all := exactPermissionPrincipal(now, "notification.templates", "list", identitysdk.DataScopeAll)
	if _, err := NewExactPermission(all, "notification.templates.list", "workspace-2", now); err == nil {
		t.Fatal("workspace mismatch was accepted")
	}
}

func exactPermissionPrincipal(now time.Time, resource identitysdk.ResourceType, action identitysdk.Action, scope identitysdk.DataScope) identitysdk.Principal {
	bundle := identitysdk.AccessBundle{
		ContractVersion: identitysdk.CurrentPolicyBundleVersion, AuthorizationRevision: "revision-1", ExpiresAt: now.Add(time.Hour),
		Subject:        identitysdk.Subject{WorkspaceID: "workspace-1", SubjectID: "user-1"},
		FunctionGrants: []identitysdk.FunctionGrant{{Resource: resource, Action: action, Effect: identitysdk.EffectAllow}},
		DataPolicies:   []identitysdk.DataPolicy{{Key: string(resource) + "." + string(action), Resource: resource, Action: action, Effect: identitysdk.EffectAllow, DataScopes: []identitysdk.DataScope{scope}}},
	}
	return identitysdk.Principal{Known: true, WorkspaceID: "workspace-1", UserID: "user-1", AccessBundle: &bundle}
}
