package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/masterkeysrd/warp"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: warp [command] [options]\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  validate [path]            Validate a WARP workspace or resource directory\n")
		fmt.Fprintf(os.Stderr, "  list [path]                List projects and resources in a WARP workspace\n")
		fmt.Fprintf(os.Stderr, "  render <ref> [args...]     Render a resource's instructions template\n")
		fmt.Fprintf(os.Stderr, "  fmt [-w] <file...>         Format WARP resource files canonically\n")
		fmt.Fprintf(os.Stderr, "  get <package>              Get and install a WARP plugin\n\n")
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
		if err := executeValidate(os.Stdout, flag.Args()[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
	case "list":
		if err := executeList(os.Stdout, flag.Args()[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
	case "render":
		if err := executeRender(os.Stdout, flag.Args()[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
	case "fmt":
		if err := executeFmt(os.Stdout, flag.Args()[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
	case "get":
		runGet(flag.Args()[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		flag.Usage()
		os.Exit(1)
	}
}

func executeValidate(out io.Writer, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	fmt.Fprintf(out, "🔍 Validating WARP workspace at: %s\n", absPath)

	reg, err := warp.LoadWorkspace(absPath)
	if err != nil {
		return fmt.Errorf("Load Error: %w", err)
	}

	for _, w := range reg.Warnings() {
		fmt.Fprintf(out, "⚠️  Warning: %s\n", w)
	}

	if err := reg.Validate(); err != nil {
		return fmt.Errorf("Validation Error: %w", err)
	}

	var contexts, agents, skills, commands, decisions, providers, tools, mcps, toolkits int
	for _, res := range reg.Resources() {
		switch res.GetKind() {
		case warp.KindContext:
			contexts++
		case warp.KindAgent:
			agents++
		case warp.KindSkill:
			skills++
		case warp.KindCommand:
			commands++
		case warp.KindDecision:
			decisions++
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

	fmt.Fprintln(out, "\n✅ Validation Successful!")
	fmt.Fprintf(out, "   Discovered (%d projects):\n", len(reg.ListProjects()))
	fmt.Fprintf(out, "   - %d Contexts\n", contexts)
	fmt.Fprintf(out, "   - %d Agents\n", agents)
	fmt.Fprintf(out, "   - %d Skills\n", skills)
	fmt.Fprintf(out, "   - %d Commands\n", commands)
	fmt.Fprintf(out, "   - %d Decisions\n", decisions)
	fmt.Fprintf(out, "   - %d Model Providers\n", providers)
	fmt.Fprintf(out, "   - %d Tools\n", tools)
	fmt.Fprintf(out, "   - %d MCP Servers\n", mcps)
	fmt.Fprintf(out, "   - %d Toolkits\n", toolkits)
	return nil
}

func executeList(out io.Writer, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	reg, err := warp.LoadWorkspace(absPath)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Projects:\n")
	for _, p := range reg.ListProjects() {
		fmt.Fprintf(out, "  - %s (%s)\n", p.Name, p.Path)
	}

	fmt.Fprintf(out, "\nResources:\n")
	for _, r := range reg.Resources() {
		desc := r.GetMetadata().Description
		if desc != "" {
			fmt.Fprintf(out, "  - %-35s %s\n", r.QualifiedName(), desc)
		} else {
			fmt.Fprintf(out, "  - %s\n", r.QualifiedName())
		}
	}
	return nil
}

func executeRender(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	dir := fs.String("dir", ".", "Workspace or project directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("resource reference required (e.g. warp render my-agent)")
	}
	ref := fs.Arg(0)
	posArgs := fs.Args()[1:]

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	reg, err := warp.LoadWorkspace(absDir)
	if err != nil {
		return err
	}

	proj, _ := reg.ProjectFromPath(absDir)
	var resolver warp.Resolver = reg
	if proj != nil {
		resolver = reg.Project(proj.Name)
	}

	res, ok := resolver.ResolveResource(ref)
	if !ok {
		return fmt.Errorf("resource %q not found", ref)
	}

	var rendered string
	switch v := res.(type) {
	case *warp.Agent:
		var resolved *warp.ResolvedAgent
		if proj != nil {
			resolved, _ = reg.Project(proj.Name).ResolveAgent(ref)
		} else {
			resolved, _ = reg.ResolveAgent(ref)
		}
		rendered, err = v.Render(&warp.AgentRenderOptions{
			Workspace: reg.WorkspaceSpec(),
			Project:   proj,
			Resolved:  resolved,
		})
	case *warp.Command:
		rendered, err = v.Render(&warp.CommandRenderOptions{
			Workspace: reg.WorkspaceSpec(),
			Project:   proj,
			Args:      posArgs,
		})
	case *warp.Decision:
		rendered, err = v.Render(&warp.DecisionRenderOptions{
			Workspace: reg.WorkspaceSpec(),
			Project:   proj,
			Args:      posArgs,
		})
	case *warp.Skill:
		rendered, err = v.Render(&warp.SkillRenderOptions{
			Workspace: reg.WorkspaceSpec(),
			Project:   proj,
		})
	case *warp.Context:
		rendered, err = v.Render(&warp.ContextRenderOptions{
			Workspace: reg.WorkspaceSpec(),
			Project:   proj,
		})
	default:
		rendered, err = warp.Render(res, &warp.RenderOptions{Args: posArgs})
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(out, rendered)
	return nil
}

func executeFmt(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	writeInPlace := fs.Bool("w", false, "Write result to (source) file instead of stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("at least one file path is required")
	}

	for _, filePath := range fs.Args() {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", filePath, err)
		}
		parsed, err := warp.Parse(filePath, string(data))
		if err != nil {
			return fmt.Errorf("parse %s: %w", filePath, err)
		}
		res, ok := parsed.Resource.(warp.Resource)
		if !ok {
			return fmt.Errorf("%s is not a formattable resource", filePath)
		}
		formatted, err := warp.Format(res)
		if err != nil {
			return fmt.Errorf("format %s: %w", filePath, err)
		}
		if *writeInPlace {
			if err := os.WriteFile(filePath, formatted, 0644); err != nil {
				return fmt.Errorf("write %s: %w", filePath, err)
			}
		} else {
			out.Write(formatted)
		}
	}
	return nil
}
