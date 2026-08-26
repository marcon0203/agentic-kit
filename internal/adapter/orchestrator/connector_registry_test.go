package orchestrator

import (
	"context"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestDSNBuilders_Postgres(t *testing.T) {
	build, ok := dsnBuilders["postgres"]
	if !ok {
		t.Fatal("expected a \"postgres\" dsnBuilder to be registered")
	}
	driver, dsn := build(ConnectorConfig{
		Host: "db.internal", Port: 5432, Database: "app", Username: "u", Password: "p",
	})
	if driver != "pgx" {
		t.Errorf("driver = %q, want \"pgx\"", driver)
	}
	want := "postgres://u:p@db.internal:5432/app"
	if dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}
}

func TestDSNBuilders_MySQL(t *testing.T) {
	build, ok := dsnBuilders["mysql"]
	if !ok {
		t.Fatal("expected a \"mysql\" dsnBuilder to be registered")
	}
	driver, dsn := build(ConnectorConfig{
		Host: "db.internal", Port: 3306, Database: "app", Username: "u", Password: "p",
	})
	if driver != "mysql" {
		t.Errorf("driver = %q, want \"mysql\"", driver)
	}
	want := "u:p@tcp(db.internal:3306)/app?parseTime=true"
	if dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}
}

func TestDSNBuilders_MySQL_RoundTripsSpecialCharacters(t *testing.T) {
	build, ok := dsnBuilders["mysql"]
	if !ok {
		t.Fatal("expected a \"mysql\" dsnBuilder to be registered")
	}
	// Building the DSN via mysql.Config.FormatDSN (rather than by hand,
	// the way the postgres builder does) is what guarantees this parses
	// back to the same credentials even when the password itself contains
	// DSN-special characters like "@" or ":".
	_, dsn := build(ConnectorConfig{Host: "db", Port: 3306, Database: "app", Username: "u", Password: "p@ss:word"})
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN(%q): %v", dsn, err)
	}
	if parsed.User != "u" || parsed.Passwd != "p@ss:word" || parsed.DBName != "app" {
		t.Errorf("parsed = %+v, want User=u Passwd=p@ss:word DBName=app", parsed)
	}
}

func TestConnectorRegistry_SQLSchema_DispatchesByDialect(t *testing.T) {
	r := NewConnectorRegistry(nil)
	r.conns["pg-ref"] = &registeredConn{dialect: "postgres", database: "app"}
	r.conns["mysql-ref"] = &registeredConn{dialect: "mysql", database: "app"}
	r.conns["unknown-ref"] = &registeredConn{dialect: "clickhouse"}

	if _, ok := schemaQueries["postgres"]; !ok {
		t.Fatal("expected a \"postgres\" schema query to be registered")
	}
	if _, ok := schemaQueries["mysql"]; !ok {
		t.Fatal("expected a \"mysql\" schema query to be registered")
	}

	// SQLSchema itself needs a real *sql.DB to run the query against, so
	// this only checks the dialect-dispatch failure mode, not a live
	// query — that's covered by the sql-connector plugin's own end-to-end
	// test (internal/adapter/extism/connector_plugin_test.go).
	if _, err := r.SQLSchema(context.Background(), "unknown-ref"); err == nil {
		t.Fatal("expected an error for a connection bound to an unsupported dialect")
	}
}

func TestConnectorRegistry_Bind_UnsupportedDialect(t *testing.T) {
	r := NewConnectorRegistry(nil)
	_, err := r.Bind(context.Background(), ConnectorConfig{Dialect: "clickhouse"})
	if err == nil {
		t.Fatal("expected an error for an unregistered dialect")
	}
}

func TestConnectorRegistry_Release_UnknownRefIsNoop(t *testing.T) {
	r := NewConnectorRegistry(nil)
	r.Release("does-not-exist") // must not panic
}

func TestConnectorRegistry_SQLQuery_UnknownConnRef(t *testing.T) {
	r := NewConnectorRegistry(nil)
	if _, err := r.SQLQuery(context.Background(), "nope", "SELECT 1", nil); err != errUnknownConnRef {
		t.Errorf("err = %v, want errUnknownConnRef", err)
	}
}

func TestConnectorRegistry_SQLExecute_UnknownConnRef(t *testing.T) {
	r := NewConnectorRegistry(nil)
	if _, err := r.SQLExecute(context.Background(), "nope", "DELETE FROM t", nil); err != errUnknownConnRef {
		t.Errorf("err = %v, want errUnknownConnRef", err)
	}
}

// TestConnectorRegistry_SQLExecute_RejectsWriteWithoutAllowWrite is the one
// rule spec-20 §4.3 requires live in the host function: a connection bound
// without allow_write must reject SQLExecute before it ever reaches the
// database — checked here directly against the registry's own map so no
// real *sql.DB is needed to prove the rejection happens first.
func TestConnectorRegistry_SQLExecute_RejectsWriteWithoutAllowWrite(t *testing.T) {
	r := NewConnectorRegistry(nil)
	r.conns["ref-1"] = &registeredConn{allowWrite: false}

	_, err := r.SQLExecute(context.Background(), "ref-1", "DELETE FROM t", nil)
	if err != errWriteNotAllowed {
		t.Errorf("err = %v, want errWriteNotAllowed", err)
	}
}

func TestConnectorRegistry_SQLSchema_UnknownConnRef(t *testing.T) {
	r := NewConnectorRegistry(nil)
	if _, err := r.SQLSchema(context.Background(), "nope"); err != errUnknownConnRef {
		t.Errorf("err = %v, want errUnknownConnRef", err)
	}
}

type fakeKVStore struct {
	values map[string]string
}

func (f *fakeKVStore) Get(_ context.Context, namespace, key string) (string, bool, error) {
	v, ok := f.values[namespace+"/"+key]
	return v, ok, nil
}

func (f *fakeKVStore) Set(_ context.Context, namespace, key, value string) error {
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[namespace+"/"+key] = value
	return nil
}

func TestConnectorRegistry_KVGetSet_NilKVReturnsNotConfigured(t *testing.T) {
	r := NewConnectorRegistry(nil)
	if _, _, err := r.KVGet(context.Background(), "p:1", "k"); err != errNotConfigured {
		t.Errorf("KVGet err = %v, want errNotConfigured", err)
	}
	if err := r.KVSet(context.Background(), "p:1", "k", "v"); err != errNotConfigured {
		t.Errorf("KVSet err = %v, want errNotConfigured", err)
	}
}

func TestConnectorRegistry_KVGetSet_DelegatesToKVStore(t *testing.T) {
	kv := &fakeKVStore{}
	r := NewConnectorRegistry(kv)

	if err := r.KVSet(context.Background(), "plugin:1", "k", "v"); err != nil {
		t.Fatalf("KVSet: %v", err)
	}
	value, ok, err := r.KVGet(context.Background(), "plugin:1", "k")
	if err != nil {
		t.Fatalf("KVGet: %v", err)
	}
	if !ok || value != "v" {
		t.Errorf("KVGet = (%q, %v), want (\"v\", true)", value, ok)
	}
}
