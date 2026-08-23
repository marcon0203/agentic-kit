package crypto

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testKey() []byte {
	return []byte("01234567890123456789012345678901"[:32])
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := testKey()
	plaintext := []byte(`{"api_key":"sk-super-secret"}`)

	ciphertext, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if strings.Contains(ciphertext, "sk-super-secret") {
		t.Fatal("ciphertext must not contain the plaintext secret")
	}

	got, err := Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("got %q, want %q", got, plaintext)
	}
}

func TestEncrypt_DifferentNoncePerCall(t *testing.T) {
	key := testKey()
	c1, err := Encrypt(key, []byte("same-plaintext"))
	if err != nil {
		t.Fatalf("encrypt 1: %v", err)
	}
	c2, err := Encrypt(key, []byte("same-plaintext"))
	if err != nil {
		t.Fatalf("encrypt 2: %v", err)
	}
	if c1 == c2 {
		t.Fatal("two encryptions of the same plaintext must differ (random nonce)")
	}
}

func TestDecrypt_WrongKeyFails(t *testing.T) {
	ciphertext, err := Encrypt(testKey(), []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	wrongKey := []byte("98765432109876543210987654321098"[:32])
	if _, err := Decrypt(wrongKey, ciphertext); err == nil {
		t.Fatal("expected decryption with the wrong key to fail")
	}
}

func TestDecodeKey_RejectsWrongLength(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("too-short"))
	if _, err := DecodeKey(short); err != ErrInvalidKeySize {
		t.Fatalf("err = %v, want ErrInvalidKeySize", err)
	}
}

func TestDecodeKey_AcceptsValid32ByteKey(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(testKey())
	key, err := DecodeKey(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(key) != KeySize {
		t.Fatalf("len = %d, want %d", len(key), KeySize)
	}
}
