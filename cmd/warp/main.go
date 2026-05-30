package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/masterkeysrd/warp"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: warp [command] [options]\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  validate [path]    Validate a WARP workspace or resource directory\n\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	command := flag.Arg(0)
	switch command {
	case "validate":
		runValidate(flag.Args()[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		flag.Usage()
		os.Exit(1)
	}
}

func runValidate(args []string) {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to get absolute path: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🔍 Validating WARP workspace at: %s\n", absPath)

	reg, err := warp.LoadWorkspace(absPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Load Error: %v\n", err)
		os.Exit(1)
	}

	if err := reg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Validation Error: %v\n", err)
		os.Exit(1)
	}

	// Success! Print summary
	agents := 0
	skills := 0
	commands := 0
	providers := 0
	tools := 0
	mcps := 0
	toolkits := 0

	for _, res := range reg.Resources() {
		switch res.GetKind() {
		case warp.KindAgent:
			agents++
		case warp.KindSkill:
			skills++
		case warp.KindCommand:
			commands++
		case warp.KindModelProvider:
			providers++
		case warp.KindTool:
			tools++
		case warp.KindMCP:
			mcps++
		case warp.KindToolkit:
			toolkits++
		}
	}

	fmt.Println("\n✅ Validation Successful!")
	fmt.Printf("   Discovered:\n")
	fmt.Printf("   - %d Agents\n", agents)
	fmt.Printf("   - %d Skills\n", skills)
	fmt.Printf("   - %d Commands\n", commands)
	fmt.Printf("   - %d Model Providers\n", providers)
	fmt.Printf("   - %d Tools\n", tools)
	fmt.Printf("   - %d MCP Servers\n", mcps)
	fmt.Printf("   - %d Toolkits\n", toolkits)
}
