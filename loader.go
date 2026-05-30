package warp

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// DefaultAgentsDir is the subdirectory that contains Agent, Skill, and
	// Command resource files, both at the workspace root (global) and inside
	// each project directory (local).
	DefaultAgentsDir = ".agents"

	// WorkspaceFileName is the name of the file that defines the workspace
	// root authority.
	WorkspaceFileName = "WORKSPACE.md"

	// ContextFileName is the name of the file that defines a project's
	// identity and instructions.
	ContextFileName = "AGENT.md"

	// MarkdownExt is the file extension that the loader looks for when
	// walking the filesystem. Only files with this extension are parsed as
	// warp resources.
	MarkdownExt = ".md"
)

// ResourceSet contains non-project, non-workspace resources that can be
// merged into a loaded workspace.
type ResourceSet struct {
	Agents         map[string]*Agent
	Skills         map[string]*Skill
	Commands       map[string]*Command
	ModelProviders map[string]*ModelProvider
	Tools          map[string]*Tool
	MCPs           map[string]*MCP
	Toolkits       map[string]*Toolkit
}

// ResourceProvider loads workspace-global resources that should be merged into
// a workspace after filesystem discovery. Existing workspace resources keep
// precedence over provider resources.
type ResourceProvider interface {
	LoadResources() (*ResourceSet, error)
}

// FSResourceProvider loads resources from a filesystem rooted at a resource
// library directory. Workspace and Context resources are ignored.
type FSResourceProvider struct {
	fsys fs.FS
}

// NewFSResourceProvider returns a resource provider that loads from fsys.
func NewFSResourceProvider(fsys fs.FS) *FSResourceProvider {
	return &FSResourceProvider{fsys: fsys}
}

// LoadResources loads Agent, Skill, Command, ModelProvider, Tool, MCP, and
// Toolkit resources from the provider filesystem.
func (p *FSResourceProvider) LoadResources() (*ResourceSet, error) {
	if p == nil {
		return nil, fmt.Errorf("resource provider is required")
	}
	if p.fsys == nil {
		return nil, fmt.Errorf("resource filesystem is required")
	}

	resources := newResourceSet()
	if err := loadAgentsDir(
		p.fsys,
		resources.Agents,
		resources.Skills,
		resources.Commands,
		resources.ModelProviders,
		resources.Tools,
		resources.MCPs,
		resources.Toolkits,
	); err != nil {
		return nil, err
	}

	return resources, nil
}

func newResourceSet() *ResourceSet {
	return &ResourceSet{
		Agents:         make(map[string]*Agent),
		Skills:         make(map[string]*Skill),
		Commands:       make(map[string]*Command),
		ModelProviders: make(map[string]*ModelProvider),
		Tools:          make(map[string]*Tool),
		MCPs:           make(map[string]*MCP),
		Toolkits:       make(map[string]*Toolkit),
	}
}

// LoadDefault loads a workspace starting from the current working directory.
// It is equivalent to LoadWorkspace(".").
//
//	reg, err := warp.LoadDefault()
func LoadDefault(providers ...ResourceProvider) (*Registry, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	return LoadWorkspace(cwd, providers...)
}

// Load loads a workspace starting from the given directory.
//
//	reg, err := warp.Load("/my/project")
func Load(dir string, providers ...ResourceProvider) (*Registry, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve directory %q: %w", dir, err)
	}
	return LoadWorkspace(abs, providers...)
}

// LoadWorkspace is the primary entry point for the 3-phase WARP loading
// algorithm.
//
// Phase 1 — Workspace Discovery: climbs from startDir looking for a
// WORKSPACE.md file. If none is found, a synthetic workspace is created
// with its root set to startDir.
//
// Phase 2 — Project Mapping: determines the active project directories from
// the workspace's spec.projects field (["*"], explicit list, or default ["."]).
//
// Phase 3 — Contextual Loading: for each project directory, loads its
// AGENT.md (Context) and walks its .agents/ subdirectory for Agents, Skills,
// and Commands.
//
// Authority Rule: when the only project is "." (WORKSPACE_PATH == PROJECT_PATH)
// the root .agents/ directory is owned by the Project. The workspace-global
// library is left empty to prevent double-loading.
func LoadWorkspace(startDir string, providers ...ResourceProvider) (*Registry, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("resolve start directory %q: %w", startDir, err)
	}

	// Phase 1: discover workspace root.
	workspaceRoot, wsDef, err := findWorkspace(abs)
	if err != nil {
		return nil, err
	}

	ws := &Workspace{
		Def:       wsDef,
		RootPath:  workspaceRoot,
		Synthetic: wsDef == nil,
	}

	reg := NewRegistry(ws)

	// Phase 2: map project directories.
	projectPaths, err := mapProjects(workspaceRoot, wsDef)
	if err != nil {
		return nil, err
	}

	// Determine whether the Authority Rule applies:
	// if any project path is "." the workspace root is itself a project, so
	// WORKSPACE_PATH/.agents/ belongs to the project — skip global loading.
	flatRoot := slices.Contains(projectPaths, ".")

	// Phase 3a: load workspace-global resources from WORKSPACE_ROOT/.agents/
	// Only when at least one project is a sub-directory (Authority Rule).
	if !flatRoot {
		globalAgentsDir := filepath.Join(workspaceRoot, DefaultAgentsDir)
		if info, err := os.Stat(globalAgentsDir); err == nil && info.IsDir() {
			if err := loadAgentsDirIntoRegistry(reg, os.DirFS(globalAgentsDir), NamespaceWorkspace); err != nil {
				return nil, fmt.Errorf("workspace global agents dir: %w", err)
			}
		}
	}

	// Phase 3b: load each project.
	var projectNames []string
	for _, relPath := range projectPaths {
		projAbsPath := filepath.Join(workspaceRoot, relPath)
		slug := projectSlug(workspaceRoot, projAbsPath)
		projectNames = append(projectNames, slug)

		// Load Context (AGENT.md).
		ctx, warn := loadContext(projAbsPath)
		if warn != "" {
			reg.warnings = append(reg.warnings, warn)
		}

		proj := &Project{
			Name:     slug,
			Path:     relPath,
			RootPath: workspaceRoot,
			Context:  ctx,
		}
		reg.AddProject(proj)

		// Load project-local resources from PROJECT_DIR/.agents/
		projAgentsDir := filepath.Join(projAbsPath, DefaultAgentsDir)
		if info, err := os.Stat(projAgentsDir); err == nil && info.IsDir() {
			if err := loadAgentsDirIntoRegistry(reg, os.DirFS(projAgentsDir), slug); err != nil {
				return nil, fmt.Errorf("project %s agents dir: %w", slug, err)
			}
		}
	}

	if err := applyResourceProviders(reg, providers); err != nil {
		return nil, err
	}

	if ws.Synthetic {
		ws.Def = &WorkspaceDef{
			BaseResource: BaseResource{
				Kind:       KindWorkspace,
				APIVersion: APIVersion,
				Directory:  workspaceRoot,
				Metadata: Metadata{
					Name:        filepath.Base(workspaceRoot),
					DisplayName: filepath.Base(workspaceRoot),
					Description: fmt.Sprintf("Synthetic workspace inferred from directory %q with %d projects", workspaceRoot, len(projectNames)),
				},
			},
			Spec: WorkspaceDefSpec{
				Projects: projectNames,
			},
		}
		// Update registry workspace pointer now that Def is set.
		reg.workspace = ws
	}

	return reg, nil
}

func applyResourceProviders(reg *Registry, providers []ResourceProvider) error {
	for i, provider := range providers {
		if provider == nil {
			return fmt.Errorf("resource provider %d is nil", i)
		}
		resources, err := provider.LoadResources()
		if err != nil {
			return fmt.Errorf("load resources from provider %d: %w", i, err)
		}
		// Inject as system namespace; do not overwrite existing keys.
		for _, maps := range []map[string]interface{}{} {
			_ = maps // placeholder — handled below
		}
		injectResourceSetFallback(reg, resources)
	}
	return nil
}

// injectResourceSetFallback injects resources from a ResourceSet into reg
// using NamespaceSystem namespace. Existing registry keys are not overwritten.
func injectResourceSetFallback(reg *Registry, rs *ResourceSet) {
	inject := func(r Resource, ns string) {
		br := resourceBase(r)
		if br == nil {
			return
		}
		br.SetNamespace(ns)
		qn := r.QualifiedName()
		if _, exists := reg.get(qn); !exists {
			reg.set(qn, r)
		}
	}
	ns := NamespaceSystem
	for _, a := range rs.Agents {
		inject(a, ns)
	}
	for _, s := range rs.Skills {
		inject(s, ns)
	}
	for _, c := range rs.Commands {
		inject(c, ns)
	}
	for _, mp := range rs.ModelProviders {
		inject(mp, ns)
	}
	for _, t := range rs.Tools {
		inject(t, ns)
	}
	for _, m := range rs.MCPs {
		inject(m, ns)
	}
	for _, tk := range rs.Toolkits {
		inject(tk, ns)
	}
}

func resourceBase(r Resource) *BaseResource {
	type baser interface{ base() *BaseResource }
	switch v := r.(type) {
	case *Agent:
		return &v.BaseResource
	case *Skill:
		return &v.BaseResource
	case *Command:
		return &v.BaseResource
	case *ModelProvider:
		return &v.BaseResource
	case *Tool:
		return &v.BaseResource
	case *MCP:
		return &v.BaseResource
	case *Toolkit:
		return &v.BaseResource
	default:
		_ = baser(nil)
		return nil
	}
}

// loadAgentsDirIntoRegistry walks an agents directory and registers all
// resources into reg under the given namespace, using QualifiedName as key.
func loadAgentsDirIntoRegistry(reg *Registry, fsys fs.FS, ns string) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		ext := path.Ext(p)
		if d.IsDir() || (ext != MarkdownExt && ext != ".yaml" && ext != ".yml") {
			return nil
		}

		content, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("read file %s: %w", p, err)
		}

		res, err := Parse(p, string(content))
		if err != nil {
			return fmt.Errorf("parse file %s: %w", p, err)
		}

		switch res.Kind {
		case KindAgent, KindSkill, KindCommand, KindModelProvider, KindTool, KindMCP, KindToolkit:
			r, ok := res.Resource.(Resource)
			if !ok {
				return nil
			}
			br := resourceBase(r)
			if br == nil {
				return nil
			}
			br.Directory = path.Dir(p)
			br.SetNamespace(ns)
			reg.set(r.QualifiedName(), r)
		}
		return nil
	})
}

// findWorkspace climbs from startDir to the filesystem root looking for a
// WORKSPACE.md file with kind: Workspace. It returns the directory that
// contains it, the parsed WorkspaceDef, and any error.
//
// If no WORKSPACE.md is found, it returns startDir and a nil WorkspaceDef
// (indicating a synthetic workspace).
func findWorkspace(startDir string) (string, *WorkspaceDef, error) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, WorkspaceFileName)
		data, err := os.ReadFile(candidate) // #nosec G304 -- path is controlled by the loader
		if err == nil {
			res, parseErr := Parse(filepath.Base(candidate), string(data))
			if parseErr == nil && res.Kind == KindWorkspace {
				return dir, res.Resource.(*WorkspaceDef), nil
			}
			// File exists but is not a valid Workspace resource — skip.
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root; use synthetic workspace.
			return startDir, nil, nil
		}
		dir = parent
	}
}

// mapProjects resolves the set of project directories to load based on the
// workspace definition's spec.projects field.
//
// Returns relative paths (from workspaceRoot) for each project.
func mapProjects(workspaceRoot string, wsDef *WorkspaceDef) ([]string, error) {
	var projectsSpec []string
	if wsDef != nil {
		projectsSpec = wsDef.Spec.Projects
	}

	// Default to ["."] when no projects are specified.
	if len(projectsSpec) == 0 {
		return []string{"."}, nil
	}

	// Wildcard: discover all non-hidden immediate subdirectories.
	if len(projectsSpec) == 1 && projectsSpec[0] == "*" {
		return discoverSubdirs(workspaceRoot)
	}

	// Explicit list: validate that every path exists.
	for _, rel := range projectsSpec {
		absPath := filepath.Join(workspaceRoot, rel)
		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("project path %q does not exist or is not a directory", rel)
		}
	}
	return projectsSpec, nil
}

// discoverSubdirs returns the relative paths of all immediate non-hidden
// subdirectories of root. Directories whose names begin with "." are skipped.
func discoverSubdirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read workspace root %q: %w", root, err)
	}

	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue // skip hidden directories (.git, .github, .vscode, etc.)
		}
		dirs = append(dirs, name)
	}
	return dirs, nil
}

// loadContext looks for an AGENT.md file in the given project directory and
// returns the parsed Context resource. Returns nil when the file is absent.
// The second return value carries any non-fatal warning.
func loadContext(projectDir string) (*Context, string) {
	// Search for AGENT.md case-insensitively by reading the directory.
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, ""
	}

	var candidates []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), ContextFileName) {
			candidates = append(candidates, filepath.Join(projectDir, e.Name()))
		}
	}

	if len(candidates) == 0 {
		return nil, ""
	}

	var warn string
	if len(candidates) > 1 {
		warn = fmt.Sprintf("project directory %q has multiple context files; using %q", projectDir, candidates[0])
	}

	data, err := os.ReadFile(candidates[0]) // #nosec G304
	if err != nil {
		return nil, fmt.Sprintf("read context file %q: %v", candidates[0], err)
	}

	res, err := Parse(filepath.Base(candidates[0]), string(data))
	if err != nil || res.Kind != KindContext {
		return nil, ""
	}

	ctx := res.Resource.(*Context)
	ctx.Directory = projectDir
	return ctx, warn
}

// loadAgentsDir walks an agents directory (provided as an fs.FS rooted at the
// agents dir itself) and registers every Agent, Skill, Command, Provider, Tool,
// MCP, and Toolkit file found.
// Map keys are file paths relative to the agents directory root.
func loadAgentsDir(fsys fs.FS,
	agents map[string]*Agent,
	skills map[string]*Skill,
	commands map[string]*Command,
	providers map[string]*ModelProvider,
	tools map[string]*Tool,
	mcps map[string]*MCP,
	toolkits map[string]*Toolkit,
) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		ext := path.Ext(p)
		if d.IsDir() || (ext != MarkdownExt && ext != ".yaml" && ext != ".yml") {
			return nil
		}

		content, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("read file %s: %w", p, err)
		}

		res, err := Parse(p, string(content))
		if err != nil {
			return fmt.Errorf("parse file %s: %w", p, err)
		}

		dir := path.Dir(p)

		switch res.Kind {
		case KindAgent:
			a := res.Resource.(*Agent)
			a.Directory = dir
			agents[p] = a
		case KindSkill:
			s := res.Resource.(*Skill)
			s.Directory = dir
			skills[p] = s
		case KindCommand:
			c := res.Resource.(*Command)
			c.Directory = dir
			commands[p] = c
		case KindModelProvider:
			mp := res.Resource.(*ModelProvider)
			mp.Directory = dir
			providers[p] = mp
		case KindTool:
			t := res.Resource.(*Tool)
			t.Directory = dir
			tools[p] = t
		case KindMCP:
			m := res.Resource.(*MCP)
			m.Directory = dir
			mcps[p] = m
		case KindToolkit:
			tk := res.Resource.(*Toolkit)
			tk.Directory = dir
			toolkits[p] = tk
		}
		// Other kinds (Workspace, Context) found inside an agents dir are
		// silently ignored.
		return nil
	})
}

// projectSlug derives the slug name for a project directory.
//
//   - When the project is at the workspace root ("."), the slug is the base
//     name of the workspace root directory.
//   - Otherwise, the slug is the slugified path of the project directory
//     relative to the workspace root.
func projectSlug(workspaceRoot, projectAbsPath string) string {
	rel, err := filepath.Rel(workspaceRoot, projectAbsPath)
	if err != nil || rel == "." {
		return strings.ToLower(filepath.Base(workspaceRoot))
	}
	return slugifyPath(rel)
}
