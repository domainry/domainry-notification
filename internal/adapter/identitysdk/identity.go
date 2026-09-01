// Package server assembles the standalone Notification SaaS process. Unlike
// Module composition, this package always opens a Remote Identity Binding and
// owns that Binding's lifecycle.
package identity

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	identityremote "github.com/domainry/domainry-identity-sdk/remote"
	notificationapplication "github.com/domainry/domainry-notification/internal/application"
)

type Options struct {
	Factory     identitysdk.Factory
	Application identitysdk.ApplicationRef
}

// IdentityOptionsFromEnvironment is the production default. There is no
// Module/auto fallback: standalone Notification is an Identity SaaS client.
func OptionsFromEnvironment() Options {
	return Options{
		Factory: identityremote.NewFactory(identityremote.ConfigFromEnvironment()),
		Application: identitysdk.ApplicationRef{
			TenantID:       identitysdk.TenantID(strings.TrimSpace(os.Getenv("NOTIFICATION_TENANT_ID"))),
			WorkspaceID:    identitysdk.WorkspaceID(strings.TrimSpace(os.Getenv("NOTIFICATION_IDENTITY_WORKSPACE_ID"))),
			ApplicationKey: identitysdk.ApplicationKey(strings.TrimSpace(os.Getenv("NOTIFICATION_IDENTITY_APPLICATION_KEY"))),
		},
	}
}

func Open(ctx context.Context, options Options) (identitysdk.Binding, error) {
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
	if descriptor.ProtocolVersion != identitysdk.CurrentProtocolVersion || descriptor.BundleVersion != identitysdk.CurrentPolicyBundleVersion || descriptor.AuthorizationVersion != identitysdk.AuthorizationContractVersionV1 {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("Notification SaaS Identity protocol is incompatible")
	}
	if strings.TrimSpace(descriptor.Issuer) == "" || strings.TrimSpace(descriptor.Audience) != string(application.ApplicationKey) {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("Notification SaaS Identity issuer/audience scope is invalid")
	}
	if nilIdentityCapability(binding.Tokens()) || nilIdentityCapability(binding.Authorization()) || nilIdentityCapability(binding.Principals()) || nilIdentityCapability(binding.Directory()) || nilIdentityCapability(binding.Applications()) || nilIdentityCapability(binding.Permissions()) {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("Notification SaaS Identity capabilities are incomplete")
	}
	if _, err := binding.Applications().Register(ctx, identitysdk.ApplicationRegistration{Application: application}); err != nil {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("register Notification Identity application: %w", err)
	}
	definitions, err := PermissionDefinitions()
	if err != nil {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("build Notification Identity permissions: %w", err)
	}
	reader, ok := binding.Permissions().(identitysdk.PermissionSnapshotReader)
	if !ok {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("Notification SaaS Identity permission snapshot reader is unavailable")
	}
	snapshotRequest := identitysdk.PermissionSourceSnapshotRequest{Application: application, SourceOwner: notificationapplication.NotificationAuthorizationOwner}
	snapshot, err := reader.CurrentSourceSnapshot(ctx, snapshotRequest)
	if err != nil {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("read Notification Identity permission snapshot: %w", err)
	}
	if err := snapshot.ValidateFor(snapshotRequest); err != nil {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("validate Notification Identity permission snapshot: %w", err)
	}
	request, err := identitysdk.NewPermissionReconcileRequest(application, notificationapplication.NotificationAuthorizationOwner, snapshot.SnapshotHash, definitions)
	if err != nil {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("build Notification Identity permission snapshot: %w", err)
	}
	receipt, err := binding.Permissions().Reconcile(ctx, request)
	if err != nil {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("reconcile Notification Identity permissions: %w", err)
	}
	if err := receipt.ValidateFor(request); err != nil {
		_ = binding.Close(ctx)
		return nil, fmt.Errorf("validate Notification Identity permission receipt: %w", err)
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

func PermissionDefinitions() ([]identitysdk.PermissionDefinition, error) {
	actions, err := notificationapplication.AuthorizationActions()
	if err != nil {
		return nil, err
	}
	definitions := make([]identitysdk.PermissionDefinition, 0, len(actions))
	for _, action := range actions {
		if action.Permission == nil {
			continue
		}
		permission := action.Permission
		definitions = append(definitions, identitysdk.PermissionDefinition{
			PermissionKey: permission.Key, ResourceKey: permission.ResourceKey, ActionKey: permission.ActionKey,
			Label: permission.Label, Description: permission.Description, Category: permission.Category, SourceKind: action.SourceKind,
		})
	}
	return definitions, nil
}
