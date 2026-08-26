package orchestrator

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// pluginWasmCacheTTL mirrors skillContentCacheTTL's reasoning exactly
// (spec-20 §4.1: "从 OSS 拉 plugin.wasm，走 Skill 那轮已有的 5 分钟 TTL
// 缓存") — long enough a busy run doesn't re-fetch per call, short enough
// a re-uploaded version's bytes show up without a restart.
const pluginWasmCacheTTL = 5 * time.Minute

// pluginAssetGetter is the one OSS method plugin wasm fetching needs —
// satisfied by the same object store the Skill upload feature already
// wires (internal/adapter/oss.Store), passed in as resource.ObjectStore
// since that interface already includes Get.
type pluginAssetGetter interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// pluginWasmFetcher fetches one plugin version's plugin.wasm content from
// OSS, cached briefly in-process — the run-time half of what
// plugin.Service.Upload wrote at OSSPrefix+"/plugin.wasm".
type pluginWasmFetcher struct {
	store pluginAssetGetter

	mu    sync.Mutex
	cache map[string][]byte
	exp   map[string]time.Time
}

// newPluginWasmFetcher returns nil when store is nil (OSS not configured)
// — Fetch on a nil *pluginWasmFetcher returns a clear error instead of a
// nil-pointer panic, the same nil-receiver-is-safe convention this file's
// sibling (skillContentFetcher's disabled variant) uses a distinct type
// for; a method value works just as well here since there's only one
// method to guard.
func newPluginWasmFetcher(store pluginAssetGetter) *pluginWasmFetcher {
	if store == nil {
		return nil
	}
	return &pluginWasmFetcher{store: store, cache: map[string][]byte{}, exp: map[string]time.Time{}}
}

func (f *pluginWasmFetcher) Fetch(ctx context.Context, ossPrefix string) ([]byte, error) {
	if f == nil {
		return nil, errors.New("对象存储未配置（OSS_*），插件的 wasm 内容无法获取")
	}
	if cached, ok := f.get(ossPrefix); ok {
		return cached, nil
	}

	rc, err := f.store.Get(ctx, ossPrefix+"/plugin.wasm")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	content, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	f.set(ossPrefix, content)
	return content, nil
}

func (f *pluginWasmFetcher) get(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	exp, ok := f.exp[key]
	if !ok || time.Now().After(exp) {
		return nil, false
	}
	return f.cache[key], true
}

func (f *pluginWasmFetcher) set(key string, content []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cache[key] = content
	f.exp[key] = time.Now().Add(pluginWasmCacheTTL)
}
