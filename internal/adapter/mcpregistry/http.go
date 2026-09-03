package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpGetter 是两个 fetcher 共用的那点 HTTP 细节：超时、Accept 头、可选的
// bearer 凭据、响应体上限、状态码判定。
//
// 抽出来不只是省几行——错误信息统一在这里拼，管理员看到的 last_sync_error
// 才不会因为源的协议不同而措辞各异。
type httpGetter struct{ client *http.Client }

func newHTTPGetter() *httpGetter {
	return &httpGetter{client: &http.Client{Timeout: 20 * time.Second}}
}

// bodyLimit 挡住一个异常源用超大响应把内存吃光。32MB 远大于任何一家注册中
// 心的单页响应（一页 100 条、每条几 KB）。
const bodyLimit = 32 << 20

// errSnippet 是出错时从响应体里带出来的长度上限。带一点上游的原话，
// "401" 和 "401：key 过期了" 对排查的价值差很远；但不能整个响应都塞进
// last_sync_error，那一列是要显示在页面上的。
const errSnippet = 300

func (g *httpGetter) getJSON(ctx context.Context, u, apiKey string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("连不上源：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if err != nil {
		return fmt.Errorf("读取源的响应失败：%w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 401/403 单独说：这两个几乎总是密钥的问题，而管理员的第一反应通常
		// 是"对方是不是挂了"，白排查半天。
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("源返回 %d：API Key 不对或没有权限 %s", resp.StatusCode, snippet(body))
		case http.StatusNotFound:
			// 404 在这套东西里九成是接口前缀填错了（/v0 vs /v0.1）。
			return fmt.Errorf("源返回 404：接口地址或版本前缀不对，对照对方文档核一下 %s", snippet(body))
		default:
			return fmt.Errorf("源返回 %d %s", resp.StatusCode, snippet(body))
		}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("源的响应不是合法 JSON：%w", err)
	}
	return nil
}

func snippet(body []byte) string {
	s := string(body)
	if len(s) > errSnippet {
		s = s[:errSnippet] + "…"
	}
	if s == "" {
		return ""
	}
	return "(" + s + ")"
}
