// Package iam is the IAM bounded context: registering an account, signing
// in, and issuing the tokens every other context's ownership checks rest
// on.
//
// It is small, and one rule shapes all of it: an unauthenticated caller
// must not be able to learn which email addresses have accounts. That is
// why a failed sign-in has a single response regardless of which half was
// wrong, and why verification runs even when no such user exists.
package iam

import "time"

// User is an account. PasswordHash stays on the entity because
// authentication is this context's own business, and never leaves it —
// the view returned to callers is Profile.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	DisplayName  string
	IsAdmin      bool
	CreatedAt    time.Time
}

// Profile is a user as every response describes them: no password hash,
// by construction rather than by remembering to omit it.
type Profile struct {
	ID          int64
	Email       string
	DisplayName string
	IsAdmin     bool
	CreatedAt   time.Time
}

func (u User) Profile() Profile {
	return Profile{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt}
}

// Session is a successful authentication: who signed in, and the pair of
// tokens they carry afterwards.
type Session struct {
	AccessToken  string
	RefreshToken string
	User         Profile
}

// MinPasswordLength is the shortest password the platform accepts.
const MinPasswordLength = 8
