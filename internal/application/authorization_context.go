package application

import "context"

type exactActionContextKey struct{}

// WithExactAction carries the immutable action chosen by the source-owned HTTP
// manifest into SDK dispatch. Direct SDK calls use their method-specific key.
func WithExactAction(ctx context.Context, actionKey string) context.Context {
	return context.WithValue(ctx, exactActionContextKey{}, actionKey)
}

func ExactAction(ctx context.Context) string {
	value, _ := ctx.Value(exactActionContextKey{}).(string)
	return value
}
