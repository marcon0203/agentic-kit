package builtinplugins

import (
	"context"
	"errors"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain/plugin"
)

type fakeSeedService struct {
	seeded []plugin.SeedBuiltinCommand
	err    error
}

func (f *fakeSeedService) SeedBuiltin(_ context.Context, cmd plugin.SeedBuiltinCommand) (plugin.Plugin, error) {
	f.seeded = append(f.seeded, cmd)
	if f.err != nil {
		return plugin.Plugin{}, f.err
	}
	return plugin.Plugin{PluginID: cmd.PluginID, Version: cmd.Version, Manifest: cmd.Manifest}, nil
}

func TestSeedAll_LoadsEveryBuiltin(t *testing.T) {
	svc := &fakeSeedService{}
	if err := SeedAll(context.Background(), svc); err != nil {
		t.Fatalf("SeedAll: %v", err)
	}
	if len(svc.seeded) != 4 {
		t.Fatalf("expected 4 built-ins seeded, got %d", len(svc.seeded))
	}

	byID := map[string]plugin.SeedBuiltinCommand{}
	for _, cmd := range svc.seeded {
		byID[cmd.PluginID] = cmd
	}

	pg, ok := byID["agentic-kit.postgres-connector"]
	if !ok {
		t.Fatal("expected agentic-kit.postgres-connector to be seeded")
	}
	if len(pg.Files["plugin.wasm"]) == 0 {
		t.Error("expected the postgres connector's plugin.wasm to be non-empty")
	}

	mysql, ok := byID["agentic-kit.mysql-connector"]
	if !ok {
		t.Fatal("expected agentic-kit.mysql-connector to be seeded")
	}
	if len(mysql.Files["plugin.wasm"]) == 0 {
		t.Error("expected the mysql connector's plugin.wasm to be non-empty")
	}
	// Both connectors are dialect-agnostic at the wasm layer — the
	// dialect lives on the connector resource a user binds, not on the
	// plugin — so they're expected to share the exact same compiled
	// module rather than each shipping their own.
	if string(pg.Files["plugin.wasm"]) != string(mysql.Files["plugin.wasm"]) {
		t.Error("expected the postgres and mysql connectors to share the same wasm module")
	}

	chart, ok := byID["agentic-kit.chart-renderer"]
	if !ok {
		t.Fatal("expected agentic-kit.chart-renderer to be seeded")
	}
	if len(chart.Files["ui/chart.html"]) == 0 {
		t.Error("expected the chart renderer's ui/chart.html to be non-empty")
	}
	// The chart renderer is now a real tools[] call (spec-20 §4.2 method
	// A, render_chart) rather than a frontend-only auto_render match, so
	// it ships a wasm module like the connectors do — the export does no
	// real computation, but the call still has to be real.
	if len(chart.Files["plugin.wasm"]) == 0 {
		t.Error("expected the chart renderer's plugin.wasm to be non-empty")
	}

	for id, cmd := range byID {
		if manifestID, _ := cmd.Manifest["id"].(string); manifestID != id {
			t.Errorf("manifest id %q does not match PluginID %q", manifestID, id)
		}
		if cmd.Version == "" {
			t.Errorf("%s: expected a non-empty version", id)
		}
	}
}

func TestSeedAll_PropagatesServiceError(t *testing.T) {
	svc := &fakeSeedService{err: errBoom}
	if err := SeedAll(context.Background(), svc); err == nil {
		t.Fatal("expected SeedAll to propagate the service's error")
	}
}

var errBoom = errors.New("boom")

// 阿里云 OSS 插件和连接器不一样：它自己持有凭据（用户安装时填），而不是
// 靠宿主注入一个连接句柄。所以这里盯两件事——凭据项确实声明在
// config_schema 里，以及那个 secret 项的键名让宿主认得出是凭据。
//
// 后一条不是形式检查：宿主只按键名决定加不加密，键名不对的话
// secret: true 就是一句没有任何效果的声明，AccessKey 会明文落库并出现在
// 每个响应里。
func TestSeedAll_AliyunOSSDeclaresItsCredentials(t *testing.T) {
	svc := &fakeSeedService{}
	if err := SeedAll(context.Background(), svc); err != nil {
		t.Fatalf("SeedAll: %v", err)
	}
	var oss plugin.SeedBuiltinCommand
	for _, cmd := range svc.seeded {
		if cmd.PluginID == "agentic-kit.aliyun-oss" {
			oss = cmd
		}
	}
	if oss.PluginID == "" {
		t.Fatal("expected agentic-kit.aliyun-oss to be seeded")
	}
	if len(oss.Files["plugin.wasm"]) == 0 {
		t.Fatal("expected the aliyun-oss plugin.wasm to be non-empty")
	}

	fields := plugin.ConfigSchema(oss.Manifest)
	if len(fields) == 0 {
		t.Fatal("阿里云 OSS 必须声明安装时要填什么，否则安装界面问不出来")
	}
	byKey := map[string]plugin.ConfigField{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	for _, want := range []string{"endpoint", "bucket", "access_key_id", "access_key_secret"} {
		f, ok := byKey[want]
		if !ok {
			t.Errorf("config_schema 缺少 %s", want)
			continue
		}
		if !f.Required {
			t.Errorf("%s 应该是必填", want)
		}
	}
	secret := byKey["access_key_secret"]
	if !secret.Secret {
		t.Error("access_key_secret 必须声明为 secret")
	}
	if !plugin.IsCredentialKey(secret.Key) {
		t.Error("声明为 secret 的键名必须让宿主认得出是凭据，否则不会被加密")
	}
	// AccessKey ID 不是秘密（相当于用户名），不该被当成凭据抹掉——抹了的话
	// 用户在配置页面上永远看不到自己填的是哪个 AK。
	if plugin.IsCredentialKey("access_key_id") {
		t.Error("access_key_id 不该被当作凭据")
	}

	// 网络白名单必须声明，否则 Extism 默认不给任何出网，插件一调就失败。
	requires, _ := oss.Manifest["requires"].(map[string]any)
	network, _ := requires["network"].([]any)
	if len(network) == 0 {
		t.Error("阿里云 OSS 需要出网，requires.network 不能为空")
	}
}
