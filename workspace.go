package warp

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
	Projects        []string `yaml:"projects"`
	DefaultProvider string   `yaml:"defaultProvider"`
	DefaultAgent    string   `yaml:"defaultAgent"`
	Plugins         []string `yaml:"plugins"`
	Instructions    string   `yaml:"instructions"`
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
		out.Plugins = make([]string, len(in.Plugins))
		copy(out.Plugins, in.Plugins)
	}
	return out
}
