package warp

import (
	"testing"
)

func TestTemplateRender_Shorthand(t *testing.T) {
	cmd := &Command{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "test-cmd"},
		},
		Spec: CommandSpec{
			Instructions: "Hello $1, this is $hint1 and ${hint2}.",
			Hints:        []string{"hint1", "hint2"},
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

	expected := "Project Name: cli | Project DisplayName: Warp CLI | Project Dir: /Users/user/code/warp/cmd/cli | Workspace Dir: /Users/user/code/warp | Context Path: /Users/user/code/warp/cmd/cli/AGENT.md | Shorthand Project Dir: /Users/user/code/warp/cmd/cli | Shorthand Workspace Dir: /Users/user/code/warp | Shorthand Context Path: /Users/user/code/warp/cmd/cli/AGENT.md | CI: true"
	if res != expected {
		t.Errorf("Expected:\n%q\nGot:\n%q", expected, res)
	}

	// Test nil safety with empty/nil options
	resNilOpts, err := ctx.Render(nil)
	if err != nil {
		t.Fatalf("Context.Render with nil opts failed: %v", err)
	}
	expectedNilOpts := "Project Name:  | Project DisplayName:  | Project Dir:  | Workspace Dir:  | Context Path: /Users/user/code/warp/cmd/cli/AGENT.md | Shorthand Project Dir:  | Shorthand Workspace Dir:  | Shorthand Context Path: /Users/user/code/warp/cmd/cli/AGENT.md | CI: <no value>"
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
			Hints:        []string{"ticket", "env"},
			Instructions: "Command: {{.Command.Name}} | Dir: {{.Command.Dir}} | Project: {{.Project.Name}} | Args: $1, $2 | Hints: $ticket, $env",
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

	expected := "Command: test-cmd | Dir: /Users/user/code/warp/commands | Project: cli | Args: TICKET-123, prod | Hints: TICKET-123, prod"
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
			Instructions: "Agent: {{.Agent.Name}} | Dir: {{.Agent.Dir}} | Project: {{.Project.Name}} | Skills: {{range .Agent.Skills}}{{.Name}}{{end}} | Tools: {{range .Agent.Tools}}{{.Name}}{{end}} | Commands: {{range .Agent.Commands}}{{.Name}}{{end}}",
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

	expected := "Agent: researcher | Dir: /Users/user/code/warp/.agents | Project: cli | Skills: go-expert | Tools: view_file | Commands: run-tests"
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
