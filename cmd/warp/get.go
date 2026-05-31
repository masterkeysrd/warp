package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"github.com/masterkeysrd/warp"
	"github.com/masterkeysrd/warp/internal/fetcher"
	"github.com/masterkeysrd/warp/internal/hasher"
	"gopkg.in/yaml.v3"
)

type exportMock struct {
	kind        string
	name        string
	description string
}

func runGet(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: package source required (e.g. github.com/acme/finance-skills@v1.2.0)")
		os.Exit(1)
	}

	pkgArg := args[0]
	parts := strings.Split(pkgArg, "@")
	source := parts[0]
	version := "latest"
	if len(parts) > 1 {
		version = parts[1]
	}

	fmt.Printf("Fetching plugin %s@%s...\n", source, version)

	cacheDir, err := fetcher.Fetch(source, version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching plugin: %v\n", err)
		os.Exit(1)
	}

	pluginPath := filepath.Join(cacheDir, "PLUGIN.md")
	content, err := os.ReadFile(pluginPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Try PLUGIN.yaml
			pluginPath = filepath.Join(cacheDir, "PLUGIN.yaml")
			content, err = os.ReadFile(pluginPath)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: repository does not contain a valid PLUGIN.md manifest\n")
			os.Exit(1)
		}
	}

	result, err := warp.Parse(pluginPath, string(content))
	if err != nil || result.Kind != warp.KindPlugin {
		fmt.Fprintf(os.Stderr, "Error: failed to parse plugin manifest: %v\n", err)
		os.Exit(1)
	}
	pluginRes := result.Resource.(*warp.Plugin)

	resourceDir := pluginRes.Spec.ResourceDir
	if resourceDir == "" {
		resourceDir = ".agents"
	}
	absResourceDir := filepath.Join(cacheDir, resourceDir)

	// Build a temporary loader to discover actual resources in the exported directory
	provider := warp.NewFSResourceProvider(os.DirFS(absResourceDir))
	tempReg, err := provider.LoadResources()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading resources from plugin: %v\n", err)
		os.Exit(1)
	}

	// Filter tempReg based on pluginRes.Spec.Exports (we'll implement glob filtering later, for now we list all)
	var discoveredExports []exportMock

	for _, a := range tempReg.Agents {
		discoveredExports = append(discoveredExports, exportMock{kind: string(a.Kind), name: a.GetName(), description: a.GetMetadata().Description})
	}
	for _, s := range tempReg.Skills {
		discoveredExports = append(discoveredExports, exportMock{kind: string(s.Kind), name: s.GetName(), description: s.GetMetadata().Description})
	}
	for _, c := range tempReg.Commands {
		discoveredExports = append(discoveredExports, exportMock{kind: string(c.Kind), name: c.GetName(), description: c.GetMetadata().Description})
	}
	for _, p := range tempReg.ModelProviders {
		discoveredExports = append(discoveredExports, exportMock{kind: string(p.Kind), name: p.GetName(), description: p.GetMetadata().Description})
	}
	for _, t := range tempReg.Tools {
		discoveredExports = append(discoveredExports, exportMock{kind: string(t.Kind), name: t.GetName(), description: t.GetMetadata().Description})
	}
	for _, m := range tempReg.MCPs {
		discoveredExports = append(discoveredExports, exportMock{kind: string(m.Kind), name: m.GetName(), description: m.GetMetadata().Description})
	}
	for _, tk := range tempReg.Toolkits {
		discoveredExports = append(discoveredExports, exportMock{kind: string(tk.Kind), name: tk.GetName(), description: tk.GetMetadata().Description})
	}

	fmt.Printf("Plugin '%s' exposes %d resources.\n", source, len(discoveredExports))

	var mode string
	var selectedResources []string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("How do you want to import these resources?").
				Options(
					huh.NewOption("Import All (Expose everything to the workspace)", "all"),
					huh.NewOption("Select Specific Resources", "specific"),
				).
				Value(&mode),
		),
	)

	err = form.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Aborted.")
		os.Exit(1)
	}

	if mode == "specific" {
		// Group by kind
		byKind := make(map[string][]exportMock)
		kindsOrder := []string{"Agent", "Skill", "Command", "Tool", "MCP", "Toolkit", "ModelProvider"}

		for _, exp := range discoveredExports {
			byKind[exp.kind] = append(byKind[exp.kind], exp)
		}

		results := make(map[string]*[]string)
		var groups []*huh.Group

		for _, kind := range kindsOrder {
			resources := byKind[kind]
			if len(resources) == 0 {
				continue
			}

			options := make([]huh.Option[string], len(resources))
			for i, exp := range resources {
				qualified := fmt.Sprintf("%s/%s", exp.kind, exp.name)
				var displayKey string
				if exp.description != "" {
					displayKey = fmt.Sprintf("%-20s  (%s)", exp.name, exp.description)
				} else {
					displayKey = exp.name
				}
				options[i] = huh.NewOption(displayKey, qualified).Selected(true)
			}

			var selected []string
			results[kind] = &selected

			fieldHeight := len(options) + 2
			if fieldHeight > 12 {
				fieldHeight = 12
			}

			group := huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title(fmt.Sprintf("Select %ss to include", kind)).
					Options(options...).
					Value(results[kind]).
					Height(fieldHeight),
			)
			groups = append(groups, group)
		}

		multiForm := huh.NewForm(groups...)

		if err := multiForm.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "Aborted.")
			os.Exit(1)
		}

		for _, kind := range kindsOrder {
			if res, ok := results[kind]; ok {
				selectedResources = append(selectedResources, *res...)
			}
		}
	} else {
		for _, exp := range discoveredExports {
			selectedResources = append(selectedResources, fmt.Sprintf("%s/%s", exp.kind, exp.name))
		}
	}

	// Summarize the selection
	fmt.Println("\n✅ Plugin configured!")
	fmt.Printf("Source: %s\n", source)
	fmt.Printf("Version: %s\n", version)

	if mode == "all" {
		fmt.Println("Imports: All exported resources")
	} else {
		fmt.Printf("Imports: %d selected resources\n", len(selectedResources))
		for _, res := range selectedResources {
			fmt.Printf("  - %s\n", res)
		}
	}

	// Update warp.lock
	dirHash, err := hasher.DirHash(absResourceDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error computing directory hash: %v\n", err)
		os.Exit(1)
	}

	manifestHash, err := hasher.FileHash(pluginPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error computing manifest hash: %v\n", err)
		os.Exit(1)
	}

	lockPath := "warp.lock"
	if err := updateLockFile(lockPath, source, version, dirHash, manifestHash, filepath.Base(pluginPath)); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating warp.lock: %v\n", err)
		os.Exit(1)
	}

	// Update WORKSPACE.md
	wsPath := "WORKSPACE.md"
	wsContent, err := os.ReadFile(wsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create synthetic WORKSPACE.md
			wsContent = []byte("---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: workspace\n---\n")
		} else {
			fmt.Fprintf(os.Stderr, "Error reading WORKSPACE.md: %v\n", err)
			os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "Error parsing WORKSPACE.md front-matter: %v\n", err)
		os.Exit(1)
	}

	// Create new plugin entry
	inferredNamespace := source
	if lastSlash := strings.LastIndex(source, "/"); lastSlash != -1 {
		inferredNamespace = source[lastSlash+1:]
	}

	newPlugin := warp.WorkspacePlugin{
		Source:    source,
		Version:   version,
		Namespace: inferredNamespace,
	}
	if mode == "specific" {
		newPlugin.Imports = &warp.ResourceFilter{
			Include: selectedResources,
		}
	}

	// Find or create 'spec' and 'plugins'
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

	// Marshal new plugin to node
	var pluginNode yaml.Node
	pBytes, _ := yaml.Marshal(newPlugin)
	yaml.Unmarshal(pBytes, &pluginNode)

	pluginsNode.Content = append(pluginsNode.Content, pluginNode.Content[0])

	updatedFrontMatter, _ := yaml.Marshal(&node)
	finalContent := fmt.Sprintf("---\n%s---\n%s", string(updatedFrontMatter), body)

	if err := os.WriteFile(wsPath, []byte(finalContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing WORKSPACE.md: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✅ Plugin installed successfully!")
	fmt.Printf("   - Updated %s\n", wsPath)
	fmt.Printf("   - Updated %s\n", lockPath)
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
