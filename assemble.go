package warp

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
)

// NamespacedProvider loads resources for a specific namespace with a defined
// priority. When multiple providers supply a resource with the same qualified
// name, the one with the highest Priority wins.
type NamespacedProvider interface {
	// Namespace returns the namespace all resources from this provider belong to.
	Namespace() string
	// Priority returns the numeric priority. Higher values beat lower ones.
	Priority() int
	// GetResources returns all resources this provider contributes.
	GetResources(ctx context.Context) ([]Resource, error)
}

// NamespacedFSProvider wraps an fs.FS and presents its Agent, Skill, Command,
// ModelProvider, Tool, MCP, and Toolkit files as a NamespacedProvider.
type NamespacedFSProvider struct {
	fsys      fs.FS
	namespace string
	priority  int
}

// NewNamespacedFSProvider returns a provider that loads resources from fsys
// and tags them all with the given namespace and priority.
func NewNamespacedFSProvider(fsys fs.FS, namespace string, priority int) *NamespacedFSProvider {
	return &NamespacedFSProvider{fsys: fsys, namespace: namespace, priority: priority}
}

// Namespace implements NamespacedProvider.
func (p *NamespacedFSProvider) Namespace() string { return p.namespace }

// Priority implements NamespacedProvider.
func (p *NamespacedFSProvider) Priority() int { return p.priority }

// GetResources implements NamespacedProvider.
func (p *NamespacedFSProvider) GetResources(ctx context.Context) ([]Resource, error) {
	if p.fsys == nil {
		return nil, fmt.Errorf("namespaced provider %q: filesystem is nil", p.namespace)
	}

	rs := newResourceSet()
	if err := loadAgentsDir(
		p.fsys,
		rs.Agents,
		rs.Skills,
		rs.Commands,
		rs.ModelProviders,
		rs.Tools,
		rs.MCPs,
		rs.Toolkits,
	); err != nil {
		return nil, fmt.Errorf("namespaced provider %q: %w", p.namespace, err)
	}

	var out []Resource
	for _, a := range rs.Agents {
		a.Namespace = p.namespace
		out = append(out, a)
	}
	for _, s := range rs.Skills {
		s.Namespace = p.namespace
		out = append(out, s)
	}
	for _, c := range rs.Commands {
		c.Namespace = p.namespace
		out = append(out, c)
	}
	for _, mp := range rs.ModelProviders {
		mp.Namespace = p.namespace
		out = append(out, mp)
	}
	for _, t := range rs.Tools {
		t.Namespace = p.namespace
		out = append(out, t)
	}
	for _, m := range rs.MCPs {
		m.Namespace = p.namespace
		out = append(out, m)
	}
	for _, tk := range rs.Toolkits {
		tk.Namespace = p.namespace
		out = append(out, tk)
	}
	return out, nil
}

// Assemble builds a Registry populated from an ordered list of
// NamespacedProviders. Providers are applied from lowest priority to highest;
// a higher-priority provider's resource overwrites any same-qualified-name
// entry from a lower-priority provider.
//
// The returned Registry has no filesystem-derived Projects, Agents, etc. —
// those maps remain nil. Use LoadWorkspace for the full loading path.
func Assemble(ctx context.Context, providers []NamespacedProvider) (*Registry, error) {
	// Sort ascending so we process lower-priority providers first.
	sorted := make([]NamespacedProvider, len(providers))
	copy(sorted, providers)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority() < sorted[j].Priority()
	})

	reg := NewRegistry(nil)

	for _, p := range sorted {
		resources, err := p.GetResources(ctx)
		if err != nil {
			return nil, fmt.Errorf("assemble: provider %q: %w", p.Namespace(), err)
		}
		for _, r := range resources {
			reg.set(r.QualifiedName(), r)
		}
	}

	return reg, nil
}
