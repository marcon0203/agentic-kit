// Package builtinchannels 是随服务端二进制发布的模型渠道描述符。
//
// 新增一个内置渠道 = 在这里放一份 <id>.json，不用改任何 Go 代码。第三方
// 渠道以后走 spec-20 插件的 extensions.model_providers[]，加载路径不同，
// 但描述符格式和这里完全一样。
//
// fixtures 按 wire（线协议族）组织而不是按渠道：deepseek / 火山方舟 /
// 通义千问 / 自定义端点说的都是 openai.chat.v1，共用一套用例，新增一个同
// 族渠道立刻就有回归覆盖。
package builtinchannels

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/modelgateway/descriptor"
)

//go:embed *.json fixtures/*.json
var files embed.FS

// Load 解析、校验并**跑一遍 fixtures**，返回全部内置渠道描述符。
//
// fixtures 在这里跑而不是只在测试里跑，是有意的：它同时也是第三方渠道的
// 安装期门槛（过不了不给装），内置渠道走同一条路才能保证那条路一直是通
// 的。启动时多花几毫秒，换的是"描述符写错了在启动时炸，而不是在某个用户
// 的对话里炸"。
func Load() ([]*descriptor.Descriptor, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	out := make([]*descriptor.Descriptor, 0, len(names))
	for _, name := range names {
		raw, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		d, err := descriptor.Load(raw)
		if err != nil {
			return nil, fmt.Errorf("内置渠道 %s: %w", name, err)
		}
		if d.ID != strings.TrimSuffix(name, ".json") {
			return nil, fmt.Errorf("内置渠道 %s 的 id 是 %q，必须和文件名一致", name, d.ID)
		}
		fixtures, err := fixturesFor(d.Wire)
		if err != nil {
			return nil, fmt.Errorf("内置渠道 %s: %w", name, err)
		}
		if err := d.Verify(fixtures); err != nil {
			return nil, fmt.Errorf("内置渠道 %s: %w", name, err)
		}
		out = append(out, d)
	}
	return out, nil
}

func fixturesFor(wire string) ([]descriptor.Fixture, error) {
	if wire == "" {
		return nil, nil
	}
	raw, err := files.ReadFile(path.Join("fixtures", wire+".json"))
	if err != nil {
		// 没有 fixtures 不是错误——一个还没人写用例的新线协议族应该能装
		// 上去。它只是没有回归保障，这是渠道作者自己的选择。
		return nil, nil
	}
	var fixtures []descriptor.Fixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		return nil, fmt.Errorf("解析 fixtures/%s.json: %w", wire, err)
	}
	return fixtures, nil
}
