package mcpregistry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain/mcpsource"
)

// 这一组是包内测试（不是 _test 包）：Smithery 的端点地址要按托管约定拼出
// 来，得把 deployHost 指到 httptest 上才能断言拼的结果。

func smitheryFetcher(deployHost string) *SmitheryFetcher {
	f := NewSmitheryFetcher()
	f.deployHost = deployHost
	return f
}

// Smithery 的列表只说"这个服务器能不能远程连"，不给地址。地址按托管约定
// 拼；remote=false 的条目不拼——它们是"下载下来自己跑"的包，平台连不上，
// 编一个地址出来只会让用户接进去之后一探测就红。
func TestSmithery_DerivesEndpointOnlyForRemoteServers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/servers" {
			t.Errorf("路径不对: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"servers":[
			{"qualifiedName":"owner/remote-one","displayName":"Remote One","description":"能远程连",
			 "homepage":"https://github.com/owner/remote-one","remote":true,"verified":true,
			 "createdAt":"2025-06-01T00:00:00Z"},
			{"qualifiedName":"owner/local-one","displayName":"Local One","remote":false}
		],"pagination":{"currentPage":1,"pageSize":100,"totalPages":1,"totalCount":2}}`))
	}))
	defer srv.Close()

	got, err := smitheryFetcher("https://server.smithery.ai").
		FetchList(context.Background(), mcpsource.FetchTarget{BaseURL: srv.URL, APIKey: "sk-1"})
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("两条都该同步进来（本地包也要让管理员看得到），得到 %d 条", len(got))
	}

	remote := got[0]
	if remote.Slug != "owner/remote-one" || remote.Name != "Remote One" {
		t.Errorf("条目解析不对: %+v", remote)
	}
	if remote.RemoteURL != "https://server.smithery.ai/owner/remote-one/mcp" {
		t.Errorf("端点拼错了: %q", remote.RemoteURL)
	}
	if remote.RemoteType != "streamable-http" {
		t.Errorf("Smithery 托管的端点是 streamable-http，得到 %q", remote.RemoteType)
	}
	if remote.RepositoryURL != "https://github.com/owner/remote-one" {
		t.Errorf("homepage 应落到 repository_url: %q", remote.RepositoryURL)
	}
	if remote.UpdatedAt.IsZero() {
		t.Error("createdAt 应该被解析出来")
	}
	// verified 是 Smithery 自己的审核标记，对我们的管理员是决定放不放行的
	// 一手信息，要能筛。
	if !contains(remote.Topics, "verified") {
		t.Errorf("verified 应进标签: %v", remote.Topics)
	}

	local := got[1]
	if local.RemoteURL != "" {
		t.Errorf("本地包不该编出一个远端地址: %q", local.RemoteURL)
	}
	if !contains(local.Topics, "local-package") {
		t.Errorf("本地包应有可筛的标签: %v", local.Topics)
	}
}

// 没密钥就别发请求了：Smithery 一定回 401，不如在这里直说该去哪补。
func TestSmithery_RequiresAPIKeyBeforeCallingOut(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	_, err := smitheryFetcher("https://server.smithery.ai").
		FetchList(context.Background(), mcpsource.FetchTarget{BaseURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Fatalf("缺密钥应直说，得到 %v", err)
	}
	if called {
		t.Error("缺密钥时不该真的发请求出去")
	}
}

// 密钥要以 bearer 发出去，否则每次同步都是 401。
func TestSmithery_SendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"servers":[],"pagination":{"totalPages":1}}`))
	}))
	defer srv.Close()

	if _, err := smitheryFetcher("https://server.smithery.ai").
		FetchList(context.Background(), mcpsource.FetchTarget{BaseURL: srv.URL, APIKey: "sk-live"}); err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if gotAuth != "Bearer sk-live" {
		t.Errorf("鉴权头不对: %q", gotAuth)
	}
}

// 按 totalPages 翻页，并且要真的停下来——对方返回一个离谱的总页数时靠
// maxPages 兜底，但正常情况下不该多打一页。
func TestSmithery_PaginatesAndStops(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("page")
		pages = append(pages, p)
		switch p {
		case "1":
			_, _ = w.Write([]byte(`{"servers":[{"qualifiedName":"a/one","remote":true}],
				"pagination":{"currentPage":1,"totalPages":2,"totalCount":2}}`))
		case "2":
			_, _ = w.Write([]byte(`{"servers":[{"qualifiedName":"a/two","remote":true}],
				"pagination":{"currentPage":2,"totalPages":2,"totalCount":2}}`))
		default:
			t.Errorf("多打了一页: %s", p)
			_, _ = w.Write([]byte(`{"servers":[],"pagination":{"totalPages":2}}`))
		}
	}))
	defer srv.Close()

	got, err := smitheryFetcher("https://server.smithery.ai").
		FetchList(context.Background(), mcpsource.FetchTarget{BaseURL: srv.URL, APIKey: "sk-1"})
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if len(got) != 2 || len(pages) != 2 {
		t.Fatalf("期望翻两页拿到 2 条，得到 %d 条 / %v", len(got), pages)
	}
}

// 同一个 qualifiedName 重复出现时只留一条：缓存表按 (source, slug) 唯一，
// 不去重的话同步会在同一批里反复 upsert 同一行。
func TestSmithery_Dedupes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[
			{"qualifiedName":"a/x","remote":true},
			{"qualifiedName":"a/x","remote":true},
			{"qualifiedName":"","remote":true}
		],"pagination":{"totalPages":1}}`))
	}))
	defer srv.Close()

	got, _ := smitheryFetcher("https://server.smithery.ai").
		FetchList(context.Background(), mcpsource.FetchTarget{BaseURL: srv.URL, APIKey: "sk-1"})
	if len(got) != 1 {
		t.Fatalf("期望去重后 1 条，得到 %d 条: %+v", len(got), got)
	}
}

// 上游报错不能静默变成空列表——那会被 ReplaceServers 当成"全下架了"，把好
// 好的缓存清空。
func TestSmithery_SurfacesUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	_, err := smitheryFetcher("https://server.smithery.ai").
		FetchList(context.Background(), mcpsource.FetchTarget{BaseURL: srv.URL, APIKey: "bad"})
	if err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("上游的错误信息必须带出来，得到 %v", err)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
