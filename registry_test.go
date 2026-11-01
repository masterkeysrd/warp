package warp

import (
	"os"
	"path/filepath"
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

	parent := agentWithSpec(NamespaceSystem, "parent", AgentSpec{
		Policies: &Policies{
			Tools: &ToolPolicies{
				Dangerous: ToolPolicyDeny,
				OpenWorld: ToolPolicyAllow,
				Include:   []string{"git_*"},
				Exclude:   []string{"*dangerous*"},
				Ask:       []string{"git_push"},
			},
		},
	})
	reg.Set(parent.QualifiedName(), parent)

	child := agentWithSpec(NamespaceWorkspace, "child", AgentSpec{
		Extends: "system/Agent/parent",
		Policies: &Policies{
			Tools: &ToolPolicies{
				Dangerous: ToolPolicyAllow,
				Include:   []string{"ssh_*"},
				Exclude:   []string{"*evil*"},
				Ask:       []string{"ssh_connect"},
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

	// Action overrides
	if p.Tools.Dangerous != ToolPolicyAllow {
		t.Errorf("expected Dangerous to be overridden to allow, got %v", p.Tools.Dangerous)
	}
	if p.Tools.OpenWorld != ToolPolicyAllow {
		t.Errorf("expected OpenWorld to be inherited as allow, got %v", p.Tools.OpenWorld)
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

	expectedAsks := []string{"git_push", "ssh_connect"}
	if !slicesEqual(p.Tools.Ask, expectedAsks) {
		t.Errorf("expected Ask to be %v, got %v", expectedAsks, p.Tools.Ask)
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
	agent := agentWithSpec(NamespaceWorkspace, "my-agent", AgentSpec{
		Policies: &Policies{
			Tools: &ToolPolicies{
				Dangerous: ToolPolicyDeny,
				OpenWorld: ToolPolicyDeny,
				Include:   []string{"git-*", "http-*"},
				Exclude:   []string{"*clone*"},
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
	// - rm-rf: annotation IsDangerous=true, which is denied (Dangerous=deny).
	// - http-get: annotation IsOpenWorld=true, which is denied (OpenWorld=deny).
	// None of the tools should be returned!
	if len(resolved.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d: %v", len(resolved.Tools), resolved.Tools)
	}

	// Now allow open world and dangerous (by removing exclude, allowing dangerous)
	agent2 := agentWithSpec(NamespaceWorkspace, "my-agent-2", AgentSpec{
		Policies: &Policies{
			Tools: &ToolPolicies{
				Dangerous: ToolPolicyAllow,
				OpenWorld: ToolPolicyAllow,
				Include:   []string{"git-*", "http-*"},
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

func TestWorkspaceAndAgent_ToolPolicy_Intersection(t *testing.T) {
	// Setup workspace with policies
	ws := &Workspace{
		Def: &WorkspaceDef{
			BaseResource: BaseResource{
				APIVersion: APIVersion,
				Kind:       KindWorkspace,
				Metadata:   Metadata{Name: "secure-ws"},
			},
			Spec: WorkspaceDefSpec{
				Policies: &Policies{
					Tools: &ToolPolicies{
						Dangerous: ToolPolicyDeny,
						Include:   []string{"git-*", "read-file"},
						Exclude:   []string{"bash"},
					},
				},
			},
		},
	}

	reg := NewRegistry(ws)

	// Add tools:
	// - git-status (safe, matches ws include)
	// - git-push (dangerous, matches ws include but blocked by ws dangerous=deny)
	// - bash (excluded by ws)
	// - read-file (safe, matches ws include)
	// - http-get (safe, not in ws include)
	tGitStatus := makeToolResource(NamespaceWorkspace, "git-status")
	tGitPush := makeToolResource(NamespaceWorkspace, "git-push")
	tGitPush.Spec.Annotations = &ToolAnnotation{IsDangerous: true}
	tBash := makeToolResource(NamespaceWorkspace, "bash")
	tReadFile := makeToolResource(NamespaceWorkspace, "read-file")
	tHTTPGET := makeToolResource(NamespaceWorkspace, "http-get")

	reg.Set(tGitStatus.QualifiedName(), tGitStatus)
	reg.Set(tGitPush.QualifiedName(), tGitPush)
	reg.Set(tBash.QualifiedName(), tBash)
	reg.Set(tReadFile.QualifiedName(), tReadFile)
	reg.Set(tHTTPGET.QualifiedName(), tHTTPGET)

	// 1. Validate() succeeds even though the registry contains tools excluded or disallowed by workspace policy
	if err := reg.Validate(); err != nil {
		t.Fatalf("Validate() should succeed with excluded tools, got: %v", err)
	}

	// 2. Agent with no policies: receives the tools allowed by workspace policy ceiling
	// Expected: git-status, read-file
	agentDefault := agentWithSpec(NamespaceWorkspace, "agent-default", AgentSpec{})
	reg.Set(agentDefault.QualifiedName(), agentDefault)

	resDefault, err := reg.ResolveAgent("workspace/Agent/agent-default")
	if err != nil {
		t.Fatalf("ResolveAgent(agent-default): %v", err)
	}
	var defaultNames []string
	for _, tool := range resDefault.Tools {
		defaultNames = append(defaultNames, tool.Metadata.Name)
	}
	slices.Sort(defaultNames)
	expectedDefault := []string{"git-status", "read-file"}
	if !slicesEqual(defaultNames, expectedDefault) {
		t.Errorf("expected default agent to get %v, got %v", expectedDefault, defaultNames)
	}

	// 3. Workspace excludes a tool (`exclude: ["bash"]`) and Agent includes it (`include: ["bash", "read-file"]`)
	// Only `read-file` should be in resolved.Tools.
	agentBashAndRead := agentWithSpec(NamespaceWorkspace, "agent-bash-read", AgentSpec{
		Policies: &Policies{
			Tools: &ToolPolicies{
				Include: []string{"bash", "read-file"},
			},
		},
	})
	reg.Set(agentBashAndRead.QualifiedName(), agentBashAndRead)

	resBashRead, err := reg.ResolveAgent("workspace/Agent/agent-bash-read")
	if err != nil {
		t.Fatalf("ResolveAgent(agent-bash-read): %v", err)
	}
	var bashReadNames []string
	for _, tool := range resBashRead.Tools {
		bashReadNames = append(bashReadNames, tool.Metadata.Name)
	}
	expectedBashRead := []string{"read-file"}
	if !slicesEqual(bashReadNames, expectedBashRead) {
		t.Errorf("expected agent with bash+read-file to get %v, got %v", expectedBashRead, bashReadNames)
	}

	// 4. Workspace sets `dangerous: deny` and Agent sets `dangerous: allow`
	// Dangerous tool (git-push) must still be filtered out because workspace is the ceiling.
	agentWantDangerous := agentWithSpec(NamespaceWorkspace, "agent-dangerous", AgentSpec{
		Policies: &Policies{
			Tools: &ToolPolicies{
				Dangerous: ToolPolicyAllow,
				Include:   []string{"git-*"},
			},
		},
	})
	reg.Set(agentWantDangerous.QualifiedName(), agentWantDangerous)

	resDangerous, err := reg.ResolveAgent("workspace/Agent/agent-dangerous")
	if err != nil {
		t.Fatalf("ResolveAgent(agent-dangerous): %v", err)
	}
	for _, tool := range resDangerous.Tools {
		if tool.Metadata.Name == "git-push" {
			t.Errorf("expected dangerous tool git-push to be filtered out by workspace ceiling, but it was present")
		}
	}
	var dangerousNames []string
	for _, tool := range resDangerous.Tools {
		dangerousNames = append(dangerousNames, tool.Metadata.Name)
	}
	expectedDangerous := []string{"git-status"}
	if !slicesEqual(dangerousNames, expectedDangerous) {
		t.Errorf("expected agent with dangerous: allow to get %v, got %v", expectedDangerous, dangerousNames)
	}

	// 5. Workspace sets `include: ["git-*", "read-file"]` and Agent sets `include: ["git-status", "http-get"]`
	// Only `git-status` is in resolved.Tools (http-get was blocked by workspace, git-push was blocked by dangerous/not in agent, read-file was not in agent).
	agentGitAndHTTP := agentWithSpec(NamespaceWorkspace, "agent-git-http", AgentSpec{
		Policies: &Policies{
			Tools: &ToolPolicies{
				Include: []string{"git-status", "http-get"},
			},
		},
	})
	reg.Set(agentGitAndHTTP.QualifiedName(), agentGitAndHTTP)

	resGitHTTP, err := reg.ResolveAgent("workspace/Agent/agent-git-http")
	if err != nil {
		t.Fatalf("ResolveAgent(agent-git-http): %v", err)
	}
	var gitHTTPNames []string
	for _, tool := range resGitHTTP.Tools {
		gitHTTPNames = append(gitHTTPNames, tool.Metadata.Name)
	}
	expectedGitHTTP := []string{"git-status"}
	if !slicesEqual(gitHTTPNames, expectedGitHTTP) {
		t.Errorf("expected agent with git-status+http-get to get %v, got %v", expectedGitHTTP, gitHTTPNames)
	}

	// 6. Test with ScopedRegistry as well
	scoped := reg.Project("my-proj")
	resScoped, err := scoped.ResolveAgent("agent-bash-read")
	if err != nil {
		t.Fatalf("scoped.ResolveAgent: %v", err)
	}
	var scopedNames []string
	for _, tool := range resScoped.Tools {
		scopedNames = append(scopedNames, tool.Metadata.Name)
	}
	if !slicesEqual(scopedNames, expectedBashRead) {
		t.Errorf("expected scoped agent to get %v, got %v", expectedBashRead, scopedNames)
	}

	// 7. Test ResolveToolsForSkill respects workspace policy
	skill := &Skill{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindSkill,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "my-skill"},
		},
		Spec: SkillSpec{
			Tools: []string{"git-status", "bash", "http-get", "git-push"},
		},
	}
	reg.Set(skill.QualifiedName(), skill)

	skillTools, err := reg.ToolsForSkill("workspace/Skill/my-skill")
	if err != nil {
		t.Fatalf("ToolsForSkill: %v", err)
	}
	var skillToolNames []string
	for _, t := range skillTools {
		skillToolNames = append(skillToolNames, t.Metadata.Name)
	}
	expectedSkillTools := []string{"git-status"}
	if !slicesEqual(skillToolNames, expectedSkillTools) {
		t.Errorf("expected skill tools to be filtered by workspace policy to %v, got %v", expectedSkillTools, skillToolNames)
	}

	scopedSkillTools, err := scoped.ToolsForSkill("my-skill")
	if err != nil {
		t.Fatalf("scoped.ToolsForSkill: %v", err)
	}
	var scopedSkillToolNames []string
	for _, t := range scopedSkillTools {
		scopedSkillToolNames = append(scopedSkillToolNames, t.Metadata.Name)
	}
	if !slicesEqual(scopedSkillToolNames, expectedSkillTools) {
		t.Errorf("expected scoped skill tools to be filtered by workspace policy to %v, got %v", expectedSkillTools, scopedSkillToolNames)
	}
}

func TestToolPolicies_EvaluateTool(t *testing.T) {
	safeTool := makeToolResource(NamespaceWorkspace, "safe-tool")
	dangerousTool := makeToolResource(NamespaceWorkspace, "dangerous-tool")
	dangerousTool.Spec.Annotations = &ToolAnnotation{IsDangerous: true}
	openWorldTool := makeToolResource(NamespaceWorkspace, "open-world-tool")
	openWorldTool.Spec.Annotations = &ToolAnnotation{IsOpenWorld: true}

	// 1. Nil tool -> deny
	var nilPolicy *ToolPolicies
	if act := nilPolicy.EvaluateTool(nil); act != ToolPolicyDeny {
		t.Errorf("expected deny for nil tool, got %v", act)
	}

	// 2. Nil policy: safe -> allow, dangerous -> ask, openWorld -> allow
	if act := nilPolicy.EvaluateTool(safeTool); act != ToolPolicyAllow {
		t.Errorf("expected allow for safe tool with nil policy, got %v", act)
	}
	if act := nilPolicy.EvaluateTool(dangerousTool); act != ToolPolicyAsk {
		t.Errorf("expected ask for dangerous tool with nil policy, got %v", act)
	}
	if act := nilPolicy.EvaluateTool(openWorldTool); act != ToolPolicyAllow {
		t.Errorf("expected allow for open-world tool with nil policy, got %v", act)
	}

	// 3. Defaults when policy is empty struct: dangerous -> ask, openWorld -> allow
	emptyPolicy := &ToolPolicies{}
	if act := emptyPolicy.EvaluateTool(dangerousTool); act != ToolPolicyAsk {
		t.Errorf("expected ask for dangerous tool with empty policy, got %v", act)
	}
	if act := emptyPolicy.EvaluateTool(openWorldTool); act != ToolPolicyAllow {
		t.Errorf("expected allow for open-world tool with empty policy, got %v", act)
	}

	// 4. Dangerous: allow / deny
	allowDangPolicy := &ToolPolicies{Dangerous: ToolPolicyAllow}
	if act := allowDangPolicy.EvaluateTool(dangerousTool); act != ToolPolicyAllow {
		t.Errorf("expected allow for dangerous tool, got %v", act)
	}
	denyDangPolicy := &ToolPolicies{Dangerous: ToolPolicyDeny}
	if act := denyDangPolicy.EvaluateTool(dangerousTool); act != ToolPolicyDeny {
		t.Errorf("expected deny for dangerous tool, got %v", act)
	}

	// 5. OpenWorld: ask / deny
	askOWPolicy := &ToolPolicies{OpenWorld: ToolPolicyAsk}
	if act := askOWPolicy.EvaluateTool(openWorldTool); act != ToolPolicyAsk {
		t.Errorf("expected ask for open-world tool, got %v", act)
	}
	denyOWPolicy := &ToolPolicies{OpenWorld: ToolPolicyDeny}
	if act := denyOWPolicy.EvaluateTool(openWorldTool); act != ToolPolicyDeny {
		t.Errorf("expected deny for open-world tool, got %v", act)
	}

	// 6. Ask pattern list
	askPatternPolicy := &ToolPolicies{
		Ask: []string{"safe-*"},
	}
	if act := askPatternPolicy.EvaluateTool(safeTool); act != ToolPolicyAsk {
		t.Errorf("expected ask for safeTool matching ask pattern, got %v", act)
	}

	// 7. Include / Exclude
	incPolicy := &ToolPolicies{
		Include: []string{"safe-*"},
	}
	if act := incPolicy.EvaluateTool(safeTool); act != ToolPolicyAllow {
		t.Errorf("expected allow for safeTool in include list, got %v", act)
	}
	if act := incPolicy.EvaluateTool(openWorldTool); act != ToolPolicyDeny {
		t.Errorf("expected deny for openWorldTool not in include list, got %v", act)
	}

	excPolicy := &ToolPolicies{
		Exclude: []string{"*tool"},
	}
	if act := excPolicy.EvaluateTool(safeTool); act != ToolPolicyDeny {
		t.Errorf("expected deny for safeTool matching exclude list, got %v", act)
	}
}

func TestResolvedAgent_ToolAction(t *testing.T) {
	safeTool := makeToolResource(NamespaceWorkspace, "safe-tool")
	dangerousTool := makeToolResource(NamespaceWorkspace, "dangerous-tool")
	dangerousTool.Spec.Annotations = &ToolAnnotation{IsDangerous: true}

	// 1. Nil ResolvedAgent or nil Tool
	var nilAgent *ResolvedAgent
	if act := nilAgent.ToolAction(safeTool); act != ToolPolicyDeny {
		t.Errorf("expected deny for nil ResolvedAgent, got %v", act)
	}
	raBase := &ResolvedAgent{}
	if act := raBase.ToolAction(nil); act != ToolPolicyDeny {
		t.Errorf("expected deny for nil tool, got %v", act)
	}

	// 2. Default resolution: dangerous tool yields ask
	if act := raBase.ToolAction(dangerousTool); act != ToolPolicyAsk {
		t.Errorf("expected ask for dangerous tool with no policies, got %v", act)
	}
	if act := raBase.ToolAction(safeTool); act != ToolPolicyAllow {
		t.Errorf("expected allow for safe tool with no policies, got %v", act)
	}

	// 3. Workspace sets dangerous: ask, Agent sets dangerous: allow
	// Stricter (ask) must prevail
	raWsAskAgAllow := &ResolvedAgent{
		WorkspacePolicy: &ToolPolicies{
			Dangerous: ToolPolicyAsk,
		},
		Agent: agentWithSpec(NamespaceWorkspace, "ag", AgentSpec{
			Policies: &Policies{
				Tools: &ToolPolicies{
					Dangerous: ToolPolicyAllow,
				},
			},
		}),
	}
	if act := raWsAskAgAllow.ToolAction(dangerousTool); act != ToolPolicyAsk {
		t.Errorf("expected workspace ask ceiling to override agent allow, got %v", act)
	}

	// 4. Workspace sets dangerous: allow, Agent sets dangerous: allow -> allow
	raBothAllow := &ResolvedAgent{
		WorkspacePolicy: &ToolPolicies{
			Dangerous: ToolPolicyAllow,
		},
		Agent: agentWithSpec(NamespaceWorkspace, "ag", AgentSpec{
			Policies: &Policies{
				Tools: &ToolPolicies{
					Dangerous: ToolPolicyAllow,
				},
			},
		}),
	}
	if act := raBothAllow.ToolAction(dangerousTool); act != ToolPolicyAllow {
		t.Errorf("expected allow when both workspace and agent allow, got %v", act)
	}

	// 5. Workspace sets dangerous: allow, Agent sets dangerous: ask -> ask (agent narrows)
	raWsAllowAgAsk := &ResolvedAgent{
		WorkspacePolicy: &ToolPolicies{
			Dangerous: ToolPolicyAllow,
		},
		Agent: agentWithSpec(NamespaceWorkspace, "ag", AgentSpec{
			Policies: &Policies{
				Tools: &ToolPolicies{
					Dangerous: ToolPolicyAsk,
				},
			},
		}),
	}
	if act := raWsAllowAgAsk.ToolAction(dangerousTool); act != ToolPolicyAsk {
		t.Errorf("expected agent narrowing to ask, got %v", act)
	}

	// 6. Workspace ask pattern vs Agent allow
	raWsAskPattern := &ResolvedAgent{
		WorkspacePolicy: &ToolPolicies{
			Ask: []string{"safe-*"},
		},
	}
	if act := raWsAskPattern.ToolAction(safeTool); act != ToolPolicyAsk {
		t.Errorf("expected workspace ask pattern to produce ask, got %v", act)
	}
}

func TestToolPolicies_ValidationErrors(t *testing.T) {
	// Invalid workspace dangerous action
	reg1 := NewRegistry(&Workspace{
		Def: &WorkspaceDef{
			BaseResource: BaseResource{
				APIVersion: APIVersion,
				Kind:       KindWorkspace,
				Metadata:   Metadata{Name: "bad-ws"},
			},
			Spec: WorkspaceDefSpec{
				Policies: &Policies{
					Tools: &ToolPolicies{
						Dangerous: "invalid-action",
					},
				},
			},
		},
	})
	if err := reg1.Validate(); err == nil || !strings.Contains(err.Error(), "invalid dangerous action") {
		t.Errorf("expected invalid dangerous action error, got %v", err)
	}

	// Invalid agent openWorld action
	reg2 := NewRegistry(nil)
	badAgent := agentWithSpec(NamespaceWorkspace, "bad-agent", AgentSpec{
		Policies: &Policies{
			Tools: &ToolPolicies{
				OpenWorld: "maybe",
			},
		},
	})
	reg2.Set(badAgent.QualifiedName(), badAgent)
	if err := reg2.Validate(); err == nil || !strings.Contains(err.Error(), "invalid openWorld action") {
		t.Errorf("expected invalid openWorld action error, got %v", err)
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
		Subagents: []string{"system/Agent/agent-b"},
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

	// 3. Resolve Agent with omitted subagents (defaults to none)
	resolvedB, err := scoped.ResolveAgent("agent-b")
	if err != nil {
		t.Fatalf("ResolveAgent(agent-b): %v", err)
	}
	if len(resolvedB.Subagents) != 0 {
		t.Errorf("expected 0 subagents when omitted, got %d (agents: %v)", len(resolvedB.Subagents), resolvedB.Subagents)
	}

	// 3b. Resolve Agent with explicitly empty subagents (should return exactly 0 subagents)
	agentEmptySubagents := agentWithSpec(NamespaceSystem, "agent-empty-subagents", AgentSpec{
		Subagents: []string{},
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
		Subagents: []string{"system/Agent/missing"},
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
		Subagents: []string{"*"},
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

func TestAgentResolution_GlobCapabilities(t *testing.T) {
	reg := NewRegistry(nil)
	reg.AddProject(&Project{Name: "app", Path: ".", RootPath: "/repo"})

	// Plugin skill & command under namespace "warp-dev"
	pluginSkill := makeSkillResource("warp-dev", "warp-developer")
	pluginCmd := makeCommandResource("warp-dev", "scaffold")
	otherSkill := makeSkillResource("other-plugin", "ignored-skill")
	reg.Set(pluginSkill.QualifiedName(), pluginSkill)
	reg.Set(pluginCmd.QualifiedName(), pluginCmd)
	reg.Set(otherSkill.QualifiedName(), otherSkill)

	// Standalone tools
	refTool := makeToolResource(NamespaceWorkspace, "list-files")
	refTool.Spec.Command = []string{"ls", "-la"}
	refTool.Spec.Description = "List files"
	reg.Set(refTool.QualifiedName(), refTool)

	echoTool := makeToolResource(NamespaceWorkspace, "echo-tool")
	echoTool.Spec.Command = []string{"echo"}
	echoTool.Spec.Description = "Echo tool"
	reg.Set(echoTool.QualifiedName(), echoTool)

	agent := agentWithSpec("app", "dev-agent", AgentSpec{
		Skills:   []string{"warp-dev/*"},
		Commands: []string{"warp-dev/Command/*"},
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
		t.Fatalf("expected 2 tools, got %d: %v", len(resolved.Tools), resolved.Tools)
	}
	if resolved.Tools[0].Metadata.Name != "echo-tool" || resolved.Tools[1].Metadata.Name != "list-files" {
		t.Errorf("expected deterministic sorted tools [echo-tool, list-files], got [%s, %s]",
			resolved.Tools[0].Metadata.Name, resolved.Tools[1].Metadata.Name)
	}
	if resolved.Tools[0].Metadata.Description != "Echo tool" {
		t.Errorf("expected echo-tool description to be populated, got %q", resolved.Tools[0].Metadata.Description)
	}
}

func TestValidate_KindAwareness_And_Inheritance(t *testing.T) {
	// 1. Referencing a Tool where a Skill is expected must fail validation.
	reg1 := NewRegistry(nil)
	tool := makeToolResource(NamespaceWorkspace, "not-a-skill")
	tool.Spec.Command = []string{"echo"}
	agentWrongKind := agentWithSpec(NamespaceWorkspace, "bad-agent", AgentSpec{
		Skills: []string{"not-a-skill"},
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

func TestValidate_CommandAgentAndContext(t *testing.T) {
	// 1. Missing agent reference
	reg1 := NewRegistry(nil)
	cmdBadAgent := &Command{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindCommand,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-cmd"},
		},
		Spec: CommandSpec{
			Agent: "missing-agent",
		},
	}
	reg1.Set(cmdBadAgent.QualifiedName(), cmdBadAgent)
	if err := reg1.Validate(); err == nil || !strings.Contains(err.Error(), "missing agent") {
		t.Errorf("expected missing agent error, got: %v", err)
	}

	// 2. Invalid context mode
	reg2 := NewRegistry(nil)
	cmdBadCtx := &Command{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindCommand,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-ctx-cmd"},
		},
		Spec: CommandSpec{
			Context: "invalid-mode",
		},
	}
	reg2.Set(cmdBadCtx.QualifiedName(), cmdBadCtx)
	if err := reg2.Validate(); err == nil || !strings.Contains(err.Error(), "invalid context") {
		t.Errorf("expected invalid context error, got: %v", err)
	}

	// 3. Valid command with agent and context
	reg3 := NewRegistry(nil)
	ag := agentWithSpec(NamespaceWorkspace, "my-agent", AgentSpec{})
	cmdGood := &Command{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindCommand,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "good-cmd"},
		},
		Spec: CommandSpec{
			Agent:   "my-agent",
			Context: CommandContextClean,
		},
	}
	reg3.Set(ag.QualifiedName(), ag)
	reg3.Set(cmdGood.QualifiedName(), cmdGood)
	if err := reg3.Validate(); err != nil {
		t.Errorf("expected Validate() to pass for valid command, got: %v", err)
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
			Thinking:     &AgentThinking{Type: ThinkingStrategyEffort, Effort: "low"},
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
			Thinking:     &AgentThinking{Type: ThinkingStrategyBudget, BudgetTokens: 4096},
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
	if ag.Spec.Thinking == nil || ag.Spec.Thinking.Type != ThinkingStrategyBudget || ag.Spec.Thinking.BudgetTokens != 4096 {
		t.Errorf("expected child thinking override, got %v", ag.Spec.Thinking)
	}
	if !slicesEqual(ag.Spec.Triggers, []string{"human"}) {
		t.Errorf("expected child triggers override, got %v", ag.Spec.Triggers)
	}
}

func resolvedQualifiedIDs(models []ResolvedModel) []string {
	if models == nil {
		return nil
	}
	out := make([]string, len(models))
	for i, m := range models {
		out[i] = m.QualifiedID
	}
	return out
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
			Type:     "ollama",
			Endpoint: "http://localhost:11434",
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
	gotExplicit := resolvedQualifiedIDs(reg1.ResolveModels(ModelRoleCompletion, "", "qwen2.5", "anthropic/claude-3-7-sonnet"))
	wantExplicit := []string{"local-ai/qwen2.5", "anthropic/claude-3-7-sonnet"}
	if !slicesEqual(gotExplicit, wantExplicit) {
		t.Errorf("explicit ResolveModels = %v, want %v", gotExplicit, wantExplicit)
	}

	// 1b. Workspace defaultModels.<role> wins when explicit is empty
	gotWsComp := resolvedQualifiedIDs(reg1.ResolveModels(ModelRoleCompletion))
	if !slicesEqual(gotWsComp, []string{"local-ai/ws-comp-model"}) {
		t.Errorf("workspace completion ResolveModels = %v, want [local-ai/ws-comp-model]", gotWsComp)
	}
	gotWsDec := resolvedQualifiedIDs(reg1.ResolveModels(ModelRoleDecision))
	if !slicesEqual(gotWsDec, []string{"typesafe/jev-latest"}) {
		t.Errorf("workspace decision ResolveModels = %v, want [typesafe/jev-latest]", gotWsDec)
	}

	// 1c. Falls through to DefaultProvider's defaultModels.embedding when workspace omits embedding default
	gotProvEmb := resolvedQualifiedIDs(reg1.ResolveModels(ModelRoleEmbedding))
	if !slicesEqual(gotProvEmb, []string{"local-ai/nomic-embed-text"}) {
		t.Errorf("provider embedding ResolveModels = %v, want [local-ai/nomic-embed-text]", gotProvEmb)
	}

	// Case 2: Workspace defines DefaultModels and DefaultProvider
	reg2 := NewRegistry(&Workspace{
		Def: &WorkspaceDef{
			BaseResource: BaseResource{
				APIVersion: APIVersion,
				Kind:       KindWorkspace,
				Metadata:   Metadata{Name: "ws2"},
			},
			Spec: WorkspaceDefSpec{
				DefaultProvider: "local-ai",
				DefaultModels: &ModelDefaults{
					Completion: "scalar-ws-model",
				},
			},
		},
	})
	reg2.Set(provider.QualifiedName(), provider)
	if got := resolvedQualifiedIDs(reg2.ResolveModels(ModelRoleCompletion)); !slicesEqual(got, []string{"local-ai/scalar-ws-model"}) {
		t.Errorf("workspace DefaultModels fallback = %v, want [local-ai/scalar-ws-model]", got)
	}

	// Case 3: Provider defines DefaultModels
	provScalar := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "simple-prov"},
		},
		Spec: ModelProviderSpec{
			Type:     "openai",
			Endpoint: "https://api.openai.com/v1",
			DefaultModels: &ModelDefaults{
				Completion: "gpt-4o",
			},
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
	if got := resolvedQualifiedIDs(scoped3.ResolveModels(ModelRoleCompletion)); !slicesEqual(got, []string{"simple-prov/gpt-4o"}) {
		t.Errorf("scoped provider DefaultModels fallback = %v, want [simple-prov/gpt-4o]", got)
	}
	if got := scoped3.ResolveModels(ModelRoleDecision); got != nil {
		t.Errorf("expected nil when no decision model is configured, got %v", got)
	}
}

func TestResolveModels_SlashModelIDsAndCatalogLookup(t *testing.T) {
	mlxProvider := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "mlx-serve"},
		},
		Spec: ModelProviderSpec{
			Type:     "openai",
			Endpoint: "http://localhost:8080/v1",
			DefaultModels: &ModelDefaults{
				Completion: "ddalcu/Qwen3.8-27B-MLX-Serve-4bit",
			},
			Models: []ProviderModel{
				{
					ID:    "ddalcu/Qwen3.8-27B-MLX-Serve-4bit",
					Name:  "qwen3.8-27b-mlx-serve-4bit",
					Label: "Qwen 3.8 27B MLX 4-bit",
					Limits: ProviderModelLimits{
						Context: 32768,
						Output:  8192,
					},
				},
			},
		},
	}

	// Workspace without DefaultProvider set
	reg := NewRegistry(&Workspace{
		Def: &WorkspaceDef{
			BaseResource: BaseResource{
				APIVersion: APIVersion,
				Kind:       KindWorkspace,
				Metadata:   Metadata{Name: "ws-mlx"},
			},
		},
	})
	reg.Set(mlxProvider.QualifiedName(), mlxProvider)

	want := []string{"mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit"}

	// 1a. Slash-containing model ID resolves to owning provider
	if got := resolvedQualifiedIDs(reg.ResolveModels(ModelRoleCompletion, "ddalcu/Qwen3.8-27B-MLX-Serve-4bit")); !slicesEqual(got, want) {
		t.Errorf("ResolveModels(slash ID) = %v, want %v", got, want)
	}

	// 1b. Model name alias resolves to canonical <provider>/<model.id>
	if got := resolvedQualifiedIDs(reg.ResolveModels(ModelRoleCompletion, "qwen3.8-27b-mlx-serve-4bit")); !slicesEqual(got, want) {
		t.Errorf("ResolveModels(name alias) = %v, want %v", got, want)
	}

	// 1c. Already-qualified <provider>/<slash-id> is preserved
	if got := resolvedQualifiedIDs(reg.ResolveModels(ModelRoleCompletion, "mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit")); !slicesEqual(got, want) {
		t.Errorf("ResolveModels(qualified slash ID) = %v, want %v", got, want)
	}

	// 1d. Qualified <provider>/<name-alias> canonicalizes to <provider>/<model.id>
	if got := resolvedQualifiedIDs(reg.ResolveModels(ModelRoleCompletion, "mlx-serve/qwen3.8-27b-mlx-serve-4bit")); !slicesEqual(got, want) {
		t.Errorf("ResolveModels(qualified name alias) = %v, want %v", got, want)
	}

	// 2. Provider defaultModels containing slash resolves when workspace has no defaultProvider
	if got := resolvedQualifiedIDs(reg.ResolveModels(ModelRoleCompletion)); !slicesEqual(got, want) {
		t.Errorf("ResolveModels(provider defaultModels with slash) = %v, want %v", got, want)
	}

	// 3. ResolveModel catalog lookup returns *ResolvedModel with Provider, Model, ModelID, and QualifiedID
	for _, ref := range []string{
		"",
		"ddalcu/Qwen3.8-27B-MLX-Serve-4bit",
		"qwen3.8-27b-mlx-serve-4bit",
		"mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit",
		"mlx-serve/qwen3.8-27b-mlx-serve-4bit",
	} {
		var explicit []string
		if ref != "" {
			explicit = []string{ref}
		}
		rm, ok := reg.ResolveModel(ModelRoleCompletion, explicit...)
		if !ok || rm == nil {
			t.Fatalf("ResolveModel(%q) returned ok=%v, rm=%v", ref, ok, rm)
		}
		if rm.Provider == nil || rm.Provider.Metadata.Name != "mlx-serve" {
			t.Errorf("ResolveModel(%q) provider = %v, want mlx-serve", ref, rm.Provider)
		}
		if rm.Model == nil {
			t.Fatalf("ResolveModel(%q) returned nil Model", ref)
		}
		if rm.Model.ID != "ddalcu/Qwen3.8-27B-MLX-Serve-4bit" || rm.Model.Label != "Qwen 3.8 27B MLX 4-bit" || rm.Model.Limits.Context != 32768 {
			t.Errorf("ResolveModel(%q) model = %+v", ref, rm.Model)
		}
		if rm.ModelID != "ddalcu/Qwen3.8-27B-MLX-Serve-4bit" {
			t.Errorf("ResolveModel(%q) ModelID = %q, want %q", ref, rm.ModelID, "ddalcu/Qwen3.8-27B-MLX-Serve-4bit")
		}
		if rm.QualifiedID != "mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit" {
			t.Errorf("ResolveModel(%q) QualifiedID = %q, want %q", ref, rm.QualifiedID, "mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit")
		}
	}

	// ScopedRegistry ResolveModel also works
	scoped := reg.Project("app")
	if rm, ok := scoped.ResolveModel(ModelRoleCompletion, "qwen3.8-27b-mlx-serve-4bit"); !ok || rm == nil || rm.Provider == nil || rm.Model == nil || rm.QualifiedID != "mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit" {
		t.Errorf("ScopedRegistry.ResolveModel unexpected result: rm=%+v ok=%v", rm, ok)
	}

	// 4. Unregistered pass-through reference is preserved by ResolveModels and returns ok=false on ResolveModel
	if got := resolvedQualifiedIDs(reg.ResolveModels(ModelRoleCompletion, "anthropic/claude-3-7-sonnet")); !slicesEqual(got, []string{"anthropic/claude-3-7-sonnet"}) {
		t.Errorf("ResolveModels(unregistered pass-through) = %v, want [anthropic/claude-3-7-sonnet]", got)
	}
	if rm, ok := reg.ResolveModel(ModelRoleCompletion, "anthropic/claude-3-7-sonnet"); ok || rm != nil {
		t.Errorf("ResolveModel(unregistered) = (%+v, %v), want (nil, false)", rm, ok)
	}

	// 5. Fallback to capability-matching models when neither workspace nor provider sets defaultModels
	capProvider := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "mlx-cap"},
		},
		Spec: ModelProviderSpec{
			Type:     "openai",
			Endpoint: "http://localhost:8080/v1",
			Models: []ProviderModel{
				{
					ID:    "ddalcu/Qwen3.8-27B-MLX-Serve-4bit",
					Name:  "qwen3.8-27b-mlx-serve-4bit",
					Label: "Qwen 3.8 27B",
					Limits: ProviderModelLimits{
						Context: 32768,
					},
				},
			},
		},
	}
	regCap := NewRegistry(nil)
	regCap.Set(capProvider.QualifiedName(), capProvider)
	if got := resolvedQualifiedIDs(regCap.ResolveModels(ModelRoleCompletion)); !slicesEqual(got, []string{"mlx-cap/ddalcu/Qwen3.8-27B-MLX-Serve-4bit"}) {
		t.Errorf("ResolveModels(capability fallback) = %v, want [mlx-cap/ddalcu/Qwen3.8-27B-MLX-Serve-4bit]", got)
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
			Rules: []string{"global-sec"},
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
			Rules:   []string{"app-style"},
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
	if err := reg2.Validate(); err == nil || !strings.Contains(err.Error(), "references missing tool: missing-tool") {
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
			Rules: []string{"non-existent-rule"},
		},
	}
	reg3 := NewRegistry(nil)
	reg3.Set(agBadRule.QualifiedName(), agBadRule)
	if err := reg3.Validate(); err == nil || !strings.Contains(err.Error(), "references missing rule") {
		t.Errorf("expected missing rule error in agent, got %v", err)
	}
}

func TestRegistry_Delete(t *testing.T) {
	reg := NewRegistry(nil)
	skill := makeSkillResource(NamespaceWorkspace, "my-skill")
	reg.Set(skill.QualifiedName(), skill)

	if _, ok := reg.get(skill.QualifiedName()); !ok {
		t.Fatalf("expected skill to exist")
	}

	if !reg.Delete(skill.QualifiedName()) {
		t.Errorf("expected Delete to return true for existing resource")
	}

	if _, ok := reg.get(skill.QualifiedName()); ok {
		t.Errorf("expected skill to be deleted")
	}

	if reg.Delete(skill.QualifiedName()) {
		t.Errorf("expected Delete to return false for already deleted resource")
	}
}

func TestComputeReloadStats(t *testing.T) {
	s1 := makeSkillResource(NamespaceWorkspace, "s1")
	s2 := makeSkillResource(NamespaceWorkspace, "s2")
	c1 := makeCommandResource(NamespaceWorkspace, "c1")

	// Old: s1, c1
	oldRes := map[string]Resource{
		s1.QualifiedName(): s1,
		c1.QualifiedName(): c1,
	}

	// New: s1 (modified description), s2 (added), (c1 removed)
	s1Mod := makeSkillResource(NamespaceWorkspace, "s1")
	s1Mod.Metadata.Description = "updated description"
	newRes := map[string]Resource{
		s1Mod.QualifiedName(): s1Mod,
		s2.QualifiedName():    s2,
	}

	stats := computeReloadStats(oldRes, newRes)
	if stats.Total.Added != 1 {
		t.Errorf("expected Total.Added=1, got %d", stats.Total.Added)
	}
	if stats.Total.Updated != 1 {
		t.Errorf("expected Total.Updated=1, got %d", stats.Total.Updated)
	}
	if stats.Total.Removed != 1 {
		t.Errorf("expected Total.Removed=1, got %d", stats.Total.Removed)
	}
	if stats.Total.Unchanged != 0 {
		t.Errorf("expected Total.Unchanged=0, got %d", stats.Total.Unchanged)
	}
	if !stats.HasChanges() {
		t.Errorf("expected HasChanges=true")
	}

	skillStats := stats.ForKind(KindSkill)
	if skillStats.Added != 1 || skillStats.Updated != 1 || skillStats.Removed != 0 {
		t.Errorf("unexpected Skill stats: %+v", skillStats)
	}
	if !skillStats.HasChanges() {
		t.Errorf("expected Skill HasChanges=true")
	}

	cmdStats := stats.ForKind(KindCommand)
	if cmdStats.Added != 0 || cmdStats.Updated != 0 || cmdStats.Removed != 1 {
		t.Errorf("unexpected Command stats: %+v", cmdStats)
	}

	ruleStats := stats.ForKind(KindRule)
	if ruleStats.HasChanges() {
		t.Errorf("expected Rule HasChanges=false")
	}

	var nilStats *ReloadStats
	if nilStats.HasChanges() {
		t.Errorf("expected nil ReloadStats HasChanges to return false")
	}
	if nilStats.ForKind(KindSkill).HasChanges() {
		t.Errorf("expected nil ReloadStats ForKind to return empty stats")
	}
}

func TestRegistry_Reload_UnsetWorkspace(t *testing.T) {
	reg := NewRegistry(nil)
	if _, err := reg.Reload(); err == nil {
		t.Errorf("expected error when reloading registry with unset workspace root")
	}
}

func TestRegistry_Reload_WorkspaceLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "warp-reload-*")
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

	agentPath := filepath.Join(agentsDir, "agent.md")
	agentMd := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: dev-agent
spec:
  description: initial agent
---
Initial instructions.
`
	if err := os.WriteFile(agentPath, []byte(agentMd), 0644); err != nil {
		t.Fatal(err)
	}

	cmdPath := filepath.Join(agentsDir, "build.md")
	cmdMd := `---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: build
spec:
  description: build command
---
`
	if err := os.WriteFile(cmdPath, []byte(cmdMd), 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}

	// 1. Reload with no changes
	stats, err := reg.Reload()
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if stats.HasChanges() {
		t.Errorf("expected no changes on identical reload, got %+v", stats)
	}
	if stats.Total.Unchanged == 0 {
		t.Errorf("expected unchanged resources, got 0")
	}

	// 2. Perform changes on disk:
	// - Add skill
	skillPath := filepath.Join(agentsDir, "test-skill.md")
	skillMd := `---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: test-skill
spec:
  description: testing skill
---
`
	if err := os.WriteFile(skillPath, []byte(skillMd), 0644); err != nil {
		t.Fatal(err)
	}

	// - Update agent
	updatedAgentMd := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: dev-agent
spec:
  description: updated agent
---
Updated instructions.
`
	if err := os.WriteFile(agentPath, []byte(updatedAgentMd), 0644); err != nil {
		t.Fatal(err)
	}

	// - Delete command
	if err := os.Remove(cmdPath); err != nil {
		t.Fatal(err)
	}

	// Reload
	stats, err = reg.Reload()
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if !stats.HasChanges() {
		t.Errorf("expected changes on reload, got none")
	}
	if stats.Total.Added != 1 {
		t.Errorf("expected 1 added, got %d", stats.Total.Added)
	}
	if stats.Total.Updated != 1 {
		t.Errorf("expected 1 updated, got %d", stats.Total.Updated)
	}
	if stats.Total.Removed != 1 {
		t.Errorf("expected 1 removed, got %d", stats.Total.Removed)
	}

	if stats.ForKind(KindSkill).Added != 1 {
		t.Errorf("expected 1 added Skill, got %d", stats.ForKind(KindSkill).Added)
	}
	if stats.ForKind(KindAgent).Updated != 1 {
		t.Errorf("expected 1 updated Agent, got %d", stats.ForKind(KindAgent).Updated)
	}
	if stats.ForKind(KindCommand).Removed != 1 {
		t.Errorf("expected 1 removed Command, got %d", stats.ForKind(KindCommand).Removed)
	}

	// Check registry state
	projects := reg.ListProjects()
	if len(projects) == 0 {
		t.Fatalf("expected at least 1 project")
	}
	scoped := reg.Project(projects[0].Name)

	if _, ok := scoped.ResolveResource("test-skill"); !ok {
		t.Errorf("expected test-skill to be resolvable after reload")
	}
	if _, ok := scoped.ResolveResource("build"); ok {
		t.Errorf("expected deleted build command not to be resolvable")
	}
	resAgent, ok := scoped.ResolveResource("dev-agent")
	if !ok {
		t.Fatalf("expected dev-agent to be resolvable")
	}
	ag, ok := resAgent.(*Agent)
	if !ok || ag.Spec.Instructions != "Updated instructions." {
		t.Errorf("expected updated instructions, got %v", ag)
	}

	// 3. ScopedRegistry Reload
	scopedStats, err := scoped.Reload()
	if err != nil {
		t.Fatalf("ScopedRegistry.Reload failed: %v", err)
	}
	if scopedStats.HasChanges() {
		t.Errorf("expected no changes on consecutive scoped reload, got %+v", scopedStats)
	}
}

func TestRegistry_Providers(t *testing.T) {
	reg := NewRegistry(nil)
	if len(reg.Providers()) != 0 {
		t.Errorf("expected 0 providers, got %d", len(reg.Providers()))
	}

	p := &FSResourceProvider{}
	reg.SetProviders(p)
	providers := reg.Providers()
	if len(providers) != 1 || providers[0] != p {
		t.Errorf("unexpected providers: %v", providers)
	}
}

func TestSkillValidation(t *testing.T) {
	// Missing tool in skill
	badSkill := &Skill{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindSkill,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-skill"},
		},
		Spec: SkillSpec{
			Tools: []string{"missing-tool"},
		},
	}
	reg := NewRegistry(nil)
	reg.Set(badSkill.QualifiedName(), badSkill)
	if err := reg.Validate(); err == nil || !strings.Contains(err.Error(), "references missing tool: missing-tool") {
		t.Errorf("expected missing tool error, got %v", err)
	}

	// Valid tool in skill
	tool := &Tool{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindTool,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "valid-tool"},
		},
	}
	goodSkill := &Skill{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindSkill,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "good-skill"},
		},
		Spec: SkillSpec{
			Tools: []string{"valid-tool"},
		},
	}
	reg2 := NewRegistry(nil)
	reg2.Set(tool.QualifiedName(), tool)
	reg2.Set(goodSkill.QualifiedName(), goodSkill)
	if err := reg2.Validate(); err != nil {
		t.Errorf("expected valid skill validation, got error: %v", err)
	}
}

func TestToolsForSkill(t *testing.T) {
	reg := NewRegistry(nil)

	toolA := &Tool{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindTool,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "tool-a", Description: "Tool A"},
		},
	}
	toolB := &Tool{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindTool,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "tool-b", Description: "Tool B"},
		},
	}
	skill := &Skill{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindSkill,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "my-skill"},
		},
		Spec: SkillSpec{
			Tools: []string{"tool-a", "tool-b"},
		},
	}
	globSkill := &Skill{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindSkill,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "glob-skill"},
		},
		Spec: SkillSpec{
			Tools: []string{"tool-*"},
		},
	}

	reg.Set(toolA.QualifiedName(), toolA)
	reg.Set(toolB.QualifiedName(), toolB)
	reg.Set(skill.QualifiedName(), skill)
	reg.Set(globSkill.QualifiedName(), globSkill)

	tools, err := reg.ToolsForSkill("my-skill")
	if err != nil {
		t.Fatalf("ToolsForSkill failed: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Metadata.Name != "tool-a" || tools[1].Metadata.Name != "tool-b" {
		t.Errorf("expected deterministic sorted tools [tool-a, tool-b], got [%s, %s]", tools[0].Metadata.Name, tools[1].Metadata.Name)
	}

	// Glob skill test
	globTools, err := reg.ToolsForSkill("glob-skill")
	if err != nil {
		t.Fatalf("glob ToolsForSkill failed: %v", err)
	}
	if len(globTools) != 2 {
		t.Fatalf("expected 2 tools from glob, got %d", len(globTools))
	}

	// Scoped registry test
	proj := &Project{Name: "test-proj", Path: "test", RootPath: "/tmp"}
	reg.AddProject(proj)
	scoped := reg.Project("test-proj")
	scopedTools, err := scoped.ToolsForSkill("my-skill")
	if err != nil {
		t.Fatalf("scoped ToolsForSkill failed: %v", err)
	}
	if len(scopedTools) != 2 {
		t.Fatalf("expected 2 scoped tools, got %d", len(scopedTools))
	}
}

func TestAgent_Thinking_ValidationAndMerge(t *testing.T) {
	reg := NewRegistry(nil)

	// Valid agent with effort strategy
	validAgent := agentWithSpec(NamespaceWorkspace, "agent-effort", AgentSpec{
		Thinking: &AgentThinking{
			Type:   ThinkingStrategyEffort,
			Effort: "medium",
		},
	})
	reg.Set(validAgent.QualifiedName(), validAgent)
	if err := reg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	// Invalid strategy
	badStrategyAgent := agentWithSpec(NamespaceWorkspace, "agent-bad-strat", AgentSpec{
		Thinking: &AgentThinking{
			Type: "invalid-strat",
		},
	})
	regBad1 := NewRegistry(nil)
	regBad1.Set(badStrategyAgent.QualifiedName(), badStrategyAgent)
	if err := regBad1.Validate(); err == nil || !strings.Contains(err.Error(), "invalid thinking type") {
		t.Errorf("expected invalid thinking type error, got: %v", err)
	}

	// Invalid negative budget tokens
	negBudgetAgent := agentWithSpec(NamespaceWorkspace, "agent-neg-budget", AgentSpec{
		Thinking: &AgentThinking{
			Type:         ThinkingStrategyBudget,
			BudgetTokens: -50,
		},
	})
	regBad2 := NewRegistry(nil)
	regBad2.Set(negBudgetAgent.QualifiedName(), negBudgetAgent)
	if err := regBad2.Validate(); err == nil || !strings.Contains(err.Error(), "budgetTokens cannot be negative") {
		t.Errorf("expected budgetTokens cannot be negative error, got: %v", err)
	}
}

func TestModelProviderAndAgent_ThinkingCapabilityValidation(t *testing.T) {
	trueVal := true

	// 1. ModelProvider with invalid thinking strategy
	badProvider := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-prov"},
		},
		Spec: ModelProviderSpec{
			Type: "openai",
			Models: []ProviderModel{
				{
					ID:    "m1",
					Name:  "m1",
					Label: "M1",
					Capabilities: &ProviderModelCapabilities{
						Completion: &trueVal,
						Thinking: []ProviderModelThinkingCapability{
							{Type: ThinkingStrategy("super-thinking")},
						},
					},
				},
			},
		},
	}
	regBadProv := NewRegistry(nil)
	regBadProv.Set(badProvider.QualifiedName(), badProvider)
	if err := regBadProv.Validate(); err == nil || !strings.Contains(err.Error(), "has invalid thinking strategy") {
		t.Errorf("expected invalid thinking strategy error, got %v", err)
	}

	// Provider with valid models and thinking capabilities
	goodProvider := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "ai"},
		},
		Spec: ModelProviderSpec{
			Type: "anthropic",
			Models: []ProviderModel{
				{
					ID:    "claude-3-7-sonnet-20250219",
					Name:  "claude-3-7-sonnet",
					Label: "Claude 3.7 Sonnet",
					Capabilities: &ProviderModelCapabilities{
						Completion: &trueVal,
						Thinking: []ProviderModelThinkingCapability{
							{
								Type:            ThinkingStrategyBudget,
								MaxBudgetTokens: 4096,
							},
						},
					},
				},
				{
					ID:    "o3-mini",
					Name:  "o3-mini",
					Label: "O3 Mini",
					Capabilities: &ProviderModelCapabilities{
						Completion: &trueVal,
						Thinking: []ProviderModelThinkingCapability{
							{
								Type:           ThinkingStrategyEffort,
								AllowedEfforts: []string{"low", "medium"},
							},
						},
					},
				},
			},
		},
	}

	// 2. Unsupported strategy
	regUnsupported := NewRegistry(nil)
	regUnsupported.Set(goodProvider.QualifiedName(), goodProvider)
	agentUnsupported := agentWithSpec(NamespaceWorkspace, "ag-unsupported", AgentSpec{
		Models: []string{"ai/claude-3-7-sonnet"},
		Thinking: &AgentThinking{
			Type:   ThinkingStrategyEffort,
			Effort: "low",
		},
	})
	regUnsupported.Set(agentUnsupported.QualifiedName(), agentUnsupported)
	if err := regUnsupported.Validate(); err == nil || !strings.Contains(err.Error(), "does not support thinking strategy") {
		t.Errorf("expected does not support thinking strategy error, got %v", err)
	}

	// 3. Disallowed effort
	regDisallowedEffort := NewRegistry(nil)
	regDisallowedEffort.Set(goodProvider.QualifiedName(), goodProvider)
	agentDisallowedEffort := agentWithSpec(NamespaceWorkspace, "ag-effort", AgentSpec{
		Models: []string{"ai/o3-mini"},
		Thinking: &AgentThinking{
			Type:   ThinkingStrategyEffort,
			Effort: "high",
		},
	})
	regDisallowedEffort.Set(agentDisallowedEffort.QualifiedName(), agentDisallowedEffort)
	if err := regDisallowedEffort.Validate(); err == nil || !strings.Contains(err.Error(), "does not allow thinking effort") {
		t.Errorf("expected does not allow thinking effort error, got %v", err)
	}

	// 4. Exceeding maxBudgetTokens
	regExceedBudget := NewRegistry(nil)
	regExceedBudget.Set(goodProvider.QualifiedName(), goodProvider)
	agentExceedBudget := agentWithSpec(NamespaceWorkspace, "ag-budget", AgentSpec{
		Models: []string{"ai/claude-3-7-sonnet"},
		Thinking: &AgentThinking{
			Type:         ThinkingStrategyBudget,
			BudgetTokens: 8192,
		},
	})
	regExceedBudget.Set(agentExceedBudget.QualifiedName(), agentExceedBudget)
	if err := regExceedBudget.Validate(); err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected budgetTokens exceeds maximum error, got %v", err)
	}

	// 5. Valid Agent thinking configurations
	regValid := NewRegistry(nil)
	regValid.Set(goodProvider.QualifiedName(), goodProvider)
	agentValid1 := agentWithSpec(NamespaceWorkspace, "ag-valid-1", AgentSpec{
		Models: []string{"ai/claude-3-7-sonnet"},
		Thinking: &AgentThinking{
			Type:         ThinkingStrategyBudget,
			BudgetTokens: 2048,
		},
	})
	agentValid2 := agentWithSpec(NamespaceWorkspace, "ag-valid-2", AgentSpec{
		Models: []string{"ai/o3-mini"},
		Thinking: &AgentThinking{
			Type:   ThinkingStrategyEffort,
			Effort: "medium",
		},
	})
	regValid.Set(agentValid1.QualifiedName(), agentValid1)
	regValid.Set(agentValid2.QualifiedName(), agentValid2)
	if err := regValid.Validate(); err != nil {
		t.Errorf("expected valid agent thinking to pass validation, got %v", err)
	}
}

func TestToolPolicies_ReadOnly(t *testing.T) {
	readOnlyTool := &Tool{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindTool,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "read-tool"},
		},
		Spec: ToolSpec{
			Annotations: &ToolAnnotation{IsReadOnly: true},
		},
	}
	writeTool := &Tool{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindTool,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "write-tool"},
		},
		Spec: ToolSpec{
			Annotations: &ToolAnnotation{IsReadOnly: false},
		},
	}

	pol := &ToolPolicies{
		ReadOnly: true,
	}

	if act := pol.EvaluateTool(readOnlyTool); act != ToolPolicyAllow {
		t.Errorf("expected allow for readOnly tool, got %v", act)
	}
	if act := pol.EvaluateTool(writeTool); act != ToolPolicyDeny {
		t.Errorf("expected deny for non-readOnly tool under ReadOnly policy, got %v", act)
	}

	// Merge test
	parentPol := &Policies{
		Tools: &ToolPolicies{ReadOnly: false},
	}
	childPol := &Policies{
		Tools: &ToolPolicies{ReadOnly: true},
	}
	merged := mergePolicies(parentPol, childPol)
	if !merged.Tools.ReadOnly {
		t.Error("expected merged policy to have ReadOnly=true from child")
	}
}

func TestAgent_Rules_AlwaysAndAmbient(t *testing.T) {
	reg := NewRegistry(nil)

	// Ambient rule (always on)
	ambientRule := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "ambient-rule"},
		},
		Spec: RuleSpec{
			Triggers: RuleTriggers{Always: true},
		},
	}
	// Path-specific rule (not ambient)
	pathRule := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "path-rule"},
		},
		Spec: RuleSpec{
			Triggers: RuleTriggers{Always: false, Globs: []string{"*.go"}},
		},
	}
	// Explicit rule attached to agent
	explicitRule := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "explicit-rule"},
		},
		Spec: RuleSpec{
			Triggers: RuleTriggers{Always: false, Globs: []string{"*.md"}},
		},
	}

	agent := agentWithSpec(NamespaceWorkspace, "test-agent", AgentSpec{
		Rules: []string{"explicit-rule"},
	})

	reg.Set(ambientRule.QualifiedName(), ambientRule)
	reg.Set(pathRule.QualifiedName(), pathRule)
	reg.Set(explicitRule.QualifiedName(), explicitRule)
	reg.Set(agent.QualifiedName(), agent)

	if err := reg.Validate(); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	resolved, err := reg.ResolveAgent("workspace/Agent/test-agent")
	if err != nil {
		t.Fatalf("ResolveAgent failed: %v", err)
	}

	// Should contain ambientRule (ambient) and explicitRule (with Always forced to true),
	// but NOT pathRule.
	if len(resolved.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d: %v", len(resolved.Rules), resolved.Rules)
	}

	var foundAmbient, foundExplicit bool
	for _, r := range resolved.Rules {
		if r.Metadata.Name == "ambient-rule" {
			foundAmbient = true
			if !r.Spec.Triggers.Always {
				t.Errorf("expected ambient-rule to have Always=true")
			}
		}
		if r.Metadata.Name == "explicit-rule" {
			foundExplicit = true
			if !r.Spec.Triggers.Always {
				t.Errorf("expected explicit-rule attached to agent to have Always forced to true")
			}
		}
	}
	if !foundAmbient || !foundExplicit {
		t.Errorf("expected both ambient and explicit rules, got ambient=%v explicit=%v", foundAmbient, foundExplicit)
	}
}

func TestAgent_ExplicitDefaults(t *testing.T) {
	reg := NewRegistry(nil)

	skill := makeSkillResource(NamespaceWorkspace, "sk1")
	cmd := makeCommandResource(NamespaceWorkspace, "cmd1")
	subAg := agentWithSpec(NamespaceWorkspace, "sub1", AgentSpec{})

	reg.Set(skill.QualifiedName(), skill)
	reg.Set(cmd.QualifiedName(), cmd)
	reg.Set(subAg.QualifiedName(), subAg)

	// Agent with default (empty) skills/commands/subagents
	agentDefault := agentWithSpec(NamespaceWorkspace, "ag-default", AgentSpec{})
	reg.Set(agentDefault.QualifiedName(), agentDefault)

	// Agent with wildcard
	agentWildcard := agentWithSpec(NamespaceWorkspace, "ag-wildcard", AgentSpec{
		Skills:    []string{"*"},
		Commands:  []string{"*"},
		Subagents: []string{"*"},
	})
	reg.Set(agentWildcard.QualifiedName(), agentWildcard)

	if err := reg.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	resDef, err := reg.ResolveAgent("workspace/Agent/ag-default")
	if err != nil {
		t.Fatalf("ResolveAgent(ag-default) failed: %v", err)
	}
	if len(resDef.Skills) != 0 || len(resDef.Commands) != 0 || len(resDef.Subagents) != 0 {
		t.Errorf("expected 0 skills, commands, subagents for default agent, got skills=%d commands=%d subagents=%d",
			len(resDef.Skills), len(resDef.Commands), len(resDef.Subagents))
	}

	resWild, err := reg.ResolveAgent("workspace/Agent/ag-wildcard")
	if err != nil {
		t.Fatalf("ResolveAgent(ag-wildcard) failed: %v", err)
	}
	if len(resWild.Skills) != 1 || len(resWild.Commands) != 1 || len(resWild.Subagents) != 3 { // ag-default, ag-wildcard, sub1
		t.Errorf("expected wildcard to resolve all resources, got skills=%d commands=%d subagents=%d",
			len(resWild.Skills), len(resWild.Commands), len(resWild.Subagents))
	}
}

func TestResource_GetPath_GetDir(t *testing.T) {
	// 1. Explicit path
	skillWithExplicitPath := &Skill{
		BaseResource: BaseResource{
			Directory: "/custom/dir",
			Path:      "/custom/dir/my-skill-file.md",
			Metadata:  Metadata{Name: "my-skill"},
		},
	}
	if skillWithExplicitPath.GetDir() != "/custom/dir" {
		t.Errorf("expected dir /custom/dir, got %q", skillWithExplicitPath.GetDir())
	}
	if skillWithExplicitPath.GetPath() != "/custom/dir/my-skill-file.md" {
		t.Errorf("expected path /custom/dir/my-skill-file.md, got %q", skillWithExplicitPath.GetPath())
	}

	// 2. Inferred path for Skill/Agent
	skillInferred := &Skill{
		BaseResource: BaseResource{
			Directory: "/custom/dir",
			Metadata:  Metadata{Name: "inferred-skill"},
		},
	}
	expectedInferred := filepath.Join("/custom/dir", "inferred-skill.md")
	if skillInferred.GetPath() != expectedInferred {
		t.Errorf("expected inferred path %q, got %q", expectedInferred, skillInferred.GetPath())
	}

	// 3. Inferred path for Context
	ctx := &Context{
		BaseResource: BaseResource{
			Directory: "/workspace/project",
		},
	}
	expectedCtxPath := filepath.Join("/workspace/project", ContextFileName)
	if ctx.GetPath() != expectedCtxPath {
		t.Errorf("expected context path %q, got %q", expectedCtxPath, ctx.GetPath())
	}

	// 4. Inferred path for WorkspaceDef
	wsDef := &WorkspaceDef{
		BaseResource: BaseResource{
			Directory: "/workspace",
		},
	}
	expectedWsPath := filepath.Join("/workspace", WorkspaceFileName)
	if wsDef.GetPath() != expectedWsPath {
		t.Errorf("expected workspace path %q, got %q", expectedWsPath, wsDef.GetPath())
	}
}

func TestResolvedAgent_ToolsForSkill(t *testing.T) {
	reg := NewRegistry(nil)

	// Tools
	readFileTool := &Tool{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindTool,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "read-file"},
		},
		Spec: ToolSpec{
			Annotations: &ToolAnnotation{IsReadOnly: true},
		},
	}
	sqlReadTool := &Tool{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindTool,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "sql-read"},
		},
		Spec: ToolSpec{
			Annotations: &ToolAnnotation{IsReadOnly: true},
		},
	}
	sqlWriteTool := &Tool{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindTool,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "sql-write"},
		},
		Spec: ToolSpec{
			Annotations: &ToolAnnotation{IsReadOnly: false},
		},
	}
	rmTool := &Tool{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindTool,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "rm"},
		},
		Spec: ToolSpec{
			Annotations: &ToolAnnotation{IsDangerous: true},
		},
	}

	// Skills
	financeSkill := &Skill{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindSkill,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "finance"},
		},
		Spec: SkillSpec{
			Tools: []string{"sql-read", "sql-write", "rm"},
		},
	}
	unattachedSkill := &Skill{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindSkill,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "unattached"},
		},
		Spec: SkillSpec{
			Tools: []string{"sql-read"},
		},
	}

	// Agent: Include: ["read-file"], Exclude: ["rm"], ReadOnly: true
	agent := agentWithSpec(NamespaceWorkspace, "finance-bot", AgentSpec{
		Skills: []string{"finance"},
		Policies: &Policies{
			Tools: &ToolPolicies{
				Include:  []string{"read-file"},
				Exclude:  []string{"rm"},
				ReadOnly: true,
			},
		},
	})

	reg.Set(readFileTool.QualifiedName(), readFileTool)
	reg.Set(sqlReadTool.QualifiedName(), sqlReadTool)
	reg.Set(sqlWriteTool.QualifiedName(), sqlWriteTool)
	reg.Set(rmTool.QualifiedName(), rmTool)
	reg.Set(financeSkill.QualifiedName(), financeSkill)
	reg.Set(unattachedSkill.QualifiedName(), unattachedSkill)
	reg.Set(agent.QualifiedName(), agent)

	if err := reg.Validate(); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	resolvedAgent, err := reg.ResolveAgent("workspace/Agent/finance-bot")
	if err != nil {
		t.Fatalf("ResolveAgent failed: %v", err)
	}

	// 1. Error when requesting tools for a skill not attached to agent
	if _, err := resolvedAgent.ToolsForSkill(reg, "unattached"); err == nil {
		t.Error("expected error when requesting tools for skill not enabled on agent, got nil")
	}

	// 2. Request tools for attached skill "finance"
	skillTools, err := resolvedAgent.ToolsForSkill(reg, "finance")
	if err != nil {
		t.Fatalf("ToolsForSkill(finance) failed: %v", err)
	}

	// Should contain sql-read only:
	// - sql-read is allowed even though not in agent's base include list ["read-file"]
	// - sql-write is denied because agent policy has ReadOnly: true
	// - rm is denied because agent policy has Exclude: ["rm"]
	if len(skillTools) != 1 {
		t.Fatalf("expected exactly 1 tool for skill (sql-read), got %d tools: %v", len(skillTools), skillTools)
	}
	if skillTools[0].Metadata.Name != "sql-read" {
		t.Errorf("expected sql-read tool, got %s", skillTools[0].Metadata.Name)
	}
}

func TestModelAliases_Resolution(t *testing.T) {
	trueVal := true
	falseVal := false

	anthropic := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "anthropic"},
		},
		Spec: ModelProviderSpec{
			DefaultModels: &ModelDefaults{
				Completion: "claude-3-5-sonnet",
			},
			Models: []ProviderModel{
				{
					ID:           "claude-3-5-haiku",
					Name:         "haiku",
					Aliases:      []string{"fast"},
					Capabilities: &ProviderModelCapabilities{Completion: &trueVal, Decision: &falseVal},
				},
				{
					ID:           "claude-3-5-sonnet",
					Name:         "sonnet",
					Capabilities: &ProviderModelCapabilities{Completion: &trueVal, Decision: &trueVal},
				},
			},
		},
	}

	openai := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "openai"},
		},
		Spec: ModelProviderSpec{
			DefaultModels: &ModelDefaults{
				Completion: "gpt-4o",
			},
			Models: []ProviderModel{
				{
					ID:           "gpt-4o-mini",
					Name:         "mini",
					Aliases:      []string{"@fast"}, // test @ prefix normalization in Aliases
					Capabilities: &ProviderModelCapabilities{Completion: &trueVal, Decision: &falseVal},
				},
				{
					ID:           "gpt-4o",
					Name:         "gpt-4o",
					Capabilities: &ProviderModelCapabilities{Completion: &trueVal, Decision: &trueVal},
				},
			},
		},
	}

	wsDef := &WorkspaceDef{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindWorkspace,
			Directory:  "/workspace",
			Metadata:   Metadata{Name: "my-ws"},
		},
		Spec: WorkspaceDefSpec{
			DefaultProvider: "anthropic",
		},
	}

	reg := NewRegistry(nil)
	reg.workspace = &Workspace{Def: wsDef, RootPath: "/workspace"}
	reg.Set(anthropic.QualifiedName(), anthropic)
	reg.Set(openai.QualifiedName(), openai)

	// 1. Unqualified @fast: returns anthropic (defaultProvider) first, then openai
	fastModels := reg.ResolveModels(ModelRoleCompletion, "@fast")
	if len(fastModels) != 2 {
		t.Fatalf("expected 2 models for @fast, got %d: %v", len(fastModels), fastModels)
	}
	if fastModels[0].Provider.Metadata.Name != "anthropic" || fastModels[0].ModelID != "claude-3-5-haiku" {
		t.Errorf("expected anthropic/claude-3-5-haiku first, got %s/%s", fastModels[0].Provider.Metadata.Name, fastModels[0].ModelID)
	}
	if fastModels[1].Provider.Metadata.Name != "openai" || fastModels[1].ModelID != "gpt-4o-mini" {
		t.Errorf("expected openai/gpt-4o-mini second, got %s/%s", fastModels[1].Provider.Metadata.Name, fastModels[1].ModelID)
	}

	singleFast, ok := reg.ResolveModel(ModelRoleCompletion, "@fast")
	if !ok || singleFast.Provider.Metadata.Name != "anthropic" || singleFast.ModelID != "claude-3-5-haiku" {
		t.Errorf("expected ResolveModel(@fast) to return anthropic/claude-3-5-haiku, got ok=%v %v", ok, singleFast)
	}

	// 2. Provider-scoped aliases: anthropic/@fast and openai/@fast
	anthropicFast, ok := reg.ResolveModel(ModelRoleCompletion, "anthropic/@fast")
	if !ok || anthropicFast.Provider.Metadata.Name != "anthropic" || anthropicFast.ModelID != "claude-3-5-haiku" {
		t.Errorf("expected anthropic/@fast to resolve, got ok=%v %v", ok, anthropicFast)
	}
	openaiFast, ok := reg.ResolveModel(ModelRoleCompletion, "openai/@fast")
	if !ok || openaiFast.Provider.Metadata.Name != "openai" || openaiFast.ModelID != "gpt-4o-mini" {
		t.Errorf("expected openai/@fast to resolve, got ok=%v %v", ok, openaiFast)
	}

	// 3. Workspace modelAliases override: @fast -> openai/gpt-4o-mini
	wsDef.Spec.ModelAliases = map[string]string{
		"fast": "openai/@fast",
	}
	overriddenFast, ok := reg.ResolveModel(ModelRoleCompletion, "@fast")
	if !ok || overriddenFast.Provider.Metadata.Name != "openai" || overriddenFast.ModelID != "gpt-4o-mini" {
		t.Errorf("expected workspace alias override to openai/@fast, got ok=%v %v", ok, overriddenFast)
	}
	// Reset alias
	wsDef.Spec.ModelAliases = nil

	// 4. Built-in magic alias @default and <provider>/@default
	// Fallback list: ["@fast", "@default"]
	fallbackList := reg.ResolveModels(ModelRoleCompletion, "@fast", "@default")
	if len(fallbackList) < 3 {
		t.Fatalf("expected at least 3 models in fallback list [@fast, @default], got %d", len(fallbackList))
	}
	if fallbackList[0].ModelID != "claude-3-5-haiku" {
		t.Errorf("expected @fast first, got %s", fallbackList[0].ModelID)
	}

	// Provider-scoped @default: ["openai/@default", "anthropic/@default"]
	provDefaults := reg.ResolveModels(ModelRoleCompletion, "openai/@default", "anthropic/@default")
	if len(provDefaults) != 2 {
		t.Fatalf("expected 2 provider defaults, got %d", len(provDefaults))
	}
	if provDefaults[0].Provider.Metadata.Name != "openai" || provDefaults[0].ModelID != "gpt-4o" {
		t.Errorf("expected openai/gpt-4o first, got %s/%s", provDefaults[0].Provider.Metadata.Name, provDefaults[0].ModelID)
	}
	if provDefaults[1].Provider.Metadata.Name != "anthropic" || provDefaults[1].ModelID != "claude-3-5-sonnet" {
		t.Errorf("expected anthropic/claude-3-5-sonnet second, got %s/%s", provDefaults[1].Provider.Metadata.Name, provDefaults[1].ModelID)
	}
}

func TestModelAliases_Validation(t *testing.T) {
	trueVal := true
	falseVal := false

	prov := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "anthropic"},
		},
		Spec: ModelProviderSpec{
			Models: []ProviderModel{
				{
					ID:           "claude-3-5-haiku",
					Aliases:      []string{"fast"},
					Capabilities: &ProviderModelCapabilities{Completion: &trueVal, Decision: &falseVal},
				},
				{
					ID:           "claude-3-5-sonnet",
					Capabilities: &ProviderModelCapabilities{Completion: &trueVal, Decision: &trueVal},
				},
			},
		},
	}

	// 1. Unknown bare @alias
	reg1 := NewRegistry(nil)
	reg1.Set(prov.QualifiedName(), prov)
	badAgent1 := agentWithSpec(NamespaceWorkspace, "bad-agent-1", AgentSpec{
		Models: []string{"@non-existent"},
	})
	reg1.Set(badAgent1.QualifiedName(), badAgent1)
	if err := reg1.Validate(); err == nil || !strings.Contains(err.Error(), "unknown model alias \"@non-existent\"") {
		t.Errorf("expected unknown model alias error, got %v", err)
	}

	// 2. Unknown provider/@alias: missing provider
	reg2 := NewRegistry(nil)
	reg2.Set(prov.QualifiedName(), prov)
	badAgent2 := agentWithSpec(NamespaceWorkspace, "bad-agent-2", AgentSpec{
		Models: []string{"missing-prov/@fast"},
	})
	reg2.Set(badAgent2.QualifiedName(), badAgent2)
	if err := reg2.Validate(); err == nil || !strings.Contains(err.Error(), "references missing provider: missing-prov") {
		t.Errorf("expected missing provider error, got %v", err)
	}

	// 2b. Unknown provider/@alias: provider exists, but missing alias
	reg2b := NewRegistry(nil)
	reg2b.Set(prov.QualifiedName(), prov)
	badAgent2b := agentWithSpec(NamespaceWorkspace, "bad-agent-2b", AgentSpec{
		Models: []string{"anthropic/@slow"},
	})
	reg2b.Set(badAgent2b.QualifiedName(), badAgent2b)
	if err := reg2b.Validate(); err == nil || !strings.Contains(err.Error(), "has no model with alias \"slow\"") {
		t.Errorf("expected provider has no model with alias error, got %v", err)
	}

	// 3. Duplicate alias within a provider
	reg3 := NewRegistry(nil)
	dupProv := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "dup-prov"},
		},
		Spec: ModelProviderSpec{
			Models: []ProviderModel{
				{ID: "m1", Aliases: []string{"fast"}},
				{ID: "m2", Aliases: []string{"@fast"}},
			},
		},
	}
	reg3.Set(dupProv.QualifiedName(), dupProv)
	if err := reg3.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate alias") {
		t.Errorf("expected duplicate alias error, got %v", err)
	}

	// 4. Alias collides with another model's ID or Name in the same provider
	reg4 := NewRegistry(nil)
	collideProv := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "collide-prov"},
		},
		Spec: ModelProviderSpec{
			Models: []ProviderModel{
				{ID: "gpt-4"},
				{ID: "m2", Aliases: []string{"gpt-4"}},
			},
		},
	}
	reg4.Set(collideProv.QualifiedName(), collideProv)
	if err := reg4.Validate(); err == nil || !strings.Contains(err.Error(), "collides with model \"gpt-4\"") {
		t.Errorf("expected alias collision error, got %v", err)
	}

	// 5. Capability mismatch on an @alias
	reg5 := NewRegistry(nil)
	reg5.Set(prov.QualifiedName(), prov)
	badDec := &Decision{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindDecision,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "test-dec"},
		},
		Spec: DecisionSpec{
			Models: []string{"@fast"}, // @fast is haiku which has Decision: false
			Questions: []DecisionQuestion{
				{Name: "q1", Type: DecisionQuestionPredicate},
			},
		},
	}
	reg5.Set(badDec.QualifiedName(), badDec)
	if err := reg5.Validate(); err == nil || !strings.Contains(err.Error(), "does not support decision capability") {
		t.Errorf("expected capability mismatch error on @alias, got %v", err)
	}

	// 6. Empty alias key or target in workspace modelAliases
	reg6 := NewRegistry(nil)
	wsBadAlias := &WorkspaceDef{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindWorkspace,
			Directory:  "/workspace",
			Metadata:   Metadata{Name: "ws"},
		},
		Spec: WorkspaceDefSpec{
			ModelAliases: map[string]string{
				"@": "target",
			},
		},
	}
	reg6.workspace = &Workspace{Def: wsBadAlias}
	if err := reg6.Validate(); err == nil || !strings.Contains(err.Error(), "alias key cannot be empty") {
		t.Errorf("expected alias key cannot be empty error, got %v", err)
	}

	wsBadTarget := &WorkspaceDef{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindWorkspace,
			Directory:  "/workspace",
			Metadata:   Metadata{Name: "ws"},
		},
		Spec: WorkspaceDefSpec{
			ModelAliases: map[string]string{
				"fast": "  ",
			},
		},
	}
	reg6.workspace = &Workspace{Def: wsBadTarget}
	if err := reg6.Validate(); err == nil || !strings.Contains(err.Error(), "target model cannot be empty") {
		t.Errorf("expected target model cannot be empty error, got %v", err)
	}
}

func TestCommandValidation(t *testing.T) {
	trueVal := true
	falseVal := false

	prov := &ModelProvider{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindModelProvider,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "openai"},
		},
		Spec: ModelProviderSpec{
			Models: []ProviderModel{
				{
					ID:           "gpt-4o",
					Aliases:      []string{"fast"},
					Capabilities: &ProviderModelCapabilities{Completion: &trueVal},
				},
				{
					ID:           "text-embedding-3",
					Capabilities: &ProviderModelCapabilities{Completion: &falseVal},
				},
			},
		},
	}

	tool := &Tool{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindTool,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "read-file"},
		},
	}

	skill := &Skill{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindSkill,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "doc-gen"},
		},
	}

	rule := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "lint-rule"},
		},
	}

	agent := agentWithSpec(NamespaceWorkspace, "writer", AgentSpec{})

	// 1. Valid command passes
	reg := NewRegistry(nil)
	reg.Set(prov.QualifiedName(), prov)
	reg.Set(tool.QualifiedName(), tool)
	reg.Set(skill.QualifiedName(), skill)
	reg.Set(rule.QualifiedName(), rule)
	reg.Set(agent.QualifiedName(), agent)

	validCmd := &Command{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindCommand,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "valid-cmd"},
		},
		Spec: CommandSpec{
			Agent:   "writer",
			Context: CommandContextFork,
			Models:  []string{"@fast"},
			Skills:  []string{"doc-gen", "other/*"},
			Rules:   []string{"lint-rule", "rules/*"},
			Tools:   []string{"read-file", "tools/*"},
		},
	}
	reg.Set(validCmd.QualifiedName(), validCmd)
	if err := reg.Validate(); err != nil {
		t.Fatalf("expected valid command to pass, got: %v", err)
	}

	// 2. Missing skill
	regMissingSkill := NewRegistry(nil)
	cmdBadSkill := &Command{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindCommand,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-skill-cmd"},
		},
		Spec: CommandSpec{
			Skills: []string{"missing-skill"},
		},
	}
	regMissingSkill.Set(cmdBadSkill.QualifiedName(), cmdBadSkill)
	if err := regMissingSkill.Validate(); err == nil || !strings.Contains(err.Error(), "references missing skill: missing-skill") {
		t.Errorf("expected missing skill error, got: %v", err)
	}

	// 3. Missing rule
	regMissingRule := NewRegistry(nil)
	cmdBadRule := &Command{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindCommand,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-rule-cmd"},
		},
		Spec: CommandSpec{
			Rules: []string{"missing-rule"},
		},
	}
	regMissingRule.Set(cmdBadRule.QualifiedName(), cmdBadRule)
	if err := regMissingRule.Validate(); err == nil || !strings.Contains(err.Error(), "references missing rule: missing-rule") {
		t.Errorf("expected missing rule error, got: %v", err)
	}

	// 4. Missing tool
	regMissingTool := NewRegistry(nil)
	cmdBadTool := &Command{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindCommand,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-tool-cmd"},
		},
		Spec: CommandSpec{
			Tools: []string{"missing-tool"},
		},
	}
	regMissingTool.Set(cmdBadTool.QualifiedName(), cmdBadTool)
	if err := regMissingTool.Validate(); err == nil || !strings.Contains(err.Error(), "references missing tool: missing-tool") {
		t.Errorf("expected missing tool error, got: %v", err)
	}

	// 5. Incompatible model (embedding model lacks completion)
	regBadModel := NewRegistry(nil)
	regBadModel.Set(prov.QualifiedName(), prov)
	cmdBadModel := &Command{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindCommand,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "bad-model-cmd"},
		},
		Spec: CommandSpec{
			Models: []string{"openai/text-embedding-3"},
		},
	}
	regBadModel.Set(cmdBadModel.QualifiedName(), cmdBadModel)
	if err := regBadModel.Validate(); err == nil || !strings.Contains(err.Error(), "does not support completion capability") {
		t.Errorf("expected model completion capability error, got: %v", err)
	}
}

func TestResolveCommand(t *testing.T) {
	reg := NewRegistry(nil)
	reg.AddProject(&Project{Name: "app", Path: ".", RootPath: "/repo"})

	// Setup tools
	toolCmd := makeToolResource(NamespaceWorkspace, "tool-cmd")
	toolCmd.Spec.Description = "Command tool"
	reg.Set(toolCmd.QualifiedName(), toolCmd)

	toolSkill := makeToolResource(NamespaceWorkspace, "tool-skill")
	toolSkill.Spec.Description = "Skill tool"
	reg.Set(toolSkill.QualifiedName(), toolSkill)

	toolDangerous := makeToolResource(NamespaceWorkspace, "tool-dangerous")
	toolDangerous.Spec.Annotations = &ToolAnnotation{IsDangerous: true}
	reg.Set(toolDangerous.QualifiedName(), toolDangerous)

	toolExcluded := makeToolResource(NamespaceWorkspace, "tool-excluded")
	reg.Set(toolExcluded.QualifiedName(), toolExcluded)

	// Setup skills
	skillDoc := &Skill{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindSkill,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "doc-skill"},
		},
		Spec: SkillSpec{
			Tools: []string{"tool-skill", "tool-dangerous"},
		},
	}
	reg.Set(skillDoc.QualifiedName(), skillDoc)

	// Setup rules
	ruleLow := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "rule-low"},
		},
		Spec: RuleSpec{
			Priority: 10,
			Triggers: RuleTriggers{Always: false},
		},
	}
	ruleHigh := &Rule{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindRule,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "rule-high"},
		},
		Spec: RuleSpec{
			Priority: 100,
			Triggers: RuleTriggers{Always: false},
		},
	}
	reg.Set(ruleLow.QualifiedName(), ruleLow)
	reg.Set(ruleHigh.QualifiedName(), ruleHigh)

	// Setup agent with policies:
	// - Include contains only unrelated tool (testing that Include is not enforced on command dynamic tools)
	// - Dangerous is denied
	// - Exclude contains "tool-excluded"
	agentWithPolicies := agentWithSpec(NamespaceWorkspace, "strict-agent", AgentSpec{
		Commands: []string{"agent-cmd"},
		Policies: &Policies{
			Tools: &ToolPolicies{
				Include:   []string{"unrelated-tool"},
				Exclude:   []string{"tool-excluded"},
				Dangerous: ToolPolicyDeny,
			},
		},
	})
	reg.Set(agentWithPolicies.QualifiedName(), agentWithPolicies)

	// Command with explicit agent
	cmdExplicitAgent := &Command{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindCommand,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "explicit-cmd"},
		},
		Spec: CommandSpec{
			Agent:  "strict-agent",
			Skills: []string{"doc-skill"},
			Rules:  []string{"rule-low", "rule-high"},
			Tools:  []string{"tool-cmd", "tool-excluded"},
		},
	}
	reg.Set(cmdExplicitAgent.QualifiedName(), cmdExplicitAgent)

	// Command meant for agent invocation without explicit agent
	cmdAgentInvoked := &Command{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindCommand,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "agent-cmd"},
		},
		Spec: CommandSpec{
			Skills: []string{"doc-skill"},
			Tools:  []string{"tool-cmd"},
		},
	}
	reg.Set(cmdAgentInvoked.QualifiedName(), cmdAgentInvoked)

	// Unenabled command
	cmdUnenabled := &Command{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindCommand,
			Namespace:  NamespaceWorkspace,
			Metadata:   Metadata{Name: "unenabled-cmd"},
		},
	}
	reg.Set(cmdUnenabled.QualifiedName(), cmdUnenabled)

	// 1. Resolve Command from registry
	resCmd, err := reg.ResolveCommand("explicit-cmd")
	if err != nil {
		t.Fatalf("ResolveCommand failed: %v", err)
	}
	if resCmd.Agent == nil || resCmd.Agent.Agent.Metadata.Name != "strict-agent" {
		t.Errorf("expected executing agent strict-agent, got %v", resCmd.Agent)
	}

	// 2. Check eager skills
	if len(resCmd.Skills) != 1 || resCmd.Skills[0].Metadata.Name != "doc-skill" {
		t.Errorf("expected [doc-skill] eager skills, got %v", resCmd.Skills)
	}

	// 3. Check eager rules (Always forced to true, sorted priority descending)
	if len(resCmd.Rules) != 2 {
		t.Fatalf("expected 2 eager rules, got %d", len(resCmd.Rules))
	}
	if resCmd.Rules[0].Metadata.Name != "rule-high" || resCmd.Rules[1].Metadata.Name != "rule-low" {
		t.Errorf("expected rules sorted priority descending [rule-high, rule-low], got [%s, %s]",
			resCmd.Rules[0].Metadata.Name, resCmd.Rules[1].Metadata.Name)
	}
	if !resCmd.Rules[0].Spec.Triggers.Always || !resCmd.Rules[1].Spec.Triggers.Always {
		t.Errorf("expected command eager rules to have Always=true, got %v, %v",
			resCmd.Rules[0].Spec.Triggers.Always, resCmd.Rules[1].Spec.Triggers.Always)
	}

	// 4. Check effective tools (tool-cmd + tool-skill; tool-dangerous denied, tool-excluded excluded)
	if len(resCmd.Tools) != 2 {
		var toolNames []string
		for _, tRef := range resCmd.Tools {
			toolNames = append(toolNames, tRef.Metadata.Name)
		}
		t.Fatalf("expected 2 effective tools [tool-cmd, tool-skill], got %d: %v", len(resCmd.Tools), toolNames)
	}
	if resCmd.Tools[0].Metadata.Name != "tool-cmd" || resCmd.Tools[1].Metadata.Name != "tool-skill" {
		t.Errorf("expected deterministic sorted tools [tool-cmd, tool-skill], got [%s, %s]",
			resCmd.Tools[0].Metadata.Name, resCmd.Tools[1].Metadata.Name)
	}

	// 5. Resolve via ResolvedAgent.ResolveCommand
	resolvedAgent, err := reg.ResolveAgent("strict-agent")
	if err != nil {
		t.Fatalf("ResolveAgent failed: %v", err)
	}

	agentResCmd, err := resolvedAgent.ResolveCommand(reg, "agent-cmd")
	if err != nil {
		t.Fatalf("resolvedAgent.ResolveCommand failed: %v", err)
	}
	if agentResCmd.Agent != resolvedAgent {
		t.Errorf("expected executing agent to be the invoking resolved agent")
	}

	// 6. Resolve unenabled command via ResolvedAgent -> fails
	_, err = resolvedAgent.ResolveCommand(reg, "unenabled-cmd")
	if err == nil || !strings.Contains(err.Error(), "command \"unenabled-cmd\" is not enabled for agent \"strict-agent\"") {
		t.Errorf("expected unenabled command error, got: %v", err)
	}

	// 7. ScopedRegistry test
	scoped := reg.Project("app")
	scopedResCmd, err := scoped.ResolveCommand("explicit-cmd")
	if err != nil {
		t.Fatalf("scoped.ResolveCommand failed: %v", err)
	}
	if scopedResCmd.Command.Metadata.Name != "explicit-cmd" {
		t.Errorf("expected explicit-cmd from scoped registry, got %s", scopedResCmd.Command.Metadata.Name)
	}
}
