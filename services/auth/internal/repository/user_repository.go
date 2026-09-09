package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/almuzky/iot/services/auth/internal/model"
	"github.com/google/uuid"
)

// ErrNotFound is returned when a record is not found.
var ErrNotFound = errors.New("record not found")

// UserRepository handles all DB operations related to users, roles, and tokens.
type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// ─── Users ────────────────────────────────────────────────────────────────────

// CreateUser inserts a new user record.
func (r *UserRepository) CreateUser(ctx context.Context, u *model.User) error {
	u.ID = uuid.New().String()
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, username, email, password_hash, is_active, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 1, $5, $6)`,
		u.ID, u.Username, u.Email, u.PasswordHash, u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// GetUserByEmail fetches a user (including roles) by email.
func (r *UserRepository) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
			err := r.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, is_active, last_login_at, created_at, updated_at
		 FROM users WHERE email = $1 AND deleted_at IS NULL`,
		email,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsActive,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}

	u.Roles, err = r.GetUserRoles(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// GetUserByIdentifier fetches a user (including roles) by email OR username.
func (r *UserRepository) GetUserByIdentifier(ctx context.Context, identifier string) (*model.User, error) {
	u := &model.User{}
			err := r.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, is_active, last_login_at, created_at, updated_at
		 FROM users WHERE (email = $1 OR username = $2) AND deleted_at IS NULL`,
		identifier, identifier,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsActive,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by identifier: %w", err)
	}

	u.Roles, err = r.GetUserRoles(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// GetUserByID fetches a user by ID.
func (r *UserRepository) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	u := &model.User{}
			err := r.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, is_active, last_login_at, created_at, updated_at
		 FROM users WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsActive,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}

	var roleErr error
	u.Roles, roleErr = r.GetUserRoles(ctx, u.ID)
	if roleErr != nil {
		return nil, roleErr
	}
	return u, nil
}

// UpdateLastLogin sets last_login_at to now.
func (r *UserRepository) UpdateLastLogin(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET last_login_at = $1 WHERE id = $2`,
		time.Now(), userID,
	)
	return err
}

// GetUserRoles returns the list of role names for a user.
func (r *UserRepository) GetUserRoles(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT r.name FROM roles r
		 INNER JOIN user_roles ur ON ur.role_id = r.id
		 WHERE ur.user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get user roles: %w", err)
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		roles = append(roles, name)
	}
	return roles, rows.Err()
}

// AssignDefaultRole assigns the "viewer" role to a newly registered user.
func (r *UserRepository) AssignDefaultRole(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO user_roles (user_id, role_id)
		 SELECT $1, id FROM roles WHERE name = 'viewer'`,
		userID,
	)
	return err
}

// ─── Admin: user management ─────────────────────────────────────────────────────

// ListUsers returns all non-deleted users (roles populated per user).
func (r *UserRepository) ListUsers(ctx context.Context) ([]*model.User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, username, email, is_active, last_login_at, created_at, updated_at
		 FROM users WHERE deleted_at IS NULL ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*model.User
	for rows.Next() {
		u := &model.User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.IsActive,
			&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, u := range users {
		roles, err := r.GetUserRoles(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		u.Roles = roles
	}
	return users, nil
}

// SetUserActive activates or deactivates a user account.
func (r *UserRepository) SetUserActive(ctx context.Context, userID string, active bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE users SET is_active = $1, updated_at = $2 WHERE id = $3 AND deleted_at IS NULL`,
		active, time.Now(), userID,
	)
	if err != nil {
		return fmt.Errorf("set user active: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetUserRoles replaces a user's roles with the given role names.
// Unknown role names are ignored. Runs in a transaction.
func (r *UserRepository) SetUserRoles(ctx context.Context, userID string, roleNames []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("clear user roles: %w", err)
	}

	for _, name := range roleNames {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_roles (user_id, role_id)
			 SELECT $1, id FROM roles WHERE name = $2`,
			userID, name,
		); err != nil {
			return fmt.Errorf("assign role %q: %w", name, err)
		}
	}
	return tx.Commit()
}

// CountAdmins returns how many active (non-deleted) users hold the admin role.
func (r *UserRepository) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT u.id) FROM users u
		 INNER JOIN user_roles ur ON ur.user_id = u.id
		 INNER JOIN roles r ON r.id = ur.role_id
		 WHERE r.name = 'admin' AND u.is_active = 1 AND u.deleted_at IS NULL`,
	).Scan(&n)
	return n, err
}

// GetAllRoles returns every role defined in the system.
func (r *UserRepository) GetAllRoles(ctx context.Context) ([]model.Role, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, description, created_at FROM roles ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("get all roles: %w", err)
	}
	defer rows.Close()

	var roles []model.Role
	for rows.Next() {
		var role model.Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// ─── Refresh Tokens ───────────────────────────────────────────────────────────

// HashToken returns the SHA-256 hex of a raw token string.
func HashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// CreateRefreshToken inserts a new refresh token record.
func (r *UserRepository) CreateRefreshToken(ctx context.Context, rt *model.RefreshToken) error {
	rt.ID = uuid.New().String()
	rt.IssuedAt = time.Now()

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, issued_at, expires_at, user_agent, ip_address)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		rt.ID, rt.UserID, rt.TokenHash, rt.IssuedAt, rt.ExpiresAt, rt.UserAgent, rt.IPAddress,
	)
	if err != nil {
		return fmt.Errorf("create refresh token: %w", err)
	}
	return nil
}

// GetRefreshToken fetches a token record by its hash.
func (r *UserRepository) GetRefreshToken(ctx context.Context, tokenHash string) (*model.RefreshToken, error) {
	rt := &model.RefreshToken{}
		err := r.db.QueryRowContext(ctx,
			`SELECT id, user_id, token_hash, issued_at, expires_at, revoked_at
			 FROM refresh_tokens WHERE token_hash = $1`,
			tokenHash,
	).Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.IssuedAt, &rt.ExpiresAt, &rt.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get refresh token: %w", err)
	}
	return rt, nil
}

// RevokeRefreshToken marks a token as revoked.
func (r *UserRepository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = $1 WHERE token_hash = $2`,
		time.Now(), tokenHash,
	)
	return err
}

// RevokeAllUserTokens revokes all refresh tokens for a user (used on logout).
func (r *UserRepository) RevokeAllUserTokens(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = $1
		 WHERE user_id = $2 AND revoked_at IS NULL`,
		time.Now(), userID,
	)
	return err
}

// ─── Data Retention ───────────────────────────────────────────────────────────

// DeleteExpiredRefreshTokens removes refresh tokens that expired more than 1 day ago.
func (r *UserRepository) DeleteExpiredRefreshTokens(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE expires_at < NOW() - INTERVAL 1 DAY`,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// SoftDeleteInactiveUsers marks users inactive for more than 365 days as deleted.
func (r *UserRepository) SoftDeleteInactiveUsers(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE users SET deleted_at = NOW()
		 WHERE last_login_at < NOW() - INTERVAL 365 DAY
		   AND deleted_at IS NULL`,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// EmailExists returns true if the email is already registered.
func (r *UserRepository) EmailExists(ctx context.Context, email string) (bool, error) {
	var count int
		err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE email = $1`, email,
		).Scan(&count)
	return count > 0, err
}

// UsernameExists returns true if the username is already taken.
func (r *UserRepository) UsernameExists(ctx context.Context, username string) (bool, error) {
	var count int
		err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE username = $1`, username,
		).Scan(&count)
	return count > 0, err
}

// UsernameExistsExcept returns true if the username is taken by a different user.
func (r *UserRepository) UsernameExistsExcept(ctx context.Context, username, excludeID string) (bool, error) {
	var count int
		err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE username = $1 AND id != $2 AND deleted_at IS NULL`, username, excludeID,
		).Scan(&count)
	return count > 0, err
}

// EmailExistsExcept returns true if the email is taken by a different user.
func (r *UserRepository) EmailExistsExcept(ctx context.Context, email, excludeID string) (bool, error) {
	var count int
		err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE email = $1 AND id != $2 AND deleted_at IS NULL`, email, excludeID,
		).Scan(&count)
	return count > 0, err
}

// UpdateProfile updates username and/or email for a user.
func (r *UserRepository) UpdateProfile(ctx context.Context, userID, username, email string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET username = $1, email = $2, updated_at = $3 WHERE id = $4 AND deleted_at IS NULL`,
		username, email, time.Now(), userID,
	)
	if err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	return nil
}

// UpdatePasswordHash replaces the password hash for a user.
func (r *UserRepository) UpdatePasswordHash(ctx context.Context, userID, newHash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET password_hash = $1, updated_at = $2 WHERE id = $3 AND deleted_at IS NULL`,
		newHash, time.Now(), userID,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

// SoftDeleteUser marks the user as deleted and deactivates their account.
func (r *UserRepository) SoftDeleteUser(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET deleted_at = $1, is_active = 0, updated_at = $2 WHERE id = $3 AND deleted_at IS NULL`,
		time.Now(), time.Now(), userID,
	)
	if err != nil {
		return fmt.Errorf("soft delete user: %w", err)
	}
	return nil
}

// GetActiveSessions returns all non-revoked, non-expired refresh tokens for a user.
func (r *UserRepository) GetActiveSessions(ctx context.Context, userID string) ([]*model.RefreshToken, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, token_hash, issued_at, expires_at, revoked_at, user_agent, ip_address
		 FROM refresh_tokens
		 WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW()
		 ORDER BY issued_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get active sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*model.RefreshToken
	for rows.Next() {
		rt := &model.RefreshToken{}
		if err := rows.Scan(
			&rt.ID, &rt.UserID, &rt.TokenHash,
			&rt.IssuedAt, &rt.ExpiresAt, &rt.RevokedAt,
			&rt.UserAgent, &rt.IPAddress,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, rt)
	}
	return sessions, rows.Err()
}
