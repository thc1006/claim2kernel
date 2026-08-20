//go:build linux

package launcher

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestConfigureProcessKillsDescendantsOnTimeout(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "orphan-was-alive")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// The descendant would create the marker after the parent timeout unless the
	// process group is killed as a unit.
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "(sleep 0.5; printf orphan > \"$1\") & sleep 10", "sh", marker)
	configureProcess(cmd)
	_ = cmd.Run()
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("expected deadline, got %v", ctx.Err())
	}
	time.Sleep(650 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("timed-out kernel left a live descendant process")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	// Keep a diagnostic breadcrumb in case slow CI makes this test flaky.
	t.Log("descendant marker absent after " + strconv.FormatInt(time.Now().Unix(), 10))
}
