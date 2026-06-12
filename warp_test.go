package warp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadWorkspace(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "warp-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create WORKSPACE.md
	wsMd := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: test-ws
spec:
  projects: ["."]
---
`
	if err := os.WriteFile(filepath.Join(tmpDir, "WORKSPACE.md"), []byte(wsMd), 0644); err != nil {
		t.Fatal(err)
	}

	// Create .agents directory
	agentsDir := filepath.Join(tmpDir, ".agents")
	if err := os.Mkdir(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a Tool in YAML
	toolYaml := `apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: test-tool
spec:
  command: ["ls"]
  description: "test tool"
`
	if err := os.WriteFile(filepath.Join(agentsDir, "test-tool.yaml"), []byte(toolYaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an Agent in Markdown
	agentMd := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: test-agent
spec:
  models: ["gpt-4"]
---
`
	if err := os.WriteFile(filepath.Join(agentsDir, "test-agent.md"), []byte(agentMd), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an MCP in YAML
	mcpYaml := `apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: test-mcp
spec:
  command: ["npx", "mcp-server"]
`
	if err := os.WriteFile(filepath.Join(agentsDir, "test-mcp.yaml"), []byte(mcpYaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a Toolkit in YAML
	toolkitYaml := `apiVersion: warp/v1alpha1
kind: Toolkit
metadata:
  name: test-toolkit
spec:
  tools:
    - $ref: "test-tool.yaml"
`
	if err := os.WriteFile(filepath.Join(agentsDir, "test-toolkit.yaml"), []byte(toolkitYaml), 0644); err != nil {
		t.Fatal(err)
	}

	ws, err := LoadWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}

	projName := strings.ToLower(filepath.Base(tmpDir))
	scoped := ws.Project(projName)

	// Check that resources under the project namespace are present.
	if _, ok := scoped.ResolveResource(NamespaceLocal + "/" + string(KindTool) + "/test-tool"); !ok {
		t.Errorf("test-tool not found in project %q", projName)
	}
	if _, ok := scoped.ResolveResource(NamespaceLocal + "/" + string(KindAgent) + "/test-agent"); !ok {
		t.Errorf("test-agent not found in project %q", projName)
	}

	// Verify kinds via ListResources.
	tools := scoped.ListResources(QueryOptions{Kinds: []Kind{KindTool}})
	if len(tools) == 0 {
		t.Error("expected at least one tool")
	}
	if tools[0].GetKind() != KindTool {
		t.Errorf("expected KindTool, got %v", tools[0].GetKind())
	}

	mcps := scoped.ListResources(QueryOptions{Kinds: []Kind{KindMCP}})
	if len(mcps) == 0 {
		t.Error("expected at least one MCP")
	}
	if mcps[0].GetKind() != KindMCP {
		t.Errorf("expected KindMCP, got %v", mcps[0].GetKind())
	}
}

func TestSlugifyPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"agent.md", "agent"},
		{"tools/ls.yaml", "tools-ls"},
		{"mcp/sqlite.yml", "mcp-sqlite"},
		{"./foo/bar.md", "foo-bar"},
	}

	for _, tc := range tests {
		got := slugifyPath(tc.path)
		if got != tc.expected {
			t.Errorf("slugifyPath(%q) = %q, want %q", tc.path, got, tc.expected)
		}
	}
}

func TestShadowing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "warp-shadow-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create WORKSPACE.md
	wsMd := `---
apiVersion: warp/v1alpha1
kind: Workspace
spec:
  projects: ["proj1"]
---
`
	os.WriteFile(filepath.Join(tmpDir, "WORKSPACE.md"), []byte(wsMd), 0644)

	// Global tool
	globalAgents := filepath.Join(tmpDir, ".agents")
	os.Mkdir(globalAgents, 0755)
	globalTool := `apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: tool
spec:
  command: ["global"]
`
	os.WriteFile(filepath.Join(globalAgents, "tool.yaml"), []byte(globalTool), 0644)

	// Project tool
	projDir := filepath.Join(tmpDir, "proj1")
	os.Mkdir(projDir, 0755)
	projAgents := filepath.Join(projDir, ".agents")
	os.Mkdir(projAgents, 0755)
	projTool := `apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: tool
spec:
  command: ["local"]
`
	os.WriteFile(filepath.Join(projAgents, "tool.yaml"), []byte(projTool), 0644)

	ws, err := LoadWorkspace(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// proj1 resources stored under slug "proj1".
	scoped := ws.Project("proj1")

	// With effective=true, local (proj1) tool shadows workspace tool.
	tools := scoped.ListResources(QueryOptions{
		Kinds:     []Kind{KindTool},
		Effective: true,
	})
	if len(tools) == 0 {
		t.Fatal("no tools found")
	}
	tool, ok := tools[0].(*Tool)
	if !ok {
		t.Fatal("tool is not *Tool")
	}
	if tool.Spec.Command[0] != "local" {
		t.Errorf("expected local tool, got %v", tool.Spec.Command)
	}
}

func TestLoadWorkspace_DefaultProvider(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "warp-default-provider-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	wsMd := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: test-ws
spec:
  projects: ["."]
  defaultProvider: genai
---
`
	if err := os.WriteFile(filepath.Join(tmpDir, "WORKSPACE.md"), []byte(wsMd), 0644); err != nil {
		t.Fatal(err)
	}

	ws, err := LoadWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}

	if got := ws.WorkspaceSpec().Def.Spec.DefaultProvider; got != "genai" {
		t.Fatalf("expected default provider genai, got %q", got)
	}
}

func TestLoadWorkspace_WithResourceProvider_UsesBuiltinsAsFallback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "warp-provider-fallback-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	wsMd := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: test-ws
spec:
  projects: ["."]
---
`
	if err := os.WriteFile(filepath.Join(tmpDir, "WORKSPACE.md"), []byte(wsMd), 0644); err != nil {
		t.Fatal(err)
	}

	provider := NewFSResourceProvider(fstest.MapFS{
		"defs/developer.md": {
			Data: []byte(`---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: developer
spec:
  skills:
    - skills/go.md
---
`),
		},
		"skills/go.md": {
			Data: []byte(`---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: go
---
`),
		},
	})

	ws, err := LoadWorkspace(tmpDir, provider)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}

	if _, ok := ws.ResolveResource(MakeQualifiedName(NamespaceSystem, KindAgent, "developer")); !ok {
		t.Fatal("expected provider developer agent to be loaded")
	}
	if _, ok := ws.ResolveResource(MakeQualifiedName(NamespaceSystem, KindSkill, "go")); !ok {
		t.Fatal("expected provider go skill to be loaded")
	}
}

func TestLoadWorkspace_WithResourceProvider_DoesNotOverrideExistingResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "warp-provider-precedence-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	wsMd := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: test-ws
spec:
  projects: ["proj1"]
---
`
	if err := os.WriteFile(filepath.Join(tmpDir, "WORKSPACE.md"), []byte(wsMd), 0644); err != nil {
		t.Fatal(err)
	}

	globalAgents := filepath.Join(tmpDir, ".agents", "defs")
	if err := os.MkdirAll(globalAgents, 0755); err != nil {
		t.Fatal(err)
	}

	existing := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: developer
  description: workspace override
---
`
	if err := os.WriteFile(filepath.Join(globalAgents, "developer.md"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(tmpDir, "proj1"), 0755); err != nil {
		t.Fatal(err)
	}

	provider := NewFSResourceProvider(fstest.MapFS{
		"defs/developer.md": {
			Data: []byte(`---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: developer
  description: builtin
---
`),
		},
	})

	ws, err := LoadWorkspace(tmpDir, provider)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}

	gotRes, ok := ws.ResolveResource(MakeQualifiedName(NamespaceWorkspace, KindAgent, "developer"))
	if !ok {
		t.Fatal("expected workspace developer agent to be loaded")
	}
	got, ok := gotRes.(*Agent)
	if !ok {
		t.Fatal("expected *Agent")
	}
	if got.Metadata.Description != "workspace override" {
		t.Fatalf("expected workspace resource to keep precedence, got %q", got.Metadata.Description)
	}
}

func TestContextDiscovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "warp-ctx-discovery-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a workspace with a specific project
	wsMd := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: test-ws
spec:
  projects: ["my-proj"]
---
`
	if err := os.WriteFile(filepath.Join(tmpDir, "WORKSPACE.md"), []byte(wsMd), 0644); err != nil {
		t.Fatal(err)
	}

	projDir := filepath.Join(tmpDir, "my-proj")
	if err := os.Mkdir(projDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create AGENT.md in the project
	agentContent := "# Project Instructions"
	if err := os.WriteFile(filepath.Join(projDir, "AGENT.md"), []byte(agentContent), 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}

	// 1. Verify global isolation: base Registry should NOT list the context
	globalCtx := reg.ListResources(QueryOptions{Kinds: []Kind{KindContext}})
	if len(globalCtx) > 0 {
		t.Errorf("Expected 0 Context resources in global registry, got %d", len(globalCtx))
	}

	// 2. Verify project visibility: ScopedRegistry SHOULD list the context
	scoped := reg.Project("my-proj")
	localCtx := scoped.ListResources(QueryOptions{Kinds: []Kind{KindContext}})
	if len(localCtx) == 0 {
		t.Error("Expected Context resource in ScopedRegistry, got 0")
	}

	// 3. Verify resolution via short name
	res, ok := scoped.ResolveResource("agent")
	if !ok {
		t.Fatal("Failed to resolve 'agent' in scoped registry")
	}
	if res.GetKind() != KindContext {
		t.Errorf("Expected KindContext, got %v", res.GetKind())
	}
}
