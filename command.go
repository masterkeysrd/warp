package warp

import "path/filepath"

// CommandRenderOptions holds options for rendering command instructions.
type CommandRenderOptions struct {
	Workspace *Workspace
	Project   *Project
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
	for k, v := range opts.Globals {
		mergedGlobals[k] = v
	}

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
	} else {
		mergedGlobals["Project"] = TemplateProject{}
		mergedGlobals["ProjectDir"] = ""
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
func (in *Command) DeepCopy() *Command {
	if in == nil {
		return nil
	}
	out := new(Command)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

// CommandSpec contains the configuration details for a Command resource.
type CommandSpec struct {
	// Instructions is the expertise prompt populated from the Markdown body
	// of the file (below the closing front-matter delimiter).
	Instructions string `yaml:"instructions,omitempty"`

	// Models is a prioritized list of LLM model identifiers to use for this
	// command (e.g., ["gpt-4o-mini", "claude-3-haiku"]). Overrides agent defaults.
	Models []string `yaml:"models,omitempty,flow"`
	// Tools is a list of resource refs (names or paths) restricting which
	// Tool resources can be used while executing this command.
	Tools []string `yaml:"tools,omitempty"`
	// Hints is an ordered list of argument hints (e.g., ["ticker", "year"])
	// that UIs can use for autocompletion and runtimes can use for positional
	// template substitution.
	Hints []string `yaml:"hints,omitempty"`
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
	if in.Tools != nil {
		out.Tools = make([]string, len(in.Tools))
		copy(out.Tools, in.Tools)
	}
	if in.Hints != nil {
		out.Hints = make([]string, len(in.Hints))
		copy(out.Hints, in.Hints)
	}
	return out
}
