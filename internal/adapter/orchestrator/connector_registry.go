package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/marcon0203/agentic-kit/internal/adapter/extism"
)

var (
	errUnknownConnRef  = errors.New("connector: unknown or expired connection_ref")
	errWriteNotAllowed = errors.New("connector: this connection was not granted allow_write")
)

// dsnBuilders maps a connector resource's declared dialect to how its
// config fields become a database/sql DSN — a registry-of-one-entry today
// (postgres, via pgx's own database/sql driver, already an indirect
// dependency — no new driver to add for a first connector). Adding mysql
// or clickhouse later is one more map entry, not a redesign, same
// "注册表项不是 switch" shape as modelgateway's provider registry.
var dsnBuilders = map[string]func(cfg ConnectorConfig) (driver, dsn string){
	"postgres": func(cfg ConnectorConfig) (string, string) {
		return "pgx", fmt.Sprintf("postgres://%s:%s@%s:%d/%s", cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
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
	r.conns[connRef] = &registeredConn{db: db, allowWrite: cfg.AllowWrite}
	r.mu.Unlock()
	return connRef, nil
}

// Release closes and forgets a connection. Safe to call on an unknown
// connRef (a no-op) — cleanup code should never itself fail a run.
func (r *connectorRegistry) Release(connRef string) {
	r.mu.Lock()
	c, ok := r.conns[connRef]
	delete(r.conns, connRef)
	r.mu.Unlock()
	if ok {
		_ = c.db.Close()
	}
}

func (r *connectorRegistry) get(connRef string) (*registeredConn, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.conns[connRef]
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

func (r *connectorRegistry) SQLSchema(ctx context.Context, connRef string) ([]string, error) {
	c, ok := r.get(connRef)
	if !ok {
		return nil, errUnknownConnRef
	}
	rows, err := c.db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name`)
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
