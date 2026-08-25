# spec-20 插件体系

> 状态：**设计稿，待确认**。spec-05a §4 里"插件只留判别位、业务梳理清楚后再做"的那件事，现在开始做。
> 本文只定方案，不含实现。带 `【待确认】` 的地方是需要你拍板的，不是我装作确定。

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
3. **规范要能撑住生态，而不是撑住第一个插件**。第一版只做两三个扩展点，但清单格式、版本协商、权限声明要按"以后会有几百个插件"来定，加扩展点应该是加一个枚举值，不是推倒重来。

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

硬把 Cordis 的 fiber/effect 树搬进 Go，会得到一个没有热重载需求的热重载框架。**【待确认】** 如果你要的恰恰是"运行中的服务不重启就能装插件并立即生效"，那结论会变，我们再谈。

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
        "config_schema": { /* 建连接时要用户填什么 */ }
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
| `hooks`【待确认】 | 后端 WASM 沙箱 | 运行生命周期事件 | 输出脱敏、审计、内容过滤 |

`hooks` 我倾向**第一版不做**：它能改写模型输出，是权限最大的扩展点，值得单独一轮把边界想清楚。

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
用户在「组件」里注册一个 clickhouse 连接
  → 凭证走已有的 config 加密（credentialKeySubstrings 已覆盖 password/token 等）
  → 宿主建连接池，给它一个 connection_ref

模型要查数
  → 调插件的 tool
  → 插件在沙箱里调宿主 host function：sql.query(connection_ref, sql, args)
     ↑ 插件拿到的是 connection_ref，永远拿不到 DSN 和密码
  → 宿主校验 connection_ref 属于本次 run 的 owner、执行、把结果回给插件
  → 插件把结果整理成模型好读的形状返回
```

插件负责的是"这个数据库的方言怎么写、schema 怎么描述给模型"，不负责"怎么连上去"。

宿主服务（host functions）第一版提供：

| 服务 | 函数 | 说明 |
|---|---|---|
| `sql` | `query(conn_ref, sql, args) → rows` | 只读；写操作【待确认】是否开放 |
| `sql` | `schema(conn_ref) → tables` | 让模型知道有哪些表 |
| `log` | `log(level, msg)` | 进运行事件流，便于调试 |
| `kv` | `get/set(key, value)` | 插件自己的小状态，按 (plugin, owner) 隔离 |

### 4.4 生命周期：谁创建谁回收

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
    status          SMALLINT     NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (plugin_id, version)
);

-- 谁装了哪个版本
CREATE TABLE plugin_installations (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT       NOT NULL REFERENCES users(id),
    plugin_id       VARCHAR(128) NOT NULL,
    version         VARCHAR(32)  NOT NULL,        -- 锁定版本，不自动升级
    config          JSONB        NOT NULL DEFAULT '{}',  -- 凭证复用现有加密
    granted         JSONB        NOT NULL DEFAULT '{}',  -- 用户实际授予的权限
    status          SMALLINT     NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, plugin_id),
    FOREIGN KEY (plugin_id, version) REFERENCES plugins (plugin_id, version)
);
CREATE INDEX idx_plugin_inst_owner ON plugin_installations (owner_user_id, status);
```

智能体怎么引用插件：**不新增字段**。装好的插件在 `capabilities.tools[]` 里按 `plugin:acme.charts/render_chart` 这样的 ref 引用——`capabilities` 的"一个 ref 一个能力"心智完全不变，Bundle/Agent 层不用感知插件的存在。

### 5.2 接口

| 方法 + 路径 | 用途 | 落库 |
|---|---|---|
| `POST /plugins` | 上传 `.akp`，解析校验清单，建版本 | 是 |
| `GET /plugins` | 插件市场列表（筛选/搜索） | 否 |
| `GET /plugins/{id}` | 详情：清单、扩展点、要哪些权限 | 否 |
| `POST /plugins/{id}/install` | 安装到当前账号，用户在这里授权 | 是 |
| `DELETE /plugins/{id}/install` | 卸载 | 是 |
| `PATCH /plugins/{id}/install` | 改配置 / 升级到指定版本 | 是 |
| `GET /plugins/assets/{id}/{ver}/*` | 前端 iframe 内容下发（**独立资源域**） | 否 |

安装接口要返回一张"这个插件要什么权限"的清单让用户确认，不能装完才说。

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
| 恶意插件上架 | 【待确认】上架审核策略：先人工审 / 只允许平台内置 / 开放但标注"未审核" |
| 插件包被篡改 | 【待确认】是否要求签名 |

---

## 七、分期

| 阶段 | 内容 | 依赖 |
|---|---|---|
| **P0 规范定稿** | `plugin.json` schema、`.akp` 打包格式、host API v1 版本号 | 本文确认 |
| **P1 后端 tools** | Extism 接入、plugins/plugin_installations 两张表、上传+安装接口、`BuildPluginTools`、per-run 回收 | P0 |
| **P2 前端 renderers** | `node.render` 事件、iframe 宿主 + postMessage bridge、资源域下发、timeline 卡片 | P1 |
| **P3 connectors** | host function `sql.query/schema`、连接池与 `connection_ref`、第一个 SQL 连接器插件 | P1 |
| **P4 插件市场** | 组件广场的「插件」Tab 真正可用（现在是"即将支持"占位）、安装授权页 | P1 |
| **P5 官方插件 + SDK** | 图表插件、SQL 连接器两个官方样板 + `create-akp-plugin` 脚手架 + 文档 | P2/P3 |
| 之后 | `hooks` 扩展点、插件间依赖、审核与签名 | — |

P1 结束就能验证最关键的假设：**Extism 在我们的运行链路里跑得通、限得住**。建议 P1 做完先停一下看效果，再决定 P2/P3 的顺序。

---

## 八、风险与未决

**风险**

1. **TinyGo 是插件作者的门槛**。Go 插件要用 TinyGo 编译，它对反射和部分标准库支持不全。缓解：官方样板用 Rust/JS 各给一个，别让作者以为只能写 Go。
2. **`auto_render` 是把双刃剑**。让模型输出被第三方代码接管，好处是体验好，坏处是"我看到的还是模型说的吗"变得不确定。缓解：渲染卡片上必须标出是哪个插件渲染的，并提供"看原始输出"。
3. **128 MiB / 30s 是拍的**。没有实测依据，P1 做完要用真实插件量一遍再定。
4. **插件市场是个产品，不只是功能**。上架审核、版本弃用、作者信誉，这些没想清楚之前，建议**先只开放平台内置插件**，把"第三方上传"押后。

**需要你拍板的（按重要性排序）**

1. **热重载要不要**：运行中的服务不重启就装插件立即生效？要的话 §2.1 的结论要重来。
2. **第三方上传第一版开不开**：只做内置插件，链路能短一半（不需要审核、签名、市场）。
3. **`hooks`（能改写模型输出的钩子）第一版做不做**：我建议不做。
4. **connectors 的写操作开不开**：只读安全得多。
5. **上架审核与签名策略**。

**我需要提醒的一点**：这个体系比之前任何一轮都大——它引入了新的运行时（WASM）、新的信任边界（第三方代码）、新的产品面（市场与审核）。P1 就有相当的量。如果你想更快看到东西，一个合理的裁剪是：**P1 只做"平台内置插件"，跳过上传/市场/审核，直接把图表插件和 SQL 连接器做成内置的**，把规范和运行时跑通，第三方生态那一层等跑通了再开。

---

## 参考

- Cordis：[cordiverse/cordis](https://github.com/cordiverse/cordis)、[cordis-rs](https://github.com/dshbox/cordis-rs)（Rust 移植）、[cordis-python](https://github.com/ddebowczyk/cordis-python)
- Extism：[go-sdk](https://github.com/extism/go-sdk)、[go-pdk](https://github.com/extism/go-pdk)、[写插件](https://extism.org/docs/quickstart/plugin-quickstart/)
- MCP Apps：[官方公告](https://blog.modelcontextprotocol.io/posts/2026-01-26-mcp-apps/)、[OpenAI Apps SDK](https://developers.openai.com/apps-sdk/reference)
- 其他 Go 方案：[knqyf263/go-plugin](https://github.com/knqyf263/go-plugin)、[网关插件架构对比](https://zuplo.com/learning-center/api-gateway-plugin-architectures-compared)
