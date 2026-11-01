package warp

import (
	"slices"
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
			Skills:       []string{"go", "rust"},
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
			Tools:        []string{"go_test", "go_build"},
			Instructions: "Skill: {{.Skill.Name}} | Dir: {{.Skill.Dir}} | Path: {{.Skill.Path}} | Shorthand Dir: $SkillDir | Shorthand Path: $SkillPath | Project: {{.Project.Name}} | Invoker: {{.Agent.Name}} | InvokerDesc: {{.Agent.Description}} | Tools: {{range .Skill.Tools}}{{.}} {{end}}",
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

	expected := "Skill: go-expert | Dir: /Users/user/code/warp/skills | Path: /Users/user/code/warp/skills/go-expert.md | Shorthand Dir: /Users/user/code/warp/skills | Shorthand Path: /Users/user/code/warp/skills/go-expert.md | Project: cli | Invoker: researcher | InvokerDesc: Code researcher | Tools: go_test go_build "
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
			Instructions: "Review: '$file'",
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

func TestCommandRender_AgentAndContext(t *testing.T) {
	cmd := &Command{
		BaseResource: BaseResource{
			Metadata: Metadata{
				Name: "audit",
			},
		},
		Spec: CommandSpec{
			Agent:        "security-analyst",
			Context:      CommandContextFork,
			Instructions: "Agent: {{.Command.Agent}} | Context: {{.Command.Context}}",
		},
	}

	res, err := cmd.Render(nil)
	if err != nil {
		t.Fatalf("Command.Render failed: %v", err)
	}
	expected := "Agent: security-analyst | Context: fork"
	if res != expected {
		t.Errorf("Expected %q, got %q", expected, res)
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

	agent := &Agent{
		BaseResource: BaseResource{
			Directory: "/Users/user/code/warp/.agents",
			Path:      "/Users/user/code/warp/.agents/reviewer.md",
			Metadata: Metadata{
				Name:        "code-reviewer",
				Description: "Reviews code changes",
			},
		},
	}

	dec := &Decision{
		BaseResource: BaseResource{
			Directory: "/Users/user/code/warp/.agents/decisions",
			Path:      "/Users/user/code/warp/.agents/decisions/pr-triage.md",
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
				{Name: "priority", Type: DecisionQuestionScore},
			},
			Instructions: "Decision: {{.Decision.Name}} | Dir: {{.Decision.Dir}} | Path: {{.Decision.Path}} | ShorthandDir: $DecisionDir | ShorthandPath: $DecisionPath | Project: {{.Project.Name}} | Branch: $branch | Agent: {{.Agent.Name}} | Questions: {{range .Decision.Questions}}{{.Name}}({{.Type}}) {{end}}| FlatQuestions: {{range .Questions}}{{.Name}} {{end}}",
		},
	}

	opts := &DecisionRenderOptions{
		Workspace: ws,
		Project:   proj,
		Agent:     agent,
		Args:      []string{"feature/auth"},
	}

	res, err := dec.Render(opts)
	if err != nil {
		t.Fatalf("Decision.Render failed: %v", err)
	}

	expected := "Decision: pr-triage | Dir: /Users/user/code/warp/.agents/decisions | Path: /Users/user/code/warp/.agents/decisions/pr-triage.md | ShorthandDir: /Users/user/code/warp/.agents/decisions | ShorthandPath: /Users/user/code/warp/.agents/decisions/pr-triage.md | Project: cli | Branch: feature/auth | Agent: code-reviewer | Questions: is_safe(predicate) priority(score) | FlatQuestions: is_safe priority "
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

func TestDecisionRender_AgentNilAndShorthands(t *testing.T) {
	dec := &Decision{
		BaseResource: BaseResource{
			Directory: "/workspace/decisions",
			Metadata: Metadata{
				Name: "check",
			},
		},
		Spec: DecisionSpec{
			Questions: []DecisionQuestion{
				{Name: "ok", Type: DecisionQuestionPredicate},
			},
			Instructions: "Dir:$DecisionDir Path:$DecisionPath Questions:{{range .Decision.Questions}}{{.Name}}{{end}} Agent:{{.Agent.Name}}",
		},
	}

	res, err := dec.Render(nil)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	expected := "Dir:/workspace/decisions Path:/workspace/decisions/check.md Questions:ok Agent:"
	if res != expected {
		t.Errorf("Expected %q, got %q", expected, res)
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
			Priority: 50,
			Triggers: RuleTriggers{
				UseWhen:  "editing Go files",
				Keywords: []string{"golang", "gofmt"},
			},
			Instructions: "Rule: {{.Rule.Name}} | Priority: {{.Rule.Priority}} | UseWhen: {{.Rule.UseWhen}} | Keywords: {{range .Rule.Keywords}}{{.}} {{end}}| Project: {{.Project.Name}} | Agent: {{.Agent.Name}} | Path: {{.Rule.Path}}",
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

	expected := "Rule: go-style | Priority: 50 | UseWhen: editing Go files | Keywords: golang gofmt | Project: backend | Agent: senior-dev | Path: /Users/user/code/warp/.agents/rules/go-style.md"
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
			Instructions: "Agent: {{.Agent.Name}} | Rules: {{range .Rules}}{{.}} {{end}} | DetailedRules: {{range .Agent.Rules}}{{.Name}}(P{{.Priority}},UseWhen={{.UseWhen}},Keywords={{range .Keywords}}{{.}},{{end}}) {{end}}",
		},
	}

	resolved := &ResolvedAgent{
		Agent: ag,
		Rules: []Rule{
			{
				BaseResource: BaseResource{Metadata: Metadata{Name: "sec-check"}},
				Spec: RuleSpec{
					Priority: 100,
					Triggers: RuleTriggers{
						UseWhen:  "writing auth code",
						Keywords: []string{"auth", "jwt"},
					},
				},
			},
			{
				BaseResource: BaseResource{Metadata: Metadata{Name: "style-guide"}},
				Spec: RuleSpec{
					Priority: 10,
					Triggers: RuleTriggers{
						UseWhen:  "formatting code",
						Keywords: []string{"lint"},
					},
				},
			},
		},
	}

	res, err := ag.Render(&AgentRenderOptions{Resolved: resolved})
	if err != nil {
		t.Fatalf("Agent.Render failed: %v", err)
	}

	expected := "Agent: test-agent | Rules: sec-check style-guide  | DetailedRules: sec-check(P100,UseWhen=writing auth code,Keywords=auth,jwt,) style-guide(P10,UseWhen=formatting code,Keywords=lint,) "
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}
}

func TestCommandRender_ResolvedAndShorthands(t *testing.T) {
	cmd := &Command{
		BaseResource: BaseResource{
			Directory: "/workspace/commands",
			Path:      "/workspace/commands/build.md",
			Metadata: Metadata{
				Name: "build",
			},
		},
		Spec: CommandSpec{
			Skills:       []string{"go-build"},
			Rules:        []string{"no-cgo"},
			Instructions: "Dir: $CommandDir | Path: $CommandPath | Agent: {{.Agent.Name}} | Skills: {{range .Command.Skills}}{{.Name}} {{end}}| Rules: {{range .Command.Rules}}{{.Name}} {{end}}| Tools: {{range .Command.Tools}}{{.Name}} {{end}}| FlattenedSkills: {{range .Skills}}{{.}} {{end}}| FlattenedRules: {{range .Rules}}{{.}} {{end}}",
		},
	}

	execAgent := &Agent{
		BaseResource: BaseResource{
			Directory: "/workspace/agents",
			Path:      "/workspace/agents/builder.md",
			Metadata: Metadata{
				Name: "builder",
			},
		},
	}

	resolved := &ResolvedCommand{
		Command: cmd,
		Agent: &ResolvedAgent{
			Agent: execAgent,
		},
		Skills: []Skill{
			{
				BaseResource: BaseResource{Metadata: Metadata{Name: "go-build"}},
				Spec:         SkillSpec{UseWhen: "compiling Go"},
			},
		},
		Rules: []Rule{
			{
				BaseResource: BaseResource{Metadata: Metadata{Name: "no-cgo"}},
				Spec:         RuleSpec{Priority: 90},
			},
		},
		Tools: []*Tool{
			{
				BaseResource: BaseResource{Metadata: Metadata{Name: "compile"}},
			},
		},
	}

	res, err := cmd.Render(&CommandRenderOptions{Resolved: resolved})
	if err != nil {
		t.Fatalf("Command.Render failed: %v", err)
	}

	expected := "Dir: /workspace/commands | Path: /workspace/commands/build.md | Agent: builder | Skills: go-build | Rules: no-cgo | Tools: compile | FlattenedSkills: go-build | FlattenedRules: no-cgo "
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}
}

func TestToolRender(t *testing.T) {
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
			Path:      "/Users/user/code/warp/.agents/reviewer.md",
			Metadata: Metadata{
				Name:        "code-reviewer",
				Description: "Reviews code",
			},
		},
	}

	tool := &Tool{
		BaseResource: BaseResource{
			Directory: "/Users/user/code/warp/tools",
			Path:      "/Users/user/code/warp/tools/ls.yaml",
			Metadata: Metadata{
				Name:        "list-files",
				Description: "List files in a directory",
			},
		},
		Spec: ToolSpec{
			Instructions: "Tool: {{.Tool.Name}} | Desc: {{.Tool.Description}} | Dir: {{.Tool.Dir}} | Path: {{.Tool.Path}} | ShorthandDir: $ToolDir | ShorthandPath: $ToolPath | Project: {{.Project.Name}} | Agent: {{.Agent.Name}} | Input: $path",
		},
	}

	opts := &ToolRenderOptions{
		Workspace: ws,
		Project:   proj,
		Agent:     agent,
		Input:     map[string]any{"path": "src/pkg"},
	}

	res, err := tool.Render(opts)
	if err != nil {
		t.Fatalf("Tool.Render failed: %v", err)
	}

	expected := "Tool: list-files | Desc: List files in a directory | Dir: /Users/user/code/warp/tools | Path: /Users/user/code/warp/tools/ls.yaml | ShorthandDir: /Users/user/code/warp/tools | ShorthandPath: /Users/user/code/warp/tools/ls.yaml | Project: cli | Agent: code-reviewer | Input: src/pkg"
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}

	var nilTool *Tool
	resNil, err := nilTool.Render(nil)
	if err != nil {
		t.Errorf("nil Tool should not error, got %v", err)
	}
	if resNil != "" {
		t.Errorf("nil Tool should render empty string, got %q", resNil)
	}
}

func TestToolRenderCommand(t *testing.T) {
	ws := &Workspace{
		RootPath: "/workspace",
	}

	proj := &Project{
		Name:     "my-proj",
		Path:     "packages/core",
		RootPath: "/workspace",
	}

	agent := &Agent{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "worker"},
		},
	}

	tool := &Tool{
		BaseResource: BaseResource{
			Directory: "/tools/scripts",
			Path:      "/tools/scripts/run.yaml",
			Metadata: Metadata{
				Name: "runner",
			},
		},
		Spec: ToolSpec{
			Command: []string{
				"python",
				"$ToolDir/script.py",
				"--target",
				"$path",
				"--proj",
				"$ProjectDir",
				"--ws",
				"$WorkspaceDir",
				"--by",
				"{{.Agent.Name}}",
			},
		},
	}

	opts := &ToolRenderOptions{
		Workspace: ws,
		Project:   proj,
		Agent:     agent,
		Input: map[string]any{
			"path": "data/sample.json",
		},
	}

	out, err := tool.RenderCommand(opts)
	if err != nil {
		t.Fatalf("Tool.RenderCommand failed: %v", err)
	}

	expected := []string{
		"python",
		"/tools/scripts/script.py",
		"--target",
		"data/sample.json",
		"--proj",
		"/workspace/packages/core",
		"--ws",
		"/workspace",
		"--by",
		"worker",
	}

	if !slices.Equal(out, expected) {
		t.Errorf("RenderCommand result:\nGot:  %v\nWant: %v", out, expected)
	}

	// Nil tool / empty command
	var nilTool *Tool
	outNil, err := nilTool.RenderCommand(nil)
	if err != nil || outNil != nil {
		t.Errorf("expected (nil, nil) for nil tool, got (%v, %v)", outNil, err)
	}

	emptyTool := &Tool{}
	outEmpty, err := emptyTool.RenderCommand(nil)
	if err != nil || outEmpty != nil {
		t.Errorf("expected (nil, nil) for empty tool command, got (%v, %v)", outEmpty, err)
	}
}

func TestMCP_RenderCommandAndEndpoint(t *testing.T) {
	ws := &Workspace{
		RootPath: "/workspace",
	}

	proj := &Project{
		Name:     "web-app",
		Path:     "apps/web",
		RootPath: "/workspace",
	}

	agent := &Agent{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "dev-bot"},
		},
	}

	mcpStdio := &MCP{
		BaseResource: BaseResource{
			Directory: "/workspace/mcps",
			Path:      "/workspace/mcps/sqlite.yaml",
			Metadata: Metadata{
				Name:        "sqlite",
				Description: "SQLite MCP server",
			},
		},
		Spec: MCPSpec{
			Type: MCPTransportStdio,
			Command: []string{
				"uvx",
				"mcp-server-sqlite",
				"--db-path",
				"$MCPDir/data.db",
				"--proj",
				"$ProjectDir",
				"--ws",
				"$WorkspaceDir",
				"--agent",
				"{{.Agent.Name}}",
				"--server-name",
				"{{.MCP.Name}}",
				"--transport",
				"{{.MCP.Type}}",
				"--file",
				"$MCPPath",
			},
		},
	}

	opts := &MCPRenderOptions{
		Workspace: ws,
		Project:   proj,
		Agent:     agent,
	}

	cmdOut, err := mcpStdio.RenderCommand(opts)
	if err != nil {
		t.Fatalf("MCP.RenderCommand failed: %v", err)
	}

	expectedCmd := []string{
		"uvx",
		"mcp-server-sqlite",
		"--db-path",
		"/workspace/mcps/data.db",
		"--proj",
		"/workspace/apps/web",
		"--ws",
		"/workspace",
		"--agent",
		"dev-bot",
		"--server-name",
		"sqlite",
		"--transport",
		"stdio",
		"--file",
		"/workspace/mcps/sqlite.yaml",
	}

	if !slices.Equal(cmdOut, expectedCmd) {
		t.Errorf("RenderCommand:\nGot:  %v\nWant: %v", cmdOut, expectedCmd)
	}

	// Remote MCP with Endpoint rendering
	mcpRemote := &MCP{
		BaseResource: BaseResource{
			Directory: "/workspace/mcps",
			Path:      "/workspace/mcps/remote.yaml",
			Metadata: Metadata{
				Name: "cloud-mcp",
			},
		},
		Spec: MCPSpec{
			Type:     MCPTransportSSE,
			Endpoint: "https://$HOST:8080/mcp/{{.MCP.Name}}/sse?proj=$ProjectDir",
		},
	}

	optsRemote := &MCPRenderOptions{
		Project: proj,
		Globals: map[string]any{
			"HOST": "internal.service.local",
		},
	}

	epOut, err := mcpRemote.RenderEndpoint(optsRemote)
	if err != nil {
		t.Fatalf("MCP.RenderEndpoint failed: %v", err)
	}

	expectedEndpoint := "https://internal.service.local:8080/mcp/cloud-mcp/sse?proj=/workspace/apps/web"
	if epOut != expectedEndpoint {
		t.Errorf("RenderEndpoint:\nGot:  %q\nWant: %q", epOut, expectedEndpoint)
	}

	// Nil / empty checks
	var nilMCP *MCP
	outNilCmd, err := nilMCP.RenderCommand(nil)
	if err != nil || outNilCmd != nil {
		t.Errorf("expected (nil, nil) for nil MCP command, got (%v, %v)", outNilCmd, err)
	}
	outNilEp, err := nilMCP.RenderEndpoint(nil)
	if err != nil || outNilEp != "" {
		t.Errorf("expected (\"\", nil) for nil MCP endpoint, got (%q, %v)", outNilEp, err)
	}

	emptyMCP := &MCP{}
	outEmptyCmd, err := emptyMCP.RenderCommand(nil)
	if err != nil || outEmptyCmd != nil {
		t.Errorf("expected (nil, nil) for empty MCP command, got (%v, %v)", outEmptyCmd, err)
	}
	outEmptyEp, err := emptyMCP.RenderEndpoint(nil)
	if err != nil || outEmptyEp != "" {
		t.Errorf("expected (\"\", nil) for empty MCP endpoint, got (%q, %v)", outEmptyEp, err)
	}
}
