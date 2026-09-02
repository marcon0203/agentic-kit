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

// ChannelSource 是 Reloader 从哪里读描述符快照——由 modelcatalog.Service
// 实现（它的 Channels 方法不做权限判定：调用方是进程自己，不是某个用户的
// 请求）。
type ChannelSource interface {
	Channels(ctx context.Context) ([]ChannelRow, error)
}

// ChannelRow 与 modelcatalog.ChannelDescriptor 同形。这里重新声明一遍而不
// 是 import 领域类型，是为了让 Reloader 的依赖方向只有一个：谁提供数据谁
// 满足这个接口。
type ChannelRow struct {
	Key        string
	Descriptor []byte
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
	slog.Info("model_channels_loaded", "count", len(descs), "providers", modelgateway.ProviderNames())
	return nil
}
