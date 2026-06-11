package warp

import "maps"

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
	Command     []string                  `yaml:"command"`               // Command to start the MCP server via stdio
	Env         map[string]string         `yaml:"env,omitempty"`         // Environment variables for the MCP server
	Annotations *ToolAnnotation           `yaml:"annotations,omitempty"` // Default safety profile for all exposed tools
	Policies    *MCPPolicies              `yaml:"policies,omitempty"`    // Controls which external MCP resources are exposed
	Overrides   map[string]ToolAnnotation `yaml:"overrides,omitempty"`   // Tool-specific annotation overrides (key is tool name)
}

// MCPPolicies defines inclusion and exclusion rules for resources exposed by an MCP server.
type MCPPolicies struct {
	Tools     *ResourceFilter `yaml:"tools,omitempty"`
	Prompts   *ResourceFilter `yaml:"prompts,omitempty"`
	Resources *ResourceFilter `yaml:"resources,omitempty"`
}

// DeepCopy returns a deep copy of the MCPPolicies.
func (in *MCPPolicies) DeepCopy() *MCPPolicies {
	if in == nil {
		return nil
	}
	out := new(MCPPolicies)
	out.Tools = in.Tools.DeepCopy()
	out.Prompts = in.Prompts.DeepCopy()
	out.Resources = in.Resources.DeepCopy()
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
		maps.Copy(out.Env, in.Env)
	}
	out.Annotations = in.Annotations.DeepCopy()
	out.Policies = in.Policies.DeepCopy()
	if in.Overrides != nil {
		out.Overrides = make(map[string]ToolAnnotation, len(in.Overrides))
		maps.Copy(out.Overrides, in.Overrides)
	}
	return out
}
