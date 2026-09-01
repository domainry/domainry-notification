package application

import (
	"strings"
	"testing"

	actioncontract "github.com/domainry/domainry-foundation/action"
)

func TestAuthorizationActionsFreezeAsSingleExactManifest(t *testing.T) {
	definitions, err := AuthorizationActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 61 {
		t.Fatalf("Action count=%d", len(definitions))
	}
	registry := actioncontract.NewRegistry()
	if err := registry.Register(definitions...); err != nil {
		t.Fatal(err)
	}
	if err := registry.Freeze(); err != nil {
		t.Fatal(err)
	}
	permissions := registry.PermissionDefinitions()
	if len(permissions) != 21 {
		t.Fatalf("Permission count=%d", len(permissions))
	}
	for _, definition := range registry.Definitions() {
		if definition.Owner != NotificationAuthorizationOwner || definition.HTTP == nil {
			t.Fatalf("incomplete owner/binding: %#v", definition)
		}
		if definition.Permission != nil {
			permission := definition.Permission
			if permission.Key != definition.Key || permission.Key != permission.ResourceKey+"."+permission.ActionKey {
				t.Fatalf("non-exact Permission: Action=%#v Permission=%#v", definition, permission)
			}
			if strings.Contains(permission.Key, "*") {
				t.Fatalf("broad or alias Permission: %#v", permission)
			}
			continue
		}
		if definition.Authorization.Strategy != actioncontract.AuthorizationAuthenticatedPrincipal || !strings.Contains(definition.CapabilityKey, "inbox") {
			t.Fatalf("unexpected Permission-free Action: %#v", definition)
		}
	}
	if _, found := registry.ResolveHTTP("POST", "/notifications/templates/{templateKey}/publish"); found {
		t.Fatal("legacy direct-publish tombstone remains registered")
	}
}
