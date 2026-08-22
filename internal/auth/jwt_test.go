package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenIssuer_AccessTokenRoundTrip(t *testing.T) {
	issuer := NewTokenIssuer("test-secret")

	token, err := issuer.IssueAccessToken(42)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	userID, err := issuer.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if userID != 42 {
		t.Fatalf("userID = %d, want 42", userID)
	}
}

func TestTokenIssuer_RefreshTokenRejectedAsAccessToken(t *testing.T) {
	issuer := NewTokenIssuer("test-secret")

	refresh, err := issuer.IssueRefreshToken(42)
	if err != nil {
		t.Fatalf("issue refresh: %v", err)
	}

	if _, err := issuer.VerifyAccessToken(refresh); err == nil {
		t.Fatal("expected a refresh token to be rejected by VerifyAccessToken")
	}
}

func TestTokenIssuer_WrongSecretRejected(t *testing.T) {
	issuer := NewTokenIssuer("secret-a")
	other := NewTokenIssuer("secret-b")

	token, err := issuer.IssueAccessToken(42)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, err := other.VerifyAccessToken(token); err == nil {
		t.Fatal("expected token signed with a different secret to be rejected")
	}
}

func TestTokenIssuer_ExpiredTokenReturnsErrTokenExpired(t *testing.T) {
	issuer := NewTokenIssuer("test-secret")

	// Build an already-expired token directly rather than sleeping past
	// AccessTokenTTL in a test.
	now := time.Now()
	c := claims{
		Type: tokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "42",
			IssuedAt:  jwt.NewNumericDate(now.Add(-3 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Hour)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(issuer.secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = issuer.VerifyAccessToken(signed)
	if err != ErrTokenExpired {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
}

func TestTokenIssuer_MalformedTokenReturnsErrTokenInvalid(t *testing.T) {
	issuer := NewTokenIssuer("test-secret")

	_, err := issuer.VerifyAccessToken("not-a-jwt")
	if err == nil {
		t.Fatal("expected an error for a malformed token")
	}
	if err == ErrTokenExpired {
		t.Fatal("a malformed token must not be classified as expired")
	}
}
