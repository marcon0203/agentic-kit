# spec-05a-resource-registration-v2 资源注册体验升级

> 承接 spec-05（资源中心）。本文是方案讨论稿，**不含代码改动**——先对齐设计，再拆实现任务。
> 触发原因：`RegisterResourceDialog` 一个弹窗（ref + display_name + 一个裸 JSON `config` textarea）覆盖 Tool / Skill / MCP Server / 知识库 / 记忆库五种资源，字段差异巨大的东西被塞进同一张脸，用户体验和 spec-05 当初"分表设计"的精神背道而驰。

## 现状盘点（先说清楚"简陋"具体简陋在哪）

- 前端：`web/src/components/resources/RegisterResourceDialog.tsx` 是唯一的创建入口，五种 kind 共用同一份表单——`ref` / `display_name` / 一个 `config`（JSON 文本框）。没有任何 kind 专属字段，没有文件上传，没有在线编辑。
- 后端：`internal/domain/resource.Config` 是纯 `map[string]any`，没有 schema、没有 kind 专属校验；凭证识别靠 key 名子串匹配（`IsCredentialKey`），只覆盖"顶层字符串字段"，覆盖不到数组/嵌套结构（`internal/domain/resource/resource.go:86-98`）。
- MCP 的"连通性探测"（`internal/adapter/mcp/probe.go`）目前只是一次裸 HTTP GET，判断"5xx 与否"，**根本没有说 MCP 协议**，更不可能拉工具列表——代码注释里自己写着"a real implementation would speak the MCP handshake"，这是从 spec-05 阶段就留下的占位实现，现在要补上。
- Tool 运行时只有一种形态：`internal/orchestrator/adk/tools.go` 的 `buildEndpointTool`——POST 到 `config.endpoint`，body 透传，一问一答，没有别的路径。
- Skill 运行时只读 `config.instructions` 一个字符串（`skillInstructions`），没有文件的概念。
- 没有任何对象存储接入，仓库里不存在 OSS/S3 相关代码。

## 设计原则

1. **弹窗退场，二级页面上位**——跟这次 Bundle 编辑器的改法一致（`/apps/bundles/new` 有独立路由、侧栏常驻、有返回按钮）。五种资源里字段简单的（记忆库）可以留在轻量表单，字段复杂/需要多步骤的（组件的 OpenAPI 导入、MCP 的探测）必须是独立路由页面，不能塞进 Dialog 的滚动区域里。
2. **新增能力是叠加，不是替换**——现有最简单的"手填 JSON config"路径继续保留兜底（比如没配 OSS 时 Skill 还能用纯文本 instructions），新表单是更好的输入方式，不是唯一路径，避免这轮改动把线上已存在的资源"契约"破坏掉。
3. **凭证加密模型从"字符串子串匹配"扩展为"按 kind 注册专属规则"**——MCP 的自定义 Header 是本轮唯一一个必须现在处理的加密模型缺口（否则用户填的 Header 值明文落库）。
4. **运行时编译代码只在必要处改**——Tool 的 `http` 形态、Skill 走纯 `instructions` 的旧路径，行为完全不变；新形态是新增分支，不是重写。
5. **名字和判别字段按终态设计，功能按当下需要实现**——"组件"这个菜单名和 `component_type` 判别字段现在就按"以后要装下插件/沙箱环境"的终态定下来，但插件/沙箱环境本身不做，只做 Tool 这一支；这样以后补类型是加一个枚举值，不是推倒重来。
6. **系统级资源目录复用已有先例**——Skill 源同步下来的"系统提供"Skill，管理模式直接照抄系统设置里已经跑通的模型目录（`modelcatalog`：管理员登记、全局只读、所有人可见），不重新发明一套。

---

## 一、MCP Server：二级页面 + Header 列表 + 真实探测拉工具列表

这是本轮**诉求最具体、改动面最小**的一块，优先做。

### 页面

新增路由 `/apps/mcp/new`（嵌套在 `AppsLayout` 下，侧栏保留，仿照 `BundleEditorPage` 的返回按钮 + 无 PageHeader 的做法）。表单字段：

| 字段 | 说明 |
|---|---|
| `ref` | 内部标识，沿用现有 pattern `^[a-z][a-z0-9_-]*$` |
| `display_name` | 展示名（可选） |
| `description` | 说明文字，喂给运行时当 tool 的补充描述 |
| `url` | MCP endpoint，对应现有 `config.endpoint` |
| Header 列表 | 动态增删的 key/value 行（不是 JSON 输入框），例如 `Authorization: Bearer xxx` |

页面底部一个「检测」按钮：点击后调用新探测接口，**成功时把探测到的工具列表（名称 + 描述）实时渲染出来**——这是用户在保存前就能看到"这台 MCP Server 到底有哪些工具"，而不是保存完再去猜。检测失败给出具体错误（连不上 / 401 / 握手失败），不是笼统的"失败"。

保存按钮在未检测通过时不强制阻塞（跟 spec-05 原有"注册接口仍允许保存，便于事后修配置"的原则一致），但会有醒目提示"尚未检测通过"。

### 后端：真实 MCP 探测

新增 `POST /resources/mcp/probe`（纯探测，不落库）：

```
Request:  { url: string, headers: [{key: string, value: string}] }
Response: { ok: true, tools: [{name, description}] } | { ok: false, error: string }
```

实现上**不走 ADK 的 `mcptoolset.Toolset.Tools()`**——那个方法要求一个 `agent.ReadonlyContext`（ADK 运行时上下文），探测场景下并不存在一次真实的 agent 调用。改为直接用 `github.com/modelcontextprotocol/go-sdk/mcp` 的原始客户端（`internal/orchestrator/adk/tools.go` 已经在用这个包）：建连接 → `client.ListTools(ctx)`，拿 `[]mcp.Tool` 的 `Name`/`Description` 直接映射返回，不需要转成 ADK 的 `tool.Tool`。

这个探测函数要在两处复用，不要写两份：
- 新的 `/resources/mcp/probe` 预览接口（不落库）。
- 替换掉 `internal/adapter/mcp/probe.go` 现在的裸 HTTP GET 实现——`resource.HealthProbe.Check` 也改成走真实 MCP 握手，`healthy` 才真正代表"这是一台正常应答的 MCP Server"，而不是"随便一个 URL 返回了非 5xx"。定时健康刷新任务顺带把"最近一次探测到的工具名单"写回 `config.last_seen_tools`（仅供列表页展示，运行时永远是实时连接，不读这个缓存字段）。

### 凭证模型：Header 逐项加密

`IsCredentialKey` 现在只按 key 名子串猜（`api_key`/`token`/`secret`...），猜不到用户自定义的 `x-secret-9527` 这类 Header。方案：**Header 的 value 不做子串猜测，一律当凭证处理**——用户会在这个字段里填什么完全不可预测，宁可全部加密。

`config` 里的存储形状：

```json
{ "endpoint": "https://...", "headers": [{"key": "Authorization", "value": "<ciphertext>"}] }
```

`encryptCredentials` / `DecryptConfig` / `Config.Redact()` 三处（`internal/domain/resource/service.go` + `resource.go`）从"纯字符串子串匹配"扩展为一个可选的 per-kind 钩子：`IsCredentialKey` 处理不了嵌套数组，所以引入一个 `Kind` 维度的特例——`kind == KindMCP` 时，`config.headers[].value` 无条件走加密/无条件从任何 GET 响应里剔除，其余字段走原有子串规则。这是本轮对资源中心加密模型唯一的结构性改动，改动集中在 `internal/domain/resource/service.go`。

---

## 二、Skill：只支持 zip 上传（存阿里云 OSS），不做在线编辑；另开"Skill 源"同步

**改口**：不做在线编辑器。Skill 的编辑方式就是"重新上传一个新版本"——跟 Agent/Bundle 的 immutable 版本语义完全一致（已发布版本不可改，要改就建新 `version`），不需要为 Skill 单独发明一套"文件树 + 保存"的编辑体验。这样第二部分的实现范围直接砍掉文件在线编辑的所有前后端工作。

### 为什么不是"一个字符串"就够

Skill 的本质是"一段可复用的做事方式"，真实场景里从来不是一句话——它是一个入口文件（正文/流程说明）加若干附属文件（脚本、模板、参考资料），跟 Claude 自己的 Skill 格式（`SKILL.md` + 资源文件）是同一个心智模型。约定：

- 一个 Skill 在 OSS 上是一棵文件树，对象 key 形如 `skills/{owner_id}/{ref}/{version}/{相对路径}`。
- 必须有一个入口文件 `SKILL.md`（agent 实际读到的正文）。
- 允许任意数量的附属文件（本轮附属文件不喂给模型，只有入口文件内容进 `buildSkillTool` 的输出——先把"能管理一整个目录"这件事立住，模型按需读取附属文件是下一步，不在本轮）。

### 创建方式：只有一种——上传 zip

`POST /resources/skills/upload`（multipart），body 另带 `ref/version/display_name`。服务端解压校验（必须含 `SKILL.md`；限制解压后总大小、单文件大小、文件数量；校验 zip slip——拒绝任何 `..` 或绝对路径条目）→ 逐文件 PUT 到 OSS → 建 resource 行 + 建文件树索引行，一步到位的单次请求，不是"先建元数据再传文件"的两阶段（避免中间态：元数据建好了但文件没传完）。

文件树索引仍然保留（见下），但只用来"查看这个 Skill 包含哪些文件、下载核对"，不提供编辑入口：

- `GET /resources/skills/{id}/files`：文件树列表（路径、大小、类型），列表页点开能看。
- `GET /resources/skills/{id}/files/{path}`：单文件内容/下载，服务端从 OSS 流式代理。
- 没有 `PUT`。要改内容，重新上传一个新 zip 作为新 `version`。

### 数据库改动

不改 `skills` 表结构（`config` JSONB 里存 `{entry: "SKILL.md", oss_prefix, total_size}` 这类元信息即可），新增一张索引表：

```sql
CREATE TABLE skill_files (
    id           BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    skill_id     BIGINT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    owner_id     BIGINT NOT NULL REFERENCES users(id),
    path         VARCHAR(500) NOT NULL,
    size_bytes   BIGINT NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (skill_id, path)
);
```

### 运行时

`buildSkillTool`（`internal/orchestrator/adk/tools.go`）现在直接读 `config.instructions`；改为：`config` 里有 `oss_prefix` 时，按需从 OSS 拉 `{oss_prefix}/{entry}` 的内容（加一层进程内缓存，TTL 5 分钟，避免每次 tool 调用都打一次 OSS）；没有 `oss_prefix`（旧的纯文本 Skill）继续走 `config.instructions`，两条路径并存。

### Skill 源：系统设置里配置同步源，同步下来的都是"系统提供"

新诉求：除了用户自己上传，系统设置要能配置若干个"Skill 源"（比如 clawhub.ai 这类 Skill 目录/市场），从源同步 Skill 下来——同步来的 Skill 统一标记"系统提供"，不是哪个用户私有的。

这跟 spec-59～65 已经落地的**模型目录**（`modelcatalog`：系统配置里管理员登记 Provider/Model，所有人在模型广场只读浏览）是同一个模式，直接照搬：

- **系统设置新增二级菜单"Skill 源"**（`/settings/skill-sources`，仿照"模型提供商"页面），管理员在这里登记同步源：`name`（展示名，如 "ClawHub"）、`sync_type`（同步协议的判别字段，先支持一种约定好的 HTTP 目录接口，`clawhub` 是第一个具体实现）、`endpoint`、可选 `api_key`（加密存储）。点"立即同步"按钮触发一次同步，不做后台定时任务——用得上自动化了再加 cron，本轮手动触发够用。
- 新表 `skill_sources`（id, name, sync_type, config JSONB, last_synced_at, last_sync_status, status, created_at）。
- 同步落地到**一张独立的全局目录表 `system_skills`**（不进用户 owner 的 `skills` 表——它是全局的，没有 owner），字段跟 `skills` 类似但多一个 `source_id` 外键：`id, source_id, ref, version, display_name, config(oss_prefix/entry), status, synced_at`。存储复用同一个 OSS，对象 key 前缀换成 `skills/system/{source_id}/{ref}/{version}/`。
- 后端加一个判别接口 `SkillSourceSyncer`（`Sync(ctx, source SkillSource) ([]SyncedSkill, error)`），每种 `sync_type` 一份实现（`internal/adapter/skillsource/clawhub` 是第一个）——这样以后接第二个源不用改同步流程的主干逻辑，只加一个新适配器。
- **展示与引用**：应用广场的 Skill 列表页，用户自己上传的和"系统提供"的混合展示，系统提供的打一个角标、禁用编辑/停用/删除（生命周期只在系统设置里管）。Agent 的 `capabilities.skills[]` 解析（`internal/api` 里的 `ResourceAuthorizer`）从"只查 owner 的 skills 表"扩展为"先查 owner 自己的，查不到再查 `system_skills` 目录"——两段查找，同名时 owner 自己的优先（允许用户用同名 ref 覆盖系统提供的版本，语义更直觉）。

---

## 三、OSS 接入（阿里云）

- 新增端口：`internal/domain/resource` 里定义 `ObjectStore` 接口（`Put(ctx, key, reader, contentType) error`、`Get(ctx, key) (io.ReadCloser, error)`、`Delete(ctx, key) error`），Skill 的 Service 依赖这个接口而不是具体 SDK。
- 新增适配器 `internal/adapter/oss`，用官方 `github.com/aliyun/aliyun-oss-go-sdk/oss` 实现上面的接口，不做多云抽象、不接 MinIO 兼容层——就是阿里云 OSS 一家，YAGNI。
- 配置项（`internal/config`）新增 `OSS_ENDPOINT` / `OSS_BUCKET` / `OSS_ACCESS_KEY_ID` / `OSS_ACCESS_KEY_SECRET`（用户会把 AK/SK 填进 `.env`，直接对接真实 SDK，不做本地磁盘 mock）。
- 比照 `KB_ENABLED` 的先例：这几个变量未配置时，Skill 的"上传 zip / 在线编辑"入口在前端置灰并提示"管理员未配置对象存储"，纯文本 Skill（走 `config.instructions` 旧路径）不受影响——不强制所有部署都必须配 OSS。

---

## 四、菜单改名"组件"：Tool 只是组件的一种类型，插件/沙箱环境是预留的其他类型

**改口**：侧栏菜单 "Tool" 改名"组件"。"组件"是一个更大的伞：Tool、插件（Plugin）、沙箱环境（Sandbox）……都是"组件"下不同的类型，差别只在类型，不是各自独立的资源门类。插件体系本身**本轮不做**——用户明确说了"这是把目前业务梳理清楚后再做的事情"，这里只做两件事：(1) 菜单/产品命名现在就改，(2) 数据结构上现在就留出"类型"判别位，不要现在把 Tool 焊死成唯一形态，将来插件/沙箱环境落地时是加一个新的 `component_type` 取值，不是另起一张表、另设计一遍。

### 判别字段设计：`component_type`（大类）+ `tool_type`（Tool 内部的子形态）

两层判别，不是一层：

- `config.component_type`：`"tool"`（本轮唯一实现的取值）｜ `"plugin"`（预留，不做）｜ `"sandbox"`（预留，不做）。菜单/列表页按这个字段分组展示。
- 当 `component_type == "tool"` 时，才有第二层 `config.tool_type` 区分 Tool 自己的形态：`"http"`（现状，单接口）｜ `"openapi"`（导入一批 operation）。

这样以后插件/沙箱环境落地时，各自会有自己的一套 `config` 形状（插件大概率需要"托管代码执行"，沙箱环境大概率需要"隔离运行环境的生命周期管理"），但都挂在同一张 `tools` 表、同一个 `component` Kind 下，靠 `component_type` 分流——不需要因为新增一个组件类型就新建一张表、新加一个 Resource Kind、新加一整套 CRUD 接口。**这是本轮唯一要现在做的改动**：把 Tool 相关的表/字段命名和判别逻辑按"组件"的终态设计，但只实现 `component_type=tool` 这一支。

### Tool 自己的形态扩展：OpenAPI 导入（收益最大，优先做）

流程是"预览 → 勾选 → 批量创建"三步，不是一次表单提交：

1. 用户贴 OpenAPI spec 的 URL，或直接粘贴/上传 YAML/JSON 内容。
2. `POST /resources/components/import-openapi`（预览，不落库）：服务端解析 spec（用 `github.com/pb33f/libopenapi` 或 `github.com/getkin/kin-openapi`），返回每个 operation 的 `{operation_id, method, path, summary}` 列表。
3. 用户勾选要开放给 Agent 的 operation。
4. `POST /resources/components/batch`（批量创建，单事务）：为每个被选中的 operation 各建一行 `tools` 记录，`ref` 形如 `{base_ref}__{operation_id}`，`config` 存 `{component_type: "tool", tool_type: "openapi", method, path, base_url, import_group: "<导入批次标识>"}`。

**每个 operation 各自是一个独立的 `tools` 行**，不是一个 tool 资源背后塞多个 operation——这样 `capabilities.tools[]` 的"一个 ref 一个 tool"心智模型完全不用变，Bundle/Agent 层不用感知这个新形态的存在。`import_group` 字段只是方便以后"整批查看/整批禁用来自同一个 spec 的 tool"，不参与运行时逻辑。

### 运行时改动

`buildEndpointTool` 按 `config.tool_type` 分支：`"openapi"` 时用 `config.method/path/base_url` 拼实际请求（而不是现在写死的"POST 到 endpoint、body 透传"）；没有 `tool_type`（旧数据，视为 `http`）继续走现在的行为。这是本轮唯一必须碰运行时编译代码的第二处（第一处是 MCP 探测）。`component_type` 本轮不影响运行时分支（只有 tool 一种取值），纯粹是为将来预留的字段。

### 前端

菜单标签 `Tool` → `组件`（内部路由 value 保持 `tool` 不变，跟这次 Bundle/Agent 改名同一个做法——只换展示文案，不动 URL）。`/apps/tool/new` 改成两步：Step 1 卡片选类型（本轮只有一张"Tool"卡片可点，插件/沙箱环境卡片可以先放上去打成"即将支持"的禁用态，提前让用户看到路线图），选完 Tool 后 Step 2 再选形态（`http` 手填单接口 / `openapi` 导入一批）进对应表单。

### 本轮不做，但写清楚以后往哪走

- **插件体系**（`component_type = "plugin"`）：用户明确说了"业务梳理清楚后再做"，本轮只留判别字段，不设计具体形态。
- **沙箱环境**（`component_type = "sandbox"`）：同上，本轮不做。
- Tool 自己的 `code`（用户写一段脚本在沙箱里执行）：这个未来可能会和"沙箱环境"这个组件类型是同一件事的两个入口，等沙箱环境立项时一起想清楚，不单独作为 Tool 的第三种 `tool_type` 抢跑。

---

## 五、记忆库：轻量升级，不引入新概念

记忆库的 `config` 目前基本没有被运行时读取任何字段（`load_memory`/`preload_memory` 内置工具读的是全局按 owner 路由的 `MemoryService`，不是某个记忆库资源自己的 config）——它本质上只是一个"存在性标记"。这块不需要 OSS/在线编辑这种重量级能力，只需要从"通用弹窗"升级成独立的轻量表单页 `/apps/memory/new`（`ref` / `display_name` / 一两个未来可能用到的命名字段），跟 MCP 的路由模式一致，但表单本身很短。本轮范围到此为止，不新增记忆库相关的运行时概念。

---

## 六、API 契约新增一览

| 方法 + 路径 | 用途 | 落库？ |
|---|---|---|
| `POST /resources/mcp/probe` | 探测 MCP Server，返回工具列表 | 否 |
| `POST /resources/components/import-openapi` | 解析 OpenAPI spec，返回 operation 列表 | 否 |
| `POST /resources/components/batch` | 批量创建勾选的 operation 对应的 tools | 是（单事务） |
| `POST /resources/skills/upload` | 上传 zip，解压+建文件树+建资源，一步到位 | 是 |
| `GET /resources/skills/{id}/files` | 文件树列表（查看/下载，不可编辑） | 否 |
| `GET /resources/skills/{id}/files/{path}` | 单文件内容（OSS 代理，下载用） | 否 |
| `POST /settings/skill-sources` | 管理员登记一个 Skill 源 | 是 |
| `POST /settings/skill-sources/{id}/sync` | 触发一次同步 | 是（写入 `system_skills`） |
| `GET /settings/skill-sources` | 列出已登记的 Skill 源 | 否 |

现有 `POST /resources`（单条创建）保持不变，继续覆盖 `http` 型 Tool（组件）、记忆库、以及"不上传 zip 的纯文本 Skill"这几种最简单的场景。

## 七、数据库改动一览

- 新表 `skill_files`（Skill zip 里的文件树索引，只读展示用）。
- 新表 `skill_sources`（管理员登记的 Skill 同步源）。
- 新表 `system_skills`（源同步下来的全局 Skill 目录，无 owner，`source_id` 外键指回 `skill_sources`）。
- 其余四张资源表（`tools`/`skills`/`mcp_servers`/`knowledge_bases`）不加列，新字段都放进已有的 `config` JSONB——跟 spec-05"分表但字段收在 config 里"的既有风格一致；`tools` 表新增的 `component_type`/`tool_type` 判别也走这条路，不加列。

## 八、前端信息架构一览

延续这次 Bundle 编辑器立的规矩：字段复杂的资源类型用独立路由页面（嵌套在 `/apps` 下、侧栏常驻、有返回按钮），不再塞进 Dialog：

- 侧栏菜单 "Tool" → **"组件"**（内部路由 value 仍是 `tool`，只换展示文案）
- `/apps/tool/new`——两步（选组件类型，本轮只有 Tool 可选 → 选 Tool 形态 http/openapi → 对应表单/导入预览）
- `/apps/skill/new`——只有"上传 zip"一种入口，没有在线编辑
- `/apps/mcp/new`——表单 + Header 列表 + 检测按钮（实时拉工具列表）
- `/apps/memory/new`——轻量表单
- 知识库暂不在本轮改动范围内（配置项相对简单，且依赖 Milvus/Elasticsearch 是否启用，维持现状）
- 系统设置新增 `/settings/skill-sources`——仿照"模型提供商"页面的管理员配置页
- `RegisterResourceDialog` 弹窗**保留组件**，作为知识库等字段确实简单的 kind 的兜底，不强求五种资源全部路由化；具体每种是否退场到实现阶段按字段数量再定。

## 九、分期计划

1. **Phase 1 —— MCP**：二级页面 + Header 列表（逐项加密）+ 真实 MCP 探测/拉工具列表，替换掉现在的裸 HTTP GET 健康检查。诉求最明确、改动面最小、不依赖 OSS，优先做。
2. **Phase 2 —— OSS + Skill 上传**：`internal/adapter/oss` 接入真实阿里云 OSS（等 AK/SK 配好 `.env`）、zip 上传/解压/建索引（只读文件树，无在线编辑）、运行时按需拉取 `SKILL.md`。
3. **Phase 2.5 —— Skill 源同步**：系统设置"Skill 源"页 + `skill_sources`/`system_skills` 表 + `SkillSourceSyncer` 接口 + clawhub 适配器 + Agent 引用解析扩展到系统目录。依赖 Phase 2 的 OSS 基础设施，紧跟其后。
4. **Phase 3 —— 组件/Tool 多形态**：菜单改名"组件"+ `component_type`/`tool_type` 判别字段落地、OpenAPI 导入（预览→勾选→批量创建）+ 运行时按 `tool_type` 分支请求逻辑。插件/沙箱环境两个组件类型只留判别位，不实现。
5. **Phase 4 —— 记忆库表单化**（体量最小，顺手做）。

## 验收清单（草案，实现阶段按 Phase 拆分为独立任务时再细化）

- [ ] MCP 检测按钮返回真实工具列表，不是"能不能连上"
- [ ] MCP Header 的 value 在任何 GET 响应里都不出现，加密落库
- [ ] Skill 上传 zip 后能在文件树里看到全部文件、能下载核对，没有编辑入口
- [ ] 未配置 OSS 时，Skill 的纯文本旧路径（`config.instructions`）不受影响，只有"上传"入口置灰
- [ ] 管理员在系统设置登记一个 Skill 源并同步后，应用广场的 Skill 列表能看到"系统提供"角标的条目，普通用户看不到编辑/删除按钮
- [ ] Agent 引用一个只存在于 `system_skills` 而不在自己 `skills` 表里的 ref 时，编译期能正确解析
- [ ] OpenAPI 导入后，Agent 的 `capabilities.tools[]` 引用其中任一 operation 的 ref 时行为和手填的 `http` 型 tool 完全一致（对 Agent 层透明）
- [ ] 侧栏菜单显示"组件"而不是"Tool"，路由 `/apps/tool` 不变
