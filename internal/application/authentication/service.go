// Package authentication defines the application-level service authentication
// boundary shared by the SaaS HTTP adapter and the Identity infrastructure adapter.
package authentication

import (
	"context"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
)

type Request struct {
	Credential  string
	Application notificationsdk.ApplicationRef
	Grant       identitysdk.ApplicationServiceGrant
}

type Authority struct {
	Application           notificationsdk.ApplicationRef
	SubjectID             string
	AuthorizationRevision string
}

type Service interface {
	Authenticate(context.Context, Request) (Authority, error)
}
