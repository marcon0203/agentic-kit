// Package rbac is 系统配置 → 用户管理 / 角色权限: standard role-based access
// control. A Permission is a fixed, code-defined capability — one per
// gated button/action, seeded by migration, never created through the API.
// A Role is an admin-configurable bundle of Permissions. A User holds zero
// or more Roles; their effective permission set is the union across all of
// them. is_admin stays a superadmin bypass on top of this (see
// internal/domain/operation and internal/domain/modelcatalog's
// requireAdmin/requireAccess), so an existing admin account never loses
// access while roles are still being configured.
package rbac

import "time"

type Permission struct {
	ID        int64
	Key       string
	Name      string
	Module    string
	CreatedAt time.Time
}

type Role struct {
	ID          int64
	Key         string
	Name        string
	Description string
	CreatedAt   time.Time
	// Permissions is populated on reads that join role_permissions; nil on
	// a bare Role returned from Create before the caller assigns any.
	Permissions []string
}

// UserAccount is one row of 系统配置 → 用户管理's list: an iam.User plus
// its assigned Role keys, without the password hash.
type UserAccount struct {
	ID          int64
	Email       string
	DisplayName string
	Status      int16
	IsAdmin     bool
	CreatedAt   time.Time
	Roles       []string
}

func (u UserAccount) Enabled() bool { return u.Status == 1 }
