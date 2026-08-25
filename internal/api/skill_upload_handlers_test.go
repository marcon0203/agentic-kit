package api

import (
	"archive/zip"
	"bytes"
	"testing"
)

func buildTestZip(t *testing.T, entries map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestExtractSkillZip_ReadsRegularFiles(t *testing.T) {
	r := buildTestZip(t, map[string]string{"SKILL.md": "hello", "scripts/run.py": "print(1)"})

	files, err := extractSkillZip(r, r.Size())
	if err != nil {
		t.Fatalf("extractSkillZip: %v", err)
	}
	if len(files) != 2 || string(files["SKILL.md"]) != "hello" || string(files["scripts/run.py"]) != "print(1)" {
		t.Fatalf("unexpected extracted files: %+v", files)
	}
}

func TestExtractSkillZip_RejectsZipSlip(t *testing.T) {
	r := buildTestZip(t, map[string]string{"../../etc/passwd": "pwned"})

	if _, err := extractSkillZip(r, r.Size()); err == nil {
		t.Fatal("expected a zip-slip entry to be rejected")
	}
}

func TestExtractSkillZip_RejectsAbsolutePath(t *testing.T) {
	r := buildTestZip(t, map[string]string{"/etc/passwd": "pwned"})

	if _, err := extractSkillZip(r, r.Size()); err == nil {
		t.Fatal("expected an absolute-path entry to be rejected")
	}
}

func TestExtractSkillZip_NotAZip_ReturnsError(t *testing.T) {
	r := bytes.NewReader([]byte("this is not a zip file"))

	if _, err := extractSkillZip(r, r.Size()); err == nil {
		t.Fatal("expected an error for a non-zip payload")
	}
}
