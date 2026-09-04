package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEmbeddedUdevRulesInSync keeps the copy embedded in the CLI
// byte-identical with the canonical resources/42-ant-usb-sticks.rules.
func TestEmbeddedUdevRulesInSync(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "resources", "42-ant-usb-sticks.rules"))
	if err != nil {
		t.Fatalf("read canonical rules: %v", err)
	}
	if string(embeddedRules) != string(want) {
		t.Error("cmd/goant/42-ant-usb-sticks.rules differs from resources/42-ant-usb-sticks.rules; copy the changes over")
	}
}

func TestRunUdevDryRun(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "42-ant-usb-sticks.rules")
	if err := runUdev([]string{"-dest", dest, "-dry_run"}); err != nil {
		t.Fatalf("runUdev dry run: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("dry run must not write the rules file")
	}
}

func TestRunUdevInstall(t *testing.T) {
	if _, err := exec.LookPath("udevadm"); err == nil {
		t.Skip("udevadm exists; skipping the install test to not touch system state")
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "42-ant-usb-sticks.rules")
	if err := runUdev([]string{"-dest", dest}); err != nil {
		t.Fatalf("runUdev: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read installed rules: %v", err)
	}
	if string(got) != string(embeddedRules) {
		t.Error("installed rules differ from the embedded ones")
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
	}

	// A second run must report "already installed" without rewriting.
	if err := runUdev([]string{"-dest", dest}); err != nil {
		t.Fatalf("second runUdev: %v", err)
	}
}
