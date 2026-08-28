package skillsource

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

const CodeSkillAlreadyInstalled = 110007

// SkillInstaller 是安装动作对资源中心的依赖：把解析好的文件集落 OSS、建
// 资源行、索引文件树——和 zip 上传走同一条 UploadSkill 管线（校验、大小
// 上限、entry 文件检查全部复用），市场安装只是多带一份 installed_from
// 标记。由 *resource.Service 实现。
type SkillInstaller interface {
	UploadSkill(ctx context.Context, ownerID int64, cmd resource.UploadSkillCommand) (resource.Resource, error)
}

// extractZip 安全解包上游安装包：跳过目录项和 zip-slip 路径（../ 或绝对
// 路径）。单文件读取上限与上传管线一致（20MB），总量交给 UploadSkill
// 校验——那里会给出一致的 field error。
func extractZip(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, domain.Invalid(CodeSkillSourceUpstream, "上游返回的安装包不是合法的 zip")
	}
	const cap = 20 << 20
	files := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		path := filepath.Clean(strings.TrimPrefix(f.Name, "/"))
		if path == ".." || strings.HasPrefix(path, "../") || filepath.IsAbs(path) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, domain.Invalid(CodeSkillSourceUpstream, "安装包里的 "+f.Name+" 打不开")
		}
		content, err := io.ReadAll(io.LimitReader(rc, cap+1))
		_ = rc.Close()
		if err != nil {
			return nil, domain.Invalid(CodeSkillSourceUpstream, "读取安装包失败")
		}
		if len(content) > cap {
			return nil, domain.Invalid(CodeSkillSourceUpstream, f.Name+" 超过单个文件 20MB 上限")
		}
		files[path] = content
	}
	return files, nil
}

// Install 把市场里的一个 Skill 装到当前账号下：回源下载 zip、解包、走
// UploadSkill 管线建本地资源，config 里记下 installed_from。ref 直接用
// 上游 slug（clawhub 的 slug 本就满足 ^[a-z][a-z0-9_-]*$；不满足的会在
// 管线里被校验拒绝，不会落库）。
func (s *Service) Install(ctx context.Context, userID, sourceID int64, slug string) (resource.Resource, error) {
	if s.installer == nil {
		return resource.Resource{}, domain.Invalid(CodeSkillSourceUpstream, "这个部署没有配置对象存储，无法安装 Skill")
	}
	cached, err := s.repo.GetMarketSkill(ctx, sourceID, slug)
	if err != nil {
		return resource.Resource{}, err
	}

	zipBytes, err := s.fetch.DownloadZip(ctx, cached.SourceBaseURL, slug, cached.Version)
	if err != nil {
		return resource.Resource{}, domain.Invalid(CodeSkillSourceUpstream, "下载安装包失败："+err.Error())
	}
	files, err := extractZip(zipBytes)
	if err != nil {
		return resource.Resource{}, err
	}

	created, err := s.installer.UploadSkill(ctx, userID, resource.UploadSkillCommand{
		Ref:         slug,
		DisplayName: cached.Name,
		Files:       files,
		Meta: map[string]any{
			"installed_from": map[string]any{
				"source_id":   sourceID,
				"source_name": cached.SourceName,
				"base_url":    cached.SourceBaseURL,
				"slug":        slug,
				"version":     cached.Version,
			},
		},
	})
	if err != nil {
		if derr, ok := domain.AsError(err); ok && derr.Code == domain.CodeResourceRefDuplicate {
			return resource.Resource{}, domain.Conflict(CodeSkillAlreadyInstalled, "已经装过这个 Skill（或 ref 已被占用）")
		}
		return resource.Resource{}, err
	}
	return created, nil
}
