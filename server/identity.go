// Package server assembles the standalone Notification SaaS process. Unlike
// Module composition, this package always opens a Remote Identity Binding and
// owns that Binding's lifecycle.
package server

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	identityremote "github.com/domainry/domainry-identity-sdk/remote"
)

type IdentityOptions struct {
	Factory     identitysdk.Factory
	Application identitysdk.ApplicationRef
}

// IdentityOptionsFromEnvironment is the production default. There is no
// Module/auto fallback: standalone Notification is an Identity SaaS client.
func IdentityOptionsFromEnvironment() IdentityOptions {
	return IdentityOptions{
		Factory: identityremote.NewFactory(identityremote.ConfigFromEnvironment()),
		Application: identitysdk.ApplicationRef{
			TenantID:       identitysdk.TenantID(strings.TrimSpace(os.Getenv("NOTIFICATION_TENANT_ID"))),
			WorkspaceID:    identitysdk.WorkspaceID(strings.TrimSpace(os.Getenv("NOTIFICATION_IDENTITY_WORKSPACE_ID"))),
			ApplicationKey: identitysdk.ApplicationKey(strings.TrimSpace(os.Getenv("NOTIFICATION_IDENTITY_APPLICATION_KEY"))),
		},
	}
}

func OpenIdentity(ctx context.Context, options IdentityOptions) (identitysdk.Binding, error) {
	if ctx == nil {
		return nil, fmt.Errorf("Notification SaaS lifecycle context is required")
	}
	if options.Factory == nil {
		return nil, fmt.Errorf("Notification SaaS requires an Identity SaaS Factory")
	}
	application := options.Application
	if !application.TenantID.Valid() || !application.WorkspaceID.Valid() || !application.ApplicationKey.Valid() {
		return nil, fmt.Errorf("Notification SaaS Identity tenant, workspace and application are required")
	}
	binding, err := options.Factory.Open(ctx, application)
	if err != nil {
		return nil, fmt.Errorf("open Notification SaaS Identity binding: %w", err)
	}
	if binding == nil {
		return nil, fmt.Errorf("Notification SaaS Identity Factory returned no Binding")
	}
	descriptor := binding.Descriptor()
	if descriptor.Mode != identitysdk.DeploymentModeSaaS {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("Notification SaaS requires Identity SaaS, got %q", descriptor.Mode)
	}
	if descriptor.ProtocolVersion != identitysdk.CurrentProtocolVersion || descriptor.BundleVersion != identitysdk.CurrentPolicyBundleVersion || descriptor.CatalogVersion != identitysdk.CatalogVersionV1 {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("Notification SaaS Identity protocol is incompatible")
	}
	if strings.TrimSpace(descriptor.Issuer) == "" || strings.TrimSpace(descriptor.Audience) != string(application.ApplicationKey) {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("Notification SaaS Identity issuer/audience scope is invalid")
	}
	if nilIdentityCapability(binding.Tokens()) || nilIdentityCapability(binding.Authorization()) || nilIdentityCapability(binding.Principals()) || nilIdentityCapability(binding.Directory()) || nilIdentityCapability(binding.Catalog()) {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("Notification SaaS Identity capabilities are incomplete")
	}
	catalog := notificationIdentityCatalog(application)
	if err := binding.Catalog().Validate(ctx, catalog); err != nil {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("validate Notification Identity catalog: %w", err)
	}
	if _, err := binding.Catalog().Publish(ctx, catalog); err != nil {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("publish Notification Identity catalog: %w", err)
	}
	return binding, nil
}

func nilIdentityCapability(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func notificationIdentityCatalog(application identitysdk.ApplicationRef) identitysdk.AuthorizationCatalog {
	applicationFacts := []string{"tenant_id", "workspace_id", "application_key"}
	resources := []identitysdk.ResourceDefinition{
		{Key: "notification_event", SupportedFacts: applicationFacts}, {Key: "notification_inbox", SupportedFacts: applicationFacts}, {Key: "notification_template", SupportedFacts: applicationFacts},
		{Key: "notification_publication", SupportedFacts: applicationFacts}, {Key: "notification_delivery_policy", SupportedFacts: applicationFacts},
		{Key: "notification_preference", SupportedFacts: applicationFacts}, {Key: "notification_team_mailbox", SupportedFacts: applicationFacts},
		{Key: "notification_delegation", SupportedFacts: applicationFacts}, {Key: "notification_governance", SupportedFacts: applicationFacts},
	}
	actions := []identitysdk.ActionDefinition{}
	for _, entry := range []struct {
		resource string
		actions  []string
	}{
		{"notification_event", []string{"publish"}},
		{"notification_inbox", []string{"read", "update", "act"}},
		{"notification_template", []string{"read", "draft", "preview", "disable"}},
		{"notification_publication", []string{"read", "request", "approve", "reject", "cancel"}},
		{"notification_delivery_policy", []string{"read", "update"}},
		{"notification_preference", []string{"read", "update"}},
		{"notification_team_mailbox", []string{"read"}},
		{"notification_delegation", []string{"read", "update", "delete"}},
		{"notification_governance", []string{"read"}},
	} {
		for _, action := range entry.actions {
			actions = append(actions, identitysdk.ActionDefinition{Resource: identitysdk.ResourceType(entry.resource), Action: identitysdk.Action(action)})
		}
	}
	return identitysdk.AuthorizationCatalog{ContractVersion: identitysdk.CatalogVersionV1, Application: application, Resources: resources, Actions: actions}
}
