package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_ValidateListRenderFmt(t *testing.T) {
	tmp := t.TempDir()

	// Create WORKSPACE.md
	wsMD := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: test-ws
spec:
  defaultAgent: coder
---
# Test Workspace
`
	if err := os.WriteFile(filepath.Join(tmp, "WORKSPACE.md"), []byte(wsMD), 0644); err != nil {
		t.Fatal(err)
	}

	// Create AGENTS.md
	agentsMD := `---
apiVersion: warp/v1alpha1
kind: Context
metadata:
  name: test-proj
  displayName: Test Project
  description: Test context
---
# Project Instructions

Always write tests.
`
	if err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte(agentsMD), 0644); err != nil {
		t.Fatal(err)
	}

	// Create .agents/agents/coder.md
	agentsDir := filepath.Join(tmp, ".agents", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	coderMD := `---
kind: Agent
apiVersion: warp/v1alpha1
metadata:
  name: coder
spec:
  models:
    - gemini-2.5-pro
---
You are a senior software engineer in {{ .Project.DisplayName }}.
`
	coderPath := filepath.Join(agentsDir, "coder.md")
	if err := os.WriteFile(coderPath, []byte(coderMD), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. Test executeValidate
	var buf bytes.Buffer
	if err := executeValidate(&buf, []string{tmp}); err != nil {
		t.Fatalf("executeValidate failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Validation Successful!") {
		t.Errorf("unexpected validate output: %s", buf.String())
	}

	// 2. Test executeList
	buf.Reset()
	if err := executeList(&buf, []string{tmp}); err != nil {
		t.Fatalf("executeList failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Agent/coder") {
		t.Errorf("expected Agent/coder in list output, got: %s", buf.String())
	}

	// 3. Test executeRender
	buf.Reset()
	if err := executeRender(&buf, []string{"-dir", tmp, "coder"}); err != nil {
		t.Fatalf("executeRender failed: %v", err)
	}
	if !strings.Contains(buf.String(), "You are a senior software engineer in Test Project.") {
		t.Errorf("unexpected render output: %s", buf.String())
	}

	// 4. Test executeFmt (stdout and -w)
	buf.Reset()
	if err := executeFmt(&buf, []string{coderPath}); err != nil {
		t.Fatalf("executeFmt failed: %v", err)
	}
	if !strings.Contains(buf.String(), "apiVersion: warp/v1alpha1") {
		t.Errorf("unexpected fmt output: %s", buf.String())
	}
	if err := executeFmt(&buf, []string{"-w", coderPath}); err != nil {
		t.Fatalf("executeFmt -w failed: %v", err)
	}
}
