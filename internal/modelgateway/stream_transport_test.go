package modelgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 流式请求必须显式声明 Accept-Encoding: identity。
//
// 不设的话 Go 的 transport 会自己加上 gzip 并透明解压，而 gzip 是按块工作
// 的：上游不逐帧 flush deflate 流时，解压侧要攒够一整块才能吐数据，SSE 于
// 是变成"静默几秒、然后一批帧一起到达"。线上就是这么表现的——数据库里的
// node.thinking 反复出现"空几秒、然后一秒内 160 条"。
func TestDescriptorClient_CompleteStream_DisablesCompression(t *testing.T) {
	var gotEncoding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + `{"choices":[{"delta":{"content":"hi"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	sc := deepSeekClient(t, srv).(StreamingClient)
	if _, err := sc.CompleteStream(context.Background(), "sk-test", "", "deepseek-chat",
		CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}},
		func(StreamDelta) {}); err != nil {
		t.Fatalf("流式调用失败: %v", err)
	}
	if gotEncoding != "identity" {
		t.Fatalf("流式请求的 Accept-Encoding 应当是 identity，实际是 %q——"+
			"空字符串意味着 Go 自动加了 gzip，SSE 会被按块缓冲", gotEncoding)
	}
}

// http.Client.Timeout 覆盖整个请求生命周期，**包括读 response body**。流式
// 响应的 body 会一直开着，把整体时限放在那里等于给每次运行判死刑：到点连
// 接被拦腰砍断，运行以失败告终，而模型其实答得好好的。
//
// 线上确实这么炸过一次：bundle.started 到 bundle.failed 正好 60.013 秒，
// 分毫不差就是当时 Client.Timeout 的值。非流式的时限改由 descriptor_client
// 在 do() 里用 context 施加，这里守住它不要再被挪回来。
func TestNewHTTPClient_HasNoOverallTimeout(t *testing.T) {
	c := newHTTPClient()
	if c.Timeout != 0 {
		t.Fatalf("模型调用的 http.Client 不能设 Timeout（当前 %v）："+
			"它会把流式响应的 body 读取一起算进去，长运行会被拦腰砍断。"+
			"整体时限请按请求类型用 context 施加", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("期望自定义 *http.Transport，实际 %T", c.Transport)
	}
	if tr.ResponseHeaderTimeout == 0 {
		t.Fatal("去掉 Client.Timeout 之后必须留 ResponseHeaderTimeout，" +
			"否则上游不回应时会一直挂着")
	}
}
