// Package operationstore adapts Notification's worker-facing method names to
// the canonical Foundation Operations SQL kernel. It owns no SQL or schema.
package operationstore

import (
	"context"
	"fmt"

	sharedoperation "github.com/domainry/domainry-foundation/operation"
	"github.com/domainry/domainry-notification-sdk/modulehost"
)

type Store struct{ kernel *sharedoperation.SQLStore }

func New(kernel *sharedoperation.SQLStore) (*Store, error) {
	if kernel == nil {
		return nil, fmt.Errorf("shared managed Operation SQL store is incomplete")
	}
	return &Store{kernel: kernel}, nil
}

func (s *Store) Create(ctx context.Context, value modulehost.ManagedOperation) error {
	return s.kernel.Create(ctx, value)
}

func (s *Store) Get(ctx context.Context, value modulehost.ManagedOperationIdentity) (modulehost.ManagedOperation, bool, error) {
	return s.kernel.Get(ctx, value)
}

func (s *Store) List(ctx context.Context, value modulehost.ManagedOperationQuery) ([]modulehost.ManagedOperation, error) {
	return s.kernel.List(ctx, value)
}

func (s *Store) Transition(ctx context.Context, value modulehost.ManagedOperationTransition) (modulehost.ManagedOperation, bool, error) {
	return s.kernel.Transition(ctx, value)
}

func (s *Store) Claim(ctx context.Context, value modulehost.ManagedOperationClaim) (modulehost.ManagedOperation, bool, error) {
	return s.kernel.ClaimManaged(ctx, value)
}

func (s *Store) GetOperationControl(ctx context.Context, purpose, kind, owner string) (modulehost.OperationControl, bool, error) {
	return s.kernel.GetControl(ctx, purpose, kind, owner)
}

func (s *Store) PutOperationControl(ctx context.Context, value modulehost.OperationControl, expectedRevision int64) (bool, error) {
	return s.kernel.PutControl(ctx, value, expectedRevision)
}

var _ modulehost.ManagedOperationStore = (*Store)(nil)
var _ modulehost.OperationControlStore = (*Store)(nil)
