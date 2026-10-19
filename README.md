# warp

The `warp` package implements the **Workspace Agent Resource Protocol (WARP)** — a
provider-agnostic, declarative format for defining AI agents and their
supporting resources.

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

Warp is not tied to any particular LLM provider, orchestration framework, or
runtime. Any tool that understands the format can load and execute the same
resource files unchanged.

## Getting Started

To add `warp` to your Go project:

```bash
go get github.com/masterkeysrd/warp
```

## File Format

Resources can be authored either as Markdown files with YAML front-matter or as pure `.yaml` / `.yml` files:

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: my-agent
  description: A helpful assistant.
spec:
  models: ["gpt-4o"]
  temperature: 0.7
  skills:
    - finance
  commands:
    - report
  tools:
    - git-toolkit
---

# My Agent

You are a helpful assistant that specializes in financial analysis...
```

## Resource Kinds

| Kind            | Purpose |
|-----------------|---------|
| `Workspace`     | Root authority for the session (`WORKSPACE.md`); declares active projects, plugins, and workspace-wide policies. |
| `Context`       | Identity, instructions, and explicit resource references for a project scope (`AGENTS.md`). |
| `Plugin`        | Manifest declaring a plugin's identity and exported resources (`PLUGIN.md`). |
| `Agent`         | An autonomous agent with LLM configuration, inheritance (`extends`), policies, and references to skills, commands, tools/toolkits, MCPs, and subagents. |
| `Skill`         | A bundle of expertise guidelines for a specific domain. Agents load skills to adopt a persona or follow conventions. |
| `Command`       | A discrete, reusable operation an agent can invoke (e.g. "generate a report"). |
| `Decision`      | A probabilistic decision evaluation defining structured questions (`predicate`, `choice`, `score`) and an evaluation rubric. |
| `ModelProvider` | Configuration for an LLM provider (API key env var, base URL, available models, default models, thinking config). |
| `Tool`          | A custom tool definition (input schema, execution binding, and safety annotations). |
| `Toolkit`       | A collection of referenced (`$ref`) or inline `Tool` definitions that can be attached to an `Agent` as a unit. |
| `MCP`           | A Model Context Protocol server configuration (`stdio` or `sse`). |

## Loading & Resolving Resources

### Load a Workspace

```go
reg, err := warp.LoadWorkspace(".")
if err != nil {
    log.Fatal(err)
}

if err := reg.Validate(); err != nil {
    log.Fatal(err)
}
```

### Load a Single Resource Directory

```go
reg, err := warp.LoadDefault() // or warp.Load("/path/to/.agents")
```

### Scoped Resolution

```go
// Locate the active project owning a subdirectory path
proj, err := reg.ProjectFromPath("/path/to/workspace/services/api/cmd")
if err != nil {
    log.Fatal(err)
}

// Scope the registry to that project
scoped := reg.Project(proj.Name)

// Resolve an executable Agent (evaluates inheritance, Toolkits, policies, and globs)
resolved, err := scoped.ResolveAgent("my-agent")
```

### Programmatic Plugin Discovery & Installation

```go
// Discover resources exposed by a plugin before installing it.
resources, err := warp.DiscoverPluginResources("github.com/acme/finance-skills", "v1.2.0")
for _, r := range resources {
    fmt.Printf("Resource: %s/%s (%s)\n", r.Kind, r.Name, r.Description)
}

// Install a plugin into the workspace at the current directory.
err = warp.InstallPlugin(".", "github.com/acme/finance-skills", "v1.2.0", []string{"Skill/hello"})
```

## Directory Layout

The default resource directory is `.agents`. Inside it, resources are organized
by kind into sub-directories:

```
WORKSPACE.md          # Optional workspace manifest
AGENTS.md             # Project context instructions (kind: Context)
.agents/
├── agents/           # Agent definitions (or defs/)
│   └── analyst.md    # kind: Agent
├── commands/         # Command definitions
│   └── report.md     # kind: Command
├── decisions/        # Decision definitions
│   └── triage.md     # kind: Decision
├── skills/           # Skill definitions
│   └── finance.md    # kind: Skill
├── tools/            # Tool definitions (kind: Tool)
├── toolkits/         # Toolkit definitions (kind: Toolkit)
├── mcps/             # MCP server definitions (kind: MCP)
└── providers/        # Model Provider definitions (kind: ModelProvider)
```

## API Reference

- **`Registry` / `ScopedRegistry`** – multi-tenant resource store supporting 3-part qualified names (`namespace/Kind/name`), short-name resolution, and deterministic listing (`ListResources`, `ListProjects`, `Resources`).
  - `Project(name string) *ScopedRegistry` / `ProjectFromPath(path string) (*Project, error)` — scopes resolution to a project namespace.
  - `ResolveAgent(ref string) (*ResolvedAgent, error)` — resolves an executable Agent (evaluating inheritance, Toolkits, policies, and globs).
  - `Validate() error` — validates all resources, inheritance chains, and cross-references across the workspace.
- **`LoadWorkspace(dir string, opts ...LoadOption) (*Registry, error)`** — discovers and loads a workspace starting from `dir`.
- **`Load(dir string) (*Registry, error)`** — loads a single `.agents` resource directory.
- **`LoadDefault() (*Registry, error)`** — loads resources from the default `.agents` directory in the current working directory.
- **`Parse(filename, content string) (*ParseResult, error)`** — parses a single WARP Markdown or YAML string into a typed resource.
- **`Format(r Resource) ([]byte, error)`** — serializes a WARP resource into canonical YAML or Markdown front-matter format.
- **`DiscoverPluginResources(source, version string) ([]DiscoveredResource, error)`** — fetches a plugin at the given source/version and lists its exported resources.
- **`InstallPlugin(workspaceDir, source, version string, imports []string) error`** — downloads a plugin, records its SHA-256 hash in `warp.lock`, and registers it in `WORKSPACE.md`.

## CLI Tool

Warp includes a CLI tool (`cmd/warp`) for validating, inspecting, rendering, formatting, and installing plugins in a workspace.

```bash
# Build the CLI
go build -o warp ./cmd/warp

# Validate a workspace (displays warnings and validation errors)
./warp validate [path]

# List all discovered projects and resources
./warp list [path]

# Render a resource's instructions template (Agent, Command, Skill, Context)
./warp render [-dir ./services/api] my-agent [args...]

# Canonicalize YAML and Markdown front-matter formatting
./warp fmt [-w] .agents/agents/my-agent.md

# Install or inspect a plugin
./warp get --include Skill/hello github.com/acme/finance-skills@v1.2.0
./warp get --list github.com/acme/finance-skills@v1.2.0
```

## JSON Schema

A JSON Schema is available at [`schema/warp.json`](schema/warp.json) to provide real-time validation and autocompletion in IDEs like VS Code.

To use it in VS Code, add the following to your settings (requires the YAML extension):

```json
"yaml.schemas": {
  "https://raw.githubusercontent.com/masterkeysrd/warp/main/schema/warp.json": [
    "**/.agents/**/*.md",
    "**/.agents/**/*.yaml",
    "**/AGENTS.md",
    "**/WORKSPACE.md",
    "**/PLUGIN.md"
  ]
}
```

## Contributing

We welcome contributions! Please see the `CONTRIBUTING.md` file (if available) or open an issue to discuss your proposed changes.

## License

This project is licensed under the [Apache License 2.0](LICENSE).
