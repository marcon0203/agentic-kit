package orchestrator

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
)

// skillContentCacheTTL bounds how long a fetched SKILL.md is reused before
// hitting OSS again — long enough that a busy run doesn't re-fetch on every
// tool call, short enough that a re-uploaded Skill's new content shows up
// without a restart (spec-05a: "TTL 5 分钟").
const skillContentCacheTTL = 5 * time.Minute

// skillContentFetcher adapts internal/domain/resource.ObjectStore to
// adk.SkillContentFetcher — a zip-uploaded Skill's SKILL.md, fetched at
// call time and cached briefly in-process.
type skillContentFetcher struct {
	store resource.ObjectStore

	mu    sync.Mutex
	cache map[string]cachedSkillContent
}

type cachedSkillContent struct {
	content   string
	expiresAt time.Time
}

// newSkillContentFetcher returns a disabledSkillContentFetcher when store is
// nil — OSS is optional (OSSEnabled()), and an Agent whose Skill was
// uploaded before OSS was configured (or after it was turned off) needs a
// clear rejection here rather than a nil-pointer panic reaching into the
// run.
func newSkillContentFetcher(store resource.ObjectStore) adk.SkillContentFetcher {
	if store == nil {
		return disabledSkillContentFetcher{}
	}
	return &skillContentFetcher{store: store, cache: map[string]cachedSkillContent{}}
}

func (f *skillContentFetcher) Fetch(ctx context.Context, _, _ int64, ossPrefix string) (string, error) {
	if cached, ok := f.get(ossPrefix); ok {
		return cached, nil
	}

	rc, err := f.store.Get(ctx, ossPrefix+"/"+resource.SkillEntryFile)
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	content, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	f.set(ossPrefix, string(content))
	return string(content), nil
}

func (f *skillContentFetcher) get(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.content, true
}

func (f *skillContentFetcher) set(key, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cache[key] = cachedSkillContent{content: content, expiresAt: time.Now().Add(skillContentCacheTTL)}
}

type disabledSkillContentFetcher struct{}

func (disabledSkillContentFetcher) Fetch(context.Context, int64, int64, string) (string, error) {
	return "", errors.New("对象存储未配置（OSS_*），这个 Agent 引用的 Skill 无法获取其上传内容")
}
