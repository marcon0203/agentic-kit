-- 单独运行（"立即体验"）功能：匿名访客也要走完整的 JWT 鉴权链路才能复
-- 用 POST /runs、GET /runs/{id}、GET /runs/{id}/stream 这整套既有代码
-- （黑盒事件过滤、run 归属校验都靠调用者的 userID，不是另开一套）——
-- 所以每个访客背后其实是一个真实创建的 users 行，只是从没人用它登录过。
--
-- is_guest 只是一个可查询的标记，不参与任何鉴权判断：区分"这行是访客
-- 自动生成的"和"这是真实注册账号"，方便后续按需清理，或者在用户列表
-- 类页面里过滤掉。
ALTER TABLE users ADD COLUMN is_guest BOOLEAN NOT NULL DEFAULT false;
