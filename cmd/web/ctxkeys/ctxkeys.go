package ctxkeys

type Key int

const (
	AccessLevel         Key = iota
	RegistrationEnabled     // bool: whether new user registration is allowed
)
