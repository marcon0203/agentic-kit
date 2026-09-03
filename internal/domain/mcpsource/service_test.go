package mcpsource_test

import (
	"context"
	"errors"
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
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{servers: map[int64]mcpsource.MarketServer{}}
}

func (f *fakeRepo) Create(_ context.Context, name, baseURL string) (mcpsource.Source, error) {
	src := mcpsource.Source{ID: int64(len(f.sources) + 1), Name: name, BaseURL: baseURL, Status: 1}
	f.sources = append(f.sources, src)
	return src, nil
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
}

func (f fakeFetcher) FetchList(context.Context, string) ([]mcpsource.FetchedServer, error) {
	return f.servers, f.err
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
	svc := mcpsource.NewService(newFakeRepo(), fakeAdmins{admin: false}, fakeFetcher{}, nil)
	ctx := context.Background()

	if _, err := svc.List(ctx, 7); !isForbidden(err) {
		t.Errorf("List 应该 403，得到 %v", err)
	}
	if _, err := svc.Create(ctx, 7, "官方", "https://registry.modelcontextprotocol.io"); !isForbidden(err) {
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
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, fakeFetcher{}, nil)

	src, err := svc.Create(context.Background(), adminID, "官方注册中心", "https://registry.modelcontextprotocol.io/")
	if err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	if src.BaseURL != "https://registry.modelcontextprotocol.io" {
		t.Errorf("尾斜杠应该被去掉，得到 %q", src.BaseURL)
	}

	if _, err := svc.Create(context.Background(), adminID, "带路径", "https://example.com/v0/servers"); err == nil {
		t.Error("带路径的地址应该被拒绝")
	}
	if _, err := svc.Create(context.Background(), adminID, "重复", "https://registry.modelcontextprotocol.io"); err == nil {
		t.Error("同一个地址不该登记两次")
	}
}

// 同步失败要留痕：last_sync_error 落库、源本身照常返回，管理员在设置页上
// 能直接看到原因，而不是"点了没反应"。
func TestSyncRecordsUpstreamFailure(t *testing.T) {
	repo := newFakeRepo()
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, fakeFetcher{err: errors.New("源返回 502")}, nil)
	src, _ := svc.Create(context.Background(), adminID, "官方", "https://registry.modelcontextprotocol.io")

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
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, fakeFetcher{}, nil)
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

	user := mcpsource.NewService(repo, fakeAdmins{admin: false}, fakeFetcher{}, nil)
	if _, err := user.GetMarketServer(context.Background(), 7, 9); err == nil {
		t.Error("待审核条目不该对普通用户可见")
	}

	admin := mcpsource.NewService(repo, fakeAdmins{admin: true}, fakeFetcher{}, nil)
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
	svc := mcpsource.NewService(repo, fakeAdmins{admin: false}, fakeFetcher{}, installer)

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
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, fakeFetcher{}, &fakeInstaller{})

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
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, fakeFetcher{}, installer)

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
	svc := mcpsource.NewService(repo, fakeAdmins{admin: true}, fakeFetcher{}, nil)

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
