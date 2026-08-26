# SQL 连接器示例插件

spec-20 §4.3 connectors 的第一个真实、可编译的样板：一个 Rust + Extism PDK
写的 WASM 插件，通过宿主的 `sql.query`/`sql.execute`/`sql.schema` host
function 读写一个由宿主绑定好的数据库连接——插件本身永远看不到 DSN 或密码，
只拿得到一个不透明的 `connection_ref`（经 `plugin_config` 的
`connection_ref` 键，由 `internal/adapter/orchestrator/plugin_authorizer.go`
的 `bindConnector` 在授权阶段注入，插件通过 Extism 的 `config.get` 读取）。

## 三个 tools

- `list_tables` — 只读，列出已绑定连接的表名，调用 `sql.schema`。
- `run_query` — 只读，执行调用方给的一条查询，调用 `sql.query`。
- `run_write` — 执行调用方给的一条写语句，调用 `sql.execute`；连接是否真的
  允许写，完全由宿主的 `connectorRegistry.SQLExecute` 按连接自身的
  `allow_write` 绑定决定，插件这边不做、也做不了任何判断。

## 编译

```sh
rustup target add wasm32-wasip1   # 一次性
cargo build --release --target wasm32-wasip1
```

产物在 `target/wasm32-wasip1/release/sql_connector_plugin.wasm`。
`internal/adapter/extism/testdata/sql_connector.wasm` 是这份产物的一份拷贝，
供 `internal/adapter/extism/connector_plugin_test.go` 做端到端验证——不经
真正的数据库连接，用一个假的 `HostServices` 实现验证插件确实经由三个
host function 收发了正确的 JSON，且写路径的拒绝确实是宿主侧发生的。
