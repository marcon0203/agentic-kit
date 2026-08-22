# DSL Schema

Agent 与 Bundle 的 JSON Schema 定义，以及可直接跑通的示例文件。

```
schemas/
├── agent.schema.json      Agent DSL 定义
├── bundle.schema.json     Bundle DSL 定义
└── examples/              符合上述 schema 的示例（POC 阶段实测跑通过）
    ├── product_manager.agent.yaml
    ├── architect.agent.yaml
    ├── ui_designer.agent.yaml
    ├── fullstack_engineer.agent.yaml
    └── web-app-builder.bundle.yaml
```

## 为什么用 JSON Schema 而不是 struct tag / zod

**同一份文件前后端复用**：后端保存时校验（`santhosh-tekuri/jsonschema/v5`），前端表单实时校验（`ajv`）。规则只有一份，不会出现"前端过了后端不过"。

这也是为什么前端 schema 校验选 Ajv 而不是 zod——zod 无法直接吃 JSON Schema 文件，得再维护一份 TS 定义，两份就会漂移。

## 校验分两层，不要混在一起

| 层 | 管什么 | 在哪做 | 失败返回 |
|---|---|---|---|
| **Schema 校验** | 字段类型、必填、枚举值、pattern | 本目录的两份文件 | 400 + 40001/40002 |
| **图逻辑校验** | entry 是否存在、边引用的节点是否声明、自环边是否导致死锁 | `internal/bundle` 的图校验器 | 422 + 40003 |

分开的理由：这是两类不同性质的错误，混在一起报错信息会很难读。Schema 说的是"你这个字段写错了"，图校验说的是"你的编排逻辑跑不通"。

**Schema 管不了的事**（必须在图校验器里做）：

- `entry` 指向的节点是否在 `agents[]` 里声明过
- `edges[].from/to` 引用的节点是否存在
- **自环边被计入 join 前置计数导致的死锁**——POC 阶段真实踩过的坑，纯看 DSL 完全看不出来
- `edge.condition` 表达式的语法正确性（用 expr-lang 预编译）

## 关键字段说明

### `display_meta`（两份 schema 都有）

**黑盒发布能安全实现的基础**。可公开展示的元信息与 `definition` **分字段存储**，不是查出来再过滤——过滤逻辑漏一处就是泄露。

订阅者视角的查询方法从 SELECT 列表里就不含 `definition`（见 spec-08 的 DAO 层强制隔离）。

### `constraints.estimated_tokens_range`（agent）

黑盒规则的**必要例外**：不暴露 persona 和编排图，但必须让订阅者能预估成本，否则他无法判断要不要订阅。

### `agents[].version`（bundle）

Bundle 引用 Agent 的**具体版本**。发布到广场时的递归依赖闭包校验按精确版本匹配——必须是「该版本」已发布，不是「该 Agent 有任意版本发过」。

### `human_gates[].approver_roles`（bundle）

为空时默认为运行发起人本人。**后端必须二次校验**，前端隐藏按钮只是体验优化，不是安全手段。

## 本地校验

```bash
pip install jsonschema pyyaml
python3 -c "
import json, yaml, glob
from jsonschema import Draft202012Validator
av = Draft202012Validator(json.load(open('agent.schema.json')))
for f in glob.glob('examples/*.agent.yaml'):
    errs = list(av.iter_errors(yaml.safe_load(open(f))))
    print(f, '✓' if not errs else errs[0].message)
"
```

示例文件已通过正向校验（5 个文件全过）和反向校验（12 个非法用例全部被拦截，覆盖枚举越界、缺必填、pattern 不匹配、多余字段、类型错误）。
