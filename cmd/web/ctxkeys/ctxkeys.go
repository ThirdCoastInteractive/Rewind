// Package ctxkeys defines typed context keys used to pass per-request values through middleware.
package ctxkeys

// Key is a typed context key to avoid collisions with other packages.
type Key int

const (
	// AccessLevel stores the user's authorization tier in the request context.
	AccessLevel Key = iota
	RegistrationEnabled     // bool: whether new user registration is allowed
	StaticVersion           // string: short hash of all dist assets for cache-busting
)
