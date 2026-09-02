package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// Service is the Agent context's application service: every business rule
// about creating, listing and deleting Agents lives here, expressed against
// ports. It returns *domain.Error, so a caller never has to decide what a
// failure "means" — only how to render it.
type Service struct {
	repo      Repository
	catalog   ResourceCatalog
	validator DefinitionValidator
	channels  ChannelDirectory
}

func NewService(repo Repository, catalog ResourceCatalog, validator DefinitionValidator, channels ChannelDirectory) *Service {
	return &Service{repo: repo, catalog: catalog, validator: validator, channels: channels}
}

// ChannelDirectory 报告当前登记了哪些模型渠道。
//
// Schema 只能管住 model.provider 的形状（小写标识），管不了"这个渠道存不
// 存在"——渠道是管理员在 系统配置 → 模型提供商 里建出来的，运行时可变。没
// 有这道校验的话，写错一个 provider 名要等到真的跑一次才会以
// "no client configured" 的形式暴露出来，而那时候用户已经把 Agent 存下来
// 并接进 Bundle 了。
type ChannelDirectory interface {
	ProviderNames() []string
}

// List returns one entry per agent_ref (its latest version), paginated.
func (s *Service) List(ctx context.Context, ownerID int64, q domain.PageQuery) (domain.Page[Agent], error) {
	q = q.Normalize()

	// Over-fetch by one: presence of the extra row is what proves another
	// page exists, without a second COUNT round-trip.
	rows, err := s.repo.ListLatestByOwner(ctx, ownerID, domain.PageQuery{Limit: q.Limit + 1, After: q.After})
	if err != nil {
		return domain.Page[Agent]{}, domain.Internal(err)
	}
	return domain.NewPage(rows, q.Limit, func(a Agent) string { return a.Ref }), nil
}

// ListVersions returns every version of one ref, newest first.
// ListVersions returns every version of the agent that owns version `id`,
// newest first. The service resolves the agent_ref from the version row
// so callers route by numeric id, not the DSL's agent key.
func (s *Service) ListVersions(ctx context.Context, ownerID, id int64) ([]Agent, error) {
	current, err := s.repo.GetByID(ctx, ownerID, id)
	if err != nil {
		return nil, domain.NotFound(domain.CodeResourceNotFound, "agent not found")
	}
	return s.repo.ListVersions(ctx, ownerID, current.Ref)
}

// Create validates a definition and persists it as a new Agent version.
//
// Two gates, in order, because they fail for different reasons and the
// second is meaningless if the first fails: the definition must satisfy the
// Agent DSL schema (40001), and every capability it references must exist
// and be enabled (30002, spec-06).
func (s *Service) Create(ctx context.Context, ownerID int64, def Definition) (Agent, error) {
	if def == nil {
		return Agent{}, domain.Invalid(domain.CodeValidationFailed, "definition is required")
	}

	schemaErrs, err := s.validator.Validate(def)
	if err != nil {
		return Agent{}, domain.Internal(err)
	}
	if len(schemaErrs) > 0 {
		return Agent{}, domain.Invalid(domain.CodeAgentSchemaInvalid, "Agent 定义不符合 Schema").WithDetails(schemaErrs...)
	}

	if modelErrs := s.checkModelProviders(def); len(modelErrs) > 0 {
		return Agent{}, domain.Invalid(domain.CodeAgentSchemaInvalid, "Agent 引用了没有登记的模型提供商").WithDetails(modelErrs...)
	}

	refErrs, err := s.checkCapabilities(ctx, ownerID, def)
	if err != nil {
		return Agent{}, domain.Internal(err)
	}
	if len(refErrs) > 0 {
		return Agent{}, domain.Invalid(domain.CodeResourceDisabled, "Agent 引用了不存在或已禁用的资源").WithDetails(refErrs...)
	}

	created, err := s.repo.Create(ctx, Agent{
		OwnerID:    ownerID,
		Ref:        def.Ref(),
		Version:    def.Version(),
		Definition: def,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateVersion) {
			return Agent{}, domain.Conflict(domain.CodeResourceRefDuplicate, "this agent_ref/version already exists")
		}
		return Agent{}, domain.Internal(err)
	}
	return created, nil
}

// Update edits an Agent by creating a new version from its latest existing
// version. The path ref must match definition.agent; the version is bumped
// automatically so callers don't have to know the existing version number.
// Update edits the latest version of the agent identified by `id` (a version
// row's numeric id) by creating a new auto-bumped version. The agent_ref
// is inherited from the existing row — definition.agent is NOT validated
// against the path, so renaming the DSL key mid-edit no longer routes the
// save to the wrong agent.
func (s *Service) Update(ctx context.Context, ownerID, id int64, def Definition) (Agent, error) {
	if def == nil {
		return Agent{}, domain.Invalid(domain.CodeValidationFailed, "definition is required")
	}

	schemaErrs, err := s.validator.Validate(def)
	if err != nil {
		return Agent{}, domain.Internal(err)
	}
	if len(schemaErrs) > 0 {
		return Agent{}, domain.Invalid(domain.CodeAgentSchemaInvalid, "Agent 定义不符合 Schema").WithDetails(schemaErrs...)
	}

	if modelErrs := s.checkModelProviders(def); len(modelErrs) > 0 {
		return Agent{}, domain.Invalid(domain.CodeAgentSchemaInvalid, "Agent 引用了没有登记的模型提供商").WithDetails(modelErrs...)
	}

	refErrs, err := s.checkCapabilities(ctx, ownerID, def)
	if err != nil {
		return Agent{}, domain.Internal(err)
	}
	if len(refErrs) > 0 {
		return Agent{}, domain.Invalid(domain.CodeResourceDisabled, "Agent 引用了不存在或已禁用的资源").WithDetails(refErrs...)
	}

	current, err := s.repo.GetByID(ctx, ownerID, id)
	if err != nil {
		return Agent{}, domain.NotFound(domain.CodeResourceNotFound, "agent not found")
	}

	versions, err := s.repo.ListVersions(ctx, ownerID, current.Ref)
	if err != nil {
		return Agent{}, domain.Internal(err)
	}
	latest := versions[0]

	def["version"] = bumpVersion(latest.Version)

	created, err := s.repo.Create(ctx, Agent{
		OwnerID:    ownerID,
		Ref:        current.Ref,
		Version:    def.Version(),
		Definition: def,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateVersion) {
			return Agent{}, domain.Conflict(domain.CodeResourceRefDuplicate, "this agent_ref/version already exists")
		}
		return Agent{}, domain.Internal(err)
	}
	return created, nil
}

// bumpVersion increments the last numeric segment of a dotted version string.
// Non-numeric or malformed versions are returned unchanged.
func bumpVersion(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) == 0 {
		return v
	}
	last := parts[len(parts)-1]
	n, err := strconv.Atoi(last)
	if err != nil {
		return v
	}
	parts[len(parts)-1] = strconv.Itoa(n + 1)
	return strings.Join(parts, ".")
}

// Delete removes every version of the agent identified by `id` (a version
// row's numeric id). The agent_ref is resolved from that row so the
// occupancy checks and the actual delete all operate on the same agent.
func (s *Service) Delete(ctx context.Context, ownerID, id int64) error {
	current, err := s.repo.GetByID(ctx, ownerID, id)
	if err != nil {
		return domain.NotFound(domain.CodeResourceNotFound, "agent not found")
	}
	ref := current.Ref

	subscribed, err := s.repo.CountActiveSubscribedVersions(ctx, ownerID, ref)
	if err != nil {
		return domain.Internal(err)
	}
	if subscribed > 0 {
		return domain.Conflict(domain.CodeSubscribedVersionLocked, "该 Agent 的某个版本已被订阅，无法删除，只能停止分发")
	}

	bundles, err := s.repo.FindReferencingBundles(ctx, ownerID, ref)
	if err != nil {
		return domain.Internal(err)
	}
	if len(bundles) > 0 {
		details := make([]domain.FieldError, 0, len(bundles))
		for _, b := range bundles {
			details = append(details, domain.FieldError{
				Field:  "referenced_by",
				Reason: fmt.Sprintf("Bundle %s v%s 正在引用", b.Ref, b.Version),
			})
		}
		return domain.Conflict(domain.CodeAgentVersionNotFound, "该 Agent 正被其他 Bundle 引用，无法删除").WithDetails(details...)
	}

	if err := s.repo.DeleteByRef(ctx, ownerID, ref); err != nil {
		if errors.Is(err, ErrVersionLocked) {
			return domain.Conflict(domain.CodeSubscribedVersionLocked, "该版本已被订阅，不可删除（快照隔离）")
		}
		return domain.Internal(err)
	}
	return nil
}

// checkCapabilities resolves every tools[]/skills[] entry, returning one
// field error per bad reference. Not-found and disabled are reported with
// different wording even though both map to the same business code — the
// fix differs, so the message has to.
func (s *Service) checkCapabilities(ctx context.Context, ownerID int64, def Definition) ([]domain.FieldError, error) {
	var errs []domain.FieldError

	check := func(kind, noun string, refs []string, lookup func(context.Context, int64, string) (RefStatus, error)) error {
		for i, ref := range refs {
			status, err := lookup(ctx, ownerID, ref)
			if err != nil {
				return err
			}
			field := fmt.Sprintf("capabilities.%s[%d]", kind, i)
			switch {
			case !status.Found:
				errs = append(errs, domain.FieldError{Field: field, Reason: fmt.Sprintf("%s %q does not exist", noun, ref)})
			case !status.Enabled:
				errs = append(errs, domain.FieldError{Field: field, Reason: fmt.Sprintf("%s %q is disabled", noun, ref)})
			}
		}
		return nil
	}

	if err := check("tools", "resource", def.Tools(), s.catalog.ToolStatus); err != nil {
		return nil, err
	}
	if err := check("skills", "skill", def.Skills(), s.catalog.SkillStatus); err != nil {
		return nil, err
	}
	return errs, nil
}

// checkModelProviders 校验 model.provider 和 model.fallback[] 里的渠道都
// 已经登记。
//
// 只查名字存不存在，不查凭据配没配：凭据是每个用户各自的事，一个管理员建
// 好的渠道对还没配 key 的用户来说仍然是"存在的"。
func (s *Service) checkModelProviders(def Definition) []domain.FieldError {
	if s.channels == nil {
		return nil
	}
	known := make(map[string]bool)
	for _, name := range s.channels.ProviderNames() {
		known[name] = true
	}

	// 一个渠道都没登记时不拦：这时候拦下来只会让新装的实例连一个 Agent 都
	// 建不了，而用户看到的报错还指不到该去哪配。
	if len(known) == 0 {
		return nil
	}

	var errs []domain.FieldError
	available := strings.Join(s.channels.ProviderNames(), ", ")
	if p := def.ModelProvider(); p != "" && !known[p] {
		errs = append(errs, domain.FieldError{
			Field:  "model.provider",
			Reason: fmt.Sprintf("没有登记名为 %q 的模型提供商，当前可用：%s", p, available),
		})
	}
	for i, spec := range def.ModelFallbacks() {
		provider, _, ok := strings.Cut(spec, "/")
		if !ok || known[provider] {
			// 形状不对由 schema 管；这里只管存在性。
			continue
		}
		errs = append(errs, domain.FieldError{
			Field:  fmt.Sprintf("model.fallback[%d]", i),
			Reason: fmt.Sprintf("没有登记名为 %q 的模型提供商，当前可用：%s", provider, available),
		})
	}
	return errs
}
