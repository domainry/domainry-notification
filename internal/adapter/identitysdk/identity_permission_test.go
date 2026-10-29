package identity

import (
	"testing"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationapplication "github.com/domainry/domainry-notification/internal/application"
)

func TestPermissionDefinitionsSatisfyIdentityContract(t *testing.T) {
	definitions, err := PermissionDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		if _, err := identitysdk.NewPermissionReconcileRequest(notificationapplication.NotificationAuthorizationOwner, "", []identitysdk.PermissionDefinition{definition}); err != nil {
			t.Fatalf("Permission %q is invalid: %v (%#v)", definition.PermissionKey, err, definition)
		}
	}
}
