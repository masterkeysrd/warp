package warp

import (
	"path/filepath"
)

// ContextRenderOptions holds options for rendering context instructions.
type ContextRenderOptions struct {
	Workspace *Workspace
	Project   *Project
	Globals   map[string]any
}

// Render processes the context's instructions as a template.
// It acts as a rich extension that injects Workspace and Project scope into the generic Render.
func (c *Context) Render(opts *ContextRenderOptions) (string, error) {
	if c == nil {
		return "", nil
	}
	if opts == nil {
		opts = &ContextRenderOptions{}
	}

	mergedGlobals := make(map[string]any)
	for k, v := range opts.Globals {
		mergedGlobals[k] = v
	}

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
	} else {
		mergedGlobals["Project"] = TemplateProject{}
		mergedGlobals["ProjectDir"] = ""
	}

	return Render(c, &RenderOptions{Globals: mergedGlobals})
}

// Context is a warp resource that defines the identity, rules, and instructions
// for any agent operating within a project directory. It is the authoritative
// entry point for the directory scope it lives in.
//
// A file named AGENT.md (case-insensitive) is automatically treated as a
// Context. If the file lacks YAML front-matter the loader infers the
// apiVersion, kind, and metadata.name fields automatically.
type Context struct {
	BaseResource `yaml:",inline"`
	// Spec holds the context-specific configuration.
	Spec ContextSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Context.
func (in *Context) DeepCopy() *Context {
	if in == nil {
		return nil
	}
	out := new(Context)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

// ContextSpec contains the configuration details for a Context resource.
type ContextSpec struct {
	// Instructions is the directive text populated from the Markdown body of
	// the file (below the closing front-matter delimiter).
	Instructions string `yaml:"instructions,omitempty"`
	// Resources is a list of paths to other Warp files to be explicitly
	// loaded into the context.
	Resources []string `yaml:"resources"`
}

// DeepCopy returns a deep copy of the ContextSpec.
func (in *ContextSpec) DeepCopy() *ContextSpec {
	if in == nil {
		return nil
	}
	out := new(ContextSpec)
	*out = *in
	if in.Resources != nil {
		out.Resources = make([]string, len(in.Resources))
		copy(out.Resources, in.Resources)
	}
	return out
}
