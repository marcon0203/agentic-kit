// Package modelchannels 把 modelcatalog（管理员登记的模型提供商）和
// modelgateway（真正发请求的渠道注册表）接起来。
//
// 放在 adapter 层是因为它两边都要 import：领域层只认自己的端口
// （ChannelTemplates / ChannelReloader），不该知道 modelgateway 的存在。
package modelchannels

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/marcon0203/agentic-kit/internal/channeltemplates"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
	"github.com/marcon0203/agentic-kit/internal/modelgateway/descriptor"
)

// Templates 实现 modelcatalog.ChannelTemplates。
type Templates struct{}

func NewTemplates() Templates { return Templates{} }

func (Templates) Instantiate(templateID, key, label, baseURL string) ([]byte, error) {
	_, rendered, err := channeltemplates.Instantiate(templateID, key, label, baseURL)
	return rendered, err
}

// ChannelSource 是 Reloader 从哪里读描述符快照和模型参数——由
// modelcatalog.Service 实现（它的 Channels / ChannelModelParams 方法不做
// 权限判定：调用方是进程自己，不是某个用户的请求）。
type ChannelSource interface {
	Channels(ctx context.Context) ([]ChannelRow, error)
	// ChannelModelParams 返回每渠道每模型的请求参数取值。实现方读失败时
	// 可以返回 nil——参数表丢了只是退回"没配参数"，渠道本身还可用。
	ChannelModelParams(ctx context.Context) ([]ModelParamsRow, error)
}

// ChannelRow 与 modelcatalog.ChannelDescriptor 同形。这里重新声明一遍而不
// 是 import 领域类型，是为了让 Reloader 的依赖方向只有一个：谁提供数据谁
// 满足这个接口。
type ChannelRow struct {
	Key        string
	Descriptor []byte
}

// ModelParamsRow 与 modelcatalog.ChannelModelParams 同形，理由同上。
type ModelParamsRow struct {
	ProviderKey string
	Model       string
	Params      map[string]any
}

// Reloader 实现 modelcatalog.ChannelReloader：从库里读全量启用中的描述符
// 快照，整体替换 modelgateway 的渠道注册表。
//
// 整体替换而不是增量同步：每次都读全量，注册表和库不会出现"某次删除漏掉
// 了"的偏差。
type Reloader struct {
	source ChannelSource
}

func NewReloader(source ChannelSource) *Reloader { return &Reloader{source: source} }

func (r *Reloader) Reload(ctx context.Context) error {
	rows, err := r.source.Channels(ctx)
	if err != nil {
		slog.Error("model_channels_reload_failed", "err", err)
		return fmt.Errorf("modelchannels: 读取渠道描述符失败: %w", err)
	}

	descs := make([]*descriptor.Descriptor, 0, len(rows))
	for _, row := range rows {
		d, err := channeltemplates.LoadStored(row.Descriptor)
		if err != nil {
			// 一份坏掉的描述符不该让其它渠道跟着不可用：跳过它，把理由记
			// 下来。管理员在页面上看到"这个提供商调不通"时，日志里有原因。
			slog.Error("model_channel_descriptor_invalid", "provider", row.Key, "err", err)
			continue
		}
		descs = append(descs, d)
	}
	modelgateway.SetChannels(descs)

	// 模型参数读失败不拦注册表更新——渠道描述符是"能不能调"的问题，参数
	// 只是"怎么调"；后者退回空表（缺必填参数会在调用前被明确拦下），前者
	// 卡住会让所有渠道一起消失。
	params := map[string]map[string]map[string]any{}
	if paramRows, err := r.source.ChannelModelParams(ctx); err != nil {
		slog.Error("model_channel_params_load_failed", "err", err)
	} else {
		for _, row := range paramRows {
			models, ok := params[row.ProviderKey]
			if !ok {
				models = map[string]map[string]any{}
				params[row.ProviderKey] = models
			}
			models[row.Model] = row.Params
		}
	}
	modelgateway.SetModelParams(params)

	slog.Info("model_channels_loaded", "count", len(descs), "providers", modelgateway.ProviderNames())
	return nil
}

// Directory 实现 agent.ChannelDirectory：Agent 保存时用它校验
// model.provider / model.fallback[] 引用的渠道都已登记。读的是同一个运行
// 时注册表，所以管理员刚建好的渠道立刻就能被引用。
type Directory struct{}

func (Directory) ProviderNames() []string { return modelgateway.ProviderNames() }
