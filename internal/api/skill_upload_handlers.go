package api

import (
	"archive/zip"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// maxSkillZipUploadBytes bounds the raw multipart body before it's ever
// handed to archive/zip — a hard ceiling on the wire, ahead of
// resource.Service.UploadSkill's own uncompressed-size check, so an
// oversized request never gets far enough to allocate that much memory.
const maxSkillZipUploadBytes = 25 << 20 // 25MB

// UploadSkill handles POST /resources/skills/upload (multipart/form-data:
// `ref`, optional `display_name`, and a `zip` file field). Parsing the zip
// and rejecting zip-slip entries happens here, in the transport layer —
// resource.Service.UploadSkill only ever sees a clean path->content map.
func (h *ResourceHandlers) UploadSkill(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSkillZipUploadBytes)
	if err := r.ParseMultipartForm(maxSkillZipUploadBytes); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed multipart body, or zip exceeds the upload limit")
		return
	}

	ref := r.FormValue("ref")
	displayName := r.FormValue("display_name")

	file, header, err := r.FormFile("zip")
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "missing zip file field")
		return
	}
	defer func() { _ = file.Close() }()

	files, err := extractSkillZip(file, header.Size)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, err.Error())
		return
	}

	created, err := h.svc.UploadSkill(r.Context(), userID, resource.UploadSkillCommand{
		Ref: ref, DisplayName: displayName, Files: files,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toResourceDTO(created))
}

// extractSkillZip reads a zip into a path->content map, rejecting any entry
// that would escape the extraction root (zip slip: "..", a leading "/", or
// an absolute Windows-style path) and skipping directory entries — the
// caller only wants files. Content is read fully into memory; the size
// ceiling that actually matters (maxSkillZipTotalBytes) is enforced by
// resource.Service.UploadSkill once every entry has a name to blame in the
// error, but reading a single entry larger than the whole upload's byte
// budget is rejected here as an obvious zip-bomb signal.
func extractSkillZip(r io.ReaderAt, size int64) (map[string][]byte, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, errBadZip
	}

	files := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.TrimPrefix(f.Name, "./")
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return nil, errZipSlip
		}
		if int64(f.UncompressedSize64) > maxSkillZipUploadBytes {
			return nil, errBadZip
		}

		rc, err := f.Open()
		if err != nil {
			return nil, errBadZip
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, errBadZip
		}
		files[name] = content
	}
	return files, nil
}

var (
	errBadZip  = errValidation("not a valid zip file, or an entry is too large")
	errZipSlip = errValidation("zip entry path escapes the archive root")
)

type errValidation string

func (e errValidation) Error() string { return string(e) }

// ListSkillFiles handles GET /resources/skills/{id}/files.
func (h *ResourceHandlers) ListSkillFiles(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	_, id, err := decodeResourceID(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}

	files, err := h.svc.ListSkillFiles(r.Context(), userID, id)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}

	dtos := make([]skillFileDTO, len(files))
	for i, f := range files {
		dtos[i] = skillFileDTO{Path: f.Path, SizeBytes: f.SizeBytes, ContentType: f.ContentType}
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": dtos})
}

type skillFileDTO struct {
	Path        string `json:"path"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentType string `json:"content_type"`
}

// GetSkillFile handles GET /resources/skills/{id}/files/* — proxies one
// file's content straight from OSS rather than buffering it, so a large
// attachment doesn't sit fully in server memory on its way out.
func (h *ResourceHandlers) GetSkillFile(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	_, id, err := decodeResourceID(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}
	path := chi.URLParam(r, "*")

	content, contentType, err := h.svc.GetSkillFile(r.Context(), userID, id, path)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	defer func() { _ = content.Close() }()

	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Content-Disposition", `attachment; filename=`+strconv.Quote(pathBase(path)))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, content)
}

func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
