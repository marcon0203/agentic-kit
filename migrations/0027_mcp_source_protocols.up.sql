-- MCP 源接第三方注册中心：一个源现在要说清楚"它说的是哪套协议"。
--
-- protocol 决定用哪个 fetcher 解析上游响应：
--   mcp-registry  官方 MCP Registry 规范（server.json + remotes/packages）。
--                 各家子注册中心（PulseMCP 等）实现的是同一套形状，只是版本
--                 前缀不同，所以它们共用这个协议，靠 api_prefix 区分。
--   smithery      Smithery 自己的一套（qualifiedName/pagination），要 API Key。
--
-- api_prefix 是接口的版本前缀，实际请求 {base_url}{api_prefix}/servers。
-- 做成一列而不是写死在协议里：子注册中心各自停在不同版本上（官方 /v0、
-- PulseMCP /v0.1），写死等于每加一家改一次代码；留成配置，管理员照着对方
-- 文档填一次就行，填错了 last_sync_error 当场告诉他。
--
-- api_key_encrypted 与模型提供商的组织级凭据同一把 AES-256 密钥，明文既不
-- 落库也不出现在任何响应里（DTO 只给 has_api_key 布尔值）。
ALTER TABLE mcp_sources
    ADD COLUMN protocol          TEXT NOT NULL DEFAULT 'mcp-registry',
    ADD COLUMN api_prefix        TEXT NOT NULL DEFAULT '/v0',
    ADD COLUMN api_key_encrypted TEXT;
