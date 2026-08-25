package resource_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// fakeObjectStore is an in-memory stand-in for OSS — good enough to prove
// UploadSkill writes the right keys with the right content, without a real
// bucket.
type fakeObjectStore struct {
	objects map[string][]byte
}

func newFakeObjectStore() *fakeObjectStore { return &fakeObjectStore{objects: map[string][]byte{}} }

func (f *fakeObjectStore) Put(_ context.Context, key string, r io.Reader, _ string) error {
	content, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = content
	return nil
}

func (f *fakeObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
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

type fakeSkillFileRepo struct {
	bySkillID map[int64][]resource.SkillFile
}

func newFakeSkillFileRepo() *fakeSkillFileRepo {
	return &fakeSkillFileRepo{bySkillID: map[int64][]resource.SkillFile{}}
}

func (f *fakeSkillFileRepo) CreateFiles(_ context.Context, skillID, _ int64, files []resource.SkillFile) error {
	f.bySkillID[skillID] = append(f.bySkillID[skillID], files...)
	return nil
}

func (f *fakeSkillFileRepo) ListFiles(_ context.Context, skillID, _ int64) ([]resource.SkillFile, error) {
	return f.bySkillID[skillID], nil
}

func newSkillSvc(repo *fakeRepo, store *fakeObjectStore, files *fakeSkillFileRepo) *resource.Service {
	return newSvc(repo, stubProbe{}).WithSkillUploads(store, files)
}

func TestUploadSkill_StoresFilesAndRegistersResource(t *testing.T) {
	repo, store, files := newFakeRepo(), newFakeObjectStore(), newFakeSkillFileRepo()
	svc := newSkillSvc(repo, store, files)

	created, err := svc.UploadSkill(context.Background(), 1, resource.UploadSkillCommand{
		Ref: "my-skill",
		Files: map[string][]byte{
			resource.SkillEntryFile: []byte("# My Skill\ninstructions here"),
			"scripts/run.py":        []byte("print('hi')"),
		},
	})
	if err != nil {
		t.Fatalf("UploadSkill: %v", err)
	}
	if created.Ref != "my-skill" || created.Kind != resource.KindSkill {
		t.Fatalf("unexpected resource: %+v", created)
	}

	prefix, _ := repo.byKind[resource.KindSkill][created.ID].Config["oss_prefix"].(string)
	if prefix == "" {
		t.Fatal("expected oss_prefix to be set on the stored config")
	}
	if string(store.objects[prefix+"/"+resource.SkillEntryFile]) != "# My Skill\ninstructions here" {
		t.Fatalf("SKILL.md was not stored at the expected key: %+v", store.objects)
	}
	if string(store.objects[prefix+"/scripts/run.py"]) != "print('hi')" {
		t.Fatalf("attachment was not stored at the expected key: %+v", store.objects)
	}

	indexed := files.bySkillID[created.ID]
	if len(indexed) != 2 {
		t.Fatalf("expected 2 indexed files, got %d: %+v", len(indexed), indexed)
	}
}

func TestUploadSkill_MissingEntryFileRejected(t *testing.T) {
	svc := newSkillSvc(newFakeRepo(), newFakeObjectStore(), newFakeSkillFileRepo())

	_, err := svc.UploadSkill(context.Background(), 1, resource.UploadSkillCommand{
		Ref:   "no-entry",
		Files: map[string][]byte{"README.md": []byte("hello")},
	})
	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestUploadSkill_TooManyFilesRejected(t *testing.T) {
	files := map[string][]byte{resource.SkillEntryFile: []byte("x")}
	for i := 0; i < 205; i++ {
		files["file"+itoa(int64(i))] = []byte("x")
	}
	svc := newSkillSvc(newFakeRepo(), newFakeObjectStore(), newFakeSkillFileRepo())

	_, err := svc.UploadSkill(context.Background(), 1, resource.UploadSkillCommand{Ref: "too-many", Files: files})
	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestUploadSkill_DuplicateRefIsConflict(t *testing.T) {
	repo, store, files := newFakeRepo(), newFakeObjectStore(), newFakeSkillFileRepo()
	svc := newSkillSvc(repo, store, files)
	cmd := resource.UploadSkillCommand{Ref: "dup", Files: map[string][]byte{resource.SkillEntryFile: []byte("x")}}

	if _, err := svc.UploadSkill(context.Background(), 1, cmd); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	_, err := svc.UploadSkill(context.Background(), 1, cmd)
	assertErr(t, err, domain.KindConflict, domain.CodeResourceRefDuplicate)
}

func TestUploadSkill_WithoutOSSConfigured_ReturnsClearError(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{}) // no .WithSkillUploads

	_, err := svc.UploadSkill(context.Background(), 1, resource.UploadSkillCommand{
		Ref: "x", Files: map[string][]byte{resource.SkillEntryFile: []byte("x")},
	})
	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestGetSkillFile_ReturnsStoredContent(t *testing.T) {
	repo, store, files := newFakeRepo(), newFakeObjectStore(), newFakeSkillFileRepo()
	svc := newSkillSvc(repo, store, files)
	created, err := svc.UploadSkill(context.Background(), 1, resource.UploadSkillCommand{
		Ref:   "readable",
		Files: map[string][]byte{resource.SkillEntryFile: []byte("skill body")},
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	rc, contentType, err := svc.GetSkillFile(context.Background(), 1, created.ID, resource.SkillEntryFile)
	if err != nil {
		t.Fatalf("GetSkillFile: %v", err)
	}
	defer func() { _ = rc.Close() }()
	content, _ := io.ReadAll(rc)
	if string(content) != "skill body" {
		t.Fatalf("expected the uploaded content back, got %q", content)
	}
	if contentType == "" {
		t.Fatal("expected a content type")
	}
}

func TestGetSkillFile_WrongOwner_NotFound(t *testing.T) {
	repo, store, files := newFakeRepo(), newFakeObjectStore(), newFakeSkillFileRepo()
	svc := newSkillSvc(repo, store, files)
	created, err := svc.UploadSkill(context.Background(), 1, resource.UploadSkillCommand{
		Ref: "mine", Files: map[string][]byte{resource.SkillEntryFile: []byte("x")},
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	_, _, err = svc.GetSkillFile(context.Background(), 2, created.ID, resource.SkillEntryFile)
	assertErr(t, err, domain.KindNotFound, domain.CodeResourceNotFound)
}

func TestGetSkillFile_UnknownPath_NotFound(t *testing.T) {
	repo, store, files := newFakeRepo(), newFakeObjectStore(), newFakeSkillFileRepo()
	svc := newSkillSvc(repo, store, files)
	created, err := svc.UploadSkill(context.Background(), 1, resource.UploadSkillCommand{
		Ref: "mine2", Files: map[string][]byte{resource.SkillEntryFile: []byte("x")},
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	_, _, err = svc.GetSkillFile(context.Background(), 1, created.ID, "not-a-real-file.txt")
	assertErr(t, err, domain.KindNotFound, domain.CodeResourceNotFound)
}
