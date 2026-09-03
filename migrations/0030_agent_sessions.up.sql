-- ADK 会话持久化：让"一次对话"跨多次运行连起来。
--
-- 在这之前 orchestrator 每跑一次运行都新建一个 ADKRunner，配的是
-- session.InMemoryService()，而且 SessionID 直接取 runID——也就是说每
-- 发一条消息都是全新会话，模型看不到上一轮说过什么，进程一重启更是什么
-- 都不剩。spec-10 的验收项"重启进程后运行仍能从暂停处恢复"要的正是这
-- 张表。
--
-- 事件整条以 JSONB 存，而不是把 model.LLMResponse 摊成几十个列：ADK 的
-- 事件结构跟着版本走，摊列意味着每次升级都要跟着改表；这里只把需要排
-- 序和检索的字段拎出来做列。

CREATE TABLE adk_sessions (
    app_name    VARCHAR(64)  NOT NULL,
    user_id     VARCHAR(64)  NOT NULL,
    session_id  VARCHAR(64)  NOT NULL,
    state       JSONB        NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (app_name, user_id, session_id)
);

CREATE TABLE adk_session_events (
    seq         BIGINT       PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    app_name    VARCHAR(64)  NOT NULL,
    user_id     VARCHAR(64)  NOT NULL,
    session_id  VARCHAR(64)  NOT NULL,
    event_id    VARCHAR(64)  NOT NULL,
    author      VARCHAR(64)  NOT NULL,
    event       JSONB        NOT NULL,
        -- 整条 session.Event 序列化后的样子，读回来直接反序列化
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    FOREIGN KEY (app_name, user_id, session_id)
        REFERENCES adk_sessions (app_name, user_id, session_id) ON DELETE CASCADE
);

CREATE INDEX idx_adk_session_events ON adk_session_events (app_name, user_id, session_id, seq);

-- app: / user: 前缀的 state 不属于某一段会话，ADK 会把它们从 StateDelta
-- 里拆出来单独存、读的时候再合回去（见 sessionutils.ExtractStateDeltas）。
-- 少了这两张表，带前缀的写入会被静默丢掉。
CREATE TABLE adk_app_state (
    app_name    VARCHAR(64)  PRIMARY KEY,
    state       JSONB        NOT NULL DEFAULT '{}',
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE adk_user_state (
    app_name    VARCHAR(64)  NOT NULL,
    user_id     VARCHAR(64)  NOT NULL,
    state       JSONB        NOT NULL DEFAULT '{}',
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (app_name, user_id)
);

-- 一次运行归属哪一段会话。历史数据留空——那些运行本来就各自独立，补一
-- 个假的会话 id 只会让"这些消息是连着的"变成谎话。
ALTER TABLE bundle_runs ADD COLUMN session_id VARCHAR(64);

CREATE INDEX idx_bundle_runs_session ON bundle_runs (session_id, created_at);
