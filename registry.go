package warp

import (
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Resolver is the common interface satisfied by both Registry (workspace-root
// scope) and ScopedRegistry (project scope).
type Resolver interface {
	ResolveResource(ref string) (Resource, bool)
	ListResources(opts QueryOptions) []Resource
	ResolveModels(role ModelRole, explicit ...string) []string
	ResolveRulesForPath(targetPath string) []Rule
}

// QueryOptions controls how ListResources filters and deduplicates results.
type QueryOptions struct {
	// Kinds filters by resource kind. An empty slice matches all kinds.
	Kinds []Kind
	// Namespaces filters by namespace. An empty slice matches all namespaces.
	Namespaces []string
	// Effective applies shadowing: when true, only the highest-priority
	// namespace version of each short name is returned.
	Effective bool
	// Filter is an optional custom predicate function called for each candidate resource.
	// If set and it returns false, the resource is excluded from the results.
	Filter func(Resource) bool
}

// Registry is the base resource store, safe for concurrent use. Resources are
// keyed by their qualified name ("namespace/Kind/name"). Project-local
// resources are stored using the project slug as their namespace — the "local"
// constant is a virtual alias, never stored literally.
//
// Consumers interact either through the base Registry (workspace-root scope:
// workspace/user/system/plugins) or through a ScopedRegistry obtained via
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

// ListProjects returns all registered projects sorted by project name.
func (r *Registry) ListProjects() []*Project {
	r.mu.RLock()
	defer r.mu.RUnlock()
	projects := make([]*Project, 0, len(r.projects))
	for _, p := range r.projects {
		projects = append(projects, p)
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Name < projects[j].Name
	})
	return projects
}

// GetProject returns the project with the given slug, or (nil, false) when not found.
func (r *Registry) GetProject(slug string) (*Project, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.projects[slug]
	return p, ok
}

// ProjectFromPath returns the project whose directory contains absPath
// (matching the most specific nested project when multiple projects match),
// or (nil, false) when no project matches. This is the preferred way for the
// application layer to determine which project is "current" — callers should
// pass os.Getwd() or an equivalent path rather than letting the Registry
// inspect the process environment.
func (r *Registry) ProjectFromPath(absPath string) (*Project, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cleanTarget := filepath.Clean(absPath)
	var best *Project
	bestLen := -1
	for _, p := range r.projects {
		projAbs := filepath.Clean(p.AbsPath())
		if cleanTarget == projAbs || strings.HasPrefix(cleanTarget, projAbs+string(filepath.Separator)) {
			if len(projAbs) > bestLen || (len(projAbs) == bestLen && best != nil && p.Name < best.Name) {
				best = p
				bestLen = len(projAbs)
			}
		}
	}
	return best, best != nil
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

// Validate checks every resource for structural correctness and resolves
// cross-references. Returns the first error encountered.
func (r *Registry) Validate() error {
	if r.workspace != nil && r.workspace.Def != nil {
		if err := r.workspace.Def.ValidateBase(); err != nil {
			return fmt.Errorf("workspace definition: %w", err)
		}
	}

	var toolPolicy *ToolPolicies
	if r.workspace != nil && r.workspace.Def != nil && r.workspace.Def.Spec.Policies != nil {
		toolPolicy = r.workspace.Def.Spec.Policies.Tools
	}

	type validator interface{ ValidateBase() error }
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.workspace != nil && r.workspace.Def != nil {
		if da := r.workspace.Def.Spec.DefaultAgent; da != "" {
			if !r.hasKindLocked(NamespaceWorkspace, KindAgent, da) {
				return fmt.Errorf("workspace defaultAgent references missing agent: %s", da)
			}
		}
		if dp := r.workspace.Def.Spec.DefaultProvider; dp != "" && len(r.findProvidersLocked()) > 0 {
			if !r.hasProviderLocked(dp) {
				return fmt.Errorf("workspace defaultProvider references missing provider: %s", dp)
			}
		}
		if dm := r.workspace.Def.Spec.DefaultModels; dm != nil {
			if err := r.validateModelDefaultsLocked("workspace", dm); err != nil {
				return err
			}
		}
		for _, rRef := range r.workspace.Def.Spec.Rules {
			if hasGlobMeta(rRef) {
				continue
			}
			if !r.hasKindLocked(NamespaceWorkspace, KindRule, rRef) {
				return fmt.Errorf("workspace references missing rule: %s", rRef)
			}
		}
	}

	// Iterate in deterministic order so validation errors are reproducible.
	keys := make([]string, 0, len(r.resources))
	for k := range r.resources {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		res := r.resources[name]
		if v, ok := res.(validator); ok {
			if err := v.ValidateBase(); err != nil {
				return fmt.Errorf("resource %s: %w", name, err)
			}
		}
		ns := res.GetNamespace()
		if agent, ok := res.(*Agent); ok {
			if agent.Spec.Extends != "" {
				if !r.hasKindLocked(ns, KindAgent, agent.Spec.Extends) {
					return fmt.Errorf("agent %s extends missing agent: %s", name, agent.Spec.Extends)
				}
				if err := r.checkInheritanceCycleLocked(agent); err != nil {
					return err
				}
			}
			for _, mRef := range agent.Spec.Models {
				if m := r.findDeclaredModelLocked(mRef); m != nil && !m.Capabilities.SupportsCompletion() {
					return fmt.Errorf("agent %s: model %q does not support completion capability", name, mRef)
				}
			}
			if agent.Spec.Skills != nil {
				for _, skillRef := range *agent.Spec.Skills {
					if hasGlobMeta(skillRef) {
						continue
					}
					if !r.hasKindLocked(ns, KindSkill, skillRef) {
						return fmt.Errorf("agent %s references missing skill: %s", name, skillRef)
					}
				}
			}
			if agent.Spec.Commands != nil {
				for _, cmdRef := range *agent.Spec.Commands {
					if hasGlobMeta(cmdRef) {
						continue
					}
					if !r.hasKindLocked(ns, KindCommand, cmdRef) {
						return fmt.Errorf("agent %s references missing command: %s", name, cmdRef)
					}
				}
			}
			if agent.Spec.Subagents != nil {
				for _, saRef := range *agent.Spec.Subagents {
					if hasGlobMeta(saRef) {
						continue
					}
					if !r.hasKindLocked(ns, KindAgent, saRef) {
						return fmt.Errorf("agent %s references missing subagent: %s", name, saRef)
					}
				}
			}
			if agent.Spec.Rules != nil {
				for _, ruleRef := range *agent.Spec.Rules {
					if hasGlobMeta(ruleRef) {
						continue
					}
					if !r.hasKindLocked(ns, KindRule, ruleRef) {
						return fmt.Errorf("agent %s references missing rule: %s", name, ruleRef)
					}
				}
			}
		}
		if ctx, ok := res.(*Context); ok {
			for _, rRef := range ctx.Spec.Rules {
				if hasGlobMeta(rRef) {
					continue
				}
				if !r.hasKindLocked(ns, KindRule, rRef) {
					return fmt.Errorf("context %s references missing rule: %s", name, rRef)
				}
			}
		}
		if rule, ok := res.(*Rule); ok {
			if err := r.validateRuleLocked(name, rule); err != nil {
				return err
			}
		}
		if dec, ok := res.(*Decision); ok {
			if err := r.validateDecisionLocked(name, dec); err != nil {
				return err
			}
		}
		if mp, ok := res.(*ModelProvider); ok {
			if mp.Spec.DefaultModels != nil {
				if err := validateProviderDefaultModels(name, mp); err != nil {
					return err
				}
			}
		}
		if cmd, ok := res.(*Command); ok {
			for _, toolRef := range cmd.Spec.Tools {
				if hasGlobMeta(toolRef) {
					continue
				}
				if !r.hasKindLocked(ns, KindTool, toolRef) {
					return fmt.Errorf("command %s references missing tool: %s", name, toolRef)
				}
			}
		}
		if tk, ok := res.(*Toolkit); ok {
			for i, item := range tk.Spec.Tools {
				if item.Ref != "" {
					if !r.hasKindLocked(ns, KindTool, item.Ref) {
						return fmt.Errorf("toolkit %s tool[%d] references missing tool: %s", name, i, item.Ref)
					}
				} else {
					if item.Name == "" {
						return fmt.Errorf("toolkit %s tool[%d]: inline tool requires name", name, i)
					}
					if len(item.Command) == 0 {
						return fmt.Errorf("toolkit %s tool[%d] (%s): inline tool requires command", name, i, item.Name)
					}
				}
			}
		}
		if pl, ok := res.(*Plugin); ok {
			for _, exp := range pl.Spec.Exports {
				lower := strings.ToLower(exp)
				if strings.HasPrefix(lower, "workspace/") || strings.HasPrefix(lower, "context/") || strings.HasPrefix(lower, "plugin/") ||
					lower == "workspace" || lower == "context" || lower == "plugin" {
					return fmt.Errorf("plugin %s cannot export structural resource pattern %q", name, exp)
				}
			}
		}
		if tool, ok := res.(*Tool); ok && toolPolicy != nil {
			if err := checkToolPolicyLocked(name, tool, toolPolicy); err != nil {
				return fmt.Errorf("tool %s violates workspace policy: %w", name, err)
			}
		}
		if mcp, ok := res.(*MCP); ok {
			transport := mcp.Spec.Type
			if transport == "" {
				transport = "stdio"
			}
			switch transport {
			case "stdio":
				if len(mcp.Spec.Command) == 0 {
					return fmt.Errorf("mcp %s: command is required for stdio transport", name)
				}
			case "sse", "http", "streamable-http":
				if mcp.Spec.Endpoint == "" {
					return fmt.Errorf("mcp %s: endpoint is required for %s transport", name, transport)
				}
			default:
				return fmt.Errorf("mcp %s: unknown transport type %q", name, transport)
			}
		}
	}
	return nil
}

func (r *Registry) validateDecisionLocked(name string, dec *Decision) error {
	seenNames := make(map[string]bool, len(dec.Spec.Questions))
	for i, q := range dec.Spec.Questions {
		if q.Name == "" {
			return fmt.Errorf("decision %s question[%d]: name is required", name, i)
		}
		if seenNames[q.Name] {
			return fmt.Errorf("decision %s: duplicate question name %q", name, q.Name)
		}
		seenNames[q.Name] = true
		if q.Threshold != nil && (*q.Threshold < 0.0 || *q.Threshold > 1.0) {
			return fmt.Errorf("decision %s question %q: threshold must be between 0.0 and 1.0", name, q.Name)
		}
		if q.MinConfidence != nil && (*q.MinConfidence < 0.0 || *q.MinConfidence > 1.0) {
			return fmt.Errorf("decision %s question %q: minConfidence must be between 0.0 and 1.0", name, q.Name)
		}
		switch q.Type {
		case DecisionQuestionPredicate:
			// valid
		case DecisionQuestionChoice:
			if len(q.Options) < 2 {
				return fmt.Errorf("decision %s question %q: choice question requires at least 2 options", name, q.Name)
			}
			for j, opt := range q.Options {
				if opt.Value == "" {
					return fmt.Errorf("decision %s question %q option[%d]: value is required", name, q.Name, j)
				}
			}
		case DecisionQuestionScore:
			if len(q.Levels) < 2 {
				return fmt.Errorf("decision %s question %q: score question requires at least 2 levels", name, q.Name)
			}
			for j, lvl := range q.Levels {
				if lvl.Label == "" {
					return fmt.Errorf("decision %s question %q level[%d]: label is required", name, q.Name, j)
				}
			}
		default:
			return fmt.Errorf("decision %s question %q: invalid type %q (must be predicate, choice, or score)", name, q.Name, q.Type)
		}
	}
	for _, mRef := range dec.Spec.Models {
		if m := r.findDeclaredModelLocked(mRef); m != nil && !m.Capabilities.SupportsDecision() {
			return fmt.Errorf("decision %s: model %q does not support decision capability", name, mRef)
		}
	}
	return nil
}

func (r *Registry) validateRuleLocked(name string, rule *Rule) error {
	ns := rule.GetNamespace()
	if dec := rule.Spec.Triggers.Decision; dec != "" {
		if !r.hasKindLocked(ns, KindDecision, dec) {
			return fmt.Errorf("rule %s: trigger references missing decision: %s", name, dec)
		}
	}
	for _, tRef := range rule.Spec.Triggers.Tools {
		if hasGlobMeta(tRef) || tRef == "*" {
			continue
		}
		if !r.hasKindLocked(ns, KindTool, tRef) && !r.hasKindLocked(ns, KindToolkit, tRef) {
			return fmt.Errorf("rule %s: trigger references missing tool or toolkit: %s", name, tRef)
		}
	}
	return nil
}

func (r *Registry) validateModelDefaultsLocked(scope string, dm *ModelDefaults) error {
	hasProviders := len(r.findProvidersLocked()) > 0
	checkRef := func(role, ref string, checkCap func(*ProviderModelCapabilities) bool) error {
		if ref == "" {
			return nil
		}
		if hasProviders {
			if idx := strings.IndexByte(ref, '/'); idx != -1 {
				prov := ref[:idx]
				if !r.hasProviderLocked(prov) {
					return fmt.Errorf("%s defaultModels.%s references missing provider: %s", scope, role, prov)
				}
			}
		}
		if m := r.findDeclaredModelLocked(ref); m != nil && !checkCap(m.Capabilities) {
			return fmt.Errorf("%s defaultModels.%s %q does not support %s capability", scope, role, ref, role)
		}
		return nil
	}
	if err := checkRef("completion", dm.Completion, (*ProviderModelCapabilities).SupportsCompletion); err != nil {
		return err
	}
	if err := checkRef("decision", dm.Decision, (*ProviderModelCapabilities).SupportsDecision); err != nil {
		return err
	}
	if err := checkRef("embedding", dm.Embedding, (*ProviderModelCapabilities).SupportsEmbedding); err != nil {
		return err
	}
	return nil
}

func validateProviderDefaultModels(name string, mp *ModelProvider) error {
	dm := mp.Spec.DefaultModels
	if dm == nil || len(mp.Spec.Models) == 0 {
		return nil
	}
	findModel := func(ref string) *ProviderModel {
		for i := range mp.Spec.Models {
			m := &mp.Spec.Models[i]
			if m.ID == ref || m.Name == ref {
				return m
			}
		}
		return nil
	}
	if dm.Completion != "" {
		if m := findModel(dm.Completion); m != nil && !m.Capabilities.SupportsCompletion() {
			return fmt.Errorf("provider %s: defaultModels.completion %q does not support completion capability", name, dm.Completion)
		}
	}
	if dm.Decision != "" {
		if m := findModel(dm.Decision); m != nil && !m.Capabilities.SupportsDecision() {
			return fmt.Errorf("provider %s: defaultModels.decision %q does not support decision capability", name, dm.Decision)
		}
	}
	if dm.Embedding != "" {
		if m := findModel(dm.Embedding); m != nil && !m.Capabilities.SupportsEmbedding() {
			return fmt.Errorf("provider %s: defaultModels.embedding %q does not support embedding capability", name, dm.Embedding)
		}
	}
	return nil
}

func (r *Registry) findDeclaredModelLocked(ref string) *ProviderModel {
	if ref == "" {
		return nil
	}
	providers := r.findProvidersLocked()
	if len(providers) == 0 {
		return nil
	}
	if idx := strings.IndexByte(ref, '/'); idx != -1 {
		prov, modelID := ref[:idx], ref[idx+1:]
		for _, mp := range providers {
			if mp.Metadata.Name == prov || mp.Spec.Type == prov {
				for i := range mp.Spec.Models {
					m := &mp.Spec.Models[i]
					if m.ID == modelID || m.Name == modelID {
						return m
					}
				}
			}
		}
		return nil
	}
	for _, mp := range providers {
		for i := range mp.Spec.Models {
			m := &mp.Spec.Models[i]
			if m.ID == ref || m.Name == ref {
				return m
			}
		}
	}
	return nil
}

func (r *Registry) findProvidersLocked() []*ModelProvider {
	var out []*ModelProvider
	for _, res := range r.resources {
		if mp, ok := res.(*ModelProvider); ok {
			out = append(out, mp)
		}
	}
	return out
}

func (r *Registry) hasProviderLocked(ref string) bool {
	for _, mp := range r.findProvidersLocked() {
		if mp.Metadata.Name == ref || mp.Spec.Type == ref || mp.QualifiedName() == ref {
			return true
		}
	}
	return false
}

func (r *Registry) checkInheritanceCycleLocked(start *Agent) error {
	visited := make(map[string]struct{})
	curr := start
	for curr != nil && curr.Spec.Extends != "" {
		qn := curr.QualifiedName()
		if _, seen := visited[qn]; seen {
			return fmt.Errorf("circular agent inheritance detected at %q", qn)
		}
		visited[qn] = struct{}{}
		nextRes := r.resolveRefLocked(curr.GetNamespace(), KindAgent, curr.Spec.Extends)
		if nextRes == nil {
			break
		}
		nextAgent, ok := nextRes.(*Agent)
		if !ok {
			break
		}
		curr = nextAgent
	}
	return nil
}

// checkToolPolicyLocked evaluates a tool against the workspace tool policy.
func checkToolPolicyLocked(qualifiedName string, tool *Tool, policy *ToolPolicies) error {
	if policy.AllowDangerous != nil && !*policy.AllowDangerous {
		if tool.Spec.Annotations != nil && tool.Spec.Annotations.IsDangerous {
			return fmt.Errorf("dangerous tools are not allowed")
		}
	}
	if policy.AllowOpenWorld != nil && !*policy.AllowOpenWorld {
		if tool.Spec.Annotations != nil && tool.Spec.Annotations.IsOpenWorld {
			return fmt.Errorf("open world tools are not allowed")
		}
	}
	shortName := tool.Metadata.Name
	nsName := tool.GetNamespace() + "/" + shortName
	if len(policy.Include) > 0 {
		matched := false
		for _, pat := range policy.Include {
			if matchResourcePattern(pat, shortName, nsName, qualifiedName) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("not in include list")
		}
	}
	for _, pat := range policy.Exclude {
		if matchResourcePattern(pat, shortName, nsName, qualifiedName) {
			return fmt.Errorf("matches exclude pattern %q", pat)
		}
	}
	return nil
}

func matchResourcePattern(pat, shortName, nsName, qualifiedName string) bool {
	if m, _ := filepath.Match(pat, shortName); m {
		return true
	}
	if m, _ := filepath.Match(pat, nsName); m {
		return true
	}
	if m, _ := filepath.Match(pat, qualifiedName); m {
		return true
	}
	return false
}

// resolveRefLocked finds a resource of the given kind matching ref from the
// perspective of fromNamespace. Must be called with r.mu held for reading.
func (r *Registry) resolveRefLocked(fromNamespace string, kind Kind, ref string) Resource {
	if strings.HasPrefix(ref, NamespaceLocal+"/") {
		suffix := ref[len(NamespaceLocal):]
		if fromNamespace != "" {
			if res, ok := r.resources[fromNamespace+suffix]; ok && res.GetKind() == kind {
				return res
			}
		}
		if res, ok := r.resources[NamespaceWorkspace+suffix]; ok && res.GetKind() == kind {
			return res
		}
		return nil
	}
	if strings.Contains(ref, "/") {
		if res, ok := r.resources[ref]; ok && res.GetKind() == kind {
			return res
		}
		// Support relative file path references such as "skills/go.md" or "tools/ls.yaml"
		slug := slugifyPath(ref)
		baseSlug := slugifyPath(path.Base(ref))
		for _, res := range r.resources {
			if res.GetKind() == kind && (res.GetName() == slug || res.GetName() == baseSlug) {
				return res
			}
		}
		return nil
	}
	baseSlug := slugifyPath(ref)
	for _, res := range r.resources {
		if res.GetKind() == kind && (res.GetName() == ref || res.GetName() == baseSlug) {
			return res
		}
	}
	return nil
}

// hasKindLocked reports whether ref resolves to a resource of the expected kind.
// Must be called with r.mu held for reading.
func (r *Registry) hasKindLocked(fromNamespace string, kind Kind, ref string) bool {
	return r.resolveRefLocked(fromNamespace, kind, ref) != nil
}

// Resources returns all resources stored in the registry across all namespaces,
// sorted deterministically by qualified name.
func (r *Registry) Resources() []Resource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]Resource, 0, len(r.resources))
	for _, v := range r.resources {
		res = append(res, v)
	}
	sortResources(res)
	return res
}

// ─── Base Registry — Resolver (workspace-root scope) ─────────────────────────────

// ResolveResource implements Resolver for the base Registry.
// Qualified refs ("namespace/Kind/name") are direct key lookups.
// Short names are resolved through global and plugin namespaces; project-specific
// namespaces are never returned, ensuring workspace-root isolation.
func (r *Registry) ResolveResource(ref string) (Resource, bool) {
	if strings.Contains(ref, "/") {
		if res, ok := r.get(ref); ok {
			return res, true
		}
		// Fallback for relative file paths like "skills/go.md"
		r.mu.RLock()
		defer r.mu.RUnlock()
		return r.resolvePathFallbackLocked(ref, r.isAllowedInBaseScopeLocked, NamespacePriority)
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
		if !r.isAllowedInBaseScopeLocked(ns) {
			continue // exclude project-specific slugs
		}
		p := NamespacePriority(ns)
		if p > bestPrio || (p == bestPrio && best != nil && res.QualifiedName() < best.QualifiedName()) {
			best = res
			bestPrio = p
		}
	}
	return best, best != nil
}

func (r *Registry) resolvePathFallbackLocked(ref string, allowNS func(string) bool, prioFn func(string) int) (Resource, bool) {
	slug := slugifyPath(ref)
	baseSlug := slugifyPath(path.Base(ref))
	var best Resource
	bestPrio := -1
	for _, res := range r.resources {
		if res.GetName() != slug && res.GetName() != baseSlug {
			continue
		}
		ns := res.GetNamespace()
		if allowNS != nil && !allowNS(ns) {
			continue
		}
		p := prioFn(ns)
		if p > bestPrio || (p == bestPrio && best != nil && res.QualifiedName() < best.QualifiedName()) {
			best = res
			bestPrio = p
		}
	}
	return best, best != nil
}

// ListResources implements Resolver for the base Registry. Project-specific
// namespaces are excluded; standard and plugin namespaces are included.
func (r *Registry) ListResources(opts QueryOptions) []Resource {
	kindSet := stringSet(opts.Kinds)
	nsSet := stringSet(opts.Namespaces)
	r.mu.RLock()
	defer r.mu.RUnlock()
	var filtered []Resource
	for _, res := range r.resources {
		ns := res.GetNamespace()
		// If caller explicitly requested specific namespaces, honor them; otherwise exclude project slugs.
		if len(nsSet) == 0 && !r.isAllowedInBaseScopeLocked(ns) {
			continue
		}
		if len(kindSet) > 0 && !kindSet[res.GetKind()] {
			continue
		}
		if len(nsSet) > 0 && !nsSet[ns] {
			continue
		}
		if opts.Filter != nil && !opts.Filter(res) {
			continue
		}
		filtered = append(filtered, res)
	}
	if opts.Effective {
		filtered = deduplicateByName(filtered, NamespacePriority)
	}
	sortResources(filtered)
	return filtered
}

// isAllowedInBaseScopeLocked reports whether ns is visible at the base Registry
// (workspace-root) scope. Registered project namespaces and "local" are excluded.
// When no projects are registered (e.g. unit tests constructing a bare Registry
// with ad-hoc project names), only standard namespaces and registered workspace
// plugins are visible.
func (r *Registry) isAllowedInBaseScopeLocked(ns string) bool {
	if isStandardNamespace(ns) {
		return true
	}
	if ns == NamespaceLocal || ns == "" {
		return false
	}
	if _, isProj := r.projects[ns]; isProj {
		return false
	}
	if r.workspace != nil && r.workspace.Def != nil {
		for _, p := range r.workspace.Def.Spec.Plugins {
			pNS := p.Namespace
			if pNS == "" {
				pNS = inferPluginNamespace(p.Source)
			}
			if pNS == ns {
				return true
			}
		}
	}
	// If projects have been registered (real workspace load), any non-project
	// namespace is a plugin/custom provider namespace.
	return len(r.projects) > 0
}

func inferPluginNamespace(source string) string {
	if lastSlash := strings.LastIndex(source, "/"); lastSlash != -1 {
		return source[lastSlash+1:]
	}
	return source
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

func (s *ScopedRegistry) isAllowedNamespaceLocked(ns string) bool {
	if ns == s.projectSlug {
		return true
	}
	return s.base.isAllowedInBaseScopeLocked(ns)
}

// ResolveResource implements Resolver for ScopedRegistry.
//   - "local/<Kind>/<name>" is rewritten to "<projectSlug>/<Kind>/<name>".
//   - Other qualified refs ("namespace/Kind/name") are direct key lookups.
//   - Short names are resolved through [projectSlug, workspace, user, system, plugins].
func (s *ScopedRegistry) ResolveResource(ref string) (Resource, bool) {
	// Rewrite the local/ virtual alias to the concrete project slug.
	if strings.HasPrefix(ref, NamespaceLocal+"/") {
		suffix := ref[len(NamespaceLocal):]
		// Try project slug first.
		if r, ok := s.base.get(s.projectSlug + suffix); ok {
			return r, true
		}
		// Fallback to workspace-global namespace.
		if r, ok := s.base.get(NamespaceWorkspace + suffix); ok {
			return r, true
		}
		return nil, false
	}
	if strings.Contains(ref, "/") {
		if r, ok := s.base.get(ref); ok {
			return r, true
		}
		s.base.mu.RLock()
		defer s.base.mu.RUnlock()
		return s.base.resolvePathFallbackLocked(ref, s.isAllowedNamespaceLocked, s.priorityFor)
	}
	// Short-name: pick the highest-priority match across visible namespaces.
	s.base.mu.RLock()
	defer s.base.mu.RUnlock()
	var best Resource
	bestPrio := -1
	for _, res := range s.base.resources {
		if res.GetName() != ref {
			continue
		}
		ns := res.GetNamespace()
		if !s.isAllowedNamespaceLocked(ns) {
			continue
		}
		p := s.priorityFor(ns)
		if p > bestPrio || (p == bestPrio && best != nil && res.QualifiedName() < best.QualifiedName()) {
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
		ns := res.GetNamespace()
		if len(nsSet) == 0 && !s.isAllowedNamespaceLocked(ns) {
			continue
		}
		if len(kindSet) > 0 && !kindSet[res.GetKind()] {
			continue
		}
		if len(nsSet) > 0 && !nsSet[ns] {
			continue
		}
		if opts.Filter != nil && !opts.Filter(res) {
			continue
		}
		filtered = append(filtered, res)
	}
	if opts.Effective {
		filtered = deduplicateByName(filtered, s.priorityFor)
	}
	sortResources(filtered)
	return filtered
}

// SkillsForAgent returns the Skill resources available to the named agent
// within this project scope. Inheritance is resolved first via ResolveAgent,
// so the merged skill list from the full inheritance chain is used.
// If the merged agent declares a non-empty Skills list, only those referenced
// skills are returned. When the list is empty every skill visible in this
// project scope is returned.
func (s *ScopedRegistry) SkillsForAgent(agentName string) ([]Skill, error) {
	rag, err := s.ResolveAgent(agentName)
	if err != nil {
		return nil, err
	}
	return rag.Skills, nil
}

// ToolsForAgent returns the Tool resources available to the named agent within
// this project scope. Inheritance is resolved first via ResolveAgent, so the
// merged tool list from the full inheritance chain is used.
func (s *ScopedRegistry) ToolsForAgent(agentName string) ([]*Tool, error) {
	rag, err := s.ResolveAgent(agentName)
	if err != nil {
		return nil, err
	}
	return rag.Tools, nil
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

	parent.Metadata = *ag.Metadata.DeepCopy()
	parent.Namespace = ag.Namespace
	parent.Directory = ag.Directory

	if len(ag.Spec.Models) > 0 {
		parent.Spec.Models = make([]string, len(ag.Spec.Models))
		copy(parent.Spec.Models, ag.Spec.Models)
	}
	if ag.Spec.Temperature != nil {
		t := *ag.Spec.Temperature
		parent.Spec.Temperature = &t
	}
	if len(ag.Spec.Thinking) > 0 {
		parent.Spec.Thinking = make(map[string]any, len(ag.Spec.Thinking))
		maps.Copy(parent.Spec.Thinking, ag.Spec.Thinking)
	}
	if len(ag.Spec.Triggers) > 0 {
		parent.Spec.Triggers = make([]string, len(ag.Spec.Triggers))
		copy(parent.Spec.Triggers, ag.Spec.Triggers)
	}

	// Merge: parent is the base; append child lists and instructions after.
	if ag.Spec.Skills != nil {
		if parent.Spec.Skills == nil {
			s := make([]string, len(*ag.Spec.Skills))
			copy(s, *ag.Spec.Skills)
			parent.Spec.Skills = &s
		} else {
			*parent.Spec.Skills = append(*parent.Spec.Skills, *ag.Spec.Skills...)
		}
	}
	if ag.Spec.Commands != nil {
		if parent.Spec.Commands == nil {
			c := make([]string, len(*ag.Spec.Commands))
			copy(c, *ag.Spec.Commands)
			parent.Spec.Commands = &c
		} else {
			*parent.Spec.Commands = append(*parent.Spec.Commands, *ag.Spec.Commands...)
		}
	}
	if ag.Spec.Subagents != nil {
		if parent.Spec.Subagents == nil {
			sa := make([]string, len(*ag.Spec.Subagents))
			copy(sa, *ag.Spec.Subagents)
			parent.Spec.Subagents = &sa
		} else {
			*parent.Spec.Subagents = append(*parent.Spec.Subagents, *ag.Spec.Subagents...)
		}
	}
	if ag.Spec.Rules != nil {
		if parent.Spec.Rules == nil {
			r := make([]string, len(*ag.Spec.Rules))
			copy(r, *ag.Spec.Rules)
			parent.Spec.Rules = &r
		} else {
			*parent.Spec.Rules = append(*parent.Spec.Rules, *ag.Spec.Rules...)
		}
	}
	switch {
	case parent.Spec.Instructions != "" && ag.Spec.Instructions != "":
		parent.Spec.Instructions = parent.Spec.Instructions + "\n\n" + ag.Spec.Instructions
	case ag.Spec.Instructions != "":
		parent.Spec.Instructions = ag.Spec.Instructions
	}
	parent.Spec.Policies = mergePolicies(parent.Spec.Policies, ag.Spec.Policies)
	return parent, nil
}

// ResolveAgent resolves an agent by ref, applying recursive inheritance merging.
// Returns the fully merged *ResolvedAgent or an error if the ref is not found, is not
// an Agent, or if a circular inheritance chain is detected.
func (r *Registry) ResolveAgent(ref string) (*ResolvedAgent, error) {
	return resolveExecutableAgent(r, ref)
}

// ResolveAgent resolves an agent by ref within this project scope, applying
// recursive inheritance merging. The project slug is the highest-priority
// namespace during resolution of every step in the chain.
func (s *ScopedRegistry) ResolveAgent(ref string) (*ResolvedAgent, error) {
	return resolveExecutableAgent(s, ref)
}

// ResolveModels returns the prioritized list of model identifiers for the given
// capability role, following the WARP model resolution hierarchy:
//  1. Non-empty explicit model overrides (e.g., from Agent, Command, or Decision).
//  2. Workspace role-specific default (spec.defaultModels.<role>), falling back
//     to spec.defaultModel when role is ModelRoleCompletion.
//  3. Default Provider role-specific default (spec.defaultModels.<role>), falling
//     back to spec.defaultModel when role is ModelRoleCompletion.
//
// Any resolved model lacking a "<provider>/" prefix is automatically qualified
// with the workspace's defaultProvider when set.
func (r *Registry) ResolveModels(role ModelRole, explicit ...string) []string {
	return resolveModels(r, r, role, explicit)
}

// ResolveModels resolves models for the given role within this project scope.
func (s *ScopedRegistry) ResolveModels(role ModelRole, explicit ...string) []string {
	return resolveModels(s.base, s, role, explicit)
}

// ResolveRulesForPath returns all rules applicable to the given path within this registry scope,
// sorted by Rule priority descending, then by name ascending.
func (r *Registry) ResolveRulesForPath(targetPath string) []Rule {
	return resolveRulesForPath(r, targetPath)
}

// ResolveRulesForPath returns all rules applicable to the given path within this scoped registry,
// sorted by Rule priority descending, then by name ascending.
func (s *ScopedRegistry) ResolveRulesForPath(targetPath string) []Rule {
	return resolveRulesForPath(s, targetPath)
}

func resolveRulesForPath(resolver Resolver, targetPath string) []Rule {
	candidates := resolver.ListResources(QueryOptions{Kinds: []Kind{KindRule}, Effective: true})
	var matched []Rule
	for _, res := range candidates {
		if rl, ok := res.(*Rule); ok {
			if rl.Spec.Triggers.MatchesPath(targetPath) {
				matched = append(matched, *rl)
			}
		}
	}
	return deduplicateRules(matched)
}

func resolveModels(base *Registry, resolver Resolver, role ModelRole, explicit []string) []string {
	base.mu.RLock()
	var wsSpec WorkspaceDefSpec
	if base.workspace != nil && base.workspace.Def != nil {
		wsSpec = base.workspace.Def.Spec
	}
	base.mu.RUnlock()

	defaultProv := wsSpec.DefaultProvider
	qualify := func(model, prov string) string {
		if model == "" {
			return ""
		}
		if !strings.Contains(model, "/") && prov != "" && !strings.Contains(prov, "/") {
			return prov + "/" + model
		}
		return model
	}

	// 1. Explicit model overrides
	var out []string
	for _, m := range explicit {
		if m != "" {
			out = append(out, qualify(m, defaultProv))
		}
	}
	if len(out) > 0 {
		return out
	}

	// 2. Workspace defaults
	wsModel := wsSpec.DefaultModels.ForRole(role)
	if wsModel == "" && role == ModelRoleCompletion {
		wsModel = wsSpec.DefaultModel
	}
	if wsModel != "" {
		return []string{qualify(wsModel, defaultProv)}
	}

	// 3. Default Provider defaults
	if defaultProv != "" {
		mp := findProviderForResolution(base, resolver, defaultProv)
		if mp != nil {
			provModel := mp.Spec.DefaultModels.ForRole(role)
			if provModel == "" && role == ModelRoleCompletion {
				provModel = mp.Spec.DefaultModel
			}
			if provModel != "" {
				provPrefix := mp.Metadata.Name
				if provPrefix == "" {
					provPrefix = defaultProv
				}
				return []string{qualify(provModel, provPrefix)}
			}
		}
	}

	return nil
}

func findProviderForResolution(base *Registry, resolver Resolver, ref string) *ModelProvider {
	if res, ok := resolver.ResolveResource(ref); ok {
		if mp, ok := res.(*ModelProvider); ok {
			return mp
		}
	}
	for _, res := range resolver.ListResources(QueryOptions{Kinds: []Kind{KindModelProvider}, Effective: true}) {
		if mp, ok := res.(*ModelProvider); ok {
			if mp.Metadata.Name == ref || mp.Spec.Type == ref || mp.QualifiedName() == ref {
				return mp
			}
		}
	}
	base.mu.RLock()
	defer base.mu.RUnlock()
	for _, mp := range base.findProvidersLocked() {
		if mp.Metadata.Name == ref || mp.Spec.Type == ref || mp.QualifiedName() == ref {
			return mp
		}
	}
	return nil
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

func sortResources(resources []Resource) {
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].QualifiedName() < resources[j].QualifiedName()
	})
}

// deduplicateByName returns one resource per short name, keeping the one whose
// namespace yields the highest value from prioFn (breaking ties by QualifiedName).
func deduplicateByName(resources []Resource, prioFn func(string) int) []Resource {
	byName := make(map[string]Resource, len(resources))
	for _, res := range resources {
		existing, ok := byName[res.GetName()]
		if !ok {
			byName[res.GetName()] = res
			continue
		}
		pNew := prioFn(res.GetNamespace())
		pOld := prioFn(existing.GetNamespace())
		if pNew > pOld || (pNew == pOld && res.QualifiedName() < existing.QualifiedName()) {
			byName[res.GetName()] = res
		}
	}
	out := make([]Resource, 0, len(byName))
	for _, res := range byName {
		out = append(out, res)
	}
	sortResources(out)
	return out
}

func stringSet[T ~string](in []T) map[T]bool {
	if len(in) == 0 {
		return nil
	}
	s := make(map[T]bool, len(in))
	for _, v := range in {
		s[v] = true
	}
	return s
}

func resolveExecutableAgent(resolver Resolver, ref string) (*ResolvedAgent, error) {
	ag, err := resolveAgentChain(resolver, ref, make(map[string]struct{}))
	if err != nil {
		return nil, err
	}

	// 1. Resolve Skills
	var skills []Skill
	if ag.Spec.Skills == nil || containsWildcard(ag.Spec.Skills) {
		all := resolver.ListResources(QueryOptions{Kinds: []Kind{KindSkill}, Effective: true})
		for _, r := range all {
			if sk, ok := r.(*Skill); ok {
				skills = append(skills, *sk)
			}
		}
	} else {
		var allSkills []Resource
		for _, sRef := range *ag.Spec.Skills {
			if hasGlobMeta(sRef) {
				if allSkills == nil {
					allSkills = resolver.ListResources(QueryOptions{Kinds: []Kind{KindSkill}, Effective: false})
				}
				for _, r := range allSkills {
					if sk, ok := r.(*Skill); ok && matchRefPattern(resolver, sRef, r) {
						skills = append(skills, *sk)
					}
				}
				continue
			}
			r, ok := resolver.ResolveResource(sRef)
			if !ok {
				continue
			}
			if sk, ok := r.(*Skill); ok {
				skills = append(skills, *sk)
			}
		}
		skills = deduplicateSkills(skills)
	}

	// 2. Resolve Tools (including Toolkit expansions)
	tools := resolveToolsAndToolkits(resolver)

	// Apply tool policy filter
	if ag.Spec.Policies != nil && ag.Spec.Policies.Tools != nil {
		tools = filterTools(tools, ag.Spec.Policies.Tools)
	}

	// 3. Resolve Commands
	var commands []*Command
	if ag.Spec.Commands == nil || containsWildcard(ag.Spec.Commands) {
		all := resolver.ListResources(QueryOptions{Kinds: []Kind{KindCommand}, Effective: true})
		for _, r := range all {
			if c, ok := r.(*Command); ok {
				commands = append(commands, c)
			}
		}
	} else {
		var allCommands []Resource
		for _, cRef := range *ag.Spec.Commands {
			if hasGlobMeta(cRef) {
				if allCommands == nil {
					allCommands = resolver.ListResources(QueryOptions{Kinds: []Kind{KindCommand}, Effective: false})
				}
				for _, r := range allCommands {
					if c, ok := r.(*Command); ok && matchRefPattern(resolver, cRef, r) {
						commands = append(commands, c)
					}
				}
				continue
			}
			r, ok := resolver.ResolveResource(cRef)
			if !ok {
				continue
			}
			if c, ok := r.(*Command); ok {
				commands = append(commands, c)
			}
		}
		commands = deduplicateCommands(commands)
	}

	// 4. Resolve Subagents
	var subagents []*Agent
	if ag.Spec.Subagents == nil || containsWildcard(ag.Spec.Subagents) {
		all := resolver.ListResources(QueryOptions{Kinds: []Kind{KindAgent}, Effective: true})
		for _, r := range all {
			if a, ok := r.(*Agent); ok {
				if canBeTriggeredByAgent(a) {
					subagents = append(subagents, a)
				}
			}
		}
	} else {
		var allAgents []Resource
		for _, saRef := range *ag.Spec.Subagents {
			if hasGlobMeta(saRef) {
				if allAgents == nil {
					allAgents = resolver.ListResources(QueryOptions{Kinds: []Kind{KindAgent}, Effective: false})
				}
				for _, r := range allAgents {
					if a, ok := r.(*Agent); ok && canBeTriggeredByAgent(a) && matchRefPattern(resolver, saRef, r) {
						subagents = append(subagents, a)
					}
				}
				continue
			}
			r, ok := resolver.ResolveResource(saRef)
			if !ok {
				continue
			}
			if a, ok := r.(*Agent); ok {
				if canBeTriggeredByAgent(a) {
					subagents = append(subagents, a)
				}
			}
		}
		subagents = deduplicateAgents(subagents)
	}

	// 5. Resolve Rules
	var rules []Rule
	if ag.Spec.Rules == nil || containsWildcard(ag.Spec.Rules) {
		all := resolver.ListResources(QueryOptions{Kinds: []Kind{KindRule}, Effective: true})
		for _, r := range all {
			if rl, ok := r.(*Rule); ok {
				rules = append(rules, *rl)
			}
		}
	} else {
		var allRules []Resource
		for _, rRef := range *ag.Spec.Rules {
			if hasGlobMeta(rRef) {
				if allRules == nil {
					allRules = resolver.ListResources(QueryOptions{Kinds: []Kind{KindRule}, Effective: false})
				}
				for _, r := range allRules {
					if rl, ok := r.(*Rule); ok && matchRefPattern(resolver, rRef, r) {
						rules = append(rules, *rl)
					}
				}
				continue
			}
			r, ok := resolver.ResolveResource(rRef)
			if !ok {
				continue
			}
			if rl, ok := r.(*Rule); ok {
				rules = append(rules, *rl)
			}
		}
	}
	rules = deduplicateRules(rules)

	return &ResolvedAgent{
		Agent:     ag,
		Tools:     tools,
		Skills:    skills,
		Commands:  commands,
		Subagents: subagents,
		Rules:     rules,
	}, nil
}

func resolveToolsAndToolkits(resolver Resolver) []*Tool {
	var tools []*Tool
	seen := make(map[string]bool)
	addTool := func(t *Tool) {
		if t == nil {
			return
		}
		// Copy and ensure Metadata.Description falls back to Spec.Description if needed
		if t.Metadata.Description == "" && t.Spec.Description != "" {
			cp := t.DeepCopy()
			cp.Metadata.Description = cp.Spec.Description
			t = cp
		}
		qn := t.QualifiedName()
		if !seen[qn] {
			seen[qn] = true
			tools = append(tools, t)
		}
	}

	allTools := resolver.ListResources(QueryOptions{Kinds: []Kind{KindTool}, Effective: true})
	for _, r := range allTools {
		if t, ok := r.(*Tool); ok {
			addTool(t)
		}
	}

	allToolkits := resolver.ListResources(QueryOptions{Kinds: []Kind{KindToolkit}, Effective: true})
	for _, r := range allToolkits {
		tk, ok := r.(*Toolkit)
		if !ok {
			continue
		}
		for _, item := range tk.Spec.Tools {
			if item.Ref != "" {
				if refRes, ok := resolver.ResolveResource(item.Ref); ok {
					if t, ok := refRes.(*Tool); ok {
						addTool(t)
					}
				}
				continue
			}
			if item.Name != "" {
				desc := item.Description
				if desc == "" {
					desc = item.Instructions
				}
				inlineTool := &Tool{
					BaseResource: BaseResource{
						APIVersion: tk.APIVersion,
						Kind:       KindTool,
						Namespace:  tk.Namespace,
						Directory:  tk.Directory,
						Metadata: Metadata{
							Name:        item.Name,
							Description: desc,
						},
					},
					Spec: *item.ToolSpec.DeepCopy(),
				}
				addTool(inlineTool)
			}
		}
	}

	sort.Slice(tools, func(i, j int) bool {
		return tools[i].QualifiedName() < tools[j].QualifiedName()
	})
	return tools
}

func matchRefPattern(resolver Resolver, pat string, res Resource) bool {
	if scoped, ok := resolver.(*ScopedRegistry); ok && strings.HasPrefix(pat, NamespaceLocal+"/") {
		pat = scoped.projectSlug + pat[len(NamespaceLocal):]
	}
	shortName := res.GetName()
	nsName := res.GetNamespace() + "/" + shortName
	qualifiedName := res.QualifiedName()
	return matchResourcePattern(pat, shortName, nsName, qualifiedName)
}

func mergePolicies(parent, child *Policies) *Policies {
	if parent == nil && child == nil {
		return nil
	}
	if parent == nil {
		return child.DeepCopy()
	}
	if child == nil {
		return parent.DeepCopy()
	}

	merged := parent.DeepCopy()
	if child.Tools == nil {
		return merged
	}
	if merged.Tools == nil {
		merged.Tools = child.Tools.DeepCopy()
		return merged
	}

	// Merge tool policies: child overrides parent booleans, unions arrays
	if child.Tools.AllowDangerous != nil {
		ad := *child.Tools.AllowDangerous
		merged.Tools.AllowDangerous = &ad
	}
	if child.Tools.AllowOpenWorld != nil {
		aow := *child.Tools.AllowOpenWorld
		merged.Tools.AllowOpenWorld = &aow
	}

	// Union parent and child includes/excludes
	merged.Tools.Include = unionSlices(merged.Tools.Include, child.Tools.Include)
	merged.Tools.Exclude = unionSlices(merged.Tools.Exclude, child.Tools.Exclude)

	return merged
}

func unionSlices(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, val := range a {
		if !seen[val] {
			seen[val] = true
			out = append(out, val)
		}
	}
	for _, val := range b {
		if !seen[val] {
			seen[val] = true
			out = append(out, val)
		}
	}
	return out
}

func deduplicateSkills(in []Skill) []Skill {
	seen := make(map[string]bool)
	var out []Skill
	for _, s := range in {
		qn := s.QualifiedName()
		if !seen[qn] {
			seen[qn] = true
			out = append(out, s)
		}
	}
	return out
}

func deduplicateCommands(in []*Command) []*Command {
	seen := make(map[string]bool)
	var out []*Command
	for _, c := range in {
		qn := c.QualifiedName()
		if !seen[qn] {
			seen[qn] = true
			out = append(out, c)
		}
	}
	return out
}

func filterTools(tools []*Tool, policy *ToolPolicies) []*Tool {
	if policy == nil {
		return tools
	}
	var filtered []*Tool
	for _, tool := range tools {
		qn := tool.QualifiedName()
		if err := checkToolPolicyLocked(qn, tool, policy); err == nil {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func deduplicateAgents(in []*Agent) []*Agent {
	seen := make(map[string]bool)
	var out []*Agent
	for _, a := range in {
		qn := a.QualifiedName()
		if !seen[qn] {
			seen[qn] = true
			out = append(out, a)
		}
	}
	return out
}

func deduplicateRules(in []Rule) []Rule {
	seen := make(map[string]bool)
	var out []Rule
	for _, r := range in {
		qn := r.QualifiedName()
		if !seen[qn] {
			seen[qn] = true
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Spec.Priority != out[j].Spec.Priority {
			return out[i].Spec.Priority > out[j].Spec.Priority
		}
		return out[i].QualifiedName() < out[j].QualifiedName()
	})
	return out
}

func canBeTriggeredByAgent(a *Agent) bool {
	if len(a.Spec.Triggers) == 0 {
		return true
	}
	for _, t := range a.Spec.Triggers {
		if strings.ToLower(t) == "agent" {
			return true
		}
	}
	return false
}

func hasGlobMeta(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func containsWildcard(slice *[]string) bool {
	if slice == nil {
		return false
	}
	for _, v := range *slice {
		if v == "*" {
			return true
		}
	}
	return false
}
