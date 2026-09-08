package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/almuzky/iot/services/auth/internal/config"
	"github.com/almuzky/iot/services/auth/internal/model"
	"github.com/almuzky/iot/services/auth/internal/repository"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Sentinel errors
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserInactive       = errors.New("user account is inactive")
	ErrTokenInvalid       = errors.New("refresh token is invalid or expired")
	ErrEmailTaken         = errors.New("email is already registered")
	ErrUsernameTaken      = errors.New("username is already taken")
	ErrWrongPassword      = errors.New("current password is incorrect")
	ErrWeakPassword       = errors.New("password must be at least 8 characters")
	ErrUserNotFound       = errors.New("user not found")
	ErrCannotModifySelf   = errors.New("you cannot deactivate or demote your own account")
	ErrLastAdmin          = errors.New("cannot remove the last active admin")
	ErrInvalidRole        = errors.New("one or more roles are invalid")
)

// Claims extends jwt.RegisteredClaims with user-specific fields.
type Claims struct {
	UserID   string   `json:"uid"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

// UserRepository is the persistence seam for users, roles, and tokens. The
// concrete implementation is *repository.UserRepository; unit tests inject a
// fake.
type UserRepository interface {
	CreateUser(ctx context.Context, u *model.User) error
	GetUserByEmail(ctx context.Context, email string) (*model.User, error)
	GetUserByIdentifier(ctx context.Context, identifier string) (*model.User, error)
	GetUserByID(ctx context.Context, id string) (*model.User, error)
	UpdateLastLogin(ctx context.Context, userID string) error
	GetUserRoles(ctx context.Context, userID string) ([]string, error)
	AssignDefaultRole(ctx context.Context, userID string) error
	ListUsers(ctx context.Context) ([]*model.User, error)
	SetUserActive(ctx context.Context, userID string, active bool) error
	SetUserRoles(ctx context.Context, userID string, roleNames []string) error
	CountAdmins(ctx context.Context) (int, error)
	GetAllRoles(ctx context.Context) ([]model.Role, error)
	CreateRefreshToken(ctx context.Context, rt *model.RefreshToken) error
	GetRefreshToken(ctx context.Context, tokenHash string) (*model.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	RevokeAllUserTokens(ctx context.Context, userID string) error
	DeleteExpiredRefreshTokens(ctx context.Context) (int64, error)
	SoftDeleteInactiveUsers(ctx context.Context) (int64, error)
	EmailExists(ctx context.Context, email string) (bool, error)
	UsernameExists(ctx context.Context, username string) (bool, error)
	UsernameExistsExcept(ctx context.Context, username, excludeID string) (bool, error)
	EmailExistsExcept(ctx context.Context, email, excludeID string) (bool, error)
	UpdateProfile(ctx context.Context, userID, username, email string) error
	UpdatePasswordHash(ctx context.Context, userID, newHash string) error
	SoftDeleteUser(ctx context.Context, userID string) error
	GetActiveSessions(ctx context.Context, userID string) ([]*model.RefreshToken, error)
}

// AuthService handles authentication and token lifecycle.
type AuthService struct {
	repo UserRepository
	cfg  *config.Config
	nats NATSPublisher
}

// NATSPublisher is a minimal interface for publishing audit events.
type NATSPublisher interface {
	Publish(subject string, data []byte) error
}

func NewAuthService(repo UserRepository, cfg *config.Config, nats NATSPublisher) *AuthService {
	return &AuthService{repo: repo, cfg: cfg, nats: nats}
}

// ─── Register ─────────────────────────────────────────────────────────────────

// Register creates a new user with the default "viewer" role.
func (s *AuthService) Register(ctx context.Context, req model.RegisterRequest, ip, ua string) (*model.TokenPair, error) {
	// Validate uniqueness
	if exists, _ := s.repo.EmailExists(ctx, req.Email); exists {
		return nil, ErrEmailTaken
	}
	if exists, _ := s.repo.UsernameExists(ctx, req.Username); exists {
		return nil, ErrUsernameTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &model.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: string(hash),
	}
	if err := s.repo.CreateUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.repo.AssignDefaultRole(ctx, user.ID); err != nil {
		return nil, err
	}

	user.Roles = []string{model.RoleViewer}

	pair, err := s.issueTokenPair(ctx, user, ip, ua)
	if err != nil {
		return nil, err
	}

	s.publishAudit("auth.register", map[string]string{
		"user_id":  user.ID,
		"username": user.Username,
		"ip":       ip,
	})

	return pair, nil
}

// ─── Login ────────────────────────────────────────────────────────────────────

// Login validates credentials and returns a new token pair.
// The identifier may be either an email or a username.
func (s *AuthService) Login(ctx context.Context, req model.LoginRequest, ip, ua string) (*model.TokenPair, error) {
	loginID := req.LoginID()
	user, err := s.repo.GetUserByIdentifier(ctx, loginID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if !user.IsActive {
		return nil, ErrUserInactive
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		s.publishAudit("auth.login.failed", map[string]string{"identifier": loginID, "ip": ip})
		return nil, ErrInvalidCredentials
	}

	_ = s.repo.UpdateLastLogin(ctx, user.ID)

	pair, err := s.issueTokenPair(ctx, user, ip, ua)
	if err != nil {
		return nil, err
	}

	s.publishAudit("auth.login", map[string]string{
		"user_id":  user.ID,
		"username": user.Username,
		"ip":       ip,
	})

	return pair, nil
}

// ─── Refresh ──────────────────────────────────────────────────────────────────

// Refresh rotates a refresh token: revokes the old one and issues a new pair.
func (s *AuthService) Refresh(ctx context.Context, rawToken, ip, ua string) (*model.TokenPair, error) {
	hash := repository.HashToken(rawToken)

	rt, err := s.repo.GetRefreshToken(ctx, hash)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrTokenInvalid
	}
	if err != nil {
		return nil, err
	}
	if !rt.IsValid() {
		return nil, ErrTokenInvalid
	}

	// Revoke old token (rotation)
	if err := s.repo.RevokeRefreshToken(ctx, hash); err != nil {
		return nil, err
	}

	user, err := s.repo.GetUserByID(ctx, rt.UserID)
	if err != nil {
		return nil, err
	}

	pair, err := s.issueTokenPair(ctx, user, ip, ua)
	if err != nil {
		return nil, err
	}

	s.publishAudit("auth.refresh", map[string]string{
		"user_id": user.ID,
		"ip":      ip,
	})

	return pair, nil
}

// ─── Logout ───────────────────────────────────────────────────────────────────

// Logout revokes all active refresh tokens for the user.
func (s *AuthService) Logout(ctx context.Context, userID, ip string) error {
	if err := s.repo.RevokeAllUserTokens(ctx, userID); err != nil {
		return err
	}
	s.publishAudit("auth.logout", map[string]string{
		"user_id": userID,
		"ip":      ip,
	})
	return nil
}

// ─── GetMe ────────────────────────────────────────────────────────────────────

// GetMe returns the profile for the given user ID.
func (s *AuthService) GetMe(ctx context.Context, userID string) (*model.MeResponse, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &model.MeResponse{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		Roles:       user.Roles,
		IsActive:    user.IsActive,
		LastLoginAt: user.LastLoginAt,
		CreatedAt:   user.CreatedAt,
	}, nil
}

// ─── Account Management ───────────────────────────────────────────────────────

// UpdateProfile updates the username and/or email for the authenticated user.
func (s *AuthService) UpdateProfile(ctx context.Context, userID string, req model.UpdateProfileRequest, ip string) (*model.MeResponse, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Use current values as defaults if fields are empty.
	newUsername := user.Username
	if req.Username != "" {
		newUsername = req.Username
	}
	newEmail := user.Email
	if req.Email != "" {
		newEmail = req.Email
	}

	// Uniqueness checks (exclude the current user).
	if req.Username != "" && req.Username != user.Username {
		if taken, _ := s.repo.UsernameExistsExcept(ctx, req.Username, userID); taken {
			return nil, ErrUsernameTaken
		}
	}
	if req.Email != "" && req.Email != user.Email {
		if taken, _ := s.repo.EmailExistsExcept(ctx, req.Email, userID); taken {
			return nil, ErrEmailTaken
		}
	}

	if err := s.repo.UpdateProfile(ctx, userID, newUsername, newEmail); err != nil {
		return nil, err
	}

	s.publishAudit("auth.profile.updated", map[string]string{
		"user_id": userID,
		"ip":      ip,
	})

	// Return refreshed profile.
	user.Username = newUsername
	user.Email = newEmail
	return &model.MeResponse{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		Roles:       user.Roles,
		IsActive:    user.IsActive,
		LastLoginAt: user.LastLoginAt,
		CreatedAt:   user.CreatedAt,
	}, nil
}

// ChangePassword verifies the current password and replaces it with a new one.
// All existing sessions are revoked on success (force re-login on other devices).
func (s *AuthService) ChangePassword(ctx context.Context, userID string, req model.ChangePasswordRequest, ip string) error {
	if len(req.NewPassword) < 8 {
		return ErrWeakPassword
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	// Verify current password.
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		return ErrWrongPassword
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}

	if err := s.repo.UpdatePasswordHash(ctx, userID, string(newHash)); err != nil {
		return err
	}

	// Revoke all sessions — user must re-login on all devices.
	_ = s.repo.RevokeAllUserTokens(ctx, userID)

	s.publishAudit("auth.password.changed", map[string]string{
		"user_id": userID,
		"ip":      ip,
	})
	return nil
}

// DeleteAccount soft-deletes the user and revokes all their sessions.
func (s *AuthService) DeleteAccount(ctx context.Context, userID, currentPassword, ip string) error {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	// Require password confirmation before deleting.
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return ErrWrongPassword
	}

	_ = s.repo.RevokeAllUserTokens(ctx, userID)

	if err := s.repo.SoftDeleteUser(ctx, userID); err != nil {
		return err
	}

	s.publishAudit("auth.account.deleted", map[string]string{
		"user_id": userID,
		"ip":      ip,
	})
	return nil
}

// GetSessions returns all active sessions (refresh tokens) for the user.
func (s *AuthService) GetSessions(ctx context.Context, userID string) ([]model.SessionResponse, error) {
	tokens, err := s.repo.GetActiveSessions(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := make([]model.SessionResponse, 0, len(tokens))
	for _, t := range tokens {
		result = append(result, model.SessionResponse{
			ID:        t.ID,
			UserAgent: t.UserAgent,
			IPAddress: t.IPAddress,
			IssuedAt:  t.IssuedAt,
			ExpiresAt: t.ExpiresAt,
			RevokedAt: t.RevokedAt,
		})
	}
	return result, nil
}

// ─── Admin: user management ─────────────────────────────────────────────────────

func toUserSummary(u *model.User) model.UserSummary {
	return model.UserSummary{
		ID:          u.ID,
		Username:    u.Username,
		Email:       u.Email,
		Roles:       u.Roles,
		IsActive:    u.IsActive,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
	}
}

// ListUsers returns all users (admin view).
func (s *AuthService) ListUsers(ctx context.Context) ([]model.UserSummary, error) {
	users, err := s.repo.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.UserSummary, 0, len(users))
	for _, u := range users {
		out = append(out, toUserSummary(u))
	}
	return out, nil
}

// GetUser returns a single user by ID (admin view).
func (s *AuthService) GetUser(ctx context.Context, id string) (*model.UserSummary, error) {
	if id == "" {
		return nil, ErrUserNotFound
	}
	user, err := s.repo.GetUserByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	summary := toUserSummary(user)
	return &summary, nil
}

// ListRoles returns all roles defined in the system.
func (s *AuthService) ListRoles(ctx context.Context) ([]model.Role, error) {
	return s.repo.GetAllRoles(ctx)
}

// AdminUpdateUser changes a target user's active status and/or roles.
// actorID is the admin performing the action (used for self-modification guards).
func (s *AuthService) AdminUpdateUser(ctx context.Context, actorID, targetID string, req model.AdminUpdateUserRequest) (*model.UserSummary, error) {
	target, err := s.repo.GetUserByID(ctx, targetID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	hasAdmin := func(roles []string) bool {
		for _, r := range roles {
			if r == model.RoleAdmin {
				return true
			}
		}
		return false
	}
	targetIsAdmin := hasAdmin(target.Roles)

	// ── Guard: deactivation ──────────────────────────────────────────────
	if req.IsActive != nil && !*req.IsActive {
		if targetID == actorID {
			return nil, ErrCannotModifySelf
		}
		if targetIsAdmin {
			admins, err := s.repo.CountAdmins(ctx)
			if err != nil {
				return nil, err
			}
			if admins <= 1 {
				return nil, ErrLastAdmin
			}
		}
	}

	// ── Validate & apply roles ───────────────────────────────────────────
	if len(req.Roles) > 0 {
		allRoles, err := s.repo.GetAllRoles(ctx)
		if err != nil {
			return nil, err
		}
		valid := make(map[string]struct{}, len(allRoles))
		for _, r := range allRoles {
			valid[r.Name] = struct{}{}
		}
		for _, name := range req.Roles {
			if _, ok := valid[name]; !ok {
				return nil, ErrInvalidRole
			}
		}

		// Guard: removing admin from the last admin (or demoting self as last admin)
		if targetIsAdmin && !hasAdmin(req.Roles) {
			admins, err := s.repo.CountAdmins(ctx)
			if err != nil {
				return nil, err
			}
			if admins <= 1 {
				return nil, ErrLastAdmin
			}
			if targetID == actorID {
				return nil, ErrCannotModifySelf
			}
		}

		if err := s.repo.SetUserRoles(ctx, targetID, req.Roles); err != nil {
			return nil, err
		}
	}

	// ── Apply active status ──────────────────────────────────────────────
	if req.IsActive != nil {
		if err := s.repo.SetUserActive(ctx, targetID, *req.IsActive); err != nil {
			return nil, err
		}
		// Revoke sessions when deactivating so access stops immediately.
		if !*req.IsActive {
			_ = s.repo.RevokeAllUserTokens(ctx, targetID)
		}
	}

	s.publishAudit("auth.admin.user.updated", map[string]string{
		"actor_id":  actorID,
		"target_id": targetID,
	})

	// Return refreshed view.
	updated, err := s.repo.GetUserByID(ctx, targetID)
	if err != nil {
		return nil, err
	}
	summary := toUserSummary(updated)
	return &summary, nil
}

// AdminDeleteUser soft-deletes a user (admin action).
func (s *AuthService) AdminDeleteUser(ctx context.Context, actorID, targetID string) error {
	target, err := s.repo.GetUserByID(ctx, targetID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}

	if targetID == actorID {
		return ErrCannotModifySelf
	}

	for _, r := range target.Roles {
		if r == model.RoleAdmin {
			admins, err := s.repo.CountAdmins(ctx)
			if err != nil {
				return err
			}
			if admins <= 1 {
				return ErrLastAdmin
			}
			break
		}
	}

	_ = s.repo.RevokeAllUserTokens(ctx, targetID)
	if err := s.repo.SoftDeleteUser(ctx, targetID); err != nil {
		return err
	}

	s.publishAudit("auth.admin.user.deleted", map[string]string{
		"actor_id":  actorID,
		"target_id": targetID,
	})
	return nil
}

// ─── Token helpers ────────────────────────────────────────────────────────────

func (s *AuthService) issueTokenPair(ctx context.Context, user *model.User, ip, ua string) (*model.TokenPair, error) {
	now := time.Now()

	// Access token (JWT)
	claims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Roles:    user.Roles,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "auth-svc",
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.JWTExpiry)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err := token.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	// Refresh token (random 32-byte)
	rawRefresh, err := generateSecureToken(32)
	if err != nil {
		return nil, err
	}

	rt := &model.RefreshToken{
		UserID:    user.ID,
		TokenHash: repository.HashToken(rawRefresh),
		ExpiresAt: now.Add(s.cfg.RefreshExpiry),
		UserAgent: ua,
		IPAddress: ip,
	}
	if err := s.repo.CreateRefreshToken(ctx, rt); err != nil {
		return nil, err
	}

	return &model.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresIn:    int64(s.cfg.JWTExpiry.Seconds()),
	}, nil
}

// ValidateClaims parses and validates an access token, returning the claims.
func (s *AuthService) ValidateClaims(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// generateSecureToken returns a URL-safe base64-encoded random token of n bytes.
func generateSecureToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// ─── Audit helper ─────────────────────────────────────────────────────────────

func (s *AuthService) publishAudit(event string, fields map[string]string) {
	if s.nats == nil {
		return
	}
	payload := fmt.Sprintf(`{"event":%q,"data":%s}`, event, mapToJSON(fields))
	_ = s.nats.Publish("audit.log", []byte(payload))
}

func mapToJSON(m map[string]string) string {
	out := "{"
	first := true
	for k, v := range m {
		if !first {
			out += ","
		}
		out += fmt.Sprintf(`%q:%q`, k, v)
		first = false
	}
	return out + "}"
}
