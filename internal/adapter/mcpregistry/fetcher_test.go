package mcpregistry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/adapter/mcpregistry"
)

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

	got, err := mcpregistry.NewFetcher().FetchList(context.Background(), srv.URL)
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

	got, err := mcpregistry.NewFetcher().FetchList(context.Background(), srv.URL)
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

	got, _ := mcpregistry.NewFetcher().FetchList(context.Background(), srv.URL)
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

	got, _ := mcpregistry.NewFetcher().FetchList(context.Background(), srv.URL)
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

	got, err := mcpregistry.NewFetcher().FetchList(context.Background(), srv.URL)
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

	if _, err := mcpregistry.NewFetcher().FetchList(context.Background(), srv.URL); err == nil {
		t.Fatal("上游 502 必须变成错误")
	}
}
