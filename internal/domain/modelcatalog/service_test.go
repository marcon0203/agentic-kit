package modelcatalog_test

import (
	"context"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/modelcatalog"
)

// ── Fakes ────────────────────────────────────────────────────────────

// fakeRepo 只实现模型参数相关测试走到的那几个方法，其余留空——这个包的
// Repository 有十几个方法，为一个测试全部造假实现只会掩盖"测试根本没走到
// 那条路"。
type fakeRepo struct {
	provider modelcatalog.Provider
	created  *modelcatalog.NewModel
	// UpdateModelParams 落库的最新参数，nil 表示还没被调过。
	savedParams map[string]any
}

func (f *fakeRepo) CreateProvider(context.Context, modelcatalog.NewProvider) (modelcatalog.Provider, error) {
	return modelcatalog.Provider{}, nil
}
func (f *fakeRepo) ListProviders(context.Context) ([]modelcatalog.Provider, error) { return nil, nil }
func (f *fakeRepo) GetProvider(_ context.Context, _ int64) (modelcatalog.Provider, error) {
	return f.provider, nil
}
func (f *fakeRepo) SetProviderStatus(context.Context, int64, int16) error { return nil }
func (f *fakeRepo) DeleteProvider(context.Context, int64) error           { return nil }
func (f *fakeRepo) SetProviderCredential(context.Context, int64, *string, string) error {
	return nil
}
func (f *fakeRepo) CreateModel(_ context.Context, in modelcatalog.NewModel) (modelcatalog.Model, error) {
	f.created = &in
	return modelcatalog.Model{ID: 1, Params: in.Params}, nil
}
func (f *fakeRepo) ListModelsForProvider(context.Context, int64) ([]modelcatalog.Model, error) {
	return nil, nil
}
func (f *fakeRepo) GetModel(_ context.Context, _ int64) (modelcatalog.Model, error) {
	return modelcatalog.Model{ID: 1, ProviderID: 7, Model: "k3"}, nil
}
func (f *fakeRepo) SetModelStatus(context.Context, int64, int16) error { return nil }
func (f *fakeRepo) UpdateModelParams(_ context.Context, _ int64, params map[string]any) error {
	f.savedParams = params
	return nil
}
func (f *fakeRepo) DeleteModel(context.Context, int64) error { return nil }
func (f *fakeRepo) ListPublic(context.Context) ([]modelcatalog.CatalogEntry, error) {
	return nil, nil
}
func (f *fakeRepo) ListChannelDescriptors(context.Context) ([]modelcatalog.ChannelDescriptor, error) {
	return nil, nil
}
func (f *fakeRepo) ListChannelModelParams(context.Context) ([]modelcatalog.ChannelModelParams, error) {
	return nil, nil
}

type fakeAdmins struct{}

func (fakeAdmins) IsAdmin(context.Context, int64) (bool, error)               { return true, nil }
func (fakeAdmins) HasPermission(context.Context, int64, string) (bool, error) { return true, nil }

// reloaded 记录渠道注册表是否被重建过——模型参数属于网关运行时状态，改完
// 不重建的话要等下一次重启才生效。
type reloaded struct{ count int }

func (r *reloaded) Reload(context.Context) error { r.count++; return nil }

// anthropicDescriptor 是带必填 max_tokens 声明的最小快照。
const anthropicDescriptor = `{
  "descriptor_version": 1,
  "id": "kimi", "label": "Kimi", "wire": "anthropic.messages.v1",
  "capabilities": ["text"],
  "base_url": "https://example.test",
  "credentials": [{"name": "api_key", "type": "secret", "label": "K", "required": true}],
  "auth": {"driver": "bearer", "credential": "api_key"},
  "request_params": [
    {"name": "max_tokens", "label": "最大输出 tokens", "type": "int", "required": true, "min": 1, "max": 65536},
    {"name": "temperature", "label": "温度", "type": "number", "min": 0, "max": 2}
  ],
  "complete": {"method": "POST", "path": "/v1/messages",
    "body": {"model": "$.model", "messages": "$.messages", "max_tokens": "$.max_tokens"}}
}`

func newTestService(t *testing.T) (*modelcatalog.Service, *fakeRepo, *reloaded) {
	t.Helper()
	repo := &fakeRepo{provider: modelcatalog.Provider{ID: 7, Key: "kimi", Descriptor: []byte(anthropicDescriptor)}}
	rl := &reloaded{}
	svc := modelcatalog.NewService(repo, fakeAdmins{}, nil, nil, rl)
	return svc, repo, rl
}

// Anthropic 线协议必填的 max_tokens 在添加模型时就要收齐：缺了不给落库——
// 存一个一调就 400 的模型，比当场报"填得不完整"难查得多。
func TestCreateModel_MissingRequiredParamIsRejected(t *testing.T) {
	svc, repo, rl := newTestService(t)
	_, err := svc.CreateModel(context.Background(), 1, modelcatalog.NewModel{
		ProviderID: 7, Model: "k3", DisplayName: "K3", Modality: modelcatalog.ModalityText,
	})
	if err == nil {
		t.Fatal("缺必填参数必须被拒绝")
	}
	if repo.created != nil {
		t.Error("校验不过不该落库")
	}
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeValidationFailed {
		t.Fatalf("应是 422 校验错误，得到 %v", err)
	}
	found := false
	for _, f := range de.Details {
		if f.Field == "params.max_tokens" {
			found = true
		}
	}
	if !found {
		t.Errorf("错误应定位到 params.max_tokens，得到 %+v", de.Details)
	}
	if rl.count != 0 {
		t.Error("没落库就不该重建注册表")
	}
}

func TestCreateModel_ValidatesParamShape(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params map[string]any
	}{
		{"未知参数", map[string]any{"top_k": 1}},
		{"非整数", map[string]any{"max_tokens": 1.5}},
		{"越界", map[string]any{"max_tokens": 1000000}},
		{"非数字", map[string]any{"max_tokens": "8192"}},
	} {
		svc, repo, _ := newTestService(t)
		_, err := svc.CreateModel(context.Background(), 1, modelcatalog.NewModel{
			ProviderID: 7, Model: "k3", DisplayName: "K3", Modality: modelcatalog.ModalityText,
			Params: tc.params,
		})
		if err == nil {
			t.Errorf("%s 应被拒绝", tc.name)
		}
		if repo.created != nil {
			t.Errorf("%s 不该落库", tc.name)
		}
	}
}

func TestCreateModel_ValidParamsAreStoredAndChannelsReload(t *testing.T) {
	svc, _, rl := newTestService(t)
	created, err := svc.CreateModel(context.Background(), 1, modelcatalog.NewModel{
		ProviderID: 7, Model: "k3", DisplayName: "K3", Modality: modelcatalog.ModalityText,
		Params: map[string]any{"max_tokens": 8192.0, "temperature": 0.5},
	})
	if err != nil {
		t.Fatalf("合法参数不该被拒: %v", err)
	}
	if created.Params["max_tokens"] != 8192.0 {
		t.Errorf("参数应原样落库: %+v", created.Params)
	}
	if rl.count == 0 {
		t.Error("模型参数落库后应立即重建渠道注册表")
	}
}

func TestUpdateModelParams_RevalidatesAgainstDescriptor(t *testing.T) {
	svc, repo, rl := newTestService(t)
	if err := svc.UpdateModelParams(context.Background(), 1, 9, map[string]any{"max_tokens": 512.0}); err != nil {
		t.Fatalf("合法更新不该被拒: %v", err)
	}
	if repo.savedParams["max_tokens"] != 512.0 || rl.count == 0 {
		t.Errorf("参数应落库并触发注册表重建: %+v / reload=%d", repo.savedParams, rl.count)
	}

	// 改成越界值：拦下，不落库。
	svc2, repo2, _ := newTestService(t)
	if err := svc2.UpdateModelParams(context.Background(), 1, 9, map[string]any{"max_tokens": 0.0}); err == nil {
		t.Fatal("越界更新必须被拒绝")
	}
	if repo2.savedParams != nil {
		t.Error("越界更新不该落库")
	}
}
