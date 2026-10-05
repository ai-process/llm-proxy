package apikeys

import "context"

type identityKey struct{}

// WithIdentity stores the authenticated caller for handlers downstream of the
// auth interceptor.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFrom returns the authenticated caller, or nil outside an
// authenticated RPC (exempt methods, tests).
func IdentityFrom(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityKey{}).(*Identity)
	return id
}
