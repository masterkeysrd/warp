package warp

import (
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Resolver is the common interface satisfied by both Registry (workspace-root
// scope) and ScopedRegistry (project scope).
type Resolver interface {
	ResolveResource(ref string) (Resource, bool)
	ListResources(opts QueryOptions) []Resource
	ResolveModels(role ModelRole, explicit ...string) []ResolvedModel
	ResolveModel(role ModelRole, explicit ...string) (*ResolvedModel, bool)
	ResolveRulesForPath(targetPath string) []Rule
	ResolveRulesForTool(toolName string) []Rule
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

type mcpToolEntry struct {
	rawTools   []*Tool
	activeKeys []string
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
	providers []ResourceProvider
	mcpTools  map[string]mcpToolEntry
}

// NewRegistry returns an empty Registry bound to the given workspace spec.
// ws may be nil for registries assembled outside the loading path.
func NewRegistry(ws *Workspace) *Registry {
	return &Registry{
		resources: make(map[string]Resource),
		projects:  make(map[string]*Project),
		workspace: ws,
		mcpTools:  make(map[string]mcpToolEntry),
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

// ResourceStats records the number of added, updated, removed, and unchanged resources.
type ResourceStats struct {
	Added     int `json:"added"`
	Updated   int `json:"updated"`
	Removed   int `json:"removed"`
	Unchanged int `json:"unchanged"`
}

// HasChanges reports whether any resources were added, updated, or removed.
func (s ResourceStats) HasChanges() bool {
	return s.Added > 0 || s.Updated > 0 || s.Removed > 0
}

// ReloadStats contains total resource counts and a breakdown by resource Kind.
type ReloadStats struct {
	Total  ResourceStats          `json:"total"`
	ByKind map[Kind]ResourceStats `json:"byKind"`
}

// ForKind returns the resource change stats for a specific kind.
func (s *ReloadStats) ForKind(kind Kind) ResourceStats {
	if s == nil || s.ByKind == nil {
		return ResourceStats{}
	}
	return s.ByKind[kind]
}

// HasChanges reports whether any resources were added, updated, or removed across all kinds.
func (s *ReloadStats) HasChanges() bool {
	if s == nil {
		return false
	}
	return s.Total.HasChanges()
}

// WorkspaceSpec returns the workspace specification. Thread-safe.
func (r *Registry) WorkspaceSpec() *Workspace {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.workspace
}

// Warnings returns non-fatal issues collected during loading. Thread-safe.
func (r *Registry) Warnings() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.warnings)
}

// Providers returns a copy of the resource providers configured on this registry. Thread-safe.
func (r *Registry) Providers() []ResourceProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.providers)
}

// SetProviders updates the resource providers for this registry. Thread-safe.
func (r *Registry) SetProviders(providers ...ResourceProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = slices.Clone(providers)
}

// Delete removes a resource by its qualified name. Returns true if the resource
// existed and was removed, false otherwise. Thread-safe.
func (r *Registry) Delete(qualifiedName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.resources[qualifiedName]; ok {
		delete(r.resources, qualifiedName)
		return true
	}
	return false
}

// Reload re-reads the workspace from disk and updates the registry in-place.
// It returns ReloadStats detailing added, updated, removed, and unchanged resources,
// both in total and broken down by Kind.
func (r *Registry) Reload() (*ReloadStats, error) {
	if r == nil {
		return nil, fmt.Errorf("registry is nil")
	}

	r.mu.RLock()
	if r.workspace == nil || r.workspace.RootPath == "" {
		r.mu.RUnlock()
		return nil, fmt.Errorf("cannot reload registry: workspace root path is not set")
	}
	rootPath := r.workspace.RootPath
	providers := slices.Clone(r.providers)
	r.mu.RUnlock()

	newReg, err := LoadWorkspace(rootPath, providers...)
	if err != nil {
		return nil, fmt.Errorf("reload workspace: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Re-apply existing r.mcpTools for any MCP resource that still exists
	newMCPTools := make(map[string]mcpToolEntry)
	for mcpQN, entry := range r.mcpTools {
		res, ok := newReg.resources[mcpQN]
		if !ok {
			continue
		}
		mcp, ok := res.(*MCP)
		if !ok {
			continue
		}
		activeTools, activeKeys := materializeMCPTools(mcp, entry.rawTools)
		for _, t := range activeTools {
			newReg.resources[t.QualifiedName()] = t
		}
		newMCPTools[mcpQN] = mcpToolEntry{
			rawTools:   entry.rawTools,
			activeKeys: activeKeys,
		}
	}
	r.mcpTools = newMCPTools

	stats := computeReloadStats(r.resources, newReg.resources)

	r.resources = newReg.resources
	r.workspace = newReg.workspace
	r.projects = newReg.projects
	r.warnings = newReg.warnings
	r.providers = newReg.providers

	return stats, nil
}

// computeReloadStats compares the old and new resource maps and computes the changes.
func computeReloadStats(oldResources, newResources map[string]Resource) *ReloadStats {
	stats := &ReloadStats{
		ByKind: make(map[Kind]ResourceStats),
	}

	for k, newR := range newResources {
		kind := newR.GetKind()
		kStats := stats.ByKind[kind]

		oldR, exists := oldResources[k]
		if !exists {
			stats.Total.Added++
			kStats.Added++
		} else if !reflect.DeepEqual(oldR, newR) {
			stats.Total.Updated++
			kStats.Updated++
		} else {
			stats.Total.Unchanged++
			kStats.Unchanged++
		}
		stats.ByKind[kind] = kStats
	}

	for k, oldR := range oldResources {
		if _, exists := newResources[k]; !exists {
			kind := oldR.GetKind()
			kStats := stats.ByKind[kind]

			stats.Total.Removed++
			kStats.Removed++

			stats.ByKind[kind] = kStats
		}
	}

	return stats
}

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
		if r.workspace.Def.Spec.ModelAliases != nil {
			for k, v := range r.workspace.Def.Spec.ModelAliases {
				if normalizeAliasName(k) == "" {
					return fmt.Errorf("workspace modelAliases: alias key cannot be empty")
				}
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("workspace modelAliases %q: target model cannot be empty", k)
				}
			}
		}
		if dm := r.workspace.Def.Spec.DefaultModels; dm != nil {
			if err := r.validateModelDefaultsLocked("workspace", dm); err != nil {
				return err
			}
		}
		if r.workspace.Def.Spec.Policies != nil && r.workspace.Def.Spec.Policies.Tools != nil {
			if err := validateToolPolicies(r.workspace.Def.Spec.Policies.Tools, "workspace policies.tools"); err != nil {
				return err
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
			if agent.Spec.Policies != nil && agent.Spec.Policies.Tools != nil {
				if err := validateToolPolicies(agent.Spec.Policies.Tools, fmt.Sprintf("agent %s policies.tools", name)); err != nil {
					return err
				}
			}
			if agent.Spec.Extends != "" {
				if !r.hasKindLocked(ns, KindAgent, agent.Spec.Extends) {
					return fmt.Errorf("agent %s extends missing agent: %s", name, agent.Spec.Extends)
				}
				if err := r.checkInheritanceCycleLocked(agent); err != nil {
					return err
				}
			}
			for _, mRef := range agent.Spec.Models {
				if len(r.findProvidersLocked()) > 0 {
					if _, _, isAlias := isAliasRef(mRef); isAlias {
						if err := r.validateAliasModelRefLocked(fmt.Sprintf("agent %s", name), mRef, ModelRoleCompletion); err != nil {
							return err
						}
					}
				}
				if m := r.findDeclaredModelForRoleLocked(mRef, ModelRoleCompletion); m != nil && !m.Capabilities.SupportsCompletion() {
					return fmt.Errorf("agent %s: model %q does not support completion capability", name, mRef)
				}
			}
			if agent.Spec.Thinking != nil {
				switch agent.Spec.Thinking.Type {
				case ThinkingStrategyToggle, ThinkingStrategyEffort, ThinkingStrategyBudget, ThinkingStrategyAdaptive:
				default:
					return fmt.Errorf("agent %s: invalid thinking type %q (must be toggle, effort, budget, or adaptive)", name, agent.Spec.Thinking.Type)
				}
				if agent.Spec.Thinking.BudgetTokens < 0 {
					return fmt.Errorf("agent %s: thinking budgetTokens cannot be negative (%d)", name, agent.Spec.Thinking.BudgetTokens)
				}
				for _, mRef := range agent.Spec.Models {
					m := r.findDeclaredModelForRoleLocked(mRef, ModelRoleCompletion)
					if m == nil || m.Capabilities == nil || len(m.Capabilities.Thinking) == 0 {
						continue
					}
					var matchingCap *ProviderModelThinkingCapability
					for i := range m.Capabilities.Thinking {
						if m.Capabilities.Thinking[i].Type == agent.Spec.Thinking.Type {
							matchingCap = &m.Capabilities.Thinking[i]
							break
						}
					}
					if matchingCap == nil {
						return fmt.Errorf("agent %s: model %q does not support thinking strategy %q", name, mRef, agent.Spec.Thinking.Type)
					}
					if agent.Spec.Thinking.Type == ThinkingStrategyEffort && agent.Spec.Thinking.Effort != "" && len(matchingCap.AllowedEfforts) > 0 {
						if !slices.Contains(matchingCap.AllowedEfforts, agent.Spec.Thinking.Effort) {
							return fmt.Errorf("agent %s: model %q does not allow thinking effort %q", name, mRef, agent.Spec.Thinking.Effort)
						}
					}
					if agent.Spec.Thinking.Type == ThinkingStrategyBudget && agent.Spec.Thinking.BudgetTokens > 0 && matchingCap.MaxBudgetTokens > 0 {
						if agent.Spec.Thinking.BudgetTokens > matchingCap.MaxBudgetTokens {
							return fmt.Errorf("agent %s: model %q thinking budgetTokens %d exceeds maximum %d", name, mRef, agent.Spec.Thinking.BudgetTokens, matchingCap.MaxBudgetTokens)
						}
					}
				}
			}
			for _, skillRef := range agent.Spec.Skills {
				if hasGlobMeta(skillRef) {
					continue
				}
				if !r.hasKindLocked(ns, KindSkill, skillRef) {
					return fmt.Errorf("agent %s references missing skill: %s", name, skillRef)
				}
			}
			for _, cmdRef := range agent.Spec.Commands {
				if hasGlobMeta(cmdRef) {
					continue
				}
				if !r.hasKindLocked(ns, KindCommand, cmdRef) {
					return fmt.Errorf("agent %s references missing command: %s", name, cmdRef)
				}
			}
			for _, saRef := range agent.Spec.Subagents {
				if hasGlobMeta(saRef) {
					continue
				}
				if !r.hasKindLocked(ns, KindAgent, saRef) {
					return fmt.Errorf("agent %s references missing subagent: %s", name, saRef)
				}
			}
			for _, ruleRef := range agent.Spec.Rules {
				if hasGlobMeta(ruleRef) {
					continue
				}
				if !r.hasKindLocked(ns, KindRule, ruleRef) {
					return fmt.Errorf("agent %s references missing rule: %s", name, ruleRef)
				}
			}
			for _, toolRef := range agent.Spec.Tools {
				if hasGlobMeta(toolRef) || toolRef == "*" {
					continue
				}
				if err := r.validateToolRefLocked(ns, toolRef); err != nil {
					if strings.Contains(err.Error(), "excluded by MCP") {
						return fmt.Errorf("agent %s: %w", name, err)
					}
					return fmt.Errorf("agent %s %w", name, err)
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
			seenAliases := make(map[string]string)
			modelIDsAndNames := make(map[string]string)
			for _, m := range mp.Spec.Models {
				if m.ID != "" {
					modelIDsAndNames[normalizeAliasName(m.ID)] = m.ID
				}
				if m.Name != "" {
					modelIDsAndNames[normalizeAliasName(m.Name)] = m.ID
				}
			}
			for _, m := range mp.Spec.Models {
				if m.Capabilities != nil {
					for _, tc := range m.Capabilities.Thinking {
						switch tc.Type {
						case ThinkingStrategyToggle, ThinkingStrategyEffort, ThinkingStrategyBudget, ThinkingStrategyAdaptive:
						default:
							return fmt.Errorf("provider %s: model %q has invalid thinking strategy %q", name, m.ID, tc.Type)
						}
					}
				}
				for _, a := range m.Aliases {
					norm := normalizeAliasName(a)
					if norm == "" {
						return fmt.Errorf("provider %s model %q: alias cannot be empty", name, m.ID)
					}
					if existingID, exists := seenAliases[norm]; exists {
						return fmt.Errorf("provider %s: duplicate alias %q on models %q and %q", name, a, existingID, m.ID)
					}
					seenAliases[norm] = m.ID
					if collidingID, exists := modelIDsAndNames[norm]; exists && collidingID != m.ID {
						return fmt.Errorf("provider %s: alias %q on model %q collides with model %q", name, a, m.ID, collidingID)
					}
				}
			}
			if mp.Spec.DefaultModels != nil {
				if err := validateProviderDefaultModels(name, mp); err != nil {
					return err
				}
			}
		}
		if skill, ok := res.(*Skill); ok {
			for _, toolRef := range skill.Spec.Tools {
				if hasGlobMeta(toolRef) || toolRef == "*" {
					continue
				}
				if err := r.validateToolRefLocked(ns, toolRef); err != nil {
					if strings.Contains(err.Error(), "excluded by MCP") {
						return fmt.Errorf("skill %s: %w", name, err)
					}
					return fmt.Errorf("skill %s %w", name, err)
				}
			}
		}
		if cmd, ok := res.(*Command); ok {
			for _, mRef := range cmd.Spec.Models {
				if len(r.findProvidersLocked()) > 0 {
					if _, _, isAlias := isAliasRef(mRef); isAlias {
						if err := r.validateAliasModelRefLocked(fmt.Sprintf("command %s", name), mRef, ModelRoleCompletion); err != nil {
							return err
						}
					}
				}
				if m := r.findDeclaredModelForRoleLocked(mRef, ModelRoleCompletion); m != nil && !m.Capabilities.SupportsCompletion() {
					return fmt.Errorf("command %s: model %q does not support completion capability", name, mRef)
				}
			}
			for _, skillRef := range cmd.Spec.Skills {
				if hasGlobMeta(skillRef) || skillRef == "*" {
					continue
				}
				if !r.hasKindLocked(ns, KindSkill, skillRef) {
					return fmt.Errorf("command %s references missing skill: %s", name, skillRef)
				}
			}
			for _, ruleRef := range cmd.Spec.Rules {
				if hasGlobMeta(ruleRef) || ruleRef == "*" {
					continue
				}
				if !r.hasKindLocked(ns, KindRule, ruleRef) {
					return fmt.Errorf("command %s references missing rule: %s", name, ruleRef)
				}
			}
			for _, toolRef := range cmd.Spec.Tools {
				if hasGlobMeta(toolRef) || toolRef == "*" {
					continue
				}
				if err := r.validateToolRefLocked(ns, toolRef); err != nil {
					if strings.Contains(err.Error(), "excluded by MCP") {
						return fmt.Errorf("command %s: %w", name, err)
					}
					return fmt.Errorf("command %s %w", name, err)
				}
			}
			if cmd.Spec.Agent != "" {
				if !hasGlobMeta(cmd.Spec.Agent) && !r.hasKindLocked(ns, KindAgent, cmd.Spec.Agent) {
					return fmt.Errorf("command %s references missing agent: %s", name, cmd.Spec.Agent)
				}
			}
			if cmd.Spec.Context != "" {
				switch cmd.Spec.Context {
				case CommandContextInline, CommandContextFork, CommandContextClean:
					// valid
				default:
					return fmt.Errorf("command %s has invalid context %q: must be one of inline, fork, clean", name, cmd.Spec.Context)
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
		if mcp, ok := res.(*MCP); ok {
			transport := mcp.Spec.Type
			if transport == "" {
				transport = MCPTransportStdio
			}
			switch transport {
			case MCPTransportStdio:
				if len(mcp.Spec.Command) == 0 {
					return fmt.Errorf("mcp %s: command is required for stdio transport", name)
				}
			case MCPTransportSSE, MCPTransportHTTP, MCPTransportStreamableHTTP:
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
		if len(r.findProvidersLocked()) > 0 {
			if _, _, isAlias := isAliasRef(mRef); isAlias {
				if err := r.validateAliasModelRefLocked(fmt.Sprintf("decision %s", name), mRef, ModelRoleDecision); err != nil {
					return err
				}
			}
		}
		if m := r.findDeclaredModelForRoleLocked(mRef, ModelRoleDecision); m != nil && !m.Capabilities.SupportsDecision() {
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
		if err := r.validateToolRefLocked(ns, tRef); err != nil {
			if strings.Contains(err.Error(), "excluded by MCP") {
				return fmt.Errorf("rule %s: trigger: %w", name, err)
			}
			return fmt.Errorf("rule %s: trigger %w", name, err)
		}
	}
	return nil
}

func (r *Registry) validateModelDefaultsLocked(scope string, dm *ModelDefaults) error {
	hasProviders := len(r.findProvidersLocked()) > 0
	checkRef := func(role ModelRole, roleStr, ref string, checkCap func(*ProviderModelCapabilities) bool) error {
		if ref == "" {
			return nil
		}
		if hasProviders {
			if _, _, isAlias := isAliasRef(ref); isAlias {
				if err := r.validateAliasModelRefLocked(fmt.Sprintf("%s defaultModels.%s", scope, roleStr), ref, role); err != nil {
					return err
				}
			}
		}
		m := r.findDeclaredModelForRoleLocked(ref, role)
		if hasProviders && m == nil {
			if before, _, ok := strings.Cut(ref, "/"); ok {
				prov := before
				if !r.hasProviderLocked(prov) {
					return fmt.Errorf("%s defaultModels.%s references missing provider: %s", scope, roleStr, prov)
				}
			}
		}
		if m != nil && !checkCap(m.Capabilities) {
			return fmt.Errorf("%s defaultModels.%s %q does not support %s capability", scope, roleStr, ref, roleStr)
		}
		return nil
	}
	if err := checkRef(ModelRoleCompletion, "completion", dm.Completion, (*ProviderModelCapabilities).SupportsCompletion); err != nil {
		return err
	}
	if err := checkRef(ModelRoleDecision, "decision", dm.Decision, (*ProviderModelCapabilities).SupportsDecision); err != nil {
		return err
	}
	if err := checkRef(ModelRoleEmbedding, "embedding", dm.Embedding, (*ProviderModelCapabilities).SupportsEmbedding); err != nil {
		return err
	}
	return nil
}

func validateProviderDefaultModels(name string, mp *ModelProvider) error {
	dm := mp.Spec.DefaultModels
	if dm == nil || len(mp.Spec.Models) == 0 {
		return nil
	}
	findModelOrAlias := func(ref string) *ProviderModel {
		if _, alias, ok := isAliasRef(ref); ok {
			return mp.findModelByAlias(alias)
		}
		return mp.findModel(ref)
	}
	if dm.Completion != "" {
		if m := findModelOrAlias(dm.Completion); m != nil && !m.Capabilities.SupportsCompletion() {
			return fmt.Errorf("provider %s: defaultModels.completion %q does not support completion capability", name, dm.Completion)
		}
	}
	if dm.Decision != "" {
		if m := findModelOrAlias(dm.Decision); m != nil && !m.Capabilities.SupportsDecision() {
			return fmt.Errorf("provider %s: defaultModels.decision %q does not support decision capability", name, dm.Decision)
		}
	}
	if dm.Embedding != "" {
		if m := findModelOrAlias(dm.Embedding); m != nil && !m.Capabilities.SupportsEmbedding() {
			return fmt.Errorf("provider %s: defaultModels.embedding %q does not support embedding capability", name, dm.Embedding)
		}
	}
	return nil
}

func (r *Registry) findDeclaredModelLocked(ref string) *ProviderModel {
	return r.findDeclaredModelForRoleLocked(ref, ModelRoleCompletion)
}

func (r *Registry) findDeclaredModelForRoleLocked(ref string, role ModelRole) *ProviderModel {
	if ref == "" {
		return nil
	}
	if provPrefix, aliasName, ok := isAliasRef(ref); ok {
		if provPrefix != "" {
			for _, mp := range r.findProvidersLocked() {
				if mp.matchesRef(provPrefix) {
					if aliasName == "default" {
						if provModel := mp.defaultForRole(role); provModel != "" {
							return r.findDeclaredModelForRoleLocked(provModel, role)
						}
						if m := mp.findModelByAlias("default"); m != nil {
							return m
						}
						for i := range mp.Spec.Models {
							m := &mp.Spec.Models[i]
							if m.ID != "" && m.Capabilities.supportsRole(role) {
								return m
							}
						}
						if len(mp.Spec.Models) > 0 {
							return &mp.Spec.Models[0]
						}
						return nil
					}
					return mp.findModelByAlias(aliasName)
				}
			}
			return nil
		}

		// Bare alias: @<alias>
		if aliasName == "default" {
			if r.workspace != nil && r.workspace.Def != nil && r.workspace.Def.Spec.ModelAliases != nil {
				target := r.workspace.Def.Spec.ModelAliases["default"]
				if target == "" {
					target = r.workspace.Def.Spec.ModelAliases["@default"]
				}
				if target != "" && target != "@default" && target != "default" {
					return r.findDeclaredModelForRoleLocked(target, role)
				}
			}
			if r.workspace != nil && r.workspace.Def != nil {
				wsModel := r.workspace.Def.Spec.DefaultModels.ForRole(role)
				if wsModel != "" && wsModel != "@default" && wsModel != "default" {
					return r.findDeclaredModelForRoleLocked(wsModel, role)
				}
			}
			var defaultProv string
			if r.workspace != nil && r.workspace.Def != nil {
				defaultProv = r.workspace.Def.Spec.DefaultProvider
			}
			providers := effectiveProviders(r, r, defaultProv)
			for _, mp := range providers {
				if provModel := mp.defaultForRole(role); provModel != "" {
					return r.findDeclaredModelForRoleLocked(provModel, role)
				}
			}
			for _, mp := range providers {
				for i := range mp.Spec.Models {
					m := &mp.Spec.Models[i]
					if m.ID != "" && m.Capabilities.supportsRole(role) {
						return m
					}
				}
			}
			return nil
		}

		// Non-default bare alias: @<alias>
		// 1. Check workspace modelAliases
		if r.workspace != nil && r.workspace.Def != nil && r.workspace.Def.Spec.ModelAliases != nil {
			target := r.workspace.Def.Spec.ModelAliases[aliasName]
			if target == "" {
				target = r.workspace.Def.Spec.ModelAliases["@"+aliasName]
			}
			if target != "" && target != "@"+aliasName && target != aliasName {
				return r.findDeclaredModelForRoleLocked(target, role)
			}
		}

		// 2. Providers in priority order
		var defaultProv string
		if r.workspace != nil && r.workspace.Def != nil {
			defaultProv = r.workspace.Def.Spec.DefaultProvider
		}
		providers := effectiveProviders(r, r, defaultProv)
		for _, mp := range providers {
			if m := mp.findModelByAlias(aliasName); m != nil {
				return m
			}
		}
		return nil
	}

	providers := r.findProvidersLocked()
	if before, after, ok := strings.Cut(ref, "/"); ok {
		for _, mp := range providers {
			if mp.matchesRef(before) {
				return mp.findModel(after)
			}
		}
	}
	for _, mp := range providers {
		if m := mp.findModel(ref); m != nil {
			return m
		}
	}
	return nil
}

func (r *Registry) validateAliasModelRefLocked(contextStr, ref string, role ModelRole) error {
	provPrefix, aliasName, isAlias := isAliasRef(ref)
	if !isAlias {
		return nil
	}
	if provPrefix != "" {
		if !r.hasProviderLocked(provPrefix) {
			return fmt.Errorf("%s: model %q references missing provider: %s", contextStr, ref, provPrefix)
		}
		mp := r.findProviderLocked(provPrefix)
		if mp == nil {
			return fmt.Errorf("%s: model %q references missing provider: %s", contextStr, ref, provPrefix)
		}
		if aliasName == "default" {
			if mp.defaultForRole(role) == "" && mp.findModelByAlias("default") == nil {
				hasRoleModel := false
				for i := range mp.Spec.Models {
					if mp.Spec.Models[i].Capabilities.supportsRole(role) {
						hasRoleModel = true
						break
					}
				}
				if !hasRoleModel && len(mp.Spec.Models) == 0 {
					return fmt.Errorf("%s: provider %q has no default or %s model for @default", contextStr, provPrefix, role)
				}
			}
			return nil
		}
		if mp.findModelByAlias(aliasName) == nil {
			return fmt.Errorf("%s: provider %q has no model with alias %q", contextStr, provPrefix, aliasName)
		}
		return nil
	}

	// Bare alias: @<alias>
	if aliasName == "default" {
		if r.findDeclaredModelForRoleLocked(ref, role) == nil {
			return fmt.Errorf("%s: cannot resolve @default, no default or %s model available", contextStr, role)
		}
		return nil
	}

	// Check if alias exists in workspace.spec.modelAliases
	if r.workspace != nil && r.workspace.Def != nil && r.workspace.Def.Spec.ModelAliases != nil {
		target := r.workspace.Def.Spec.ModelAliases[aliasName]
		if target == "" {
			target = r.workspace.Def.Spec.ModelAliases["@"+aliasName]
		}
		if target != "" {
			if _, _, targetIsAlias := isAliasRef(target); targetIsAlias {
				return r.validateAliasModelRefLocked(contextStr, target, role)
			}
			if before, after, ok := strings.Cut(target, "/"); ok {
				if !r.hasProviderLocked(before) {
					return fmt.Errorf("%s: model %q references missing provider: %s", contextStr, target, before)
				}
				mp := r.findProviderLocked(before)
				if mp == nil || mp.findModel(after) == nil {
					return fmt.Errorf("%s: provider %q has no model %q", contextStr, before, after)
				}
				return nil
			}
			if r.findDeclaredModelLocked(target) == nil {
				return fmt.Errorf("%s: unknown model %q", contextStr, target)
			}
			return nil
		}
	}

	// Check if any provider has model with this alias
	foundInProvider := false
	for _, mp := range r.findProvidersLocked() {
		if mp.findModelByAlias(aliasName) != nil {
			foundInProvider = true
			break
		}
	}
	if !foundInProvider {
		return fmt.Errorf("%s: unknown model alias %q", contextStr, ref)
	}
	return nil
}

func (r *Registry) findProviderLocked(ref string) *ModelProvider {
	for _, mp := range r.findProvidersLocked() {
		if mp.matchesRef(ref) {
			return mp
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
		if mp.matchesRef(ref) {
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

func validateToolPolicies(tp *ToolPolicies, prefix string) error {
	if tp == nil {
		return nil
	}
	if tp.Dangerous != "" {
		switch tp.Dangerous {
		case ToolPolicyAllow, ToolPolicyAsk, ToolPolicyDeny:
		default:
			return fmt.Errorf("%s: invalid dangerous action %q (must be allow, ask, or deny)", prefix, tp.Dangerous)
		}
	}
	if tp.OpenWorld != "" {
		switch tp.OpenWorld {
		case ToolPolicyAllow, ToolPolicyAsk, ToolPolicyDeny:
		default:
			return fmt.Errorf("%s: invalid openWorld action %q (must be allow, ask, or deny)", prefix, tp.OpenWorld)
		}
	}
	return nil
}

func matchResourcePattern(pat, shortName, nsName, qualifiedName string) bool {
	if m, _ := path.Match(pat, shortName); m {
		return true
	}
	if m, _ := path.Match(pat, nsName); m {
		return true
	}
	if m, _ := path.Match(pat, qualifiedName); m {
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

// resolveKindLocked finds a resource of the expected kind matching ref from fromNamespace.
// Must be called with r.mu held for reading.
func (r *Registry) resolveKindLocked(fromNamespace string, kind Kind, ref string) Resource {
	return r.resolveRefLocked(fromNamespace, kind, ref)
}

// hasKindLocked reports whether ref resolves to a resource of the expected kind.
// Must be called with r.mu held for reading.
func (r *Registry) hasKindLocked(fromNamespace string, kind Kind, ref string) bool {
	return r.resolveRefLocked(fromNamespace, kind, ref) != nil
}

// validateToolRefLocked checks if a tool reference resolves to a known Tool or an allowed MCP tool.
// Must be called with r.mu held for reading.
func (r *Registry) validateToolRefLocked(fromNamespace, toolRef string) error {
	if r.hasKindLocked(fromNamespace, KindTool, toolRef) {
		return nil
	}

	var mcpRes Resource
	var rawTool string
	var mcpName string

	if ns, kind, short, ok := ParseQualifiedName(toolRef); ok && kind == KindTool {
		if mName, rTool, ok := ParseMCPToolName(short); ok {
			mcpName = mName
			rawTool = rTool
			mcpRef := MakeQualifiedName(ns, KindMCP, mcpName)
			mcpRes = r.resolveKindLocked(fromNamespace, KindMCP, mcpRef)
			if mcpRes == nil && ns != fromNamespace {
				mcpRes = r.resolveKindLocked(ns, KindMCP, mcpName)
			}
		}
	} else if before, after, ok := strings.Cut(toolRef, "/"); ok && !strings.Contains(after, "/") {
		if mName, rTool, ok := ParseMCPToolName(after); ok {
			mcpName = mName
			rawTool = rTool
			mcpRes = r.resolveKindLocked(fromNamespace, KindMCP, before+"/MCP/"+mcpName)
			if mcpRes == nil {
				mcpRes = r.resolveKindLocked(fromNamespace, KindMCP, before+"/"+mcpName)
			}
			if mcpRes == nil {
				mcpRes = r.resolveKindLocked(before, KindMCP, mcpName)
			}
		}
	} else if mName, rTool, ok := ParseMCPToolName(toolRef); ok {
		mcpName = mName
		rawTool = rTool
		mcpRes = r.resolveKindLocked(fromNamespace, KindMCP, mcpName)
	}

	if mcpRes != nil {
		if mcp, ok := mcpRes.(*MCP); ok {
			if mcp.AllowsTool(rawTool) {
				return nil
			}
			return fmt.Errorf("tool %q is excluded by MCP %q policies", rawTool, mcpName)
		}
	}

	return fmt.Errorf("references missing tool: %s", toolRef)
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolveResourceLocked(ref)
}

func (r *Registry) resolveResourceLocked(ref string) (Resource, bool) {
	if strings.Contains(ref, "/") {
		if res, ok := r.resources[ref]; ok {
			return res, true
		}
		// Fallback for relative file paths like "skills/go.md"
		return r.resolvePathFallbackLocked(ref, r.isAllowedInBaseScopeLocked, NamespacePriority)
	}
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

// ToolsForSkill returns the Tool resources required or declared by the named skill within
// this project scope.
func (s *ScopedRegistry) ToolsForSkill(skillRef string) ([]*Tool, error) {
	return resolveToolsForSkill(s, skillRef)
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
	if ag.Spec.Thinking != nil {
		parent.Spec.Thinking = ag.Spec.Thinking.DeepCopy()
	}
	if len(ag.Spec.Triggers) > 0 {
		parent.Spec.Triggers = make([]string, len(ag.Spec.Triggers))
		copy(parent.Spec.Triggers, ag.Spec.Triggers)
	}

	// Merge: parent is the base; append child lists and instructions after.
	if len(ag.Spec.Skills) > 0 {
		parent.Spec.Skills = append(parent.Spec.Skills, ag.Spec.Skills...)
	}
	if len(ag.Spec.Commands) > 0 {
		parent.Spec.Commands = append(parent.Spec.Commands, ag.Spec.Commands...)
	}
	if len(ag.Spec.Subagents) > 0 {
		parent.Spec.Subagents = append(parent.Spec.Subagents, ag.Spec.Subagents...)
	}
	if len(ag.Spec.Rules) > 0 {
		parent.Spec.Rules = append(parent.Spec.Rules, ag.Spec.Rules...)
	}
	if len(ag.Spec.Tools) > 0 {
		parent.Spec.Tools = append(parent.Spec.Tools, ag.Spec.Tools...)
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

// ResolveCommand resolves a command by ref, eagerly loading its skills, rules, and effective tools.
func (r *Registry) ResolveCommand(ref string) (*ResolvedCommand, error) {
	return resolveCommand(r, nil, ref)
}

// ResolveCommand resolves a command by ref within this project scope, eagerly loading its skills, rules, and effective tools.
func (s *ScopedRegistry) ResolveCommand(ref string) (*ResolvedCommand, error) {
	return resolveCommand(s, nil, ref)
}

// ResolveModels returns the prioritized list of resolved models for the given
// capability role, following the WARP model resolution hierarchy:
//  1. Non-empty explicit model overrides (e.g., from Agent, Command, or Decision).
//  2. Workspace role-specific default (spec.defaultModels.<role>).
//  3. Default Provider (or effective providers') role-specific default
//     (spec.defaultModels.<role>), and then capability-matching models in spec.models.
//
// Model references (including IDs containing "/" and model name aliases) are
// matched against effective ModelProvider catalogs and canonicalized to
// "<provider>/<model-id>".
func (r *Registry) ResolveModels(role ModelRole, explicit ...string) []ResolvedModel {
	return resolveModels(r, r, role, explicit)
}

// ResolveModels resolves models for the given role within this project scope.
func (s *ScopedRegistry) ResolveModels(role ModelRole, explicit ...string) []ResolvedModel {
	return resolveModels(s.base, s, role, explicit)
}

// ResolveModel resolves a model reference (or role default when explicit is empty)
// against effective ModelProvider resources, returning the first *ResolvedModel
// backed by a registered provider.
func (r *Registry) ResolveModel(role ModelRole, explicit ...string) (*ResolvedModel, bool) {
	return resolveModel(r, r, role, explicit)
}

// ResolveModel resolves a model reference (or role default when explicit is empty)
// within this project scope against effective ModelProvider resources.
func (s *ScopedRegistry) ResolveModel(role ModelRole, explicit ...string) (*ResolvedModel, bool) {
	return resolveModel(s.base, s, role, explicit)
}

// ResolveRulesForPath returns all rules applicable to the given path within this registry scope,
// sorted by Rule priority descending, then by name ascending.
func (r *Registry) ResolveRulesForPath(targetPath string) []Rule {
	return resolveRulesForPath(r, targetPath)
}

// ResolveRulesForTool returns all rules applicable to the given tool within this registry scope,
// sorted by Rule priority descending, then by name ascending.
func (r *Registry) ResolveRulesForTool(toolName string) []Rule {
	return resolveRulesForTool(r, toolName)
}

// ToolsForSkill returns the Tool resources required or declared by the named skill within
// this registry scope.
func (r *Registry) ToolsForSkill(skillRef string) ([]*Tool, error) {
	return resolveToolsForSkill(r, skillRef)
}

// ResolveRulesForPath returns all rules applicable to the given path within this scoped registry,
// sorted by Rule priority descending, then by name ascending.
func (s *ScopedRegistry) ResolveRulesForPath(targetPath string) []Rule {
	return resolveRulesForPath(s, targetPath)
}

// ResolveRulesForTool returns all rules applicable to the given tool within this scoped registry,
// sorted by Rule priority descending, then by name ascending.
func (s *ScopedRegistry) ResolveRulesForTool(toolName string) []Rule {
	return resolveRulesForTool(s, toolName)
}

// Reload re-reads the workspace from disk and updates the underlying registry in-place.
func (s *ScopedRegistry) Reload() (*ReloadStats, error) {
	if s == nil || s.base == nil {
		return nil, fmt.Errorf("registry is nil")
	}
	return s.base.Reload()
}

// SetMCPTools registers dynamically discovered tools for an MCP server in the registry.
func (r *Registry) SetMCPTools(mcpRef string, tools []*Tool) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.mcpTools == nil {
		r.mcpTools = make(map[string]mcpToolEntry)
	}

	var targetMCP *MCP

	// 1. Check exact qualified name
	if res, ok := r.resources[mcpRef]; ok {
		if m, ok := res.(*MCP); ok {
			targetMCP = m
		} else {
			return fmt.Errorf("resource %q is not an MCP (kind: %s)", mcpRef, res.GetKind())
		}
	}

	// 2. ResolveResource logic
	if targetMCP == nil {
		if res, ok := r.resolveResourceLocked(mcpRef); ok {
			if m, ok := res.(*MCP); ok {
				targetMCP = m
			} else {
				return fmt.Errorf("resource %q is not an MCP (kind: %s)", mcpRef, res.GetKind())
			}
		}
	}

	// 3. Match *MCP by short name across all resources
	if targetMCP == nil {
		var candidates []*MCP
		for _, res := range r.resources {
			if res.GetKind() == KindMCP && res.GetName() == mcpRef {
				if m, ok := res.(*MCP); ok {
					candidates = append(candidates, m)
				}
			}
		}
		if len(candidates) == 1 {
			targetMCP = candidates[0]
		} else if len(candidates) > 1 {
			sort.Slice(candidates, func(i, j int) bool {
				pi := NamespacePriority(candidates[i].GetNamespace())
				pj := NamespacePriority(candidates[j].GetNamespace())
				if pi != pj {
					return pi > pj
				}
				return candidates[i].QualifiedName() < candidates[j].QualifiedName()
			})
			targetMCP = candidates[0]
		}
	}

	if targetMCP == nil {
		return fmt.Errorf("MCP resource %q not found", mcpRef)
	}

	mcpQN := targetMCP.QualifiedName()

	// Remove previously registered dynamic tools for this MCP
	if prev, ok := r.mcpTools[mcpQN]; ok {
		for _, key := range prev.activeKeys {
			delete(r.resources, key)
		}
		delete(r.mcpTools, mcpQN)
	}

	var rawCopy []*Tool
	for _, t := range tools {
		if t != nil {
			rawCopy = append(rawCopy, t.DeepCopy())
		}
	}

	activeTools, activeKeys := materializeMCPTools(targetMCP, rawCopy)
	for _, t := range activeTools {
		r.resources[t.QualifiedName()] = t
	}

	r.mcpTools[mcpQN] = mcpToolEntry{
		rawTools:   rawCopy,
		activeKeys: activeKeys,
	}
	return nil
}

func materializeMCPTools(targetMCP *MCP, rawTools []*Tool) (active []*Tool, activeKeys []string) {
	for _, rawTool := range rawTools {
		if rawTool == nil {
			continue
		}
		rawName := rawTool.Metadata.Name
		if mcpName, raw, ok := ParseMCPToolName(rawName); ok && mcpName == targetMCP.Metadata.Name {
			rawName = raw
		} else if after, ok0 := strings.CutPrefix(rawName, targetMCP.Metadata.Name+"__"); ok0 {
			rawName = after
		}

		if !targetMCP.AllowsTool(rawName) {
			continue
		}

		t := rawTool.DeepCopy()
		t.Kind = KindTool
		t.Namespace = targetMCP.GetNamespace()
		if t.Namespace == "" {
			t.Namespace = NamespaceLocal
		}
		t.Metadata.Name = targetMCP.ToolName(rawName)
		if t.APIVersion == "" {
			if targetMCP.APIVersion != "" {
				t.APIVersion = targetMCP.APIVersion
			} else {
				t.APIVersion = APIVersion
			}
		}
		if t.Directory == "" {
			t.Directory = targetMCP.Directory
		}
		if t.Path == "" {
			t.Path = targetMCP.GetPath()
		}
		t.Spec.Annotations = targetMCP.EffectiveAnnotation(rawName, rawTool.Spec.Annotations)

		active = append(active, t)
		activeKeys = append(activeKeys, t.QualifiedName())
	}
	return active, activeKeys
}

// SetMCPTools registers dynamically discovered tools for an MCP server in the registry.
func (s *ScopedRegistry) SetMCPTools(mcpRef string, tools []*Tool) error {
	if s == nil || s.base == nil {
		return fmt.Errorf("registry is nil")
	}
	if res, ok := s.ResolveResource(mcpRef); ok && res.GetKind() == KindMCP {
		return s.base.SetMCPTools(res.QualifiedName(), tools)
	}
	return s.base.SetMCPTools(mcpRef, tools)
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

func resolveRulesForTool(resolver Resolver, toolName string) []Rule {
	candidates := resolver.ListResources(QueryOptions{Kinds: []Kind{KindRule}, Effective: true})
	var matched []Rule
	for _, res := range candidates {
		if rl, ok := res.(*Rule); ok {
			if rl.Spec.Triggers.MatchesTool(toolName) {
				matched = append(matched, *rl)
			}
		}
	}
	return deduplicateRules(matched)
}

func resolveModel(base *Registry, resolver Resolver, role ModelRole, explicit []string) (*ResolvedModel, bool) {
	for _, rm := range resolveModels(base, resolver, role, explicit) {
		if rm.Provider != nil {
			return &rm, true
		}
	}
	return nil, false
}

func resolveModels(base *Registry, resolver Resolver, role ModelRole, explicit []string) []ResolvedModel {
	base.mu.RLock()
	var wsSpec WorkspaceDefSpec
	if base.workspace != nil && base.workspace.Def != nil {
		wsSpec = base.workspace.Def.Spec
	}
	base.mu.RUnlock()

	defaultProv := wsSpec.DefaultProvider
	providers := effectiveProviders(base, resolver, defaultProv)
	findProvider := func(ref string) *ModelProvider {
		for _, mp := range providers {
			if mp.matchesRef(ref) {
				return mp
			}
		}
		return nil
	}
	defaultMP := findProvider(defaultProv)

	var qualify func(ref string, owner *ModelProvider) ResolvedModel
	var resolveAlias func(ref string, visited map[string]bool) []ResolvedModel

	resolveAlias = func(ref string, visited map[string]bool) []ResolvedModel {
		if visited[ref] {
			return nil
		}
		visited[ref] = true

		provPrefix, aliasName, isAlias := isAliasRef(ref)
		if !isAlias {
			return nil
		}

		if provPrefix != "" {
			mp := findProvider(provPrefix)
			if mp == nil {
				return []ResolvedModel{newResolvedModel(nil, ref, provPrefix)}
			}
			if aliasName == "default" {
				if provModel := mp.defaultForRole(role); provModel != "" {
					return []ResolvedModel{qualify(provModel, mp)}
				}
				if m := mp.findModelByAlias("default"); m != nil {
					return []ResolvedModel{newResolvedModel(mp, m.ID, defaultProv)}
				}
				for i := range mp.Spec.Models {
					m := &mp.Spec.Models[i]
					if m.ID != "" && m.Capabilities.supportsRole(role) {
						return []ResolvedModel{newResolvedModel(mp, m.ID, defaultProv)}
					}
				}
				return []ResolvedModel{newResolvedModel(nil, ref, provPrefix)}
			}
			if m := mp.findModelByAlias(aliasName); m != nil {
				return []ResolvedModel{newResolvedModel(mp, m.ID, defaultProv)}
			}
			return []ResolvedModel{newResolvedModel(nil, ref, provPrefix)}
		}

		// Bare alias: @<alias>
		if aliasName == "default" {
			// Check workspace modelAliases override
			if wsSpec.ModelAliases != nil {
				target := wsSpec.ModelAliases["default"]
				if target == "" {
					target = wsSpec.ModelAliases["@default"]
				}
				if target != "" && target != "@default" && target != "default" {
					if _, _, ok := isAliasRef(target); ok {
						return resolveAlias(target, visited)
					}
					return []ResolvedModel{qualify(target, nil)}
				}
			}
			wsModel := wsSpec.DefaultModels.ForRole(role)
			if wsModel != "" && wsModel != "@default" && wsModel != "default" {
				if _, _, ok := isAliasRef(wsModel); ok {
					return resolveAlias(wsModel, visited)
				}
				return []ResolvedModel{qualify(wsModel, nil)}
			}
			for _, mp := range providers {
				if provModel := mp.defaultForRole(role); provModel != "" {
					return []ResolvedModel{qualify(provModel, mp)}
				}
			}
			var defModels []ResolvedModel
			for _, mp := range providers {
				for i := range mp.Spec.Models {
					m := &mp.Spec.Models[i]
					if m.ID != "" && m.Capabilities.supportsRole(role) {
						defModels = append(defModels, newResolvedModel(mp, m.ID, defaultProv))
					}
				}
			}
			if len(defModels) > 0 {
				return defModels
			}
			return []ResolvedModel{newResolvedModel(nil, ref, "")}
		}

		// Non-default bare alias: @<alias>
		// 1. Check workspace ModelAliases
		if wsSpec.ModelAliases != nil {
			target := wsSpec.ModelAliases[aliasName]
			if target == "" {
				target = wsSpec.ModelAliases["@"+aliasName]
			}
			if target != "" {
				if _, _, ok := isAliasRef(target); ok {
					return resolveAlias(target, visited)
				}
				return []ResolvedModel{qualify(target, nil)}
			}
		}

		// 2. Providers in priority order
		var aliasModels []ResolvedModel
		for _, mp := range providers {
			if m := mp.findModelByAlias(aliasName); m != nil {
				aliasModels = append(aliasModels, newResolvedModel(mp, m.ID, defaultProv))
			}
		}
		if len(aliasModels) > 0 {
			return aliasModels
		}
		return []ResolvedModel{newResolvedModel(nil, ref, "")}
	}

	qualify = func(ref string, owner *ModelProvider) ResolvedModel {
		if _, _, ok := isAliasRef(ref); ok {
			rms := resolveAlias(ref, make(map[string]bool))
			if len(rms) > 0 {
				return rms[0]
			}
		}
		if before, after, ok := strings.Cut(ref, "/"); ok {
			if mp := findProvider(before); mp != nil {
				return newResolvedModel(mp, after, before)
			}
		}
		if owner != nil {
			return newResolvedModel(owner, ref, defaultProv)
		}
		for _, mp := range providers {
			if mp.findModel(ref) != nil {
				return newResolvedModel(mp, ref, defaultProv)
			}
		}
		if defaultProv != "" && !strings.Contains(ref, "/") && !strings.Contains(defaultProv, "/") {
			return newResolvedModel(defaultMP, ref, defaultProv)
		}
		return newResolvedModel(nil, ref, "")
	}

	// 1. Explicit model overrides
	var out []ResolvedModel
	for _, m := range explicit {
		if m != "" {
			if _, _, ok := isAliasRef(m); ok {
				out = append(out, resolveAlias(m, make(map[string]bool))...)
			} else {
				out = append(out, qualify(m, nil))
			}
		}
	}
	if len(out) > 0 {
		return out
	}

	// 2. Workspace defaults
	wsModel := wsSpec.DefaultModels.ForRole(role)
	if wsModel != "" {
		if _, _, ok := isAliasRef(wsModel); ok {
			return resolveAlias(wsModel, make(map[string]bool))
		}
		return []ResolvedModel{qualify(wsModel, nil)}
	}

	// 3. Provider defaults, then capability-matching models in spec.models
	for _, mp := range providers {
		if provModel := mp.defaultForRole(role); provModel != "" {
			return []ResolvedModel{qualify(provModel, mp)}
		}
	}
	for _, mp := range providers {
		for i := range mp.Spec.Models {
			if m := &mp.Spec.Models[i]; m.ID != "" && m.Capabilities.supportsRole(role) {
				out = append(out, newResolvedModel(mp, m.ID, defaultProv))
			}
		}
	}
	return out
}

func newResolvedModel(mp *ModelProvider, rawID, fallbackProv string) ResolvedModel {
	m := mp.findModel(rawID)
	id := rawID
	if m != nil {
		id = m.ID
	}
	pName := fallbackProv
	if mp != nil && mp.Metadata.Name != "" {
		pName = mp.Metadata.Name
	}
	if pName != "" {
		return ResolvedModel{Provider: mp, Model: m, ModelID: id, QualifiedID: pName + "/" + id}
	}
	if _, after, ok := strings.Cut(id, "/"); ok {
		return ResolvedModel{ModelID: after, QualifiedID: id}
	}
	return ResolvedModel{ModelID: id, QualifiedID: id}
}

func effectiveProviders(base *Registry, resolver Resolver, defaultProv string) []*ModelProvider {
	var list []*ModelProvider
	for _, res := range resolver.ListResources(QueryOptions{Kinds: []Kind{KindModelProvider}, Effective: true}) {
		if mp, ok := res.(*ModelProvider); ok {
			list = append(list, mp)
		}
	}
	if len(list) == 0 {
		base.mu.RLock()
		list = base.findProvidersLocked()
		base.mu.RUnlock()
		sort.Slice(list, func(i, j int) bool {
			return list[i].QualifiedName() < list[j].QualifiedName()
		})
	}
	if defaultProv != "" {
		for i, mp := range list {
			if mp.matchesRef(defaultProv) {
				if i > 0 {
					list = append([]*ModelProvider{mp}, append(list[:i], list[i+1:]...)...)
				}
				break
			}
		}
	}
	return list
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
	if containsWildcard(ag.Spec.Skills) {
		all := resolver.ListResources(QueryOptions{Kinds: []Kind{KindSkill}, Effective: true})
		for _, r := range all {
			if sk, ok := r.(*Skill); ok {
				skills = append(skills, *sk)
			}
		}
	} else if len(ag.Spec.Skills) > 0 {
		var allSkills []Resource
		for _, sRef := range ag.Spec.Skills {
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

	// 2. Resolve Tools
	tools := resolveToolsFromRefs(resolver, ag.Spec.Tools)

	// Apply tool policy filter
	wsPolicy := workspaceToolPolicy(resolver)
	if wsPolicy != nil {
		tools = filterTools(tools, wsPolicy)
	}
	if ag.Spec.Policies != nil && ag.Spec.Policies.Tools != nil {
		tools = filterTools(tools, ag.Spec.Policies.Tools)
	}

	// 3. Resolve Commands
	var commands []*Command
	if containsWildcard(ag.Spec.Commands) {
		all := resolver.ListResources(QueryOptions{Kinds: []Kind{KindCommand}, Effective: true})
		for _, r := range all {
			if c, ok := r.(*Command); ok {
				commands = append(commands, c)
			}
		}
	} else if len(ag.Spec.Commands) > 0 {
		var allCommands []Resource
		for _, cRef := range ag.Spec.Commands {
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
	resolveCandidateSubagent := func(a *Agent) *Agent {
		if a == nil {
			return nil
		}
		if a.Spec.Extends != "" {
			if merged, err := resolveAgentChain(resolver, a.QualifiedName(), make(map[string]struct{})); err == nil {
				return merged
			}
		}
		return a
	}

	var subagents []*Agent
	if containsWildcard(ag.Spec.Subagents) {
		all := resolver.ListResources(QueryOptions{Kinds: []Kind{KindAgent}, Effective: true})
		for _, r := range all {
			if a, ok := r.(*Agent); ok {
				candidate := resolveCandidateSubagent(a)
				if canBeTriggeredByAgent(candidate) {
					subagents = append(subagents, candidate)
				}
			}
		}
	} else if len(ag.Spec.Subagents) > 0 {
		var allAgents []Resource
		for _, saRef := range ag.Spec.Subagents {
			if hasGlobMeta(saRef) {
				if allAgents == nil {
					allAgents = resolver.ListResources(QueryOptions{Kinds: []Kind{KindAgent}, Effective: false})
				}
				for _, r := range allAgents {
					if a, ok := r.(*Agent); ok && matchRefPattern(resolver, saRef, r) {
						candidate := resolveCandidateSubagent(a)
						if canBeTriggeredByAgent(candidate) {
							subagents = append(subagents, candidate)
						}
					}
				}
				continue
			}
			r, ok := resolver.ResolveResource(saRef)
			if !ok {
				continue
			}
			if a, ok := r.(*Agent); ok {
				candidate := resolveCandidateSubagent(a)
				if canBeTriggeredByAgent(candidate) {
					subagents = append(subagents, candidate)
				}
			}
		}
		subagents = deduplicateAgents(subagents)
	}

	// 5. Resolve Rules
	var rules []Rule
	ruleMap := make(map[string]Rule)
	var ruleOrder []string

	addRule := func(rl Rule, forceAlways bool) {
		qn := rl.QualifiedName()
		if forceAlways {
			cp := *rl.DeepCopy()
			cp.Spec.Triggers.Always = true
			rl = cp
		}
		if existing, ok := ruleMap[qn]; ok {
			if !existing.Spec.Triggers.Always && rl.Spec.Triggers.Always {
				ruleMap[qn] = rl
			}
			return
		}
		ruleMap[qn] = rl
		ruleOrder = append(ruleOrder, qn)
	}

	allEffectiveRules := resolver.ListResources(QueryOptions{Kinds: []Kind{KindRule}, Effective: true})

	// 5a. Always include effective rules in scope whose Spec.Triggers.Always == true
	for _, r := range allEffectiveRules {
		if rl, ok := r.(*Rule); ok && rl.Spec.Triggers.Always {
			addRule(*rl, false)
		}
	}

	// 5b. If containsWildcard(ag.Spec.Rules), include all effective rules in scope
	if containsWildcard(ag.Spec.Rules) {
		for _, r := range allEffectiveRules {
			if rl, ok := r.(*Rule); ok {
				addRule(*rl, false)
			}
		}
	}

	// 5c. For any rule explicitly matched by ag.Spec.Rules (via ref or non-"*" glob),
	// attach a copy with Always = true
	var allNonEffectiveRules []Resource
	for _, rRef := range ag.Spec.Rules {
		if rRef == "*" {
			continue
		}
		if hasGlobMeta(rRef) {
			if allNonEffectiveRules == nil {
				allNonEffectiveRules = resolver.ListResources(QueryOptions{Kinds: []Kind{KindRule}, Effective: false})
			}
			for _, r := range allNonEffectiveRules {
				if rl, ok := r.(*Rule); ok && matchRefPattern(resolver, rRef, r) {
					addRule(*rl, true)
				}
			}
			continue
		}
		r, ok := resolver.ResolveResource(rRef)
		if !ok {
			continue
		}
		if rl, ok := r.(*Rule); ok {
			addRule(*rl, true)
		}
	}

	for _, qn := range ruleOrder {
		rules = append(rules, ruleMap[qn])
	}

	// 6. Resolve MCPs
	var mcps []*MCP
	mcpRes := resolver.ListResources(QueryOptions{Kinds: []Kind{KindMCP}, Effective: true})
	for _, r := range mcpRes {
		if m, ok := r.(*MCP); ok {
			mcps = append(mcps, m)
		}
	}
	sort.Slice(mcps, func(i, j int) bool {
		return mcps[i].QualifiedName() < mcps[j].QualifiedName()
	})

	// 7. Resolve Models
	models := resolver.ResolveModels(ModelRoleCompletion, ag.Spec.Models...)

	return &ResolvedAgent{
		Agent:           ag,
		Models:          models,
		Tools:           tools,
		Skills:          skills,
		Commands:        commands,
		Subagents:       subagents,
		Rules:           rules,
		MCPs:            mcps,
		WorkspacePolicy: wsPolicy,
	}, nil
}

func resolveToolsFromRefs(resolver Resolver, toolRefs []string) []*Tool {
	var tools []*Tool
	seen := make(map[string]bool)
	addTool := func(t *Tool) {
		if t == nil {
			return
		}
		qn := t.QualifiedName()
		if !seen[qn] {
			seen[qn] = true
			tools = append(tools, t)
		}
	}

	if containsWildcard(toolRefs) {
		all := resolver.ListResources(QueryOptions{Kinds: []Kind{KindTool}, Effective: true})
		for _, r := range all {
			if t, ok := r.(*Tool); ok {
				addTool(t)
			}
		}
	} else if len(toolRefs) > 0 {
		var allTools []Resource
		for _, tRef := range toolRefs {
			if hasGlobMeta(tRef) {
				if allTools == nil {
					allTools = resolver.ListResources(QueryOptions{Kinds: []Kind{KindTool}, Effective: false})
				}
				for _, r := range allTools {
					if t, ok := r.(*Tool); ok && matchRefPattern(resolver, tRef, r) {
						addTool(t)
					}
				}
				continue
			}
			tRes, ok := resolver.ResolveResource(tRef)
			if !ok {
				continue
			}
			if t, ok := tRes.(*Tool); ok {
				addTool(t)
			}
		}
	}

	sort.Slice(tools, func(i, j int) bool {
		return tools[i].QualifiedName() < tools[j].QualifiedName()
	})
	return tools
}

func resolveToolsForSkill(resolver Resolver, skillRef string) ([]*Tool, error) {
	res, ok := resolver.ResolveResource(skillRef)
	if !ok {
		return nil, fmt.Errorf("skill %q not found", skillRef)
	}
	skill, ok := res.(*Skill)
	if !ok {
		return nil, fmt.Errorf("resource %q is a %s, not a Skill", skillRef, res.GetKind())
	}

	tools := resolveToolsFromRefs(resolver, skill.Spec.Tools)
	if wsPolicy := workspaceToolPolicy(resolver); wsPolicy != nil {
		tools = filterTools(tools, wsPolicy)
	}
	return tools, nil
}

// ToolsForSkill returns the tools required or declared by the named skill for this resolved agent.
// It verifies that the resolved skill is present in ra.Skills (returning an error if not enabled),
// resolves the skill's tools (applying the workspace policy ceiling), and filters the tools against
// the agent's full policy ceiling. Skill tools do not need to be part of the agent's ambient
// spec.tools, but they are still filtered against agent.spec.policies.tools (including Include,
// Exclude, Dangerous, OpenWorld, and ReadOnly).
func (ra *ResolvedAgent) ToolsForSkill(resolver Resolver, skillRef string) ([]*Tool, error) {
	if ra == nil {
		return nil, fmt.Errorf("resolved agent is nil")
	}
	res, ok := resolver.ResolveResource(skillRef)
	if !ok {
		return nil, fmt.Errorf("skill %q not found", skillRef)
	}
	skill, ok := res.(*Skill)
	if !ok {
		return nil, fmt.Errorf("resource %q is a %s, not a Skill", skillRef, res.GetKind())
	}

	found := false
	for _, s := range ra.Skills {
		if s.QualifiedName() == skill.QualifiedName() {
			found = true
			break
		}
	}
	if !found {
		agentName := "agent"
		if ra.Agent != nil && ra.Agent.Metadata.Name != "" {
			agentName = ra.Agent.Metadata.Name
		}
		return nil, fmt.Errorf("skill %q is not enabled for agent %q", skillRef, agentName)
	}

	tools, err := resolveToolsForSkill(resolver, skillRef)
	if err != nil {
		return nil, err
	}

	if ra.Agent != nil && ra.Agent.Spec.Policies != nil && ra.Agent.Spec.Policies.Tools != nil {
		tools = filterTools(tools, ra.Agent.Spec.Policies.Tools)
	}

	return tools, nil
}

// ResolveCommand resolves a command for this resolved agent, verifying that the command
// is enabled for the agent (unless the command specifies an explicit agent override)
// and filtering effective tools against the agent's security policies.
func (ra *ResolvedAgent) ResolveCommand(resolver Resolver, ref string) (*ResolvedCommand, error) {
	return resolveCommand(resolver, ra, ref)
}

// ResolveSubagent resolves a subagent for this resolved agent, verifying that the
// referenced agent is present in ra.Subagents (and can be triggered by an agent)
// before returning the fully merged and policy-filtered *ResolvedAgent.
func (ra *ResolvedAgent) ResolveSubagent(resolver Resolver, ref string) (*ResolvedAgent, error) {
	if ra == nil {
		return nil, fmt.Errorf("resolved agent is nil")
	}
	res, ok := resolver.ResolveResource(ref)
	if !ok {
		return nil, fmt.Errorf("subagent %q not found", ref)
	}
	target, ok := res.(*Agent)
	if !ok {
		return nil, fmt.Errorf("resource %q is a %s, not an Agent", ref, res.GetKind())
	}

	found := false
	for _, sa := range ra.Subagents {
		if sa != nil && sa.QualifiedName() == target.QualifiedName() {
			found = true
			break
		}
	}
	if !found {
		agentName := "agent"
		if ra.Agent != nil && ra.Agent.Metadata.Name != "" {
			agentName = ra.Agent.Metadata.Name
		}
		return nil, fmt.Errorf("subagent %q is not enabled for agent %q", ref, agentName)
	}

	return resolveExecutableAgent(resolver, target.QualifiedName())
}

// ResolveModels returns the prioritized list of resolved completion models for this
// agent. When requested is empty, it resolves ra.Agent.Spec.Models (falling back to
// workspace/provider completion defaults). When requested is non-empty and the agent
// declares a non-empty spec.models list, every requested model or alias must be
// permitted by ra.Agent.Spec.Models (either by direct reference/alias match or by
// resolving to one of the agent's allowed models).
func (ra *ResolvedAgent) ResolveModels(resolver Resolver, requested ...string) ([]ResolvedModel, error) {
	if ra == nil || ra.Agent == nil {
		return nil, fmt.Errorf("resolved agent is nil")
	}

	var req []string
	for _, m := range requested {
		if m != "" {
			req = append(req, m)
		}
	}

	if len(req) == 0 {
		return resolver.ResolveModels(ModelRoleCompletion, ra.Agent.Spec.Models...), nil
	}

	if len(ra.Agent.Spec.Models) == 0 {
		return resolver.ResolveModels(ModelRoleCompletion, req...), nil
	}

	allowedResolved := resolver.ResolveModels(ModelRoleCompletion, ra.Agent.Spec.Models...)
	allowedQualified := make(map[string]bool, len(allowedResolved))
	for _, am := range allowedResolved {
		if am.QualifiedID != "" {
			allowedQualified[am.QualifiedID] = true
		}
	}

	agentName := ra.Agent.Metadata.Name
	if agentName == "" {
		agentName = "agent"
	}

	var out []ResolvedModel
	for _, rRef := range req {
		candidates := resolver.ResolveModels(ModelRoleCompletion, rRef)
		if slices.Contains(ra.Agent.Spec.Models, rRef) {
			out = append(out, candidates...)
			continue
		}
		var matched []ResolvedModel
		for _, c := range candidates {
			if allowedQualified[c.QualifiedID] {
				matched = append(matched, c)
			}
		}
		if len(matched) == 0 {
			return nil, fmt.Errorf("model %q is not allowed for agent %q (allowed: %v)", rRef, agentName, ra.Agent.Spec.Models)
		}
		out = append(out, matched...)
	}

	return out, nil
}

// ResolveModel resolves a single completion model for this agent (using the agent's
// default priority order when requested is empty, or validating requested against
// ra.Agent.Spec.Models when provided), returning the first candidate backed by a
// registered ModelProvider.
func (ra *ResolvedAgent) ResolveModel(resolver Resolver, requested ...string) (*ResolvedModel, error) {
	models, err := ra.ResolveModels(resolver, requested...)
	if err != nil {
		return nil, err
	}
	for i := range models {
		if models[i].Provider != nil {
			rm := models[i]
			return &rm, nil
		}
	}
	agentName := "agent"
	if ra != nil && ra.Agent != nil && ra.Agent.Metadata.Name != "" {
		agentName = ra.Agent.Metadata.Name
	}
	return nil, fmt.Errorf("no registered model provider found for agent %q", agentName)
}

// RulesForPath returns all rules in the resolved agent that match targetPath,
// sorted by Rule priority descending, then by name ascending.
func (ra *ResolvedAgent) RulesForPath(targetPath string) []Rule {
	if ra == nil {
		return nil
	}
	var matched []Rule
	for _, rl := range ra.Rules {
		if rl.Spec.Triggers.MatchesPath(targetPath) {
			matched = append(matched, rl)
		}
	}
	return deduplicateRules(matched)
}

// RulesForTool returns all rules in the resolved agent that match toolName,
// sorted by Rule priority descending, then by name ascending.
func (ra *ResolvedAgent) RulesForTool(toolName string) []Rule {
	if ra == nil {
		return nil
	}
	var matched []Rule
	for _, rl := range ra.Rules {
		if rl.Spec.Triggers.MatchesTool(toolName) {
			matched = append(matched, rl)
		}
	}
	return deduplicateRules(matched)
}

func resolveCommand(resolver Resolver, executingAgent *ResolvedAgent, ref string) (*ResolvedCommand, error) {
	res, ok := resolver.ResolveResource(ref)
	if !ok {
		return nil, fmt.Errorf("command %q not found", ref)
	}
	cmd, ok := res.(*Command)
	if !ok {
		return nil, fmt.Errorf("resource %q is a %s, not a Command", ref, res.GetKind())
	}

	// Determine executing agent
	if cmd.Spec.Agent != "" {
		ag, err := resolveExecutableAgent(resolver, cmd.Spec.Agent)
		if err != nil {
			return nil, err
		}
		executingAgent = ag
	} else if executingAgent != nil {
		found := false
		for _, c := range executingAgent.Commands {
			if c.QualifiedName() == cmd.QualifiedName() {
				found = true
				break
			}
		}
		if !found {
			agentName := "agent"
			if executingAgent.Agent != nil && executingAgent.Agent.Metadata.Name != "" {
				agentName = executingAgent.Agent.Metadata.Name
			}
			return nil, fmt.Errorf("command %q is not enabled for agent %q", ref, agentName)
		}
	}

	// 1. Eagerly resolve Skills (cmd.Spec.Skills)
	var skills []Skill
	if containsWildcard(cmd.Spec.Skills) {
		all := resolver.ListResources(QueryOptions{Kinds: []Kind{KindSkill}, Effective: true})
		for _, r := range all {
			if sk, ok := r.(*Skill); ok {
				skills = append(skills, *sk)
			}
		}
	} else if len(cmd.Spec.Skills) > 0 {
		var allSkills []Resource
		for _, sRef := range cmd.Spec.Skills {
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
	}
	skills = deduplicateSkills(skills)

	// 2. Eagerly resolve Rules (cmd.Spec.Rules)
	var rules []Rule
	addRule := func(r *Rule) {
		if r == nil {
			return
		}
		cp := r.DeepCopy()
		cp.Spec.Triggers.Always = true
		rules = append(rules, *cp)
	}

	if containsWildcard(cmd.Spec.Rules) {
		all := resolver.ListResources(QueryOptions{Kinds: []Kind{KindRule}, Effective: true})
		for _, r := range all {
			if rl, ok := r.(*Rule); ok {
				addRule(rl)
			}
		}
	} else if len(cmd.Spec.Rules) > 0 {
		var allRules []Resource
		for _, rRef := range cmd.Spec.Rules {
			if hasGlobMeta(rRef) {
				if allRules == nil {
					allRules = resolver.ListResources(QueryOptions{Kinds: []Kind{KindRule}, Effective: false})
				}
				for _, r := range allRules {
					if rl, ok := r.(*Rule); ok && matchRefPattern(resolver, rRef, r) {
						addRule(rl)
					}
				}
				continue
			}
			r, ok := resolver.ResolveResource(rRef)
			if !ok {
				continue
			}
			if rl, ok := r.(*Rule); ok {
				addRule(rl)
			}
		}
	}
	rules = deduplicateRules(rules)

	// 3. Eagerly resolve Tools (cmd.Spec.Tools + skill.Spec.Tools)
	combinedToolRefs := slices.Clone(cmd.Spec.Tools)
	for _, sk := range skills {
		combinedToolRefs = append(combinedToolRefs, sk.Spec.Tools...)
	}
	tools := resolveToolsFromRefs(resolver, combinedToolRefs)

	if wsPolicy := workspaceToolPolicy(resolver); wsPolicy != nil {
		tools = filterTools(tools, wsPolicy)
	}

	if executingAgent != nil && executingAgent.Agent != nil && executingAgent.Agent.Spec.Policies != nil && executingAgent.Agent.Spec.Policies.Tools != nil {
		tools = filterTools(tools, executingAgent.Agent.Spec.Policies.Tools)
	}

	return &ResolvedCommand{
		Command: cmd,
		Agent:   executingAgent,
		Skills:  skills,
		Rules:   rules,
		Tools:   tools,
	}, nil
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

	// Merge tool policies: child overrides parent actions if non-empty, unions arrays
	if child.Tools.Dangerous != "" {
		merged.Tools.Dangerous = child.Tools.Dangerous
	}
	if child.Tools.OpenWorld != "" {
		merged.Tools.OpenWorld = child.Tools.OpenWorld
	}
	if child.Tools.ReadOnly {
		merged.Tools.ReadOnly = true
	}

	// Union parent and child includes/excludes/ask
	merged.Tools.Include = unionSlices(merged.Tools.Include, child.Tools.Include)
	merged.Tools.Exclude = unionSlices(merged.Tools.Exclude, child.Tools.Exclude)
	merged.Tools.Ask = unionSlices(merged.Tools.Ask, child.Tools.Ask)

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

func workspaceToolPolicy(resolver Resolver) *ToolPolicies {
	var base *Registry
	switch r := resolver.(type) {
	case *Registry:
		base = r
	case *ScopedRegistry:
		if r != nil {
			base = r.base
		}
	}
	if base == nil {
		return nil
	}
	base.mu.RLock()
	defer base.mu.RUnlock()
	if base.workspace != nil && base.workspace.Def != nil && base.workspace.Def.Spec.Policies != nil {
		return base.workspace.Def.Spec.Policies.Tools
	}
	return nil
}

func filterTools(tools []*Tool, policy *ToolPolicies) []*Tool {
	if policy == nil {
		return tools
	}
	var filtered []*Tool
	for _, tool := range tools {
		if policy.EvaluateTool(tool) != ToolPolicyDeny {
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
		if strings.ToLower(t) == TriggerAgent {
			return true
		}
	}
	return false
}

func hasGlobMeta(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func containsWildcard(slice []string) bool {
	return slices.Contains(slice, "*")
}
