// Sample SQL connector plugin (spec-20 §4.3) — the first real, compiled
// .wasm plugin that exercises the connectors host functions
// (internal/adapter/extism/host_functions.go): sql.query/execute/schema.
// A plugin never sees a DSN or credentials, only the connection_ref the
// host injects into its plugin_config at authorize time.
use extism_pdk::*;
use serde::{Deserialize, Serialize};

#[host_fn]
extern "ExtismHost" {
    fn sql_query(input: Json<SqlQueryRequest>) -> Json<Envelope>;
    fn sql_execute(input: Json<SqlExecRequest>) -> Json<Envelope>;
    fn sql_schema(input: Json<SqlSchemaRequest>) -> Json<Envelope>;
}

#[derive(Serialize)]
struct SqlQueryRequest {
    conn_ref: String,
    sql: String,
    args: Vec<serde_json::Value>,
}

#[derive(Serialize)]
struct SqlExecRequest {
    conn_ref: String,
    sql: String,
    args: Vec<serde_json::Value>,
}

#[derive(Serialize)]
struct SqlSchemaRequest {
    conn_ref: String,
}

/// Envelope is the wire shape every connectors host function replies with
/// (internal/adapter/extism/host_functions.go's writeEnvelope): `ok` plus
/// the call's own fields flattened in, or `error` when ok is false. A
/// plugin must check `ok` itself — an Extism host function call only fails
/// at the Rust level for a transport/memory problem, never because the
/// host's own logic (like allow_write) rejected the request.
#[derive(Deserialize)]
struct Envelope {
    ok: bool,
    error: Option<String>,
    #[serde(flatten)]
    rest: serde_json::Value,
}

impl Envelope {
    fn into_field(self, key: &str) -> FnResult<serde_json::Value> {
        if !self.ok {
            let msg = self.error.unwrap_or_else(|| "connector host call failed".to_string());
            return Err(Error::msg(msg).into());
        }
        Ok(self.rest.get(key).cloned().unwrap_or(serde_json::Value::Null))
    }
}

#[derive(Deserialize)]
struct ToolInput {
    /// connection_ref is filled in by the host (resourceAuthorizer.bindConnector)
    /// via plugin_config, not by the caller — but the plugin still needs it in
    /// its own input to know which of its args-based host calls to make, so
    /// BuildPluginTool's opts.Config carries it through as a plugin-visible
    /// config value the wasm side reads via extism_pdk::config::get.
    #[serde(default)]
    query: String,
}

#[derive(Serialize)]
struct ToolOutput {
    rows: serde_json::Value,
}

#[derive(Serialize)]
struct WriteOutput {
    affected_rows: serde_json::Value,
}

/// list_tables is the sample's `tools[].entry` for a read-only schema
/// listing — the minimal, always-safe call every SQL connector plugin
/// should expose regardless of allow_write.
#[plugin_fn]
pub fn list_tables() -> FnResult<Json<ToolOutput>> {
    let conn_ref = config::get("connection_ref")?.unwrap_or_default();
    let Json(envelope) = unsafe { sql_schema(Json(SqlSchemaRequest { conn_ref }))? };
    let tables = envelope.into_field("tables")?;
    Ok(Json(ToolOutput { rows: tables }))
}

/// run_query is the sample's `tools[].entry` for a caller-supplied
/// read-only query.
#[plugin_fn]
pub fn run_query(input: Json<ToolInput>) -> FnResult<Json<ToolOutput>> {
    let Json(input) = input;
    let conn_ref = config::get("connection_ref")?.unwrap_or_default();
    let Json(envelope) = unsafe {
        sql_query(Json(SqlQueryRequest {
            conn_ref,
            sql: input.query,
            args: vec![],
        }))?
    };
    let rows = envelope.into_field("rows")?;
    Ok(Json(ToolOutput { rows }))
}

/// run_write is the sample's `tools[].entry` for a caller-supplied
/// mutating statement. It always calls sql_execute — whether the
/// statement is actually allowed to run is decided entirely host-side
/// (connectorRegistry.SQLExecute checks the connection's own allow_write
/// binding), so this plugin never needs to know or claim its own
/// permission: sending the request is the only thing it can do, and a
/// connection opened without allow_write will simply reject it, which
/// surfaces here as `envelope.ok == false` — not as a special error type
/// this plugin has to know about.
#[plugin_fn]
pub fn run_write(input: Json<ToolInput>) -> FnResult<Json<WriteOutput>> {
    let Json(input) = input;
    let conn_ref = config::get("connection_ref")?.unwrap_or_default();
    let Json(envelope) = unsafe {
        sql_execute(Json(SqlExecRequest {
            conn_ref,
            sql: input.query,
            args: vec![],
        }))?
    };
    let affected_rows = envelope.into_field("affected_rows")?;
    Ok(Json(WriteOutput { affected_rows }))
}
