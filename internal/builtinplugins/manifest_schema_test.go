package builtinplugins

import (
	"context"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/dslschema"
)

// 内置插件的清单也得过 schemas/plugin.schema.json。
//
// SeedAll 的另一条测试用的是替身，绕过了真正的校验；而线上播种走
// validateAndStore，schema 不过就整批失败、服务起不来。这条把校验提到编译
// 期之后的第一时间——改坏一个内置清单，在这里就红，而不是等部署时才发现。
func TestBuiltinManifestsPassTheSchema(t *testing.T) {
	validator, err := dslschema.NewPluginValidator()
	if err != nil {
		t.Fatalf("compile plugin schema: %v", err)
	}

	svc := &fakeSeedService{}
	if err := SeedAll(context.Background(), svc); err != nil {
		t.Fatalf("SeedAll: %v", err)
	}
	if len(svc.seeded) == 0 {
		t.Fatal("没有任何内置插件被播种")
	}

	for _, cmd := range svc.seeded {
		fieldErrs, err := validator.Validate(cmd.Manifest)
		if err != nil {
			t.Fatalf("%s: validate: %v", cmd.PluginID, err)
		}
		for _, fe := range fieldErrs {
			t.Errorf("%s 的清单不合 schema: %s — %s", cmd.PluginID, fe.Field, fe.Message)
		}
	}
}
