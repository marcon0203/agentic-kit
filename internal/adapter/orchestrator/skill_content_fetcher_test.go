package orchestrator

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

type fakeObjectStore struct {
	objects map[string][]byte
	gets    int
}

func (f *fakeObjectStore) Put(_ context.Context, key string, r io.Reader, _ string) error {
	content, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = content
	return nil
}

func (f *fakeObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f.gets++
	content, ok := f.objects[key]
	if !ok {
		return nil, resource.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func (f *fakeObjectStore) Delete(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

func TestSkillContentFetcher_FetchesAndCaches(t *testing.T) {
	store := &fakeObjectStore{objects: map[string][]byte{
		"skills/1/my-skill/1.0/SKILL.md": []byte("# instructions"),
	}}
	fetcher := newSkillContentFetcher(store)

	content, err := fetcher.Fetch(context.Background(), 1, 42, "skills/1/my-skill/1.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if content != "# instructions" {
		t.Fatalf("unexpected content: %q", content)
	}

	if _, err := fetcher.Fetch(context.Background(), 1, 42, "skills/1/my-skill/1.0"); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if store.gets != 1 {
		t.Fatalf("expected the second Fetch to be served from cache, got %d OSS gets", store.gets)
	}
}

func TestSkillContentFetcher_MissingObject_ReturnsError(t *testing.T) {
	fetcher := newSkillContentFetcher(&fakeObjectStore{objects: map[string][]byte{}})

	if _, err := fetcher.Fetch(context.Background(), 1, 42, "skills/1/missing/1.0"); err == nil {
		t.Fatal("expected an error for a prefix with no SKILL.md in the store")
	}
}

func TestSkillContentFetcher_NilStore_ReturnsDisabledError(t *testing.T) {
	fetcher := newSkillContentFetcher(nil)

	if _, err := fetcher.Fetch(context.Background(), 1, 42, "skills/1/x/1.0"); err == nil {
		t.Fatal("expected the disabled fetcher to reject with a clear error")
	}
}
