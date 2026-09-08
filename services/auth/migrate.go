package main

import (
	"log"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// =============================================================================
// SINGLE SOURCE OF TRUTH — auth_db schema & seed data
//
// Go (GORM) is the ONLY authority for:
//   - Table definitions  → AutoMigrate below
//   - Seed data          → seedAll() below
//
// infra/mariadb/auth/init.sql is intentionally empty.
// Do NOT add DDL or DML there.
// =============================================================================

// ─── GORM table models ────────────────────────────────────────────────────────

type gormRole struct {
	ID          string    `gorm:"column:id;type:char(36);primaryKey"`
	Name        string    `gorm:"column:name;type:varchar(50);uniqueIndex;not null"`
	Description string    `gorm:"column:description;type:varchar(255)"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (gormRole) TableName() string { return "roles" }

type gormPermission struct {
	ID          string    `gorm:"column:id;type:char(36);primaryKey"`
	Resource    string    `gorm:"column:resource;type:varchar(100);not null"`
	Action      string    `gorm:"column:action;type:varchar(50);not null"`
	Description string    `gorm:"column:description;type:varchar(255)"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (gormPermission) TableName() string { return "permissions" }

type gormRolePermission struct {
	RoleID       string `gorm:"column:role_id;type:char(36);primaryKey"`
	PermissionID string `gorm:"column:permission_id;type:char(36);primaryKey"`
}

func (gormRolePermission) TableName() string { return "role_permissions" }

type gormUser struct {
	ID           string     `gorm:"column:id;type:char(36);primaryKey"`
	Username     string     `gorm:"column:username;type:varchar(100);uniqueIndex;not null"`
	Email        string     `gorm:"column:email;type:varchar(255);uniqueIndex;not null"`
	PasswordHash string     `gorm:"column:password_hash;type:varchar(255);not null"`
	IsActive     bool       `gorm:"column:is_active;not null;default:1"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at"`
	CreatedAt    time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt    *time.Time `gorm:"column:deleted_at;index"`
}

func (gormUser) TableName() string { return "users" }

type gormUserRole struct {
	UserID string `gorm:"column:user_id;type:char(36);primaryKey"`
	RoleID string `gorm:"column:role_id;type:char(36);primaryKey"`
}

func (gormUserRole) TableName() string { return "user_roles" }

type gormRefreshToken struct {
	ID        string     `gorm:"column:id;type:char(36);primaryKey"`
	UserID    string     `gorm:"column:user_id;type:char(36);not null;index"`
	TokenHash string     `gorm:"column:token_hash;type:varchar(255);uniqueIndex;not null"`
	IssuedAt  time.Time  `gorm:"column:issued_at;autoCreateTime"`
	ExpiresAt time.Time  `gorm:"column:expires_at;not null;index"`
	RevokedAt *time.Time `gorm:"column:revoked_at"`
	UserAgent string     `gorm:"column:user_agent;type:varchar(255)"`
	IPAddress string     `gorm:"column:ip_address;type:varchar(45)"`
}

func (gormRefreshToken) TableName() string { return "refresh_tokens" }

// ─── runMigrations ────────────────────────────────────────────────────────────

// runMigrations is called on every startup. It is fully idempotent:
//  1. AutoMigrate — create/alter tables to match the models above.
//  2. seedAll     — INSERT IGNORE static reference data.
//  3. seedAdmin   — create the default admin account if it does not exist.
func runMigrations(dsn string, admin AdminSeed) error {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}

	// ── Step 1: Schema ────────────────────────────────────────────────────────
	// Order matters: referenced tables must exist before tables with FK constraints.
	if err := db.AutoMigrate(
		&gormRole{},
		&gormPermission{},
		&gormRolePermission{},
		&gormUser{},
		&gormUserRole{},
		&gormRefreshToken{},
	); err != nil {
		return err
	}
	log.Println("[migrate] schema OK")

	// ── Step 2: Seed static reference data ───────────────────────────────────
	if err := seedAll(db); err != nil {
		return err
	}
	log.Println("[migrate] seed OK")

	// ── Step 3: Seed default admin account ───────────────────────────────────
	if err := seedAdmin(db, admin); err != nil {
		return err
	}

	return nil
}

// AdminSeed holds the default admin credentials seeded on first startup.
type AdminSeed struct {
	Username string
	Email    string
	Password string
}

// seedAdmin creates a default admin user (with the "admin" role) if no user
// with the configured admin email/username exists yet. Idempotent.
func seedAdmin(db *gorm.DB, admin AdminSeed) error {
	if admin.Username == "" || admin.Email == "" || admin.Password == "" {
		log.Println("[migrate] admin seed skipped (missing credentials)")
		return nil
	}

	var count int64
	if err := db.Model(&gormUser{}).
		Where("email = ? OR username = ?", admin.Email, admin.Username).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		log.Println("[migrate] admin account already exists — skip")
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(admin.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	adminID := uuid.New().String()
	now := time.Now()
	if err := db.Create(&gormUser{
		ID:           adminID,
		Username:     admin.Username,
		Email:        admin.Email,
		PasswordHash: string(hash),
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error; err != nil {
		return err
	}

	if err := db.Create(&gormUserRole{
		UserID: adminID,
		RoleID: "role-admin-001",
	}).Error; err != nil {
		return err
	}

	log.Printf("[migrate] default admin created: %s (%s) — CHANGE THE PASSWORD after first login", admin.Username, admin.Email)
	return nil
}

// seedAll inserts reference data using PostgreSQL upsert so it is safe to run
// repeatedly without duplicating rows.
func seedAll(db *gorm.DB) error {
	// ── Roles ─────────────────────────────────────────────────────────────────
	if err := db.Exec(`
		INSERT INTO roles (id, name, description, created_at) VALUES
		  ('role-admin-001',    'admin',    'Full access to all resources',          NOW()),
		  ('role-operator-001', 'operator', 'Manage devices and view telemetry',     NOW()),
		  ('role-viewer-001',   'viewer',   'Read-only access to telemetry and alerts', NOW())
		ON CONFLICT (name) DO NOTHING
	`).Error; err != nil {
		return err
	}

	// ── Permissions ───────────────────────────────────────────────────────────
	if err := db.Exec(`
		INSERT INTO permissions (id, resource, action, description, created_at) VALUES
		  ('perm-tel-read',    'telemetry', 'read',   'View telemetry data',                NOW()),
		  ('perm-tel-write',   'telemetry', 'write',  'Publish telemetry data',             NOW()),
		  ('perm-ctrl-read',   'control',   'read',   'View control commands',              NOW()),
		  ('perm-ctrl-write',  'control',   'write',  'Send control commands to devices',   NOW()),
		  ('perm-alert-read',  'alert',     'read',   'View alerts',                        NOW()),
		  ('perm-alert-ack',   'alert',     'ack',    'Acknowledge alerts',                 NOW()),
		  ('perm-user-admin',  'users',     'admin',  'Manage users roles permissions',     NOW()),
		  ('perm-stream-read', 'stream',    'read',   'View camera streams',                NOW())
		ON CONFLICT (id) DO NOTHING
	`).Error; err != nil {
		return err
	}

	// ── Role → Permission mappings ─────────────────────────────────────────────
	if err := db.Exec(`
		INSERT INTO role_permissions (role_id, permission_id) VALUES
		  -- admin: all permissions
		  ('role-admin-001', 'perm-tel-read'),
		  ('role-admin-001', 'perm-tel-write'),
		  ('role-admin-001', 'perm-ctrl-read'),
		  ('role-admin-001', 'perm-ctrl-write'),
		  ('role-admin-001', 'perm-alert-read'),
		  ('role-admin-001', 'perm-alert-ack'),
		  ('role-admin-001', 'perm-user-admin'),
		  ('role-admin-001', 'perm-stream-read'),
		  -- operator
		  ('role-operator-001', 'perm-tel-read'),
		  ('role-operator-001', 'perm-tel-write'),
		  ('role-operator-001', 'perm-ctrl-read'),
		  ('role-operator-001', 'perm-ctrl-write'),
		  ('role-operator-001', 'perm-alert-read'),
		  ('role-operator-001', 'perm-alert-ack'),
		  ('role-operator-001', 'perm-stream-read'),
		  -- viewer: read-only
		  ('role-viewer-001', 'perm-tel-read'),
		  ('role-viewer-001', 'perm-ctrl-read'),
		  ('role-viewer-001', 'perm-alert-read'),
		  ('role-viewer-001', 'perm-stream-read')
		ON CONFLICT (role_id, permission_id) DO NOTHING
	`).Error; err != nil {
		return err
	}

	return nil
}
