package warp

// MCP is a warp resource that describes an MCP server.
type MCP struct {
	BaseResource `yaml:",inline"`
	// Spec holds the MCP-specific configuration.
	Spec MCPSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the MCP.
func (in *MCP) DeepCopy() *MCP {
	if in == nil {
		return nil
	}
	out := new(MCP)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

// MCPSpec contains the configuration details for an MCP resource.
type MCPSpec struct {
	Command     []string                  `yaml:"command"`     // Command to start the MCP server via stdio
	Env         map[string]string         `yaml:"env"`         // Environment variables for the MCP server
	Annotations *ToolAnnotation           `yaml:"annotations"` // Default safety profile for all exposed tools
	Tools       *MCPFilter                `yaml:"tools"`       // Controls which tools are exposed by this server
	Overrides   map[string]ToolAnnotation `yaml:"overrides"`   // Tool-specific annotation overrides (key is tool name)
}

// MCPFilter defines the inclusion/exclusion rules for MCP tools.
type MCPFilter struct {
	Include []string `yaml:"include"` // Glob patterns for tools to expose
	Exclude []string `yaml:"exclude"` // Glob patterns for tools to block (applied after include)
}

// DeepCopy returns a deep copy of the MCPFilter.
func (in *MCPFilter) DeepCopy() *MCPFilter {
	if in == nil {
		return nil
	}
	out := new(MCPFilter)
	if in.Include != nil {
		out.Include = make([]string, len(in.Include))
		copy(out.Include, in.Include)
	}
	if in.Exclude != nil {
		out.Exclude = make([]string, len(in.Exclude))
		copy(out.Exclude, in.Exclude)
	}
	return out
}

// DeepCopy returns a deep copy of the MCPSpec.
func (in *MCPSpec) DeepCopy() *MCPSpec {
	if in == nil {
		return nil
	}
	out := new(MCPSpec)
	*out = *in
	if in.Command != nil {
		out.Command = make([]string, len(in.Command))
		copy(out.Command, in.Command)
	}
	if in.Env != nil {
		out.Env = make(map[string]string, len(in.Env))
		for k, v := range in.Env {
			out.Env[k] = v
		}
	}
	out.Annotations = in.Annotations.DeepCopy()
	out.Tools = in.Tools.DeepCopy()
	if in.Overrides != nil {
		out.Overrides = make(map[string]ToolAnnotation, len(in.Overrides))
		for k, v := range in.Overrides {
			out.Overrides[k] = v
		}
	}
	return out
}
