package warp

// Toolkit is a warp resource that describes a collection of tools.
type Toolkit struct {
	BaseResource `yaml:",inline"`
	// Spec holds the toolkit-specific configuration.
	Spec ToolkitSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Toolkit.
func (in *Toolkit) DeepCopy() *Toolkit {
	if in == nil {
		return nil
	}
	out := new(Toolkit)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

// ToolkitTool can be either a reference to an external file or a fully inline ToolSpec.
type ToolkitTool struct {
	Ref      string           `yaml:"$ref,omitempty"` // Path to external tool (e.g., "tools/formatter.yaml")
	ToolSpec `yaml:",inline"` // Inline Tool definition
}

// DeepCopy returns a deep copy of the ToolkitTool.
func (in *ToolkitTool) DeepCopy() *ToolkitTool {
	if in == nil {
		return nil
	}
	out := new(ToolkitTool)
	out.Ref = in.Ref
	out.ToolSpec = *in.ToolSpec.DeepCopy()
	return out
}

// ToolkitSpec contains the configuration details for a Toolkit resource.
type ToolkitSpec struct {
	Tools []ToolkitTool `yaml:"tools"` // Array of tools (referenced or inline)
}

// DeepCopy returns a deep copy of the ToolkitSpec.
func (in *ToolkitSpec) DeepCopy() *ToolkitSpec {
	if in == nil {
		return nil
	}
	out := new(ToolkitSpec)
	if in.Tools != nil {
		out.Tools = make([]ToolkitTool, len(in.Tools))
		for i, v := range in.Tools {
			out.Tools[i] = *v.DeepCopy()
		}
	}
	return out
}
