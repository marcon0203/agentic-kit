# spec-20 插件体系

> 状态：**设计稿 v2，五个确认项已拍板**。spec-05a §4 里"插件只留判别位、业务梳理清楚后再做"的那件事，现在开始做。
> 本文只定方案，不含实现。带 `【待确认】` 的地方是还没拍板、需要继续谈的，不是我装作确定。
>
> **v1 → v2 变化一览**（对照 v1 末尾的五个确认项）：
> 1. 热重载：不做 Cordis 式 fiber 树，但**加一个可配置的版本解析时机**（§2.1）。
> 2. 第三方上传：**v1 就开放**，同时给出自动化审核门槛 + 私有/公开两级可见性（§5.3）。
> 3. `hooks`：**v1 就做**，且直接接管 Agent DSL 里已经存在但从未被运行时读取的 `capabilities.hooks` 五个字段（§3.2、§4.4）。
> 4. `connectors` 写操作：**建连接时按开关**，用户自己决定这个连接允许不允许写（§4.3）。
> 5. 上架审核与签名：**发布必须签名，审核走自动化门槛 + 人工复核队列**（§5.3）。
>
> 另外回应你提的两个前瞻建议：模型 Provider 作为插件的可能性（§3.3，只留位置不实现），以及 K8s 部署下的适配（§九）。

---

## 一、系统概览

### 定位

插件是**第三方可以写、我们不用改代码就能装上、装上之后能改变智能体运行时行为**的扩展单元。

和已有的"组件"划清界限——这是本设计最重要的一条线：

| | 组件（已实现） | 插件（本文） |
|---|---|---|
| 是什么 | 一条外部能力的**配置** | 一段第三方**代码** |
| 谁写的 | 用户填表单 | 开发者写并编译 |
| 平台执行什么 | 按配置发 HTTP 请求 | 执行别人的代码 |
| 出问题的后果 | 请求失败 | 可能跑挂/跑穿平台 |
| 扩展点数量 | 一个（被模型调用） | 多个（工具 / 渲染 / 连接器 / 钩子） |

一句话：**组件是数据，插件是代码。** 只要接受了这条，后面所有的安全边界和运行时设计都是它的推论。

### 核心挑战

1. **执行第三方代码而不被它搞死**。生态意味着代码不可信。必须沙箱、必须有超时和内存上限、必须没有默认的网络与文件系统权限。
2. **插件要能影响"输出的呈现"，而不只是"能调用的工具"**。你举的图表插件就是这一类：它不是被模型调用的工具，是挂上去之后**智能体的输出被它接管渲染**。这要求插件有前端运行时态，而前端运行时态同样是不可信代码。
3. **规范要能撑住生态，而不是撑住第一个插件**。清单格式、版本协商、权限声明要按"以后会有几百个插件"来定，加扩展点应该是加一个枚举值，不是推倒重来——`extensions` 字段允许未知类型透传就是这一条的直接体现（§3.3）。

### 设计原则

- **能力靠声明，不靠约定**。插件在清单里声明要什么（网络域名、宿主服务、扩展点），宿主按声明授权。没声明的一律没有。这是 Cordis `inject` 的思路，也是我们 `capabilities` 白名单已经在用的思路。
- **谁创建谁回收**。一次运行里起的所有沙箱实例、连接、iframe，运行结束必须全部回收。这是 Cordis "effect 归属于创建它的 fiber" 的思路。
- **不造轮子，但也不硬套**。沙箱、渲染协议都用现成的；Cordis 那套进程内 fiber 树不搬（下面 §2 说明为什么）。

---

## 二、参考实现调研

你点名了 Cordis，我去查了它现在的实际状态，以及 Go 侧有没有能直接用的东西。

### 2.1 Cordis 是什么，能借什么

Cordis 是 Koishi 用了四年的插件内核，现在也是 DeepSeek Harness（dsh）的插件内核，有一篇 *A Programming Paradigm for Spatiotemporal Composability* 的预印本。它的三个核心机制：

- **时间可组合**：`ctx.effect` 注册的副作用在卸载时**完全回滚**。热重载因此是特性而不是崩溃——卸旧版（所有逆操作执行）、装新版。
- **空间可组合**：插件声明 coeffect（`export const inject = ['tools']`），运行时在上下文满足/不再满足时通知它。插件在依赖齐全前处于 Pending；provider 被替换会推进 dependency epoch，消费者先卸载、条件满足时再启动。
- **作用域 DI**：不同分支可以解析到同一服务的不同实现。

**已有移植**：Rust（`dshbox/cordis-rs`）、Python（`ddebowczyk/cordis-python`）。**Go 没有移植**——我查过了，Awesome Go 的 DI 那一栏里也没有等价物。

**所以"不造轮子"在这里的准确落法是**：借它的三个概念（声明式依赖、disposer 归属、服务注册表），不移植它的内核。原因不是懒，是我们的插件和 Cordis 的插件根本不是一种东西：

- Cordis 的插件是**同进程 TS 模块**，热重载靠的是 JS 能重新求值一个模块。
- 我们的插件是**数据库里的一条记录 + 一个沙箱实例**。"热重载"对我们等于"下次运行时装新版本"，不需要 fiber 树，也不需要在一个 Go 进程里做模块级卸载（Go 本来也做不到）。

硬把 Cordis 的 fiber/effect 树搬进 Go，会得到一个没有热重载需求的热重载框架——所以内核不搬。但**你要的"服务不重启就能生效"是另一件事，不需要 fiber 树也能做到**，而且我们已经具备大半：Skill 那轮做的"OSS 内容 + 5 分钟 TTL 缓存"本身就是一种热重载——服务器从没重启过，缓存过期后自然读到新内容。

插件比 Skill 复杂的地方在于**一次运行可能跨越很长时间**（graph/flow 类型的 Bundle，一个节点等另一个节点，中间可能有人工审批网关卡几分钟到几小时）。这里真正需要拍板的不是"要不要热重载"，而是**版本在什么边界上被锁定**：

| 解析时机 | 行为 | 适合 |
|---|---|---|
| **run 级锁定**（默认，先做这个） | 整次运行开始时解析一次 `plugin_installations` 当前指向的版本，run 内所有节点用同一个版本，哪怕运行途中管理员升级了插件 | 一致性优先——同一次运行不该出现"前半段用 v1 后半段用 v2"的诡异行为 |
| **node 级解析**（配置化开关，后做） | 每个节点执行到它、要用某个插件时才解析当时最新版本 | 长流程 + 想让"刚上线的修复版本"立刻影响还没跑到的节点 |

`plugin_installations` 加一个 `resolution` 字段（`pinned` \| `live`，默认 `pinned`），管理员可以按插件自己选。P1 只实现 `pinned`，`live` 留作后续——语义已经在表里定义好了，加实现不需要改 schema。

### 2.2 后端沙箱：选 Extism（wazero）

| 方案 | 沙箱 | 多语言 | 成本 | 判断 |
|---|---|---|---|---|
| **Extism / wazero** | ✅ WASM | ✅ 任意能编到 wasm 的语言 | 编译缓存后实例化很快 | **选它** |
| hashicorp/go-plugin | ❌ 子进程，权限同宿主 | ✅ | 每次调用 ~30-50μs，运维要管进程 | 不选：无沙箱，生态不可信代码不能用 |
| knqyf263/go-plugin | ✅ WASM | 用 protobuf 定义 | 内存内通信不走 RPC | 备选，但生态和文档不如 Extism |
| yaegi（Go 解释器） | ❌ | ❌ 只能 Go | 与编译版行为有差异（struct tag、反射、JSON 边界情况），不能用任何依赖 CGO 的库 | 不选 |
| Go 原生 `plugin` | ❌ | ❌ | 版本必须完全一致，实际不可用 | 不选 |

Extism 给的现成能力正好是我们要一条条自己写的：host function、`AllowedHosts`（受控 HTTP）、`AllowedPaths`、超时与内存限制、`NewCompiledPlugin` 编译一次多实例复用。底层是 wazero——**纯 Go、无 CGO**，和我们现在的部署方式不冲突。

插件作者写 Go 的话用 `extism/go-pdk` + TinyGo 编译。

### 2.3 前端渲染：直接采用 MCP Apps

这一条是本次调研最值钱的发现：**"工具返回一段 UI、宿主渲染它"这件事已经有标准了，而且就在 MCP 里**。

MCP Apps 已于 2026 年 1 月正式并入 MCP：工具声明一个 UI resource，宿主把它渲染在**沙箱 iframe** 里，iframe 与宿主之间走 **JSON-RPC over postMessage**。OpenAI 的 Apps SDK 是同一套东西（`window.openai` bridge + 三层 iframe 做跨源隔离）。

我们**已经是 MCP 客户端**（spec-05a 那轮做的真实握手）。让图表插件按 MCP Apps 出 UI，等于：

- 不用发明我们自己的前端插件格式
- 第三方为 ChatGPT / 其他 MCP 宿主写的 UI 插件，理论上能直接跑在我们这里
- 安全模型是别人验证过的：iframe sandbox + 可审计的 JSON-RPC + 宿主可要求用户确认

**不选**的两个方案，说明理由：动态 `import()` 加载第三方 ESM（等于把第三方代码放进我们的主 origin，XSS 即全站沦陷）；Module Federation（同上，且构建期耦合）。

---

## 三、插件模型

### 3.1 一个插件 = 一份清单 + 若干扩展点

```jsonc
// plugin.json —— 插件包根目录，这是"咱们系统标准的规范"的核心
{
  "manifest_version": 1,              // 规范版本，宿主据此决定怎么解析后面的字段
  "id": "acme.charts",                // 全局唯一，反向域名式，发布后不可改
  "version": "1.2.0",                 // 语义化版本
  "display_name": "图表渲染",
  "description": "把模型输出里的图表描述渲染成可交互图表",
  "author": { "name": "ACME", "url": "https://acme.example" },
  "license": "MIT",
  "icon": "assets/icon.svg",

  // 依赖声明 —— Cordis inject 的思路：要什么写清楚，宿主按这个授权
  "requires": {
    "host_api": ">=1.0 <2.0",         // 宿主 API 版本区间，不兼容就不装载
    "services": ["sql"],              // 需要宿主提供的服务（见 §4.3）
    "network": ["api.acme.example"],  // 允许访问的域名白名单，其余一律拒绝
    "permissions": ["read:run_output"] // 细粒度权限，用户安装时可见
  },

  // 扩展点 —— 一个插件可以同时是工具、渲染器、连接器
  "extensions": {
    "tools": [
      {
        "name": "render_chart",
        "description": "把一组数据渲染成图表",
        "input_schema": { "type": "object", "properties": { /* JSON Schema */ } },
        "entry": "plugin.wasm#render_chart",   // WASM 模块里的导出函数
        "ui": "ui/chart.html"                  // 可选：返回 UI resource（MCP Apps）
      }
    ],
    "renderers": [
      {
        "name": "chart",
        "entry": "ui/chart.html",
        // 自动接管：命中就渲染，不需要模型显式调用（见 §4.2）
        "auto_render": { "fenced_lang": ["chart", "vega-lite"] }
      }
    ],
    "connectors": [
      {
        "name": "clickhouse",
        "kind": "sql",
        "entry": "plugin.wasm#dialect",   // 只做方言/schema 映射，连接由宿主持有
        "config_schema": { /* 建连接时要用户填什么，含下面的 allow_write */ }
      }
    ],
    "hooks": [
      {
        // 直接对应 Agent DSL 里已经存在的 capabilities.hooks.before_tool_call
        // 等四个字段（见 §3.2）——不是新发明的钩子点，是把一直没实现的坑填上。
        "point": "before_tool_call",
        "entry": "plugin.wasm#redact_pii"
      }
    ]
  }
}
```

打包成 `.akp`（zip），结构：

```
plugin.json
plugin.wasm          # 后端扩展点的实现，可选
ui/                  # 前端扩展点的实现（纯静态 HTML/JS），可选
assets/
README.md
```

复用已经建好的东西：zip 上传 + OSS 存储 + 文件树索引在 Skill 那轮已经做了（`skill_files`、`internal/adapter/oss`），插件包走同一条路，不再新建一套。

### 3.2 四个扩展点，各自解决什么

| 扩展点 | 运行在哪 | 谁触发 | 典型例子 |
|---|---|---|---|
| `tools` | 后端 WASM 沙箱 | 模型决定调用 | 计算、格式转换、调用第三方 API |
| `renderers` | 前端沙箱 iframe | 输出命中规则，或工具返回 UI resource | **你说的图表插件** |
| `connectors` | 后端 WASM + 宿主服务 | 模型调用/知识库检索 | **你说的数据库连接器** |
| `hooks` | 后端 WASM 沙箱 | 运行生命周期事件 | 输出脱敏、审计、内容过滤 |

`hooks` 做，且**不新开一个坑**：Agent DSL（`schemas/agent.schema.json`、`capabilities.hooks`）里早就有 `before_tool_call`/`after_tool_call`/`before_response`/`after_response`/`on_error` 五个字段，前端 `AgentForm`/`AgentStudioPage` 也一直在填这些字段——但我确认过，`internal/orchestrator/adk` 的编译器**从未读取过它们**，纯粹是个填了没用的坑。插件的 `hooks` 扩展点就是把这五个点接上：字段里填的不再是自由文本，而是 `plugin:{id}/{point}` 这样的插件 ref。

范围限制在权限最大之处：**`hooks` 能看到、能改写的东西必须显式声明**（`requires.permissions` 里要有 `read:tool_input`/`write:response` 这类），装插件时用户看得到"这个插件会读/改什么"；执行超时比 `tools` 更紧（**5s**，因为它卡在每一步的关键路径上，不是异步调用）；一个 hook 点上**同名冲突**（两个插件都想接管 `before_response`）在编译期直接拒绝，不猜谁赢——这和 `renderers` 的"先声明者赢"是两种冲突，`hooks` 改写的是正文而不是附加渲染，模糊掉赢家比 `renderers` 危险得多。

### 3.3 给"模型 Provider 作为插件"留位置，但不在这轮做

你提到以后可能把大模型厂商对接也做成插件。现在的实现是 `internal/modelgateway` 里的一个 Go 原生注册表（`ProviderDefinition{NewClient, NewValidator, Pricing}`，spec-09 那轮从"五处 switch"收敛成的"加一条注册表项"）。把它变成插件维度，形状上是自洽的——不需要推翻现有设计：

- `Client.Complete(ctx, apiKey, baseURL, model, req) (CompletionResult, error)` 本身就是**请求进、结果出**的形状，和 `connectors` 走的"插件只管方言转换，网络调用由宿主 host function 代劳"是同一个模式：插件把 `CompletionRequest` 翻译成该厂商的 HTTP 请求形状，调宿主的 `http.call`（域名仍然要在 `requires.network` 白名单里），厂商的响应再由插件翻译回 `CompletionResult`。
- 这样"模型 Provider 插件"不需要一个新的信任边界，是 `connectors` 的一个变体：`extensions.providers[]`，`kind: "model"`。

**这轮不实现**，原因是两个真实的技术未知数，值得单独一轮想清楚而不是现在猜：
1. **流式**。ADK 这边模型输出是要流式吐字的，现在 `Extism.Call` 是一次性调用/一次性返回，没有"边算边回调宿主"的机制——这个 gap 需要 Extism 那边支持分块回调，或者我们在 host function 层面自己搭一个流式接口，属于要单独验证的事。
2. **凭证与限速**。Provider 级别的凭证轮换、限速、重试退避，现在是 `modelgateway.Gateway` 里手写的，搬进插件之前要想清楚这层信任边界搬不搬得动。

`plugin.json` 的 `extensions` 字段允许出现未知的扩展点类型（宿主看不懂的类型直接忽略，不是报错拒绝整个清单）——这一条现在就该定，是"以后加扩展点是加枚举值不是推倒重来"这句话的具体落地，`providers` 将来落地时不需要老插件全部重新打包。

---

## 四、运行时设计

### 4.1 tools：和现有 compileTools 同一条路

编译期已经有的分支逻辑（`internal/orchestrator/adk/agent_compiler.go`）再加一支：

```
capabilities.tools[] 里的 ref
  → Authorize（已有：未授权的不进图）
  → Kind == mcp        → BuildMCPToolset      （已有）
  → component_type == sandbox → BuildSandboxTools（已有）
  → component_type == plugin  → BuildPluginTools （新增）
  → 其余              → BuildTool             （已有）
```

`BuildPluginTools(spec)` 做的事：从 OSS 拉 `plugin.wasm`（走 Skill 那轮已有的 5 分钟 TTL 缓存）→ `extism.NewCompiledPlugin` 编译一次 → 按清单里每个 tool 生成一个 ADK `functiontool` → 调用时 `instance.Call(entry, inputJSON)`。

沙箱参数硬上限（**不接受插件清单里调大**）：

| 限制 | 值 | 理由 |
|---|---|---|
| 单次调用超时 | 30s | 与现有 `toolCallTimeout` 一致 |
| 内存上限 | 128 MiB | 【待确认】没有实测依据，先按经验值定 |
| 网络 | 仅 `requires.network` 白名单内的域名 | Extism `AllowedHosts` |
| 文件系统 | 无 | 不给 `AllowedPaths` |
| 实例生命周期 | 一次 run 内复用，run 结束销毁 | 与 sandbox 组件同一套"谁创建谁回收" |

### 4.2 renderers：图表插件到底怎么"自动渲染"

你的原话是"给某个智能体分配了这个插件，这个智能体在输出的时候自动用这个图表插件渲染"。这里有两种触发方式，我建议**两个都要，主推第一个**：

**方式 A：模型显式调用（推荐做主路径）**

模型调用 `render_chart` 工具 → 插件返回一个 UI resource → 宿主把它作为一条新的运行事件推给前端 → 前端渲染 iframe。

这是 MCP Apps 的原生语义。好处：模型知道自己在画图，可以决定画什么；链路可审计；和别的 MCP 宿主兼容。

**方式 B：输出自动接管（补你说的那个体验）**

插件在 `renderers[].auto_render` 里声明匹配规则（例如"输出里出现 \`\`\`chart 代码块"）。宿主在节点输出完成时跑匹配，命中就发渲染事件。

好处：模型不需要知道有这个插件，普通的 markdown 输出也能被美化。代价：匹配规则是插件自己声明的，两个插件可能抢同一个 fenced lang——**冲突时按智能体 `capabilities.tools[]` 里的声明顺序，先声明的赢**，并在运行事件里记录谁接管了。

**事件流改动**（这是唯一需要动 spec-12 的地方）：新增一个事件类型

```jsonc
{
  "type": "node.render",
  "payload": {
    "node": "analyst",
    "plugin": "acme.charts",
    "renderer": "chart",
    "resource_uri": "ui://acme.charts/chart",  // 前端据此取 iframe 内容
    "data": { /* 传给 iframe 的数据 */ }
  }
}
```

前端 `buildTimeline`（`web/src/lib/runs/timeline.ts`）加一种 entry，渲染成一个 iframe 卡片。iframe 属性固定为 `sandbox="allow-scripts"`、**不加 `allow-same-origin`**，内容从独立的资源域下发，与主站不同源。

### 4.3 connectors：宿主持连接，插件只做方言

这是 WASM 沙箱下唯一安全、也是唯一可行的做法——**WASM 插件打不开 TCP 连接**，数据库访问必须由宿主中介。这不是我们的设计偏好，是沙箱的物理约束。

```
用户在「组件」里注册一个 clickhouse 连接，勾选"允许写"（默认不勾）
  → 凭证走已有的 config 加密（credentialKeySubstrings 已覆盖 password/token 等）
  → allow_write 这个开关和凭证一起存在这条组件资源的 config 里
  → 宿主建连接池，给它一个 connection_ref（连接池按 allow_write 状态各建各的：
     一个只读连接不会因为运行时判断失误而被拿去写）

模型要查数 / 写数
  → 调插件的 tool
  → 插件在沙箱里调宿主 host function：
       sql.query(connection_ref, sql, args)    — 任何连接都能调
       sql.execute(connection_ref, sql, args)  — 仅 allow_write=true 的连接放行，
                                                  否则 host function 直接拒绝，
                                                  插件代码里怎么拼 SQL 都没用
     ↑ 插件拿到的是 connection_ref，永远拿不到 DSN 和密码
  → 宿主校验 connection_ref 属于本次 run 的 owner、执行、把结果回给插件
  → 插件把结果整理成模型好读的形状返回
```

插件负责的是"这个数据库的方言怎么写、schema 怎么描述给模型"，不负责"怎么连上去"、也不负责"能不能写"——**读写权限的判断点在宿主的 host function 里，不在插件代码里**，插件即便是恶意的也翻不出这堵墙。

宿主服务（host functions）第一版提供：

| 服务 | 函数 | 说明 |
|---|---|---|
| `sql` | `query(conn_ref, sql, args) → rows` | 任何连接都能调 |
| `sql` | `execute(conn_ref, sql, args) → affected_rows` | 仅 `allow_write=true` 的连接放行 |
| `sql` | `schema(conn_ref) → tables` | 让模型知道有哪些表 |
| `log` | `log(level, msg)` | 进运行事件流，便于调试 |
| `kv` | `get/set(key, value)` | 插件自己的小状态，按 (plugin, owner) 隔离 |

### 4.4 hooks：接管早就声明过、却从没实现的钩子

`internal/orchestrator/adk/agent_compiler.go` 编译一个 Agent 时，`capabilities.hooks` 里的五个字段现在直接被忽略。插件的 `hooks` 扩展点把它们接上：

```
capabilities.hooks.before_tool_call = ["plugin:acme.pii/redact_pii"]
  → 编译期解析成插件 ref，和 tools/skills 走同一条 Authorize 检查
  → 运行期，ADK 每次要调工具之前，先把 (工具名, 入参) 丢给这个 hook
  → hook 返回改写后的入参（或者"拒绝调用"），再继续原来的流程
```

五个点里 `before_tool_call`/`after_tool_call` 挂在工具调用前后，`before_response`/`after_response` 挂在一个节点产出正文前后，`on_error` 挂在任何一步抛错时。**改写权限按点分开声明**——`before_response` 想改写正文必须在 `requires.permissions` 里写明 `write:response`，只声明 `read:response` 的话运行时把它的返回值当只读日志，不接受改写，装插件时用户能看清楚这条区别。

超时 5s（比 `tools` 的 30s 紧得多），因为它挡在关键路径上，超时直接跳过这个 hook（记一条运行事件，run 本身不失败）——一个装错的钩子插件不该拖垮整次运行。

### 4.5 生命周期：谁创建谁回收

```
一次 run 开始
 ├─ 编译期：解析 agent 引用的插件 → 校验 requires（版本/服务/权限）
 │           缺依赖 → 这个插件不进图（沿用"未授权的资源不进图"的既有规则）
 ├─ 运行期：WASM 实例、DB 连接、iframe 会话，全部登记到这次 run 名下
 └─ run 结束 / 取消 / 超时 → 全部销毁
```

Cordis 的 disposer 归属，落到 Go 这边就是一个 per-run 的 `pluginScope`，`Close()` 时倒序回收。不需要框架。

---

## 五、数据模型与 API

### 5.1 表

沿用 spec-05a "不加列、字段收在 config 里"的既定风格，但插件需要两张新表——因为它有版本和安装关系，这是配置类资源没有的：

```sql
-- 插件包本体（一个 id 多个版本）
CREATE TABLE plugins (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    plugin_id       VARCHAR(128) NOT NULL,        -- acme.charts
    version         VARCHAR(32)  NOT NULL,
    manifest        JSONB        NOT NULL,        -- plugin.json 原文
    oss_prefix      TEXT         NOT NULL,        -- 包内容在 OSS 的位置
    publisher_id    BIGINT       REFERENCES users(id),  -- NULL = 平台内置
    signature       TEXT         NOT NULL,        -- 见 §5.3，发布时强制签名
    visibility      VARCHAR(16)  NOT NULL DEFAULT 'private', -- private | public，见 §5.3
    review_status   VARCHAR(16)  NOT NULL DEFAULT 'pending', -- pending | passed | rejected，见 §5.3
    status          SMALLINT     NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (plugin_id, version)
);

-- 谁装了哪个版本
CREATE TABLE plugin_installations (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT       NOT NULL REFERENCES users(id),
    plugin_id       VARCHAR(128) NOT NULL,
    version         VARCHAR(32)  NOT NULL,        -- 装的时候锁定的版本
    resolution      VARCHAR(8)   NOT NULL DEFAULT 'pinned', -- pinned | live，见 §2.1；P1 只实现 pinned
    config          JSONB        NOT NULL DEFAULT '{}',  -- 凭证复用现有加密；connectors 的 allow_write 也在这里
    granted         JSONB        NOT NULL DEFAULT '{}',  -- 用户实际授予的权限
    status          SMALLINT     NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, plugin_id),
    FOREIGN KEY (plugin_id, version) REFERENCES plugins (plugin_id, version)
);
CREATE INDEX idx_plugin_inst_owner ON plugin_installations (owner_user_id, status);
```

智能体怎么引用插件：**不新增字段**。装好的插件在 `capabilities.tools[]`（`tools`/`renderers`/`connectors`）或 `capabilities.hooks.*`（`hooks`）里按 `plugin:acme.charts/render_chart` 这样的 ref 引用——`capabilities` 的"一个 ref 一个能力"心智完全不变，Bundle/Agent 层不用感知插件的存在。

### 5.2 接口

| 方法 + 路径 | 用途 | 落库 |
|---|---|---|
| `POST /plugins` | 上传 `.akp`，校验签名 + 清单，建版本（`review_status=pending`） | 是 |
| `GET /plugins` | 插件市场列表（筛选/搜索；只列 `review_status=passed` 且 `visibility=public` 的） | 否 |
| `GET /plugins/{id}` | 详情：清单、扩展点、要哪些权限 | 否 |
| `PATCH /plugins/{id}` | 发布者改可见性（`private`→`public`，即"申请上架"） | 是 |
| `POST /plugins/{id}/install` | 安装到当前账号，用户在这里授权；`private` 插件持有者本人可直接装，不用等审核 | 是 |
| `DELETE /plugins/{id}/install` | 卸载 | 是 |
| `PATCH /plugins/{id}/install` | 改配置 / 升级到指定版本 / 切 `resolution` | 是 |
| `GET /plugins/assets/{id}/{ver}/*` | 前端 iframe 内容下发（**独立资源域**） | 否 |
| `GET /moderation/plugins` | 待审插件队列（复用 spec-18 运营中心已有的举报/审核模式） | 否 |
| `POST /moderation/plugins/{id}/review` | 审核通过/驳回，写 `review_status` | 是 |

安装接口要返回一张"这个插件要什么权限"的清单让用户确认，不能装完才说。

### 5.3 上架审核与签名

第三方上传从 v1 就开放，但"能上传"和"能被别人搜到装上"是两件事，中间隔着签名和审核：

```
开发者本地打包 .akp
  → 用平台颁发的开发者密钥对清单+内容做签名（Ed25519，具体算法【待确认】，
    先占个位——参考 npm/VSCode 扩展市场的签名做法，不是我们自己发明密码学）
  → POST /plugins 上传
       ├─ 签名校验不过 → 拒绝，压根不建版本行
       ├─ 签名校验通过 → 建版本行，review_status=pending，visibility=private
       │    ↑ 到这一步，发布者自己已经能装、能测——不用等审核就能验证插件能跑
       └─ 自动化门槛（发布时跑，不是人工）：
            manifest 格式校验、扩展点声明的 entry 在包里真实存在、
            requires.network 域名格式合法、WASM 能被 wazero 正常编译、
            体积上限、【待确认】静态扫描要不要接（比如扫描明显的恶意 host function 调用模式）
  → 开发者觉得可以公开了，PATCH /plugins/{id} 把 visibility 改成 public
       → 进 review_status=pending 的人工复核队列（复用运营中心已有的举报处理界面/流程，
         不是另起一套）
       → 复核通过 review_status=passed，才会出现在 GET /plugins 的市场列表里
```

**私有可装、公开需审**是这里的关键设计：开发者验证自己插件能不能跑，不用等人工审核排队；但别人能不能在市场里搜到、装到，要过一轮人审。这个二级可见性和现有 Marketplace（spec-08）"发布/浏览"分离的思路是一致的，不是新发明的模式。

---

## 六、安全边界

| 威胁 | 措施 |
|---|---|
| 插件跑死循环 / 吃内存 | Extism 超时 + 内存上限，硬编码，插件改不了 |
| 插件外连挖数据 | 默认无网络；只放行 `requires.network` 里的域名，安装时展示给用户 |
| 插件读宿主文件 | 不给 `AllowedPaths` |
| 插件偷数据库密码 | 插件只拿 `connection_ref`，DSN 与密码不出宿主 |
| 前端插件偷 token / XSS | iframe `sandbox="allow-scripts"`，**不给 `allow-same-origin`**，独立资源域，与宿主只有 postMessage |
| 前端插件伪造工具调用 | UI→宿主 的调用走 JSON-RPC，宿主侧鉴权；敏感操作要用户确认 |
| 插件包被篡改 | 发布强制签名（§5.3），装载前校验 |
| 恶意插件上架 | 私有可装（发布者自测不用等审核）+ 公开须过自动化门槛与人工复核才进市场列表（§5.3） |
| `connectors` 越权写库 | 写权限判断点在宿主 host function，不在插件代码（§4.3），插件端逻辑不可信也翻不出这堵墙 |
| `hooks` 越权改写模型正文 | 改写权限按 hook 点显式声明（`write:response` 等），未声明只读；同一 hook 点两插件抢注编译期直接拒绝，不猜赢家 |

---

## 七、分期

五个确认项都拍板成了"开放"，所以分期不再是"先只做内置插件"的裁剪版，而是按依赖顺序把完整体系铺开——**但依然一次只做一层，每层做完能独立验证**：

| 阶段 | 内容 | 依赖 |
|---|---|---|
| **P0 规范定稿** | `plugin.json` schema（含 `hooks`/未知扩展点透传）、`.akp` 打包格式、签名算法选型、host API v1 版本号 | 本文确认 |
| **P1 后端 tools + 签名/私有安装** | Extism 接入、`plugins`/`plugin_installations` 两张表、上传（含签名校验+自动化门槛）+ 私有安装接口、`BuildPluginTools`、per-run 回收 | P0 |
| **P2 前端 renderers** | `node.render` 事件、iframe 宿主 + postMessage bridge、资源域下发、timeline 卡片 | P1 |
| **P3 connectors（含写开关）** | host function `sql.query/execute/schema`、连接池按 `allow_write` 分建、`connection_ref`、第一个 SQL 连接器插件 | P1 |
| **P4 hooks** | 编译期接管 `capabilities.hooks` 五个字段、按 hook 点的权限声明、5s 超时与失败降级 | P1 |
| **P5 插件市场** | 组件广场「插件」Tab 真正可用、可见性切换（private→public）、`GET /moderation/plugins` 复核队列（复用运营中心） | P1、P4 完成后再开公开可见性更安全 |
| **P6 官方插件 + SDK** | 图表插件、SQL 连接器两个官方样板 + `create-akp-plugin` 脚手架 + 文档 | P2/P3 |
| **P7 `resolution: live`** | 节点级动态解析插件版本（§2.1 表格里现在按下不表的那一半） | P1 |
| 研究阶段，不进分期 | `extensions.providers`（模型 Provider 插件，§3.3）——流式与凭证轮换两个未知数没解决之前不排期 | — |

P1 结束就能验证最关键的假设：**Extism 在我们的运行链路里跑得通、限得住**，以及签名+私有安装这条链路本身是否顺手。建议 P1 做完先停一下看效果，再往下走。

---

## 八、风险与未决

**风险**

1. **TinyGo 是插件作者的门槛**。Go 插件要用 TinyGo 编译，它对反射和部分标准库支持不全。缓解：官方样板用 Rust/JS 各给一个，别让作者以为只能写 Go。
2. **`auto_render` 是把双刃剑**。让模型输出被第三方代码接管，好处是体验好，坏处是"我看到的还是模型说的吗"变得不确定。缓解：渲染卡片上必须标出是哪个插件渲染的，并提供"看原始输出"。
3. **128 MiB / 30s 是拍的**。没有实测依据，P1 做完要用真实插件量一遍再定。
4. **审核队列是新的运营负担**。公开可见性一旦开放，"谁来审、多久审完"是运营流程问题，不只是技术问题——建议 P5 开始前先确认这条队列有人力接得住，技术上通不代表流程上通。
5. **`hooks` 是四个扩展点里权限最大的**。即便按点声明了读写权限，"一个 hook 决定拒绝某次工具调用"这种控制流层面的影响，比 renderers/connectors 更容易在排查问题时让人摸不着头脑——P4 做完建议先在内置插件上跑一段时间，再对第三方开放 `hooks` 类型的上传。

**仍然待确认（都不是阻塞 P0/P1 的，可以边做边定）**

1. 签名算法具体选型（Ed25519 是我的默认建议，需要确认）。
2. 自动化审核门槛要不要包含静态恶意模式扫描，还是先只做格式/体积/可编译性校验。
3. 128 MiB 内存上限、30s/5s 超时的具体数字，P1 用真实插件跑出来再定。
4. `hooks` 对第三方开放的时间点（见上面风险 5）——建议先内置插件验证，第三方稍后跟上，但"稍后多久"没有定论。

---

## 九、K8s 部署适配

现在的部署方式（Dockerfile.server + docker-compose，参考 spec-19）迟早要搬 K8s，这里检查插件体系有没有埋下不兼容的坑。结论是**没有埋坑，但有两处需要在 P1 落地时留意**：

**结论一：插件执行不需要额外的 Pod/容器**。Extism/wazero 是纯 Go 库，**进程内**执行 WASM，不 fork 子进程、不需要 Docker-in-Docker、不需要 gVisor 之类的 OS 级沙箱——沙箱边界是 wazero 自己的解释执行层，和容器编排无关。这意味着插件调用就是现有 API 服务器 Pod 里的一次函数调用，**水平扩容现有 Pod 就是扩容插件执行能力**，不需要为插件另起 CRD、Job 或 sidecar。这是选 WASM 而不是"子进程/容器隔离每个插件"方案的额外好处，之前调研阶段没强调，这里补上。

**结论二：wazero 纯 Go 无 CGO，不挑 base image**。`FROM scratch`/distroless 都能跑，不需要像某些沙箱方案那样要求特权容器或者宿主内核开某个 feature（比如 gVisor 需要的 seccomp 配置、Firecracker 需要的 KVM）。现有 `Dockerfile.server` 不用改构建方式。

**两处需要留意的地方**：

1. **编译缓存不跨 Pod 共享**。`extism.NewCompiledPlugin` 编译一次的缓存是进程内的，多副本部署下每个 Pod 各自第一次调用时都要重新编译一次目标插件——这不是 bug，是无状态水平扩展的正常代价，和现有服务完全一样（没有哪个内存缓存是跨 Pod 共享的）。如果编译耗时在实测中显著（不确定，需要 P1 实测），后续可以考虑给"编译结果"加一层可选的共享缓存（Redis 存编译产物或直接靠 wazero 自己的 cache 目录挂共享卷），但**这是性能优化，不是正确性问题**，不阻塞 P1。
2. **插件包已经在 OSS 上**，任何 Pod 都能拉到，这条继承自 Skill 那轮已经做好的基础设施，不用重新解决"多副本怎么访问同一份插件内容"这个问题。
3. **资源上限要嵌套在 Pod 资源限制之内**。Extism 的内存/超时上限（§4.1 表格里的 128 MiB/30s）必须显著小于 Pod 自己的 `resources.limits.memory`，否则一次插件调用的峰值加上宿主进程本身的内存占用可能触发 Pod 被 OOMKilled，进而把这次 run 之外、同一 Pod 上其他运行中的请求也带崩——这是"沙箱内存上限"和"Pod 内存上限"两层限制没对齐时才会出的问题，P1 要把这两个数字放在一起核对，不能只看 Extism 自己那一层。

---

## 参考

- Cordis：[cordiverse/cordis](https://github.com/cordiverse/cordis)、[cordis-rs](https://github.com/dshbox/cordis-rs)（Rust 移植）、[cordis-python](https://github.com/ddebowczyk/cordis-python)
- Extism：[go-sdk](https://github.com/extism/go-sdk)、[go-pdk](https://github.com/extism/go-pdk)、[写插件](https://extism.org/docs/quickstart/plugin-quickstart/)
- MCP Apps：[官方公告](https://blog.modelcontextprotocol.io/posts/2026-01-26-mcp-apps/)、[OpenAI Apps SDK](https://developers.openai.com/apps-sdk/reference)
- 其他 Go 方案：[knqyf263/go-plugin](https://github.com/knqyf263/go-plugin)、[网关插件架构对比](https://zuplo.com/learning-center/api-gateway-plugin-architectures-compared)
