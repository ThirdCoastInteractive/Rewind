package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/pkg/utils/passwords"
)

// NewUserParams contains the parameters for creating a new user
type NewUserParams struct {
	Username string
	Email    string
	Password string // plaintext password
	Role     string
}

// NewUser creates a new user with a hashed password
func (q *Queries) NewUser(ctx context.Context, params NewUserParams) (*User, error) {
	// Hash the password
	hashedPassword, err := passwords.NewPassword(passwords.PasswordInput{
		Password: params.Password,
	})
	if err != nil {
		return nil, err
	}

	// Generate a new UUID for the user
	userID := uuid.New()
	pgUUID := pgtype.UUID{
		Bytes: userID,
		Valid: true,
	}

	role := UserRole(params.Role)
	if params.Role == "" {
		role = UserRoleUser
	}

	// Insert the user with the hashed password
	return q.insertUser(ctx, &insertUserParams{
		ID:       pgUUID,
		Email:    params.Email,
		Password: hashedPassword,
		UserName: params.Username,
		Role:     role,
	})
}
