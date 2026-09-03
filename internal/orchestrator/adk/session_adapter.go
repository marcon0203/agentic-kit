package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"strings"
	"time"

	"google.golang.org/adk/session"
)

// SessionKey 定位一段会话。ADK 的每个会话调用都带着这三元组。
type SessionKey struct {
	AppName   string
	UserID    string
	SessionID string
}

// StoredSessionEvent 是一条落库的会话事件。Payload 是整条 session.Event
// 序列化后的 JSON——**故意不拆字段**：ADK 还在 1.x 之前，事件结构跟着版
// 本走，摊成列意味着每次升级都要跟着改表和改映射。ID 和 Author 单拎出来
// 是因为存储侧要用它们排序和检索。
type StoredSessionEvent struct {
	ID      string
	Author  string
	Payload []byte
}

// SessionSnapshot 是一段会话读回来的全部内容。三份 state 分开给：合并成
// 一份是 ADK 的语义（见 mergeStates），不该压给存储层。
type SessionSnapshot struct {
	SessionID    string
	SessionState map[string]any
	AppState     map[string]any
	UserState    map[string]any
	Events       []StoredSessionEvent
	UpdatedAt    time.Time
}

// SessionStore 持久化 ADK 会话。跟 MemoryStore 一样，这个口子上不出现任
// 何 ADK 类型，好让 internal/adapter/postgres 里的实现不必 import
// google.golang.org/adk（spec-10：所有 ADK 调用收敛在
// internal/orchestrator/adk 包内，import_confinement_test.go 会验）。
type SessionStore interface {
	CreateSession(ctx context.Context, key SessionKey, state map[string]any) error
	// GetSession 在会话不存在时返回 found=false 而不是错误——"没有"是常
	// 态，ADK runner 遇到就自己去 Create。
	GetSession(ctx context.Context, key SessionKey) (snap SessionSnapshot, found bool, err error)
	// ListSessions 只返回会话本身，Events 留空：列表要的是清单，不是把每
	// 段对话的全部事件都拉出来。
	ListSessions(ctx context.Context, appName, userID string) ([]SessionSnapshot, error)
	DeleteSession(ctx context.Context, key SessionKey) error
	AppendSessionEvent(ctx context.Context, key SessionKey, ev StoredSessionEvent) error
	// ApplyStateDelta 把一次事件带来的 state 变更按作用域写下去。三份都
	// 可能为空。
	ApplyStateDelta(ctx context.Context, key SessionKey, appDelta, userDelta, sessionDelta map[string]any) error
}

// NewSessionService 把 SessionStore 适配成 ADK 的 session.Service。
//
// 换掉 session.InMemoryService() 的意义有两条：一是同一段对话跨多次运行
// 能连起来（在这之前 SessionID 直接取 runID，每发一条消息模型都失忆），
// 二是进程重启后会话还在。
func NewSessionService(store SessionStore) session.Service {
	return &sessionServiceAdapter{store: store}
}

type sessionServiceAdapter struct{ store SessionStore }

var _ session.Service = (*sessionServiceAdapter)(nil)

func (a *sessionServiceAdapter) Create(ctx context.Context, req *session.CreateRequest) (*session.CreateResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("adk session: create request is nil")
	}
	key := SessionKey{AppName: req.AppName, UserID: req.UserID, SessionID: req.SessionID}
	if err := validateKey(key); err != nil {
		return nil, err
	}
	// 建会话时给的 state 同样要分作用域：app:/user: 前缀的不属于这一段会
	// 话，temp: 前缀的根本不落库。
	appDelta, userDelta, sessionDelta := splitStateDeltas(req.State)
	if err := a.store.CreateSession(ctx, key, sessionDelta); err != nil {
		return nil, err
	}
	if len(appDelta) > 0 || len(userDelta) > 0 {
		if err := a.store.ApplyStateDelta(ctx, key, appDelta, userDelta, nil); err != nil {
			return nil, err
		}
	}
	snap, found, err := a.store.GetSession(ctx, key)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("adk session: %q disappeared right after create", req.SessionID)
	}
	sess, err := newStoredSession(a.store, key, snap)
	if err != nil {
		return nil, err
	}
	return &session.CreateResponse{Session: sess}, nil
}

func (a *sessionServiceAdapter) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("adk session: get request is nil")
	}
	key := SessionKey{AppName: req.AppName, UserID: req.UserID, SessionID: req.SessionID}
	if err := validateKey(key); err != nil {
		return nil, err
	}
	snap, found, err := a.store.GetSession(ctx, key)
	if err != nil {
		return nil, err
	}
	if !found {
		// runner.Run 靠这个错误决定要不要 AutoCreateSession，所以"不存在"
		// 必须是错误而不是空会话（见 adk runner.go 的 Run）。
		return nil, fmt.Errorf("adk session: %q not found", req.SessionID)
	}
	sess, err := newStoredSession(a.store, key, snap)
	if err != nil {
		return nil, err
	}
	sess.events = filterEvents(sess.events, req.NumRecentEvents, req.After)
	return &session.GetResponse{Session: sess}, nil
}

func (a *sessionServiceAdapter) List(ctx context.Context, req *session.ListRequest) (*session.ListResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("adk session: list request is nil")
	}
	snaps, err := a.store.ListSessions(ctx, req.AppName, req.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]session.Session, 0, len(snaps))
	for _, snap := range snaps {
		key := SessionKey{AppName: req.AppName, UserID: req.UserID, SessionID: snap.SessionID}
		sess, err := newStoredSession(a.store, key, snap)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return &session.ListResponse{Sessions: out}, nil
}

func (a *sessionServiceAdapter) Delete(ctx context.Context, req *session.DeleteRequest) error {
	if req == nil {
		return fmt.Errorf("adk session: delete request is nil")
	}
	key := SessionKey{AppName: req.AppName, UserID: req.UserID, SessionID: req.SessionID}
	if err := validateKey(key); err != nil {
		return err
	}
	return a.store.DeleteSession(ctx, key)
}

func (a *sessionServiceAdapter) AppendEvent(ctx context.Context, cur session.Session, ev *session.Event) error {
	if cur == nil {
		return fmt.Errorf("adk session: session is nil")
	}
	if ev == nil {
		return fmt.Errorf("adk session: event is nil")
	}
	// 流式过程中的增量片段不落库——最终那条非 partial 事件带着完整内容，
	// 存两份只会让下一轮的历史里同一句话出现很多遍。ADK 自己的
	// InMemoryService 也是这么做的。
	if ev.Partial {
		return nil
	}
	sess, ok := cur.(*storedSession)
	if !ok {
		return fmt.Errorf("adk session: unexpected session type %T", cur)
	}

	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("adk session: marshal event: %w", err)
	}
	if err := a.store.AppendSessionEvent(ctx, sess.key, StoredSessionEvent{
		ID: ev.ID, Author: ev.Author, Payload: payload,
	}); err != nil {
		return err
	}

	appDelta, userDelta, sessionDelta := splitStateDeltas(ev.Actions.StateDelta)
	if len(appDelta) > 0 || len(userDelta) > 0 || len(sessionDelta) > 0 {
		if err := a.store.ApplyStateDelta(ctx, sess.key, appDelta, userDelta, sessionDelta); err != nil {
			return err
		}
	}

	// 正在跑的这次调用拿的是同一个 Session 对象，所以内存里的那份也要跟
	// 上——不然本轮后面的步骤看不到刚刚写进去的 state 和事件。
	sess.events = append(sess.events, ev)
	sess.updatedAt = ev.Timestamp
	maps.Copy(sess.sessionState, sessionDelta)
	maps.Copy(sess.appState, appDelta)
	maps.Copy(sess.userState, userDelta)
	return nil
}

// ── 会话对象 ────────────────────────────────────────────────────────

type storedSession struct {
	store SessionStore
	key   SessionKey

	// 三份 state 分开留着，因为 Set 要按 key 前缀决定写去哪一份。
	sessionState map[string]any
	appState     map[string]any
	userState    map[string]any

	events    []*session.Event
	updatedAt time.Time
}

var _ session.Session = (*storedSession)(nil)

func newStoredSession(store SessionStore, key SessionKey, snap SessionSnapshot) (*storedSession, error) {
	events := make([]*session.Event, 0, len(snap.Events))
	for _, row := range snap.Events {
		var ev session.Event
		if err := json.Unmarshal(row.Payload, &ev); err != nil {
			return nil, fmt.Errorf("adk session: unmarshal event %s: %w", row.ID, err)
		}
		events = append(events, &ev)
	}
	return &storedSession{
		store:        store,
		key:          key,
		sessionState: orEmpty(snap.SessionState),
		appState:     orEmpty(snap.AppState),
		userState:    orEmpty(snap.UserState),
		events:       events,
		updatedAt:    snap.UpdatedAt,
	}, nil
}

func (s *storedSession) ID() string                { return s.key.SessionID }
func (s *storedSession) AppName() string           { return s.key.AppName }
func (s *storedSession) UserID() string            { return s.key.UserID }
func (s *storedSession) LastUpdateTime() time.Time { return s.updatedAt }
func (s *storedSession) Events() session.Events    { return sessionEvents(s.events) }
func (s *storedSession) State() session.State      { return &sessionState{sess: s} }

type sessionEvents []*session.Event

func (e sessionEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, ev := range e {
			if !yield(ev) {
				return
			}
		}
	}
}

func (e sessionEvents) Len() int                { return len(e) }
func (e sessionEvents) At(i int) *session.Event { return e[i] }

// sessionState 把 ADK 的扁平 key 空间映射到三份按作用域分开的 map 上。
// 读的时候合成一份（app:/user: 键带前缀出现），写的时候按前缀分流。
type sessionState struct{ sess *storedSession }

var _ session.State = (*sessionState)(nil)

func (s *sessionState) Get(key string) (any, error) {
	if k, ok := strings.CutPrefix(key, session.KeyPrefixApp); ok {
		if v, hit := s.sess.appState[k]; hit {
			return v, nil
		}
		return nil, session.ErrStateKeyNotExist
	}
	if k, ok := strings.CutPrefix(key, session.KeyPrefixUser); ok {
		if v, hit := s.sess.userState[k]; hit {
			return v, nil
		}
		return nil, session.ErrStateKeyNotExist
	}
	if v, hit := s.sess.sessionState[key]; hit {
		return v, nil
	}
	return nil, session.ErrStateKeyNotExist
}

func (s *sessionState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for k, v := range s.sess.sessionState {
			if !yield(k, v) {
				return
			}
		}
		for k, v := range s.sess.appState {
			if !yield(session.KeyPrefixApp+k, v) {
				return
			}
		}
		for k, v := range s.sess.userState {
			if !yield(session.KeyPrefixUser+k, v) {
				return
			}
		}
	}
}

func (s *sessionState) Set(key string, value any) error {
	ctx := context.Background()
	appDelta, userDelta, sessionDelta := splitStateDeltas(map[string]any{key: value})
	// temp: 前缀会被 splitStateDeltas 全部丢掉，那就只落到内存里——它按定
	// 义只活在这一次调用内。
	if len(appDelta)+len(userDelta)+len(sessionDelta) == 0 {
		return nil
	}
	if err := s.sess.store.ApplyStateDelta(ctx, s.sess.key, appDelta, userDelta, sessionDelta); err != nil {
		return err
	}
	maps.Copy(s.sess.sessionState, sessionDelta)
	maps.Copy(s.sess.appState, appDelta)
	maps.Copy(s.sess.userState, userDelta)
	return nil
}

// ── 小工具 ──────────────────────────────────────────────────────────

// splitStateDeltas 按 ADK 的 key 前缀把一份 state delta 拆成三份，
// temp: 前缀的直接丢掉。等价于 ADK 内部的 sessionutils.ExtractStateDeltas
// ——那个包在 internal/ 下，外部导不到，只能照着实现一份。
func splitStateDeltas(delta map[string]any) (appDelta, userDelta, sessionDelta map[string]any) {
	appDelta = map[string]any{}
	userDelta = map[string]any{}
	sessionDelta = map[string]any{}
	for key, value := range delta {
		switch {
		case strings.HasPrefix(key, session.KeyPrefixApp):
			appDelta[strings.TrimPrefix(key, session.KeyPrefixApp)] = value
		case strings.HasPrefix(key, session.KeyPrefixUser):
			userDelta[strings.TrimPrefix(key, session.KeyPrefixUser)] = value
		case strings.HasPrefix(key, session.KeyPrefixTemp):
			// 只活在这一次调用里，不落库。
		default:
			sessionDelta[key] = value
		}
	}
	return appDelta, userDelta, sessionDelta
}

func filterEvents(events []*session.Event, numRecent int, after time.Time) []*session.Event {
	if !after.IsZero() {
		kept := events[:0:0]
		for _, ev := range events {
			if !ev.Timestamp.Before(after) {
				kept = append(kept, ev)
			}
		}
		events = kept
	}
	if numRecent > 0 && len(events) > numRecent {
		events = events[len(events)-numRecent:]
	}
	return events
}

func validateKey(key SessionKey) error {
	if key.AppName == "" || key.UserID == "" || key.SessionID == "" {
		return fmt.Errorf("adk session: app_name/user_id/session_id 都不能为空，收到 %q/%q/%q",
			key.AppName, key.UserID, key.SessionID)
	}
	return nil
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
