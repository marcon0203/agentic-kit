package resource

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// Port sentinels.
var (
	ErrNotFound  = errors.New("resource: not found")
	ErrDuplicate = errors.New("resource: ref already exists")
)

// Repository persists resources. Kind is a parameter rather than a separate
// repository per type, so the four-table fan-out (spec-05 "分表设计") stays
// inside the adapter and the service reads as one flow.
type Repository interface {
	Create(ctx context.Context, r Resource) (Resource, error)
	GetByID(ctx context.Context, kind Kind, id, ownerID int64) (Resource, error)
	ListPage(ctx context.Context, kind Kind, ownerID, afterID int64, limit int32) ([]Resource, error)
	Update(ctx context.Context, r Resource) (Resource, error)
	FindReferencingAgents(ctx context.Context, kind Kind, ownerID int64, ref string) ([]AgentReference, error)
	SetHealth(ctx context.Context, id int64, health Health) error
}

// CredentialCipher encrypts and decrypts a single credential value. The
// domain decides *which* values are credentials; how they are encrypted is
// infrastructure.
type CredentialCipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

// HealthProbe checks whether an MCP server is reachable.
type HealthProbe interface {
	Check(ctx context.Context, config Config) Health
}

// refPattern is the ref format the API contract publishes.
var refPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// Service is the 资源中心 application service.
type Service struct {
	repo   Repository
	cipher CredentialCipher
	probe  HealthProbe
}

func NewService(repo Repository, cipher CredentialCipher, probe HealthProbe) *Service {
	return &Service{repo: repo, cipher: cipher, probe: probe}
}

// encryptCredentials returns a copy of config with every credential value
// replaced by its ciphertext. Non-credential values pass through unchanged so
// the rest of the config stays inspectable.
func (s *Service) encryptCredentials(config Config) (Config, error) {
	out := make(Config, len(config))
	for k, v := range config {
		if !IsCredentialKey(k) {
			out[k] = v
			continue
		}
		str, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("resource config: credential field %q must be a string", k)
		}
		ciphertext, err := s.cipher.Encrypt(str)
		if err != nil {
			return nil, fmt.Errorf("resource config: encrypt %q: %w", k, err)
		}
		out[k] = ciphertext
	}
	return out, nil
}

// DecryptCredentials reverses encryption. Used only to build a real tool
// from stored config at run time — the result must never reach a response,
// which is why the read paths in this service return Redact()ed configs
// instead.
func (s *Service) DecryptCredentials(config Config) (Config, error) {
	return DecryptConfig(s.cipher, config)
}

// DecryptConfig is DecryptCredentials without a Service, for the run-time
// path that needs a usable config and nothing else this context offers.
// Which fields it touches is still IsCredentialKey's decision, so encrypt
// and decrypt cannot disagree about what a credential is.
func DecryptConfig(cipher CredentialCipher, config Config) (Config, error) {
	out := make(Config, len(config))
	for k, v := range config {
		if !IsCredentialKey(k) {
			out[k] = v
			continue
		}
		str, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("resource config: credential field %q must be a string", k)
		}
		plaintext, err := cipher.Decrypt(str)
		if err != nil {
			return nil, fmt.Errorf("resource config: decrypt %q: %w", k, err)
		}
		out[k] = plaintext
	}
	return out, nil
}

// ListQuery filters the resource list. An empty Kind merges all four kinds.
type ListQuery struct {
	Kind  string
	Limit int
	After int64
}

// List returns resources, redacted.
//
// With a kind filter this is real keyset pagination. Without one it merges a
// first page from each kind — a deliberate V1 simplification (spec-05), not
// a true cross-table cursor, so has_more is reported if *any* kind had more.
func (s *Service) List(ctx context.Context, ownerID int64, q ListQuery) (domain.Page[Resource], error) {
	limit := domain.PageQuery{Limit: q.Limit}.Normalize().Limit

	kinds := AllKinds
	filtered := false
	if q.Kind != "" {
		kind, ok := ParseKind(q.Kind)
		if !ok {
			return domain.Page[Resource]{}, domain.Invalid(domain.CodeValidationFailed, "unknown resource type")
		}
		kinds, filtered = []Kind{kind}, true
	}

	var (
		items      []Resource
		hasMore    bool
		nextCursor string
	)
	for _, kind := range kinds {
		rows, err := s.repo.ListPage(ctx, kind, ownerID, q.After, int32(limit+1))
		if err != nil {
			return domain.Page[Resource]{}, domain.Internal(err)
		}
		if len(rows) > limit {
			hasMore = true
			rows = rows[:limit]
		}
		for _, row := range rows {
			row.Config = row.Config.Redact()
			items = append(items, row)
		}
		// A cursor is only meaningful for a single-kind page; a merged page
		// has no single keyset to resume from.
		if filtered && len(rows) > 0 {
			nextCursor = itoa(rows[len(rows)-1].ID)
		}
	}

	if items == nil {
		items = []Resource{}
	}
	return domain.Page[Resource]{Items: items, HasMore: hasMore, NextCursor: nextCursor}, nil
}

// Get returns one resource, redacted. Exposed (List only returns pages)
// for callers that need a single resource's own config by id — the
// knowledge-base retrieval service reads embedding_provider/embedding_model
// off it this way.
func (s *Service) Get(ctx context.Context, ownerID int64, kind Kind, id int64) (Resource, error) {
	res, err := s.repo.GetByID(ctx, kind, id, ownerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Resource{}, domain.NotFound(domain.CodeResourceNotFound, "resource not found")
		}
		return Resource{}, domain.Internal(err)
	}
	res.Config = res.Config.Redact()
	return res, nil
}

// CreateCommand registers a new resource.
type CreateCommand struct {
	Kind        string
	Ref         string
	DisplayName string
	Config      Config
}

// Create registers a resource, encrypting its credentials on the way in.
func (s *Service) Create(ctx context.Context, ownerID int64, cmd CreateCommand) (Resource, error) {
	var errs []domain.FieldError

	kind, ok := ParseKind(cmd.Kind)
	if !ok {
		errs = append(errs, domain.FieldError{Field: "type", Reason: "must be one of tool, skill, mcp, knowledge_base, memory"})
	}
	if !refPattern.MatchString(cmd.Ref) {
		errs = append(errs, domain.FieldError{Field: "ref", Reason: "must match ^[a-z][a-z0-9_-]*$"})
	}
	if cmd.Config == nil {
		errs = append(errs, domain.FieldError{Field: "config", Reason: "required"})
	}
	if len(errs) > 0 {
		return Resource{}, domain.Invalid(domain.CodeValidationFailed, "validation failed").WithDetails(errs...)
	}

	encrypted, err := s.encryptCredentials(cmd.Config)
	if err != nil {
		return Resource{}, domain.Invalid(domain.CodeValidationFailed, "invalid config")
	}

	created, err := s.repo.Create(ctx, Resource{
		OwnerID: ownerID, Kind: kind, Ref: cmd.Ref, Version: "1.0",
		DisplayName: cmd.DisplayName, Config: encrypted, Status: StatusEnabled,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			return Resource{}, domain.Conflict(domain.CodeResourceRefDuplicate, "a resource with this ref already exists")
		}
		return Resource{}, domain.Internal(err)
	}

	// An MCP server gets a connectivity probe. A failed probe still saves
	// (spec-05) so the owner can come back and fix the config — it only
	// leaves health=unhealthy for them to notice.
	if kind == KindMCP {
		health := s.probe.Check(ctx, cmd.Config)
		_ = s.repo.SetHealth(ctx, created.ID, health)
		created.Health = health
	}

	created.Config = created.Config.Redact()
	return created, nil
}

// UpdateCommand patches a resource. Nil fields are left alone, which is what
// makes this a PATCH rather than a replace.
type UpdateCommand struct {
	DisplayName *string
	Config      Config
	Status      *Status
}

// Update patches a resource, including enabling/disabling it.
func (s *Service) Update(ctx context.Context, ownerID int64, kind Kind, id int64, cmd UpdateCommand) (Resource, error) {
	current, err := s.repo.GetByID(ctx, kind, id, ownerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Resource{}, domain.NotFound(domain.CodeResourceNotFound, "resource not found")
		}
		return Resource{}, domain.Internal(err)
	}

	if cmd.DisplayName != nil {
		current.DisplayName = *cmd.DisplayName
	}
	if cmd.Status != nil {
		current.Status = *cmd.Status
	}
	if cmd.Config != nil {
		encrypted, err := s.encryptCredentials(cmd.Config)
		if err != nil {
			return Resource{}, domain.Invalid(domain.CodeValidationFailed, "invalid config")
		}
		current.Config = encrypted
	}

	updated, err := s.repo.Update(ctx, current)
	if err != nil {
		return Resource{}, domain.Internal(err)
	}
	updated.Config = updated.Config.Redact()
	return updated, nil
}

// DeleteCheck reports whether a resource is safe to delete, listing the
// Agent versions still referencing it.
func (s *Service) DeleteCheck(ctx context.Context, ownerID int64, kind Kind, id int64) (DeleteCheck, error) {
	res, err := s.repo.GetByID(ctx, kind, id, ownerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return DeleteCheck{}, domain.NotFound(domain.CodeResourceNotFound, "resource not found")
		}
		return DeleteCheck{}, domain.Internal(err)
	}

	refs, err := s.repo.FindReferencingAgents(ctx, kind, ownerID, res.Ref)
	if err != nil {
		return DeleteCheck{}, domain.Internal(err)
	}
	if refs == nil {
		refs = []AgentReference{}
	}
	return DeleteCheck{Deletable: len(refs) == 0, ReferencedBy: refs}, nil
}
