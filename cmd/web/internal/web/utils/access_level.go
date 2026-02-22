package utils

type AccessLevel string

const (
	AccessLevelUnauthenticated AccessLevel = "unauthenticated"
	AccessLevelUser            AccessLevel = "user"
	AccessLevelProducer        AccessLevel = "producer"
	AccessLevelAdmin           AccessLevel = "admin"
)
