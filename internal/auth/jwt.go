package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Token lifetimes per 架构设计文档 6.5 / spec-04.
const (
	AccessTokenTTL  = 2 * time.Hour
	RefreshTokenTTL = 7 * 24 * time.Hour
)

// tokenType distinguishes an access token from a refresh token so a
// refresh token can never be used directly as an access token (and vice
// versa) even though both are signed with the same secret.
type tokenType string

const (
	tokenTypeAccess  tokenType = "access"
	tokenTypeRefresh tokenType = "refresh"
)

type claims struct {
	Type tokenType `json:"typ"`
	jwt.RegisteredClaims
}

// TokenIssuer issues and verifies JWTs signed with a single HMAC secret.
type TokenIssuer struct {
	secret []byte
}

func NewTokenIssuer(secret string) *TokenIssuer {
	return &TokenIssuer{secret: []byte(secret)}
}

// IssueAccessToken returns a JWT valid for AccessTokenTTL, subject = userID.
func (i *TokenIssuer) IssueAccessToken(userID int64) (string, error) {
	return i.issue(userID, tokenTypeAccess, AccessTokenTTL)
}

// IssueRefreshToken returns a JWT valid for RefreshTokenTTL, subject = userID.
func (i *TokenIssuer) IssueRefreshToken(userID int64) (string, error) {
	return i.issue(userID, tokenTypeRefresh, RefreshTokenTTL)
}

func (i *TokenIssuer) issue(userID int64, typ tokenType, ttl time.Duration) (string, error) {
	now := time.Now()
	c := claims{
		Type: typ,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, nil
}

// ErrTokenInvalid and ErrTokenExpired distinguish the two 401 cases the
// acceptance checklist calls out (20001 vs 20002) — a malformed/wrong-type
// token is a different failure mode from a validly-issued one that's simply
// past its ExpiresAt.
var (
	ErrTokenInvalid = errors.New("auth: token invalid")
	ErrTokenExpired = errors.New("auth: token expired")
)

// VerifyAccessToken parses and validates an access token, returning the
// subject user ID. Rejects a refresh token presented as an access token.
func (i *TokenIssuer) VerifyAccessToken(tokenString string) (int64, error) {
	return i.verify(tokenString, tokenTypeAccess)
}

// VerifyRefreshToken parses and validates a refresh token, returning the
// subject user ID.
func (i *TokenIssuer) VerifyRefreshToken(tokenString string) (int64, error) {
	return i.verify(tokenString, tokenTypeRefresh)
}

func (i *TokenIssuer) verify(tokenString string, want tokenType) (int64, error) {
	var c claims
	token, err := jwt.ParseWithClaims(tokenString, &c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return i.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return 0, ErrTokenExpired
		}
		return 0, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}
	if !token.Valid || c.Type != want {
		return 0, ErrTokenInvalid
	}

	userID, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: bad subject", ErrTokenInvalid)
	}
	return userID, nil
}
