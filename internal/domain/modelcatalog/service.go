package modelcatalog

import (
	"context"
	"errors"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

var ErrNotFound = errors.New("modelcatalog: not found")
var ErrDuplicate = errors.New("modelcatalog: already exists")

// Repository is the persistence port. AdminDirectory gates every write —
// 系统配置 → 模型提供商 is an admin-only surface, unlike modelcenter's
// per-user provider credentials.
type Repository interface {
	CreateProvider(ctx context.Context, key, displayName, icon, baseURL string) (Provider, error)
	ListProviders(ctx context.Context) ([]Provider, error)
	GetProvider(ctx context.Context, id int64) (Provider, error)
	SetProviderStatus(ctx context.Context, id int64, status int16) error
	DeleteProvider(ctx context.Context, id int64) error

	CreateModel(ctx context.Context, providerID int64, model, displayName, description string, modality Modality, featured bool) (Model, error)
	ListModelsForProvider(ctx context.Context, providerID int64) ([]Model, error)
	SetModelStatus(ctx context.Context, id int64, status int16) error
	DeleteModel(ctx context.Context, id int64) error

	ListPublic(ctx context.Context) ([]CatalogEntry, error)
}

type AdminDirectory interface {
	IsAdmin(ctx context.Context, userID int64) (bool, error)
}

type Service struct {
	repo   Repository
	admins AdminDirectory
}

func NewService(repo Repository, admins AdminDirectory) *Service {
	return &Service{repo: repo, admins: admins}
}

// List is the public read for 模型广场 — every logged-in user, not just
// admins, since this is what a user browses to pick a model.
func (s *Service) List(ctx context.Context) ([]CatalogEntry, error) {
	entries, err := s.repo.ListPublic(ctx)
	if err != nil {
		return nil, domain.Internal(err)
	}
	return entries, nil
}

// CreateProvider registers a new catalog provider. Admin only.
func (s *Service) CreateProvider(ctx context.Context, userID int64, key, displayName, icon, baseURL string) (Provider, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return Provider{}, err
	}

	key = strings.TrimSpace(key)
	displayName = strings.TrimSpace(displayName)
	var fields []domain.FieldError
	if key == "" {
		fields = append(fields, domain.FieldError{Field: "key", Reason: "required"})
	}
	if displayName == "" {
		fields = append(fields, domain.FieldError{Field: "display_name", Reason: "required"})
	}
	if len(fields) > 0 {
		return Provider{}, domain.Invalid(domain.CodeValidationFailed, "validation failed").WithDetails(fields...)
	}

	created, err := s.repo.CreateProvider(ctx, key, displayName, icon, baseURL)
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			return Provider{}, domain.Conflict(domain.CodeCatalogProviderKeyDup, "该 provider key 已存在")
		}
		return Provider{}, domain.Internal(err)
	}
	return created, nil
}

// ListProviders returns every catalog provider, enabled or not. Admin only
// — 模型广场 itself only ever sees providers through List's join.
func (s *Service) ListProviders(ctx context.Context, userID int64) ([]Provider, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return nil, err
	}
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return nil, domain.Internal(err)
	}
	return providers, nil
}

// SetProviderStatus enables/disables a provider — disabling it also hides
// every model under it from 模型广场's join without touching those rows.
func (s *Service) SetProviderStatus(ctx context.Context, userID, providerID int64, enabled bool) error {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return err
	}
	if _, err := s.repo.GetProvider(ctx, providerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.NotFound(domain.CodeCatalogProviderNotFound, "provider 不存在")
		}
		return domain.Internal(err)
	}
	status := int16(0)
	if enabled {
		status = 1
	}
	if err := s.repo.SetProviderStatus(ctx, providerID, status); err != nil {
		return domain.Internal(err)
	}
	return nil
}

// DeleteProvider removes a provider and, via ON DELETE CASCADE, every model
// registered under it — deleting a provider is deleting its whole catalog
// branch, not something that can leave orphaned models behind.
func (s *Service) DeleteProvider(ctx context.Context, userID, providerID int64) error {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return err
	}
	if _, err := s.repo.GetProvider(ctx, providerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.NotFound(domain.CodeCatalogProviderNotFound, "provider 不存在")
		}
		return domain.Internal(err)
	}
	if err := s.repo.DeleteProvider(ctx, providerID); err != nil {
		return domain.Internal(err)
	}
	return nil
}

// CreateModel registers a model under a provider. Admin only.
func (s *Service) CreateModel(ctx context.Context, userID, providerID int64, model, displayName, description string, modality Modality, featured bool) (Model, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return Model{}, err
	}
	if _, err := s.repo.GetProvider(ctx, providerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Model{}, domain.NotFound(domain.CodeCatalogProviderNotFound, "provider 不存在")
		}
		return Model{}, domain.Internal(err)
	}

	model = strings.TrimSpace(model)
	displayName = strings.TrimSpace(displayName)
	var fields []domain.FieldError
	if model == "" {
		fields = append(fields, domain.FieldError{Field: "model", Reason: "required"})
	}
	if displayName == "" {
		fields = append(fields, domain.FieldError{Field: "display_name", Reason: "required"})
	}
	if !modality.Valid() {
		fields = append(fields, domain.FieldError{Field: "modality", Reason: "must be one of text, image, video, vision, embedding"})
	}
	if len(fields) > 0 {
		return Model{}, domain.Invalid(domain.CodeValidationFailed, "validation failed").WithDetails(fields...)
	}

	created, err := s.repo.CreateModel(ctx, providerID, model, displayName, description, modality, featured)
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			return Model{}, domain.Conflict(domain.CodeCatalogProviderKeyDup, "该 provider 下已存在同名模型")
		}
		return Model{}, domain.Internal(err)
	}
	return created, nil
}

// ListModelsForProvider returns every model under a provider, enabled or
// not. Admin only.
func (s *Service) ListModelsForProvider(ctx context.Context, userID, providerID int64) ([]Model, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return nil, err
	}
	models, err := s.repo.ListModelsForProvider(ctx, providerID)
	if err != nil {
		return nil, domain.Internal(err)
	}
	return models, nil
}

func (s *Service) SetModelStatus(ctx context.Context, userID, modelID int64, enabled bool) error {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return err
	}
	status := int16(0)
	if enabled {
		status = 1
	}
	if err := s.repo.SetModelStatus(ctx, modelID, status); err != nil {
		return domain.Internal(err)
	}
	return nil
}

func (s *Service) DeleteModel(ctx context.Context, userID, modelID int64) error {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return err
	}
	if err := s.repo.DeleteModel(ctx, modelID); err != nil {
		return domain.Internal(err)
	}
	return nil
}

func (s *Service) requireAdmin(ctx context.Context, userID int64) error {
	isAdmin, err := s.admins.IsAdmin(ctx, userID)
	if err != nil {
		// Fail closed: a lookup failure must never read as "is admin".
		return domain.Forbidden(domain.CodeForbidden, "admin access required")
	}
	if !isAdmin {
		return domain.Forbidden(domain.CodeForbidden, "admin access required")
	}
	return nil
}
