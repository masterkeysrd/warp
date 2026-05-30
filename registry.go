package warp

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// Resolver is the common interface satisfied by both Registry (workspace-root
// scope) and ScopedRegistry (project scope).
type Resolver interface {
	ResolveResource(ref string) (Resource, bool)
	ListResources(opts QueryOptions) []Resource
}

// QueryOptions controls how ListResources filters and deduplicates results.
type QueryOptions struct {
	// Kinds filters by resource kind. An empty slice matches all kinds.
	Kinds []string
	// Namespaces filters by namespace. An empty slice matches all namespaces.
	Namespaces []string
	// Effective applies shadowing: when true, only the highest-priority
	// namespace version of each short name is returned.
	Effective bool
}

// Registry is the base resource store, safe for concurrent use. Resources are
// keyed by their qualified name ("namespace/Kind/name"). Project-local
// resources are stored using the project slug as their namespace — the "local"
// constant is a virtual alias, never stored literally.
//
// Consumers interact either through the base Registry (workspace-root scope:
// workspace/user/system only) or through a ScopedRegistry obtained via
// Project(), which elevates a specific project namespace to the top priority.
type Registry struct {
	mu        sync.RWMutex
	resources map[string]Resource
	workspace *Workspace
	projects  map[string]*Project
	warnings  []string
}

// NewRegistry returns an empty Registry bound to the given workspace spec.
// ws may be nil for registries assembled outside the loading path.
func NewRegistry(ws *Workspace) *Registry {
	return &Registry{
		resources: make(map[string]Resource),
		projects:  make(map[string]*Project),
		workspace: ws,
	}
}

// AddProject registers project metadata in the registry.
func (r *Registry) AddProject(p *Project) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects[p.Name] = p
}

// ListProjects returns all registered projects in undefined order.
func (r *Registry) ListProjects() []*Project {
	r.mu.RLock()
	defer r.mu.RUnlock()
	projects := make([]*Project, 0, len(r.projects))
	for _, p := range r.projects {
		projects = append(projects, p)
	}
	return projects
}

// GetProject returns the project with the given slug, or (nil, false) when not found.
func (r *Registry) GetProject(slug string) (*Project, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.projects[slug]
	return p, ok
}

// ProjectFromPath returns the project whose absolute directory matches absPath,
// or (nil, false) when no project matches. This is the preferred way for the
// application layer to determine which project is "current" — callers should
// pass os.Getwd() or an equivalent path rather than letting the Registry
// inspect the process environment.
func (r *Registry) ProjectFromPath(absPath string) (*Project, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.projects {
		if filepath.Clean(p.AbsPath()) == filepath.Clean(absPath) {
			return p, true
		}
	}
	return nil, false
}

// WorkspaceSpec returns the immutable workspace specification.
func (r *Registry) WorkspaceSpec() *Workspace { return r.workspace }

// Warnings returns non-fatal issues collected during loading.
func (r *Registry) Warnings() []string { return r.warnings }

// Project returns a ScopedRegistry scoped to slug. Resolution methods on the
// returned value treat slug as the highest-priority "local" namespace, and a
// ref beginning with "local/" is transparently rewritten to "<slug>/".
func (r *Registry) Project(slug string) *ScopedRegistry {
	return &ScopedRegistry{base: r, projectSlug: slug}
}

// get returns a resource by exact qualified name. Thread-safe.
func (r *Registry) get(qualifiedName string) (Resource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.resources[qualifiedName]
	return v, ok
}

// Set stores a resource under the given qualified name. It overwrites any
// existing entry. Use this for programmatic registry construction in tests or
// custom providers. For loader paths, use the internal set() method instead.
func (r *Registry) Set(qualifiedName string, res Resource) {
	r.set(qualifiedName, res)
}

// set stores (or overwrites) a resource under its qualified name. Thread-safe.
func (r *Registry) set(qualifiedName string, res Resource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resources[qualifiedName] = res
}

// ─── Validation ─────────────────────────────────────────────────────────────

// Validate checks every resource for structural correctness and resolves Agent
// cross-references. Returns the first error encountered.
func (r *Registry) Validate() error {
	if r.workspace != nil && r.workspace.Def != nil {
		if err := r.workspace.Def.ValidateBase(); err != nil {
			return fmt.Errorf("workspace definition: %w", err)
		}
	}

	type validator interface{ ValidateBase() error }
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, res := range r.resources {
		if v, ok := res.(validator); ok {
			if err := v.ValidateBase(); err != nil {
				return fmt.Errorf("resource %s: %w", name, err)
			}
		}
		if agent, ok := res.(*Agent); ok {
			for _, skillRef := range agent.Spec.Skills {
				if !r.hasLocked(skillRef) {
					return fmt.Errorf("agent %s references missing skill: %s", name, skillRef)
				}
			}
			for _, cmdRef := range agent.Spec.Commands {
				if !r.hasLocked(cmdRef) {
					return fmt.Errorf("agent %s references missing command: %s", name, cmdRef)
				}
			}
		}
		if cmd, ok := res.(*Command); ok {
			for _, toolRef := range cmd.Spec.Tools {
				if !r.hasLocked(toolRef) {
					return fmt.Errorf("command %s references missing tool: %s", name, toolRef)
				}
			}
		}
	}
	return nil
}

// hasLocked reports whether ref resolves to any resource. Must be called with
// r.mu held for reading.
func (r *Registry) hasLocked(ref string) bool {
	if strings.Contains(ref, "/") {
		_, ok := r.resources[ref]
		return ok
	}
	for _, res := range r.resources {
		if res.GetName() == ref {
			return true
		}
	}
	return false
}

// ─── Base Registry — Resolver (workspace-root scope) ─────────────────────────────

// ResolveResource implements Resolver for the base Registry.
// Qualified refs ("namespace/Kind/name") are direct key lookups.
// Short names are resolved through [workspace, user, system]; project-specific
// namespaces are never returned, ensuring workspace-root isolation.
func (r *Registry) ResolveResource(ref string) (Resource, bool) {
	if strings.Contains(ref, "/") {
		return r.get(ref)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var best Resource
	bestPrio := -1
	for _, res := range r.resources {
		if res.GetName() != ref {
			continue
		}
		ns := res.GetNamespace()
		if !isStandardNamespace(ns) {
			continue // exclude project-specific slugs
		}
		if p := NamespacePriority(ns); p > bestPrio {
			best = res
			bestPrio = p
		}
	}
	return best, best != nil
}

// ListResources implements Resolver for the base Registry. Only resources from
// standard namespaces (workspace/user/system) are included.
func (r *Registry) ListResources(opts QueryOptions) []Resource {
	kindSet := stringSet(opts.Kinds)
	nsSet := stringSet(opts.Namespaces)
	r.mu.RLock()
	defer r.mu.RUnlock()
	var filtered []Resource
	for _, res := range r.resources {
		ns := res.GetNamespace()
		if !isStandardNamespace(ns) {
			continue
		}
		if len(kindSet) > 0 && !kindSet[string(res.GetKind())] {
			continue
		}
		if len(nsSet) > 0 && !nsSet[ns] {
			continue
		}
		filtered = append(filtered, res)
	}
	if !opts.Effective {
		return filtered
	}
	return deduplicateByName(filtered, NamespacePriority)
}

// isStandardNamespace reports whether ns is one of the three fixed global
// namespaces. "local" and project slugs are not standard namespaces.
func isStandardNamespace(ns string) bool {
	return ns == NamespaceWorkspace || ns == NamespaceUser || ns == NamespaceSystem
}

// ─── ScopedRegistry ──────────────────────────────────────────────────────────

// ScopedRegistry wraps a Registry with a project slug, implementing Resolver
// with that slug treated as the top-priority "local" namespace.
// Obtain one via Registry.Project(slug).
type ScopedRegistry struct {
	base        *Registry
	projectSlug string
}

// priorityFor returns the resolution priority for ns in this scope. The active
// project slug is ranked above NamespaceLocal (100) at 200.
func (s *ScopedRegistry) priorityFor(ns string) int {
	if ns == s.projectSlug {
		return 200
	}
	return NamespacePriority(ns)
}

// ResolveResource implements Resolver for ScopedRegistry.
//   - "local/<Kind>/<name>" is rewritten to "<projectSlug>/<Kind>/<name>".
//   - Other qualified refs ("namespace/Kind/name") are direct key lookups.
//   - Short names are resolved through [projectSlug, workspace, user, system].
func (s *ScopedRegistry) ResolveResource(ref string) (Resource, bool) {
	// Rewrite the local/ virtual alias to the concrete project slug.
	if strings.HasPrefix(ref, NamespaceLocal+"/") {
		ref = s.projectSlug + ref[len(NamespaceLocal):]
	}
	if strings.Contains(ref, "/") {
		return s.base.get(ref)
	}
	// Short-name: pick the highest-priority match across all namespaces.
	s.base.mu.RLock()
	defer s.base.mu.RUnlock()
	var best Resource
	bestPrio := -1
	for _, res := range s.base.resources {
		if res.GetName() != ref {
			continue
		}
		if p := s.priorityFor(res.GetNamespace()); p > bestPrio {
			best = res
			bestPrio = p
		}
	}
	return best, best != nil
}

// ListResources implements Resolver for ScopedRegistry. When opts.Effective is
// true, the active project namespace wins over all others for each short name.
func (s *ScopedRegistry) ListResources(opts QueryOptions) []Resource {
	kindSet := stringSet(opts.Kinds)
	nsSet := stringSet(opts.Namespaces)
	s.base.mu.RLock()
	defer s.base.mu.RUnlock()
	var filtered []Resource
	for _, res := range s.base.resources {
		if len(kindSet) > 0 && !kindSet[string(res.GetKind())] {
			continue
		}
		if len(nsSet) > 0 && !nsSet[res.GetNamespace()] {
			continue
		}
		filtered = append(filtered, res)
	}
	if !opts.Effective {
		return filtered
	}
	return deduplicateByName(filtered, s.priorityFor)
}

// SkillsForAgent returns the Skill resources available to the named agent
// within this project scope. Inheritance is resolved first via ResolveAgent,
// so the merged skill list from the full inheritance chain is used.
// If the merged agent declares a non-empty Skills list, only those referenced
// skills are returned. When the list is empty every skill visible in this
// project scope is returned.
func (s *ScopedRegistry) SkillsForAgent(agentName string) ([]Skill, error) {
	ag, err := s.ResolveAgent(agentName)
	if err != nil {
		return nil, err
	}

	if len(ag.Spec.Skills) == 0 {
		all := s.ListResources(QueryOptions{Kinds: []string{string(KindSkill)}, Effective: true})
		skills := make([]Skill, 0, len(all))
		for _, r := range all {
			if sk, ok := r.(*Skill); ok {
				skills = append(skills, *sk)
			}
		}
		return skills, nil
	}

	skills := make([]Skill, 0, len(ag.Spec.Skills))
	for _, ref := range ag.Spec.Skills {
		r, ok := s.ResolveResource(ref)
		if !ok {
			continue
		}
		if sk, ok := r.(*Skill); ok {
			skills = append(skills, *sk)
		}
	}
	return skills, nil
}

// ToolsForAgent returns the Tool resources available to the named agent within
// this project scope. Inheritance is resolved first via ResolveAgent, so the
// merged tool list from the full inheritance chain is used.
// If the merged agent declares a non-empty Tools list, only those referenced
// tools are returned. When the list is empty every tool visible in this
// project scope is returned.
func (s *ScopedRegistry) ToolsForAgent(agentName string) ([]*Tool, error) {
	ag, err := s.ResolveAgent(agentName)
	if err != nil {
		return nil, err
	}

	if len(ag.Spec.Tools) == 0 {
		all := s.ListResources(QueryOptions{Kinds: []string{string(KindTool)}, Effective: true})
		tools := make([]*Tool, 0, len(all))
		for _, r := range all {
			if t, ok := r.(*Tool); ok {
				tools = append(tools, t)
			}
		}
		return tools, nil
	}

	tools := make([]*Tool, 0, len(ag.Spec.Tools))
	for _, ref := range ag.Spec.Tools {
		r, ok := s.ResolveResource(ref)
		if !ok {
			continue
		}
		if t, ok := r.(*Tool); ok {
			tools = append(tools, t)
		}
	}
	return tools, nil
}

// ─── Agent Inheritance ─────────────────────────────────────────────────────────

// resolveAgentChain resolves an agent by ref using the given Resolver and
// recursively merges its inheritance chain. visited tracks qualified names
// already in the chain to detect cycles.
func resolveAgentChain(resolver Resolver, ref string, visited map[string]struct{}) (*Agent, error) {
	res, ok := resolver.ResolveResource(ref)
	if !ok {
		return nil, fmt.Errorf("agent %q not found", ref)
	}
	ag, ok := res.(*Agent)
	if !ok {
		return nil, fmt.Errorf("%q is not an Agent resource", ref)
	}

	qn := res.QualifiedName()
	if _, seen := visited[qn]; seen {
		return nil, fmt.Errorf("circular agent inheritance detected at %q", qn)
	}

	if ag.Spec.Extends == "" {
		return ag.DeepCopy(), nil
	}

	visited[qn] = struct{}{}

	parent, err := resolveAgentChain(resolver, ag.Spec.Extends, visited)
	if err != nil {
		return nil, fmt.Errorf("agent %q: %w", qn, err)
	}

	// Merge: parent is the base; append child lists and instructions after.
	parent.Spec.Skills = append(parent.Spec.Skills, ag.Spec.Skills...)
	parent.Spec.Tools = append(parent.Spec.Tools, ag.Spec.Tools...)
	parent.Spec.Commands = append(parent.Spec.Commands, ag.Spec.Commands...)
	switch {
	case parent.Spec.Instructions != "" && ag.Spec.Instructions != "":
		parent.Spec.Instructions = parent.Spec.Instructions + "\n\n" + ag.Spec.Instructions
	case ag.Spec.Instructions != "":
		parent.Spec.Instructions = ag.Spec.Instructions
	}
	return parent, nil
}

// ResolveAgent resolves an agent by ref, applying recursive inheritance merging.
// Returns the fully merged *Agent or an error if the ref is not found, is not
// an Agent, or if a circular inheritance chain is detected.
func (r *Registry) ResolveAgent(ref string) (*Agent, error) {
	return resolveAgentChain(r, ref, make(map[string]struct{}))
}

// ResolveAgent resolves an agent by ref within this project scope, applying
// recursive inheritance merging. The project slug is the highest-priority
// namespace during resolution of every step in the chain.
func (s *ScopedRegistry) ResolveAgent(ref string) (*Agent, error) {
	return resolveAgentChain(s, ref, make(map[string]struct{}))
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

// deduplicateByName returns one resource per short name, keeping the one whose
// namespace yields the highest value from prioFn.
func deduplicateByName(resources []Resource, prioFn func(string) int) []Resource {
	byName := make(map[string]Resource, len(resources))
	for _, res := range resources {
		existing, ok := byName[res.GetName()]
		if !ok || prioFn(res.GetNamespace()) > prioFn(existing.GetNamespace()) {
			byName[res.GetName()] = res
		}
	}
	out := make([]Resource, 0, len(byName))
	for _, res := range byName {
		out = append(out, res)
	}
	return out
}

func stringSet(in []string) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	s := make(map[string]bool, len(in))
	for _, v := range in {
		s[v] = true
	}
	return s
}
