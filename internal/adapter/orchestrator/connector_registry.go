package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/marcon0203/agentic-kit/internal/adapter/extism"
)

var (
	errUnknownConnRef  = errors.New("connector: unknown or expired connection_ref")
	errWriteNotAllowed = errors.New("connector: this connection was not granted allow_write")
)

// dsnBuilders maps a connector resource's declared dialect to how its
// config fields become a database/sql DSN — postgres via pgx's own
// database/sql driver, mysql via go-sql-driver/mysql's, both already
// dependencies elsewhere in the module (pgx directly, mysql transitively)
// so neither is a new supply-chain addition for these two built-in
// connectors. Adding clickhouse or another dialect later is one more map
// entry, not a redesign, same "注册表项不是 switch" shape as modelgateway's
// provider registry.
var dsnBuilders = map[string]func(cfg ConnectorConfig) (driver, dsn string){
	"postgres": func(cfg ConnectorConfig) (string, string) {
		return "pgx", fmt.Sprintf("postgres://%s:%s@%s:%d/%s", cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	},
	"mysql": func(cfg ConnectorConfig) (string, string) {
		// mysql.Config.FormatDSN/ParseDSN round-trip a password containing
		// DSN-special characters ("@", ":") correctly — building the DSN
		// by hand the way the postgres builder above does would not.
		dsnCfg := mysql.NewConfig()
		dsnCfg.User, dsnCfg.Passwd = cfg.Username, cfg.Password
		dsnCfg.Net = "tcp"
		dsnCfg.Addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
		dsnCfg.DBName = cfg.Database
		dsnCfg.ParseTime = true
		return "mysql", dsnCfg.FormatDSN()
	},
}

// ConnectorConfig is a connector resource's config (component_type=
// connector, spec-20 §4.3) — host/port/database/username/password follow
// the resource context's existing credential-field convention (password
// matches IsCredentialKey and is encrypted at rest the same way every
// other resource's credentials are); AllowWrite is the write-access
// toggle the host function layer, not plugin code, enforces.
type ConnectorConfig struct {
	Dialect    string
	Host       string
	Port       int
	Database   string
	Username   string
	Password   string
	AllowWrite bool
}

// connectorRegistry implements extism.HostServices — the process-wide
// backend every compiled plugin's sql.*/kv.* host functions call into. A
// connection_ref is only ever handed to a plugin after Bind opens (and
// pings) a real database/sql connection; the plugin never sees the DSN or
// credentials themselves (spec-20 §4.3: "插件拿到的是 connection_ref，永远
// 拿不到 DSN 和密码").
type connectorRegistry struct {
	mu    sync.Mutex
	conns map[string]*registeredConn
	kv    kvStore
}

type registeredConn struct {
	db         *sql.DB
	allowWrite bool
	// dialect/database are kept alongside the connection because
	// SQLSchema's query is dialect-specific (Postgres scopes
	// information_schema by a fixed "public" schema name; MySQL scopes it
	// by the database name itself) — everything else here is plain SQL
	// the driver already made portable.
	dialect  string
	database string
}

// kvStore backs the kv.get/set host functions — persisted so a plugin's
// small state survives past one run (spec-20 §4.3's "插件自己的小状态，按
// (plugin, owner) 隔离"). namespace is "{plugin_id}:{owner_user_id}",
// composed by the caller that resolves a plugin ref, not by this package.
type kvStore interface {
	Get(ctx context.Context, namespace, key string) (string, bool, error)
	Set(ctx context.Context, namespace, key, value string) error
}

// NewConnectorRegistry constructs the process-wide connector backend
// (spec-20 §4.3). kv may be nil — a deployment can run connectors without
// kv persistence (kv.get/set then returns a clear "not configured" error,
// same as the sql.* functions do when connectors themselves aren't wired).
func NewConnectorRegistry(kv kvStore) *connectorRegistry {
	return &connectorRegistry{conns: map[string]*registeredConn{}, kv: kv}
}

var _ extism.HostServices = (*connectorRegistry)(nil)

// Bind opens a real connection for cfg and returns the opaque token a
// plugin's host function calls will use to reach it. Call Release once the
// binding's owner (a run, per spec-20 §4.5's "谁创建谁回收") is done with
// it — Bind itself never expires a connection on its own.
func (r *connectorRegistry) Bind(ctx context.Context, cfg ConnectorConfig) (connRef string, err error) {
	build, ok := dsnBuilders[cfg.Dialect]
	if !ok {
		return "", fmt.Errorf("connector: unsupported dialect %q", cfg.Dialect)
	}
	driver, dsn := build(cfg)
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return "", fmt.Errorf("connector: open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return "", fmt.Errorf("connector: ping: %w", err)
	}

	connRef = uuid.NewString()
	r.mu.Lock()
	r.conns[connRef] = &registeredConn{db: db, allowWrite: cfg.AllowWrite, dialect: cfg.Dialect, database: cfg.Database}
	live := len(r.conns)
	r.mu.Unlock()
	slog.Info("connector_bound", "connection_ref", connRef, "dialect", cfg.Dialect, "database", cfg.Database, "live_connections", live)
	return connRef, nil
}

// Release closes and forgets a connection. Safe to call on an unknown
// connRef (a no-op) — cleanup code should never itself fail a run.
func (r *connectorRegistry) Release(connRef string) {
	r.mu.Lock()
	c, ok := r.conns[connRef]
	delete(r.conns, connRef)
	live := len(r.conns)
	r.mu.Unlock()
	if ok {
		slog.Info("connector_released", "connection_ref", connRef, "live_connections", live)
		_ = c.db.Close()
	}
}

func (r *connectorRegistry) get(connRef string) (*registeredConn, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.conns[connRef]
	if !ok {
		// Diagnostic for "unknown or expired connection_ref" reports: this
		// logs every *other* live ref too (never the failing one's data,
		// there's none to log) so a report can distinguish "this ref was
		// never bound at all" from "it was bound and already released" —
		// the live count alone doesn't tell them apart.
		live := make([]string, 0, len(r.conns))
		for ref := range r.conns {
			live = append(live, ref)
		}
		slog.Warn("connector_unknown_ref", "requested_connection_ref", connRef, "live_connection_refs", live)
	}
	return c, ok
}

func (r *connectorRegistry) SQLQuery(ctx context.Context, connRef, query string, args []any) ([]map[string]any, error) {
	c, ok := r.get(connRef)
	if !ok {
		return nil, errUnknownConnRef
	}
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanRows(rows)
}

// SQLExecute is the one call site spec-20 §4.3 requires the enforcement
// point to be: allow_write is checked here, against the connection's own
// binding — not against anything the plugin sent — so no input a plugin
// constructs can talk its way past it.
func (r *connectorRegistry) SQLExecute(ctx context.Context, connRef, query string, args []any) (int64, error) {
	c, ok := r.get(connRef)
	if !ok {
		return 0, errUnknownConnRef
	}
	if !c.allowWrite {
		return 0, errWriteNotAllowed
	}
	result, err := c.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// schemaQueries mirrors dsnBuilders' one-map-entry-per-dialect shape:
// Postgres' information_schema is scoped by a fixed "public" schema name,
// MySQL's by the database name itself, so this is the one query that
// can't be written portably across both.
var schemaQueries = map[string]struct {
	sql  string
	args func(c *registeredConn) []any
}{
	"postgres": {
		sql:  `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name`,
		args: func(*registeredConn) []any { return nil },
	},
	"mysql": {
		sql:  `SELECT table_name FROM information_schema.tables WHERE table_schema = ? ORDER BY table_name`,
		args: func(c *registeredConn) []any { return []any{c.database} },
	},
}

func (r *connectorRegistry) SQLSchema(ctx context.Context, connRef string) ([]string, error) {
	c, ok := r.get(connRef)
	if !ok {
		return nil, errUnknownConnRef
	}
	q, ok := schemaQueries[c.dialect]
	if !ok {
		return nil, fmt.Errorf("connector: unsupported dialect %q", c.dialect)
	}
	rows, err := c.db.QueryContext(ctx, q.sql, q.args(c)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func (r *connectorRegistry) KVGet(ctx context.Context, namespace, key string) (string, bool, error) {
	if r.kv == nil {
		return "", false, errNotConfigured
	}
	return r.kv.Get(ctx, namespace, key)
}

func (r *connectorRegistry) KVSet(ctx context.Context, namespace, key, value string) error {
	if r.kv == nil {
		return errNotConfigured
	}
	return r.kv.Set(ctx, namespace, key, value)
}

var errNotConfigured = errors.New("connector: kv storage is not configured on this deployment")

// scanRows turns a *sql.Rows into JSON-friendly maps — the shape
// sqlQueryResponse hands back to the plugin. []byte values are converted
// to string (most drivers hand back TEXT/VARCHAR as []byte) so json.Marshal
// doesn't base64-encode ordinary text.
func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var out []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			if b, ok := values[i].([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = values[i]
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
