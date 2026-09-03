package resource

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// SkillFile is one file inside an uploaded Skill's zip — an index entry,
// not the content itself (that lives in ObjectStore under the resource's
// config.oss_prefix).
type SkillFile struct {
	Path        string
	SizeBytes   int64
	ContentType string
}

// SkillFileRepository indexes a Skill's uploaded file tree. Separate from
// Repository because it doesn't fit the four-kind-shared single-row CRUD
// shape — it's Skill-only, and it's one-to-many per resource.
type SkillFileRepository interface {
	CreateFiles(ctx context.Context, skillID, ownerID int64, files []SkillFile) error
	ListFiles(ctx context.Context, skillID, ownerID int64) ([]SkillFile, error)
}

// SkillEntryFile is the one file inside a Skill's zip an Agent actually
// reads at run time — every other file is reference material a person (or
// eventually the model, by request) can read but isn't fed automatically.
const SkillEntryFile = "SKILL.md"

// maxSkillZipFiles/maxSkillZipTotalBytes bound a single upload — generous
// enough for a real Skill (scripts, templates, reference docs) without
// letting one upload become a quota problem against the OSS bucket.
const (
	maxSkillZipFiles      = 200
	maxSkillZipTotalBytes = 20 << 20 // 20MB, uncompressed
)

// UploadSkillCommand is a parsed, already-validated-as-a-zip's contents.
// Rejecting zip slip (".."/absolute entries) and decompressing happens in
// the transport layer, before this ever reaches the service — by the time
// it's here, Files is just "what to store", keyed by the path inside the
// archive.
type UploadSkillCommand struct {
	Ref         string
	DisplayName string
	Files       map[string][]byte
	// Meta 是调用方要求一并落进 config 的标记字段（Skill 市场安装用它记录
	// installed_from）。与 entry/oss_prefix/total_size 合并，键冲突时以
	// 平台自己的三个键为准。
	Meta map[string]any
}

// contentTypeFor guesses a content type from a file's extension, falling
// back to a generic binary type — good enough for OSS metadata and for the
// list/download UI to decide whether a file is worth trying to preview as
// text; it isn't sniffed from content, and doesn't need to be exact.
func contentTypeFor(path string) string {
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	if strings.HasSuffix(path, ".md") {
		return "text/markdown"
	}
	return "application/octet-stream"
}

// skillOSSPrefix is where one Skill's files live in the bucket — one
// prefix per (owner, ref), matching the "always version 1.0" ceiling every
// other resource kind already has through this service (see Create):
// there's no re-upload-as-a-new-version story yet, only create-or-409.
func skillOSSPrefix(ownerID int64, ref string) string {
	return fmt.Sprintf("skills/%d/%s/1.0", ownerID, ref)
}

// UploadSkill stores a zip's files to ObjectStore and registers the Skill
// resource pointing at them, in that order — files land in the bucket
// before the resource row (and its file index) ever claims they exist, so
// a failed upload never leaves a resource pointing at a half-written
// prefix.
func (s *Service) UploadSkill(ctx context.Context, ownerID int64, cmd UploadSkillCommand) (Resource, error) {
	if s.objectStore == nil || s.skillFiles == nil {
		return Resource{}, domain.Invalid(domain.CodeValidationFailed, "skill upload is not configured on this deployment (OSS)")
	}

	var errs []domain.FieldError
	if !refPattern.MatchString(cmd.Ref) {
		errs = append(errs, domain.FieldError{Field: "ref", Reason: "must match ^[a-z][a-z0-9_-]*$"})
	}
	if _, ok := cmd.Files[SkillEntryFile]; !ok {
		errs = append(errs, domain.FieldError{Field: "files", Reason: "zip must contain " + SkillEntryFile})
	}
	if len(cmd.Files) > maxSkillZipFiles {
		errs = append(errs, domain.FieldError{Field: "files", Reason: fmt.Sprintf("zip has more than %d files", maxSkillZipFiles)})
	}
	var total int64
	for _, content := range cmd.Files {
		total += int64(len(content))
	}
	if total > maxSkillZipTotalBytes {
		errs = append(errs, domain.FieldError{Field: "files", Reason: fmt.Sprintf("zip exceeds %d bytes uncompressed", maxSkillZipTotalBytes)})
	}
	if len(errs) > 0 {
		return Resource{}, domain.Invalid(domain.CodeValidationFailed, "invalid skill upload").WithDetails(errs...)
	}

	prefix := skillOSSPrefix(ownerID, cmd.Ref)
	for path, content := range cmd.Files {
		key := prefix + "/" + path
		if err := s.objectStore.Put(ctx, key, bytes.NewReader(content), contentTypeFor(path)); err != nil {
			return Resource{}, domain.Internal(fmt.Errorf("upload %q: %w", path, err))
		}
	}

	config := Config{
		"entry":      SkillEntryFile,
		"oss_prefix": prefix,
		"total_size": total,
	}
	for k, v := range cmd.Meta {
		if _, exists := config[k]; !exists {
			config[k] = v
		}
	}

	created, err := s.repo.Create(ctx, Resource{
		OwnerID: ownerID, Kind: KindSkill, Ref: cmd.Ref, Version: "1.0",
		DisplayName: cmd.DisplayName,
		Config:      config,
		Status:      StatusEnabled,
	})
	if err != nil {
		if err == ErrDuplicate {
			return Resource{}, domain.Conflict(domain.CodeResourceRefDuplicate, "a resource with this ref already exists")
		}
		return Resource{}, domain.Internal(err)
	}

	files := make([]SkillFile, 0, len(cmd.Files))
	for path, content := range cmd.Files {
		files = append(files, SkillFile{Path: path, SizeBytes: int64(len(content)), ContentType: contentTypeFor(path)})
	}
	if err := s.skillFiles.CreateFiles(ctx, created.ID, ownerID, files); err != nil {
		return Resource{}, domain.Internal(err)
	}

	created.redactConfig()
	return created, nil
}

// ListSkillFiles returns a Skill's file tree for the list/download UI.
func (s *Service) ListSkillFiles(ctx context.Context, ownerID, skillID int64) ([]SkillFile, error) {
	if s.skillFiles == nil {
		return nil, domain.NotFound(domain.CodeResourceNotFound, "resource not found")
	}
	// GetByID enforces ownership before ListFiles ever runs — a skill_id
	// alone doesn't prove the caller owns it.
	if _, err := s.repo.GetByID(ctx, KindSkill, skillID, ownerID); err != nil {
		if err == ErrNotFound {
			return nil, domain.NotFound(domain.CodeResourceNotFound, "resource not found")
		}
		return nil, domain.Internal(err)
	}
	files, err := s.skillFiles.ListFiles(ctx, skillID, ownerID)
	if err != nil {
		return nil, domain.Internal(err)
	}
	return files, nil
}

// GetSkillFile streams one file's content back out of ObjectStore, after
// confirming the caller owns the skill and the path is actually part of
// its indexed file tree — the OSS key is never trusted from the request
// directly, only ever built from what CreateFiles already recorded.
func (s *Service) GetSkillFile(ctx context.Context, ownerID, skillID int64, path string) (io.ReadCloser, string, error) {
	if s.objectStore == nil || s.skillFiles == nil {
		return nil, "", domain.NotFound(domain.CodeResourceNotFound, "resource not found")
	}
	res, err := s.repo.GetByID(ctx, KindSkill, skillID, ownerID)
	if err != nil {
		if err == ErrNotFound {
			return nil, "", domain.NotFound(domain.CodeResourceNotFound, "resource not found")
		}
		return nil, "", domain.Internal(err)
	}
	files, err := s.skillFiles.ListFiles(ctx, skillID, ownerID)
	if err != nil {
		return nil, "", domain.Internal(err)
	}
	var contentType string
	found := false
	for _, f := range files {
		if f.Path == path {
			found, contentType = true, f.ContentType
			break
		}
	}
	if !found {
		return nil, "", domain.NotFound(domain.CodeResourceNotFound, "file not found")
	}
	prefix, _ := res.Config["oss_prefix"].(string)
	rc, err := s.objectStore.Get(ctx, prefix+"/"+path)
	if err != nil {
		return nil, "", domain.Internal(err)
	}
	return rc, contentType, nil
}
