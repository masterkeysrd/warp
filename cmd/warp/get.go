package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/huh/v2"
	"github.com/masterkeysrd/warp"
)

type exportMock struct {
	kind        string
	name        string
	description string
}

func runGet(args []string) {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	autoApprove := fs.Bool("y", false, "Auto-approve imports and installation hooks")
	fs.Parse(args)

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Error: package source required (e.g. github.com/acme/finance-skills@v1.2.0)")
		os.Exit(1)
	}

	pkgArg := fs.Arg(0)
	parts := strings.Split(pkgArg, "@")
	source := parts[0]
	version := "latest"
	if len(parts) > 1 {
		version = parts[1]
	}

	fmt.Printf("Fetching plugin %s@%s...\n", source, version)

	resources, hooks, err := warp.DiscoverPluginResources(source, version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error discovering resources: %v\n", err)
		os.Exit(1)
	}

	// Filter based on pluginRes.Spec.Exports (we'll implement glob filtering later, for now we list all)
	var discoveredExports []exportMock
	for _, r := range resources {
		discoveredExports = append(discoveredExports, exportMock{
			kind:        string(r.Kind),
			name:        r.Name,
			description: r.Description,
		})
	}

	fmt.Printf("Plugin '%s' exposes %d resources.\n", source, len(discoveredExports))

	var mode string = "all"
	var selectedResources []string

	if !*autoApprove {
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
	}

	if mode == "specific" && !*autoApprove {
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

	var importsToInstall []string
	if mode == "specific" {
		importsToInstall = selectedResources
	}

	if err := warp.InstallPlugin(".", source, version, importsToInstall); err != nil {
		fmt.Fprintf(os.Stderr, "Error installing plugin: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✅ Plugin installed successfully!")
	fmt.Printf("   - Updated WORKSPACE.md\n")
	fmt.Printf("   - Updated warp.lock\n")

	if hooks != nil && len(hooks.PostInstall) > 0 {
		fmt.Println("\n📦 This plugin has post-install hooks.")
		for _, cmd := range hooks.PostInstall {
			fmt.Printf("  - %s\n", strings.Join(cmd, " "))
		}

		approveHooks := *autoApprove
		if !approveHooks {
			err = huh.NewConfirm().
				Title("Do you want to run these setup commands?").
				Value(&approveHooks).
				Run()
			if err != nil {
				fmt.Fprintln(os.Stderr, "Aborted.")
				os.Exit(1)
			}
		}

		if approveHooks {
			for _, cmdArgs := range hooks.PostInstall {
				if len(cmdArgs) == 0 {
					continue
				}
				fmt.Printf("Running: %s...\n", strings.Join(cmdArgs, " "))
				cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					fmt.Fprintf(os.Stderr, "Error running hook: %v\n", err)
					os.Exit(1)
				}
			}
			fmt.Println("✅ Setup hooks complete!")
		}
	}
}
