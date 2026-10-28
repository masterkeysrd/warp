package warp

import (
	"strings"
	"testing"
)

func TestTemplateRender_Shorthand(t *testing.T) {
	cmd := &Command{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "test-cmd"},
		},
		Spec: CommandSpec{
			Instructions: "Hello $1, this is $hint1 and ${hint2}.",
			Arguments: []Argument{
				{Name: "hint1"},
				{Name: "hint2"},
			},
		},
	}

	opts := &RenderOptions{
		Args: []string{"Pos1", "HintVal2"},
	}

	res, err := Render(cmd, opts)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	expected := "Hello Pos1, this is Pos1 and HintVal2."
	if res != expected {
		t.Errorf("Expected %q, got %q", expected, res)
	}
}

func TestTemplateRender_Fields(t *testing.T) {
	agent := &Agent{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "my-agent", DisplayName: "My Agent"},
		},
		Spec: AgentSpec{
			Instructions: "I am $DisplayName. I know {{range .Skills}}{{.}} {{end}}",
			Skills:       &[]string{"go", "rust"},
		},
	}

	res, err := Render(agent, nil)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	expected := "I am My Agent. I know go rust "
	if res != expected {
		t.Errorf("Expected %q, got %q", expected, res)
	}
}

func TestTemplateRender_Globals(t *testing.T) {
	skill := &Skill{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "test"},
		},
		Spec: SkillSpec{
			Instructions: "Path is $PWD.",
		},
	}

	opts := &RenderOptions{
		Globals: map[string]any{"PWD": "/usr/src"},
	}

	res, err := Render(skill, opts)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	expected := "Path is /usr/src."
	if res != expected {
		t.Errorf("Expected %q, got %q", expected, res)
	}
}

func TestWorkspaceRender(t *testing.T) {
	ws := &Workspace{
		RootPath: "/Users/user/code/warp",
		Def: &WorkspaceDef{
			BaseResource: BaseResource{
				Directory: "/Users/user/code/warp",
			},
			Spec: WorkspaceDefSpec{
				Instructions: "Root: {{.Workspace.Dir}} | File: {{.Workspace.Path}} | Shorthand Root: $WorkspaceDir | Shorthand File: $WorkspacePath | CI: {{.CI_ENV}} | Shorthand CI: $CI_ENV",
			},
		},
	}

	opts := &WorkspaceRenderOptions{
		Globals: map[string]any{
			"CI_ENV": "true",
		},
	}

	res, err := ws.Render(opts)
	if err != nil {
		t.Fatalf("Workspace.Render failed: %v", err)
	}

	expected := "Root: /Users/user/code/warp | File: /Users/user/code/warp/WORKSPACE.md | Shorthand Root: /Users/user/code/warp | Shorthand File: /Users/user/code/warp/WORKSPACE.md | CI: true | Shorthand CI: true"
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}

	// Test nil safety
	var nilWs *Workspace
	resNil, err := nilWs.Render(nil)
	if err != nil {
		t.Errorf("nil Workspace should not error, got %v", err)
	}
	if resNil != "" {
		t.Errorf("nil Workspace should render empty string, got %q", resNil)
	}
}

func TestContextRender(t *testing.T) {
	ws := &Workspace{
		RootPath: "/Users/user/code/warp",
	}

	ctx := &Context{
		BaseResource: BaseResource{
			Directory: "/Users/user/code/warp/cmd/cli",
			Metadata: Metadata{
				Name:        "cli-tool",
				DisplayName: "Warp CLI",
			},
		},
		Spec: ContextSpec{
			Instructions: "Project Name: {{.Project.Name}} | Project DisplayName: {{.Project.DisplayName}} | Project Dir: {{.Project.Dir}} | Workspace Dir: {{.Workspace.Dir}} | Context Path: {{.Context.Path}} | Shorthand Project Dir: $ProjectDir | Shorthand Workspace Dir: $WorkspaceDir | Shorthand Context Path: $ContextPath | CI: {{.CI_ENV}}",
		},
	}

	proj := &Project{
		Name:     "cli",
		Path:     "cmd/cli",
		RootPath: "/Users/user/code/warp",
		Context:  ctx,
	}

	opts := &ContextRenderOptions{
		Workspace: ws,
		Project:   proj,
		Globals: map[string]any{
			"CI_ENV": "true",
		},
	}

	res, err := ctx.Render(opts)
	if err != nil {
		t.Fatalf("Context.Render failed: %v", err)
	}

	expected := "Project Name: cli | Project DisplayName: Warp CLI | Project Dir: /Users/user/code/warp/cmd/cli | Workspace Dir: /Users/user/code/warp | Context Path: /Users/user/code/warp/cmd/cli/AGENTS.md | Shorthand Project Dir: /Users/user/code/warp/cmd/cli | Shorthand Workspace Dir: /Users/user/code/warp | Shorthand Context Path: /Users/user/code/warp/cmd/cli/AGENTS.md | CI: true"
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}

	// Test nil safety with empty/nil options
	resNilOpts, err := ctx.Render(nil)
	if err != nil {
		t.Fatalf("Context.Render with nil opts failed: %v", err)
	}
	expectedNilOpts := "Project Name:  | Project DisplayName:  | Project Dir:  | Workspace Dir:  | Context Path: /Users/user/code/warp/cmd/cli/AGENTS.md | Shorthand Project Dir:  | Shorthand Workspace Dir:  | Shorthand Context Path: /Users/user/code/warp/cmd/cli/AGENTS.md | CI: <no value>"
	if resNilOpts != expectedNilOpts {
		t.Errorf("Expected:\n%q\nGot:\n%q", expectedNilOpts, resNilOpts)
	}

	// Test nil safety with nil Context pointer
	var nilCtx *Context
	resNilCtx, err := nilCtx.Render(nil)
	if err != nil {
		t.Errorf("nil Context should not error, got %v", err)
	}
	if resNilCtx != "" {
		t.Errorf("nil Context should render empty string, got %q", resNilCtx)
	}
}

func TestCommandRender(t *testing.T) {
	ws := &Workspace{
		RootPath: "/Users/user/code/warp",
	}

	proj := &Project{
		Name:     "cli",
		Path:     "cmd/cli",
		RootPath: "/Users/user/code/warp",
	}

	cmd := &Command{
		BaseResource: BaseResource{
			Directory: "/Users/user/code/warp/commands",
			Metadata: Metadata{
				Name:        "test-cmd",
				Description: "A test command",
			},
		},
		Spec: CommandSpec{
			Arguments: []Argument{
				{Name: "ticket"},
				{Name: "env"},
			},
			Instructions: "Command: {{.Command.Name}} | Dir: {{.Command.Dir}} | Project: {{.Project.Name}} | Args: $1, $2 | Arguments: $ticket, $env",
		},
	}

	opts := &CommandRenderOptions{
		Workspace: ws,
		Project:   proj,
		Args:      []string{"TICKET-123", "prod"},
	}

	res, err := cmd.Render(opts)
	if err != nil {
		t.Fatalf("Command.Render failed: %v", err)
	}

	expected := "Command: test-cmd | Dir: /Users/user/code/warp/commands | Project: cli | Args: TICKET-123, prod | Arguments: TICKET-123, prod"
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}

	// Test nil safety
	var nilCmd *Command
	resNil, err := nilCmd.Render(nil)
	if err != nil {
		t.Errorf("nil Command should not error, got %v", err)
	}
	if resNil != "" {
		t.Errorf("nil Command should render empty string, got %q", resNil)
	}
}

func TestSkillRender(t *testing.T) {
	ws := &Workspace{
		RootPath: "/Users/user/code/warp",
	}

	proj := &Project{
		Name:     "cli",
		Path:     "cmd/cli",
		RootPath: "/Users/user/code/warp",
	}

	invokerAgent := &Agent{
		BaseResource: BaseResource{
			Directory: "/Users/user/code/warp/.agents",
			Metadata: Metadata{
				Name:        "researcher",
				Description: "Code researcher",
			},
		},
	}

	skill := &Skill{
		BaseResource: BaseResource{
			Directory: "/Users/user/code/warp/skills",
			Metadata: Metadata{
				Name:        "go-expert",
				Description: "Go conventions",
			},
		},
		Spec: SkillSpec{
			Instructions: "Skill: {{.Skill.Name}} | Dir: {{.Skill.Dir}} | Project: {{.Project.Name}} | Invoker: {{.Agent.Name}} | InvokerDesc: {{.Agent.Description}}",
		},
	}

	opts := &SkillRenderOptions{
		Workspace: ws,
		Project:   proj,
		Agent:     invokerAgent,
	}

	res, err := skill.Render(opts)
	if err != nil {
		t.Fatalf("Skill.Render failed: %v", err)
	}

	expected := "Skill: go-expert | Dir: /Users/user/code/warp/skills | Project: cli | Invoker: researcher | InvokerDesc: Code researcher"
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}

	// Test nil safety
	var nilSkill *Skill
	resNil, err := nilSkill.Render(nil)
	if err != nil {
		t.Errorf("nil Skill should not error, got %v", err)
	}
	if resNil != "" {
		t.Errorf("nil Skill should render empty string, got %q", resNil)
	}
}

func TestSkillRender_Arguments(t *testing.T) {
	skill := &Skill{
		BaseResource: BaseResource{
			Metadata: Metadata{
				Name: "code-review",
			},
		},
		Spec: SkillSpec{
			ArgumentHint: "Provide the file path to review and optional focus",
			Arguments: []Argument{
				{
					Name:        "file",
					Description: "Path to file",
					Required:    true,
				},
				{
					Name:        "focus",
					Description: "Focus area",
					Required:    false,
					Default:     "all",
				},
			},
			Instructions: "Review $file with focus $focus (Hint: $ArgumentHint)",
		},
	}

	// 1. With both arguments supplied
	res, err := skill.Render(&SkillRenderOptions{
		Args: []string{"main.go", "security"},
	})
	if err != nil {
		t.Fatalf("Skill.Render failed: %v", err)
	}
	expected := "Review main.go with focus security (Hint: Provide the file path to review and optional focus)"
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}

	// 2. With only the required argument supplied (default applies for focus)
	resDefault, err := skill.Render(&SkillRenderOptions{
		Args: []string{"main.go"},
	})
	if err != nil {
		t.Fatalf("Skill.Render failed: %v", err)
	}
	expectedDefault := "Review main.go with focus all (Hint: Provide the file path to review and optional focus)"
	if resDefault != expectedDefault {
		t.Errorf("Expected:\n%q\nGot:\n%q", expectedDefault, resDefault)
	}
}

func TestSkillRender_RequiredArgumentMissing(t *testing.T) {
	skill := &Skill{
		BaseResource: BaseResource{
			Metadata: Metadata{
				Name: "code-review",
			},
		},
		Spec: SkillSpec{
			Arguments: []Argument{
				{
					Name:     "file",
					Required: true,
				},
			},
			Instructions: "Review $file",
		},
	}

	_, err := skill.Render(&SkillRenderOptions{
		Args: []string{},
	})
	if err == nil {
		t.Fatalf("Expected error for missing required argument, got nil")
	}
	if !strings.Contains(err.Error(), `required argument "file" is missing for skill "code-review"`) {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestAgentRender(t *testing.T) {
	ws := &Workspace{
		RootPath: "/Users/user/code/warp",
	}

	proj := &Project{
		Name:     "cli",
		Path:     "cmd/cli",
		RootPath: "/Users/user/code/warp",
	}

	agent := &Agent{
		BaseResource: BaseResource{
			Directory: "/Users/user/code/warp/.agents",
			Metadata: Metadata{
				Name:        "researcher",
				Description: "Code researcher",
			},
		},
		Spec: AgentSpec{
			Instructions: "Agent: {{.Agent.Name}} | Dir: {{.Agent.Dir}} | Project: {{.Project.Name}} | Skills: {{range .Skills}}{{.}} {{end}}| Tools: {{range .Agent.Tools}}{{.Name}}{{end}} | Commands: {{range .Commands}}{{.}} {{end}}| Subagents: {{range .Subagents}}{{.}} {{end}}| RichSubagents: {{range .Agent.Subagents}}{{.Name}} {{end}}",
		},
	}

	resolved := &ResolvedAgent{
		Agent: agent,
		Skills: []Skill{
			{
				BaseResource: BaseResource{
					Metadata: Metadata{Name: "go-expert"},
				},
			},
		},
		Tools: []*Tool{
			{
				BaseResource: BaseResource{
					Metadata: Metadata{Name: "view_file"},
				},
			},
		},
		Commands: []*Command{
			{
				BaseResource: BaseResource{
					Metadata: Metadata{Name: "run-tests"},
				},
			},
		},
		Subagents: []*Agent{
			{
				BaseResource: BaseResource{
					Metadata: Metadata{Name: "writer"},
				},
			},
		},
	}

	opts := &AgentRenderOptions{
		Workspace: ws,
		Project:   proj,
		Resolved:  resolved,
	}

	res, err := agent.Render(opts)
	if err != nil {
		t.Fatalf("Agent.Render failed: %v", err)
	}

	expected := "Agent: researcher | Dir: /Users/user/code/warp/.agents | Project: cli | Skills: go-expert | Tools: view_file | Commands: run-tests | Subagents: writer | RichSubagents: writer "
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}

	// Test nil safety
	var nilAgent *Agent
	resNil, err := nilAgent.Render(nil)
	if err != nil {
		t.Errorf("nil Agent should not error, got %v", err)
	}
	if resNil != "" {
		t.Errorf("nil Agent should render empty string, got %q", resNil)
	}
}

func TestDecisionRender(t *testing.T) {
	ws := &Workspace{
		RootPath: "/Users/user/code/warp",
	}

	proj := &Project{
		Name:     "cli",
		Path:     "cmd/cli",
		RootPath: "/Users/user/code/warp",
	}

	dec := &Decision{
		BaseResource: BaseResource{
			Directory: "/Users/user/code/warp/.agents/decisions",
			Metadata: Metadata{
				Name:        "pr-triage",
				Description: "Triage PR",
			},
		},
		Spec: DecisionSpec{
			Arguments: []Argument{
				{Name: "branch"},
			},
			Questions: []DecisionQuestion{
				{Name: "is_safe", Type: DecisionQuestionPredicate},
			},
			Instructions: "Decision: {{.Decision.Name}} | Dir: {{.Decision.Dir}} | Project: {{.Project.Name}} | Branch: $branch | Questions: {{range .Questions}}{{.Name}}({{.Type}}){{end}}",
		},
	}

	opts := &DecisionRenderOptions{
		Workspace: ws,
		Project:   proj,
		Args:      []string{"feature/auth"},
	}

	res, err := dec.Render(opts)
	if err != nil {
		t.Fatalf("Decision.Render failed: %v", err)
	}

	expected := "Decision: pr-triage | Dir: /Users/user/code/warp/.agents/decisions | Project: cli | Branch: feature/auth | Questions: is_safe(predicate)"
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}

	var nilDec *Decision
	resNil, err := nilDec.Render(nil)
	if err != nil {
		t.Errorf("nil Decision should not error, got %v", err)
	}
	if resNil != "" {
		t.Errorf("nil Decision should render empty string, got %q", resNil)
	}
}

func TestRuleRender(t *testing.T) {
	ws := &Workspace{
		RootPath: "/Users/user/code/warp",
	}

	proj := &Project{
		Name:     "backend",
		Path:     "services/backend",
		RootPath: "/Users/user/code/warp",
	}

	agent := &Agent{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "senior-dev"},
		},
	}

	rule := &Rule{
		BaseResource: BaseResource{
			Directory: "/Users/user/code/warp/.agents/rules",
			Metadata: Metadata{
				Name:        "go-style",
				Description: "Style guide for Go",
			},
		},
		Spec: RuleSpec{
			Priority:     50,
			Instructions: "Rule: {{.Rule.Name}} | Priority: {{.Rule.Priority}} | Project: {{.Project.Name}} | Agent: {{.Agent.Name}} | Path: {{.Rule.Path}}",
		},
	}

	opts := &RuleRenderOptions{
		Workspace: ws,
		Project:   proj,
		Agent:     agent,
	}

	res, err := rule.Render(opts)
	if err != nil {
		t.Fatalf("Rule.Render failed: %v", err)
	}

	expected := "Rule: go-style | Priority: 50 | Project: backend | Agent: senior-dev | Path: /Users/user/code/warp/.agents/rules/go-style.md"
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}

	var nilRule *Rule
	resNil, err := nilRule.Render(nil)
	if err != nil {
		t.Errorf("nil Rule should not error, got %v", err)
	}
	if resNil != "" {
		t.Errorf("nil Rule should render empty string, got %q", resNil)
	}
}

func TestAgentRender_WithRules(t *testing.T) {
	ag := &Agent{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "test-agent"},
		},
		Spec: AgentSpec{
			Instructions: "Agent: {{.Agent.Name}} | Rules: {{range .Rules}}{{.}} {{end}} | DetailedRules: {{range .Agent.Rules}}{{.Name}}(P{{.Priority}}) {{end}}",
		},
	}

	resolved := &ResolvedAgent{
		Agent: ag,
		Rules: []Rule{
			{
				BaseResource: BaseResource{Metadata: Metadata{Name: "sec-check"}},
				Spec:         RuleSpec{Priority: 100},
			},
			{
				BaseResource: BaseResource{Metadata: Metadata{Name: "style-guide"}},
				Spec:         RuleSpec{Priority: 10},
			},
		},
	}

	res, err := ag.Render(&AgentRenderOptions{Resolved: resolved})
	if err != nil {
		t.Fatalf("Agent.Render failed: %v", err)
	}

	expected := "Agent: test-agent | Rules: sec-check style-guide  | DetailedRules: sec-check(P100) style-guide(P10) "
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}
}
