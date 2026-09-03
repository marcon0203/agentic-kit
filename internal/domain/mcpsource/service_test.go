package mcpsource_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/mcpsource"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// ── 测试替身 ─────────────────────────────────────────────────────────

type reviewCall struct {
	id     int64
	status mcpsource.ReviewStatus
}

type fakeRepo struct {
	sources   []mcpsource.Source
	servers   map[int64]mcpsource.MarketServer
	reviews   []reviewCall
	lastQuery mcpsource.ReviewQuery
	replaced  []mcpsource.FetchedServer
	syncErr   string
	created   []mcpsource.CreateParams
	keys      []string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{servers: map[int64]mcpsource.MarketServer{}}
}

func (f *fakeRepo) Create(_ context.Context, p mcpsource.CreateParams) (mcpsource.Source, error) {
	src := mcpsource.Source{
		ID: int64(len(f.sources) + 1), Name: p.Name, BaseURL: p.BaseURL,
		Protocol: p.Protocol, APIPrefix: p.APIPrefix,
		HasAPIKey: p.EncryptedAPIKey != "", Status: 1,
	}
	f.created = append(f.created, p)
	f.keys = append(f.keys, p.EncryptedAPIKey)
	f.sources = append(f.sources, src)
	return src, nil
}

func (f *fakeRepo) EncryptedAPIKey(_ context.Context, id int64) (string, error) {
	if int(id) <= len(f.keys) {
		return f.keys[id-1], nil
	}
	return "", nil
}

func (f *fakeRepo) SetEncryptedAPIKey(_ context.Context, id int64, encrypted string) error {
	if int(id) <= len(f.keys) {
		f.keys[id-1] = encrypted
	}
	return nil
}
func (f *fakeRepo) List(context.Context) ([]mcpsource.Source, error) { return f.sources, nil }
func (f *fakeRepo) Get(_ context.Context, id int64) (mcpsource.Source, error) {
	for _, s := range f.sources {
		if s.ID == id {
			s.LastSyncError = f.syncErr
			return s, nil
		}
	}
	return mcpsource.Source{}, domain.NotFound(mcpsource.CodeMCPSourceNotFound, "not found")
}
func (f *fakeRepo) GetByURL(_ context.Context, baseURL string) (mcpsource.Source, error) {
	for _, s := range f.sources {
		if s.BaseURL == baseURL {
			return s, nil
		}
	}
	return mcpsource.Source{}, errors.New("no rows")
}
func (f *fakeRepo) Delete(context.Context, int64) error     { return nil }
func (f *fakeRepo) MarkSynced(context.Context, int64) error { f.syncErr = ""; return nil }
func (f *fakeRepo) MarkSyncError(_ context.Context, _ int64, m string) error {
	f.syncErr = m
	return nil
}
func (f *fakeRepo) ReplaceServers(_ context.Context, _ int64, s []mcpsource.FetchedServer) error {
	f.replaced = s
	return nil
}
func (f *fakeRepo) ListMarketServers(context.Context) ([]mcpsource.MarketServer, error) {
	out := make([]mcpsource.MarketServer, 0, len(f.servers))
	for _, s := range f.servers {
		if s.ReviewStatus == mcpsource.ReviewApproved {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeRepo) GetMarketServer(_ context.Context, id int64) (mcpsource.MarketServer, error) {
	s, ok := f.servers[id]
	if !ok {
		return mcpsource.MarketServer{}, domain.NotFound(mcpsource.CodeMarketMCPNotFound, "not found")
	}
	return s, nil
}
func (f *fakeRepo) ListMarketServersForReview(_ context.Context, q mcpsource.ReviewQuery) ([]mcpsource.MarketServer, error) {
	f.lastQuery = q
	return nil, nil
}
func (f *fakeRepo) CountMarketServersForReview(context.Context, mcpsource.ReviewQuery) (int64, error) {
	return 0, nil
}
func (f *fakeRepo) CountByReviewStatus(context.Context, int64) (map[mcpsource.ReviewStatus]int64, error) {
	return map[mcpsource.ReviewStatus]int64{}, nil
}
func (f *fakeRepo) SetReview(_ context.Context, id int64, status mcpsource.ReviewStatus, _ string, _ int64) error {
	f.reviews = append(f.reviews, reviewCall{id, status})
	return nil
}

type fakeAdmins struct{ admin bool }

func (f fakeAdmins) IsAdmin(context.Context, int64) (bool, error) { return f.admin, nil }

type fakeFetcher struct {
	servers []mcpsource.FetchedServer
	err     error
	// gotTarget 记下最后一次同步收到的 target，用来断言前缀和密钥确实传下
	// 去了——这两样传丢了不会报错，只会静默地拉回空列表。
	gotTarget *mcpsource.FetchTarget
}

func (f *fakeFetcher) FetchList(_ context.Context, t mcpsource.FetchTarget) ([]mcpsource.FetchedServer, error) {
	f.gotTarget = &t
	return f.servers, f.err
}

// fakeRegistry 是 FetcherRegistry 的替身：两个协议，一个要密钥一个不要。
type fakeRegistry struct {
	fetcher *fakeFetcher
	// unknown 为真时 FetcherFor 一律报错，模拟"库里存着一个本部署不认识的
	// 协议"（降级部署后的老数据）。
	unknown bool
}

func newFakeRegistry() *fakeRegistry { return &fakeRegistry{fetcher: &fakeFetcher{}} }

func (r *fakeRegistry) FetcherFor(p mcpsource.Protocol) (mcpsource.Fetcher, error) {
	if r.unknown {
		return nil, errors.New("不认识这个源协议：" + string(p))
	}
	for _, spec := range r.Protocols() {
		if spec.ID == p {
			return r.fetcher, nil
		}
	}
	return nil, errors.New("不认识这个源协议：" + string(p))
}

func (r *fakeRegistry) Protocols() []mcpsource.ProtocolSpec {
	return []mcpsource.ProtocolSpec{
		{ID: mcpsource.ProtocolMCPRegistry, Label: "MCP Registry 规范",
			DefaultBaseURL: "https://registry.modelcontextprotocol.io", DefaultPrefix: "/v0"},
		{ID: mcpsource.ProtocolSmithery, Label: "Smithery",
			DefaultBaseURL: "https://registry.smithery.ai", RequiresAPIKey: true},
	}
}

// fakeCipher 用一个可见的前缀代替真加密：断言"存下去的不是明文"比断言
// 密文内容有意义得多。
type fakeCipher struct{ failDecrypt bool }

func (c fakeCipher) Encrypt(plaintext string) (string, error) { return "enc:" + plaintext, nil }
func (c fakeCipher) Decrypt(ciphertext string) (string, error) {
	if c.failDecrypt {
		return "", errors.New("解不开")
	}
	return strings.TrimPrefix(ciphertext, "enc:"), nil
}

type fakeInstaller struct{ created []resource.CreateCommand }

func (f *fakeInstaller) Create(_ context.Context, _ int64, cmd resource.CreateCommand) (resource.Resource, error) {
	f.created = append(f.created, cmd)
	return resource.Resource{ID: 1, Ref: cmd.Ref, Kind: resource.Kind(cmd.Kind)}, nil
}

const adminID = int64(1)

// ── 用例 ─────────────────────────────────────────────────────────────

// 非管理员碰不到源管理面的任何一个动作。权限判定收在 Service 里，就是为了
// 不用在每个 handler 上再检一遍。
func TestSourceAdminSurfaceIsAdminOnly(t *testing.T) {
	svc := mcpsource.NewService(newFakeRepo(), fakeAdmins{admin: false}, newFakeRegistry(), fakeCipher{}, nil)
	ctx := context.Background()

	if _, err := svc.List(ctx, 7); !isForbidden(err) {
		t.Errorf("List 应该 403，得到 %v", err)
	}
	if _, err := svc.Create(ctx, 7, mcpsource.CreateParams{Name: "官方", BaseURL: "https://registry.modelcontextprotocol.io"}); !isForbidden(err) {
		t.Errorf("Create 应该 403，得到 %v", err)
	}
	if _, err := svc.Sync(ctx, 7, 1); !isForbidden(err) {
		t.Errorf("Sync 应该 403，得到 %v", err)
	}
	if err := svc.Delete(ctx, 7, 1); !isForbidden(err) {
		t.Errorf("Delete 应该 403，得到 %v", err)
	}
	if _, err := svc.ListForReview(ctx, 7, mcpsource.ReviewQuery{}); !isForbidden(err) {
		t.Errorf("ListForReview 应该 403，得到 %v", err)
	}
	if err := svc.Review(ctx, 7, []int64{1}, mcpsource.ReviewApproved, ""); !isForbidden(err) {
		t.Errorf("Review 应该 403，得到 %v", err)
	}
}

func isForbidden(err error) bool {
	derr, ok := domain.AsError(err)
	return ok && derr.Code == mcpsource.CodeMCPSourceForbidden
}

// 登记地址统一规范化：尾斜杠去掉，带路径的直接打回——同一个注册中心写成
// 三种形式会同步出三份重复缓存。
func TestCreateNormalizesBaseURL(t *testing.T) {
	repo := newFakeRepo()
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, nil)

	src, err := svc.Create(context.Background(), adminID, mcpsource.CreateParams{Name: "官方注册中心", BaseURL: "https://registry.modelcontextprotocol.io/"})
	if err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	if src.BaseURL != "https://registry.modelcontextprotocol.io" {
		t.Errorf("尾斜杠应该被去掉，得到 %q", src.BaseURL)
	}

	if _, err := svc.Create(context.Background(), adminID, mcpsource.CreateParams{Name: "带路径", BaseURL: "https://example.com/v0/servers"}); err == nil {
		t.Error("带路径的地址应该被拒绝")
	}
	if _, err := svc.Create(context.Background(), adminID, mcpsource.CreateParams{Name: "重复", BaseURL: "https://registry.modelcontextprotocol.io"}); err == nil {
		t.Error("同一个地址不该登记两次")
	}
}

// 同步失败要留痕：last_sync_error 落库、源本身照常返回，管理员在设置页上
// 能直接看到原因，而不是"点了没反应"。
func TestSyncRecordsUpstreamFailure(t *testing.T) {
	repo := newFakeRepo()
	reg := newFakeRegistry()
	reg.fetcher.err = errors.New("源返回 502")
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, reg, fakeCipher{}, nil)
	src, _ := svc.Create(context.Background(), adminID, mcpsource.CreateParams{Name: "官方", BaseURL: "https://registry.modelcontextprotocol.io"})

	got, err := svc.Sync(context.Background(), adminID, src.ID)
	if err == nil {
		t.Fatal("上游失败时 Sync 应该返回错误")
	}
	if got.LastSyncError == "" {
		t.Error("失败原因必须落在 last_sync_error 上")
	}
}

// 审核台的分页参数要夹回可用范围：page_size 传 0 走默认 15，传 99999 夹到
// 上限，否则分页等于没做。
func TestListForReviewClampsPaging(t *testing.T) {
	repo := newFakeRepo()
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, nil)
	ctx := context.Background()

	if _, err := svc.ListForReview(ctx, adminID, mcpsource.ReviewQuery{}); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if repo.lastQuery.Limit != mcpsource.ReviewPageSizeDefault {
		t.Errorf("默认每页应为 %d，得到 %d", mcpsource.ReviewPageSizeDefault, repo.lastQuery.Limit)
	}

	if _, err := svc.ListForReview(ctx, adminID, mcpsource.ReviewQuery{Limit: 99999, Offset: -5}); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if repo.lastQuery.Limit != mcpsource.ReviewPageSizeMax {
		t.Errorf("每页上限应为 %d，得到 %d", mcpsource.ReviewPageSizeMax, repo.lastQuery.Limit)
	}
	if repo.lastQuery.Offset != 0 {
		t.Errorf("负偏移应夹回 0，得到 %d", repo.lastQuery.Offset)
	}

	if _, err := svc.ListForReview(ctx, adminID, mcpsource.ReviewQuery{Status: "whatever"}); err == nil {
		t.Error("未知审核状态应该被拒绝，而不是当成不筛")
	}
}

// 未过审的条目对普通用户不存在——否则审核只挡住了列表，详情页仍是绕过去
// 的一条路。管理员照常看得到（他就是来审的）。
func TestGetMarketServerHidesUnapprovedFromUsers(t *testing.T) {
	repo := newFakeRepo()
	repo.servers[9] = mcpsource.MarketServer{ID: 9, Slug: "io.github.x/y", ReviewStatus: mcpsource.ReviewPending}

	user := mcpsource.NewService(repo, fakeAdmins{admin: false}, newFakeRegistry(), fakeCipher{}, nil)
	if _, err := user.GetMarketServer(context.Background(), 7, 9); err == nil {
		t.Error("待审核条目不该对普通用户可见")
	}

	admin := mcpsource.NewService(repo, fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, nil)
	if _, err := admin.GetMarketServer(context.Background(), adminID, 9); err != nil {
		t.Errorf("管理员应该看得到待审核条目: %v", err)
	}
}

// 安装接口是审核的第二道门：知道 id 的人不能靠直接 POST 把没过审的第三方
// 地址接进来。
func TestInstallRejectsUnapproved(t *testing.T) {
	repo := newFakeRepo()
	repo.servers[3] = mcpsource.MarketServer{
		ID: 3, Slug: "io.github.x/y", RemoteURL: "https://mcp.example.com/mcp",
		ReviewStatus: mcpsource.ReviewPending,
	}
	installer := &fakeInstaller{}
	svc := mcpsource.NewService(repo, fakeAdmins{admin: false}, newFakeRegistry(), fakeCipher{}, installer)

	_, err := svc.Install(context.Background(), 7, 3)
	derr, ok := domain.AsError(err)
	if !ok || derr.Code != mcpsource.CodeMarketMCPNotPassed {
		t.Fatalf("未过审的条目不该装得上，得到 %v", err)
	}
	if len(installer.created) != 0 {
		t.Error("被拒绝的安装不该建出资源")
	}
}

// 只能本地起进程的条目（上游只给 packages、没有 remotes）平台连不上。与其
// 建一条一探测就红的资源，不如在这里说清楚。
func TestInstallRejectsLocalOnlyServer(t *testing.T) {
	repo := newFakeRepo()
	repo.servers[4] = mcpsource.MarketServer{ID: 4, Slug: "io.github.x/y", ReviewStatus: mcpsource.ReviewApproved}
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, &fakeInstaller{})

	_, err := svc.Install(context.Background(), adminID, 4)
	derr, ok := domain.AsError(err)
	if !ok || derr.Code != mcpsource.CodeMarketMCPNotRemote {
		t.Fatalf("没有远端地址的条目应该给出明确理由，得到 %v", err)
	}
}

// 装上之后是一条普通的 mcp 资源：远端地址进 config.endpoint（和手工接入
// 走同一条管线），来源记在 installed_from 上。ref 由上游限定名转写而来。
func TestInstallCreatesMCPResourceFromSnapshot(t *testing.T) {
	repo := newFakeRepo()
	repo.servers[5] = mcpsource.MarketServer{
		ID: 5, SourceID: 2, SourceName: "官方", SourceBaseURL: "https://registry.modelcontextprotocol.io",
		Slug: "io.github.domdomegg/Airtable.MCP-Server", Name: "Airtable",
		Version: "1.2.0", RemoteURL: "https://mcp.example.com/mcp", RemoteType: "streamable-http",
		ReviewStatus: mcpsource.ReviewApproved,
	}
	installer := &fakeInstaller{}
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, installer)

	if _, err := svc.Install(context.Background(), adminID, 5); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	if len(installer.created) != 1 {
		t.Fatalf("应该建出一条资源，得到 %d 条", len(installer.created))
	}
	cmd := installer.created[0]
	if cmd.Kind != "mcp" {
		t.Errorf("资源类型应为 mcp，得到 %q", cmd.Kind)
	}
	// io.github.domdomegg/Airtable.MCP-Server → airtable-mcp-server
	if cmd.Ref != "airtable-mcp-server" {
		t.Errorf("ref 转写不对: %q", cmd.Ref)
	}
	if cmd.Config["endpoint"] != "https://mcp.example.com/mcp" {
		t.Errorf("远端地址必须落到 config.endpoint，得到 %v", cmd.Config["endpoint"])
	}
	from, ok := cmd.Config["installed_from"].(map[string]any)
	if !ok || from["slug"] != "io.github.domdomegg/Airtable.MCP-Server" {
		t.Errorf("安装来源没记全: %v", cmd.Config["installed_from"])
	}
}

// 批量审核是主路径：一次请求给一批条目下同一个结论。
func TestReviewAppliesToWholeBatch(t *testing.T) {
	repo := newFakeRepo()
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, nil)

	if err := svc.Review(context.Background(), adminID, []int64{1, 2, 3}, mcpsource.ReviewApproved, ""); err != nil {
		t.Fatalf("批量审核失败: %v", err)
	}
	if len(repo.reviews) != 3 {
		t.Fatalf("三条都该被写入结论，得到 %d 条", len(repo.reviews))
	}
	for _, r := range repo.reviews {
		if r.status != mcpsource.ReviewApproved {
			t.Errorf("条目 %d 的结论不对: %s", r.id, r.status)
		}
	}
	if err := svc.Review(context.Background(), adminID, nil, mcpsource.ReviewApproved, ""); err == nil {
		t.Error("空批次应该被拒绝")
	}
}

// ── 协议与凭据 ───────────────────────────────────────────────────────

// 不填前缀就走协议的默认值：管理员不用去记官方是 /v0 还是 /v0.1，填错了
// 才是这套东西最常见的故障。
func TestCreateFillsProtocolDefaults(t *testing.T) {
	repo := newFakeRepo()
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, nil)

	src, err := svc.Create(context.Background(), adminID, mcpsource.CreateParams{
		Name: "官方", BaseURL: "https://registry.modelcontextprotocol.io",
	})
	if err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	// 协议留空 = 官方那套规范，绝大多数源都是它。
	if src.Protocol != mcpsource.ProtocolMCPRegistry {
		t.Errorf("协议默认值不对: %q", src.Protocol)
	}
	if src.APIPrefix != "/v0" {
		t.Errorf("前缀应取协议默认值 /v0，得到 %q", src.APIPrefix)
	}
}

// 前缀写法五花八门（v0 / /v0 / /v0/），统一收敛，否则拼出来的地址会多一
// 道或少一道斜杠。
func TestCreateNormalizesAPIPrefix(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"v0", "/v0"}, {"/v0", "/v0"}, {"/v0/", "/v0"}, {"  /v0.1  ", "/v0.1"},
	} {
		repo := newFakeRepo()
		svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, nil)
		src, err := svc.Create(context.Background(), adminID, mcpsource.CreateParams{
			Name: "子注册中心", BaseURL: "https://sub.example.com", APIPrefix: tc.in,
		})
		if err != nil {
			t.Fatalf("登记 %q 失败: %v", tc.in, err)
		}
		if src.APIPrefix != tc.want {
			t.Errorf("前缀 %q 应收敛成 %q，得到 %q", tc.in, tc.want, src.APIPrefix)
		}
	}
}

// 不认识的协议在登记时就挡下来，不要等到同步那一刻才发现没有 fetcher。
func TestCreateRejectsUnknownProtocol(t *testing.T) {
	svc := mcpsource.NewService(newFakeRepo(), fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, nil)
	_, err := svc.Create(context.Background(), adminID, mcpsource.CreateParams{
		Name: "野的", BaseURL: "https://x.example.com", Protocol: "nope",
	})
	derr, ok := domain.AsError(err)
	if !ok || derr.Code != mcpsource.CodeMCPProtocolUnknown {
		t.Fatalf("未知协议应被拒绝，得到 %v", err)
	}
}

// 要密钥的协议缺密钥直接打回：建出来也只是一个一同步就 401 的空源，让管
// 理员当场发现比过两天看 last_sync_error 强。
func TestCreateRequiresAPIKeyWhenProtocolDemandsIt(t *testing.T) {
	svc := mcpsource.NewService(newFakeRepo(), fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, nil)
	_, err := svc.Create(context.Background(), adminID, mcpsource.CreateParams{
		Name: "Smithery", BaseURL: "https://registry.smithery.ai", Protocol: mcpsource.ProtocolSmithery,
	})
	derr, ok := domain.AsError(err)
	if !ok || derr.Code != mcpsource.CodeMCPAPIKeyRequired {
		t.Fatalf("缺密钥应被拒绝，得到 %v", err)
	}
}

// 密钥加密落库，且领域对象上只留一个"配没配"的布尔值——明文不进 Source，
// 就没有哪条路径能把它顺手序列化到响应里。
func TestCreateEncryptsAPIKeyAndNeverExposesIt(t *testing.T) {
	repo := newFakeRepo()
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, nil)

	src, err := svc.Create(context.Background(), adminID, mcpsource.CreateParams{
		Name: "Smithery", BaseURL: "https://registry.smithery.ai",
		Protocol: mcpsource.ProtocolSmithery, APIKey: "sk-live-secret",
	})
	if err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	if !src.HasAPIKey {
		t.Error("配了密钥就该报告 has_api_key")
	}
	if len(repo.created) != 1 {
		t.Fatalf("应有一次落库，得到 %d 次", len(repo.created))
	}
	stored := repo.created[0]
	if stored.EncryptedAPIKey != "enc:sk-live-secret" {
		t.Errorf("落库的应是密文，得到 %q", stored.EncryptedAPIKey)
	}
	// 明文不能跟着参数结构体流进仓储。
	if stored.APIKey != "" {
		t.Errorf("明文密钥不该传到仓储层，得到 %q", stored.APIKey)
	}
}

// 同步时把源的前缀和解密后的密钥一并交给 fetcher。这两样传丢了不会报错，
// 只会静默地拉回一个空列表，然后把好好的缓存清空——所以要盯住。
func TestSyncPassesPrefixAndDecryptedKeyToFetcher(t *testing.T) {
	repo := newFakeRepo()
	reg := newFakeRegistry()
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, reg, fakeCipher{}, nil)

	src, err := svc.Create(context.Background(), adminID, mcpsource.CreateParams{
		Name: "Smithery", BaseURL: "https://registry.smithery.ai",
		Protocol: mcpsource.ProtocolSmithery, APIPrefix: "/v1", APIKey: "sk-live-secret",
	})
	if err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	if _, err := svc.Sync(context.Background(), adminID, src.ID); err != nil {
		t.Fatalf("同步失败: %v", err)
	}
	got := reg.fetcher.gotTarget
	if got == nil {
		t.Fatal("fetcher 没被调用")
	}
	if got.APIPrefix != "/v1" {
		t.Errorf("前缀没传下去: %q", got.APIPrefix)
	}
	if got.APIKey != "sk-live-secret" {
		t.Errorf("fetcher 拿到的应是解密后的明文，得到 %q", got.APIKey)
	}
}

// 换过加密密钥之后旧密文解不开。这时要说清楚是密钥的问题——不然管理员会
// 一直以为是对方接口挂了，去查一个根本没坏的东西。
func TestSyncReportsUndecryptableKeyAsSuch(t *testing.T) {
	repo := newFakeRepo()
	reg := newFakeRegistry()
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, reg, fakeCipher{}, nil)
	src, _ := svc.Create(context.Background(), adminID, mcpsource.CreateParams{
		Name: "Smithery", BaseURL: "https://registry.smithery.ai",
		Protocol: mcpsource.ProtocolSmithery, APIKey: "sk-live-secret",
	})

	// 换一把解不开旧密文的密钥。
	broken := mcpsource.NewService(repo, fakeAdmins{admin: true}, reg, fakeCipher{failDecrypt: true}, nil)
	_, err := broken.Sync(context.Background(), adminID, src.ID)
	if err == nil {
		t.Fatal("解不开密钥时同步应该失败")
	}
	if !strings.Contains(err.Error(), "解不开") {
		t.Errorf("错误信息应指向密钥而不是上游: %v", err)
	}
	if !strings.Contains(repo.syncErr, "解不开") {
		t.Errorf("原因要落进 last_sync_error，得到 %q", repo.syncErr)
	}
}

// 库里存着一个本部署不认识的协议（降级部署后的老数据）时，同步失败要留
// 痕，而不是静默什么都不做。
func TestSyncRecordsUnknownProtocol(t *testing.T) {
	repo := newFakeRepo()
	reg := newFakeRegistry()
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, reg, fakeCipher{}, nil)
	src, _ := svc.Create(context.Background(), adminID, mcpsource.CreateParams{
		Name: "官方", BaseURL: "https://registry.modelcontextprotocol.io",
	})

	reg.unknown = true
	if _, err := svc.Sync(context.Background(), adminID, src.ID); err == nil {
		t.Fatal("协议不认识时同步应该失败")
	}
	if repo.syncErr == "" {
		t.Error("失败原因必须落在 last_sync_error 上")
	}
}

// 协议清单不需要管理员权限：它是一张静态能力清单，没有任何部署信息。
func TestProtocolsAreListable(t *testing.T) {
	svc := mcpsource.NewService(newFakeRepo(), fakeAdmins{admin: false}, newFakeRegistry(), fakeCipher{}, nil)
	specs := svc.Protocols()
	if len(specs) == 0 {
		t.Fatal("协议清单不该为空")
	}
	for _, spec := range specs {
		if spec.Label == "" {
			t.Errorf("协议 %s 缺少显示名", spec.ID)
		}
	}
}

// 换密钥是独立动作：重建源会把这个源下面所有条目的审核结论一起丢掉。
func TestSetAPIKeyReplacesCiphertextOnly(t *testing.T) {
	repo := newFakeRepo()
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, newFakeRegistry(), fakeCipher{}, nil)
	src, _ := svc.Create(context.Background(), adminID, mcpsource.CreateParams{
		Name: "Smithery", BaseURL: "https://registry.smithery.ai",
		Protocol: mcpsource.ProtocolSmithery, APIKey: "old",
	})

	if err := svc.SetAPIKey(context.Background(), adminID, src.ID, "new"); err != nil {
		t.Fatalf("换密钥失败: %v", err)
	}
	stored, _ := repo.EncryptedAPIKey(context.Background(), src.ID)
	if stored != "enc:new" {
		t.Errorf("应存新密文，得到 %q", stored)
	}
	if err := svc.SetAPIKey(context.Background(), adminID, src.ID, "  "); err == nil {
		t.Error("空密钥应该被拒绝")
	}
	// 替身的 IsAdmin 不看 userID，所以"非管理员"要换一个 admins 替身来断言。
	asUser := mcpsource.NewService(repo, fakeAdmins{admin: false}, newFakeRegistry(), fakeCipher{}, nil)
	if err := asUser.SetAPIKey(context.Background(), 7, src.ID, "x"); !isForbidden(err) {
		t.Error("换密钥应限管理员")
	}
}
