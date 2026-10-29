---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: warp-developer
  description: Expertise in developing and integrating with the Workspace Agent Resource Protocol (WARP).
---

# WARP Developer Skill

You are an expert in the Workspace Agent Resource Protocol (WARP). Your goal is to help users and other agents manage, extend, and integrate with WARP-compliant workspaces.

## Core Concepts

### 1. Resource Model
Every WARP resource is a Markdown file with YAML front-matter or a pure YAML file.
- **Workspace**: The root authority (`WORKSPACE.md`). Defines project boundaries and global settings.
- **Context**: Project-specific instructions (`AGENTS.md`).
- **Agent**: Defines an autonomous actor, its persona, and its capabilities (skills, commands, tools).
- **Skill**: Domain-specific expertise guidelines.
- **Command**: Reusable operations an agent can invoke.
- **Decision**: Probabilistic decision evaluations (`predicate`, `choice`, `score` questions and evaluation rubrics).
- **Rule**: Ambient constraints, coding standards, or guardrails with multi-modal activation triggers (`always`, `globs`, `useWhen`, `keywords`, `decision`, `tools`).
- **ModelProvider**: LLM provider configuration (API endpoints, default models).
- **Tool**: Definition for an executable tool with parameter schema.
- **MCP**: Model Context Protocol server integration.
- **Toolkit**: A collection of tools grouped together.

### 2. Resource Properties (Spec)

Each resource has specific fields in its `spec` block:

| Kind | Key Properties |
|------|----------------|
| **Workspace** | `projects` (string[]), `defaultProvider` (string), `defaultModel` (string), `defaultModels` (ModelDefaults), `defaultAgent` (string), `rules` (string[]), `plugins` (WorkspacePlugin[]), `policies` (Policies) |
| **Context** | `resources` (string[]), `rules` (string[]) |
| **Agent** | `extends` (string), `triggers` (string[]), `models` (string[]), `temperature` (float), `thinking` (map), `skills` (string[]), `commands` (string[]), `subagents` (string[]), `rules` (string[]), `policies` (Policies) |
| **Skill** | `useWhen` (string), `keywords` (string[]), `tools` (string[]), `argumentHint` (string), `arguments` (Argument[]) |
| **Command** | `agent` (string), `context` (string: `inline`, `fork`, `clean`), `models` (string[]), `tools` (string[]), `argumentHint` (string), `arguments` (Argument[]) |
| **Decision** | `models` (string[]), `argumentHint` (string), `arguments` (Argument[]), `questions` (DecisionQuestion[]) |
| **Rule** | `triggers` (RuleTriggers: `always`, `globs`, `useWhen`, `keywords`, `decision`, `tools`), `priority` (int) |
| **ModelProvider** | `type` (string), `endpoint` (string), `defaultModel` (string), `defaultModels` (ModelDefaults), `auth` (ProviderAuth), `models` (ProviderModel[]) |
| **Tool** | `command` (string[]), `description` (string), `env` (map), `inputSchema` (JSON Schema), `outputSchema` (JSON Schema), `annotations` (ToolAnnotation) |
| **MCP** | `type` (string: `stdio`, `sse`, `http`, `streamable-http`), `command` (string[]), `endpoint` (string), `env` (map), `auth` (ProviderAuth), `headers` (map), `annotations` (ToolAnnotation), `policies` (MCPPolicies), `overrides` (map) |
| **Toolkit** | `tools` (ToolkitTool[]) |

### 3. Directory Layout
- `.agents/`: The standard location for resources.
  - `defs/` or `agents/`: Markdown files for Agents.
  - `skills/`: Markdown files for Skills.
  - `commands/`: Markdown files for Commands.
  - `decisions/`: Markdown files for Decisions.
  - `rules/`: Markdown files for Rules.
  - `tools/`, `mcps/`, `providers/`: YAML files for technical configurations.

### 4. Resource Resolution
- **Short Name**: `my-skill` (searches through `local` -> `workspace` -> `user` -> `system` namespaces).
- **Qualified Name**: `namespace/Kind/name` (e.g., `local/Skill/warp-developer`).

### 5. Instruction Templating
All Markdown resources render their `instructions` body via Go `text/template` plus shell-style shorthand (`$Name`, `$DisplayName`, `$WorkspaceDir`, `$WorkspacePath`, `$ProjectDir`, `$ContextPath`, `$RuleDir`, `$RulePath`, `$1`, `$2`, and named arguments):
- `{{.Workspace}}`: `.Dir`, `.Path`
- `{{.Project}}`: `.Name`, `.DisplayName`, `.Dir`
- `{{.Context}}`: `.Path`, `.Instructions`
- `{{.Agent}}`: `.Name`, `.Description`, `.Dir`, `.Path`, `.Skills`, `.Tools`, `.Commands`, `.Subagents`, `.Rules`
- `{{.Skill}}`: `.Name`, `.Description`, `.UseWhen`, `.Keywords`, `.Tools`, `.ArgumentHint`, `.Arguments`, `.Dir`, `.Path`
- `{{.Rule}}`: `.Name`, `.Description`, `.UseWhen`, `.Keywords`, `.Dir`, `.Path`, `.Priority`
- `{{.Command}}`: `.Name`, `.Description`, `.Agent`, `.Context`, `.ArgumentHint`, `.Arguments`, `.Dir`, `.Path`, `.Tools`
- `{{.Decision}}`: `.Name`, `.Description`, `.ArgumentHint`, `.Arguments`, `.Dir`, `.Path`

## Resource Creation

### 1. Standard Resources (.md)
Most resources use Markdown with a YAML front-matter block.
```markdown
---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: my-skill
---
# My Skill
Instructions here...
```

### 2. Technical Resources (.yaml)
Resources like `ModelProvider`, `Tool`, `MCP`, and `Toolkit` can be defined as pure YAML.
```yaml
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: list-files
spec:
  command: ["ls"]
  description: "List files"
```

### 3. Special Files
- `WORKSPACE.md`: Defines the workspace root and project boundaries.
- `AGENTS.md`: Defines the context/identity for a specific project.

## Using the Go API

### Loading a Workspace
Use the `warp` package to load and interact with workspaces programmatically.

```go
import "github.com/masterkeysrd/warp"

// Load from current directory
reg, err := warp.LoadDefault()

// Load from a specific path
reg, err := warp.LoadWorkspace("/path/to/workspace")
```

### Programmatic Resource Management
You can build resources and registries manually for tests or custom integrations.

```go
// Create a resource
skill := &warp.Skill{
    BaseResource: warp.BaseResource{
        Kind:       warp.KindSkill,
        APIVersion: warp.APIVersion,
        Metadata:   warp.Metadata{Name: "custom-skill"},
    },
    Spec: warp.SkillSpec{
        Instructions: "Manual instructions",
    },
}

// Add to a registry
reg := warp.NewRegistry(nil)
reg.Set("local/Skill/custom-skill", skill)
```

### Low-level Parsing
Use `warp.Parse` to decode a single file's content into a typed resource.
```go
result, err := warp.Parse("my-skill.md", content)
skill := result.Resource.(*warp.Skill)
```

## Validation & Linting

### CLI Validation
Use the `warp` CLI to validate your workspace structure and resource integrity.
```bash
warp validate .
```

### Programmatic Validation
The `Registry.Validate()` method checks for structural correctness and ensures all cross-references (skills, commands, tools) resolve correctly.

```go
err := reg.Validate()
if err != nil {
    log.Fatalf("Validation failed: %v", err)
}
```

## Development Guidelines

### Creating Resources
1. **Metadata**: Always include `apiVersion: warp/v1alpha1`, `kind`, and `metadata.name`.
2. **Naming**: Use kebab-case for resource names.
3. **Instructions**: Write clear, actionable Markdown instructions in the body.
4. **Inheritance**: Agents can extend others using `spec.extends`.

### Integrating with WARP
- When adding capabilities to an agent, add the resource name to the agent's `spec.skills` or `spec.commands` list.
- Use `AGENTS.md` to define the "soul" of a project or directory.
- Use `WORKSPACE.md` to orchestrate multiple projects.

## Best Practices
- Keep skills modular and focused on a single domain.
- Use `Command` resources for tasks with structured output or specific tool requirements.
- Leverage `MCP` for complex tool integrations.
- Always validate resources against the `warp/v1alpha1` schema.
