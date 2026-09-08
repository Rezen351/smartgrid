package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/almuzky/iot/services/auth/internal/config"
	"github.com/almuzky/iot/services/auth/internal/middleware"
	"github.com/almuzky/iot/services/auth/internal/model"
	"github.com/almuzky/iot/services/auth/internal/repository"
	"github.com/almuzky/iot/services/auth/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type fakeRepo struct {
	users           map[string]*model.User
	byIdentifier    map[string]*model.User
	roles           []model.Role
	refreshTokens   map[string]*model.RefreshToken
	activeSessions  []*model.RefreshToken
	adminCount      int
	err             error
	getByIDErr      error
	emailTaken      bool
	usernameTaken   bool
	emailTakenEx    bool
	usernameTakenEx bool
	deleted         []string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		users:         map[string]*model.User{},
		byIdentifier:  map[string]*model.User{},
		roles:         []model.Role{{Name: "viewer"}, {Name: "operator"}, {Name: "admin"}},
		refreshTokens: map[string]*model.RefreshToken{},
		adminCount:    1,
	}
}

func (f *fakeRepo) CreateUser(ctx context.Context, u *model.User) error {
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	f.users[u.ID] = u
	f.byIdentifier[u.Email] = u
	f.byIdentifier[u.Username] = u
	return f.err
}
func (f *fakeRepo) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	u, ok := f.byIdentifier[email]
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
	if u, ok := f.users[userID]; ok {
		return u.Roles, nil
	}
	return nil, nil
}
func (f *fakeRepo) AssignDefaultRole(ctx context.Context, userID string) error { return f.err }
func (f *fakeRepo) ListUsers(ctx context.Context) ([]*model.User, error) {
	out := make([]*model.User, 0, len(f.users))
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, f.err
}
func (f *fakeRepo) SetUserActive(ctx context.Context, userID string, active bool) error {
	return f.err
}
func (f *fakeRepo) SetUserRoles(ctx context.Context, userID string, roleNames []string) error {
	return f.err
}
func (f *fakeRepo) CountAdmins(ctx context.Context) (int, error) { return f.adminCount, f.err }
func (f *fakeRepo) GetAllRoles(ctx context.Context) ([]model.Role, error) {
	return f.roles, f.err
}
func (f *fakeRepo) CreateRefreshToken(ctx context.Context, rt *model.RefreshToken) error {
	f.refreshTokens[rt.TokenHash] = rt
	return f.err
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
func (f *fakeRepo) RevokeRefreshToken(ctx context.Context, tokenHash string) error { return f.err }
func (f *fakeRepo) RevokeAllUserTokens(ctx context.Context, userID string) error   { return f.err }
func (f *fakeRepo) DeleteExpiredRefreshTokens(ctx context.Context) (int64, error)  { return 0, f.err }
func (f *fakeRepo) SoftDeleteInactiveUsers(ctx context.Context) (int64, error)     { return 0, f.err }
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
	return f.emailTakenEx, f.err
}
func (f *fakeRepo) UpdateProfile(ctx context.Context, userID, username, email string) error {
	return f.err
}
func (f *fakeRepo) UpdatePasswordHash(ctx context.Context, userID, newHash string) error {
	return f.err
}
func (f *fakeRepo) SoftDeleteUser(ctx context.Context, userID string) error {
	f.deleted = append(f.deleted, userID)
	return f.err
}
func (f *fakeRepo) GetActiveSessions(ctx context.Context, userID string) ([]*model.RefreshToken, error) {
	return f.activeSessions, f.err
}

type fakeNATS struct{ count int }

func (n *fakeNATS) Publish(subject string, data []byte) error { n.count++; return nil }

func testCfg() *config.Config {
	return &config.Config{JWTSecret: "s", JWTExpiry: 15 * time.Minute, RefreshExpiry: 168 * time.Hour}
}

func withUserID(req *http.Request, id string) *http.Request {
	ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, id)
	return req.WithContext(ctx)
}

func newSvc(t *testing.T, repo service.UserRepository) *service.AuthService {
	t.Helper()
	return service.NewAuthService(repo, testCfg(), &fakeNATS{})
}

func TestHandlerRegister(t *testing.T) {
	h := NewAuthHandler(newSvc(t, newFakeRepo()))
	body := `{"email":"a@b.com","username":"alice","password":"password1"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(body))
	h.Register(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandlerRegisterValidation(t *testing.T) {
	cases := []string{
		`{"email":"","username":"alice","password":"password1"}`,
		`{"email":"a@b.com","username":"","password":"password1"}`,
		`{"email":"a@b.com","username":"alice","password":"short"}`,
		`not json`,
	}
	for _, body := range cases {
		h := NewAuthHandler(newSvc(t, newFakeRepo()))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(body))
		h.Register(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: expected 400, got %d", body, rec.Code)
		}
	}
}

func TestHandlerLogin(t *testing.T) {
	repo := newFakeRepo()
	repo.byIdentifier["alice"] = &model.User{ID: "u1", Username: "alice", PasswordHash: hash(t, "password1"), IsActive: true, Roles: []string{"viewer"}}
	h := NewAuthHandler(newSvc(t, repo))
	body := `{"identifier":"alice","password":"password1"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(body))
	h.Login(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandlerLoginWrongPassword(t *testing.T) {
	repo := newFakeRepo()
	repo.byIdentifier["alice"] = &model.User{ID: "u1", Username: "alice", PasswordHash: hash(t, "password1"), IsActive: true}
	h := NewAuthHandler(newSvc(t, repo))
	body := `{"login_id":"alice","password":"wrong"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(body))
	h.Login(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandlerRefreshMissingToken(t *testing.T) {
	h := NewAuthHandler(newSvc(t, newFakeRepo()))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewBufferString(`{}`))
	h.Refresh(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandlerLogoutNoUser(t *testing.T) {
	h := NewAuthHandler(newSvc(t, newFakeRepo()))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	h.Logout(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandlerMe(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Username: "alice"}
	h := NewAuthHandler(newSvc(t, repo))
	rec := httptest.NewRecorder()
	req := withUserID(httptest.NewRequest(http.MethodGet, "/auth/me", nil), "u1")
	h.Me(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandlerUpdateProfile(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Username: "alice"}
	h := NewAuthHandler(newSvc(t, repo))
	body := `{"username":"alice2"}`
	rec := httptest.NewRecorder()
	req := withUserID(httptest.NewRequest(http.MethodPut, "/auth/me", bytes.NewBufferString(body)), "u1")
	h.UpdateProfile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandlerUpdateProfileNothing(t *testing.T) {
	h := NewAuthHandler(newSvc(t, newFakeRepo()))
	rec := httptest.NewRecorder()
	req := withUserID(httptest.NewRequest(http.MethodPut, "/auth/me", bytes.NewBufferString(`{}`)), "u1")
	h.UpdateProfile(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandlerChangePassword(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", PasswordHash: hash(t, "current")}
	h := NewAuthHandler(newSvc(t, repo))
	body := `{"current_password":"current","new_password":"newlongenough"}`
	rec := httptest.NewRecorder()
	req := withUserID(httptest.NewRequest(http.MethodPut, "/auth/password", bytes.NewBufferString(body)), "u1")
	h.ChangePassword(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandlerChangePasswordWrong(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", PasswordHash: hash(t, "current")}
	h := NewAuthHandler(newSvc(t, repo))
	body := `{"current_password":"wrong","new_password":"newlongenough"}`
	rec := httptest.NewRecorder()
	req := withUserID(httptest.NewRequest(http.MethodPut, "/auth/password", bytes.NewBufferString(body)), "u1")
	h.ChangePassword(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandlerDeleteAccount(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", PasswordHash: hash(t, "current")}
	h := NewAuthHandler(newSvc(t, repo))
	body := `{"password":"current"}`
	rec := httptest.NewRecorder()
	req := withUserID(httptest.NewRequest(http.MethodDelete, "/auth/account", bytes.NewBufferString(body)), "u1")
	h.DeleteAccount(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandlerDeleteAccountNoPassword(t *testing.T) {
	h := NewAuthHandler(newSvc(t, newFakeRepo()))
	rec := httptest.NewRecorder()
	req := withUserID(httptest.NewRequest(http.MethodDelete, "/auth/account", bytes.NewBufferString(`{}`)), "u1")
	h.DeleteAccount(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandlerGetSessions(t *testing.T) {
	repo := newFakeRepo()
	repo.activeSessions = []*model.RefreshToken{{ID: "s1"}}
	h := NewAuthHandler(newSvc(t, repo))
	rec := httptest.NewRecorder()
	req := withUserID(httptest.NewRequest(http.MethodGet, "/auth/sessions", nil), "u1")
	h.GetSessions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandlerListUsers(t *testing.T) {
	h := NewAuthHandler(newSvc(t, newFakeRepo()))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/users", nil)
	h.ListUsers(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandlerGetUserNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.getByIDErr = repository.ErrNotFound
	h := NewAuthHandler(newSvc(t, repo))
	rec := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodGet, "/auth/users/x", nil), map[string]string{"id": "x"})
	h.GetUser(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandlerGetUserSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Username: "alice"}
	h := NewAuthHandler(newSvc(t, repo))
	rec := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodGet, "/auth/users/u1", nil), map[string]string{"id": "u1"})
	h.GetUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandlerListRoles(t *testing.T) {
	h := NewAuthHandler(newSvc(t, newFakeRepo()))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/roles", nil)
	h.ListRoles(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandlerUpdateUser(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Roles: []string{"admin"}}
	repo.users["u2"] = &model.User{ID: "u2", Roles: []string{"viewer"}}
	h := NewAuthHandler(newSvc(t, repo))
	body := `{"is_active":false,"roles":["operator"]}`
	rec := httptest.NewRecorder()
	req := withUserID(withURLParam(httptest.NewRequest(http.MethodPut, "/auth/users/u2", bytes.NewBufferString(body)), map[string]string{"id": "u2"}), "u1")
	h.UpdateUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandlerUpdateUserNothing(t *testing.T) {
	h := NewAuthHandler(newSvc(t, newFakeRepo()))
	rec := httptest.NewRecorder()
	req := withUserID(withURLParam(httptest.NewRequest(http.MethodPut, "/auth/users/u2", bytes.NewBufferString(`{}`)), map[string]string{"id": "u2"}), "u1")
	h.UpdateUser(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandlerUpdateUserLastAdmin(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Roles: []string{"admin"}}
	repo.adminCount = 1
	h := NewAuthHandler(newSvc(t, repo))
	body := `{"is_active":false}`
	rec := httptest.NewRecorder()
	req := withUserID(withURLParam(httptest.NewRequest(http.MethodPut, "/auth/users/u1", bytes.NewBufferString(body)), map[string]string{"id": "u1"}), "u2")
	h.UpdateUser(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}

func TestHandlerDeleteUser(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Roles: []string{"admin"}}
	repo.users["u2"] = &model.User{ID: "u2", Roles: []string{"viewer"}}
	repo.adminCount = 2
	h := NewAuthHandler(newSvc(t, repo))
	rec := httptest.NewRecorder()
	req := withUserID(withURLParam(httptest.NewRequest(http.MethodDelete, "/auth/users/u2", nil), map[string]string{"id": "u2"}), "u1")
	h.DeleteUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandlerDeleteUserSelf(t *testing.T) {
	repo := newFakeRepo()
	repo.users["u1"] = &model.User{ID: "u1", Roles: []string{"admin"}}
	h := NewAuthHandler(newSvc(t, repo))
	rec := httptest.NewRecorder()
	req := withUserID(withURLParam(httptest.NewRequest(http.MethodDelete, "/auth/users/u1", nil), map[string]string{"id": "u1"}), "u1")
	h.DeleteUser(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestHandlerHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	Health(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// withURLParam injects chi URL params into the request context.
func withURLParam(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func hash(t *testing.T, pw string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pw), 4)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}
