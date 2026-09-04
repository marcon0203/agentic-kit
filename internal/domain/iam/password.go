package iam

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
)

const passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%"

// randomPassword generates a cryptographically random password for
// BootstrapSuperAdmin. Not a general-purpose token generator — this
// package has exactly one caller for it.
func randomPassword(length int) (string, error) {
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(passwordAlphabet))))
		if err != nil {
			return "", err
		}
		out[i] = passwordAlphabet[n.Int64()]
	}
	return string(out), nil
}

// randomHex returns n random bytes hex-encoded — unlike randomPassword's
// alphabet (which includes '@', fine for a password but not for building an
// email address out of), every character here is a safe, unquoted local-part
// character. Used only to make each CreateGuest email unique.
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
