package model

import "time"

// Role names — used in JWT claims and RBAC checks.
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// User represents a registered user.
type User struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	IsActive     bool       `json:"is_active"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
	Roles        []string   `json:"roles,omitempty"`
}

// Role represents an RBAC role.
type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// Permission represents a single resource+action permission.
type Permission struct {
	ID          string    `json:"id"`
	Resource    string    `json:"resource"`
	Action      string    `json:"action"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// RefreshToken represents a stored refresh token record.
type RefreshToken struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	TokenHash string     `json:"-"` // SHA-256 of raw token
	IssuedAt  time.Time  `json:"issued_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	UserAgent string     `json:"user_agent,omitempty"`
	IPAddress string     `json:"ip_address,omitempty"`
}

// IsValid returns true if the token is not expired and not revoked.
func (rt *RefreshToken) IsValid() bool {
	return rt.RevokedAt == nil && time.Now().Before(rt.ExpiresAt)
}

// ----- Request / Response DTOs -----

type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginRequest struct {
	// Identifier accepts either an email or a username.
	// `email` and `username` are kept for backward compatibility.
	Identifier string `json:"identifier"`
	Email      string `json:"email"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

// LoginID returns the first non-empty login identifier (email or username).
func (r LoginRequest) LoginID() string {
	switch {
	case r.Identifier != "":
		return r.Identifier
	case r.Email != "":
		return r.Email
	default:
		return r.Username
	}
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
}

type MeResponse struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	Email       string     `json:"email"`
	Roles       []string   `json:"roles"`
	IsActive    bool       `json:"is_active"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// UpdateProfileRequest holds fields the user can change on their profile.
type UpdateProfileRequest struct {
	Username string `json:"username"` // optional — only updated if non-empty
	Email    string `json:"email"`    // optional — only updated if non-empty
}

// ChangePasswordRequest requires the current password for verification.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// SessionResponse represents a single active refresh-token session.
type SessionResponse struct {
	ID        string     `json:"id"`
	UserAgent string     `json:"user_agent"`
	IPAddress string     `json:"ip_address"`
	IssuedAt  time.Time  `json:"issued_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// ----- Admin: user management DTOs -----

// UserSummary is the admin-facing view of a user.
type UserSummary struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	Email       string     `json:"email"`
	Roles       []string   `json:"roles"`
	IsActive    bool       `json:"is_active"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// AdminUpdateUserRequest lets an admin change a user's status and/or roles.
// Fields are optional: nil/empty means "leave unchanged".
type AdminUpdateUserRequest struct {
	IsActive *bool    `json:"is_active,omitempty"`
	Roles    []string `json:"roles,omitempty"`
}
