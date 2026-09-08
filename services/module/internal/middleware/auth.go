package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const (
	ContextKeyUserID contextKey = "user_id"
	ContextKeyRoles  contextKey = "roles"
)

var (
	errInvalidToken     = errors.New("invalid token")
	errInvalidSignature = errors.New("invalid signature")
	errExpired          = errors.New("token expired")
)

type claims struct {
	UID      string   `json:"uid"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	Exp      int64    `json:"exp"`
}

func base64urlEncode(b []byte) string {
	s := base64.StdEncoding.EncodeToString(b)
	s = strings.ReplaceAll(s, "+", "-")
	s = strings.ReplaceAll(s, "/", "_")
	return strings.TrimRight(s, "=")
}

func base64urlDecode(s string) ([]byte, error) {
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.StdEncoding.DecodeString(s)
}

func verifyHS256(token, secret string) (*claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errInvalidToken
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := base64urlEncode(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, errInvalidSignature
	}
	payload, err := base64urlDecode(parts[1])
	if err != nil {
		return nil, err
	}
	var c claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, err
	}
	if c.Exp > 0 && time.Now().Unix() > c.Exp {
		return nil, errExpired
	}
	return &c, nil
}

// JWTAuth validates the Bearer token using the shared JWT secret (HS256).
// When the secret is empty (dev), validation is skipped and requests pass
// through — Kong still fronts the service. When set, tokens are enforced.
func JWTAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				next.ServeHTTP(w, r)
				return
			}
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				unauthorized(w, "missing or invalid Authorization header")
				return
			}
			tokenStr := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			c, err := verifyHS256(tokenStr, secret)
			if err != nil {
				unauthorized(w, "invalid or expired token")
				return
			}
			ctx := context.WithValue(r.Context(), ContextKeyUserID, c.UID)
			ctx = context.WithValue(ctx, ContextKeyRoles, c.Roles)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole allows only users holding at least one of the given roles.
// When the JWT secret is disabled the request has no roles and is allowed.
func RequireRole(secret string, allowed ...string) func(http.Handler) http.Handler {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, role := range allowed {
		allowedSet[role] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				next.ServeHTTP(w, r)
				return
			}
			roles, _ := r.Context().Value(ContextKeyRoles).([]string)
			for _, role := range roles {
				if _, ok := allowedSet[role]; ok {
					next.ServeHTTP(w, r)
					return
				}
			}
			forbidden(w)
		})
	}
}

// UserIDFromContext extracts the user id set by JWTAuth (empty if absent).
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyUserID).(string)
	return v
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	// Standard error envelope (AGENTS.md §4.4).
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error":   map[string]string{"code": "UNAUTHORIZED", "message": msg},
	})
}

func forbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error":   map[string]string{"code": "FORBIDDEN", "message": "forbidden: insufficient role"},
	})
}
