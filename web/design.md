# Design — Agentic Kit

这个 app 的锁定设计系统。每次页面改版**先读这个文件**，不要按页各自发挥。
系统需要生长时，改这个文件，而不是在某个页面里就地覆盖。

由 `hallmark redesign ./web` 产出（Hallmark v1.1.0）。

## Genre

**modern-minimal** — SaaS 控制台 / 平台 / 开发者工具。克制的单一强调色、
确信的无衬线大字、成体系的留白、描边分层。不是"没有选择"的极简，是"有主张"
的极简。

## Macrostructure family

页面按类型分三个家族。同一家族内共享形态，只在组件原型（archetype）上变化。

- **App 页**（控制台主体，约 30 个路由）：**Workbench**
  页面是"应用在被使用"的现场，不是营销叙述。功能承载页面，少文案、多操作面。
  变化旋钮：列表 / 编辑器 / 详情三种内容密度。
- **营销页**（`/` 首页）：**Split Studio**
  双联画。每个主内容块把画面一分为二——一侧陈述，一侧佐证；方向沿页面交替。
- **内容页**（广场详情、组件详情、市场详情）：**Long Document**
  连续的散文式区块 + 内联小标题，靠间距和层级分节，不靠卡片堆叠。

## Theme

沿用项目既有 token（pre-flight 保留项），定义在 `src/index.css` 的 `:root`。
Hallmark 不改配色，只约束它的**用法**。

| Token | 值 | 角色 |
|---|---|---|
| `--color-surface` | `#ffffff` | 卡片表面 |
| `--color-surface-page` | `#f6f7fb` | 页面画布 |
| `--color-surface-muted` | `#f1f1f5` | 表头 / 轨道 / 骨架屏 |
| `--color-ink-900` | `#1a1a22` | 标题、数值（近黑，不用纯黑） |
| `--color-ink-700` | `#6e7079` | 正文 |
| `--color-ink-500` | `#a0a3ae` | 时间戳、占位符（2.7:1，不用于必读信息） |
| `--color-border` | `#f0f0f4` | 卡片描边——**承担分区职责** |
| `--color-blueprint` | `#7c5cfc` | 唯一强调色：主按钮、链接、激活态、运行中 |
| `--color-signal` | `#e09000` | 唯一注意色：等待人工审批 |
| `--color-moss` / `--color-rust` | `#22c55e` / `#f04438` | 成功 / 失败 |

### 强调色纪律（本次改版的核心约束）

**一屏之内紫色出现不超过 6 处。** 允许出现的位置只有：激活的导航项、
主按钮、图标底 chip、图表主数据、少量激活态。图标、标题、边框、分割线一律中性色。

**渐变全站只保留一处** —— 首页收尾的 CTA 横幅。其余任何位置都用实色
`--color-blueprint`。理由：紫→紫渐变铺满 41 个按钮时，它不再是品牌信号，
而是 AI 生成页面最容易被一眼认出的特征（Hallmark critical:
*the purple-gradient hero*）。

**`background-clip: text` 渐变文字全站禁用**（Hallmark gate 2，无例外）。
标题的强调用字重、accent 色或下划线承载，不用渐变填充。

## Typography

- **Display**：Plus Jakarta Sans 600–700，字距 `-0.015em ~ -0.03em`
- **Body**：Plus Jakarta Sans 400
- **Mono**：JetBrains Mono 500 —— 只用于标识符（ref、version、run_id、DSL key）
- 标题**一律 roman**（`font-style: normal`）。斜体只作为正文内的强调存在。
- 数字容器一律 `font-variant-numeric: tabular-nums`（`.tabular` / `.text-figure`）。
- 长标题在词内断行：`overflow-wrap: anywhere; min-width: 0`。

## Spacing

4pt 命名刻度，值在 `src/index.css`。页面必须用命名 token（`p-space-6`），
不写裸值。区块之间的呼吸不要每节都一样——刻意收紧一节、放开一节。

## Radius

五档层级信号，外层大于内层。**不引入第六个值。**

`--radius-xs` 6（复选框）· `--radius-sm` 10（按钮/输入框）·
`--radius-md` 12（内嵌块）· `--radius-lg` 16（一级卡片/弹窗）·
`--radius-xl` 24（最外层容器）

## Motion

motion-cut 项目（无 framer-motion / gsap）。CSS 动效，克制优先。

- 只动 `transform` 和 `opacity`，绝不动布局属性。
- 禁止 `transition-all` —— 逐条列出要动的属性。
- 缓动只用 `ease-out`，不用 bounce / overshoot。时长 120–180ms。
- **焦点环必须瞬时出现**，绝不放进 transition —— 键盘用户不能在动画的前 100ms
  里看不到自己在哪。
- 尊重 `prefers-reduced-motion: reduce`（已在 index.css 全局处理）。
- 唯一允许的循环动画：`animate-gate-await`（人工审批门在等人，这是它字面上
  正在做的事）。

## Microinteractions 立场

- **静默成功**：用户看得见结果的操作（保存后跳转、列表当场刷新）不弹 toast。
  toast 只留给失败、效果不可见的异步动作、以及用户之后会需要的确认。
- **乐观更新 + 撤销** 优先于确认弹窗。确认弹窗只留给不可逆的破坏性操作。
- Tooltip：hover 延迟 800ms，focus 延迟 0ms。
- 骨架屏优先于 spinner；spinner 延迟 150ms 再出现。

## CTA voice

- **主按钮**：实色 `--color-blueprint` + 白字 + 柔和紫色投影
  （`0 6px 16px rgb(124 92 252 / .3)`），`--radius-sm`，14px/600。
  一屏只放一个主按钮——出现两个说明主次没分清。
  **不要再写 `className="bg-gradient-cta text-white"` 覆盖**：Button 的
  `default` variant 已经是这套，覆盖只会把渐变重新铺回来。
- **次按钮**：白底 + `--color-border` 描边 + `--color-ink-700` 文字。
- **轻量操作**：透明底，hover 才有底色。

## Per-page allowances

- 营销页（`/`）**可以**用 enrichment：唯一那处渐变横幅、双联画版式。
- App 页**禁止** enrichment —— 功能承载页面。不要装饰性光球、不要模糊色斑、
  不要为了"有层次"而加的背景元素。
- 内容页：仅排版。

## 页面之间必须共享

- 品牌字标与导航形态（N1b 规范三段式：wordmark 左 / 导航中 / 账号右）。
- 强调色及其出现密度（每屏 ≤ 5%）。
- Display + Body 字体。
- CTA 声音（按钮形状、圆角、内边距节奏）。
- 卡片语言：`rounded-lg border border-border bg-surface` 扁平卡片，
  靠描边分层，不靠阴影。阴影只出现在浮层（下拉/弹窗）和主按钮的彩色投影。

## 页面之间可以不同

- 家族内的 macrostructure 变体（列表页 / 编辑器页 / 详情页三种密度）。
- 内容区的栅格与分栏。
- 区块间距的松紧（刻意变化，不要每节等距）。

## 禁止清单（本次审计实际命中的）

1. 渐变文字（`background-clip: text`）—— 已全站移除。
2. 渐变铺满主按钮 —— 已收敛为实色 + 单处横幅。
3. 悬浮模糊光球 / 色斑装饰 —— 已移除。
4. 每个区块顶部的大写 eyebrow 标签 —— 默认关闭，只在内容真的有序号时用。
5. `transition-all` —— 已逐条指定属性。
6. 带过渡的焦点环 —— 已改为瞬时。
7. 庆祝式成功 toast —— 结果可见的操作改为静默。
8. tag-left / heading-right 两栏式区块头（悬挂标题）—— 禁用，标签一律竖排在
   标题正上方同一列。

## Exports

### tokens.css

见 `src/index.css` 的 `:root` 块——那里是本项目 token 的唯一事实来源，
`@theme inline` 把它们映射成 Tailwind utility（`bg-surface`、`text-ink-900`、
`p-space-6`、`rounded-lg`）。组件只消费语义 token 名，不写死色值。
