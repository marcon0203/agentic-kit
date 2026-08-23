package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/crypto"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
	"github.com/marcon0203/agentic-kit/internal/store"
)

type fakeModelProviderQuerier struct {
	store.Querier
	rows   []store.ListModelProvidersForOwnerRow
	nextID int64
	// keyed by id: the raw (encrypted) credentials, to prove List never
	// touches this and Create actually stored something non-empty.
	credentials map[int64][]byte
}

func newFakeModelProviderQuerier() *fakeModelProviderQuerier {
	return &fakeModelProviderQuerier{nextID: 1, credentials: map[int64][]byte{}}
}

func (f *fakeModelProviderQuerier) ListModelProvidersForOwner(_ context.Context, ownerUserID int64) ([]store.ListModelProvidersForOwnerRow, error) {
	var out []store.ListModelProvidersForOwnerRow
	for _, row := range f.rows {
		if row.OwnerUserID == ownerUserID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeModelProviderQuerier) CreateModelProvider(_ context.Context, arg store.CreateModelProviderParams) (store.CreateModelProviderRow, error) {
	row := store.CreateModelProviderRow{
		ID: f.nextID, OwnerUserID: arg.OwnerUserID, Provider: arg.Provider, Status: 1,
		CreatedAt: pgtype.Timestamptz{Valid: true},
	}
	f.rows = append(f.rows, store.ListModelProvidersForOwnerRow(row))
	f.credentials[f.nextID] = arg.Credentials
	f.nextID++
	return row, nil
}

func fixedValidator(name string, err error) func(string) modelgateway.Validator {
	return func(provider string) modelgateway.Validator {
		if provider != name {
			return nil
		}
		return fakeValidator{err: err}
	}
}

type fakeValidator struct{ err error }

func (v fakeValidator) Validate(context.Context, string) error { return v.err }

func doModelProviderRequest(h http.HandlerFunc, userID int64, method, path string, body any) *httptest.ResponseRecorder {
	var bodyReader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(b))
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req = req.WithContext(WithUserID(req.Context(), userID))
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func TestModelProviderCreate_ValidCredentials_Succeeds(t *testing.T) {
	f := newFakeModelProviderQuerier()
	aesKey := []byte("01234567890123456789012345678901"[:32])
	h := &ModelProviderHandlers{Queries: f, AESKey: aesKey, NewValidator: fixedValidator("anthropic", nil)}

	rr := doModelProviderRequest(h.Create, 1, http.MethodPost, "/model-providers", createModelProviderRequest{
		Provider: "anthropic", APIKey: "sk-real-secret-key",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-real-secret-key") {
		t.Fatalf("response leaked the plaintext api_key: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "credentials") {
		t.Fatalf("response exposed a credentials field: %s", rr.Body.String())
	}

	stored := f.credentials[1]
	if len(stored) == 0 || strings.Contains(string(stored), "sk-real-secret-key") {
		t.Fatalf("stored credentials must be encrypted, not plaintext: %q", stored)
	}
	plain, err := crypto.Decrypt(aesKey, string(stored))
	if err != nil || string(plain) != "sk-real-secret-key" {
		t.Fatalf("expected stored credentials to decrypt back to the original key, got %q, err=%v", plain, err)
	}
}

func TestModelProviderCreate_InvalidCredentials_Returns422(t *testing.T) {
	f := newFakeModelProviderQuerier()
	h := &ModelProviderHandlers{Queries: f, AESKey: make([]byte, 32), NewValidator: fixedValidator("openai", modelgateway.ErrCredentialsInvalid)}

	rr := doModelProviderRequest(h.Create, 1, http.MethodPost, "/model-providers", createModelProviderRequest{
		Provider: "openai", APIKey: "bad-key",
	})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rr.Code, rr.Body.String())
	}
	if !containsCode(rr.Body.String(), ErrProviderCredsInvalid) {
		t.Fatalf("expected 60002 in body, got %s", rr.Body.String())
	}
	if len(f.rows) != 0 {
		t.Fatalf("expected nothing stored on invalid credentials, got %+v", f.rows)
	}
}

func TestModelProviderCreate_UnknownProvider_Returns400(t *testing.T) {
	f := newFakeModelProviderQuerier()
	h := &ModelProviderHandlers{Queries: f, AESKey: make([]byte, 32), NewValidator: func(string) modelgateway.Validator { return nil }}

	rr := doModelProviderRequest(h.Create, 1, http.MethodPost, "/model-providers", createModelProviderRequest{
		Provider: "bogus", APIKey: "key",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestModelProviderList_NeverIncludesCredentials(t *testing.T) {
	f := newFakeModelProviderQuerier()
	f.rows = []store.ListModelProvidersForOwnerRow{
		{ID: 1, OwnerUserID: 1, Provider: "anthropic", Status: 1, CreatedAt: pgtype.Timestamptz{Valid: true}},
	}
	h := &ModelProviderHandlers{Queries: f, AESKey: make([]byte, 32)}

	rr := doModelProviderRequest(h.List, 1, http.MethodGet, "/model-providers", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(strings.ToLower(rr.Body.String()), "credential") || strings.Contains(strings.ToLower(rr.Body.String()), "api_key") {
		t.Fatalf("list response must never mention credentials: %s", rr.Body.String())
	}
}

func TestModelProviderList_ScopedToOwner(t *testing.T) {
	f := newFakeModelProviderQuerier()
	f.rows = []store.ListModelProvidersForOwnerRow{
		{ID: 1, OwnerUserID: 1, Provider: "anthropic", Status: 1, CreatedAt: pgtype.Timestamptz{Valid: true}},
		{ID: 2, OwnerUserID: 2, Provider: "openai", Status: 1, CreatedAt: pgtype.Timestamptz{Valid: true}},
	}
	h := &ModelProviderHandlers{Queries: f, AESKey: make([]byte, 32)}

	rr := doModelProviderRequest(h.List, 1, http.MethodGet, "/model-providers", nil)
	var env struct {
		Data []modelProviderDTO `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data) != 1 || env.Data[0].Provider != "anthropic" {
		t.Fatalf("expected only owner 1's provider, got %+v", env.Data)
	}
}
