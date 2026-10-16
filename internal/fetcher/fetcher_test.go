package fetcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestFetch_LocalAndGitRefs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// 1. Local directory path bypasses cache and returns absolute path
	localDir := t.TempDir()
	gotLocal, err := Fetch(localDir, "latest")
	if err != nil {
		t.Fatalf("Fetch local dir failed: %v", err)
	}
	if filepath.Clean(gotLocal) != filepath.Clean(localDir) {
		t.Errorf("expected %q, got %q", localDir, gotLocal)
	}

	// 2. Create a local git repository with a tag, branch, and multiple commits
	repoDir := filepath.Join(t.TempDir(), "plugin-repo")
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	writeAndCommit := func(filename, content, msg string) plumbing.Hash {
		if err := os.WriteFile(filepath.Join(repoDir, filename), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(filename); err != nil {
			t.Fatal(err)
		}
		h, err := wt.Commit(msg, &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Test",
				Email: "test@example.com",
				When:  time.Now(),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	commit1 := writeAndCommit("PLUGIN.md", "v1-tag", "initial commit")
	if _, err := repo.CreateTag("v1.0.0", commit1, nil); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	commit2 := writeAndCommit("PLUGIN.md", "v2-commit", "second commit")

	branchRef := plumbing.NewBranchReferenceName("feature-branch")
	if err := wt.Checkout(&git.CheckoutOptions{Branch: branchRef, Create: true}); err != nil {
		t.Fatalf("Checkout branch: %v", err)
	}
	writeAndCommit("PLUGIN.md", "v3-branch", "branch commit")

	// Test tag clone
	tagTarget := filepath.Join(t.TempDir(), "clone-tag")
	if err := cloneAtVersion(repoDir, tagTarget, "v1.0.0"); err != nil {
		t.Fatalf("cloneAtVersion tag failed: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(tagTarget, "PLUGIN.md")); err != nil || string(b) != "v1-tag" {
		t.Errorf("expected 'v1-tag', got %q (err=%v)", string(b), err)
	}

	// Test branch clone
	branchTarget := filepath.Join(t.TempDir(), "clone-branch")
	if err := cloneAtVersion(repoDir, branchTarget, "feature-branch"); err != nil {
		t.Fatalf("cloneAtVersion branch failed: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(branchTarget, "PLUGIN.md")); err != nil || string(b) != "v3-branch" {
		t.Errorf("expected 'v3-branch', got %q (err=%v)", string(b), err)
	}

	// Test commit SHA clone
	shaTarget := filepath.Join(t.TempDir(), "clone-sha")
	if err := cloneAtVersion(repoDir, shaTarget, commit2.String()); err != nil {
		t.Fatalf("cloneAtVersion commit SHA failed: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(shaTarget, "PLUGIN.md")); err != nil || string(b) != "v2-commit" {
		t.Errorf("expected 'v2-commit', got %q (err=%v)", string(b), err)
	}
}
