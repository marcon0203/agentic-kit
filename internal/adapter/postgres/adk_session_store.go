package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// ADKSessionStore 把 ADK 会话落在 Postgres 上，实现 adk.SessionStore。
//
// 在这之前 orchestrator 用的是 ADK 自带的 session.InMemoryService()，而
// 且每次运行都新建一个——也就是说同一段对话的上一轮说了什么，下一轮完全
// 看不到，进程一重启更是什么都不剩。
type ADKSessionStore struct{ q store.Querier }

func NewADKSessionStore(q store.Querier) *ADKSessionStore { return &ADKSessionStore{q: q} }

var _ adk.SessionStore = (*ADKSessionStore)(nil)

func (s *ADKSessionStore) CreateSession(ctx context.Context, key adk.SessionKey, state map[string]any) error {
	raw, err := marshalState(state)
	if err != nil {
		return err
	}
	_, err = s.q.UpsertADKSession(ctx, store.UpsertADKSessionParams{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID, State: raw,
	})
	return err
}

func (s *ADKSessionStore) GetSession(ctx context.Context, key adk.SessionKey) (adk.SessionSnapshot, bool, error) {
	row, err := s.q.GetADKSession(ctx, store.GetADKSessionParams{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return adk.SessionSnapshot{}, false, nil
		}
		return adk.SessionSnapshot{}, false, err
	}

	snap, err := s.snapshotOf(ctx, key.AppName, key.UserID, row)
	if err != nil {
		return adk.SessionSnapshot{}, false, err
	}

	events, err := s.q.ListADKSessionEvents(ctx, store.ListADKSessionEventsParams{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID,
	})
	if err != nil {
		return adk.SessionSnapshot{}, false, err
	}
	snap.Events = make([]adk.StoredSessionEvent, 0, len(events))
	for _, ev := range events {
		snap.Events = append(snap.Events, adk.StoredSessionEvent{
			ID: ev.EventID, Author: ev.Author, Payload: ev.Event,
		})
	}
	return snap, true, nil
}

// ListSessions 只列会话本身，Events 留空——口子上就是这么约定的，把每段
// 对话的全部事件都拉出来会让一次列表变成全表扫描。
func (s *ADKSessionStore) ListSessions(ctx context.Context, appName, userID string) ([]adk.SessionSnapshot, error) {
	rows, err := s.q.ListADKSessions(ctx, store.ListADKSessionsParams{AppName: appName, UserID: userID})
	if err != nil {
		return nil, err
	}
	out := make([]adk.SessionSnapshot, 0, len(rows))
	for _, row := range rows {
		snap, err := s.snapshotOf(ctx, appName, userID, row)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, nil
}

func (s *ADKSessionStore) DeleteSession(ctx context.Context, key adk.SessionKey) error {
	return s.q.DeleteADKSession(ctx, store.DeleteADKSessionParams{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID,
	})
}

func (s *ADKSessionStore) AppendSessionEvent(ctx context.Context, key adk.SessionKey, ev adk.StoredSessionEvent) error {
	return s.q.AppendADKSessionEvent(ctx, store.AppendADKSessionEventParams{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID,
		EventID: ev.ID, Author: ev.Author, Event: ev.Payload,
	})
}

func (s *ADKSessionStore) ApplyStateDelta(ctx context.Context, key adk.SessionKey, appDelta, userDelta, sessionDelta map[string]any) error {
	if len(appDelta) > 0 {
		raw, err := marshalState(appDelta)
		if err != nil {
			return err
		}
		if err := s.q.MergeADKAppState(ctx, store.MergeADKAppStateParams{AppName: key.AppName, State: raw}); err != nil {
			return err
		}
	}
	if len(userDelta) > 0 {
		raw, err := marshalState(userDelta)
		if err != nil {
			return err
		}
		if err := s.q.MergeADKUserState(ctx, store.MergeADKUserStateParams{
			AppName: key.AppName, UserID: key.UserID, State: raw,
		}); err != nil {
			return err
		}
	}
	if len(sessionDelta) == 0 {
		return nil
	}
	// 会话自己的 state 没法直接用 jsonb 的 || 合并——那条 UPDATE 的目标行
	// 必须已经存在，而且这里要的是"读出来、并上 delta、写回去"。读一次再
	// 写一次即可：同一段会话同一时刻只有一次运行在写。
	row, err := s.q.GetADKSession(ctx, store.GetADKSessionParams{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID,
	})
	if err != nil {
		return err
	}
	merged, err := unmarshalState(row.State)
	if err != nil {
		return err
	}
	for k, v := range sessionDelta {
		merged[k] = v
	}
	raw, err := marshalState(merged)
	if err != nil {
		return err
	}
	return s.q.SetADKSessionState(ctx, store.SetADKSessionStateParams{
		AppName: key.AppName, UserID: key.UserID, SessionID: key.SessionID, State: raw,
	})
}

func (s *ADKSessionStore) snapshotOf(ctx context.Context, appName, userID string, row store.AdkSession) (adk.SessionSnapshot, error) {
	sessionState, err := unmarshalState(row.State)
	if err != nil {
		return adk.SessionSnapshot{}, err
	}
	appState, err := s.scopedState(ctx, func() ([]byte, error) { return s.q.GetADKAppState(ctx, appName) })
	if err != nil {
		return adk.SessionSnapshot{}, err
	}
	userState, err := s.scopedState(ctx, func() ([]byte, error) {
		return s.q.GetADKUserState(ctx, store.GetADKUserStateParams{AppName: appName, UserID: userID})
	})
	if err != nil {
		return adk.SessionSnapshot{}, err
	}
	return adk.SessionSnapshot{
		SessionID:    row.SessionID,
		SessionState: sessionState,
		AppState:     appState,
		UserState:    userState,
		UpdatedAt:    row.UpdatedAt.Time,
	}, nil
}

// scopedState 把"这个作用域还没写过任何 state"当成空 map 而不是错误。
func (s *ADKSessionStore) scopedState(_ context.Context, read func() ([]byte, error)) (map[string]any, error) {
	raw, err := read()
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	return unmarshalState(raw)
}

func marshalState(state map[string]any) ([]byte, error) {
	if len(state) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(state)
}

func unmarshalState(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
