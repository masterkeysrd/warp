package warp

import (
	"fmt"
	"os"
	"path/filepath"
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
	cacheDir, err := fetcher.Fetch(source, version)
	if err != nil {
		return fmt.Errorf("fetching plugin: %w", err)
	}

	pluginPath := filepath.Join(cacheDir, "PLUGIN.md")
	content, err := os.ReadFile(pluginPath)
	if err != nil {
		if os.IsNotExist(err) {
			pluginPath = filepath.Join(cacheDir, "PLUGIN.yaml")
			content, err = os.ReadFile(pluginPath)
		}
		if err != nil {
			return fmt.Errorf("repository does not contain a valid PLUGIN.md or PLUGIN.yaml manifest: %w", err)
		}
	}

	result, err := Parse(pluginPath, string(content))
	if err != nil || result.Kind != KindPlugin {
		return fmt.Errorf("failed to parse plugin manifest: %w", err)
	}
	pluginRes := result.Resource.(*Plugin)

	resourceDir := pluginRes.Spec.ResourceDir
	if resourceDir == "" {
		resourceDir = ".agents"
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

	inferredNamespace := source
	if lastSlash := strings.LastIndex(source, "/"); lastSlash != -1 {
		inferredNamespace = source[lastSlash+1:]
	}

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

	wsPath := filepath.Join(workspaceDir, "WORKSPACE.md")
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
	}

	var pluginNode yaml.Node
	pBytes, _ := yaml.Marshal(newPlugin)
	yaml.Unmarshal(pBytes, &pluginNode)

	pluginsNode.Content = append(pluginsNode.Content, pluginNode.Content[0])

	updatedFrontMatter, _ := yaml.Marshal(&node)
	finalContent := fmt.Sprintf("---\n%s---\n%s", string(updatedFrontMatter), body)

	if err := os.WriteFile(wsPath, []byte(finalContent), 0644); err != nil {
		return fmt.Errorf("writing WORKSPACE.md: %w", err)
	}

	return nil
}

func updateLockFile(lockPath, source, version, dirHash, manifestHash, manifestName string) error {
	existing, err := os.ReadFile(lockPath)
	locks := make(map[string]string)
	var keysOrder []string

	if err == nil {
		lines := strings.Split(string(existing), "\n")
		for _, line := range lines {
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

// DiscoveredResource holds information about a discovered resource in a plugin.
type DiscoveredResource struct {
	Kind        Kind
	Name        string
	Description string
}

// DiscoverPluginResources fetches the plugin at the given source and version,
// and returns the list of all resources exposed by it.
func DiscoverPluginResources(source, version string) ([]DiscoveredResource, error) {
	cacheDir, err := fetcher.Fetch(source, version)
	if err != nil {
		return nil, fmt.Errorf("fetching plugin: %w", err)
	}

	pluginPath := filepath.Join(cacheDir, "PLUGIN.md")
	content, err := os.ReadFile(pluginPath)
	if err != nil {
		if os.IsNotExist(err) {
			pluginPath = filepath.Join(cacheDir, "PLUGIN.yaml")
			content, err = os.ReadFile(pluginPath)
		}
		if err != nil {
			return nil, fmt.Errorf("repository does not contain a valid PLUGIN.md or PLUGIN.yaml manifest: %w", err)
		}
	}

	result, err := Parse(pluginPath, string(content))
	if err != nil || result.Kind != KindPlugin {
		return nil, fmt.Errorf("failed to parse plugin manifest: %w", err)
	}
	pluginRes := result.Resource.(*Plugin)

	resourceDir := pluginRes.Spec.ResourceDir
	if resourceDir == "" {
		resourceDir = ".agents"
	}
	absResourceDir := filepath.Join(cacheDir, resourceDir)

	provider := NewFSResourceProvider(os.DirFS(absResourceDir))
	tempReg, err := provider.LoadResources()
	if err != nil {
		return nil, fmt.Errorf("loading resources from plugin: %w", err)
	}

	var resources []DiscoveredResource

	appendResource := func(k Kind, name, desc string) {
		resources = append(resources, DiscoveredResource{
			Kind:        k,
			Name:        name,
			Description: desc,
		})
	}

	for _, a := range tempReg.Agents {
		appendResource(a.Kind, a.GetName(), a.GetMetadata().Description)
	}
	for _, s := range tempReg.Skills {
		appendResource(s.Kind, s.GetName(), s.GetMetadata().Description)
	}
	for _, c := range tempReg.Commands {
		appendResource(c.Kind, c.GetName(), c.GetMetadata().Description)
	}
	for _, p := range tempReg.ModelProviders {
		appendResource(p.Kind, p.GetName(), p.GetMetadata().Description)
	}
	for _, t := range tempReg.Tools {
		appendResource(t.Kind, t.GetName(), t.GetMetadata().Description)
	}
	for _, m := range tempReg.MCPs {
		appendResource(m.Kind, m.GetName(), m.GetMetadata().Description)
	}
	for _, tk := range tempReg.Toolkits {
		appendResource(tk.Kind, tk.GetName(), tk.GetMetadata().Description)
	}

	return resources, nil
}
