package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/almuzky/iot/services/auth/internal/config"
	"github.com/almuzky/iot/services/auth/internal/model"
	"github.com/almuzky/iot/services/auth/internal/repository"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type fakeRepo struct {
	users            map[string]*model.User
	byEmail          map[string]*model.User
	byIdentifier     map[string]*model.User
	roles            []model.Role
	refreshTokens    map[string]*model.RefreshToken
	activeSessions   []*model.RefreshToken
	adminCount       int
	err              error
	getByIDErr       error
	createUserErr    error
	createTokenErr   error
	revokeAllErr     error
	emailTaken       bool
	usernameTaken    bool
	emailTakenExcept bool
	usernameTakenEx  bool
	deleted          []string
	publishCount     int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		users:         map[string]*model.User{},
		byEmail:       map[string]*model.User{},
		byIdentifier:  map[string]*model.User{},
		roles:         []model.Role{{Name: "viewer"}, {Name: "operator"}, {Name: "admin"}},
		refreshTokens: map[string]*model.RefreshToken{},
		adminCount:    1,
	}
}

func (f *fakeRepo) CreateUser(ctx context.Context, u *model.User) error {
	if f.createUserErr != nil {
		return f.createUserErr
	}
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	f.users[u.ID] = u
	f.byEmail[u.Email] = u
	f.byIdentifier[u.Email] = u
	f.byIdentifier[u.Username] = u
	return nil
}
func (f *fakeRepo) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	u, ok := f.byEmail[email]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return u, nil
}
func (f *fakeRepo) GetUserByIdentifier(ctx context.Context, id string) (*model.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	u, ok := f.byIdentifier[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return u, nil
}
func (f *fakeRepo) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	if f.getByIDErr != nil {
		return nil, f.getByIDErr
	}
	u, ok := f.users[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return u, nil
}
func (f *fakeRepo) UpdateLastLogin(ctx context.Context, userID string) error { return f.err }
func (f *fakeRepo) GetUserRoles(ctx context.Context, userID string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	if u, ok := f.users[userID]; ok {
		return u.Roles, nil
	}
	return nil, nil
}
func (f *fakeRepo) AssignDefaultRole(ctx context.Context, userID string) error { return f.err }
func (f *fakeRepo) ListUsers(ctx context.Context) ([]*model.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]*model.User, 0, len(f.users))
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}
func (f *fakeRepo) SetUserActive(ctx context.Context, userID string, active bool) error {
	if f.err != nil {
		return f.err
	}
	if u, ok := f.users[userID]; ok {
		u.IsActive = active
		f.users[userID] = u
	}
	return nil
}
func (f *fakeRepo) SetUserRoles(ctx context.Context, userID string, roleNames []string) error {
	if f.err != nil {
		return f.err
	}
	if u, ok := f.users[userID]; ok {
		u.Roles = roleNames
		f.users[userID] = u
	}
	return nil
}
func (f *fakeRepo) CountAdmins(ctx context.Context) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.adminCount, nil
}
func (f *fakeRepo) GetAllRoles(ctx context.Context) ([]model.Role, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.roles, nil
}
func (f *fakeRepo) CreateRefreshToken(ctx context.Context, rt *model.RefreshToken) error {
	if f.createTokenErr != nil {
		return f.createTokenErr
	}
	f.refreshTokens[rt.TokenHash] = rt
	return nil
}
func (f *fakeRepo) GetRefreshToken(ctx context.Context, tokenHash string) (*model.RefreshToken, error) {
	if f.err != nil {
		return nil, f.err
	}
	rt, ok := f.refreshTokens[tokenHash]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return rt, nil
}
func (f *fakeRepo) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	if f.err != nil {
		return f.err
	}
	if rt, ok := f.refreshTokens[tokenHash]; ok {
		now := time.Now()
		rt.RevokedAt = &now
	}
	return nil
}
func (f *fakeRepo) RevokeAllUserTokens(ctx context.Context, userID string) error {
	if f.revokeAllErr != nil {
		return f.revokeAllErr
	}
	return nil
}
func (f *fakeRepo) DeleteExpiredRefreshTokens(ctx context.Context) (int64, error) {
	return 0, f.err
}
func (f *fakeRepo) SoftDeleteInactiveUsers(ctx context.Context) (int64, error) {
	return 0, f.err
}
func (f *fakeRepo) EmailExists(ctx context.Context, email string) (bool, error) {
	return f.emailTaken, f.err
}
func (f *fakeRepo) UsernameExists(ctx context.Context, username string) (bool, error) {
	return f.usernameTaken, f.err
}
func (f *fakeRepo) UsernameExistsExcept(ctx context.Context, username, excludeID string) (bool, error) {
	return f.usernameTakenEx, f.err
}
func (f *fakeRepo) EmailExistsExcept(ctx context.Context, email, excludeID string) (bool, error) {
	return f.emailTakenExcept, f.err
}
func (f *fakeRepo) UpdateProfile(ctx context.Context, userID, username, email string) error {
	if f.err != nil {
		return f.err
	}
	if u, ok := f.users[userID]; ok {
		if username != "" {
			u.Username = username
		}
		if email != "" {
			u.Email = email
		}
		f.users[userID] = u
	}
	return nil
}
func (f *fakeRepo) UpdatePasswordHash(ctx context.Context, userID, newHash string) error {
	if f.err != nil {
		return f.err
	}
	if u, ok := f.users[userID]; ok {
		u.PasswordHash = newHash
		f.users[userID] = u
	}
	return nil
}
func (f *fakeRepo) SoftDeleteUser(ctx context.Context, userID string) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, userID)
	delete(f.users, userID)
	return nil
}
func (f *fakeRepo) GetActiveSessions(ctx context.Context, userID string) ([]*model.RefreshToken, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.activeSessions, nil
}

// fakeNATS counts publishes.
type fakeNATS struct {
	publishErr error
	count      int
}

func (n *fakeNATS) Publish(subject string, data []byte) error {
	if n.publishErr != nil {
		return n.publishErr
	}
	n.count++
	return nil
}

func testConfig() *config.Config {
	return &config.Config{
		JWTSecret:     "test-secret",
		JWTExpiry:     15 * time.Minute,
		RefreshExpiry: 168 * time.Hour,
	}
}

func hashPw(t *testing.T, pw string) string {
	t.Helper()
	// bcrypt hash of pw using cost 4 to keep tests fast.
	h, err := bcrypt.GenerateFromPassword([]byte(pw), 4)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

func registerUser(t *testing.T, svc *AuthService, email, username, pw string) *model.TokenPair {
	t.Helper()
	pair, err := svc.Register(context.Background(), model.RegisterRequest{Email: email, Username: username, Password: pw}, "1.2.3.4", "ua")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	return pair
}

func TestRegisterSuccess(t *testing.T) {
	repo := newFakeRepo()
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	pair := registerUser(t, svc, "a@b.com", "alice", "password1")
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected tokens")
	}
	if len(repo.users) != 1 {
		t.Errorf("expected 1 user persisted")
	}
}

func TestRegisterEmailTaken(t *testing.T) {
	repo := newFakeRepo()
	repo.emailTaken = true
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.Register(context.Background(), model.RegisterRequest{Email: "a@b.com", Username: "alice", Password: "password1"}, "ip", "ua")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

func TestRegisterUsernameTaken(t *testing.T) {
	repo := newFakeRepo()
	repo.usernameTaken = true
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.Register(context.Background(), model.RegisterRequest{Email: "a@b.com", Username: "alice", Password: "password1"}, "ip", "ua")
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestLoginSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.byIdentifier["alice"] = &model.User{ID: "u1", Username: "alice", Email: "a@b.com", PasswordHash: hashPw(t, "password1"), IsActive: true, Roles: []string{"viewer"}}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	pair, err := svc.Login(context.Background(), model.LoginRequest{Identifier: "alice", Password: "password1"}, "ip", "ua")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if pair.AccessToken == "" {
		t.Fatal("expected access token")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	repo := newFakeRepo()
	repo.byIdentifier["alice"] = &model.User{ID: "u1", Username: "alice", PasswordHash: hashPw(t, "password1"), IsActive: true}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.Login(context.Background(), model.LoginRequest{Identifier: "alice", Password: "wrong"}, "ip", "ua")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLoginUnknownUser(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.Login(context.Background(), model.LoginRequest{Identifier: "nobody", Password: "x"}, "ip", "ua")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLoginInactive(t *testing.T) {
	repo := newFakeRepo()
	repo.byIdentifier["alice"] = &model.User{ID: "u1", Username: "alice", PasswordHash: hashPw(t, "password1"), IsActive: false}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.Login(context.Background(), model.LoginRequest{Identifier: "alice", Password: "password1"}, "ip", "ua")
	if !errors.Is(err, ErrUserInactive) {
		t.Fatalf("expected ErrUserInactive, got %v", err)
	}
}

func TestRefreshSuccess(t *testing.T) {
	repo := newFakeRepo()
	raw := "somerawrefreshtoken"
	repo.refreshTokens[repository.HashToken(raw)] = &model.RefreshToken{UserID: "u1", RevokedAt: nil, ExpiresAt: time.Now().Add(time.Hour)}
	repo.users["u1"] = &model.User{ID: "u1", Username: "alice", Roles: []string{"viewer"}}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	pair, err := svc.Refresh(context.Background(), raw, "ip", "ua")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if pair.AccessToken == "" {
		t.Fatal("expected new access token")
	}
	// Old token revoked.
	if rt, _ := repo.refreshTokens[repository.HashToken(raw)]; rt.RevokedAt == nil {
		t.Error("expected old refresh token revoked")
	}
}

func TestRefreshInvalid(t *testing.T) {
	repo := newFakeRepo()
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.Refresh(context.Background(), "unknown", "ip", "ua")
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestRefreshExpired(t *testing.T) {
	repo := newFakeRepo()
	raw := "expiredtoken"
	repo.refreshTokens[repository.HashToken(raw)] = &model.RefreshToken{UserID: "u1", ExpiresAt: time.Now().Add(-time.Hour)}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.Refresh(context.Background(), raw, "ip", "ua")
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for expired, got %v", err)
	}
}

func TestLogout(t *testing.T) {
	repo := newFakeRepo()
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	if err := svc.Logout(context.Background(), "u1", "ip"); err != nil {
		t.Fatal(err)
	}
}

func TestGetMe(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Username: "alice"}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	me, err := svc.GetMe(context.Background(), "u1")
	if err != nil || me.Username != "alice" {
		t.Fatalf("expected me, got %v err %v", me, err)
	}
}

func TestUpdateProfileEmailTaken(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Username: "alice", Email: "a@b.com"}
	repo.emailTakenExcept = true
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.UpdateProfile(context.Background(), "u1", model.UpdateProfileRequest{Email: "new@b.com"}, "ip")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

func TestUpdateProfileSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Username: "alice", Email: "a@b.com"}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.UpdateProfile(context.Background(), "u1", model.UpdateProfileRequest{Username: "alice2"}, "ip")
	if err != nil {
		t.Fatal(err)
	}
	if repo.users["u1"].Username != "alice2" {
		t.Error("username not updated")
	}
}

func TestChangePasswordWeak(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	err := svc.ChangePassword(context.Background(), "u1", model.ChangePasswordRequest{CurrentPassword: "x", NewPassword: "short"}, "ip")
	if !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword, got %v", err)
	}
}

func TestChangePasswordWrongCurrent(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{PasswordHash: hashPw(t, "correct")}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	err := svc.ChangePassword(context.Background(), "u1", model.ChangePasswordRequest{CurrentPassword: "wrong", NewPassword: "longenough"}, "ip")
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got %v", err)
	}
}

func TestChangePasswordSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{PasswordHash: hashPw(t, "correct")}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	if err := svc.ChangePassword(context.Background(), "u1", model.ChangePasswordRequest{CurrentPassword: "correct", NewPassword: "longenough"}, "ip"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAccountWrongPassword(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{PasswordHash: hashPw(t, "correct")}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	err := svc.DeleteAccount(context.Background(), "u1", "wrong", "ip")
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got %v", err)
	}
}

func TestDeleteAccountSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{PasswordHash: hashPw(t, "correct")}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	if err := svc.DeleteAccount(context.Background(), "u1", "correct", "ip"); err != nil {
		t.Fatal(err)
	}
	if len(repo.deleted) != 1 {
		t.Error("expected user soft-deleted")
	}
}

func TestGetSessions(t *testing.T) {
	repo := newFakeRepo()
	repo.activeSessions = []*model.RefreshToken{{ID: "s1"}}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	sessions, err := svc.GetSessions(context.Background(), "u1")
	if err != nil || len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %v err %v", sessions, err)
	}
}

func TestListUsers(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1"}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	users, err := svc.ListUsers(context.Background())
	if err != nil || len(users) != 1 {
		t.Fatalf("expected 1 user, got %v err %v", users, err)
	}
}

func TestGetUserNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.getByIDErr = repository.ErrNotFound
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.GetUser(context.Background(), "missing")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestGetUserEmptyID(t *testing.T) {
	repo := newFakeRepo()
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.GetUser(context.Background(), "")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound for empty id, got %v", err)
	}
}

func TestListRoles(t *testing.T) {
	repo := newFakeRepo()
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	roles, err := svc.ListRoles(context.Background())
	if err != nil || len(roles) != 3 {
		t.Fatalf("expected 3 roles, got %v err %v", roles, err)
	}
}

func TestAdminUpdateUserLastAdminGuard(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Roles: []string{"admin"}}
	repo.adminCount = 1
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.AdminUpdateUser(context.Background(), "u2", "u1", model.AdminUpdateUserRequest{IsActive: boolPtr(false)})
	if !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin, got %v", err)
	}
}

func TestAdminUpdateUserCannotModifySelf(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Roles: []string{"admin"}}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.AdminUpdateUser(context.Background(), "u1", "u1", model.AdminUpdateUserRequest{IsActive: boolPtr(false)})
	if !errors.Is(err, ErrCannotModifySelf) {
		t.Fatalf("expected ErrCannotModifySelf, got %v", err)
	}
}

func TestAdminUpdateUserInvalidRole(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Roles: []string{"admin"}}
	repo.users["u2"] = &model.User{ID: "u2", Roles: []string{"viewer"}}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	_, err := svc.AdminUpdateUser(context.Background(), "u1", "u2", model.AdminUpdateUserRequest{Roles: []string{"superuser"}})
	if !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("expected ErrInvalidRole, got %v", err)
	}
}

func TestAdminUpdateUserSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Roles: []string{"admin"}}
	repo.users["u2"] = &model.User{ID: "u2", Roles: []string{"viewer"}}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	active := false
	updated, err := svc.AdminUpdateUser(context.Background(), "u1", "u2", model.AdminUpdateUserRequest{IsActive: &active, Roles: []string{"operator"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.IsActive {
		t.Error("expected user deactivated")
	}
	if len(updated.Roles) != 1 || updated.Roles[0] != "operator" {
		t.Error("expected operator role assigned")
	}
}

func TestAdminDeleteUserSelfGuard(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Roles: []string{"admin"}}
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	err := svc.AdminDeleteUser(context.Background(), "u1", "u1")
	if !errors.Is(err, ErrCannotModifySelf) {
		t.Fatalf("expected ErrCannotModifySelf, got %v", err)
	}
}

func TestAdminDeleteUserLastAdminGuard(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Roles: []string{"admin"}}
	repo.users["u2"] = &model.User{ID: "u2", Roles: []string{"admin"}}
	repo.adminCount = 1
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	err := svc.AdminDeleteUser(context.Background(), "u1", "u2")
	if !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin, got %v", err)
	}
}

func TestAdminDeleteUserSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Roles: []string{"admin"}}
	repo.users["u2"] = &model.User{ID: "u2", Roles: []string{"viewer"}}
	repo.adminCount = 2
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	if err := svc.AdminDeleteUser(context.Background(), "u1", "u2"); err != nil {
		t.Fatal(err)
	}
	if len(repo.deleted) != 1 {
		t.Error("expected user deleted")
	}
}

func TestValidateClaims(t *testing.T) {
	repo := newFakeRepo()
	svc := NewAuthService(repo, testConfig(), &fakeNATS{})
	// Issue a token via register, then validate it.
	pair := registerUser(t, svc, "v@b.com", "val", "password1")
	claims, err := svc.ValidateClaims(pair.AccessToken)
	if err != nil {
		t.Fatalf("validate claims failed: %v", err)
	}
	if claims.UserID == "" {
		t.Error("expected user id in claims")
	}
	// Garbage token should fail.
	if _, err := svc.ValidateClaims("garbage"); err == nil {
		t.Error("expected error for garbage token")
	}
}

func TestPublishAuditNilNATS(t *testing.T) {
	repo := newFakeRepo()
	svc := NewAuthService(repo, testConfig(), nil)
	// Should not panic when nats is nil.
	svc.publishAudit("auth.test", map[string]string{"k": "v"})
}

func boolPtr(b bool) *bool { return &b }
