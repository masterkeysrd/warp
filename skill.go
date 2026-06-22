package warp

import "path/filepath"

// SkillRenderOptions holds options for rendering skill instructions.
type SkillRenderOptions struct {
	Workspace *Workspace
	Project   *Project
	Agent     *Agent // The invoking agent
	Globals   map[string]any
}

// Render processes the skill's instructions as a template.
// It acts as a rich extension that injects Workspace, Project, and the invoking
// Agent into the generic Render.
func (s *Skill) Render(opts *SkillRenderOptions) (string, error) {
	if s == nil {
		return "", nil
	}
	if opts == nil {
		opts = &SkillRenderOptions{}
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

	// Inject invoking Agent derived from arguments
	if opts.Agent != nil {
		ta := TemplateAgent{
			Name:        opts.Agent.Metadata.Name,
			Description: opts.Agent.Metadata.Description,
			Dir:         opts.Agent.Directory,
			Path:        filepath.Join(opts.Agent.Directory, opts.Agent.Metadata.Name+".md"),
		}
		mergedGlobals["Agent"] = ta
	} else {
		mergedGlobals["Agent"] = TemplateAgent{}
	}

	renderOpts := &RenderOptions{
		Globals: mergedGlobals,
	}

	return Render(s, renderOpts)
}

// Skill is a warp resource that bundles expertise guidelines for a specific
// domain. An agent loads a skill's instructions to adopt its persona or
// follow its conventions.
type Skill struct {
	BaseResource `yaml:",inline"`
	// Spec holds the skill-specific configuration.
	Spec SkillSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Skill.
func (s *Skill) DeepCopy() *Skill {
	if s == nil {
		return nil
	}
	out := new(Skill)
	out.BaseResource = *s.BaseResource.DeepCopy()
	out.Spec = *s.Spec.DeepCopy()
	return out
}

// SkillSpec contains the configuration details for a Skill resource.
type SkillSpec struct {
	// Instructions is the expertise prompt populated from the Markdown body
	// of the file (below the closing front-matter delimiter).
	Instructions string `yaml:"instructions,omitempty"`
}

// DeepCopy returns a deep copy of the SkillSpec.
func (s *SkillSpec) DeepCopy() *SkillSpec {
	if s == nil {
		return nil
	}
	out := new(SkillSpec)
	*out = *s
	return out
}
