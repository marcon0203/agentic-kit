package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/runstream"
)

// RunHandlers is the HTTP transport for the 编排运行时 context (spec-11).
// The launch chain and the gate rules live in internal/domain/run; what is
// left here is JSON, status codes and the one endpoint that streams.
type RunHandlers struct {
	svc *run.Service
	// bus 是运行事件的实时投递通道（internal/runstream）。可以为 nil：
	// 那时 Stream 退回纯轮询，行为和引入广播器之前一致——测试里的裸
	// handler 就走这条路。
	bus *runstream.Broker
	// now is overridable in tests.
	now func() time.Time
}

func NewRunHandlers(svc *run.Service, bus *runstream.Broker) *RunHandlers {
	return &RunHandlers{svc: svc, bus: bus, now: time.Now}
}

// ── DTOs ─────────────────────────────────────────────────────────────

type runSummaryDTO struct {
	RunID         string     `json:"run_id"`
	SessionID     string     `json:"session_id,omitempty"`
	BundleRef     string     `json:"bundle_ref"`
	BundleVersion string     `json:"bundle_version"`
	Status        string     `json:"status"`
	Error         *string    `json:"error"`
	CreatedAt     time.Time  `json:"created_at"`
	FinishedAt    *time.Time `json:"finished_at"`
}

type runUsageDTO struct {
	TotalTokens     int64   `json:"total_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	DurationSeconds int64   `json:"duration_seconds"`
}

type runDetailDTO struct {
	runSummaryDTO
	SharedState map[string]any `json:"shared_state"`
	IsOwner     bool           `json:"is_owner"`
	Usage       runUsageDTO    `json:"usage"`
}

func toRunSummaryDTO(r run.Run) runSummaryDTO {
	dto := runSummaryDTO{
		RunID: r.ID, SessionID: r.SessionID, BundleRef: r.BundleRef, BundleVersion: r.BundleVersion,
		Status: string(r.Status), CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
	}
	if r.Error != "" {
		errText := r.Error
		dto.Error = &errText
	}
	return dto
}

// ── Create ───────────────────────────────────────────────────────────

type createRunRequest struct {
	BundleRef     string         `json:"bundle_ref"`
	BundleVersion string         `json:"bundle_version"`
	Input         map[string]any `json:"input"`
	// SessionID 接上已有的一段对话，留空就开一段新的。响应里会回传这次用
	// 的 session_id，下一条消息带回来即可续上。
	SessionID string `json:"session_id"`
}

// Create handles POST /runs. The run starts asynchronously — openapi's
// "异步启动，立即返回 run_id" — so the response is the freshly created run,
// still in `running`.
func (h *RunHandlers) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req createRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	created, err := h.svc.Start(r.Context(), userID, run.StartCommand{
		BundleRef: req.BundleRef, BundleVersion: req.BundleVersion, Input: req.Input,
		SessionID: req.SessionID,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toRunSummaryDTO(created))
}

// CreateAgentTest handles POST /runs/agent-test — the 智能体工作台's
// right-hand test panel. It takes a full Agent definition rather than a ref
// so the配置 being tested can be one the user has not saved yet, and returns
// the same RunSummary POST /runs does, so the caller consumes the result
// through the existing /runs/{id}/stream rather than a second event
// transport.
func (h *RunHandlers) CreateAgentTest(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req createAgentTestRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	created, err := h.svc.StartAgentTest(r.Context(), userID, run.AgentTestCommand{
		Definition: req.Definition, Input: req.Input, SessionID: req.SessionID,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toRunSummaryDTO(created))
}

type createAgentTestRunRequest struct {
	Definition map[string]any `json:"definition"`
	Input      map[string]any `json:"input"`
	SessionID  string         `json:"session_id"`
}

// ── Session ──────────────────────────────────────────────────────────

// ListSession handles GET /sessions/{id}/runs — 一段对话里的全部运行，时
// 间正序。前端刷新页面后靠它把整段对话重建出来：一次运行只是一问一答，
// 一段对话是它们串起来的那条线。
func (h *RunHandlers) ListSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	rows, err := h.svc.ListSession(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]runSummaryDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toRunSummaryDTO(row))
	}
	writeJSON(w, r, http.StatusOK, items)
}

// ── Conversations ────────────────────────────────────────────────────

type conversationDTO struct {
	SessionID    string    `json:"session_id"`
	Title        string    `json:"title"`
	StartedAt    time.Time `json:"started_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	RunCount     int64     `json:"run_count"`
}

// ListConversations handles GET /bundles/{ref}/conversations — the独立
// 聊天页（/chat/bundle/:bundleId）左侧"最近对话"列表。走的是和 POST
// /runs 完全一样的 Bundle 解析（所有权/订阅/访客三选一），所以谁能对着
// 这个 ref 发起新运行，谁就能看到自己在它下面攒的历史对话；反过来也一
// 样，没资格运行的人这里直接拿到和 POST /runs 相同的错误。
func (h *RunHandlers) ListConversations(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	rows, err := h.svc.ListConversations(r.Context(), userID, chi.URLParam(r, "ref"))
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]conversationDTO, 0, len(rows))
	for _, c := range rows {
		items = append(items, conversationDTO{
			SessionID: c.SessionID, Title: c.Title,
			StartedAt: c.StartedAt, LastActiveAt: c.LastActiveAt, RunCount: c.RunCount,
		})
	}
	writeJSON(w, r, http.StatusOK, items)
}

// ── List ─────────────────────────────────────────────────────────────

// List handles GET /runs.
func (h *RunHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	// Runs paginate by offset: run ids are random rather than monotonic,
	// so there is no keyset to resume from. The offset still travels as an
	// opaque cursor so the wire contract matches every other list.
	offset := 0
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		decoded, err := decodeCursorString(cursor)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid cursor")
			return
		}
		offset, _ = strconv.Atoi(decoded)
	}

	page, err := h.svc.List(r.Context(), userID, run.ListQuery{
		BundleRef: r.URL.Query().Get("bundle_ref"), Status: r.URL.Query().Get("status"),
		Limit: parseLimit(r.URL.Query().Get("limit")), Offset: offset,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeDomainPage(w, r, mapPage(page, toRunSummaryDTO))
}

// ── Get ──────────────────────────────────────────────────────────────

// Get handles GET /runs/{id}.
func (h *RunHandlers) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	detail, err := h.svc.Get(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, runDetailDTO{
		runSummaryDTO: toRunSummaryDTO(detail.Run),
		SharedState:   detail.SharedState,
		IsOwner:       detail.IsOwner,
		Usage: runUsageDTO{
			TotalTokens: detail.Run.Usage.TotalTokens, CostUSD: detail.Run.Usage.CostUSD,
			DurationSeconds: detail.Run.DurationSeconds(),
		},
	})
}

// ── Stream ───────────────────────────────────────────────────────────

// 两个间隔，对应两种形态。
//
// 有广播器时（正常部署），事件是推过来的，轮询降级成纯兜底心跳：只为覆盖
// 广播器覆盖不到的情况——运行跑在另一个副本上、订阅者积压丢过事件、进程
// 崩溃导致运行没写终态事件。2s 一次，每条连接 0.5 QPS，是原来 6.7 QPS 的
// 十三分之一。
//
// 没有广播器时（测试里的裸 handler），退回 spec-12 原本的 300ms 轮询，行为
// 和这次改造之前一致。
const (
	streamSafetyInterval = 2 * time.Second
	streamPollInterval   = 300 * time.Millisecond
)

type runEventDTO struct {
	ID        int64          `json:"id"`
	Type      string         `json:"type"`
	RunID     string         `json:"run_id"`
	Node      *string        `json:"node,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload"`
}

// Stream handles GET /runs/{id}/stream — NDJSON, and per openapi.yaml the
// one endpoint that does not use the unified envelope.
func (h *RunHandlers) Stream(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	afterID := int64(0)
	if v := r.URL.Query().Get("after_id"); v != "" {
		afterID, _ = strconv.ParseInt(v, 10, 64)
	}

	// 订阅要在下面首次读库之前建立。顺序反过来的话，"读完库"到"挂上订阅"
	// 这段空窗里引擎写进去的事件，既不在查询结果里、也不在订阅流里，会被
	// 永久跳过——正好是模型开始吐字最密的那一瞬间。
	var sub *runstream.Subscription
	if h.bus != nil {
		sub = h.bus.Subscribe(id)
		defer sub.Close()
	}

	// 状态在事件之前读。engine.finish 先写终态事件再改状态，所以"读到的
	// 状态是终态"就意味着终态事件在这次读取之前已经落库，紧接着的事件查
	// 询一定带得上它。反过来先读事件再读状态，就会在两次读取之间漏掉刚写
	// 进去的 bundle.finished，连接断在终态事件之前——前端只能判成"意外断
	// 流"，永远等不到完成。
	//
	// 第一次读取都在写任何响应头之前，所以越权或运行不存在还能正常返回错
	// 误信封，而不是先 200 再在流里塞一行错误。
	status, err := h.svc.Status(r.Context(), id)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	events, err := h.svc.EventsAfter(r.Context(), userID, id, afterID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}

	// 补完历史之后、进入实时之前，把"现场"下发一次：正在跑的节点已经生成
	// 了哪些文字。逐 token 的增量不落库（见 runstream.PublishingStore），
	// 所以库里读不到这半段；没有这一步，刷新页面的人会看到答案凭空少了一
	// 截，只能空等这一轮结束。
	//
	// 顺序上必须排在历史之后：快照是赋值语义，先发会被后面的历史盖掉。
	if sub != nil {
		events = append(events, h.bus.Snapshot(id)...)
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	terminal := false
	for {
		for _, ev := range events {
			dto := runEventDTO{ID: ev.ID, Type: ev.Type, RunID: id, Timestamp: ev.CreatedAt, Payload: ev.Payload}
			if ev.Node != "" {
				node := ev.Node
				dto.Node = &node
			}
			if err := writeNDJSONLine(w, dto); err != nil {
				return
			}
			// ephemeral 事件没有数据库 id（恒为 0），不能拿它推进游标——
			// 直接赋值会把 afterID 打回 0，下一次回库补读就会从头再来一遍。
			if ev.ID > afterID {
				afterID = ev.ID
			}
			// 终态事件本身就是关流的信号，不用再查一次状态确认。引擎的
			// finish 先写终态事件再改状态，所以看见事件必然不早于状态变更。
			if ev.Type == run.EventBundleFinished || ev.Type == run.EventBundleFailed {
				terminal = true
			}
		}
		if flusher != nil {
			flusher.Flush()
		}

		if terminal || status.Terminal() {
			return
		}

		// 实时路径：阻塞等广播器推事件。推到了就直接写出去，一次库都不查。
		if sub != nil {
			live, ok := waitForLive(r.Context(), sub, afterID)
			if !ok {
				return // 客户端断开
			}
			if len(live) > 0 {
				if sub.Lagged() {
					// 这个订阅者积压过，中间丢了若干条增量。丢掉的那些
					// **不在数据库里**（逐 token 的增量只推不存），所以回
					// 库补读补不回来——补洞要靠累积缓冲的快照：它是"到目前
					// 为止的全文"，赋值语义，一条就把漏掉的全填上。
					//
					// 快照排在增量之后：它是赋值，先发会被后面的增量盖掉。
					live = append(live, h.bus.Snapshot(id)...)
				}
				events = live
				continue
			}
			// 落到这里 = 心跳到点，这一轮没有新事件。回库看一眼，为的是发
			// 现广播器看不见的状态变化（运行跑在另一个副本上、或者进程崩溃
			// 时没写终态事件）。
		}

		// 兜底路径（也是没有广播器时的唯一路径）。同样先状态后事件，理由
		// 见上面第一次读取处。
		if sub == nil {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(streamPollInterval):
			}
		}
		if status, err = h.svc.Status(r.Context(), id); err != nil {
			return
		}
		if events, err = h.svc.EventsAfter(r.Context(), userID, id, afterID); err != nil {
			_ = writeNDJSONLine(w, runEventDTO{Type: "stream.error", RunID: id, Timestamp: h.now()})
			return
		}
	}
}

// keepLive 决定一条实时事件要不要转发。
//
// 落库的事件按 id 去重：小于等于游标的那些已经在补历史时发过了。ephemeral
// 事件不在库里，补读永远不会带上它们，所以一律放行——它们的 id 恒为 0，走
// id 比较会被全部丢掉。
func keepLive(ev run.Event, afterID int64) bool {
	return ev.Ephemeral || ev.ID > afterID
}

// waitForLive 阻塞到广播器推来至少一个事件、心跳到点、或者客户端断开。
//
// 第二个返回值为 false 表示客户端走了，调用方应当直接结束这次流。返回空切
// 片而 ok 为 true 表示心跳到点、这一轮没有实时事件——调用方该回库看一眼。
func waitForLive(ctx context.Context, sub *runstream.Subscription, afterID int64) ([]run.Event, bool) {
	var out []run.Event
	select {
	case <-ctx.Done():
		return nil, false
	case ev := <-sub.Events():
		if keepLive(ev, afterID) {
			out = append(out, ev)
		}
	case <-time.After(streamSafetyInterval):
		return nil, true
	}
	// 一条一条写会让每个 token 走一次 flush；把此刻已经排在 channel 里的
	// 都取出来合成一批，是"尽快出去"和"别把 syscall 打爆"之间的平衡。
	// 事件按 id 升序到达，所以拿 afterID 一路比过去是安全的。
	for {
		select {
		case ev := <-sub.Events():
			if keepLive(ev, afterID) {
				out = append(out, ev)
			}
		default:
			return out, true
		}
	}
}

func writeNDJSONLine(w http.ResponseWriter, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// ── Cancel ───────────────────────────────────────────────────────────

// Cancel handles POST /runs/{id}/cancel.
func (h *RunHandlers) Cancel(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	if err := h.svc.Cancel(r.Context(), userID, chi.URLParam(r, "id")); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// compile-time guard: the page helper must keep producing a domain.Page.
var _ = domain.Page[run.Run]{}
