package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/almuzky/iot/services/auth/internal/middleware"
	"github.com/almuzky/iot/services/auth/internal/model"
	"github.com/almuzky/iot/services/auth/internal/service"
	"github.com/go-chi/chi/v5"
)

// AuthHandler handles HTTP requests for authentication endpoints.
type AuthHandler struct {
	svc *service.AuthService
}

func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// ─── POST /auth/register ──────────────────────────────────────────────────────

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req model.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "username, email, and password are required")
		return
	}
	if len(req.Password) < 8 {
		respondError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	ip := realIP(r)
	ua := r.UserAgent()

	pair, err := h.svc.Register(r.Context(), req, ip, ua)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailTaken):
			respondError(w, http.StatusConflict, "email is already registered")
		case errors.Is(err, service.ErrUsernameTaken):
			respondError(w, http.StatusConflict, "username is already taken")
		default:
			respondError(w, http.StatusInternalServerError, "registration failed")
		}
		return
	}

	respond(w, http.StatusCreated, pair)
}

// ─── POST /auth/login ─────────────────────────────────────────────────────────

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req model.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ip := realIP(r)
	ua := r.UserAgent()

	pair, err := h.svc.Login(r.Context(), req, ip, ua)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			respondError(w, http.StatusUnauthorized, "invalid email or password")
		case errors.Is(err, service.ErrUserInactive):
			respondError(w, http.StatusForbidden, "account is inactive")
		default:
			respondError(w, http.StatusInternalServerError, "login failed")
		}
		return
	}

	respond(w, http.StatusOK, pair)
}

// ─── POST /auth/refresh ───────────────────────────────────────────────────────

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req model.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		respondError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	ip := realIP(r)
	ua := r.UserAgent()

	pair, err := h.svc.Refresh(r.Context(), req.RefreshToken, ip, ua)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTokenInvalid):
			respondError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		default:
			respondError(w, http.StatusInternalServerError, "token refresh failed")
		}
		return
	}

	respond(w, http.StatusOK, pair)
}

// ─── POST /auth/logout ────────────────────────────────────────────────────────

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	ip := realIP(r)
	if err := h.svc.Logout(r.Context(), userID, ip); err != nil {
		respondError(w, http.StatusInternalServerError, "logout failed")
		return
	}

	respond(w, http.StatusOK, map[string]string{"message": "logged out successfully"})
}

// ─── GET /auth/me ─────────────────────────────────────────────────────────────

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	me, err := h.svc.GetMe(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get user info")
		return
	}

	respond(w, http.StatusOK, me)
}

// ─── PUT /auth/me ─────────────────────────────────────────────────────────────

func (h *AuthHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req model.UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" && req.Email == "" {
		respondError(w, http.StatusBadRequest, "at least one field (username or email) must be provided")
		return
	}

	ip := realIP(r)
	updated, err := h.svc.UpdateProfile(r.Context(), userID, req, ip)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailTaken):
			respondError(w, http.StatusConflict, "email is already in use")
		case errors.Is(err, service.ErrUsernameTaken):
			respondError(w, http.StatusConflict, "username is already taken")
		default:
			respondError(w, http.StatusInternalServerError, "failed to update profile")
		}
		return
	}

	respond(w, http.StatusOK, updated)
}

// ─── PUT /auth/password ───────────────────────────────────────────────────────

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req model.ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		respondError(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}

	ip := realIP(r)
	if err := h.svc.ChangePassword(r.Context(), userID, req, ip); err != nil {
		switch {
		case errors.Is(err, service.ErrWrongPassword):
			respondError(w, http.StatusUnauthorized, "current password is incorrect")
		case errors.Is(err, service.ErrWeakPassword):
			respondError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		default:
			respondError(w, http.StatusInternalServerError, "failed to change password")
		}
		return
	}

	respond(w, http.StatusOK, map[string]string{"message": "password changed — please log in again"})
}

// ─── DELETE /auth/account ─────────────────────────────────────────────────────

func (h *AuthHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
		respondError(w, http.StatusBadRequest, "password confirmation is required")
		return
	}

	ip := realIP(r)
	if err := h.svc.DeleteAccount(r.Context(), userID, body.Password, ip); err != nil {
		switch {
		case errors.Is(err, service.ErrWrongPassword):
			respondError(w, http.StatusUnauthorized, "password confirmation failed")
		default:
			respondError(w, http.StatusInternalServerError, "failed to delete account")
		}
		return
	}

	respond(w, http.StatusOK, map[string]string{"message": "account deactivated successfully"})
}

// ─── GET /auth/sessions ───────────────────────────────────────────────────────

func (h *AuthHandler) GetSessions(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	sessions, err := h.svc.GetSessions(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get sessions")
		return
	}

	respond(w, http.StatusOK, map[string]any{"sessions": sessions, "count": len(sessions)})
}

// ─── GET /health ──────────────────────────────────────────────────────────────

func Health(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── Admin: user management ─────────────────────────────────────────────────────

// GET /auth/users
func (h *AuthHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.ListUsers(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	respond(w, http.StatusOK, map[string]any{"users": users, "count": len(users)})
}

// GET /auth/users/{id}
func (h *AuthHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "user id is required")
		return
	}

	user, err := h.svc.GetUser(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUserNotFound):
			respondError(w, http.StatusNotFound, "user not found")
		default:
			respondError(w, http.StatusInternalServerError, "failed to get user")
		}
		return
	}
	respond(w, http.StatusOK, user)
}

// GET /auth/roles
func (h *AuthHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.svc.ListRoles(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list roles")
		return
	}
	respond(w, http.StatusOK, map[string]any{"roles": roles})
}

// PUT /auth/users/{id}
func (h *AuthHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	actorID := middleware.UserIDFromContext(r.Context())
	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		respondError(w, http.StatusBadRequest, "user id is required")
		return
	}

	var req model.AdminUpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.IsActive == nil && len(req.Roles) == 0 {
		respondError(w, http.StatusBadRequest, "nothing to update (provide is_active and/or roles)")
		return
	}

	updated, err := h.svc.AdminUpdateUser(r.Context(), actorID, targetID, req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUserNotFound):
			respondError(w, http.StatusNotFound, "user not found")
		case errors.Is(err, service.ErrCannotModifySelf):
			respondError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, service.ErrLastAdmin):
			respondError(w, http.StatusConflict, err.Error())
		case errors.Is(err, service.ErrInvalidRole):
			respondError(w, http.StatusBadRequest, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "failed to update user")
		}
		return
	}
	respond(w, http.StatusOK, updated)
}

// DELETE /auth/users/{id}
func (h *AuthHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	actorID := middleware.UserIDFromContext(r.Context())
	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		respondError(w, http.StatusBadRequest, "user id is required")
		return
	}

	if err := h.svc.AdminDeleteUser(r.Context(), actorID, targetID); err != nil {
		switch {
		case errors.Is(err, service.ErrUserNotFound):
			respondError(w, http.StatusNotFound, "user not found")
		case errors.Is(err, service.ErrCannotModifySelf):
			respondError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, service.ErrLastAdmin):
			respondError(w, http.StatusConflict, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "failed to delete user")
		}
		return
	}
	respond(w, http.StatusOK, map[string]string{"message": "user deleted"})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Standard API response wrapper (AGENTS.md §4.4): { success, data }.
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": v})
}

// respondError emits the standard error envelope (AGENTS.md §4.4):
// { success:false, error:{ code, message } }.
func respondError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error":   map[string]string{"code": errorCode(status), "message": msg},
	})
}

// errorCode maps an HTTP status to a stable machine-readable error code.
func errorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "BAD_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusConflict:
		return "CONFLICT"
	default:
		return "INTERNAL_ERROR"
	}
}

// realIP extracts the client IP, respecting X-Forwarded-For from Kong.
func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}
