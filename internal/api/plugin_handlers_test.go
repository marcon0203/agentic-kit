package api

import (
	"bytes"
	"io"
	"testing"
)

func readAll(t *testing.T, r *bytes.Reader) []byte {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read test zip: %v", err)
	}
	return b
}

// The plugin domain rules — signature verification, manifest schema
// validation, the automated wasm-entry gate, OSS storage — are tested
// against the service in internal/domain/plugin. What's left here is the
// one piece of real transport-layer logic this handler adds:
// extractAKP's zip parsing.

func TestExtractAKP_ReadsManifestAndFiles(t *testing.T) {
	r := buildTestZip(t, map[string]string{
		"plugin.json":   `{"id":"acme.charts","version":"1.0.0"}`,
		"plugin.wasm":   "\x00asm",
		"ui/chart.html": "<html></html>",
	})

	manifest, files, err := extractAKP(readAll(t, r))
	if err != nil {
		t.Fatalf("extractAKP: %v", err)
	}
	if manifest["id"] != "acme.charts" || manifest["version"] != "1.0.0" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 non-manifest files, got %d: %+v", len(files), files)
	}
	if string(files["plugin.wasm"]) != "\x00asm" {
		t.Fatalf("unexpected plugin.wasm content: %q", files["plugin.wasm"])
	}
	if _, ok := files["plugin.json"]; ok {
		t.Fatal("expected plugin.json to be pulled out as the manifest, not left in Files")
	}
}

func TestExtractAKP_RequiresManifest(t *testing.T) {
	r := buildTestZip(t, map[string]string{"plugin.wasm": "\x00asm"})

	if _, _, err := extractAKP(readAll(t, r)); err == nil {
		t.Fatal("expected an error for a package with no plugin.json")
	}
}

func TestExtractAKP_RejectsMalformedManifestJSON(t *testing.T) {
	r := buildTestZip(t, map[string]string{"plugin.json": "not json"})

	if _, _, err := extractAKP(readAll(t, r)); err == nil {
		t.Fatal("expected an error for a plugin.json that isn't valid JSON")
	}
}

func TestExtractAKP_RejectsZipSlip(t *testing.T) {
	r := buildTestZip(t, map[string]string{
		"plugin.json":      `{"id":"acme.charts","version":"1.0.0"}`,
		"../../etc/passwd": "pwned",
	})

	if _, _, err := extractAKP(readAll(t, r)); err == nil {
		t.Fatal("expected a zip-slip entry to be rejected")
	}
}

func TestExtractAKP_NotAZip_ReturnsError(t *testing.T) {
	if _, _, err := extractAKP([]byte("this is not a zip file")); err == nil {
		t.Fatal("expected an error for a non-zip payload")
	}
}

func TestValidAssetPath(t *testing.T) {
	valid := []string{"ui/chart.html", "index.html", "assets/icon.png", "a/b/c.js"}
	for _, p := range valid {
		if !validAssetPath(p) {
			t.Errorf("expected %q to be valid", p)
		}
	}

	invalid := []string{"", "/etc/passwd", "../secrets", "ui/../../../etc/passwd", ".."}
	for _, p := range invalid {
		if validAssetPath(p) {
			t.Errorf("expected %q to be rejected", p)
		}
	}
}
