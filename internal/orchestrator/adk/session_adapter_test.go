package adk

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// fakeSessionStore 是一份内存实现，够把适配器的语义验清楚：前缀分流、
// partial 不落库、跨轮次事件累积。
type fakeSessionStore struct {
	sessions map[SessionKey]map[string]any
	events   map[SessionKey][]StoredSessionEvent
	appState map[string]map[string]any
	usrState map[string]map[string]any
	updated  map[SessionKey]time.Time
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{
		sessions: map[SessionKey]map[string]any{},
		events:   map[SessionKey][]StoredSessionEvent{},
		appState: map[string]map[string]any{},
		usrState: map[string]map[string]any{},
		updated:  map[SessionKey]time.Time{},
	}
}

func (f *fakeSessionStore) CreateSession(_ context.Context, key SessionKey, state map[string]any) error {
	if _, ok := f.sessions[key]; !ok {
		f.sessions[key] = map[string]any{}
	}
	for k, v := range state {
		f.sessions[key][k] = v
	}
	return nil
}

func (f *fakeSessionStore) GetSession(_ context.Context, key SessionKey) (SessionSnapshot, bool, error) {
	state, ok := f.sessions[key]
	if !ok {
		return SessionSnapshot{}, false, nil
	}
	return SessionSnapshot{
		SessionID:    key.SessionID,
		SessionState: cloneAny(state),
		AppState:     cloneAny(f.appState[key.AppName]),
		UserState:    cloneAny(f.usrState[key.AppName+"/"+key.UserID]),
		Events:       f.events[key],
		UpdatedAt:    f.updated[key],
	}, true, nil
}

func (f *fakeSessionStore) ListSessions(_ context.Context, appName, userID string) ([]SessionSnapshot, error) {
	var out []SessionSnapshot
	for key, state := range f.sessions {
		if key.AppName != appName || key.UserID != userID {
			continue
		}
		out = append(out, SessionSnapshot{SessionID: key.SessionID, SessionState: cloneAny(state)})
	}
	return out, nil
}

func (f *fakeSessionStore) DeleteSession(_ context.Context, key SessionKey) error {
	delete(f.sessions, key)
	delete(f.events, key)
	return nil
}

func (f *fakeSessionStore) AppendSessionEvent(_ context.Context, key SessionKey, ev StoredSessionEvent) error {
	f.events[key] = append(f.events[key], ev)
	return nil
}

func (f *fakeSessionStore) ApplyStateDelta(_ context.Context, key SessionKey, appDelta, userDelta, sessionDelta map[string]any) error {
	if len(appDelta) > 0 {
		if f.appState[key.AppName] == nil {
			f.appState[key.AppName] = map[string]any{}
		}
		for k, v := range appDelta {
			f.appState[key.AppName][k] = v
		}
	}
	if len(userDelta) > 0 {
		uk := key.AppName + "/" + key.UserID
		if f.usrState[uk] == nil {
			f.usrState[uk] = map[string]any{}
		}
		for k, v := range userDelta {
			f.usrState[uk][k] = v
		}
	}
	if len(sessionDelta) > 0 {
		if f.sessions[key] == nil {
			f.sessions[key] = map[string]any{}
		}
		for k, v := range sessionDelta {
			f.sessions[key][k] = v
		}
	}
	return nil
}

func cloneAny(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func textEvent(id, author, text string) *session.Event {
	ev := session.NewEventWithContext(context.Background(), "inv-"+id)
	ev.ID = id
	ev.Author = author
	ev.Content = genai.NewContentFromText(text, genai.RoleModel)
	ev.Timestamp = time.Unix(1700000000, 0).UTC()
	return ev
}

func mustCreate(t *testing.T, svc session.Service, key SessionKey) session.Session {
	t.Helper()
	resp, err := svc.Create(context.Background(), &session.CreateRequest{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return resp.Session
}

// 这条是整件事的重点：第二轮拿到的会话里必须带着第一轮的事件。在换掉
// InMemoryService 之前，SessionID 直接取 runID，第二轮永远是空历史——用
// 户看到的就是"每发一条消息模型都失忆"。
func TestSessionService_SecondTurnSeesFirstTurnsEvents(t *testing.T) {
	store := newFakeSessionStore()
	svc := NewSessionService(store)
	key := SessionKey{AppName: "agentic-kit", UserID: "7", SessionID: "sess-1"}

	sess := mustCreate(t, svc, key)
	if err := svc.AppendEvent(context.Background(), sess, textEvent("e1", "user", "北京天气怎么样")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := svc.AppendEvent(context.Background(), sess, textEvent("e2", "writer", "晴，25 度")); err != nil {
		t.Fatalf("append: %v", err)
	}

	// 第二轮：一次全新的 Get，模拟下一次运行重新把会话读出来。
	resp, err := svc.Get(context.Background(), &session.GetRequest{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID,
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got := resp.Session.Events()
	if got.Len() != 2 {
		t.Fatalf("第二轮应当看到第一轮的 2 条事件，实际 %d 条", got.Len())
	}
	if text := contentText(got.At(1).Content); text != "晴，25 度" {
		t.Fatalf("事件内容没原样读回来：%q", text)
	}
}

// 流式增量不落库，否则下一轮历史里同一句话会以逐字前缀的形式出现很多遍。
func TestSessionService_PartialEventsAreNotPersisted(t *testing.T) {
	store := newFakeSessionStore()
	svc := NewSessionService(store)
	key := SessionKey{AppName: "agentic-kit", UserID: "7", SessionID: "sess-1"}
	sess := mustCreate(t, svc, key)

	partial := textEvent("p1", "writer", "晴")
	partial.Partial = true
	if err := svc.AppendEvent(context.Background(), sess, partial); err != nil {
		t.Fatalf("append partial: %v", err)
	}
	if err := svc.AppendEvent(context.Background(), sess, textEvent("e1", "writer", "晴，25 度")); err != nil {
		t.Fatalf("append final: %v", err)
	}

	if n := len(store.events[key]); n != 1 {
		t.Fatalf("只该落最终那条，实际落了 %d 条", n)
	}
}

// state 按 key 前缀分流：app: 归应用、user: 归用户、temp: 谁也不归（只活
// 在这一次调用里），其余归这一段会话。
func TestSessionService_StateDeltaRoutesByKeyPrefix(t *testing.T) {
	store := newFakeSessionStore()
	svc := NewSessionService(store)
	key := SessionKey{AppName: "agentic-kit", UserID: "7", SessionID: "sess-1"}
	sess := mustCreate(t, svc, key)

	ev := textEvent("e1", "writer", "ok")
	ev.Actions.StateDelta = map[string]any{
		"app:tier":   "pro",
		"user:lang":  "zh",
		"temp:token": "不该落库",
		"draft":      "会话自己的",
	}
	if err := svc.AppendEvent(context.Background(), sess, ev); err != nil {
		t.Fatalf("append: %v", err)
	}

	if got := store.appState["agentic-kit"]["tier"]; got != "pro" {
		t.Fatalf("app: 前缀该进应用级 state，得到 %v", got)
	}
	if got := store.usrState["agentic-kit/7"]["lang"]; got != "zh" {
		t.Fatalf("user: 前缀该进用户级 state，得到 %v", got)
	}
	if got := store.sessions[key]["draft"]; got != "会话自己的" {
		t.Fatalf("无前缀该进会话 state，得到 %v", got)
	}
	for _, m := range []map[string]any{store.appState["agentic-kit"], store.usrState["agentic-kit/7"], store.sessions[key]} {
		for k := range m {
			if k == "token" || k == "temp:token" {
				t.Fatal("temp: 前缀的 state 不该落库")
			}
		}
	}
}

// 读回来的 state 要把 app:/user: 重新带上前缀，跟 ADK 自己的 mergeStates
// 一致——编译出来的 agent 是按带前缀的 key 去读的。
func TestSessionState_ReadsBackWithScopePrefixes(t *testing.T) {
	store := newFakeSessionStore()
	svc := NewSessionService(store)
	key := SessionKey{AppName: "agentic-kit", UserID: "7", SessionID: "sess-1"}
	sess := mustCreate(t, svc, key)

	if err := sess.State().Set("app:tier", "pro"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := sess.State().Set("user:lang", "zh"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := sess.State().Set("draft", "x"); err != nil {
		t.Fatalf("set: %v", err)
	}

	resp, err := svc.Get(context.Background(), &session.GetRequest{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID,
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	seen := map[string]any{}
	for k, v := range resp.Session.State().All() {
		seen[k] = v
	}
	for k, want := range map[string]any{"app:tier": "pro", "user:lang": "zh", "draft": "x"} {
		if seen[k] != want {
			t.Fatalf("state[%q] = %v，期望 %v（全部键：%v）", k, seen[k], want, seen)
		}
	}
	if v, err := resp.Session.State().Get("app:tier"); err != nil || v != "pro" {
		t.Fatalf(`Get("app:tier") = %v, %v`, v, err)
	}
	if _, err := resp.Session.State().Get("不存在"); err == nil {
		t.Fatal("取不存在的 key 应当报 ErrStateKeyNotExist")
	}
}

// runner.Run 靠 Get 报错来决定要不要 AutoCreateSession；返回一个空会话会
// 让它当成"已存在但没有历史"，第一轮就永远建不出会话来。
func TestSessionService_GetMissingSessionIsAnError(t *testing.T) {
	svc := NewSessionService(newFakeSessionStore())
	_, err := svc.Get(context.Background(), &session.GetRequest{
		AppName: "agentic-kit", UserID: "7", SessionID: "从没建过",
	})
	if err == nil {
		t.Fatal("会话不存在时 Get 必须报错")
	}
}

func TestSessionService_GetAppliesNumRecentEventsAndAfter(t *testing.T) {
	store := newFakeSessionStore()
	svc := NewSessionService(store)
	key := SessionKey{AppName: "agentic-kit", UserID: "7", SessionID: "sess-1"}
	sess := mustCreate(t, svc, key)

	base := time.Unix(1700000000, 0).UTC()
	for i, text := range []string{"一", "二", "三"} {
		ev := textEvent(string(rune('a'+i)), "writer", text)
		ev.Timestamp = base.Add(time.Duration(i) * time.Minute)
		if err := svc.AppendEvent(context.Background(), sess, ev); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	resp, err := svc.Get(context.Background(), &session.GetRequest{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID, NumRecentEvents: 2,
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if n := resp.Session.Events().Len(); n != 2 {
		t.Fatalf("NumRecentEvents=2 应当只回 2 条，得到 %d", n)
	}
	if got := contentText(resp.Session.Events().At(0).Content); got != "二" {
		t.Fatalf("回的应当是最后两条，第一条却是 %q", got)
	}

	resp, err = svc.Get(context.Background(), &session.GetRequest{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID, After: base.Add(90 * time.Second),
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if n := resp.Session.Events().Len(); n != 1 {
		t.Fatalf("After 过滤后应当只剩 1 条，得到 %d", n)
	}
}

// 事件整条以 JSON 存，所以工具调用这类结构必须原样回得来——回不来的话，
// 下一轮模型看到的历史就是残的。
func TestSessionEvent_SurvivesJSONRoundTrip(t *testing.T) {
	ev := textEvent("e1", "writer", "查一下")
	ev.Content.Parts = append(ev.Content.Parts, &genai.Part{
		FunctionCall: &genai.FunctionCall{Name: "get_weather", Args: map[string]any{"city": "北京"}},
	})
	ev.Actions.TransferToAgent = "reviewer"
	ev.LongRunningToolIDs = []string{"call-1"}

	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back session.Event
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if back.ID != ev.ID || back.Author != ev.Author {
		t.Fatalf("身份字段丢了：%+v", back)
	}
	if back.Actions.TransferToAgent != "reviewer" {
		t.Fatalf("Actions 丢了：%+v", back.Actions)
	}
	if len(back.LongRunningToolIDs) != 1 || back.LongRunningToolIDs[0] != "call-1" {
		t.Fatalf("LongRunningToolIDs 丢了：%v", back.LongRunningToolIDs)
	}
	if len(back.Content.Parts) != 2 || back.Content.Parts[1].FunctionCall == nil {
		t.Fatalf("工具调用没回来：%+v", back.Content)
	}
	if got := back.Content.Parts[1].FunctionCall.Args["city"]; got != "北京" {
		t.Fatalf("工具参数没回来：%v", got)
	}
}
