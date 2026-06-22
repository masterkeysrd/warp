package warp

import (
	"bytes"
	"fmt"
	"path/filepath"
	"text/template"
)

// WorkspaceRenderOptions holds options for rendering workspace instructions.
type WorkspaceRenderOptions struct {
	// Globals are custom runtime variables to inject into the template.
	Globals map[string]any
}

// TemplateWorkspace represents the workspace view model for templates.
type TemplateWorkspace struct {
	Dir  string
	Path string
}

// Render processes the workspace's instructions as a template.
// It supports both standard Go text/template syntax ({{.Workspace.Dir}}) and a
// convenient shorthand syntax ($WorkspaceDir, $WorkspacePath).
func (w *Workspace) Render(opts *WorkspaceRenderOptions) (string, error) {
	if w == nil || w.Def == nil {
		return "", nil
	}
	if opts == nil {
		opts = &WorkspaceRenderOptions{}
	}

	rawInstructions := w.Def.Spec.Instructions
	if rawInstructions == "" {
		return "", nil
	}

	// 1. Build the template data context
	data := make(map[string]any)

	// Merge globals
	for k, v := range opts.Globals {
		data[k] = v
	}

	// Determine absolute paths
	wsDir := w.RootPath
	wsPath := filepath.Join(w.RootPath, WorkspaceFileName)

	tw := TemplateWorkspace{
		Dir:  wsDir,
		Path: wsPath,
	}
	data["Workspace"] = tw

	// Add shorthands matching standard convention
	data["WorkspaceDir"] = tw.Dir
	data["WorkspacePath"] = tw.Path

	// 2. Preprocess shorthand $Var to {{.Var}}
	tmplStr := preprocessShorthand(rawInstructions)

	// 3. Parse and execute the template
	t, err := template.New("Workspace").Option("missingkey=zero").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// Workspace holds the immutable specification of a WARP workspace.
// All loaded resources and projects live in a Registry. Use DeepCopy to
// obtain a safe, independent snapshot of the spec.
type Workspace struct {
	// Def is the parsed WORKSPACE.md resource. Nil when Synthetic is true.
	Def *WorkspaceDef

	// RootPath is the absolute filesystem path to the workspace root directory.
	RootPath string

	// Synthetic is true when no WORKSPACE.md was found during discovery and
	// the workspace was inferred from the starting directory.
	Synthetic bool
}

// DeepCopy returns a deep copy of the Workspace spec, making the value safe
// to use as an immutable snapshot independent of the original.
func (w *Workspace) DeepCopy() *Workspace {
	if w == nil {
		return nil
	}
	return &Workspace{
		Def:       w.Def.DeepCopy(),
		RootPath:  w.RootPath,
		Synthetic: w.Synthetic,
	}
}

// WorkspaceDef is the parsed representation of a WORKSPACE.md resource.
type WorkspaceDef struct {
	BaseResource `yaml:",inline"`
	Spec         WorkspaceDefSpec `yaml:"spec"`
}

// ValidateBase validates the WorkspaceDef base fields.
func (w *WorkspaceDef) ValidateBase() error { return w.BaseResource.ValidateBase() }

// DeepCopy returns a deep copy of the WorkspaceDef.
func (w *WorkspaceDef) DeepCopy() *WorkspaceDef {
	if w == nil {
		return nil
	}
	out := new(WorkspaceDef)
	out.BaseResource = *w.BaseResource.DeepCopy()
	out.Spec = *w.Spec.DeepCopy()
	return out
}

// WorkspaceDefSpec contains configuration for a Workspace resource.
type WorkspaceDefSpec struct {
	Projects        []string          `yaml:"projects,flow"`
	DefaultProvider string            `yaml:"defaultProvider"`
	DefaultAgent    string            `yaml:"defaultAgent"`
	Plugins         []WorkspacePlugin `yaml:"plugins"`
	Policies        *Policies         `yaml:"policies,omitempty"`
	Instructions    string            `yaml:"instructions,omitempty"`
}

// WorkspacePlugin defines an external repository to load as a plugin.
type WorkspacePlugin struct {
	Source    string          `yaml:"source"`
	Version   string          `yaml:"version"`
	Namespace string          `yaml:"namespace"`
	Imports   *ResourceFilter `yaml:"imports"`
}

// DeepCopy returns a deep copy of the WorkspacePlugin.
func (in *WorkspacePlugin) DeepCopy() *WorkspacePlugin {
	if in == nil {
		return nil
	}
	out := new(WorkspacePlugin)
	*out = *in
	out.Imports = in.Imports.DeepCopy()
	return out
}

// DeepCopy returns a deep copy of the WorkspaceDefSpec.
func (in *WorkspaceDefSpec) DeepCopy() *WorkspaceDefSpec {
	if in == nil {
		return nil
	}
	out := new(WorkspaceDefSpec)
	*out = *in
	if in.Projects != nil {
		out.Projects = make([]string, len(in.Projects))
		copy(out.Projects, in.Projects)
	}
	if in.Plugins != nil {
		out.Plugins = make([]WorkspacePlugin, len(in.Plugins))
		for i, p := range in.Plugins {
			out.Plugins[i] = *p.DeepCopy()
		}
	}
	if in.Policies != nil {
		out.Policies = in.Policies.DeepCopy()
	}
	return out
}
