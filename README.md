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
go get github.com/your-username/warp
```

(Replace `your-username` with the appropriate repository path once hosted.)

## File Format

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent            # Agent | Skill | Command
metadata:
  name: my-agent
  description: A helpful assistant.
spec:
  model: gpt-4o
  temperature: 0.7
  skills:
    - skills/finance.md
  commands:
    - cmd/report.md
---

# My Agent

You are a helpful assistant that specializes in financial analysis...
```

## Resource Kinds

| Kind      | Purpose |
|-----------|---------|
| `Agent`   | An autonomous agent with LLM configuration and references to the skills and commands it may invoke. |
| `Skill`   | A bundle of expertise guidelines for a specific domain. Agents load skills to adopt a persona or follow conventions. |
| `Command` | A discrete, reusable operation an agent can invoke (e.g. "generate a report"). |

## Loading Resources

### OS filesystem — default directory

```go
// Loads from .agents/ in the current working directory.
registry, err := warp.LoadDefault()
if err != nil {
    log.Fatal(err)
}

if err := registry.Validate(); err != nil {
    log.Fatal(err)
}
```

### OS filesystem — custom directory

```go
registry, err := warp.Load("/custom/path")
```

### Custom `fs.FS` (embedded assets, testing, etc.)

```go
//go:embed testdata
var testFS embed.FS

registry, err := warp.NewLoader(testFS).Load()
```

## Directory Layout

The default root directory is `.agents`. Inside it, resources are organised
by kind into three sub-directories:

```
.agents/
├── cmd/              # Command definitions
│   └── report.md     # kind: Command
├── defs/             # Agent definitions
│   └── analyst.md    # kind: Agent
└── skills/           # Skill definitions
    └── finance.md    # kind: Skill
```

## API Reference

### Types

- **`Registry`** – holds parsed resources indexed by their FS-relative path.
  - `Agents`, `Skills`, `Commands` — typed maps.
  - `Validate() error` — checks required fields and resolves Agent cross-references.
- **`Loader`** – walks an `fs.FS` and parses every `.md` file it finds.
  - `NewLoader(fsys fs.FS) *Loader`
  - `Load() (*Registry, error)`
- **`Load(root string) (*Registry, error)`** — loads resources from the given
  OS filesystem path.
- **`LoadDefault() (*Registry, error)`** — loads resources from the default
  `.agents` directory in the current working directory.
- **`Parse(content string) (*ParseResult, error)`** — parses a single warp
  Markdown string and returns the typed resource.

## Contributing

We welcome contributions! Please see the `CONTRIBUTING.md` file (if available) or open an issue to discuss your proposed changes.

## License

This project is licensed under the [Apache License 2.0](LICENSE).
