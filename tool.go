package warp

import (
	"fmt"
	"maps"
	"path/filepath"
)

// Tool is a warp resource that describes a custom or host-native tool.
type Tool struct {
	BaseResource `yaml:",inline"`
	// Spec holds the tool-specific configuration.
	Spec ToolSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Tool.
func (t *Tool) DeepCopy() *Tool {
	if t == nil {
		return nil
	}
	out := new(Tool)
	out.BaseResource = *t.BaseResource.DeepCopy()
	out.Spec = *t.Spec.DeepCopy()
	return out
}

// ToolRenderOptions holds options for rendering tool instructions or command arguments.
type ToolRenderOptions struct {
	Workspace *Workspace
	Project   *Project
	Agent     *Agent
	Input     map[string]any // Tool call arguments (keyed by inputSchema property names)
	Globals   map[string]any
}

func (t *Tool) buildGlobals(opts *ToolRenderOptions) map[string]any {
	if opts == nil {
		opts = &ToolRenderOptions{}
	}
	mergedGlobals := make(map[string]any)
	maps.Copy(mergedGlobals, opts.Globals)
	maps.Copy(mergedGlobals, opts.Input)
	if opts.Input != nil {
		mergedGlobals["Input"] = opts.Input
	}

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

	if opts.Agent != nil {
		ta := TemplateAgent{
			Name:        opts.Agent.Metadata.Name,
			Description: opts.Agent.Metadata.Description,
			Models:      opts.Agent.Spec.Models,
			Dir:         opts.Agent.Directory,
			Path:        opts.Agent.GetPath(),
		}
		mergedGlobals["Agent"] = ta
	} else {
		mergedGlobals["Agent"] = TemplateAgent{}
	}

	return mergedGlobals
}

// Render processes the tool's instructions as a template.
// It injects Workspace, Project scope, invoking Agent, and Input arguments into the generic Render.
func (t *Tool) Render(opts *ToolRenderOptions) (string, error) {
	if t == nil {
		return "", nil
	}
	renderOpts := &RenderOptions{
		Globals: t.buildGlobals(opts),
	}
	return Render(t, renderOpts)
}

// RenderCommand renders each element in ToolSpec.Command as a template using the tool's render context.
func (t *Tool) RenderCommand(opts *ToolRenderOptions) ([]string, error) {
	if t == nil || len(t.Spec.Command) == 0 {
		return nil, nil
	}
	renderOpts := &RenderOptions{
		Globals: t.buildGlobals(opts),
	}
	data, err := buildTemplateData(t, renderOpts)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(t.Spec.Command))
	for i, cmdPart := range t.Spec.Command {
		rendered, err := renderTemplate(fmt.Sprintf("%s-command-%d", t.GetName(), i), cmdPart, data)
		if err != nil {
			return nil, fmt.Errorf("failed to render tool command [%d] %q: %w", i, cmdPart, err)
		}
		out[i] = rendered
	}
	return out, nil
}

// ToolSpec contains the configuration details for a Tool resource.
type ToolSpec struct {
	// Instructions is the detailed description of what the tool does, populated from the Markdown body.
	Instructions string            `yaml:"instructions,omitempty"`
	Command      []string          `yaml:"command,omitempty" json:"command,omitempty"` // Executable and static args (e.g., ["python", "script.py"]). Optional for built-in / host-native tools.
	Env          map[string]string `yaml:"env,omitempty"`                              // Environment variables injected into the process
	InputSchema  map[string]any    `yaml:"inputSchema,omitempty"`                      // JSON Schema object defining arguments the LLM must pass
	OutputSchema map[string]any    `yaml:"outputSchema,omitempty"`                     // JSON Schema object defining the tool's output
	Annotations  *ToolAnnotation   `yaml:"annotations,omitempty"`                      // Safety profile for Tool Execution Security
}

// ToolAnnotation defines the safety profile for a tool.
type ToolAnnotation struct {
	IsOpenWorld  bool   `yaml:"isOpenWorld,omitempty"`  // Interacts with external resources
	IsDangerous  bool   `yaml:"isDangerous,omitempty"`  // Can perform destructive actions
	IsReadOnly   bool   `yaml:"isReadOnly,omitempty"`   // Does not modify state
	IsIdempotent bool   `yaml:"isIdempotent,omitempty"` // Safe to retry
	UserHint     string `yaml:"userHint,omitempty"`     // Human-readable hint for approval prompts
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
		maps.Copy(out.Env, in.Env)
	}
	if in.InputSchema != nil {
		out.InputSchema = make(map[string]any, len(in.InputSchema))
		maps.Copy(out.InputSchema, in.InputSchema)
	}
	if in.OutputSchema != nil {
		out.OutputSchema = make(map[string]any, len(in.OutputSchema))
		maps.Copy(out.OutputSchema, in.OutputSchema)
	}
	out.Annotations = in.Annotations.DeepCopy()
	return out
}
