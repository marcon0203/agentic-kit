package mcpregistry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/adapter/mcpregistry"
	"github.com/marcon0203/agentic-kit/internal/domain/mcpsource"
)

// target 是官方那套的默认形状：/v0 前缀、不带密钥。
func target(baseURL string) mcpsource.FetchTarget {
	return mcpsource.FetchTarget{BaseURL: baseURL, APIPrefix: "/v0"}
}

// 注册中心的响应换过一次形状：早期条目平铺，后来包进 {"server": …} 并把
// 发布元数据挪进 _meta。自建注册中心的版本参差不齐，两种都得认——认错一
// 种的后果是整个源同步下来空空如也，还不报错。
func TestFetchList_AcceptsBothEntryShapes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/servers" {
			t.Errorf("路径不对: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"servers":[
			{"name":"io.github.a/flat","description":"平铺形状","version":"1.0.0",
			 "remotes":[{"type":"streamable-http","url":"https://a.example.com/mcp"}],
			 "_meta":{"io.modelcontextprotocol.registry/official":{"updatedAt":"2025-06-01T00:00:00Z"}}},
			{"server":{"name":"io.github.b/nested","description":"嵌套形状","version":"2.0.0",
			  "repository":{"url":"https://github.com/b/nested","source":"github"},
			  "remotes":[{"type":"sse","url":"https://b.example.com/sse"}]},
			 "_meta":{"io.modelcontextprotocol.registry/official":{"publishedAt":"2025-05-01T00:00:00Z"}}}
		],"metadata":{}}`))
	}))
	defer srv.Close()

	got, err := mcpregistry.NewRegistryFetcher().FetchList(context.Background(), target(srv.URL))
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("两种形状都该认，得到 %d 条: %+v", len(got), got)
	}
	if got[0].Slug != "io.github.a/flat" || got[0].RemoteURL != "https://a.example.com/mcp" {
		t.Errorf("平铺条目解析不对: %+v", got[0])
	}
	if got[0].UpdatedAt.IsZero() {
		t.Error("updatedAt 应该被解析出来")
	}
	if got[1].Slug != "io.github.b/nested" || got[1].RemoteType != "sse" {
		t.Errorf("嵌套条目解析不对: %+v", got[1])
	}
	// 外层 _meta 要并回来：新形状把发布元数据放在条目上而不是 server 里。
	if got[1].UpdatedAt.IsZero() {
		t.Error("没有 updatedAt 时应退回 publishedAt")
	}
	if got[1].RepositoryURL != "https://github.com/b/nested" {
		t.Errorf("仓库地址没带出来: %+v", got[1])
	}
}

// 同一个 Server 在注册中心按版本存多行，缓存表按 (source, slug) 唯一。不
// 去重的话同步会在同一批里 upsert 同一行几十次，落下的还是随机某个版本。
// 下架条目同样不该进市场。
func TestFetchList_DedupesAndSkipsInactive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[
			{"name":"io.github.a/x","version":"2.0.0","status":"active"},
			{"name":"io.github.a/x","version":"1.0.0","status":"active"},
			{"name":"io.github.a/gone","version":"1.0.0","status":"deleted"}
		],"metadata":{}}`))
	}))
	defer srv.Close()

	got, err := mcpregistry.NewRegistryFetcher().FetchList(context.Background(), target(srv.URL))
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("期望去重后 1 条，得到 %d 条: %+v", len(got), got)
	}
	if got[0].Version != "2.0.0" {
		t.Errorf("列表按最新在前，应留下 2.0.0，得到 %q", got[0].Version)
	}
}

// 同一个 Server 可能同时给 streamable-http 和 sse 两个入口。sse 是上一代
// 传输，新服务端只保证前者可用。
func TestFetchList_PrefersStreamableHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[{"name":"io.github.a/x","remotes":[
			{"type":"sse","url":"https://a.example.com/sse"},
			{"type":"streamable-http","url":"https://a.example.com/mcp"}]}],"metadata":{}}`))
	}))
	defer srv.Close()

	got, _ := mcpregistry.NewRegistryFetcher().FetchList(context.Background(), target(srv.URL))
	if len(got) != 1 || got[0].RemoteURL != "https://a.example.com/mcp" {
		t.Fatalf("应优先选 streamable-http，得到 %+v", got)
	}
}

// 只给本地运行包的条目照样同步进来（管理员要看得到"上游有什么"），但没有
// 远端地址——安装那一侧据此拒绝，页面据此把按钮置灰。
func TestFetchList_KeepsLocalOnlyServersWithoutRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[{"name":"io.github.a/local",
			"packages":[{"registryType":"npm","identifier":"a-mcp"}]}],"metadata":{}}`))
	}))
	defer srv.Close()

	got, _ := mcpregistry.NewRegistryFetcher().FetchList(context.Background(), target(srv.URL))
	if len(got) != 1 {
		t.Fatalf("期望 1 条，得到 %d 条", len(got))
	}
	if got[0].RemoteURL != "" {
		t.Errorf("没有 remotes 的条目不该编出一个远端地址: %+v", got[0])
	}
	if len(got[0].Topics) == 0 || got[0].Topics[0] != "local-package" {
		t.Errorf("只能本地跑的条目应该有可筛的标签: %+v", got[0].Topics)
	}
}

// 游标翻页：两种拼写（nextCursor / next_cursor）都认，没有游标就停。停不
// 下来的话一个异常源能把同步挂死。
func TestFetchList_FollowsCursor(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Query().Get("cursor") {
		case "":
			_, _ = w.Write([]byte(`{"servers":[{"name":"io.github.a/one"}],"metadata":{"next_cursor":"p2"}}`))
		case "p2":
			_, _ = w.Write([]byte(`{"servers":[{"name":"io.github.a/two"}],"metadata":{}}`))
		default:
			t.Errorf("意料之外的游标: %s", r.URL.Query().Get("cursor"))
		}
	}))
	defer srv.Close()

	got, err := mcpregistry.NewRegistryFetcher().FetchList(context.Background(), target(srv.URL))
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if len(got) != 2 || calls != 2 {
		t.Fatalf("期望翻两页拿到 2 条，得到 %d 条 / %d 次请求", len(got), calls)
	}
}

// 上游报错时不能静默返回空列表——那会被 ReplaceServers 当成"上游全下架
// 了"，把好好的缓存清空。
func TestFetchList_SurfacesUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	if _, err := mcpregistry.NewRegistryFetcher().FetchList(context.Background(), target(srv.URL)); err == nil {
		t.Fatal("上游 502 必须变成错误")
	}
}

// 前缀可配是"接第三方子注册中心"的全部机关：各家实现的是同一套规范，只是
// 停在不同版本上。这条盯住 prefix 真的进了请求路径——写死 /v0 的话，接
// PulseMCP 这类停在 /v0.1 的源会一路 404。
func TestFetchList_UsesConfiguredAPIPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"servers":[{"name":"io.github.a/x"}],"metadata":{}}`))
	}))
	defer srv.Close()

	got, err := mcpregistry.NewRegistryFetcher().FetchList(context.Background(),
		mcpsource.FetchTarget{BaseURL: srv.URL, APIPrefix: "/v0.1"})
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if gotPath != "/v0.1/servers" {
		t.Errorf("前缀没进请求路径: %s", gotPath)
	}
	if len(got) != 1 {
		t.Errorf("期望 1 条，得到 %d 条", len(got))
	}
}

// 有的子注册中心要密钥才给读，官方不要。配了就带上，没配就不带——不能因
// 为空密钥而发一个 "Bearer " 的空头出去。
func TestFetchList_SendsBearerOnlyWhenKeyConfigured(t *testing.T) {
	for _, tc := range []struct{ key, wantAuth string }{
		{"", ""},
		{"sk-1", "Bearer sk-1"},
	} {
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"servers":[],"metadata":{}}`))
		}))
		_, err := mcpregistry.NewRegistryFetcher().FetchList(context.Background(),
			mcpsource.FetchTarget{BaseURL: srv.URL, APIPrefix: "/v0", APIKey: tc.key})
		srv.Close()
		if err != nil {
			t.Fatalf("拉取失败: %v", err)
		}
		if gotAuth != tc.wantAuth {
			t.Errorf("密钥 %q 时鉴权头应为 %q，得到 %q", tc.key, tc.wantAuth, gotAuth)
		}
	}
}

// 404 在这套东西里九成是前缀填错了，401/403 九成是密钥问题。错误信息得直
// 说，否则管理员会去排查一个根本没坏的上游。
func TestFetchList_ErrorMessagesPointAtTheLikelyCause(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusNotFound, "版本前缀"},
		{http.StatusUnauthorized, "API Key"},
		{http.StatusForbidden, "API Key"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
		}))
		_, err := mcpregistry.NewRegistryFetcher().FetchList(context.Background(), target(srv.URL))
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%d 的错误信息里应提到 %q，得到 %v", tc.status, tc.want, err)
		}
	}
}

// 上游的错误正文要带出来一截：只回一个状态码的话，"key 过期了"和"这个源
// 下线了"看起来一模一样。
func TestFetchList_IncludesUpstreamErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"database is down"}`))
	}))
	defer srv.Close()

	_, err := mcpregistry.NewRegistryFetcher().FetchList(context.Background(), target(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "database is down") {
		t.Fatalf("上游的错误正文必须带出来，得到 %v", err)
	}
}

// 图标的三种写法都要认。认漏一种的结果不是报错，是整片卡片没有图——这种
// 静默的降级最容易漏过去。
func TestFetchList_PicksIconFromAnyOfTheThreeShapes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[
			{"name":"a/icons","icons":[{"src":"https://cdn/a.png","mimeType":"image/png"}]},
			{"name":"a/iconurl","iconUrl":"https://cdn/b.png"},
			{"name":"a/logourl","logoUrl":"https://cdn/c.png"},
			{"name":"a/none"}
		],"metadata":{}}`))
	}))
	defer srv.Close()

	got, err := mcpregistry.NewRegistryFetcher().FetchList(context.Background(), target(srv.URL))
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	want := []string{"https://cdn/a.png", "https://cdn/b.png", "https://cdn/c.png", ""}
	if len(got) != len(want) {
		t.Fatalf("期望 %d 条，得到 %d 条", len(want), len(got))
	}
	for i, w := range want {
		if got[i].IconURL != w {
			t.Errorf("第 %d 条图标应为 %q，得到 %q", i, w, got[i].IconURL)
		}
	}
}
