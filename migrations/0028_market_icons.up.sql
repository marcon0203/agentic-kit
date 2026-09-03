-- 市场卡片要有图。
--
-- 两张快照表各加一列 icon_url，同步时从上游解析：官方 MCP Registry 规范里
-- 是 server.json 的 icons[]，Smithery 给 iconUrl，Skill 源给列表项上的
-- icon/image/logo。上游没给就留空——前端按 slug 生成字母图标兜底，不会出
-- 现半张空白卡片。
--
-- 只存地址不存图：图由浏览器直接去上游拉。把几千张图代理或落到对象存储，
-- 是为了一个纯装饰性的东西引入一整套缓存与失效逻辑，不划算。
ALTER TABLE market_skills      ADD COLUMN icon_url TEXT;
ALTER TABLE market_mcp_servers ADD COLUMN icon_url TEXT;
