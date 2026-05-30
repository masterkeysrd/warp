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
}

// DeepCopy returns a deep copy of the CommandSpec.
func (in *CommandSpec) DeepCopy() *CommandSpec {
	if in == nil {
		return nil
	}
	out := new(CommandSpec)
	*out = *in
	return out
}
