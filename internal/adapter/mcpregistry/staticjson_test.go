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

// 清单两种写法都得认：手写的清单更可能是一个裸数组，从注册中心导出来的则
// 带着 servers 包装。只认一种的话，另一种会报"不是合法 JSON"，而管理员看
// 着自己那份明明合法的文件无从下手。
func TestStaticJSON_AcceptsBareArrayAndWrappedObject(t *testing.T) {
	bodies := map[string]string{
		"裸数组": `[{"name":"com.tencent/cos","description":"腾讯云 COS",
			"remotes":[{"type":"streamable-http","url":"https://mcp.example.cn/cos"}]}]`,
		"带包装": `{"servers":[{"name":"com.tencent/cos","description":"腾讯云 COS",
			"remotes":[{"type":"streamable-http","url":"https://mcp.example.cn/cos"}]}]}`,
	}
	for label, body := range bodies {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		got, err := mcpregistry.NewStaticJSONFetcher().FetchList(context.Background(),
			mcpsource.FetchTarget{BaseURL: srv.URL, APIPrefix: "/mcp-servers.json"})
		srv.Close()
		if err != nil {
			t.Fatalf("%s 解析失败: %v", label, err)
		}
		if len(got) != 1 {
			t.Fatalf("%s 期望 1 条，得到 %d 条", label, len(got))
		}
		if got[0].Slug != "com.tencent/cos" || got[0].RemoteURL != "https://mcp.example.cn/cos" {
			t.Errorf("%s 解析不对: %+v", label, got[0])
		}
	}
}

// 地址原样拼接、不追加 /servers：这个协议指向的是一个具体文件，不是一套
// 接口。追加了的话管理员填的 .../mcp-servers.json 会变成
// .../mcp-servers.json/servers，然后 404。
func TestStaticJSON_RequestsTheGivenPathVerbatim(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`[{"name":"a/x"}]`))
	}))
	defer srv.Close()

	if _, err := mcpregistry.NewStaticJSONFetcher().FetchList(context.Background(),
		mcpsource.FetchTarget{BaseURL: srv.URL, APIPrefix: "/team/mcp-servers.json"}); err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if gotPath != "/team/mcp-servers.json" {
		t.Errorf("地址被改写了: %s", gotPath)
	}
}

// 空清单要报错，不能静默返回零条——那会被 ReplaceServers 当成"上游全下架
// 了"，把这个源已有的缓存和审核结论一起清空。地址填错指到别的 JSON 上时，
// 表现恰好也是"解析出零条"，同样得拦住。
func TestStaticJSON_EmptyListIsAnErrorNotAWipe(t *testing.T) {
	for _, body := range []string{`[]`, `{"servers":[]}`, `{"unrelated":"json"}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		_, err := mcpregistry.NewStaticJSONFetcher().FetchList(context.Background(),
			mcpsource.FetchTarget{BaseURL: srv.URL, APIPrefix: "/x.json"})
		srv.Close()
		if err == nil {
			t.Errorf("清单 %s 应该报错而不是清空缓存", body)
		}
	}
}

// 清单和注册中心必须转得一模一样：同一个服务从两条路进来，落库的字段不能
// 有差别，否则从"清单"切到"注册中心"会变成两批不同的条目。
func TestStaticJSON_MapsIdenticallyToTheRegistryProtocol(t *testing.T) {
	entry := `{"name":"io.github.a/x","description":"同一个服务","version":"1.2.0",
		"repository":{"url":"https://github.com/a/x","source":"github"},
		"icons":[{"src":"https://cdn.example.com/x.png","mimeType":"image/png"}],
		"remotes":[{"type":"streamable-http","url":"https://a.example.com/mcp"}],
		"_meta":{"io.modelcontextprotocol.registry/official":{"updatedAt":"2025-06-01T00:00:00Z"}}}`

	regSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[` + entry + `],"metadata":{}}`))
	}))
	defer regSrv.Close()
	jsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[` + entry + `]`))
	}))
	defer jsonSrv.Close()

	fromRegistry, err := mcpregistry.NewRegistryFetcher().FetchList(context.Background(),
		mcpsource.FetchTarget{BaseURL: regSrv.URL, APIPrefix: "/v0"})
	if err != nil {
		t.Fatalf("注册中心拉取失败: %v", err)
	}
	fromJSON, err := mcpregistry.NewStaticJSONFetcher().FetchList(context.Background(),
		mcpsource.FetchTarget{BaseURL: jsonSrv.URL, APIPrefix: "/x.json"})
	if err != nil {
		t.Fatalf("清单拉取失败: %v", err)
	}
	if len(fromRegistry) != 1 || len(fromJSON) != 1 {
		t.Fatalf("两边各应有 1 条，得到 %d / %d", len(fromRegistry), len(fromJSON))
	}
	a, b := fromRegistry[0], fromJSON[0]
	if a.Slug != b.Slug || a.Name != b.Name || a.Summary != b.Summary || a.Version != b.Version ||
		a.RemoteURL != b.RemoteURL || a.RemoteType != b.RemoteType ||
		a.RepositoryURL != b.RepositoryURL || a.IconURL != b.IconURL || !a.UpdatedAt.Equal(b.UpdatedAt) {
		t.Errorf("两条路解析结果不一致:\n注册中心 %+v\n清单     %+v", a, b)
	}
}

// 清单托在内网/对象存储上时可能要一个令牌。带上就是了。
func TestStaticJSON_SendsBearerWhenConfigured(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[{"name":"a/x"}]`))
	}))
	defer srv.Close()

	if _, err := mcpregistry.NewStaticJSONFetcher().FetchList(context.Background(),
		mcpsource.FetchTarget{BaseURL: srv.URL, APIPrefix: "/x.json", APIKey: "tok"}); err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("令牌没带上: %q", gotAuth)
	}
}

// 不是 JSON 的东西（比如登录页的 HTML）要给出能看懂的理由。
func TestStaticJSON_ReportsNonJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>请先登录</body></html>`))
	}))
	defer srv.Close()

	_, err := mcpregistry.NewStaticJSONFetcher().FetchList(context.Background(),
		mcpsource.FetchTarget{BaseURL: srv.URL, APIPrefix: "/x.json"})
	if err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("应指出响应不是合法 JSON，得到 %v", err)
	}
}
