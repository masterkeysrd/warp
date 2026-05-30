package warp

import (
	"path/filepath"
)

// Project holds metadata for a single project directory discovered during
// workspace loading. Resources for the project are stored in the Registry
// under the project's slug namespace.
type Project struct {
	// Name is the slug identifier derived from the project's path.
	Name string

	// Path is the project's path relative to the workspace RootPath.
	Path string

	// RootPath is the absolute filesystem path of the workspace root.
	// Stored here so AbsPath() works without a back-pointer to Workspace.
	RootPath string

	// Context is the parsed AGENT.md resource for this project. Nil when no
	// AGENT.md was found.
	Context *Context
}

// DeepCopy returns a deep copy of the Project.
func (p *Project) DeepCopy() *Project {
	if p == nil {
		return nil
	}
	return &Project{
		Name:     p.Name,
		Path:     p.Path,
		RootPath: p.RootPath,
		Context:  p.Context.DeepCopy(),
	}
}

// AbsPath returns the absolute filesystem path to the project directory.
func (p *Project) AbsPath() string {
	return filepath.Join(p.RootPath, p.Path)
}
