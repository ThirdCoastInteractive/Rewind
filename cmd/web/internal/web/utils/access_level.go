package utils

// AccessLevel represents an authorization tier used by template rendering.
type AccessLevel string

const (
	// AccessLevelUnauthenticated indicates no valid session exists.
	AccessLevelUnauthenticated AccessLevel = "unauthenticated"
	// AccessLevelUser indicates a regular authenticated user.
	AccessLevelUser AccessLevel = "user"
	// AccessLevelProducer indicates a user with live-session producer privileges.
	AccessLevelProducer AccessLevel = "producer"
	// AccessLevelAdmin indicates an administrator with full privileges.
	AccessLevelAdmin AccessLevel = "admin"
)
