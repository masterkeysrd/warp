package warp

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
	// Instructions is the directive prompt populated from the Markdown body
	// of the file (below the closing front-matter delimiter).
	Instructions string `yaml:"instructions"`
	// Models is a prioritized list of LLM model identifiers to use for this
	// command (e.g., ["gpt-4o-mini", "claude-3-haiku"]). Overrides agent defaults.
	Models []string `yaml:"models,omitempty"`
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
