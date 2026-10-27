package warp

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/masterkeysrd/warp/internal/fetcher"
	"github.com/masterkeysrd/warp/internal/hasher"
	"gopkg.in/yaml.v3"
)

// InstallPlugin downloads a plugin, computes its hashes, writes them to warp.lock,
// and registers the plugin in WORKSPACE.md.
//
// workspaceDir is the root directory of the workspace (containing WORKSPACE.md).
// source is the package source (e.g. "github.com/acme/finance-skills").
// version is the version (e.g. "latest" or a tag name).
// imports is the list of resource qualified names to import (e.g. ["Skill/hello"]).
// If imports is empty/nil, all exported resources are imported.
func InstallPlugin(workspaceDir, source, version string, imports []string) error {
	if version == "" {
		version = "latest"
	}
	resolvedSource := resolvePluginSource(workspaceDir, source)
	cacheDir, err := fetcher.Fetch(resolvedSource, version)
	if err != nil {
		return fmt.Errorf("fetching plugin: %w", err)
	}

	pluginRes, pluginPath, err := readPluginManifest(cacheDir)
	if err != nil {
		return err
	}

	resourceDir := pluginRes.Spec.ResourceDir
	if resourceDir == "" {
		resourceDir = DefaultAgentsDir
	}
	absResourceDir := filepath.Join(cacheDir, resourceDir)

	dirHash, err := hasher.DirHash(absResourceDir)
	if err != nil {
		return fmt.Errorf("computing directory hash: %w", err)
	}

	manifestHash, err := hasher.FileHash(pluginPath)
	if err != nil {
		return fmt.Errorf("computing manifest hash: %w", err)
	}

	lockPath := filepath.Join(workspaceDir, "warp.lock")
	if err := updateLockFile(lockPath, source, version, dirHash, manifestHash, filepath.Base(pluginPath)); err != nil {
		return fmt.Errorf("updating warp.lock: %w", err)
	}

	inferredNamespace := inferPluginNamespace(source)

	newPlugin := WorkspacePlugin{
		Source:    source,
		Version:   version,
		Namespace: inferredNamespace,
	}
	if len(imports) > 0 {
		newPlugin.Imports = &ResourceFilter{
			Include: imports,
		}
	}

	wsPath := filepath.Join(workspaceDir, WorkspaceFileName)
	wsContent, err := os.ReadFile(wsPath)
	if err != nil {
		if os.IsNotExist(err) {
			wsContent = []byte("---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: workspace\n---\n")
		} else {
			return fmt.Errorf("reading WORKSPACE.md: %w", err)
		}
	}

	wsParts := strings.SplitN(string(wsContent), "---", 3)
	var frontMatter, body string
	if len(wsParts) < 3 {
		frontMatter = "apiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: workspace"
		body = strings.TrimSpace(string(wsContent))
	} else {
		frontMatter = wsParts[1]
		body = wsParts[2]
	}

	var node yaml.Node
	if err = yaml.Unmarshal([]byte(frontMatter), &node); err != nil {
		return fmt.Errorf("parsing WORKSPACE.md front-matter: %w", err)
	}

	root := node.Content[0]
	var specNode *yaml.Node
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "spec" {
			specNode = root.Content[i+1]
			break
		}
	}

	if specNode == nil {
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "spec"}, &yaml.Node{Kind: yaml.MappingNode})
		specNode = root.Content[len(root.Content)-1]
	} else if specNode.Kind != yaml.MappingNode {
		specNode.Kind = yaml.MappingNode
		specNode.Tag = "!!map"
		specNode.Value = ""
	}

	var pluginsNode *yaml.Node
	for i := 0; i < len(specNode.Content); i += 2 {
		if specNode.Content[i].Value == "plugins" {
			pluginsNode = specNode.Content[i+1]
			break
		}
	}

	if pluginsNode == nil {
		specNode.Content = append(specNode.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "plugins"}, &yaml.Node{Kind: yaml.SequenceNode})
		pluginsNode = specNode.Content[len(specNode.Content)-1]
	} else if pluginsNode.Kind != yaml.SequenceNode {
		pluginsNode.Kind = yaml.SequenceNode
		pluginsNode.Tag = "!!seq"
		pluginsNode.Value = ""
	}

	var pluginNode yaml.Node
	pBytes, _ := yaml.Marshal(newPlugin)
	yaml.Unmarshal(pBytes, &pluginNode)

	var existingPluginIndex = -1
	for idx, item := range pluginsNode.Content {
		if item.Kind == yaml.MappingNode {
			for k := 0; k < len(item.Content); k += 2 {
				if item.Content[k].Value == "source" && item.Content[k+1].Value == newPlugin.Source {
					existingPluginIndex = idx
					break
				}
			}
		}
	}

	if existingPluginIndex != -1 {
		pluginsNode.Content[existingPluginIndex] = pluginNode.Content[0]
	} else {
		pluginsNode.Content = append(pluginsNode.Content, pluginNode.Content[0])
	}

	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&node); err != nil {
		return fmt.Errorf("marshaling WORKSPACE.md front-matter: %w", err)
	}
	encoder.Close()

	updatedFrontMatter := buf.String()
	// Encode adds a trailing newline, so we don't need a \n before the second ---
	finalContent := fmt.Sprintf("---\n%s---\n%s", updatedFrontMatter, body)

	if err := os.WriteFile(wsPath, []byte(finalContent), 0644); err != nil {
		return fmt.Errorf("writing WORKSPACE.md: %w", err)
	}

	return nil
}

func resolvePluginSource(workspaceDir, source string) string {
	if strings.HasPrefix(source, ".") && workspaceDir != "" {
		return filepath.Join(workspaceDir, source)
	}
	return source
}

func readPluginManifest(cacheDir string) (*Plugin, string, error) {
	pluginPath := filepath.Join(cacheDir, "PLUGIN.md")
	content, err := os.ReadFile(pluginPath)
	if err != nil {
		if os.IsNotExist(err) {
			pluginPath = filepath.Join(cacheDir, "PLUGIN.yaml")
			content, err = os.ReadFile(pluginPath)
		}
		if err != nil {
			return nil, "", fmt.Errorf("repository does not contain a valid PLUGIN.md or PLUGIN.yaml manifest: %w", err)
		}
	}

	result, err := Parse(pluginPath, string(content))
	if err != nil || result.Kind != KindPlugin {
		return nil, "", fmt.Errorf("failed to parse plugin manifest: %w", err)
	}
	return result.Resource.(*Plugin), pluginPath, nil
}

func parseLockFile(lockPath string) (map[string]string, []string) {
	existing, err := os.ReadFile(lockPath)
	locks := make(map[string]string)
	var keysOrder []string

	if err == nil {
		lines := strings.SplitSeq(string(existing), "\n")
		for line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				key := parts[0] + " " + parts[1]
				val := parts[2]
				if _, exists := locks[key]; !exists {
					keysOrder = append(keysOrder, key)
				}
				locks[key] = val
			}
		}
	}
	return locks, keysOrder
}

func updateLockFile(lockPath, source, version, dirHash, manifestHash, manifestName string) error {
	locks, keysOrder := parseLockFile(lockPath)

	key1 := fmt.Sprintf("%s %s", source, version)
	val1 := dirHash
	if _, exists := locks[key1]; !exists {
		keysOrder = append(keysOrder, key1)
	}
	locks[key1] = val1

	key2 := fmt.Sprintf("%s %s/%s", source, version, manifestName)
	val2 := manifestHash
	if _, exists := locks[key2]; !exists {
		keysOrder = append(keysOrder, key2)
	}
	locks[key2] = val2

	var sb strings.Builder
	sb.WriteString("# This file is automatically generated by warp. DO NOT EDIT.\n")
	for _, key := range keysOrder {
		sb.WriteString(fmt.Sprintf("%s %s\n", key, locks[key]))
	}

	return os.WriteFile(lockPath, []byte(sb.String()), 0644)
}

func matchesPluginExport(exports []string, relPath string, k Kind, name string) bool {
	if len(exports) == 0 {
		return true
	}
	kindName := string(k) + "/" + name
	relNoExt := strings.TrimSuffix(relPath, path.Ext(relPath))
	for _, pat := range exports {
		if pat == "*" {
			return true
		}
		for _, candidate := range []string{name, kindName, strings.ToLower(kindName), relPath, relNoExt} {
			if m, _ := path.Match(pat, candidate); m {
				return true
			}
			if m, _ := path.Match(strings.ToLower(pat), strings.ToLower(candidate)); m {
				return true
			}
		}
	}
	return false
}

func matchesPluginImport(filter *ResourceFilter, ns string, relPath string, k Kind, name string) bool {
	if filter == nil {
		return true
	}
	kindName := string(k) + "/" + name
	nsKindName := MakeQualifiedName(ns, k, name)
	nsName := ns + "/" + name
	relNoExt := strings.TrimSuffix(relPath, path.Ext(relPath))
	candidates := []string{name, kindName, nsKindName, nsName, relPath, relNoExt}

	matchAny := func(patterns []string) bool {
		for _, pat := range patterns {
			if pat == "*" {
				return true
			}
			for _, c := range candidates {
				if m, _ := path.Match(pat, c); m {
					return true
				}
				if m, _ := path.Match(strings.ToLower(pat), strings.ToLower(c)); m {
					return true
				}
			}
		}
		return false
	}

	if len(filter.Include) > 0 && !matchAny(filter.Include) {
		return false
	}
	if len(filter.Exclude) > 0 && matchAny(filter.Exclude) {
		return false
	}
	return true
}

// DiscoveredResource holds information about a discovered resource in a plugin.
type DiscoveredResource struct {
	Kind        Kind
	Name        string
	Description string
}

// DiscoverPluginResources fetches the plugin at the given source and version,
// and returns the list of all resources exposed by it (filtered by the plugin's
// spec.exports).
func DiscoverPluginResources(source, version string) ([]DiscoveredResource, *PluginHooks, error) {
	if version == "" {
		version = "latest"
	}
	cacheDir, err := fetcher.Fetch(source, version)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching plugin: %w", err)
	}

	pluginRes, _, err := readPluginManifest(cacheDir)
	if err != nil {
		return nil, nil, err
	}

	resourceDir := pluginRes.Spec.ResourceDir
	if resourceDir == "" {
		resourceDir = DefaultAgentsDir
	}
	absResourceDir := filepath.Join(cacheDir, resourceDir)

	provider := NewFSResourceProvider(os.DirFS(absResourceDir), absResourceDir)
	tempReg, err := provider.LoadResources()
	if err != nil {
		return nil, nil, fmt.Errorf("loading resources from plugin: %w", err)
	}

	var resources []DiscoveredResource

	appendResource := func(relPath string, k Kind, name, desc string) {
		if !matchesPluginExport(pluginRes.Spec.Exports, relPath, k, name) {
			return
		}
		resources = append(resources, DiscoveredResource{
			Kind:        k,
			Name:        name,
			Description: desc,
		})
	}

	for p, a := range tempReg.Agents {
		appendResource(p, a.Kind, a.GetName(), a.GetMetadata().Description)
	}
	for p, s := range tempReg.Skills {
		appendResource(p, s.Kind, s.GetName(), s.GetMetadata().Description)
	}
	for p, c := range tempReg.Commands {
		appendResource(p, c.Kind, c.GetName(), c.GetMetadata().Description)
	}
	for p, d := range tempReg.Decisions {
		appendResource(p, d.Kind, d.GetName(), d.GetMetadata().Description)
	}
	for p, rl := range tempReg.Rules {
		appendResource(p, rl.Kind, rl.GetName(), rl.GetMetadata().Description)
	}
	for p, mp := range tempReg.ModelProviders {
		appendResource(p, mp.Kind, mp.GetName(), mp.GetMetadata().Description)
	}
	for p, t := range tempReg.Tools {
		appendResource(p, t.Kind, t.GetName(), t.GetMetadata().Description)
	}
	for p, m := range tempReg.MCPs {
		appendResource(p, m.Kind, m.GetName(), m.GetMetadata().Description)
	}
	for p, tk := range tempReg.Toolkits {
		appendResource(p, tk.Kind, tk.GetName(), tk.GetMetadata().Description)
	}

	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Kind != resources[j].Kind {
			return resources[i].Kind < resources[j].Kind
		}
		return resources[i].Name < resources[j].Name
	})

	return resources, pluginRes.Spec.Hooks, nil
}
