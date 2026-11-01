package warp

import (
	"maps"
	"path/filepath"
)

// CommandContextMode defines how conversational memory is handled when executing a command.
type CommandContextMode string

const (
	// CommandContextInline executes directly inside the current conversation thread (shared history).
	CommandContextInline CommandContextMode = "inline"
	// CommandContextFork branches a sub-session with a snapshot of the current history.
	CommandContextFork CommandContextMode = "fork"
	// CommandContextClean starts a sub-session with a clean slate (zero conversation history).
	CommandContextClean CommandContextMode = "clean"
)

// CommandRenderOptions holds options for rendering command instructions.
type CommandRenderOptions struct {
	Workspace *Workspace
	Project   *Project
	Agent     *Agent
	Resolved  *ResolvedCommand
	Args      []string
	Globals   map[string]any
}

// Render processes the command's instructions as a template.
// It acts as a rich extension that injects Workspace, Project scope, and Args into the generic Render.
func (c *Command) Render(opts *CommandRenderOptions) (string, error) {
	if c == nil {
		return "", nil
	}
	if opts == nil {
		opts = &CommandRenderOptions{}
	}

	mergedGlobals := make(map[string]any)
	maps.Copy(mergedGlobals, opts.Globals)

	// Inject Workspace derived from arguments
	if opts.Workspace != nil {
		tw := TemplateWorkspace{
			Dir:  opts.Workspace.RootPath,
			Path: filepath.Join(opts.Workspace.RootPath, WorkspaceFileName),
		}
		mergedGlobals["Workspace"] = tw
		mergedGlobals["WorkspaceDir"] = tw.Dir
		mergedGlobals["WorkspacePath"] = tw.Path
	} else {
		mergedGlobals["Workspace"] = TemplateWorkspace{}
		mergedGlobals["WorkspaceDir"] = ""
		mergedGlobals["WorkspacePath"] = ""
	}

	// Inject Project derived from arguments
	if opts.Project != nil {
		var displayName string
		if opts.Project.Context != nil && opts.Project.Context.Metadata.DisplayName != "" {
			displayName = opts.Project.Context.Metadata.DisplayName
		} else {
			displayName = opts.Project.Name
		}

		tp := TemplateProject{
			Name:        opts.Project.Name,
			DisplayName: displayName,
			Dir:         opts.Project.AbsPath(),
		}
		mergedGlobals["Project"] = tp
		mergedGlobals["ProjectDir"] = tp.Dir

		if opts.Project.Context != nil {
			tc := TemplateContext{
				Instructions: opts.Project.Context.Spec.Instructions,
				Path:         opts.Project.Context.GetPath(),
			}
			mergedGlobals["Context"] = tc
			mergedGlobals["ContextPath"] = tc.Path
		}
	} else {
		mergedGlobals["Project"] = TemplateProject{}
		mergedGlobals["ProjectDir"] = ""
	}

	// Inject invoking Agent derived from arguments or resolved command
	var ag *Agent
	if opts.Agent != nil {
		ag = opts.Agent
	} else if opts.Resolved != nil && opts.Resolved.Agent != nil && opts.Resolved.Agent.Agent != nil {
		ag = opts.Resolved.Agent.Agent
	}

	if ag != nil {
		ta := TemplateAgent{
			Name:        ag.Metadata.Name,
			Description: ag.Metadata.Description,
			Dir:         ag.Directory,
			Path:        ag.GetPath(),
		}
		mergedGlobals["Agent"] = ta
	} else {
		mergedGlobals["Agent"] = TemplateAgent{}
	}

	// Inject resolved command details if provided
	if opts.Resolved != nil {
		var tools []TemplateTool
		for _, t := range opts.Resolved.Tools {
			tools = append(tools, TemplateTool{
				Name:        t.Metadata.Name,
				Description: t.Metadata.Description,
				Dir:         t.Directory,
				Path:        t.GetPath(),
			})
		}
		var skills []TemplateSkill
		for _, s := range opts.Resolved.Skills {
			skills = append(skills, TemplateSkill{
				Name:         s.Metadata.Name,
				Description:  s.Metadata.Description,
				UseWhen:      s.Spec.UseWhen,
				Keywords:     s.Spec.Keywords,
				Tools:        s.Spec.Tools,
				ArgumentHint: s.Spec.ArgumentHint,
				Arguments:    s.Spec.Arguments,
				Dir:          s.Directory,
				Path:         s.GetPath(),
			})
		}
		var rules []TemplateRule
		for _, r := range opts.Resolved.Rules {
			rules = append(rules, TemplateRule{
				Name:        r.Metadata.Name,
				Description: r.Metadata.Description,
				UseWhen:     r.Spec.Triggers.UseWhen,
				Keywords:    r.Spec.Triggers.Keywords,
				Dir:         r.Directory,
				Path:        r.GetPath(),
				Priority:    r.Spec.Priority,
			})
		}
		ctxMode := c.Spec.Context
		if ctxMode == "" {
			ctxMode = CommandContextInline
		}
		tc := TemplateCommand{
			Name:         c.Metadata.Name,
			Description:  c.Metadata.Description,
			Agent:        c.Spec.Agent,
			Context:      ctxMode,
			ArgumentHint: c.Spec.ArgumentHint,
			Arguments:    c.Spec.Arguments,
			Dir:          c.Directory,
			Path:         c.GetPath(),
			Tools:        tools,
			Skills:       skills,
			Rules:        rules,
		}
		mergedGlobals["Command"] = tc
		mergedGlobals["CommandDir"] = tc.Dir
		mergedGlobals["CommandPath"] = tc.Path
	}

	renderOpts := &RenderOptions{
		Args:    opts.Args,
		Globals: mergedGlobals,
	}

	return Render(c, renderOpts)
}

// Command is a warp resource that encapsulates a discrete, reusable
// operation an agent can perform. Its instructions are authored as the
// Markdown body of the defining file.
type Command struct {
	BaseResource `yaml:",inline"`
	// Spec holds the command-specific configuration.
	Spec CommandSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Command.
func (c *Command) DeepCopy() *Command {
	if c == nil {
		return nil
	}
	out := new(Command)
	out.BaseResource = *c.BaseResource.DeepCopy()
	out.Spec = *c.Spec.DeepCopy()
	return out
}

// CommandSpec contains the configuration details for a Command resource.
type CommandSpec struct {
	// Instructions is the expertise prompt populated from the Markdown body
	// of the file (below the closing front-matter delimiter).
	Instructions string `yaml:"instructions,omitempty"`

	// Agent specifies the preferred or required Agent resource to execute this command.
	// If omitted, the invoking agent or workspace defaultAgent is used.
	Agent string `yaml:"agent,omitempty"`
	// Context specifies the conversational execution lifecycle mode: "inline", "fork", or "clean".
	// Defaults to "inline".
	Context CommandContextMode `yaml:"context,omitempty"`

	// Models is a prioritized list of LLM model identifiers to use for this
	// command (e.g., ["gpt-4o-mini", "claude-3-haiku"]). Overrides agent defaults.
	Models []string `yaml:"models,omitempty,flow"`
	// Skills is a list of Skill refs or globs eagerly loaded by the system when executing this command.
	Skills []string `yaml:"skills,omitempty" json:"skills,omitempty"`
	// Rules is a list of Rule refs or globs eagerly loaded (always: true) by the system when executing this command.
	Rules []string `yaml:"rules,omitempty" json:"rules,omitempty"`
	// Tools is a list of resource refs (names or paths) restricting which
	// Tool resources can be used while executing this command.
	Tools []string `yaml:"tools,omitempty"`
	// ArgumentHint is a single-line text hint telling the agent or user what
	// input format or example to provide upon invocation.
	ArgumentHint string `yaml:"argumentHint,omitempty"`
	// Arguments is the list of structured parameters accepted by the command.
	Arguments []Argument `yaml:"arguments,omitempty"`
}

// DeepCopy returns a deep copy of the CommandSpec.
func (in *CommandSpec) DeepCopy() *CommandSpec {
	if in == nil {
		return nil
	}
	out := new(CommandSpec)
	*out = *in
	if in.Models != nil {
		out.Models = make([]string, len(in.Models))
		copy(out.Models, in.Models)
	}
	if in.Skills != nil {
		out.Skills = make([]string, len(in.Skills))
		copy(out.Skills, in.Skills)
	}
	if in.Rules != nil {
		out.Rules = make([]string, len(in.Rules))
		copy(out.Rules, in.Rules)
	}
	if in.Tools != nil {
		out.Tools = make([]string, len(in.Tools))
		copy(out.Tools, in.Tools)
	}
	if in.Arguments != nil {
		out.Arguments = make([]Argument, len(in.Arguments))
		copy(out.Arguments, in.Arguments)
	}
	return out
}
