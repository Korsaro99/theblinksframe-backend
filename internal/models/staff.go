// Package models holds GORM structs that mirror the Liquibase schema in db/changelog.
// The schema is owned by Liquibase: these structs follow it and never create or alter tables.
package models

import (
	"time"

	"github.com/google/uuid"
)

// StaffRole is a back-office role. The permissions for each live in internal/auth/rbac_policy.csv.
type StaffRole string

const (
	RoleOwner StaffRole = "OWNER"
	RoleStaff StaffRole = "STAFF"
)

// StaffUser is a back-office account (table staff_users).
type StaffUser struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:uuidv7()"`
	Name         string
	Phone        string // E.164
	Email        *string
	PasswordHash string
	Role         StaffRole
	IsActive     bool
	// TokenVersion is copied into every access token. Bumping it invalidates them all at once.
	TokenVersion int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Session revoke reasons (auth_sessions.revoke_reason).
const (
	RevokeLogout         = "LOGOUT"
	RevokeReuseDetected  = "REUSE_DETECTED"
	RevokeAdmin          = "ADMIN"
	RevokePasswordChange = "PASSWORD_CHANGE"
)

// AuthSession is one login on one device: a family of rotated refresh tokens (table auth_sessions).
type AuthSession struct {
	ID                uuid.UUID `gorm:"type:uuid;primaryKey;default:uuidv7()"`
	StaffUserID       uuid.UUID `gorm:"type:uuid"`
	CreatedAt         time.Time
	LastUsedAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
	RevokeReason      *string
	UserAgent         string
	IP                *string `gorm:"column:ip;type:inet"`
}

// RefreshToken is one issued refresh token, stored only as its SHA-256 hash (table refresh_tokens).
type RefreshToken struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:uuidv7()"`
	SessionID    uuid.UUID `gorm:"type:uuid"`
	TokenHash    []byte
	IssuedAt     time.Time
	UsedAt       *time.Time
	ReplacedByID *uuid.UUID `gorm:"type:uuid"`
}
