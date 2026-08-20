// Package artifact verifies and stages executable kernel artifacts.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/thc1006/claim2kernel/pkg/contract"
)

type Staged struct {
	Path    string
	Size    int64
	Digest  string
	cleanup func() error
}

func (s *Staged) Cleanup() error {
	if s == nil || s.cleanup == nil {
		return nil
	}
	return s.cleanup()
}

// Stage opens the resolved artifact, hashes the exact bytes copied, and writes
// them to a private read/execute-only staging directory. Execution uses the
// staged copy, not the mutable source path, closing the ordinary verify/exec
// TOCTOU window.
func Stage(root string, spec contract.ArtifactSpec) (*Staged, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact root: %w", err)
	}
	if filepath.IsAbs(spec.Path) {
		return nil, fmt.Errorf("artifact path must be relative to root")
	}
	cleanRel := filepath.Clean(spec.Path)
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("artifact path escapes root")
	}
	candidate := filepath.Join(rootReal, cleanRel)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact: %w", err)
	}
	if !within(rootReal, resolved) {
		return nil, fmt.Errorf("resolved artifact escapes root")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact is not a regular file")
	}
	if info.Mode().Perm()&0o111 == 0 {
		return nil, fmt.Errorf("artifact is not executable")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("artifact must not be group- or world-writable")
	}
	if info.Size() != spec.SizeBytes {
		return nil, fmt.Errorf("artifact size mismatch: got %d want %d", info.Size(), spec.SizeBytes)
	}
	if info.Size() > spec.MaxBytes {
		return nil, fmt.Errorf("artifact exceeds maxBytes")
	}
	stageDir, err := os.MkdirTemp("", "c2k-stage-")
	if err != nil {
		return nil, err
	}
	cleanup := func() error { return os.RemoveAll(stageDir) }
	if err := os.Chmod(stageDir, 0o700); err != nil {
		cleanup()
		return nil, err
	}
	stagePath := filepath.Join(stageDir, "kernel")
	out, err := os.OpenFile(stagePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o500)
	if err != nil {
		cleanup()
		return nil, err
	}
	h := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(f, spec.MaxBytes+1))
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		cleanup()
		return nil, fmt.Errorf("stage artifact: %w", copyErr)
	}
	if syncErr != nil {
		cleanup()
		return nil, fmt.Errorf("sync staged artifact: %w", syncErr)
	}
	if closeErr != nil {
		cleanup()
		return nil, fmt.Errorf("close staged artifact: %w", closeErr)
	}
	if written != spec.SizeBytes || written > spec.MaxBytes {
		cleanup()
		return nil, fmt.Errorf("artifact changed while staging or exceeded limit: copied %d", written)
	}
	digest := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if digest != spec.Digest {
		cleanup()
		return nil, fmt.Errorf("artifact digest mismatch: got %s want %s", digest, spec.Digest)
	}
	if err := os.Chmod(stagePath, 0o500); err != nil {
		cleanup()
		return nil, err
	}
	return &Staged{Path: stagePath, Size: written, Digest: digest, cleanup: cleanup}, nil
}

func DigestFile(path string, maxBytes int64) (string, int64, error) {
	if maxBytes <= 0 {
		return "", 0, fmt.Errorf("maxBytes must be > 0")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", 0, err
	}
	if n > maxBytes {
		return "", n, fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), n, nil
}
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
