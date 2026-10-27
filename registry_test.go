package warp

import (
	"slices"
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
		Skills:       &[]string{"system/Skill/skill-a"},
	})
	reg.Set(parent.QualifiedName(), parent)
	reg.Set(MakeQualifiedName(NamespaceSystem, KindSkill, "skill-a"),
		makeSkillResource(NamespaceSystem, "skill-a"))

	// user/Agent/main — child: extends parent, adds 1 skill.
	child := agentWithSpec(NamespaceUser, "main", AgentSpec{
		Extends:      "system/Agent/main",
		Instructions: "child instructions",
		Skills:       &[]string{"user/Skill/skill-b"},
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

	if len(*merged.Agent.Spec.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d: %v", len(*merged.Agent.Spec.Skills), *merged.Agent.Spec.Skills)
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
		Skills:       &[]string{"skill-a"},
	})
	parent := agentWithSpec(NamespaceUser, "middle", AgentSpec{
		Extends:      "system/Agent/base",
		Instructions: "parent",
		Skills:       &[]string{"skill-b"},
	})
	child := agentWithSpec(NamespaceWorkspace, "child", AgentSpec{
		Extends:      "user/Agent/middle",
		Instructions: "child",
		Skills:       &[]string{"skill-c"},
	})
	for _, r := range []Resource{grandparent, parent, child} {
		reg.Set(r.QualifiedName(), r)
	}

	merged, err := reg.ResolveAgent("workspace/Agent/child")
	if err != nil {
		t.Fatalf("ResolveAgent: %v", err)
	}
	if len(*merged.Agent.Spec.Skills) != 3 {
		t.Errorf("expected 3 skills, got %d: %v", len(*merged.Agent.Spec.Skills), *merged.Agent.Spec.Skills)
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

	// 6. Valid http and streamable-http MCPs
	for _, tr := range []string{"http", "streamable-http"} {
		regOk := NewRegistry(nil)
		mcpRemote := &MCP{
			BaseResource: BaseResource{
				APIVersion: APIVersion,
				Kind:       KindMCP,
				Metadata:   Metadata{Name: tr + "-mcp"},
			},
			Spec: MCPSpec{
				Type:     tr,
				Endpoint: "https://example.com/mcp",
				Auth: &ProviderAuth{
					Type: "bearer",
					Env:  "MCP_TOKEN",
				},
				Headers: map[string]string{"X-Header": "val"},
			},
		}
		regOk.set(mcpRemote.QualifiedName(), mcpRemote)
		if err := regOk.Validate(); err != nil {
			t.Errorf("Expected valid %s MCP to pass, got: %v", tr, err)
		}

		regMissing := NewRegistry(nil)
		mcpMissing := &MCP{
			BaseResource: BaseResource{
				APIVersion: APIVersion,
				Kind:       KindMCP,
				Metadata:   Metadata{Name: tr + "-mcp-err"},
			},
			Spec: MCPSpec{
				Type: tr,
			},
		}
		regMissing.set(mcpMissing.QualifiedName(), mcpMissing)
		if err := regMissing.Validate(); err == nil {
			t.Errorf("Expected validation error for %s MCP with empty endpoint, got nil", tr)
		}
	}
}

func TestAgent_Subagents(t *testing.T) {
	// Setup Registry
	reg := NewRegistry(nil)

	// Define some agents
	agentA := agentWithSpec(NamespaceSystem, "agent-a", AgentSpec{
		Subagents: &[]string{"system/Agent/agent-b"},
	})
	agentB := agentWithSpec(NamespaceSystem, "agent-b", AgentSpec{})
	agentC := agentWithSpec(NamespaceSystem, "agent-c", AgentSpec{})
	// agentD cannot be triggered by agent (only human)
	agentD := agentWithSpec(NamespaceSystem, "agent-d", AgentSpec{
		Triggers: []string{"human"},
	})
	// agentE can be triggered by agent
	agentE := agentWithSpec(NamespaceSystem, "agent-e", AgentSpec{
		Triggers: []string{"agent"},
	})

	reg.Set(agentA.QualifiedName(), agentA)
	reg.Set(agentB.QualifiedName(), agentB)
	reg.Set(agentC.QualifiedName(), agentC)
	reg.Set(agentD.QualifiedName(), agentD)
	reg.Set(agentE.QualifiedName(), agentE)

	// 1. Validate passes when subagent exists
	if err := reg.Validate(); err != nil {
		t.Fatalf("Expected validation to pass, got: %v", err)
	}

	// 2. Resolve Agent with specific subagent
	scoped := reg.Project("myproject")
	resolvedA, err := scoped.ResolveAgent("agent-a")
	if err != nil {
		t.Fatalf("ResolveAgent(agent-a): %v", err)
	}
	if len(resolvedA.Subagents) != 1 || resolvedA.Subagents[0].Metadata.Name != "agent-b" {
		t.Errorf("expected only agent-b as subagent, got: %v", resolvedA.Subagents)
	}

	// 3. Resolve Agent with empty subagents (should return all agents that can be triggered by "agent")
	resolvedB, err := scoped.ResolveAgent("agent-b")
	if err != nil {
		t.Fatalf("ResolveAgent(agent-b): %v", err)
	}
	// It should list agent-a, agent-b, agent-c, and agent-e (total 4), but NOT agent-d (which is only triggered by human).
	if len(resolvedB.Subagents) != 4 {
		t.Errorf("expected 4 subagents, got %d (agents: %v)", len(resolvedB.Subagents), resolvedB.Subagents)
	}
	for _, sa := range resolvedB.Subagents {
		if sa.Metadata.Name == "agent-d" {
			t.Errorf("did not expect agent-d (triggers: human) to be resolved as a subagent")
		}
	}

	// 3b. Resolve Agent with explicitly empty subagents (should return exactly 0 subagents)
	agentEmptySubagents := agentWithSpec(NamespaceSystem, "agent-empty-subagents", AgentSpec{
		Subagents: &[]string{},
	})
	reg.Set(agentEmptySubagents.QualifiedName(), agentEmptySubagents)
	resolvedEmpty, err := scoped.ResolveAgent("agent-empty-subagents")
	if err != nil {
		t.Fatalf("ResolveAgent(agent-empty-subagents): %v", err)
	}
	if len(resolvedEmpty.Subagents) != 0 {
		t.Errorf("expected 0 subagents, got %d: %v", len(resolvedEmpty.Subagents), resolvedEmpty.Subagents)
	}

	// 4. Validation fails on missing subagent
	agentBad := agentWithSpec(NamespaceSystem, "agent-bad", AgentSpec{
		Subagents: &[]string{"system/Agent/missing"},
	})
	reg.Set(agentBad.QualifiedName(), agentBad)
	if err := reg.Validate(); err == nil {
		t.Error("Expected validation to fail for missing subagent reference, got nil")
	}
}

func TestRegistry_ListResources_Filter(t *testing.T) {
	reg := NewRegistry(nil)
	agentA := agentWithSpec(NamespaceSystem, "agent-a", AgentSpec{
		Triggers: []string{"human"},
	})
	agentB := agentWithSpec(NamespaceSystem, "agent-b", AgentSpec{
		Triggers: []string{"agent"},
	})
	reg.Set(agentA.QualifiedName(), agentA)
	reg.Set(agentB.QualifiedName(), agentB)

	scoped := reg.Project("myproject")

	// Filter for agents that can be triggered by "human"
	humanAgents := scoped.ListResources(QueryOptions{
		Kinds: []Kind{KindAgent},
		Filter: func(res Resource) bool {
			if a, ok := res.(*Agent); ok {
				if slices.Contains(a.Spec.Triggers, "human") {
					return true
				}
			}
			return false
		},
	})

	if len(humanAgents) != 1 || humanAgents[0].GetName() != "agent-a" {
		t.Errorf("expected only agent-a to match filter, got: %v", humanAgents)
	}
}

func TestWildcardSubagents(t *testing.T) {
	reg := NewRegistry(nil)
	agentA := agentWithSpec(NamespaceSystem, "agent-a", AgentSpec{
		Subagents: &[]string{"*"},
	})
	agentB := agentWithSpec(NamespaceSystem, "agent-b", AgentSpec{})
	agentC := agentWithSpec(NamespaceSystem, "agent-c", AgentSpec{})
	// agentD cannot be triggered by agent (only human)
	agentD := agentWithSpec(NamespaceSystem, "agent-d", AgentSpec{
		Triggers: []string{"human"},
	})
	reg.Set(agentA.QualifiedName(), agentA)
	reg.Set(agentB.QualifiedName(), agentB)
	reg.Set(agentC.QualifiedName(), agentC)
	reg.Set(agentD.QualifiedName(), agentD)

	if err := reg.Validate(); err != nil {
		t.Fatalf("Expected validation to pass with wildcard subagent reference, got: %v", err)
	}

	scoped := reg.Project("myproject")
	resolvedA, err := scoped.ResolveAgent("agent-a")
	if err != nil {
		t.Fatalf("ResolveAgent(agent-a): %v", err)
	}

	// Should list agent-a, agent-b, agent-c (total 3), but NOT agent-d (only human).
	if len(resolvedA.Subagents) != 3 {
		t.Errorf("expected 3 subagents, got %d (agents: %v)", len(resolvedA.Subagents), resolvedA.Subagents)
	}
}

func TestProjectFromPath_SubdirectoryMatching(t *testing.T) {
	reg := NewRegistry(nil)
	reg.AddProject(&Project{Name: "root", Path: ".", RootPath: "/repo"})
	reg.AddProject(&Project{Name: "api", Path: "services/api", RootPath: "/repo"})

	// Exact match on nested project
	if p, ok := reg.ProjectFromPath("/repo/services/api"); !ok || p.Name != "api" {
		t.Errorf("expected project 'api', got %v (ok=%v)", p, ok)
	}
	// Subdirectory inside nested project should resolve to the deepest matching project ('api')
	if p, ok := reg.ProjectFromPath("/repo/services/api/internal/handlers"); !ok || p.Name != "api" {
		t.Errorf("expected project 'api' for nested dir, got %v (ok=%v)", p, ok)
	}
	// Directory outside nested project but inside root should resolve to 'root'
	if p, ok := reg.ProjectFromPath("/repo/docs/guide"); !ok || p.Name != "root" {
		t.Errorf("expected project 'root', got %v (ok=%v)", p, ok)
	}
	// Unrelated path should not match
	if p, ok := reg.ProjectFromPath("/other/path"); ok {
		t.Errorf("expected no match for /other/path, got %v", p)
	}
}

func TestToolkitExpansion_And_GlobCapabilities(t *testing.T) {
	reg := NewRegistry(nil)
	reg.AddProject(&Project{Name: "app", Path: ".", RootPath: "/repo"})

	// Plugin skill & command under namespace "warp-dev"
	pluginSkill := makeSkillResource("warp-dev", "warp-developer")
	pluginCmd := makeCommandResource("warp-dev", "scaffold")
	otherSkill := makeSkillResource("other-plugin", "ignored-skill")
	reg.Set(pluginSkill.QualifiedName(), pluginSkill)
	reg.Set(pluginCmd.QualifiedName(), pluginCmd)
	reg.Set(otherSkill.QualifiedName(), otherSkill)

	// Standalone tool + Toolkit with both $ref and inline tool
	refTool := makeToolResource(NamespaceWorkspace, "list-files")
	refTool.Spec.Command = []string{"ls", "-la"}
	refTool.Spec.Description = "List files"
	reg.Set(refTool.QualifiedName(), refTool)

	tk := &Toolkit{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindToolkit,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "default-toolkit"},
		},
		Spec: ToolkitSpec{
			Tools: []ToolkitTool{
				{Ref: "list-files.yaml"},
				{
					ToolSpec: ToolSpec{
						Name:        "echo-inline",
						Description: "Inline echo tool",
						Command:     []string{"echo"},
					},
				},
			},
		},
	}
	reg.Set(tk.QualifiedName(), tk)

	agent := agentWithSpec("app", "dev-agent", AgentSpec{
		Skills:   &[]string{"warp-dev/*"},
		Commands: &[]string{"warp-dev/Command/*"},
	})
	reg.Set(agent.QualifiedName(), agent)

	if err := reg.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Base registry should also see plugin namespace resources since 'app' is registered
	if _, ok := reg.ResolveResource("warp-developer"); !ok {
		t.Error("expected base Registry to resolve plugin skill 'warp-developer'")
	}

	scoped := reg.Project("app")
	resolved, err := scoped.ResolveAgent("dev-agent")
	if err != nil {
		t.Fatalf("ResolveAgent failed: %v", err)
	}

	if len(resolved.Skills) != 1 || resolved.Skills[0].Metadata.Name != "warp-developer" {
		t.Errorf("expected [warp-developer] skill from glob, got %v", resolved.Skills)
	}
	if len(resolved.Commands) != 1 || resolved.Commands[0].Metadata.Name != "scaffold" {
		t.Errorf("expected [scaffold] command from glob, got %v", resolved.Commands)
	}
	if len(resolved.Tools) != 2 {
		t.Fatalf("expected 2 tools (1 standalone + 1 inline from toolkit), got %d: %v", len(resolved.Tools), resolved.Tools)
	}
	if resolved.Tools[0].Metadata.Name != "echo-inline" || resolved.Tools[1].Metadata.Name != "list-files" {
		t.Errorf("expected deterministic sorted tools [echo-inline, list-files], got [%s, %s]",
			resolved.Tools[0].Metadata.Name, resolved.Tools[1].Metadata.Name)
	}
	if resolved.Tools[0].Metadata.Description != "Inline echo tool" {
		t.Errorf("expected inline tool description to be populated, got %q", resolved.Tools[0].Metadata.Description)
	}
}

func TestValidate_KindAwareness_And_Inheritance(t *testing.T) {
	// 1. Referencing a Tool where a Skill is expected must fail validation.
	reg1 := NewRegistry(nil)
	tool := makeToolResource(NamespaceWorkspace, "not-a-skill")
	tool.Spec.Command = []string{"echo"}
	agentWrongKind := agentWithSpec(NamespaceWorkspace, "bad-agent", AgentSpec{
		Skills: &[]string{"not-a-skill"},
	})
	reg1.Set(tool.QualifiedName(), tool)
	reg1.Set(agentWrongKind.QualifiedName(), agentWrongKind)
	if err := reg1.Validate(); err == nil {
		t.Error("expected Validate() to fail when skill ref points to a Tool")
	}

	// 2. Circular inheritance must fail Validate().
	reg2 := NewRegistry(nil)
	loopy := agentWithSpec(NamespaceWorkspace, "loopy", AgentSpec{
		Extends: "workspace/Agent/loopy",
	})
	reg2.Set(loopy.QualifiedName(), loopy)
	if err := reg2.Validate(); err == nil || !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected circular inheritance validation error, got: %v", err)
	}

	// 3. Plugin exporting structural resources must fail Validate().
	reg3 := NewRegistry(nil)
	badPlugin := &Plugin{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindPlugin,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-plugin"},
		},
		Spec: PluginSpec{
			Exports: []string{"Workspace/*"},
		},
	}
	reg3.Set(badPlugin.QualifiedName(), badPlugin)
	if err := reg3.Validate(); err == nil {
		t.Error("expected Validate() to fail when Plugin exports structural resource kind")
	}
}

func TestValidate_Decision_And_ModelCapabilities(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }
	floatPtr := func(f float64) *float64 { return &f }

	provider := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "local-ai"},
		},
		Spec: ModelProviderSpec{
			Type:     "ollama",
			Endpoint: "http://localhost:11434",
			DefaultModels: &ModelDefaults{
				Completion: "llama3.3",
				Decision:   "nimble",
				Embedding:  "nomic-embed-text",
			},
			Models: []ProviderModel{
				{
					ID:    "llama3.3",
					Name:  "llama3.3",
					Label: "Llama 3.3",
					Limits: ProviderModelLimits{
						Context: 128000,
						Output:  4096,
					},
				},
				{
					ID:    "nimble",
					Name:  "nimble",
					Label: "Nimble",
					Limits: ProviderModelLimits{
						Context: 64000,
					},
					Capabilities: &ProviderModelCapabilities{
						Completion: boolPtr(false),
						Decision:   boolPtr(true),
					},
				},
				{
					ID:    "nomic-embed-text",
					Name:  "nomic-embed-text",
					Label: "Nomic Embed Text",
					Limits: ProviderModelLimits{
						Context:    8192,
						Dimensions: 768,
					},
					Capabilities: &ProviderModelCapabilities{
						Completion: boolPtr(false),
						Embedding:  boolPtr(true),
					},
				},
			},
		},
	}

	validDecision := &Decision{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindDecision,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "triage"},
		},
		Spec: DecisionSpec{
			Models: []string{"local-ai/nimble"},
			Questions: []DecisionQuestion{
				{
					Name:      "is_bug",
					Type:      DecisionQuestionPredicate,
					Threshold: floatPtr(0.8),
				},
				{
					Name:          "category",
					Type:          DecisionQuestionChoice,
					MinConfidence: floatPtr(0.6),
					Options: []DecisionOption{
						{Value: "ui"},
						{Value: "api"},
					},
				},
				{
					Name: "severity",
					Type: DecisionQuestionScore,
					Levels: []DecisionLevel{
						{Label: "low"},
						{Label: "high"},
					},
				},
			},
		},
	}

	reg := NewRegistry(&Workspace{
		Def: &WorkspaceDef{
			BaseResource: BaseResource{
				APIVersion: APIVersion,
				Kind:       KindWorkspace,
				Metadata:   Metadata{Name: "ws"},
			},
			Spec: WorkspaceDefSpec{
				DefaultModels: &ModelDefaults{
					Completion: "local-ai/llama3.3",
					Decision:   "local-ai/nimble",
					Embedding:  "local-ai/nomic-embed-text",
				},
			},
		},
	})
	reg.Set(provider.QualifiedName(), provider)
	reg.Set(validDecision.QualifiedName(), validDecision)

	if err := reg.Validate(); err != nil {
		t.Fatalf("Expected valid Decision and ModelDefaults to pass Validate(), got: %v", err)
	}

	// 1. Decision referencing a non-decision model should fail
	badModelDec := validDecision.DeepCopy()
	badModelDec.Metadata.Name = "bad-model-dec"
	badModelDec.Spec.Models = []string{"local-ai/llama3.3"}
	regBadModel := NewRegistry(nil)
	regBadModel.Set(provider.QualifiedName(), provider)
	regBadModel.Set(badModelDec.QualifiedName(), badModelDec)
	if err := regBadModel.Validate(); err == nil || !strings.Contains(err.Error(), "does not support decision capability") {
		t.Errorf("Expected decision capability error, got: %v", err)
	}

	// 2. Agent referencing a pure decision/embedding model (completion: false) should fail
	badAgent := agentWithSpec(NamespaceWorkspace, "bad-agent", AgentSpec{
		Models: []string{"local-ai/nimble"},
	})
	regBadAgent := NewRegistry(nil)
	regBadAgent.Set(provider.QualifiedName(), provider)
	regBadAgent.Set(badAgent.QualifiedName(), badAgent)
	if err := regBadAgent.Validate(); err == nil || !strings.Contains(err.Error(), "does not support completion capability") {
		t.Errorf("Expected completion capability error, got: %v", err)
	}

	// 3. Decision with out-of-range threshold should fail
	badThreshDec := validDecision.DeepCopy()
	badThreshDec.Spec.Questions[0].Threshold = floatPtr(1.5)
	regBadThresh := NewRegistry(nil)
	regBadThresh.Set(badThreshDec.QualifiedName(), badThreshDec)
	if err := regBadThresh.Validate(); err == nil || !strings.Contains(err.Error(), "threshold must be between 0.0 and 1.0") {
		t.Errorf("Expected threshold bounds error, got: %v", err)
	}

	// 4. Decision with duplicate question name should fail
	dupNameDec := validDecision.DeepCopy()
	dupNameDec.Spec.Questions[1].Name = "is_bug"
	regDup := NewRegistry(nil)
	regDup.Set(dupNameDec.QualifiedName(), dupNameDec)
	if err := regDup.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate question name") {
		t.Errorf("Expected duplicate question name error, got: %v", err)
	}
}

func TestAgent_Inheritance_ScalarAndOverrideFields(t *testing.T) {
	reg := NewRegistry(nil)
	parentTemp := 0.7
	childTemp := 0.0

	parent := &Agent{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindAgent,
			Namespace:  NamespaceSystem,
			Directory:  "/system/agents",
			Metadata: Metadata{
				Name:        "base-agent",
				Description: "Base description",
			},
		},
		Spec: AgentSpec{
			Models:       []string{"gpt-4o"},
			Temperature:  &parentTemp,
			Thinking:     map[string]any{"type": "effort", "effort": "low"},
			Triggers:     []string{"human", "agent"},
			Instructions: "Base prompt.",
		},
	}

	child := &Agent{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindAgent,
			Namespace:  NamespaceWorkspace,
			Directory:  "/workspace/.agents/defs",
			Metadata: Metadata{
				Name:        "custom-agent",
				Description: "Child description",
			},
		},
		Spec: AgentSpec{
			Extends:      "system/Agent/base-agent",
			Models:       []string{"claude-3-7-sonnet"},
			Temperature:  &childTemp,
			Thinking:     map[string]any{"type": "budget", "budgetTokens": 4096},
			Triggers:     []string{"human"},
			Instructions: "Child prompt.",
		},
	}

	reg.Set(parent.QualifiedName(), parent)
	reg.Set(child.QualifiedName(), child)

	resolved, err := reg.ResolveAgent("workspace/Agent/custom-agent")
	if err != nil {
		t.Fatalf("ResolveAgent failed: %v", err)
	}

	ag := resolved.Agent
	if ag.Metadata.Name != "custom-agent" || ag.Metadata.Description != "Child description" {
		t.Errorf("expected child metadata, got %+v", ag.Metadata)
	}
	if ag.Namespace != NamespaceWorkspace {
		t.Errorf("expected child namespace %q, got %q", NamespaceWorkspace, ag.Namespace)
	}
	if ag.Directory != "/workspace/.agents/defs" {
		t.Errorf("expected child directory, got %q", ag.Directory)
	}
	if !slicesEqual(ag.Spec.Models, []string{"claude-3-7-sonnet"}) {
		t.Errorf("expected child models override, got %v", ag.Spec.Models)
	}
	if ag.Spec.Temperature == nil || *ag.Spec.Temperature != 0.0 {
		t.Errorf("expected child temperature override 0.0, got %v", ag.Spec.Temperature)
	}
	if ag.Spec.Thinking["type"] != "budget" || ag.Spec.Thinking["budgetTokens"] != 4096 {
		t.Errorf("expected child thinking override, got %v", ag.Spec.Thinking)
	}
	if !slicesEqual(ag.Spec.Triggers, []string{"human"}) {
		t.Errorf("expected child triggers override, got %v", ag.Spec.Triggers)
	}
}

func TestResolveModels(t *testing.T) {
	provider := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "local-ai"},
		},
		Spec: ModelProviderSpec{
			Type:         "ollama",
			Endpoint:     "http://localhost:11434",
			DefaultModel: "llama3.3-fallback",
			DefaultModels: &ModelDefaults{
				Completion: "llama3.3",
				Decision:   "nimble",
				Embedding:  "nomic-embed-text",
			},
		},
	}

	// Case 1: Workspace defines DefaultProvider and DefaultModels
	reg1 := NewRegistry(&Workspace{
		Def: &WorkspaceDef{
			BaseResource: BaseResource{
				APIVersion: APIVersion,
				Kind:       KindWorkspace,
				Metadata:   Metadata{Name: "ws"},
			},
			Spec: WorkspaceDefSpec{
				DefaultProvider: "local-ai",
				DefaultModels: &ModelDefaults{
					Completion: "ws-comp-model",
					Decision:   "typesafe/jev-latest",
				},
			},
		},
	})
	reg1.Set(provider.QualifiedName(), provider)

	// 1a. Explicit models win; unqualified models get <defaultProvider>/ prefix
	gotExplicit := reg1.ResolveModels(ModelRoleCompletion, "", "qwen2.5", "anthropic/claude-3-7-sonnet")
	wantExplicit := []string{"local-ai/qwen2.5", "anthropic/claude-3-7-sonnet"}
	if !slicesEqual(gotExplicit, wantExplicit) {
		t.Errorf("explicit ResolveModels = %v, want %v", gotExplicit, wantExplicit)
	}

	// 1b. Workspace defaultModels.<role> wins when explicit is empty
	gotWsComp := reg1.ResolveModels(ModelRoleCompletion)
	if !slicesEqual(gotWsComp, []string{"local-ai/ws-comp-model"}) {
		t.Errorf("workspace completion ResolveModels = %v, want [local-ai/ws-comp-model]", gotWsComp)
	}
	gotWsDec := reg1.ResolveModels(ModelRoleDecision)
	if !slicesEqual(gotWsDec, []string{"typesafe/jev-latest"}) {
		t.Errorf("workspace decision ResolveModels = %v, want [typesafe/jev-latest]", gotWsDec)
	}

	// 1c. Falls through to DefaultProvider's defaultModels.embedding when workspace omits embedding default
	gotProvEmb := reg1.ResolveModels(ModelRoleEmbedding)
	if !slicesEqual(gotProvEmb, []string{"local-ai/nomic-embed-text"}) {
		t.Errorf("provider embedding ResolveModels = %v, want [local-ai/nomic-embed-text]", gotProvEmb)
	}

	// Case 2: Workspace defines DefaultModel (legacy/scalar) and DefaultProvider
	reg2 := NewRegistry(&Workspace{
		Def: &WorkspaceDef{
			BaseResource: BaseResource{
				APIVersion: APIVersion,
				Kind:       KindWorkspace,
				Metadata:   Metadata{Name: "ws2"},
			},
			Spec: WorkspaceDefSpec{
				DefaultProvider: "local-ai",
				DefaultModel:    "scalar-ws-model",
			},
		},
	})
	reg2.Set(provider.QualifiedName(), provider)
	if got := reg2.ResolveModels(ModelRoleCompletion); !slicesEqual(got, []string{"local-ai/scalar-ws-model"}) {
		t.Errorf("workspace DefaultModel fallback = %v, want [local-ai/scalar-ws-model]", got)
	}

	// Case 3: Provider defines only DefaultModel (no DefaultModels)
	provScalar := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "simple-prov"},
		},
		Spec: ModelProviderSpec{
			Type:         "openai",
			Endpoint:     "https://api.openai.com/v1",
			DefaultModel: "gpt-4o",
		},
	}
	reg3 := NewRegistry(&Workspace{
		Def: &WorkspaceDef{
			BaseResource: BaseResource{
				APIVersion: APIVersion,
				Kind:       KindWorkspace,
				Metadata:   Metadata{Name: "ws3"},
			},
			Spec: WorkspaceDefSpec{
				DefaultProvider: "simple-prov",
			},
		},
	})
	reg3.Set(provScalar.QualifiedName(), provScalar)
	scoped3 := reg3.Project("app")
	if got := scoped3.ResolveModels(ModelRoleCompletion); !slicesEqual(got, []string{"simple-prov/gpt-4o"}) {
		t.Errorf("scoped provider DefaultModel fallback = %v, want [simple-prov/gpt-4o]", got)
	}
	if got := scoped3.ResolveModels(ModelRoleDecision); got != nil {
		t.Errorf("expected nil when no decision model is configured, got %v", got)
	}
}

func TestRuleResolutionAndInheritance(t *testing.T) {
	rule1 := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "global-sec"},
		},
		Spec: RuleSpec{
			Priority: 100,
			Triggers: RuleTriggers{Always: true},
		},
	}
	rule2 := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  "app",
			Metadata:   Metadata{Name: "app-style"},
		},
		Spec: RuleSpec{
			Priority: 50,
			Triggers: RuleTriggers{Globs: []string{"**/*.go"}},
		},
	}

	baseAgent := &Agent{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindAgent,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "base-agent"},
		},
		Spec: AgentSpec{
			Rules: &[]string{"global-sec"},
		},
	}

	childAgent := &Agent{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindAgent,
			Namespace:  "app",
			Metadata:   Metadata{Name: "child-agent"},
		},
		Spec: AgentSpec{
			Extends: "workspace/Agent/base-agent",
			Rules:   &[]string{"app-style"},
		},
	}

	reg := NewRegistry(nil)
	reg.Set(rule1.QualifiedName(), rule1)
	reg.Set(rule2.QualifiedName(), rule2)
	reg.Set(baseAgent.QualifiedName(), baseAgent)
	reg.Set(childAgent.QualifiedName(), childAgent)

	scoped := reg.Project("app")
	resolved, err := scoped.ResolveAgent("child-agent")
	if err != nil {
		t.Fatalf("ResolveAgent failed: %v", err)
	}

	if len(resolved.Rules) != 2 {
		t.Fatalf("expected 2 resolved rules, got %d", len(resolved.Rules))
	}
	// Highest priority (100) first
	if resolved.Rules[0].GetName() != "global-sec" {
		t.Errorf("expected first rule to be global-sec (P100), got %s", resolved.Rules[0].GetName())
	}
	if resolved.Rules[1].GetName() != "app-style" {
		t.Errorf("expected second rule to be app-style (P50), got %s", resolved.Rules[1].GetName())
	}
}

func TestResolveRulesForPath(t *testing.T) {
	rAlways := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "always-rule"},
		},
		Spec: RuleSpec{
			Priority: 10,
			Triggers: RuleTriggers{Always: true},
		},
	}

	rGo := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "go-rule"},
		},
		Spec: RuleSpec{
			Priority: 80,
			Triggers: RuleTriggers{
				Globs: []string{"**/*.go", "!**/vendor/**"},
			},
		},
	}

	rTest := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "test-rule"},
		},
		Spec: RuleSpec{
			Priority: 90,
			Triggers: RuleTriggers{
				Globs: []string{"**/*_test.go"},
			},
		},
	}

	reg := NewRegistry(nil)
	reg.Set(rAlways.QualifiedName(), rAlways)
	reg.Set(rGo.QualifiedName(), rGo)
	reg.Set(rTest.QualifiedName(), rTest)

	// Path 1: "pkg/api/handler.go" -> matches rAlways and rGo (not rTest)
	rules1 := reg.ResolveRulesForPath("pkg/api/handler.go")
	if len(rules1) != 2 {
		t.Fatalf("expected 2 rules for handler.go, got %d", len(rules1))
	}
	if rules1[0].GetName() != "go-rule" || rules1[1].GetName() != "always-rule" {
		t.Errorf("expected [go-rule, always-rule], got [%s, %s]", rules1[0].GetName(), rules1[1].GetName())
	}

	// Path 2: "pkg/api/handler_test.go" -> matches rTest (P90), rGo (P80), rAlways (P10)
	rules2 := reg.ResolveRulesForPath("pkg/api/handler_test.go")
	if len(rules2) != 3 {
		t.Fatalf("expected 3 rules for handler_test.go, got %d", len(rules2))
	}
	if rules2[0].GetName() != "test-rule" || rules2[1].GetName() != "go-rule" || rules2[2].GetName() != "always-rule" {
		t.Errorf("expected [test-rule, go-rule, always-rule], got [%s, %s, %s]", rules2[0].GetName(), rules2[1].GetName(), rules2[2].GetName())
	}

	// Path 3: "vendor/pkg/foo.go" -> rGo excluded due to !**/vendor/**; matches rAlways
	rules3 := reg.ResolveRulesForPath("vendor/pkg/foo.go")
	if len(rules3) != 1 || rules3[0].GetName() != "always-rule" {
		t.Errorf("expected only [always-rule] for vendor path, got %v", rules3)
	}

	// Path 4: "web/app.tsx" -> only rAlways
	rules4 := reg.ResolveRulesForPath("web/app.tsx")
	if len(rules4) != 1 || rules4[0].GetName() != "always-rule" {
		t.Errorf("expected only [always-rule] for tsx path, got %v", rules4)
	}
}

func TestRuleValidation(t *testing.T) {
	// Missing decision
	rBadDec := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-decision-rule"},
		},
		Spec: RuleSpec{
			Triggers: RuleTriggers{Decision: "missing-decision"},
		},
	}
	reg := NewRegistry(nil)
	reg.Set(rBadDec.QualifiedName(), rBadDec)
	if err := reg.Validate(); err == nil || !strings.Contains(err.Error(), "references missing decision") {
		t.Errorf("expected missing decision error, got %v", err)
	}

	// Missing tool in rule
	rBadTool := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-tool-rule"},
		},
		Spec: RuleSpec{
			Triggers: RuleTriggers{Tools: []string{"missing-tool"}},
		},
	}
	reg2 := NewRegistry(nil)
	reg2.Set(rBadTool.QualifiedName(), rBadTool)
	if err := reg2.Validate(); err == nil || !strings.Contains(err.Error(), "references missing tool or toolkit") {
		t.Errorf("expected missing tool error, got %v", err)
	}

	// Missing rule in agent
	agBadRule := &Agent{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindAgent,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-agent"},
		},
		Spec: AgentSpec{
			Rules: &[]string{"non-existent-rule"},
		},
	}
	reg3 := NewRegistry(nil)
	reg3.Set(agBadRule.QualifiedName(), agBadRule)
	if err := reg3.Validate(); err == nil || !strings.Contains(err.Error(), "references missing rule") {
		t.Errorf("expected missing rule error in agent, got %v", err)
	}
}
