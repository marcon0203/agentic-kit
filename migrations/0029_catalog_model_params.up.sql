-- 模型级请求参数（如 Anthropic 线协议必填的 max_tokens）。
--
-- 参数的**形状**（名字/类型/区间/必填）由渠道描述符快照里的 request_params
-- 声明（描述符层校验），这里存的是管理员在「添加模型」时填的**取值**，调用
-- 时由网关按 provider+model 取出注入进请求——平台不做任何静默兜底，缺必填
-- 参数的请求在发出去之前就会被拦下。
ALTER TABLE catalog_models
    ADD COLUMN params JSONB NOT NULL DEFAULT '{}';
