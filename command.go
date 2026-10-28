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
				Path:         filepath.Join(opts.Project.Context.Directory, ContextFileName),
			}
			mergedGlobals["Context"] = tc
			mergedGlobals["ContextPath"] = tc.Path
		}
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
