package modelcatalog

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

var ErrNotFound = errors.New("modelcatalog: not found")
var ErrDuplicate = errors.New("modelcatalog: already exists")

// Repository is the persistence port. AdminDirectory gates every write —
// 系统配置 → 模型提供商 is an admin-only surface, unlike modelcenter's
// per-user provider credentials.
type Repository interface {
	CreateProvider(ctx context.Context, p NewProvider) (Provider, error)
	ListProviders(ctx context.Context) ([]Provider, error)
	GetProvider(ctx context.Context, id int64) (Provider, error)
	SetProviderStatus(ctx context.Context, id int64, status int16) error
	DeleteProvider(ctx context.Context, id int64) error
	// SetProviderCredential stores the org-wide default credential for a
	// provider. encryptedKey nil means "leave the stored key as-is".
	SetProviderCredential(ctx context.Context, id int64, encryptedKey *string, baseURL string) error

	CreateModel(ctx context.Context, providerID int64, model, displayName, description string, modality Modality, featured bool) (Model, error)
	ListModelsForProvider(ctx context.Context, providerID int64) ([]Model, error)
	SetModelStatus(ctx context.Context, id int64, status int16) error
	DeleteModel(ctx context.Context, id int64) error

	ListPublic(ctx context.Context) ([]CatalogEntry, error)

	// ListChannelDescriptors 返回全部**启用中**提供商的渠道描述符快照。
	ListChannelDescriptors(ctx context.Context) ([]ChannelDescriptor, error)
}

// ChannelDescriptor 是一个提供商的渠道描述符快照。
type ChannelDescriptor struct {
	Key        string
	Descriptor []byte
}

// Channels 是给渠道注册表重建用的读接口，不做权限判定——它的调用方是进程
// 自己（启动时和写操作之后），不是某个用户的请求。
func (s *Service) Channels(ctx context.Context) ([]ChannelDescriptor, error) {
	return s.repo.ListChannelDescriptors(ctx)
}

// ChannelTemplates 是协议模板的端口：把一个模板 + 管理员填的
// key/显示名/接口地址，渲染成一份**已完整校验**的渠道描述符快照。
//
// 校验在这一步做而不是等到第一次调用：一个连协议都写不对的提供商存下来，
// 只会让人在"为什么这个模型调不通"上耗时间。由 internal/channeltemplates
// 实现。
type ChannelTemplates interface {
	Instantiate(templateID, key, label, baseURL string) (descriptorJSON []byte, err error)
}

// ChannelReloader 让服务在提供商增删改之后重建 modelgateway 的渠道注册
// 表。整体重建而不是增量同步：每次都从库里读全量，注册表和库不会出现"某次
// 删除漏掉了"的偏差。
type ChannelReloader interface {
	Reload(ctx context.Context) error
}

// NewProvider 是创建一个模型提供商需要的全部输入。
type NewProvider struct {
	Key         string
	DisplayName string
	Icon        string
	BaseURL     string
	Template    string
	Descriptor  []byte
}

type AdminDirectory interface {
	IsAdmin(ctx context.Context, userID int64) (bool, error)
	HasPermission(ctx context.Context, userID int64, key string) (bool, error)
}

// Cipher encrypts the org-wide default api_key before it's stored — the
// same AES-256 key already used for per-user provider credentials
// (internal/adapter/crypto.Cipher), so both live under one key-rotation
// story. The plaintext never round-trips back out through this package.
type Cipher interface {
	Encrypt(plaintext string) (string, error)
}

// Permission keys this package gates on — seeded in migrations/0017_rbac.up.sql
// and editable per-Role from 系统配置 → 角色权限. is_admin still bypasses
// all of them (see requireAccess), so a superadmin never depends on roles
// being configured correctly.
const (
	PermProviderView   = "model_catalog.provider.view"
	PermProviderCreate = "model_catalog.provider.create"
	PermProviderToggle = "model_catalog.provider.toggle"
	PermProviderDelete = "model_catalog.provider.delete"
	PermModelCreate    = "model_catalog.model.create"
	PermModelToggle    = "model_catalog.model.toggle"
	PermModelDelete    = "model_catalog.model.delete"
)

type Service struct {
	repo      Repository
	admins    AdminDirectory
	cipher    Cipher
	templates ChannelTemplates
	channels  ChannelReloader
}

func NewService(repo Repository, admins AdminDirectory, cipher Cipher, templates ChannelTemplates, channels ChannelReloader) *Service {
	return &Service{repo: repo, admins: admins, cipher: cipher, templates: templates, channels: channels}
}

// SetChannelReloader 回填渠道注册表的重建器。它和 Service 互相需要（重建
// 器要从 Service 读描述符），所以在构造之后回填，而不是在 NewService 里传
// ——为了避开这个循环把"读描述符"复制一份到别处，才是更差的选择。
func (s *Service) SetChannelReloader(r ChannelReloader) { s.channels = r }

// reloadChannels 在提供商增删改之后重建渠道注册表。失败只记在返回值上由调
// 用方决定——写已经落库了，这里再报错会让管理员以为操作没成功、然后重试一
// 次撞上"key 已存在"。
func (s *Service) reloadChannels(ctx context.Context) {
	if s.channels == nil {
		return
	}
	_ = s.channels.Reload(ctx)
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

// providerKeyPattern 约束 provider key：它会作为 Agent DSL 里
// `model.provider` 的取值（schemas/agent.schema.json 的同一条 pattern），
// 也会作为凭据表的 provider 列。放开成自由文本的话，一个带斜杠的 key 会让
// "provider/model" 这个引用格式直接失去意义。
var providerKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// CreateProvider 登记一个模型提供商——同时也就是一个可调用的渠道。
//
// 必须挑一个协议模板：平台开箱不带任何渠道，"新建一个提供商"这个动作的实
// 质就是"从模板实例化一个渠道"。模板渲染出的描述符会被完整校验并跑一遍
// fixtures，过不了就不落库。
func (s *Service) CreateProvider(ctx context.Context, userID int64, in NewProvider) (Provider, error) {
	if err := s.requireAccess(ctx, userID, PermProviderCreate); err != nil {
		return Provider{}, err
	}

	in.Key = strings.TrimSpace(in.Key)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.Template = strings.TrimSpace(in.Template)

	var fields []domain.FieldError
	if in.Key == "" {
		fields = append(fields, domain.FieldError{Field: "key", Reason: "required"})
	} else if !providerKeyPattern.MatchString(in.Key) {
		fields = append(fields, domain.FieldError{
			Field:  "key",
			Reason: "只能用小写字母开头，后面接小写字母、数字、下划线或短横线，最长 32 个字符",
		})
	}
	if in.DisplayName == "" {
		fields = append(fields, domain.FieldError{Field: "display_name", Reason: "required"})
	}
	if in.Template == "" {
		fields = append(fields, domain.FieldError{Field: "template", Reason: "required"})
	}
	if len(fields) > 0 {
		return Provider{}, domain.Invalid(domain.CodeValidationFailed, "validation failed").WithDetails(fields...)
	}

	if s.templates == nil {
		return Provider{}, domain.Internal(errors.New("modelcatalog: 没有配置协议模板"))
	}
	rendered, err := s.templates.Instantiate(in.Template, in.Key, in.DisplayName, in.BaseURL)
	if err != nil {
		// 模板名写错、接口地址缺失、渲染出的描述符校验不过——都是管理员填
		// 错了东西，原样把理由说清楚，不要包成一句"创建失败"。
		return Provider{}, domain.Invalid(domain.CodeValidationFailed, err.Error()).
			WithDetails(domain.FieldError{Field: "template", Reason: err.Error()})
	}
	in.Descriptor = rendered

	created, err := s.repo.CreateProvider(ctx, in)
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			return Provider{}, domain.Conflict(domain.CodeCatalogProviderKeyDup, "该 provider key 已存在")
		}
		return Provider{}, domain.Internal(err)
	}
	s.reloadChannels(ctx)
	return created, nil
}

// ListProviders returns every catalog provider, enabled or not. Admin only
// — 模型广场 itself only ever sees providers through List's join.
func (s *Service) ListProviders(ctx context.Context, userID int64) ([]Provider, error) {
	if err := s.requireAccess(ctx, userID, PermProviderView); err != nil {
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
	if err := s.requireAccess(ctx, userID, PermProviderToggle); err != nil {
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
	// 停用要立刻调不通，而不是等下次重启。
	s.reloadChannels(ctx)
	return nil
}

// DeleteProvider removes a provider and, via ON DELETE CASCADE, every model
// registered under it — deleting a provider is deleting its whole catalog
// branch, not something that can leave orphaned models behind.
func (s *Service) DeleteProvider(ctx context.Context, userID, providerID int64) error {
	if err := s.requireAccess(ctx, userID, PermProviderDelete); err != nil {
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
	s.reloadChannels(ctx)
	return nil
}

// SetProviderCredential registers/updates a provider's org-wide default
// api_key + base_url — the admin-managed fallback credential used when a
// user has no personal connection for that provider (see
// postgres.ProviderKeyStore.Keys). Gated on the same permission as creating
// a provider: this is still "managing the provider entry", just a field on
// it that happens to be secret. apiKey empty means "leave the stored key
// untouched", so an admin can update base_url alone without re-entering a
// key that's already set.
func (s *Service) SetProviderCredential(ctx context.Context, userID, providerID int64, apiKey, baseURL string) error {
	if err := s.requireAccess(ctx, userID, PermProviderCreate); err != nil {
		return err
	}
	if _, err := s.repo.GetProvider(ctx, providerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.NotFound(domain.CodeCatalogProviderNotFound, "provider 不存在")
		}
		return domain.Internal(err)
	}

	var encryptedKey *string
	apiKey = strings.TrimSpace(apiKey)
	if apiKey != "" {
		encrypted, err := s.cipher.Encrypt(apiKey)
		if err != nil {
			return domain.Internal(err)
		}
		encryptedKey = &encrypted
	}

	if err := s.repo.SetProviderCredential(ctx, providerID, encryptedKey, strings.TrimSpace(baseURL)); err != nil {
		return domain.Internal(err)
	}
	return nil
}

// CreateModel registers a model under a provider. Admin only.
func (s *Service) CreateModel(ctx context.Context, userID, providerID int64, model, displayName, description string, modality Modality, featured bool) (Model, error) {
	if err := s.requireAccess(ctx, userID, PermModelCreate); err != nil {
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
	if err := s.requireAccess(ctx, userID, PermProviderView); err != nil {
		return nil, err
	}
	models, err := s.repo.ListModelsForProvider(ctx, providerID)
	if err != nil {
		return nil, domain.Internal(err)
	}
	return models, nil
}

func (s *Service) SetModelStatus(ctx context.Context, userID, modelID int64, enabled bool) error {
	if err := s.requireAccess(ctx, userID, PermModelToggle); err != nil {
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
	if err := s.requireAccess(ctx, userID, PermModelDelete); err != nil {
		return err
	}
	if err := s.repo.DeleteModel(ctx, modelID); err != nil {
		return domain.Internal(err)
	}
	return nil
}

// requireAccess grants a caller who is either a superadmin (is_admin) or
// whose assigned Role(s) include the given permission key — the button-
// level RBAC check, with is_admin as the bypass that keeps existing admin
// accounts working while roles are configured.
func (s *Service) requireAccess(ctx context.Context, userID int64, permission string) error {
	isAdmin, err := s.admins.IsAdmin(ctx, userID)
	if err != nil {
		// Fail closed: a lookup failure must never read as "is admin".
		return domain.Forbidden(domain.CodeForbidden, "permission required: "+permission)
	}
	if isAdmin {
		return nil
	}
	has, err := s.admins.HasPermission(ctx, userID, permission)
	if err != nil || !has {
		return domain.Forbidden(domain.CodeForbidden, "permission required: "+permission)
	}
	return nil
}
