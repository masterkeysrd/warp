package warp

// Agent is a warp resource that describes an autonomous agent: its LLM
// configuration, persona instructions, and the set of skills and commands it
// may invoke at runtime.
type Agent struct {
	BaseResource `yaml:",inline"`
	// Spec holds the agent-specific configuration.
	Spec AgentSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Agent.
func (in *Agent) DeepCopy() *Agent {
	if in == nil {
		return nil
	}
	out := new(Agent)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

// AgentSpec contains the configuration details for an Agent resource.
type AgentSpec struct {
	// Extends is the Qualified Name or Short Name of another Agent resource to
	// inherit from. When set, the engine merges the parent's skills and tools
	// arrays (parent first) and prepends the parent's instructions to this
	// agent's instructions.
	Extends string `yaml:"extends,omitempty"`
	// Instructions is the persona prompt populated from the Markdown body of
	// the file (below the closing front-matter delimiter).
	Instructions string `yaml:"instructions,omitempty"`
	// Triggers defines the architectural constraints on what can invoke this agent
	// (e.g., "human", "agent"). An empty list means the agent can be triggered
	// by anything.
	Triggers []string `yaml:"triggers,omitempty,flow"`
	// Models is a prioritized list of LLM model identifiers to use (e.g.,
	// ["gpt-4o", "claude-3-5-sonnet"]). The runtime should attempt to use the
	// first available model.
	Models []string `yaml:"models,omitempty,flow"`
	// Temperature controls the randomness of the model's output (0.0–2.0).
	Temperature float64 `yaml:"temperature"`
	// Tools is a list of resource refs (names or paths) restricting which
	// Tool resources this agent may use. An empty list means no restriction.
	Tools []string `yaml:"tools,omitempty"`
	// Skills is a list of file paths (relative to the FS root) that reference
	// Skill resources this agent is allowed to use.
	Skills []string `yaml:"skills,omitempty"`
	// Commands is a list of file paths (relative to the FS root) that
	// reference Command resources this agent can invoke.
	Commands []string `yaml:"commands,omitempty"`
}

// DeepCopy returns a deep copy of the AgentSpec.
func (in *AgentSpec) DeepCopy() *AgentSpec {
	if in == nil {
		return nil
	}
	out := new(AgentSpec)
	*out = *in
	if in.Triggers != nil {
		out.Triggers = make([]string, len(in.Triggers))
		copy(out.Triggers, in.Triggers)
	}
	if in.Models != nil {
		out.Models = make([]string, len(in.Models))
		copy(out.Models, in.Models)
	}
	if in.Tools != nil {
		out.Tools = make([]string, len(in.Tools))
		copy(out.Tools, in.Tools)
	}
	if in.Skills != nil {
		out.Skills = make([]string, len(in.Skills))
		copy(out.Skills, in.Skills)
	}
	if in.Commands != nil {
		out.Commands = make([]string, len(in.Commands))
		copy(out.Commands, in.Commands)
	}
	return out
}
