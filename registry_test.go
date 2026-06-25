package warp

import (
	"strings"
	"testing"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func makeSkillResource(ns, name string) *Skill {
	return &Skill{BaseResource: BaseResource{
		APIVersion: "warp/v1alpha1",
		Kind:       KindSkill,
		Metadata:   Metadata{Name: name},
		Namespace:  ns,
	}}
}

func makeCommandResource(ns, name string) *Command {
	return &Command{BaseResource: BaseResource{
		APIVersion: "warp/v1alpha1",
		Kind:       KindCommand,
		Metadata:   Metadata{Name: name},
		Namespace:  ns,
	}}
}

// buildRegistry returns a Registry with resources across several namespaces:
//
//	services-api/Skill/python   (project-local)
//	workspace/Skill/python      (workspace-global)
//	system/Skill/python         (system built-in)
//	services-api/Skill/finance  (project-local)
//	workspace/Command/build     (workspace-global)
func buildRegistry() *Registry {
	reg := NewRegistry(nil)
	for _, r := range []Resource{
		makeSkillResource("services-api", "python"),
		makeSkillResource(NamespaceWorkspace, "python"),
		makeSkillResource(NamespaceSystem, "python"),
		makeSkillResource("services-api", "finance"),
		makeCommandResource(NamespaceWorkspace, "build"),
	} {
		reg.set(r.QualifiedName(), r)
	}
	return reg
}

// ─── TestScopedRegistry_AliasSwap ────────────────────────────────────────────

// TestScopedRegistry_AliasSwap verifies that "local/<Kind>/<name>" on a
// registry scoped to services-api correctly retrieves the resource stored
// internally as "services-api/<Kind>/<name>".
func TestScopedRegistry_AliasSwap(t *testing.T) {
	scoped := buildRegistry().Project("services-api")

	ref := NamespaceLocal + "/" + string(KindSkill) + "/finance"
	res, ok := scoped.ResolveResource(ref)
	if !ok {
		t.Fatalf("ResolveResource(%q): not found", ref)
	}
	if res.GetNamespace() != "services-api" {
		t.Errorf("expected namespace %q, got %q", "services-api", res.GetNamespace())
	}
	if res.GetName() != "finance" {
		t.Errorf("expected name %q, got %q", "finance", res.GetName())
	}
}

// ─── TestScopedRegistry_ShortName ────────────────────────────────────────────

// TestScopedRegistry_ShortName verifies that a short name resolves to the
// services-api namespace rather than workspace or system.
func TestScopedRegistry_ShortName(t *testing.T) {
	scoped := buildRegistry().Project("services-api")

	res, ok := scoped.ResolveResource("python")
	if !ok {
		t.Fatal("ResolveResource(\"python\"): not found")
	}
	if res.GetNamespace() != "services-api" {
		t.Errorf("expected services-api to win, got %q", res.GetNamespace())
	}
}

// ─── TestBaseRegistry_Isolation ──────────────────────────────────────────────

// TestBaseRegistry_Isolation verifies that the base Registry never returns
// project-specific resources for a short-name query.
func TestBaseRegistry_Isolation(t *testing.T) {
	reg := buildRegistry()

	res, ok := reg.ResolveResource("python")
	if !ok {
		t.Fatal("ResolveResource(\"python\"): not found")
	}
	// workspace (priority 80) must beat system (40); services-api must be excluded.
	if res.GetNamespace() != NamespaceWorkspace {
		t.Errorf("expected namespace %q, got %q", NamespaceWorkspace, res.GetNamespace())
	}
}

// ─── additional coverage ─────────────────────────────────────────────────────

// TestResolveResource_Qualified verifies that a fully qualified name bypasses
// priority search and returns the exact resource.
func TestResolveResource_Qualified(t *testing.T) {
	reg := buildRegistry()

	qn := MakeQualifiedName(NamespaceSystem, KindSkill, "python")
	res, ok := reg.ResolveResource(qn)
	if !ok {
		t.Fatalf("expected resource %q to be found", qn)
	}
	if res.GetNamespace() != NamespaceSystem {
		t.Errorf("expected namespace %q, got %q", NamespaceSystem, res.GetNamespace())
	}
}

// TestListResources_Effective verifies that effective mode deduplicates by
// short name, keeping only the highest-priority entry in scope.
func TestListResources_Effective(t *testing.T) {
	scoped := buildRegistry().Project("services-api")

	results := scoped.ListResources(QueryOptions{
		Kinds:     []Kind{KindSkill},
		Effective: true,
	})

	byName := make(map[string]Resource)
	for _, r := range results {
		byName[r.GetName()] = r
	}

	if r, ok := byName["python"]; !ok || r.GetNamespace() != "services-api" {
		t.Errorf("expected services-api/python to win, got %v", byName["python"])
	}
	if _, ok := byName["finance"]; !ok {
		t.Error("expected finance to appear in results")
	}
}

// ─── Agent Inheritance ────────────────────────────────────────────────────────

func makeToolResource(ns, name string) *Tool {
	return &Tool{BaseResource: BaseResource{
		APIVersion: "warp/v1alpha1",
		Kind:       KindTool,
		Metadata:   Metadata{Name: name},
		Namespace:  ns,
	}}
}

func agentWithSpec(ns, name string, spec AgentSpec) *Agent {
	return &Agent{
		BaseResource: BaseResource{
			APIVersion: "warp/v1alpha1",
			Kind:       KindAgent,
			Metadata:   Metadata{Name: name},
			Namespace:  ns,
		},
		Spec: spec,
	}
}

// TestAgent_Inheritance_Merge verifies that a child agent extending a parent
// produces a merged agent with combined tools and concatenated instructions.
func TestAgent_Inheritance_Merge(t *testing.T) {
	reg := NewRegistry(nil)

	// system/Agent/main — parent: 1 skill, base instructions.
	parent := agentWithSpec(NamespaceSystem, "main", AgentSpec{
		Instructions: "parent instructions",
		Skills:       []string{"system/Skill/skill-a"},
	})
	reg.Set(parent.QualifiedName(), parent)
	reg.Set(MakeQualifiedName(NamespaceSystem, KindSkill, "skill-a"),
		makeSkillResource(NamespaceSystem, "skill-a"))

	// user/Agent/main — child: extends parent, adds 1 skill.
	child := agentWithSpec(NamespaceUser, "main", AgentSpec{
		Extends:      "system/Agent/main",
		Instructions: "child instructions",
		Skills:       []string{"user/Skill/skill-b"},
	})
	reg.Set(child.QualifiedName(), child)
	reg.Set(MakeQualifiedName(NamespaceUser, KindSkill, "skill-b"),
		makeSkillResource(NamespaceUser, "skill-b"))

	// ResolveAgent("main") must pick user/Agent/main (user > system) and merge.
	scoped := reg.Project("myproject")
	merged, err := scoped.ResolveAgent("main")
	if err != nil {
		t.Fatalf("ResolveAgent: %v", err)
	}

	if len(merged.Agent.Spec.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d: %v", len(merged.Agent.Spec.Skills), merged.Agent.Spec.Skills)
	}
	if !strings.Contains(merged.Agent.Spec.Instructions, "parent instructions") {
		t.Error("merged instructions should contain parent instructions")
	}
	if !strings.Contains(merged.Agent.Spec.Instructions, "child instructions") {
		t.Error("merged instructions should contain child instructions")
	}
	// Parent instructions must come first.
	parentIdx := strings.Index(merged.Agent.Spec.Instructions, "parent instructions")
	childIdx := strings.Index(merged.Agent.Spec.Instructions, "child instructions")
	if parentIdx > childIdx {
		t.Error("parent instructions should appear before child instructions")
	}
}

// TestAgent_Inheritance_Chain verifies three-level inheritance produces the
// union of all skills from grandparent → parent → child.
func TestAgent_Inheritance_Chain(t *testing.T) {
	reg := NewRegistry(nil)

	grandparent := agentWithSpec(NamespaceSystem, "base", AgentSpec{
		Instructions: "grandparent",
		Skills:       []string{"skill-a"},
	})
	parent := agentWithSpec(NamespaceUser, "middle", AgentSpec{
		Extends:      "system/Agent/base",
		Instructions: "parent",
		Skills:       []string{"skill-b"},
	})
	child := agentWithSpec(NamespaceWorkspace, "child", AgentSpec{
		Extends:      "user/Agent/middle",
		Instructions: "child",
		Skills:       []string{"skill-c"},
	})
	for _, r := range []Resource{grandparent, parent, child} {
		reg.Set(r.QualifiedName(), r)
	}

	merged, err := reg.ResolveAgent("workspace/Agent/child")
	if err != nil {
		t.Fatalf("ResolveAgent: %v", err)
	}
	if len(merged.Agent.Spec.Skills) != 3 {
		t.Errorf("expected 3 skills, got %d: %v", len(merged.Agent.Spec.Skills), merged.Agent.Spec.Skills)
	}
	// Instruction order: grandparent → parent → child.
	gpIdx := strings.Index(merged.Agent.Spec.Instructions, "grandparent")
	pIdx := strings.Index(merged.Agent.Spec.Instructions, "parent")
	cIdx := strings.Index(merged.Agent.Spec.Instructions, "child")
	if gpIdx >= pIdx || pIdx >= cIdx {
		t.Errorf("unexpected instruction order: gp=%d p=%d c=%d in %q",
			gpIdx, pIdx, cIdx, merged.Agent.Spec.Instructions)
	}
}

// TestAgent_Inheritance_SelfCircular ensures that an agent that extends itself
// returns an error rather than looping forever.
func TestAgent_Inheritance_SelfCircular(t *testing.T) {
	reg := NewRegistry(nil)
	self := agentWithSpec(NamespaceSystem, "loopy", AgentSpec{
		Extends: "system/Agent/loopy",
	})
	reg.Set(self.QualifiedName(), self)

	_, err := reg.ResolveAgent("system/Agent/loopy")
	if err == nil {
		t.Fatal("expected circular inheritance error, got nil")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected 'circular' in error message, got: %v", err)
	}
}

// TestAgent_Inheritance_MutualCircular ensures a mutual A→B→A cycle is detected.
func TestAgent_Inheritance_MutualCircular(t *testing.T) {
	reg := NewRegistry(nil)
	a := agentWithSpec(NamespaceSystem, "a", AgentSpec{Extends: "system/Agent/b"})
	b := agentWithSpec(NamespaceSystem, "b", AgentSpec{Extends: "system/Agent/a"})
	reg.Set(a.QualifiedName(), a)
	reg.Set(b.QualifiedName(), b)

	_, err := reg.ResolveAgent("system/Agent/a")
	if err == nil {
		t.Fatal("expected circular inheritance error, got nil")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected 'circular' in error message, got: %v", err)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAgent_Policies_Merging(t *testing.T) {
	reg := NewRegistry(nil)

	allowDangerousParent := false
	allowOpenWorldParent := true
	parent := agentWithSpec(NamespaceSystem, "parent", AgentSpec{
		Policies: &Policies{
			Tools: &ToolPolicies{
				AllowDangerous: &allowDangerousParent,
				AllowOpenWorld: &allowOpenWorldParent,
				Include:        []string{"git_*"},
				Exclude:        []string{"*dangerous*"},
			},
		},
	})
	reg.Set(parent.QualifiedName(), parent)

	allowDangerousChild := true
	child := agentWithSpec(NamespaceWorkspace, "child", AgentSpec{
		Extends: "system/Agent/parent",
		Policies: &Policies{
			Tools: &ToolPolicies{
				AllowDangerous: &allowDangerousChild,
				Include:        []string{"ssh_*"},
				Exclude:        []string{"*evil*"},
			},
		},
	})
	reg.Set(child.QualifiedName(), child)

	resolved, err := reg.ResolveAgent("workspace/Agent/child")
	if err != nil {
		t.Fatalf("ResolveAgent: %v", err)
	}

	p := resolved.Agent.Spec.Policies
	if p == nil || p.Tools == nil {
		t.Fatal("expected resolved agent to have tool policies")
	}

	// Boolean overrides
	if p.Tools.AllowDangerous == nil || !*p.Tools.AllowDangerous {
		t.Errorf("expected AllowDangerous to be overridden to true, got %v", p.Tools.AllowDangerous)
	}
	if p.Tools.AllowOpenWorld == nil || !*p.Tools.AllowOpenWorld {
		t.Errorf("expected AllowOpenWorld to be inherited as true, got %v", p.Tools.AllowOpenWorld)
	}

	// Slice unions (should deduplicate / union)
	expectedIncludes := []string{"git_*", "ssh_*"}
	if !slicesEqual(p.Tools.Include, expectedIncludes) {
		t.Errorf("expected Include to be %v, got %v", expectedIncludes, p.Tools.Include)
	}

	expectedExcludes := []string{"*dangerous*", "*evil*"}
	if !slicesEqual(p.Tools.Exclude, expectedExcludes) {
		t.Errorf("expected Exclude to be %v, got %v", expectedExcludes, p.Tools.Exclude)
	}
}

func TestAgent_ResolvedAgent_ToolFiltering(t *testing.T) {
	reg := NewRegistry(nil)

	// Add tools to registry
	toolA := makeToolResource(NamespaceWorkspace, "git-clone")
	toolB := makeToolResource(NamespaceWorkspace, "rm-rf")
	toolB.Spec.Annotations = &ToolAnnotation{IsDangerous: true}
	toolC := makeToolResource(NamespaceWorkspace, "http-get")
	toolC.Spec.Annotations = &ToolAnnotation{IsOpenWorld: true}

	reg.Set(toolA.QualifiedName(), toolA)
	reg.Set(toolB.QualifiedName(), toolB)
	reg.Set(toolC.QualifiedName(), toolC)

	// Agent policy
	allowDangerous := false
	allowOpenWorld := false
	agent := agentWithSpec(NamespaceWorkspace, "my-agent", AgentSpec{
		Policies: &Policies{
			Tools: &ToolPolicies{
				AllowDangerous: &allowDangerous,
				AllowOpenWorld: &allowOpenWorld,
				Include:        []string{"git-*", "http-*"},
				Exclude:        []string{"*clone*"},
			},
		},
	})
	reg.Set(agent.QualifiedName(), agent)

	resolved, err := reg.ResolveAgent("workspace/Agent/my-agent")
	if err != nil {
		t.Fatalf("ResolveAgent: %v", err)
	}

	// Filter logic:
	// - git-clone: excluded by "*clone*" pattern.
	// - rm-rf: annotation IsDangerous=true, which is forbidden (AllowDangerous=false).
	// - http-get: annotation IsOpenWorld=true, which is forbidden (AllowOpenWorld=false).
	// None of the tools should be returned!
	if len(resolved.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d: %v", len(resolved.Tools), resolved.Tools)
	}

	// Now allow open world and git-clone (by removing exclude, allowing dangerous)
	allowDangerous2 := true
	allowOpenWorld2 := true
	agent2 := agentWithSpec(NamespaceWorkspace, "my-agent-2", AgentSpec{
		Policies: &Policies{
			Tools: &ToolPolicies{
				AllowDangerous: &allowDangerous2,
				AllowOpenWorld: &allowOpenWorld2,
				Include:        []string{"git-*", "http-*"},
			},
		},
	})
	reg.Set(agent2.QualifiedName(), agent2)

	resolved2, err := reg.ResolveAgent("workspace/Agent/my-agent-2")
	if err != nil {
		t.Fatalf("ResolveAgent: %v", err)
	}

	// git-clone and http-get are included.
	// rm-rf is not included (doesn't match "git-*" or "http-*").
	if len(resolved2.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(resolved2.Tools))
	}
	names := []string{resolved2.Tools[0].Metadata.Name, resolved2.Tools[1].Metadata.Name}
	if !slicesEqual(names, []string{"git-clone", "http-get"}) && !slicesEqual(names, []string{"http-get", "git-clone"}) {
		t.Errorf("unexpected resolved tools: %v", names)
	}
}

func TestAgent_NamespaceMatching(t *testing.T) {
	reg := NewRegistry(nil)

	// Add tools to registry
	toolA := makeToolResource("workspace", "git-clone")
	toolB := makeToolResource("workspace", "http-get")
	toolC := makeToolResource("system", "sys-tool")

	reg.Set(toolA.QualifiedName(), toolA)
	reg.Set(toolB.QualifiedName(), toolB)
	reg.Set(toolC.QualifiedName(), toolC)

	// Namespace/name matching (e.g. "workspace/*" should match workspace/Tool/git-clone and workspace/Tool/http-get, but not system/Tool/sys-tool)
	agent1 := agentWithSpec(NamespaceWorkspace, "agent-ns-matching", AgentSpec{
		Policies: &Policies{
			Tools: &ToolPolicies{
				Include: []string{"workspace/*"},
			},
		},
	})
	reg.Set(agent1.QualifiedName(), agent1)

	resolved1, err := reg.ResolveAgent("workspace/Agent/agent-ns-matching")
	if err != nil {
		t.Fatalf("ResolveAgent: %v", err)
	}

	if len(resolved1.Tools) != 2 {
		t.Errorf("expected 2 tools (git-clone and http-get), got %d: %v", len(resolved1.Tools), resolved1.Tools)
	}
	for _, tool := range resolved1.Tools {
		if tool.GetNamespace() != "workspace" {
			t.Errorf("expected only workspace tools, but got: %s", tool.QualifiedName())
		}
	}
}

func TestMCP_Validation(t *testing.T) {
	// 1. Valid stdio MCP
	reg1 := NewRegistry(nil)
	mcpStd := &MCP{
		BaseResource: BaseResource{
			APIVersion: "warp/v1alpha1",
			Kind:       KindMCP,
			Metadata:   Metadata{Name: "std-mcp"},
		},
		Spec: MCPSpec{
			Type:    "stdio",
			Command: []string{"node", "server.js"},
		},
	}
	reg1.set(mcpStd.QualifiedName(), mcpStd)
	if err := reg1.Validate(); err != nil {
		t.Errorf("Expected valid stdio MCP to pass, got: %v", err)
	}

	// 2. Invalid stdio MCP (missing command)
	reg2 := NewRegistry(nil)
	mcpStdErr := &MCP{
		BaseResource: BaseResource{
			APIVersion: "warp/v1alpha1",
			Kind:       KindMCP,
			Metadata:   Metadata{Name: "std-mcp-err"},
		},
		Spec: MCPSpec{
			Type: "stdio",
		},
	}
	reg2.set(mcpStdErr.QualifiedName(), mcpStdErr)
	if err := reg2.Validate(); err == nil {
		t.Error("Expected validation error for stdio MCP with empty command, got nil")
	}

	// 3. Valid sse MCP
	reg3 := NewRegistry(nil)
	mcpSse := &MCP{
		BaseResource: BaseResource{
			APIVersion: "warp/v1alpha1",
			Kind:       KindMCP,
			Metadata:   Metadata{Name: "sse-mcp"},
		},
		Spec: MCPSpec{
			Type:     "sse",
			Endpoint: "https://example.com/sse",
		},
	}
	reg3.set(mcpSse.QualifiedName(), mcpSse)
	if err := reg3.Validate(); err != nil {
		t.Errorf("Expected valid sse MCP to pass, got: %v", err)
	}

	// 4. Invalid sse MCP (missing endpoint)
	reg4 := NewRegistry(nil)
	mcpSseErr := &MCP{
		BaseResource: BaseResource{
			APIVersion: "warp/v1alpha1",
			Kind:       KindMCP,
			Metadata:   Metadata{Name: "sse-mcp-err"},
		},
		Spec: MCPSpec{
			Type: "sse",
		},
	}
	reg4.set(mcpSseErr.QualifiedName(), mcpSseErr)
	if err := reg4.Validate(); err == nil {
		t.Error("Expected validation error for sse MCP with empty endpoint, got nil")
	}

	// 5. Invalid transport type
	reg5 := NewRegistry(nil)
	mcpBad := &MCP{
		BaseResource: BaseResource{
			APIVersion: "warp/v1alpha1",
			Kind:       KindMCP,
			Metadata:   Metadata{Name: "bad-mcp"},
		},
		Spec: MCPSpec{
			Type: "websocket",
		},
	}
	reg5.set(mcpBad.QualifiedName(), mcpBad)
	if err := reg5.Validate(); err == nil {
		t.Error("Expected validation error for unknown transport type, got nil")
	}
}
