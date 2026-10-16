package hasher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirHashAndFileHash(t *testing.T) {
	dir := t.TempDir()

	fileA := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(fileA, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Hidden files outside .agents should be ignored
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main"), 0644); err != nil {
		t.Fatal(err)
	}

	// Files inside .agents should be included
	agentsDir := filepath.Join(dir, ".agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "skill.md"), []byte("skill"), 0644); err != nil {
		t.Fatal(err)
	}

	fh, err := FileHash(fileA)
	if err != nil {
		t.Fatalf("FileHash failed: %v", err)
	}
	if !strings.HasPrefix(fh, "h1:") {
		t.Errorf("expected h1: prefix, got %q", fh)
	}

	dh1, err := DirHash(dir)
	if err != nil {
		t.Fatalf("DirHash failed: %v", err)
	}
	dh2, err := DirHash(dir)
	if err != nil {
		t.Fatalf("second DirHash failed: %v", err)
	}
	if dh1 != dh2 {
		t.Errorf("expected deterministic DirHash, got %q vs %q", dh1, dh2)
	}

	// Modifying a file inside .agents must change the hash
	if err := os.WriteFile(filepath.Join(agentsDir, "skill.md"), []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}
	dh3, err := DirHash(dir)
	if err != nil {
		t.Fatalf("third DirHash failed: %v", err)
	}
	if dh1 == dh3 {
		t.Error("expected DirHash to change when file inside .agents changes")
	}
}
