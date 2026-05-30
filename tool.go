package warp

// Tool is a warp resource that describes a custom tool.
type Tool struct {
	BaseResource `yaml:",inline"`
	// Spec holds the tool-specific configuration.
	Spec ToolSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Tool.
func (in *Tool) DeepCopy() *Tool {
	if in == nil {
		return nil
	}
	out := new(Tool)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

// ToolSpec contains the configuration details for a Tool resource.
type ToolSpec struct {
	Name        string                 `yaml:"name,omitempty"` // Used only when defined inline in a Toolkit
	Command     []string               `yaml:"command"`        // Executable and static args (e.g., ["python", "script.py"])
	Description string                 `yaml:"description"`    // What the tool does (sent to the LLM)
	Env         map[string]string      `yaml:"env"`            // Environment variables injected into the process
	Parameters  map[string]interface{} `yaml:"parameters"`     // JSON Schema object defining arguments the LLM must pass
	Annotations *ToolAnnotation        `yaml:"annotations"`    // Safety profile for Tool Execution Security
}

// ToolAnnotation defines the safety profile for a tool.
type ToolAnnotation struct {
	IsOpenWorld  bool   `yaml:"isOpenWorld"`  // Interacts with external resources
	IsDangerous  bool   `yaml:"isDangerous"`  // Can perform destructive actions
	IsReadOnly   bool   `yaml:"isReadOnly"`   // Does not modify state
	IsIdempotent bool   `yaml:"isIdempotent"` // Safe to retry
	UserHint     string `yaml:"userHint"`     // Human-readable hint for approval prompts
}

// DeepCopy returns a deep copy of the ToolAnnotation.
func (in *ToolAnnotation) DeepCopy() *ToolAnnotation {
	if in == nil {
		return nil
	}
	out := new(ToolAnnotation)
	*out = *in
	return out
}

// DeepCopy returns a deep copy of the ToolSpec.
func (in *ToolSpec) DeepCopy() *ToolSpec {
	if in == nil {
		return nil
	}
	out := new(ToolSpec)
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
	if in.Parameters != nil {
		out.Parameters = make(map[string]interface{}, len(in.Parameters))
		for k, v := range in.Parameters {
			out.Parameters[k] = v
		}
	}
	out.Annotations = in.Annotations.DeepCopy()
	return out
}
