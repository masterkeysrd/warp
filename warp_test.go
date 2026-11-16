package warp

import (
	"context"
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
		{"agents.md", "agents"},
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
		"agents/developer.md": {
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
	}, "")

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

	globalAgents := filepath.Join(tmpDir, ".agents", "agents")
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
		"agents/developer.md": {
			Data: []byte(`---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: developer
  description: builtin
---
`),
		},
	}, "")

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

	// Create AGENTS.md in the project
	agentContent := "# Project Instructions"
	if err := os.WriteFile(filepath.Join(projDir, "AGENTS.md"), []byte(agentContent), 0644); err != nil {
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
	res, ok := scoped.ResolveResource("agents")
	if !ok {
		t.Fatal("Failed to resolve 'agents' in scoped registry")
	}
	if res.GetKind() != KindContext {
		t.Errorf("Expected KindContext, got %v", res.GetKind())
	}
}

func TestLoadWorkspace_IgnoresMissingFrontMatter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "warp-test-missing-fm-*")
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

	agentsDir := filepath.Join(tmpDir, ".agents")
	if err := os.Mkdir(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Valid agent
	agentMd := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: valid-agent
---
`
	if err := os.WriteFile(filepath.Join(agentsDir, "valid-agent.md"), []byte(agentMd), 0644); err != nil {
		t.Fatal(err)
	}

	// Markdown file missing YAML front matter completely
	noFmMd := `# Reference Doc
This is just a reference doc.

## Table
| a | b |
|---|---|
| 1 | 2 |

---
Horizontal rule
`
	if err := os.WriteFile(filepath.Join(agentsDir, "README.md"), []byte(noFmMd), 0644); err != nil {
		t.Fatal(err)
	}

	// Malformed file (invalid YAML)
	malformedMd := `---
kind: Agent
apiVersion: warp/v1alpha1
malformed: [
---
# Invalid yaml
`
	if err := os.WriteFile(filepath.Join(agentsDir, "malformed.md"), []byte(malformedMd), 0644); err != nil {
		t.Fatal(err)
	}

	// It should load successfully, ignoring README.md and emitting warning for malformed.md
	ws, err := LoadWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}

	projName := strings.ToLower(filepath.Base(tmpDir))
	scoped := ws.Project(projName)

	if _, ok := scoped.ResolveResource(NamespaceLocal + "/" + string(KindAgent) + "/valid-agent"); !ok {
		t.Errorf("valid-agent not found in project %q", projName)
	}

	// Verify that a warning was emitted for the malformed file
	warnings := ws.Warnings()
	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "malformed.md") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Errorf("Expected a warning for malformed.md, but got: %v", warnings)
	}
}

func TestFlatRootAuthorityRule(t *testing.T) {
	tmpDir := t.TempDir()

	wsMd := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: flat-ws
spec:
  projects: ["."]
---
`
	if err := os.WriteFile(filepath.Join(tmpDir, "WORKSPACE.md"), []byte(wsMd), 0644); err != nil {
		t.Fatal(err)
	}

	agentsDir := filepath.Join(tmpDir, ".agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillMd := `---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: root-skill
---
`
	if err := os.WriteFile(filepath.Join(agentsDir, "root-skill.md"), []byte(skillMd), 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}

	// Workspace-global namespace must be empty for flat-root project
	if _, ok := reg.ResolveResource(MakeQualifiedName(NamespaceWorkspace, KindSkill, "root-skill")); ok {
		t.Error("expected workspace-global namespace to be empty when only project is '.'")
	}

	projSlug := strings.ToLower(filepath.Base(tmpDir))
	res, ok := reg.Project(projSlug).ResolveResource("local/Skill/root-skill")
	if !ok {
		t.Fatal("expected root-skill to be loaded into project local namespace")
	}
	if res.GetNamespace() != projSlug {
		t.Errorf("expected namespace %q, got %q", projSlug, res.GetNamespace())
	}
}

func TestInstallAndLoadPlugin_WithLockVerification(t *testing.T) {
	pluginDir := t.TempDir()
	resDir := filepath.Join(pluginDir, "resources", "skills")
	if err := os.MkdirAll(resDir, 0755); err != nil {
		t.Fatal(err)
	}

	pluginManifest := `---
apiVersion: warp/v1alpha1
kind: Plugin
metadata:
  name: acme-plugin
spec:
  resourceDir: "resources"
  exports:
    - "Skill/public-*"
---
`
	if err := os.WriteFile(filepath.Join(pluginDir, "PLUGIN.md"), []byte(pluginManifest), 0644); err != nil {
		t.Fatal(err)
	}

	pubSkill := `---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: public-one
  description: Exposed skill
---
`
	privSkill := `---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: private-one
  description: Private skill
---
`
	if err := os.WriteFile(filepath.Join(resDir, "public.md"), []byte(pubSkill), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "private.md"), []byte(privSkill), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. DiscoverPluginResources should respect spec.exports
	discovered, _, err := DiscoverPluginResources(pluginDir, "latest")
	if err != nil {
		t.Fatalf("DiscoverPluginResources failed: %v", err)
	}
	if len(discovered) != 1 || discovered[0].Name != "public-one" {
		t.Fatalf("expected only [public-one] to be discovered, got %+v", discovered)
	}

	// 2. InstallPlugin into a workspace and LoadWorkspace
	wsDir := t.TempDir()
	if err := InstallPlugin(wsDir, pluginDir, "latest", []string{"Skill/public-one"}); err != nil {
		t.Fatalf("InstallPlugin failed: %v", err)
	}

	reg, err := LoadWorkspace(wsDir)
	if err != nil {
		t.Fatalf("LoadWorkspace with plugin failed: %v", err)
	}

	if _, ok := reg.ResolveResource("public-one"); !ok {
		t.Error("expected public-one skill from plugin to be resolved in workspace")
	}
	if _, ok := reg.ResolveResource("private-one"); ok {
		t.Error("did not expect unexported private-one skill to be loaded")
	}

	// 3. Tamper with plugin resource after locking -> LoadWorkspace must fail hash check
	tampered := pubSkill + "\n# Tampered content\n"
	if err := os.WriteFile(filepath.Join(resDir, "public.md"), []byte(tampered), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWorkspace(wsDir); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("expected hash mismatch error after tampering, got: %v", err)
	}
}

func TestLoadRulesFromDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .agents/rules/
	rulesDir := filepath.Join(tmpDir, ".agents", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	ruleMd := `---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: repo-lint
  description: Repository linting rule
spec:
  priority: 50
  triggers:
    always: true
---
# Linting
Always run gofmt.
`
	if err := os.WriteFile(filepath.Join(rulesDir, "lint.md"), []byte(ruleMd), 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}

	projSlug := strings.ToLower(filepath.Base(tmpDir))
	scoped := reg.Project(projSlug)
	res, ok := scoped.ResolveResource("repo-lint")
	if !ok {
		t.Fatal("expected repo-lint rule to be resolved in scoped registry")
	}
	rl, ok := res.(*Rule)
	if !ok {
		t.Fatalf("expected *Rule, got %T", res)
	}
	if rl.Spec.Priority != 50 {
		t.Errorf("expected priority 50, got %d", rl.Spec.Priority)
	}
	if !rl.Spec.Triggers.Always {
		t.Errorf("expected Always == true")
	}

	// Verify ResolveRulesForPath picks it up
	rules := scoped.ResolveRulesForPath("cmd/main.go")
	if len(rules) != 1 || rules[0].GetName() != "repo-lint" {
		t.Errorf("expected [repo-lint], got %v", rules)
	}
}

func TestLoadWithOptions_UserProvidersAndSubprojectDiscovery(t *testing.T) {
	t.Run("UserProviders shadowing and missing dir skip", func(t *testing.T) {
		wsDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(wsDir, ".agents", "skills"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte("---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: ws\nspec:\n  projects: [\".\"]\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wsDir, ".agents", "skills", "local-only.md"), []byte("---\napiVersion: warp/v1alpha1\nkind: Skill\nmetadata:\n  name: shared-skill\n  description: local version\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}

		userFS := fstest.MapFS{
			"skills/shared.md": &fstest.MapFile{Data: []byte("---\napiVersion: warp/v1alpha1\nkind: Skill\nmetadata:\n  name: shared-skill\n  description: user version\n---\n")},
			"skills/user.md":   &fstest.MapFile{Data: []byte("---\napiVersion: warp/v1alpha1\nkind: Skill\nmetadata:\n  name: user-skill\n  description: from user\n---\n")},
		}
		sysFS := fstest.MapFS{
			"skills/user.md": &fstest.MapFile{Data: []byte("---\napiVersion: warp/v1alpha1\nkind: Skill\nmetadata:\n  name: user-skill\n  description: from system\n---\n")},
			"skills/sys.md":  &fstest.MapFile{Data: []byte("---\napiVersion: warp/v1alpha1\nkind: Skill\nmetadata:\n  name: sys-skill\n  description: from system\n---\n")},
		}

		missingDir := filepath.Join(t.TempDir(), "does-not-exist")
		reg, err := LoadWithOptions(context.Background(), wsDir, LoadOptions{
			UserProviders: []ResourceProvider{
				NewFSResourceProvider(os.DirFS(missingDir), missingDir),
				NewFSResourceProvider(userFS, "/xdg/user/agents"),
			},
			SystemProviders: []ResourceProvider{
				NewFSResourceProvider(sysFS, "builtin://"),
			},
		})
		if err != nil {
			t.Fatalf("LoadWithOptions failed: %v", err)
		}

		// WorkspaceDef.Directory must be hydrated with cleaned RootPath
		wsSpec := reg.WorkspaceSpec()
		if wsSpec == nil || wsSpec.Def == nil || wsSpec.Def.Directory != filepath.Clean(wsSpec.RootPath) {
			t.Fatalf("expected Def.Directory == cleaned RootPath (%q), got %+v", wsSpec.RootPath, wsSpec.Def)
		}

		// user-skill shadows system -> user version wins at base Registry
		resUser, ok := reg.ResolveResource("user-skill")
		if !ok || resUser.GetNamespace() != NamespaceUser || resUser.GetMetadata().Description != "from user" {
			t.Errorf("expected user-skill from NamespaceUser, got ok=%v res=%+v", ok, resUser)
		}

		// shared-skill in local project shadows user version in ScopedRegistry
		projSlug := strings.ToLower(filepath.Base(wsDir))
		scoped := reg.Project(projSlug)
		resShared, ok := scoped.ResolveResource("shared-skill")
		if !ok || resShared.GetNamespace() != projSlug || resShared.GetMetadata().Description != "local version" {
			t.Errorf("expected local shared-skill to shadow user, got ok=%v res=%+v", ok, resShared)
		}

		// sys-skill still available from NamespaceSystem
		resSys, ok := scoped.ResolveResource("sys-skill")
		if !ok || resSys.GetNamespace() != NamespaceSystem {
			t.Errorf("expected sys-skill from NamespaceSystem, got ok=%v res=%+v", ok, resSys)
		}
	})

	t.Run("AutoDiscoverStartSubproject preserves root .agents in NamespaceWorkspace and reloads", func(t *testing.T) {
		wsRoot := t.TempDir()
		if err := os.WriteFile(filepath.Join(wsRoot, "WORKSPACE.md"), []byte("---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: monorepo\nspec:\n  projects: [\".\"]\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(wsRoot, ".agents", "skills"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wsRoot, ".agents", "skills", "root.md"), []byte("---\napiVersion: warp/v1alpha1\nkind: Skill\nmetadata:\n  name: root-skill\n  description: root workspace skill\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}

		subDir := filepath.Join(wsRoot, "packages", "foo")
		if err := os.MkdirAll(filepath.Join(subDir, ".agents", "skills"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, "AGENTS.md"), []byte("# Foo Package Context\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, ".agents", "skills", "foo.md"), []byte("---\napiVersion: warp/v1alpha1\nkind: Skill\nmetadata:\n  name: foo-skill\n  description: subproject skill\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}

		userFS := fstest.MapFS{
			"skills/user.md": &fstest.MapFile{Data: []byte("---\napiVersion: warp/v1alpha1\nkind: Skill\nmetadata:\n  name: user-only\n---\n")},
		}

		// Without AutoDiscoverStartSubproject, packages/foo is ignored.
		regNoAuto, err := LoadWithOptions(context.Background(), subDir, LoadOptions{
			AutoDiscoverStartSubproject: false,
		})
		if err != nil {
			t.Fatalf("LoadWithOptions(false) failed: %v", err)
		}
		if _, ok := regNoAuto.GetProject("packages-foo"); ok {
			t.Fatalf("did not expect packages-foo project when AutoDiscoverStartSubproject is false")
		}

		// With AutoDiscoverStartSubproject: true
		reg, err := LoadWithOptions(context.Background(), subDir, LoadOptions{
			AutoDiscoverStartSubproject: true,
			UserProviders: []ResourceProvider{
				NewFSResourceProvider(userFS, ""),
			},
		})
		if err != nil {
			t.Fatalf("LoadWithOptions(true) failed: %v", err)
		}

		// Root .agents/ must be preserved in NamespaceWorkspace
		if res, ok := reg.ResolveResource(MakeQualifiedName(NamespaceWorkspace, KindSkill, "root-skill")); !ok || res.GetNamespace() != NamespaceWorkspace {
			t.Fatalf("expected root-skill in NamespaceWorkspace, got ok=%v res=%+v", ok, res)
		}

		// Subproject must be registered and discoverable via ProjectFromPath
		proj, ok := reg.ProjectFromPath(subDir)
		if !ok || proj.Name != "packages-foo" || proj.Context == nil {
			t.Fatalf("expected packages-foo project with Context, got ok=%v proj=%+v", ok, proj)
		}

		scoped := reg.Project("packages-foo")
		if _, ok := scoped.ResolveResource("foo-skill"); !ok {
			t.Errorf("expected foo-skill in packages-foo scope")
		}
		if _, ok := scoped.ResolveResource("root-skill"); !ok {
			t.Errorf("expected root-skill inherited from NamespaceWorkspace in packages-foo scope")
		}
		if _, ok := scoped.ResolveResource("user-only"); !ok {
			t.Errorf("expected user-only inherited from NamespaceUser in packages-foo scope")
		}

		// Reload() must preserve LoadOptions (UserProviders + AutoDiscoverStartSubproject)
		if err := os.WriteFile(filepath.Join(subDir, ".agents", "skills", "foo2.md"), []byte("---\napiVersion: warp/v1alpha1\nkind: Skill\nmetadata:\n  name: foo2-skill\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}
		stats, err := reg.Reload()
		if err != nil {
			t.Fatalf("Reload failed: %v", err)
		}
		if stats.Total.Added != 1 {
			t.Errorf("expected 1 added resource after Reload, got %+v", stats)
		}
		if _, ok := reg.Project("packages-foo").ResolveResource("foo2-skill"); !ok {
			t.Errorf("expected foo2-skill after Reload")
		}
		if _, ok := reg.ResolveResource("user-only"); !ok {
			t.Errorf("expected user-only preserved after Reload")
		}

		// Subdirectory without .agents/ or AGENTS.md must not create an extra project
		plainSub := filepath.Join(wsRoot, "internal", "util")
		if err := os.MkdirAll(plainSub, 0755); err != nil {
			t.Fatal(err)
		}
		regPlain, err := LoadWithOptions(context.Background(), plainSub, LoadOptions{
			AutoDiscoverStartSubproject: true,
		})
		if err != nil {
			t.Fatalf("LoadWithOptions(plainSub) failed: %v", err)
		}
		if _, ok := regPlain.GetProject("internal-util"); ok {
			t.Errorf("did not expect internal-util project without .agents/ or AGENTS.md")
		}
	})
}
