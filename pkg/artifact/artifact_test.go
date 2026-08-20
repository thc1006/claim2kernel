package artifact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thc1006/claim2kernel/pkg/contract"
)

func makeArtifact(t *testing.T, root, name, body string, mode os.FileMode) contract.ArtifactSpec {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	d, n, err := DigestFile(p, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return contract.ArtifactSpec{Path: name, Digest: d, SizeBytes: n, MaxBytes: 1 << 20, Protocol: "stdin-json-v1"}
}
func TestStage(t *testing.T) {
	root := t.TempDir()
	spec := makeArtifact(t, root, "kernel", "#!/bin/sh\nexit 0\n", 0o500)
	s, err := Stage(root, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Cleanup()
	if _, err := os.Stat(s.Path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Path, "c2k-stage-") {
		t.Fatalf("not staged: %s", s.Path)
	}
}
func TestRejectDigest(t *testing.T) {
	root := t.TempDir()
	spec := makeArtifact(t, root, "kernel", "x", 0o500)
	spec.Digest = "sha256:" + strings.Repeat("0", 64)
	if _, err := Stage(root, spec); err == nil {
		t.Fatal("expected digest error")
	}
}
func TestRejectWritable(t *testing.T) {
	root := t.TempDir()
	spec := makeArtifact(t, root, "kernel", "x", 0o522)
	if _, err := Stage(root, spec); err == nil {
		t.Fatal("expected writable rejection")
	}
}
func TestRejectEscape(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	spec := makeArtifact(t, other, "kernel", "x", 0o500)
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(other, "kernel"), link); err != nil {
		t.Fatal(err)
	}
	spec.Path = "link"
	if _, err := Stage(root, spec); err == nil {
		t.Fatal("expected escape rejection")
	}
}
