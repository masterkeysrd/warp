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
	Projects        []string           `yaml:"projects"`
	DefaultProvider string             `yaml:"defaultProvider"`
	DefaultAgent    string             `yaml:"defaultAgent"`
	Plugins         []WorkspacePlugin  `yaml:"plugins"`
	Policies        *WorkspacePolicies `yaml:"policies,omitempty"`
	Instructions    string             `yaml:"instructions"`
}

// WorkspacePolicies defines workspace-level security and access policies.
type WorkspacePolicies struct {
	Tools *WorkspaceToolPolicies `yaml:"tools,omitempty"`
}

// DeepCopy returns a deep copy of the WorkspacePolicies.
func (in *WorkspacePolicies) DeepCopy() *WorkspacePolicies {
	if in == nil {
		return nil
	}
	out := new(WorkspacePolicies)
	out.Tools = in.Tools.DeepCopy()
	return out
}

// WorkspaceToolPolicies defines workspace-level restrictions for tool usage.
type WorkspaceToolPolicies struct {
	AllowDangerous *bool    `yaml:"allowDangerous,omitempty"`
	AllowOpenWorld *bool    `yaml:"allowOpenWorld,omitempty"`
	Include        []string `yaml:"include,omitempty"`
	Exclude        []string `yaml:"exclude,omitempty"`
}

// DeepCopy returns a deep copy of the WorkspaceToolPolicies.
func (in *WorkspaceToolPolicies) DeepCopy() *WorkspaceToolPolicies {
	if in == nil {
		return nil
	}
	out := new(WorkspaceToolPolicies)
	if in.AllowDangerous != nil {
		ad := *in.AllowDangerous
		out.AllowDangerous = &ad
	}
	if in.AllowOpenWorld != nil {
		aow := *in.AllowOpenWorld
		out.AllowOpenWorld = &aow
	}
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
