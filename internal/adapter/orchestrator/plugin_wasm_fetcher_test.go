package orchestrator

import (
	"context"
	"testing"
)

func TestPluginWasmFetcher_FetchesAndCaches(t *testing.T) {
	store := &fakeObjectStore{objects: map[string][]byte{
		"plugins/acme.charts/1.0.0/plugin.wasm": []byte("\x00asm"),
	}}
	fetcher := newPluginWasmFetcher(store)

	content, err := fetcher.Fetch(context.Background(), "plugins/acme.charts/1.0.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(content) != "\x00asm" {
		t.Fatalf("unexpected content: %q", content)
	}

	if _, err := fetcher.Fetch(context.Background(), "plugins/acme.charts/1.0.0"); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if store.gets != 1 {
		t.Fatalf("expected the second Fetch to be served from cache, got %d OSS gets", store.gets)
	}
}

func TestPluginWasmFetcher_MissingObject_ReturnsError(t *testing.T) {
	fetcher := newPluginWasmFetcher(&fakeObjectStore{objects: map[string][]byte{}})
	if _, err := fetcher.Fetch(context.Background(), "plugins/missing/1.0.0"); err == nil {
		t.Fatal("expected an error for a prefix with no plugin.wasm in the store")
	}
}

func TestPluginWasmFetcher_NilStore_ReturnsClearError(t *testing.T) {
	fetcher := newPluginWasmFetcher(nil)
	if fetcher != nil {
		t.Fatal("expected newPluginWasmFetcher(nil) to return a nil *pluginWasmFetcher")
	}
	if _, err := fetcher.Fetch(context.Background(), "plugins/acme.charts/1.0.0"); err == nil {
		t.Fatal("expected a nil fetcher to reject with a clear error")
	}
}
