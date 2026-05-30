# Workspace Agent Resource Protocol (WARP)

**Version:** `warp/v1alpha1`

Warp is a language-agnostic, declarative format for defining AI agents and
their supporting resources. Resources are primarily plain Markdown files: a
YAML front-matter block carries structured metadata and configuration, while the
Markdown body below it becomes the resource's instruction text. Resources that
are pure configuration may also be defined as plain `.yaml` or `.yml` files.

Any runtime that can read a filesystem and parse YAML + Markdown can load and
execute Warp resources without modification.

---

## Table of Contents

1. [File Format](#1-file-format)
2. [Common Fields](#2-common-fields)
3. [Resource Kinds](#3-resource-kinds)
   - [3.1 Workspace](#31-workspace)
   - [3.2 Context](#32-context)
   - [3.3 Agent](#33-agent)
   - [3.4 Skill](#34-skill)
   - [3.5 Command](#35-command)
   - [3.6 ModelProvider](#36-modelprovider)
   - [3.7 Tool](#37-tool)
   - [3.8 MCP](#38-mcp)
   - [3.9 Toolkit](#39-toolkit)
4. [Cross-References](#4-cross-references)
5. [Discovery and Loading](#5-discovery-and-loading)
   - [5.1 Phase 1: Workspace Discovery](#51-phase-1-workspace-discovery)
   - [5.2 Phase 2: Project Mapping](#52-phase-2-project-mapping)
   - [5.3 Phase 3: Contextual Loading](#53-phase-3-contextual-loading)
   - [5.4 Resource Resolution Hierarchy](#54-resource-resolution-hierarchy)
6. [Directory Layout](#6-directory-layout)
7. [Validation Rules](#7-validation-rules)
8. [Parsing Algorithm](#8-parsing-algorithm)
9. [Plugins & Adapters](#9-plugins--adapters)
10. [Standard Namespaces](#10-standard-namespaces)

---

## 1. File Format

Every Warp resource is either a `.md` file or a `.yaml`/`.yml` file.

### Markdown Resources

Standard resources follow this structure:

```markdown
---
<YAML front-matter>
---

<Markdown body — becomes the resource's instruction text>
```

- The file **must** begin with a line containing only `---`.
- The YAML block **must** be closed by a second line containing only `---`.
- Everything after the closing `---` is treated as the instruction body.
- **Exception:** Files named `WORKSPACE.md` or `AGENT.md` (case-insensitive) may omit the YAML
  front-matter entirely. In this case, the entire file content is treated as the instruction body,
  and metadata is inferred by the loader.

### YAML Resources

Resources that are pure configuration (e.g. `ModelProvider`, `Tool`, `MCP`, `Toolkit`)
may omit the Markdown body and instruction delimiters. These files must have a
`.yaml` or `.yml` extension and contain only a valid YAML document.

---

## 2. Common Fields

Every resource kind shares the following top-level YAML fields.

| Field                  | Type   | Required | Description                                                                                |
|------------------------|--------|:--------:|--------------------------------------------------------------------------------------------|
| `apiVersion`           | string | ✅       | Schema version. Currently always `warp/v1alpha1`.                                          |
| `kind`                 | string | ✅       | Resource type. One of `Workspace`, `Context`, `Agent`, `Skill`, `Command`, `ModelProvider`, `Tool`, `MCP`, `Toolkit`. |
| `metadata.name`        | string | ✅       | Unique identifier for the resource within its kind.                                        |
| `metadata.description` | string |          | Short human-readable summary.                                                              |
| `metadata.displayName` | string |          | Pretty-printed label for UIs.                                                              |
| `metadata.labels`      | map    |          | Arbitrary key-value pairs for categorisation and filtering.                                |

---

## 3. Resource Kinds

### 3.1 Workspace

A `Workspace` is the **root authority** for the entire agentic environment. It defines the
boundaries of the session: which directories are active projects and any workspace-wide instruction
text. There is at most one `Workspace` per session.

The canonical filename is `WORKSPACE.md` (case-insensitive). A conforming loader locates this file
by climbing the directory tree from the current working directory (see
[Section 5.1](#51-phase-1-workspace-discovery)). If no file is found, a **Synthetic Workspace** is
created automatically with its root set to `$CWD`.

#### `spec` fields

| Field             | Type     | Required | Default  | Description                                                                                                                   |
|-------------------|----------|:--------:|----------|-------------------------------------------------------------------------------------------------------------------------------|
| `projects`        | string[] |          | `[]`     | Directories to load as active projects. `["*"]` discovers all non-hidden immediate subdirectories. An empty value or omission defaults to `["."]`. |
| `defaultProvider` | string   |          |          | Provider to use when an agent omits `spec.model`. May reference either a `ModelProvider.metadata.name` or `ModelProvider.spec.type`. |

#### Full example

```markdown
---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: my-workspace
  description: Monorepo workspace for the platform team.
spec:
  projects:
    - services/api
    - services/auth
    - libs/core
  defaultProvider: genai
---
```

# Platform Workspace

All agents in this workspace follow the platform coding standards documented in
`docs/standards.md`. Always prefer incremental, reversible changes.

---

### 3.2 Context

A `Context` defines the **identity and instructions** for a single project scope. It is the
authoritative entry point for the directory it lives in.

The canonical filename is `AGENT.md` (case-insensitive). A loader that finds an `AGENT.md` file
inside a project directory will automatically parse it as a `Context` resource. If the file lacks
YAML front-matter, the loader infers the `apiVersion`, `kind`, and `metadata.name` fields
automatically.

#### `spec` fields

_None beyond shared metadata._ The Markdown body becomes the `instructions` field.

#### Full example

```markdown
---
apiVersion: warp/v1alpha1
kind: Context
metadata:
  name: services-api
  description: Contextual rules for the API service project.
---

# API Service

You are working inside the `services/api` directory. This is a Go gRPC service.

## Rules
- Never modify generated `.pb.go` files directly.
- All new endpoints must have a corresponding integration test.
```

---

### 3.3 Agent

An `Agent` describes an autonomous actor: its LLM configuration, its persona
(the Markdown body), and the set of skills and commands it may invoke at
runtime.

#### `spec` fields

| Field         | Type     | Required | Default | Description                                                                                        |
|---------------|----------|:--------:|---------|----------------------------------------------------------------------------------------------------|
| `extends`     | string   |          | —       | Qualified Name (`namespace/Agent/name`) or Short Name of another Agent to extend. When set, the engine merges the parent's `skills` and `tools` arrays with the child's (parent entries first) and concatenates their Markdown instructions (parent first, then child). |
| `triggers`    | string[] |          | `[]`    | Defines what architectural entities can invoke this agent (e.g. `["human"]`, `["agent"]`, `["system"]`). An empty list means it can be triggered by anything. |
| `models`      | string[] |          | `[]`    | A prioritized list of LLM model identifiers (e.g. `["gpt-4o", "claude-3-5-sonnet"]`). The runtime should use the first available model. |
| `temperature` | float    |          | `0.0`   | Sampling temperature in the range `0.0`–`2.0`. Higher values produce more varied output.          |
| `tools`       | string[] |          | `[]`    | Names or qualified refs of `Tool` resources this agent may use. An empty list means no restriction. |
| `skills`      | string[] |          | `[]`    | Names or qualified refs of `Skill` resources this agent is allowed to use.                        |
| `commands`    | string[] |          | `[]`    | Names or qualified refs of `Command` resources this agent can invoke.                             |

> `instructions` is **not** written in the YAML front-matter. It is populated
> automatically from the Markdown body below the closing `---`.

#### Inheritance Merge Rules

When `spec.extends` is set the engine performs a **recursive merge**:

1. The parent agent is resolved (which may itself extend another agent, forming a chain).
2. A deep copy of the resolved parent is made as the merge base.
3. The child's `skills` and `tools` lists are **appended** to the parent's lists.
4. The child's Markdown instructions are **concatenated** after the parent's, separated by a blank line.
5. Circular inheritance chains (including self-extension) are detected and return an error.

#### Global Override Example

A user can augment the built-in `system/Agent/main` agent without replacing its
system prompt by placing an agent file in the `user` namespace:

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: main
  description: My customised main agent.
spec:
  extends: "system/Agent/main"
  skills:
    - go
    - git
---

## Additional Rules

Always prefer table-driven tests in Go.
```

Because `user` namespace has higher priority than `system`, `ResolveAgent("main")`
returns the merged agent: the system prompt prepended, then the user's extra rules
appended, with the skill lists combined.

#### Full example

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: analyst
  description: Senior financial analysis agent.
  displayName: Analyst
spec:
  models: ["gpt-4o", "claude-3-5-sonnet"]
  temperature: 0.2
  skills:
    - skills/finance.md
    - skills/sql.md
  commands:
    - cmd/generate-report.md
---

# Analyst

You are a senior financial analyst with deep expertise in equity research and
SQL-based data retrieval. Always cite the data source before drawing any
conclusion.

## Rules
- Never fabricate figures. If data is unavailable, say so.
- Prefer concise bullet-point summaries unless a full narrative is requested.
```

---

### 3.4 Skill

A `Skill` bundles expertise guidelines for a specific domain. Agents load a skill's instruction
body to adopt a persona or follow a set of conventions. Skills carry no configuration beyond the
shared metadata header.

#### `spec` fields

_None._ The only content is the Markdown body, which becomes `instructions`.

#### Full example

```markdown
---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: finance
  description: Core financial analysis conventions and terminology.
---

# Finance Skill

## Terminology
- **EPS** — Earnings per share.
- **P/E ratio** — Price-to-earnings ratio.
- **EBITDA** — Earnings before interest, taxes, depreciation, and amortisation.

## Conventions
- Always express monetary values in USD unless the user specifies otherwise.
- Round percentages to two decimal places.
- Cite the reporting period (e.g. Q1 2026) when quoting financial figures.
```

---

### 3.5 Command

A `Command` encapsulates a discrete, reusable operation that an agent can invoke. Commands carry no
configuration beyond the shared metadata header; their behaviour is described entirely in the
Markdown body.

#### `spec` fields

_None._ The only content is the Markdown body, which becomes `instructions`.

#### Full example

```markdown
---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: generate-report
  description: Produce a structured equity research report.
  displayName: Generate Report
spec:
  models: ["gpt-4o-mini"]
  tools: ["local/Tool/read-file"]
  hints: ["ticker", "year"]
---

# Generate Report

Produce a structured equity research report for the given ticker symbol.

## Output Format

<example>
## {TICKER} — Equity Research Report ({DATE})

### Summary
{Two-sentence executive summary}

### Key Metrics
| Metric | Value |
|--------|-------|
| ...    | ...   |

### Recommendation
{Buy / Hold / Sell} — {One-paragraph rationale}

Include a disclaimer at the end of every report.
</example>
```

---

### 3.6 ModelProvider

A `ModelProvider` resource describes an LLM provider configuration, such as API
endpoints and default models.

#### `spec` fields

| Field          | Type   | Required | Description                                     |
|----------------|--------|:--------:|-------------------------------------------------|
| `type`         | string | ✅       | e.g., `ollama`, `openai`, `anthropic`.          |
| `endpoint`     | string | ✅       | API base URL.                                   |
| `defaultModel` | string |          | Model to use if none specified.                 |
| `auth`         | map    |          | Authentication config (e.g., `type: env`, `key: OPENAI_API_KEY`). |
| `models`       | object[]|          | Available models from this provider.            |

#### `ProviderModel` fields

| Field    | Type   | Required | Description                                     |
|----------|--------|:--------:|-------------------------------------------------|
| `id`     | string | ✅       | Unique model ID (e.g., `gpt-4`).                |
| `name`   | string | ✅       | Model name (e.g., `gpt-4`).                     |
| `label`  | string | ✅       | Human-friendly label (e.g., `GPT-4`).           |
| `limits` | object | ✅       | Context and output token limits.                |

#### `ProviderModelLimits` fields

| Field     | Type | Required | Description                          |
|-----------|------|:--------:|--------------------------------------|
| `context` | int  | ✅       | Max context length in tokens.        |
| `output`  | int  | ✅       | Max output length in tokens.         |

#### Full example (`providers/ollama.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: local-ollama
  description: Local Ollama instance.
spec:
  type: ollama
  endpoint: http://localhost:11434
  defaultModel: llama3
  models:
    - id: llama3
      name: llama3
      label: Llama 3
      limits:
        context: 8192
        output: 4096
```

---

### 3.7 Tool

A `Tool` resource describes a custom tool that an agent can invoke.

#### `spec` fields

| Field         | Type     | Required | Description                                                                 |
|---------------|----------|:--------:|-----------------------------------------------------------------------------|
| `command`     | string[] | ✅       | Executable and static args (e.g., `["python", "script.py"]`).               |
| `description` | string   | ✅       | What the tool does (sent to the LLM).                                       |
| `env`         | map      |          | Environment variables injected into the process.                            |
| `parameters`  | map      |          | JSON Schema object defining arguments the LLM must pass.                    |
| `annotations` | object   |          | Safety profile for Tool Execution Security.                                 |

#### `ToolAnnotation` fields

| Field          | Type   | Default | Description                                               |
|----------------|--------|---------|-----------------------------------------------------------|
| `isOpenWorld`  | bool   | `false` | Interacts with external resources (network, etc.).         |
| `isDangerous`  | bool   | `false` | Can perform destructive actions (delete, format, etc.).    |
| `isReadOnly`   | bool   | `false` | Does not modify state.                                    |
| `isIdempotent` | bool   | `false` | Safe to retry on failure.                                 |
| `userHint`     | string | —       | Human-readable hint for approval prompts.                 |

#### Full example (`tools/ls.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: list-files
  description: List files in a directory.
spec:
  command: ["ls", "-F"]
  description: List files in the current directory with type indicators.
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Directory path to list.
```

---

### 3.8 MCP

An `MCP` resource describes an [Model Context Protocol](https://modelcontextprotocol.io)
server.

#### `spec` fields

| Field         | Type     | Required | Description                                     |
|---------------|----------|:--------:|-------------------------------------------------|
| `command`     | string[] | ✅       | Command to start the MCP server via stdio.      |
| `env`         | map      |          | Environment variables for the MCP server.       |
| `annotations` | object   |          | Default safety profile for all exposed tools.   |
| `tools`       | object   |          | Controls which tools are exposed by this server. |
| `overrides`   | map      |          | Tool-specific annotation overrides.             |

#### `MCPFilter` fields

| Field     | Type     | Description                                           |
|-----------|----------|-------------------------------------------------------|
| `include` | string[] | Glob patterns for tools to expose.                    |
| `exclude` | string[] | Glob patterns for tools to block (applied after include).|

#### Full example (`mcp/sqlite.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: sqlite-mcp
spec:
  command: ["npx", "-y", "@modelcontextprotocol/server-sqlite", "--db", "data.db"]
```

---

### 3.9 Toolkit

A `Toolkit` resource groups multiple tools together. Tools can be defined inline
or referenced by file path.

#### `spec` fields

| Field   | Type     | Required | Description                          |
|---------|----------|:--------:|--------------------------------------|
| `tools` | object[] | ✅       | Array of tool references or inline definitions. |

#### Full example (`toolkits/default.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: Toolkit
metadata:
  name: default-toolkit
spec:
  tools:
    - $ref: tools/ls.yaml
    - name: echo
      command: ["echo"]
      description: Print a message.
      parameters:
        type: object
        properties:
          msg: { type: string }
```

---

## 4. Cross-References

An `Agent` resource may declare lists of skill and command references under `spec.skills` and
`spec.commands`. A reference can be either a **Qualified Name** or a **Short Name**.

- **Qualified Name**: `namespace/kind/name` (e.g. `system/Skill/python`). Directs the loader to exactly one resource in the registry.
- **Short Name**: `name` (e.g. `python`). The loader will iterate through the standard namespaces in priority order to find the first matching kind and name.

```yaml
spec:
  skills:
    - system/Skill/finance   # Qualified Name: Explicitly targets the system-level finance skill
  commands:
    - report                 # Short Name: Resolves using Search Path Priority
```

A conforming loader **must** verify that every referenced path resolves to a loaded resource of the
correct kind, applying the [resolution hierarchy](#54-resource-resolution-hierarchy). A reference
that cannot be satisfied is a validation error.

---

## 5. Discovery and Loading

Loading happens in three sequential phases. The result is a fully-populated **Workspace** that
contains one or more **Projects**, each with their own resource registries.

### 5.1 Phase 1: Workspace Discovery

The loader establishes the **Root Authority** for the session.

1. **Immediate Check:** Look for `WORKSPACE.md` (case-insensitive) in `$CWD`.
2. **Parent Climb:** If not found, move to the parent directory and repeat.
3. **Terminal Fallback:** If the filesystem root is reached without finding a `kind: Workspace`
   resource, the loader initialises a **Synthetic Workspace** with `RootPath = $CWD` and
   `Synthetic = true`.

> **Note:** The loader intentionally ignores `.git`, `go.mod`, and other structural anchors.
> Discovery is strictly tied to WARP files.

### 5.2 Phase 2: Project Mapping

Once `WORKSPACE_PATH` is established, the loader enumerates the active project directories.

| Value of `spec.projects`  | Behaviour                                                                                      |
|---------------------------|-----------------------------------------------------------------------------------------------|
| `["*"]`                   | Shallow scan of `WORKSPACE_PATH`. Every immediate subdirectory that **does not** start with a dot (`.`) is registered as an **Implicit Project**. |
| Explicit list             | Only the listed paths (e.g. `["services/api", "libs/core"]`) are loaded. If a path does not exist, the loader returns an error. |
| Empty / omitted           | Defaults to `["."]`. The workspace root itself is treated as the single project.              |

### 5.3 Phase 3: Contextual Loading

The loader enters each project directory and builds its **Identity** and **Resource Registry**.

**Per project:**

1. **Context Resolution:**
   - Check for `AGENT.md` (case-insensitive) in the project directory.
   - **Found:** Parse it as a `Context` resource.
   - **Missing:** No implicit context is created; the project has no instruction body.

2. **Resource Discovery:**
   - Walk the `.agents/` subdirectory relative to the project path.
   - Register all `.md`, `.yaml`, and `.yml` files of known kinds into the project's local
     registry.

3. **Project Naming (The Slug Rule):**
   - If the project path equals `"."`, the name is the workspace root folder's base name.
   - Otherwise, the name is the **Slugified Relative Path** of the project directory relative to
     `WORKSPACE_PATH` (e.g. `services/auth` → `services-auth`).

**Workspace-Global Resources (sub-directory projects only):**
The loader also walks `WORKSPACE_PATH/.agents/` and stores those resources in the workspace's own
global registry. These are made available as a fallback to all sub-directory projects.

#### The Authority Rule (Flat Root)

When `WORKSPACE_PATH == PROJECT_PATH` (i.e. the only project is `"."`), the `.agents/` directory
at the root **belongs exclusively to the Project**. The workspace's global library is treated as
empty. This prevents resources from being loaded twice and keeps attribution unambiguous.

| Topology | Owns `WORKSPACE_PATH/.agents/` | Global library |
|---|---|---|
| Sub-directory projects | Workspace (global scope) | Non-empty |
| Root-is-project (`"."`") | Project (local scope) | Empty |

### 5.4 Resource Resolution Hierarchy

Resources are identified by a **Qualified Name** (`namespace/Kind/name`) or a **Short Name**
(`name`). The Search Path Priority determines which resource wins when two namespaces provide a
resource with the same short name.

**Search Path Priority** (highest → lowest):

| Priority | Namespace   | Description                                                         |
|----------|-------------|---------------------------------------------------------------------|
| 100      | `local`     | Resources from the active project's own `.agents/` directory.       |
| 80       | `workspace` | Resources from `WORKSPACE_PATH/.agents/` (sub-directory projects).  |
| 60       | `user`      | User-level resources (e.g. `~/.config/warp/`).                      |
| 40       | `system`    | System/builtin resources embedded or installed globally.             |
| 0        | _(plugins)_ | Any other namespace contributed by a registered plugin.              |

**Short-Name Resolution Algorithm:**

1. Iterate namespaces in descending priority order.
2. For each namespace, check whether the registry contains any resource whose
   `metadata.name` matches the short name.
3. Return the first match found.

**Qualified-Name Resolution:** A reference containing `/` is treated as an exact qualified name
(`namespace/Kind/name`) and is looked up directly in the registry — no namespace search occurs.

**Shadowing (Effective View):** When the same short name exists in multiple namespaces, the
higher-priority namespace version **shadows** the lower-priority ones. The `Effective` query mode
returns only the winning resource per short name.

---

## 6. Directory Layout

A typical multi-project workspace:

```
WORKSPACE.md              # Root Authority — defines projects
.agents/                  # Workspace-global resources
│   ├── skills/
│   │   └── coding.md
│   └── cmd/
│       └── review.md
services/
├── api/
│   ├── AGENT.md          # Context — identity for the api project
│   └── .agents/          # Project-local resources
│       ├── skills/
│       │   └── grpc.md
│       └── cmd/
│           └── gen-proto.md
└── auth/
    ├── AGENT.md
    └── .agents/
        └── skills/
            └── jwt.md
```

A minimal single-project setup (no `WORKSPACE.md` needed):

```
AGENT.md                  # Context for the single project
.agents/
├── skills/
└── cmd/
```

---

## 7. Validation Rules

A conforming implementation **must** enforce the following rules when `Validate()` is called on a
loaded Workspace.

### 7.1 All resources

| Rule                         | Error condition                                                    |
|------------------------------|--------------------------------------------------------------------|
| `apiVersion` is present      | Field is empty or absent.                                          |
| `kind` is present            | Field is empty or absent.                                          |
| `metadata.name` is present   | Field is empty or absent.                                          |
| `kind` is a known value      | Value is not one of `Workspace`, `Context`, `Agent`, `Skill`, `Command`, `ModelProvider`, `Tool`, `MCP`, `Toolkit`. |

### 7.2 Agent-specific

| Rule                              | Error condition                                                              |
|-----------------------------------|------------------------------------------------------------------------------|
| All `spec.skills` paths resolve   | A listed path does not match any loaded `Skill` in the resolution hierarchy. |
| All `spec.commands` paths resolve | A listed path does not match any loaded `Command` in the resolution hierarchy. |

### 7.3 Workspace-specific

| Rule                                | Error condition                                                |
|-------------------------------------|----------------------------------------------------------------|
| All `spec.projects` paths exist     | A listed path (when not `"*"`) does not exist on the filesystem. |

---

## 8. Parsing Algorithm

The following algorithm describes how a conforming parser handles standard resources and the two
special inferred-manifest filenames.

```
function parse(filePath, fileContent):
  1. fileName = basename(filePath)
  2. isWorkspaceFile = (fileName.toLowerCase() == "workspace.md")
  3. isContextFile   = (fileName.toLowerCase() == "agent.md")

  4. Split fileContent on "---" into segments.

  5. IF segments.length < 3:
     IF isWorkspaceFile:
        # Infer Workspace resource
        resource.apiVersion  = "warp/v1alpha1"
        resource.kind        = "Workspace"
        resource.metadata.name = slugifyPath(filePath, workspaceRoot)
        resource.instructions = trim(fileContent)
        return resource
     ELSE IF isContextFile:
        # Infer Context resource
        resource.apiVersion  = "warp/v1alpha1"
        resource.kind        = "Context"
        resource.metadata.name = slugifyPath(filePath, workspaceRoot)
        resource.instructions = trim(fileContent)
        return resource
     ELSE:
        return error "missing front-matter delimiters"

  6. Decode segments[1] as YAML.
  7. IF kind is not in {Workspace, Context, Agent, Skill, Command, ModelProvider, Tool, MCP, Toolkit}:
     return error "unsupported kind"

  8. Populate resource from YAML and segments[2] (instructions).
  9. Return resource
```

### Path Slugification Rule

`slugifyPath(filePath, workspaceRoot)` produces a stable resource name:

1. Make `filePath` relative to `workspaceRoot`.
2. Remove the `.md` extension.
3. Replace all path separators (`/` or `\`) with a single hyphen `-`.
4. Convert to lowercase.

**Examples** (workspace root = `/home/user/repos/platform`):

| Input path                                         | Slug              |
|----------------------------------------------------|-------------------|
| `/home/user/repos/platform/AGENT.md`               | `agent`           |
| `/home/user/repos/platform/services/api/AGENT.md`  | `services-api-agent` |
| `/home/user/repos/platform/WORKSPACE.md`           | `workspace`       |

---

## 9. Plugins & Adapters

### 9.1 Overview

A **Plugin** extends the workspace with resources from an external source. Plugins declare
themselves as a `kind: Plugin` resource and are listed in `spec.plugins` of the `Workspace`
definition. When the loader processes a workspace it instantiates each listed plugin and merges
its resources into the registry under a plugin-specific namespace.

### 9.2 `kind: Plugin`

```yaml
apiVersion: warp/v1alpha1
kind: Plugin
metadata:
  name: my-plugin
  description: Provides shared tools from a shared library.
spec:
  namespace: shared-tools  # namespace all resources from this plugin are registered under
  source: /path/to/plugin/dir  # or a URI handled by a registered adapter
```

#### `spec` fields

| Field       | Type   | Required | Description                                                             |
|-------------|--------|:--------:|-------------------------------------------------------------------------|
| `namespace` | string | ✅       | Namespace all resources from this plugin are registered under.          |
| `source`    | string | ✅       | Filesystem path or URI the loader/adapter resolves to load resources.   |

### 9.3 Declaring Plugins in a Workspace

```yaml
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: my-workspace
spec:
  projects: ["services/api"]
  plugins:
    - plugins/shared-tools.yaml
    - plugins/docs.yaml
```

### 9.4 Adapters

An **Adapter** is a runtime component that maps a `source` URI scheme to a `NamespacedProvider`.
The default adapter handles `file://` and bare filesystem paths. Custom adapters can be registered
to support remote sources (HTTP, S3, Git, etc.).

Adapters are responsible for:
1. Fetching or reading the source.
2. Parsing each file as a WARP resource.
3. Presenting the resources through the `NamespacedProvider` interface so the assembler can
   populate the registry.

---

## 10. Standard Namespaces

The following namespace identifiers are reserved and carry fixed priorities in the resolution
hierarchy. Plugins and adapters **must not** register resources under these names.

| Namespace   | Priority | Typical Source                                       |
|-------------|----------|------------------------------------------------------|
| `local`     | 100      | Project-local `.agents/` directory.                  |
| `workspace` | 80       | Workspace-global `.agents/` directory.               |
| `user`      | 60       | User configuration directory (e.g. `~/.config/warp/`).|
| `system`    | 40       | Embedded builtin resources shipped with the runtime.  |

Any namespace not listed above receives priority **0**. Resources in higher-priority namespaces
shadow same-named resources in lower-priority namespaces when queries use `Effective = true`.

