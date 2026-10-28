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
   - [3.6 Decision](#36-decision)
   - [3.7 ModelProvider](#37-modelprovider)
   - [3.8 Tool](#38-tool)
   - [3.9 MCP](#39-mcp)
   - [3.10 Toolkit](#310-toolkit)
   - [3.11 Plugin](#311-plugin)
   - [3.12 Rule](#312-rule)
4. [Cross-References](#4-cross-references)
5. [Discovery and Loading](#5-discovery-and-loading)
   - [5.1 Phase 1: Workspace Discovery](#51-phase-1-workspace-discovery)
   - [5.2 Phase 2: Project Mapping](#52-phase-2-project-mapping)
   - [5.3 Phase 3: Contextual Loading](#53-phase-3-contextual-loading)
   - [5.4 Resource Resolution Hierarchy](#54-resource-resolution-hierarchy)
6. [Directory Layout](#6-directory-layout)
7. [Validation Rules](#7-validation-rules)
8. [Parsing Algorithm](#8-parsing-algorithm)
9. [Modules and Package Management](#9-modules-and-package-management)
10. [Standard Namespaces](#10-standard-namespaces)
11. [Templating](#11-templating)

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
- **Exception:** Files named `WORKSPACE.md` or `AGENTS.md` (case-insensitive) may omit the YAML
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
| `kind`                 | string | ✅       | Resource type. One of `Workspace`, `Context`, `Agent`, `Skill`, `Command`, `Decision`, `ModelProvider`, `Tool`, `MCP`, `Toolkit`, `Plugin`. |
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
| `defaultProvider` | string   |          |          | Provider to use when an agent omits `spec.models`. May reference either a `ModelProvider.metadata.name` or `ModelProvider.spec.type`. |
| `defaultModel`    | string   |          |          | Default completion model to use when an agent omits `spec.models`.                                                            |
| `defaultModels`   | object   |          | —        | Role-specific default models (`completion`, `decision`, `embedding`) across the workspace.                                    |
| `defaultAgent`    | string   |          |          | Default agent to use for this workspace if none is specified.                                                                 |
| `plugins`         | object[] |          | `[]`     | External repositories to load as plugins into this workspace.                                                                 |
| `policies`        | object   |          | —        | Security and access policies (e.g., tool execution restrictions).                                                             |

#### `ModelDefaults` fields

| Field        | Type   | Description                                                                                      |
|--------------|--------|--------------------------------------------------------------------------------------------------|
| `completion` | string | Default model for text/chat generation (e.g., `"anthropic/claude-3-7-sonnet"`).                  |
| `decision`   | string | Default model for probabilistic decision evaluations (e.g., `"typesafe/jev-latest"`).            |
| `embedding`  | string | Default model for vector embeddings (e.g., `"openai/text-embedding-3-small"`).                   |

#### Model Resolution Precedence (`ResolveModels`)

When resolving the prioritized model list for a capability role (`completion`, `decision`, or `embedding`), a conforming runtime or `ResolveModels(role, explicit...)` evaluates sources in the following order:

1. **Resource-Level Explicit Models**: Non-empty `spec.models` declared directly on the resource (`Agent`, `Command`, or `Decision`).
2. **Workspace Role Defaults**: `workspace.spec.defaultModels.<role>`, falling back to `workspace.spec.defaultModel` when the role is `completion`.
3. **Default Provider Role Defaults**: On the `ModelProvider` referenced by `workspace.spec.defaultProvider`, `provider.spec.defaultModels.<role>`, falling back to `provider.spec.defaultModel` when the role is `completion`.

Any resolved model identifier that omits a `<provider>/` prefix is automatically qualified with `<defaultProvider>/` when `workspace.spec.defaultProvider` is configured.

#### `WorkspacePlugin` fields

| Field       | Type   | Required | Description                                                         |
|-------------|--------|:--------:|---------------------------------------------------------------------|
| `source`    | string | ✅       | Source URL or path for the plugin.                                  |
| `version`   | string |          | Specific version of the plugin to load (e.g., git tag or branch).   |
| `namespace` | string |          | Custom namespace to mount the plugin resources under.               |
| `imports`   | object |          | Filter specifying which resources to import (`include`/`exclude`).  |

#### `Policies` fields

| Field   | Type   | Description                                           |
|---------|--------|-------------------------------------------------------|
| `tools` | object | Restrictions on which tools agents are allowed to use. |

#### `ToolPolicies` fields

| Field            | Type     | Default | Description                                                                                 |
|------------------|----------|---------|---------------------------------------------------------------------------------------------|
| `allowDangerous` | bool     | `true`  | If false, rejects any tool with `annotations.isDangerous: true`.                            |
| `allowOpenWorld` | bool     | `true`  | If false, rejects any tool with `annotations.isOpenWorld: true`.                            |
| `include`        | string[] | `[]`    | List of patterns for tools explicitly allowed. If set, tools not matching are forbidden.    |
| `exclude`        | string[] | `[]`    | List of patterns for tools explicitly forbidden.                                            |

Patterns in `include` and `exclude` are glob-matched against three target strings:
1. The tool's short name (e.g. `git-clone`).
2. The tool's namespace and name (e.g. `workspace/git-*`), allowing easy scope/namespace limits without specifying the Kind.
3. The tool's fully qualified name (e.g. `workspace/Tool/git-clone`).

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
  defaultModels:
    completion: anthropic/claude-3-7-sonnet
    decision: typesafe/jev-latest
    embedding: openai/text-embedding-3-small
---
```

# Platform Workspace

All agents in this workspace follow the platform coding standards documented in
`docs/standards.md`. Always prefer incremental, reversible changes.

---

### 3.2 Context

A `Context` defines the **identity and instructions** for a single project scope. It is the
authoritative entry point for the directory it lives in.

The canonical filename is `AGENTS.md` (case-insensitive). A loader that finds an `AGENTS.md` file
inside a project directory will automatically parse it as a `Context` resource. If the file lacks
YAML front-matter, the loader infers the `apiVersion`, `kind`, and `metadata.name` fields
automatically.

#### `spec` fields

| Field       | Type     | Required | Default | Description                                                                                 |
|-------------|----------|:--------:|---------|---------------------------------------------------------------------------------------------|
| `resources` | string[] |          | `[]`    | Paths (relative to the project directory) of additional Warp resource files or directories to load into the project namespace. |

> `instructions` is **not** written in the YAML front-matter when using Markdown format. It is populated
> automatically from the Markdown body below the closing `---`.

#### Full example

```markdown
---
apiVersion: warp/v1alpha1
kind: Context
metadata:
  name: services-api
  description: Contextual rules for the API service project.
spec:
  resources:
    - custom/extra-skill.md
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
| `extends`     | string   |          | —       | Qualified Name (`namespace/Agent/name`) or Short Name of another Agent to extend. When set, the engine merges the parent's `skills` and `policies` blocks. |
| `triggers`    | string[] |          | `[]`    | Defines what architectural entities can invoke this agent (e.g. `["human"]`, `["agent"]`, `["system"]`). An empty list means it can be triggered by anything. |
| `models`      | string[] |          | `[]`    | A prioritized list of LLM model identifiers (e.g. `["gpt-4o", "claude-3-5-sonnet"]`). The runtime should use the first available model. |
| `temperature` | float    |          | `0.0`   | Sampling temperature in the range `0.0`–`2.0`. Higher values produce more varied output.          |
| `thinking`    | object   |          | —       | Runtime configuration for extended reasoning (e.g., `type: effort`, `effort: high`). Overrides the provider's default. |
| `skills`      | string[] |          | `[]`    | Names or qualified refs of `Skill` resources this agent is allowed to use.                        |
| `commands`    | string[] |          | `[]`    | Names or qualified refs of `Command` resources this agent can invoke.                             |
| `subagents`   | string[] |          | `[]`    | Names or qualified refs of `Agent` resources this agent is allowed to delegate to.                |
| `policies`    | object   |          | —       | Security and access policies (specifically `tools` execution restrictions) for this agent.         |

> `instructions` is **not** written in the YAML front-matter. It is populated
> automatically from the Markdown body below the closing `---`.

#### Inheritance Merge Rules

When `spec.extends` is set the engine performs a **recursive merge**:

1. The parent agent is resolved (which may itself extend another agent, forming a chain).
2. A deep copy of the resolved parent is made as the merge base, with `metadata`, `namespace`, and `directory` updated to reflect the child agent.
3. Scalar and override fields (`models`, `temperature`, `thinking`, and `triggers`) on the child replace the parent's values when specified (non-empty / non-nil).
4. The child's `skills`, `commands`, and `subagents` lists are **appended** to the parent's lists.
5. The child's `policies` block is merged with the parent's:
   - Boolean options (`allowDangerous`, `allowOpenWorld`) in the child override those of the parent if specified.
   - Slice arrays (`include`, `exclude`) are combined using a set union.
6. The child's Markdown instructions are **concatenated** after the parent's, separated by a blank line.
7. Circular inheritance chains (including self-extension) are detected and return an error.

#### Tool Access Resolution and Policy Filtering

To support secure tool execution, the engine performs the following step during agent resolution:
1. **Policy Filter Check**: The complete list of effective tools is filtered against the agent's merged `spec.policies.tools` policy block. Only tools satisfying the policy are exposed to the agent.

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
    - commands/generate-report.md
  subagents:
    - system/Agent/writer
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

| Field          | Type         | Description                                                                              |
|----------------|--------------|------------------------------------------------------------------------------------------|
| `useWhen`      | string       | Explicit routing instructions to the LLM on when to trigger this skill (e.g. "Use this when..."). |
| `keywords`     | string[]     | Array of semantic tags used for discovery and prompting.                                 |
| `argumentHint` | string       | Single-line hint indicating expected argument format upon invocation.                    |
| `arguments`    | Argument[]   | Structured parameters accepted by the skill.                                             |

##### `Argument` fields

| Field         | Type    | Description                                                                             |
|---------------|---------|-----------------------------------------------------------------------------------------|
| `name`        | string  | Parameter identifier, accessible as `$name` or `{{.name}}`.                            |
| `description` | string  | Guidance describing what this parameter is for.                                         |
| `required`    | boolean | Whether the argument must be supplied upon invocation.                                  |
| `default`     | string  | Default fallback value if the argument is omitted.                                      |

_Note: The Markdown body below the closing YAML delimiter automatically becomes the `instructions` field._

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

| Field          | Type         | Required | Description                                                         |
|----------------|--------------|:--------:|---------------------------------------------------------------------|
| `agent`        | string       |          | Preferred or required Agent ref to execute this command.            |
| `context`      | string       |          | Conversational context mode: `inline` (default), `fork`, or `clean`.|
| `models`       | string[]     |          | Preferred models for this command. Overrides agent defaults.        |
| `tools`        | string[]     |          | Tools allowed during this command execution.                        |
| `argumentHint` | string       |          | Single-line hint indicating expected argument format upon invocation. |
| `arguments`    | Argument[]   |          | Structured parameters accepted by this command.                     |

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
  arguments:
    - name: ticker
      description: Stock ticker symbol
      required: true
    - name: year
      description: Target year
      default: "2026"
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

### 3.6 Decision

A `Decision` resource encapsulates a probabilistic decision evaluation (such as binary predicates,
categorical choices, and ordinal scoring rubrics). Its general evaluation context or rubric is
authored in the Markdown body of the file, while structured questions and criteria are declared in
`spec.questions` (or supplied dynamically at runtime).

#### `spec` fields

| Field          | Type         | Required | Description                                                                                              |
|----------------|--------------|:--------:|----------------------------------------------------------------------------------------------------------|
| `models`       | string[]     |          | Prioritized list of decision model identifiers (e.g., `["typesafe/jev-latest", "ollama/nimble"]`). Overrides workspace/provider defaults. |
| `argumentHint` | string       |          | Single-line hint indicating expected argument format upon invocation.                                    |
| `arguments`    | Argument[]   |          | Structured parameters accepted by this decision.                                                         |
| `questions`    | object[]     |          | List of structured questions (`predicate`, `choice`, `score`) evaluated by this decision.                |

> `instructions` is **not** written in the YAML front-matter when using Markdown format. It is populated
> automatically from the Markdown body below the closing `---`.

#### `DecisionQuestion` fields

| Field           | Type     | Required | Description                                                                                      |
|-----------------|----------|:--------:|--------------------------------------------------------------------------------------------------|
| `name`          | string   | ✅       | Unique identifier for this question within the `Decision`.                                       |
| `type`          | string   | ✅       | Question evaluation mode: `predicate`, `choice`, or `score`.                                     |
| `instructions`  | string   |          | Question-specific prompt or condition to evaluate.                                               |
| `threshold`     | float    |          | Minimum probability (`0.0`–`1.0`) required for a `predicate` question to pass.                   |
| `minConfidence` | float    |          | Minimum confidence (`0.0`–`1.0`) required for the evaluated answer.                              |
| `criteria`      | object   |          | Explicit positive (`true`) and negative (`false`) criteria for a `predicate` question.           |
| `options`       | object[] | *        | Candidate choices (at least 2 required when `type: choice`).                                     |
| `levels`        | object[] | *        | Ordered rubric levels (at least 2 required when `type: score`).                                  |

#### `DecisionCriteria` fields

| Field   | Type   | Description                                                    |
|---------|--------|----------------------------------------------------------------|
| `true`  | string | Description of when the predicate evaluates to `true`.         |
| `false` | string | Description of when the predicate evaluates to `false`.        |

#### `DecisionOption` fields

| Field         | Type   | Required | Description                                                    |
|---------------|--------|:--------:|----------------------------------------------------------------|
| `value`       | string | ✅       | Identifier returned when this option is selected.              |
| `description` | string |          | Semantic description of when to select this option.            |

#### `DecisionLevel` fields

| Field         | Type   | Required | Description                                                    |
|---------------|--------|:--------:|----------------------------------------------------------------|
| `label`       | string | ✅       | Label for this rubric level (e.g., `"low"`, `"medium"`, `"high"`). |
| `description` | string |          | Rubric description for this score level.                       |

#### Full example (`decisions/pr-triage.md`)

```markdown
---
apiVersion: warp/v1alpha1
kind: Decision
metadata:
  name: pr-triage
  description: Triage an incoming pull request for security, routing, and urgency.
spec:
  models: ["typesafe/jev-latest", "ollama/nimble"]
  questions:
    - name: is_security_sensitive
      type: predicate
      instructions: Does this diff touch authentication, cryptography, or permissions?
      threshold: 0.7
      criteria:
        true: Modifies auth, tokens, RBAC, secrets, or encryption logic.
        false: Only touches UI, docs, or non-sensitive business logic.

    - name: reviewer_team
      type: choice
      instructions: Which team should review this change?
      minConfidence: 0.6
      options:
        - value: security
          description: Auth, crypto, secrets, or access control changes.
        - value: backend
          description: API, database, or service logic changes.
        - value: frontend
          description: UI components, styling, or client state changes.

    - name: urgency
      type: score
      instructions: How urgent is this pull request?
      levels:
        - label: low
          description: Typo fix, chore, or minor refactor.
        - label: medium
          description: Standard feature or bug fix.
        - label: high
          description: Production hotfix or critical security patch.
---

# Pull Request Triage Rubric

Evaluate the provided pull request diff and metadata objectively against each
question's criteria.
```

---

### 3.7 ModelProvider

A `ModelProvider` resource describes an LLM provider configuration, such as API
endpoints and default models.

#### `spec` fields

| Field           | Type     | Required | Description                                                                                      |
|-----------------|----------|:--------:|--------------------------------------------------------------------------------------------------|
| `type`          | string   | ✅       | e.g., `ollama`, `openai`, `anthropic`.                                                           |
| `endpoint`      | string   | ✅       | API base URL.                                                                                    |
| `defaultModel`  | string   |          | Default completion model to use if none specified.                                               |
| `defaultModels` | object   |          | Role-specific default models (`completion`, `decision`, `embedding`) for this provider.          |
| `auth`          | object   |          | Authentication config (e.g., `type: bearer`, `env: OPENAI_API_KEY`).                             |
| `headers`       | map      |          | Custom HTTP headers included in all requests to the provider.                                    |
| `config`        | map      |          | Provider-specific settings and custom configuration parameters.                                  |
| `models`        | object[] |          | Available models from this provider.                                                             |

> **Note on configuration:** The `config` parameters and custom `headers` are entirely provider-specific. Different runtimes or provider backends may define and read different options from the `config` map (e.g., connection timeouts, system-level tweaks, or custom API options).

#### `ProviderAuth` fields

| Field    | Type   | Description                                                                 |
|----------|--------|-----------------------------------------------------------------------------|
| `type`   | string | The auth scheme (e.g., `bearer`, `api-key`, `basic`).                       |
| `header` | string | Custom HTTP header name to use if `type` is `api-key` (e.g., `x-api-key`).  |
| `env`    | string | Read the secret credential from this environment variable.                  |
| `file`   | string | Read the secret credential from this file path.                             |

#### `ProviderModel` fields

| Field          | Type     | Required | Description                                     |
|----------------|----------|:--------:|-------------------------------------------------|
| `id`           | string   | ✅       | Unique model ID (e.g., `gpt-4`).                |
| `name`         | string   | ✅       | Model name (e.g., `gpt-4`).                     |
| `label`        | string   | ✅       | Human-friendly label (e.g., `GPT-4`).           |
| `limits`       | object   | ✅       | Context, output token, and vector dimension limits. |
| `capabilities` | object   |          | Supported features and configuration boundaries.|
| `cost`         | object   |          | Token pricing and conditional tiers.            |

#### `ProviderModelCapabilities` fields

| Field              | Type     | Default | Description                                                                                 |
|--------------------|----------|---------|---------------------------------------------------------------------------------------------|
| `completion`       | bool     | `true`  | Whether the model supports text/chat completion. Set to `false` for pure decision or embedding models. |
| `decision`         | bool     | `false` | Whether the model supports probabilistic decision evaluation.                               |
| `embedding`        | bool     | `false` | Whether the model supports generating vector embeddings.                                    |
| `dimensions`       | int[]    | —       | Configurable output vector dimensions supported by an embedding model (e.g., `[256, 512, 1536]`). |
| `attachment`       | bool     | `false` | Whether the model supports file/document attachments.                                       |
| `tools`            | bool     | `false` | Whether the model supports function/tool calling (`tool_call`).                             |
| `structuredOutput` | bool     | `false` | Whether the model supports strict structured outputs.                                       |
| `temperature`      | bool     | `false` | Whether the model allows configuring the sampling temperature.                              |
| `thinking`         | object[] | —       | List of supported reasoning configuration strategies.                                       |
| `modalities`       | object   | —       | Defines supported input and output data modalities.                                         |

#### `ProviderModelModalities` fields

| Field    | Type     | Description                                                        |
|----------|----------|--------------------------------------------------------------------|
| `input`  | string[] | Supported input modalities (e.g., `["text", "image", "video"]`).   |
| `output` | string[] | Supported output modalities (e.g., `["text", "image"]`).           |

#### `ProviderModelThinkingCapability` fields

| Field             | Type     | Required | Description                                                              |
|-------------------|----------|:--------:|--------------------------------------------------------------------------|
| `type`            | string   | ✅       | The strategy this model supports: `toggle`, `effort`, `budget`, or `adaptive`. |
| `isDefault`       | bool     |          | Marks this strategy as the primary default if the agent does not specify one. |
| `allowedEfforts`  | string[] |          | Used if `type: effort`. e.g., `["low", "medium", "high"]`.               |
| `maxBudgetTokens` | int      |          | Used if `type: budget`. The maximum allowed thinking tokens.             |
| `default`         | any      |          | The default value (e.g., `true` for toggle, `"medium"` for effort, `1024` for budget). |

#### `ProviderModelCost` fields

| Field        | Type     | Description                                                          |
|--------------|----------|----------------------------------------------------------------------|
| `input`      | float    | Base cost for input tokens (typically per 1M tokens).                |
| `output`     | float    | Base cost for output tokens (optional for embedding/decision models).|
| `cacheRead`  | float    | Cost for reading from prompt cache.                                  |
| `cacheWrite` | float    | Cost for writing to prompt cache.                                    |
| `tiers`      | object[] | Array of conditional pricing tiers (e.g., for large contexts).       |

#### `ProviderModelCostTier` fields

| Field        | Type   | Description                                                                 |
|--------------|--------|-----------------------------------------------------------------------------|
| `label`      | string | A human-readable label for the tier (e.g., `"Context over 200k"`).          |
| `tier`       | object | The condition that triggers this pricing (e.g., `{ type: "context", size: 200000 }`). |
| `input`      | float  | Override cost for input tokens when this tier is active.                    |
| `output`     | float  | Override cost for output tokens (optional for embedding/decision models).   |
| `cacheRead`  | float  | Override cost for cache read.                                               |
| `cacheWrite` | float  | Override cost for cache write.                                              |

#### `ProviderModelLimits` fields

| Field        | Type | Required | Description                                                             |
|--------------|------|:--------:|-------------------------------------------------------------------------|
| `context`    | int  | ✅       | Max context length in tokens.                                           |
| `output`     | int  |          | Max output length in tokens (optional for non-completion models).       |
| `dimensions` | int  |          | Default or maximum output vector size for embedding models.             |

#### Full example (`providers/anthropic.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: anthropic
  description: Anthropic API
spec:
  type: anthropic
  endpoint: https://api.anthropic.com
  defaultModel: claude-3-7-sonnet
  headers:
    X-Custom-Client-Name: "warp-platform"
  config:
    retryAttempts: 3
    timeoutMs: 15000
  models:
    - id: claude-3-7-sonnet
      name: claude-3-7-sonnet-20250219
      label: Claude 3.7 Sonnet
      capabilities:
        tools: true
        modalities:
          input: ["text", "image"]
          output: ["text"]
        thinking:
          - type: budget
            isDefault: true
            maxBudgetTokens: 128000
            default: 4096
      limits:
        context: 200000
        output: 128000
      cost:
        input: 1.25
        output: 10.0
        cacheRead: 0.31
        cacheWrite: 2.375
        tiers:
          - label: "Context over 200k"
            tier:
              type: context
              size: 200000
            input: 2.5
            output: 15.0
            cacheRead: 0.25
```

---

### 3.8 Tool

A `Tool` resource describes a custom tool that an agent can invoke.

#### `spec` fields

| Field         | Type     | Required | Description                                                                 |
|---------------|----------|:--------:|-----------------------------------------------------------------------------|
| `command`     | string[] | ✅       | Executable and static args (e.g., `["python", "script.py"]`).               |
| `env`         | map      |          | Environment variables injected into the process.                            |
| `inputSchema`  | map      |          | JSON Schema object defining arguments the LLM must pass.                    |
| `outputSchema`| map      |          | JSON Schema object defining the tool's output.                              |
| `annotations` | object   |          | Safety profile for Tool Execution Security.                                 |

> `instructions` is **not** written in the YAML front-matter. It is populated
> automatically from the Markdown body below the closing `---`. This Markdown body
> serves as the detailed description of what the tool does (sent to the LLM).

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
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: Directory path to list.
```

---

### 3.9 MCP

An `MCP` resource describes an [Model Context Protocol](https://modelcontextprotocol.io)
server.

#### `spec` fields

| Field         | Type     | Required | Description                                     |
|---------------|----------|:--------:|-------------------------------------------------|
| `type`        | string   |          | Transport type: `stdio` (default), `sse`, `http`, or `streamable-http`. |
| `command`     | string[] | *        | Command to start the MCP server (required if `type: stdio`). |
| `endpoint`    | string   | *        | HTTP URL for the MCP server (required if `type` is `sse`, `http`, or `streamable-http`). |
| `env`         | map      |          | Environment variables for the MCP server.       |
| `auth`        | object   |          | `ProviderAuth` configuration (`type`, `header`, `env`, `file`) for remote MCP servers. |
| `headers`     | map      |          | Custom HTTP headers included in requests to remote MCP servers. |
| `annotations` | object   |          | Default safety profile for all exposed tools.   |
| `policies`    | object   |          | Controls which external MCP resources (`tools`, `prompts`, `resources`) are exposed by this server. |
| `overrides`   | map      |          | Tool-specific annotation overrides (keyed by tool name). |

#### `MCPPolicies` fields

| Field       | Type   | Description                                                                    |
|-------------|--------|--------------------------------------------------------------------------------|
| `tools`     | object | `ResourceFilter` (`include`/`exclude`) controlling which tools are exposed.    |
| `prompts`   | object | `ResourceFilter` (`include`/`exclude`) controlling which prompts are exposed.  |
| `resources` | object | `ResourceFilter` (`include`/`exclude`) controlling which resources are exposed.|

#### `ResourceFilter` fields

| Field     | Type     | Description                                                    |
|-----------|----------|----------------------------------------------------------------|
| `include` | string[] | Glob patterns for resources to expose.                         |
| `exclude` | string[] | Glob patterns for resources to block (applied after `include`).|

#### Full example (`mcps/sqlite.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: sqlite-mcp
spec:
  type: stdio
  command: ["npx", "-y", "@modelcontextprotocol/server-sqlite", "--db", "data.db"]
```

#### Full example SSE (`mcps/remote.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: remote-mcp
spec:
  type: sse
  endpoint: "http://localhost:8080/sse"
```

---

### 3.10 Toolkit

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
      inputSchema:
        type: object
        properties:
          msg: { type: string }
```

### 3.11 Plugin

A `Plugin` resource (typically named `PLUGIN.md` or `PLUGIN.yaml`) is placed at the root of a repository to declare it as a distributable WARP package. It acts as the manifest for the repository, telling the loader where to find the resources and which ones are meant for public consumption.

#### `spec` fields

| Field         | Type     | Required | Default    | Description                                                                                             |
|---------------|----------|:--------:|------------|---------------------------------------------------------------------------------------------------------|
| `resourceDir` | string   |          | `.agents/` | The relative path within the repository where the loader should look for resources.                     |
| `exports`     | string[] |          | `["*"]`    | Glob patterns defining which resources are exposed to consumers. Resources not matching are private.    |
| `hooks`       | object   |          | —          | Setup and installation hooks executed when the plugin is installed.                                     |

#### `PluginHooks` fields

| Field         | Type       | Description                                                                                             |
|---------------|------------|---------------------------------------------------------------------------------------------------------|
| `postInstall` | string[][] | Commands to run after the plugin is downloaded (e.g., `[["go", "install", "./..."]]`). |

#### Full example (`PLUGIN.md`)

```markdown
---
apiVersion: warp/v1alpha1
kind: Plugin
metadata:
  name: acme-finance
  description: Official financial analysis skills and tools for Acme Corp.
spec:
  resourceDir: "src/agents"
  exports:
    - skills/analysis
    - commands/*
---

# Acme Finance Plugin
This repository provides standard financial analysis skills.
```

---

### 3.12 Rule

A `Rule` resource defines ambient constraints, coding standards, security guardrails, or architectural invariants that agents must adhere to within a project or workspace scope. Unlike `Skill` resources (which represent active, opt-in capabilities), rules represent passive governance and boundaries that are evaluated and injected based on multi-modal triggers.

#### `spec` fields

| Field      | Type         | Required | Default | Description                                                                                   |
|------------|--------------|:--------:|---------|-----------------------------------------------------------------------------------------------|
| `triggers` | `RuleTriggers`|          | —       | Activation conditions that determine when this rule applies.                                 |
| `priority` | integer      |          | `0`     | Ordering priority when multiple rules are resolved (higher numbers take precedence).        |

#### `RuleTriggers` fields

| Field      | Type     | Required | Default | Description                                                                                             |
|------------|----------|:--------:|---------|---------------------------------------------------------------------------------------------------------|
| `always`   | boolean  |          | `false` | When `true`, this rule applies unconditionally within its scope.                                       |
| `globs`    | string[] |          | —       | Path patterns (e.g. `["**/*.go", "!**/vendor/**"]`) that activate this rule when matching files are edited. |
| `useWhen`  | string   |          | —       | Semantic instructions to an LLM or router model describing when this rule should be active.             |
| `keywords` | string[] |          | —       | Array of semantic tags used for discovery and prompting.                                               |
| `decision` | string   |          | —       | Qualified or Short Name of a `Decision` resource evaluated to determine whether to activate this rule.  |
| `tools`    | string[] |          | —       | List of Tool or Toolkit names that activate this rule when invoked by the agent.                        |

#### Full example (`.agents/rules/go-concurrency.md`)

```markdown
---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: go-concurrency
  description: Concurrency standards and mutex guidelines for Go
spec:
  priority: 80
  triggers:
    globs:
      - "**/*.go"
      - "!**/vendor/**"
    useWhen: "When writing concurrent Go code, spawning goroutines, or managing shared state"
    keywords: ["goroutine", "mutex", "channel", "sync"]
    tools: ["bash"]
---

# Go Concurrency Guidelines

1. Always acquire mutex locks with defer immediately following the lock call:
   ```go
   mu.Lock()
   defer mu.Unlock()
   ```
2. Never spawn unmonitored goroutines; always pass a `context.Context` and handle cancellation.
3. Use `sync/atomic` for single-variable counters instead of mutexes where appropriate.
```

---

## 4. Cross-References

An `Agent` resource may declare lists of skill, command, and subagent references under `spec.skills`,
`spec.commands`, and `spec.subagents`. A reference can be either a **Qualified Name** or a **Short Name**.

- **Qualified Name**: `namespace/kind/name` (e.g. `system/Skill/python`). Directs the loader to exactly one resource in the registry.
- **Short Name**: `name` (e.g. `python`). The loader will iterate through the standard namespaces in priority order to find the first matching kind and name.

```yaml
spec:
  skills:
    - system/Skill/finance   # Qualified Name: Explicitly targets the system-level finance skill
  commands:
    - report                 # Short Name: Resolves using Search Path Priority
  subagents:
    - analyst                # Short Name: Resolves to an Agent using Search Path Priority
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
   - Check for `AGENTS.md` (case-insensitive) in the project directory.
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
│   ├── defs/
│   │   └── analyst.md
│   ├── skills/
│   │   └── coding.md
│   ├── rules/
│   │   └── style.md
│   └── commands/
│       └── review.md
services/
├── api/
│   ├── AGENTS.md          # Context — identity for the api project
│   └── .agents/          # Project-local resources
│       ├── skills/
│       │   └── grpc.md
│       └── commands/
│           └── gen-proto.md

└── auth/
    ├── AGENTS.md
    └── .agents/
        └── skills/
            └── jwt.md
```

A minimal single-project setup (no `WORKSPACE.md` needed):

```
AGENTS.md                  # Context for the single project
.agents/
├── skills/
└── commands/
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
| `kind` is a known value      | Value is not one of `Workspace`, `Context`, `Agent`, `Skill`, `Command`, `Decision`, `Rule`, `ModelProvider`, `Tool`, `MCP`, `Toolkit`, `Plugin`. |

### 7.2 Agent-specific

| Rule                              | Error condition                                                              |
|-----------------------------------|------------------------------------------------------------------------------|
| All `spec.skills` paths resolve   | A listed path does not match any loaded `Skill` in the resolution hierarchy. |
| All `spec.commands` paths resolve | A listed path does not match any loaded `Command` in the resolution hierarchy. |
| All `spec.subagents` paths resolve | A listed path does not match any loaded `Agent` in the resolution hierarchy. |
| All `spec.rules` paths resolve    | A listed path does not match any loaded `Rule` in the resolution hierarchy. |
| Declared `spec.models` support completion | A listed model resolves to a `ProviderModel` whose `capabilities.completion` is explicitly `false`. |

### 7.3 Workspace-specific

| Rule                                | Error condition                                                |
|-------------------------------------|----------------------------------------------------------------|
| All `spec.projects` paths exist     | A listed path (when not `"*"`) does not exist on the filesystem. |
| `spec.defaultModels` capabilities match role | A model in `defaultModels` resolves to a `ProviderModel` lacking the required role capability (`completion`, `decision`, or `embedding`). |

### 7.4 Plugin-specific

| Rule                                | Error condition                                                |
|-------------------------------------|----------------------------------------------------------------|
| No structural resources in exports  | The `spec.exports` list matches a resource of kind `Workspace`, `Context`, or `Plugin`. |

### 7.5 Decision-specific

| Rule                                | Error condition                                                |
|-------------------------------------|----------------------------------------------------------------|
| Unique question names               | Any `spec.questions` entry has an empty `name` or duplicates another question's `name`. |
| Valid question type                 | A question's `type` is not `predicate`, `choice`, or `score`.  |
| Valid probability bounds            | `threshold` or `minConfidence` is outside `[0.0, 1.0]`.        |
| Choice options                      | A `choice` question has fewer than 2 `options` or an option with an empty `value`. |
| Score levels                        | A `score` question has fewer than 2 `levels` or a level with an empty `label`. |
| Declared `spec.models` support decision | A listed model resolves to a `ProviderModel` whose `capabilities.decision` is `false`. |

### 7.6 Rule-specific

| Rule                                | Error condition                                                |
|-------------------------------------|----------------------------------------------------------------|
| Valid `triggers.decision` reference | A declared decision reference does not match any loaded `Decision` in the resolution hierarchy. |
| Valid `triggers.tools` references   | A declared tool reference does not match any loaded `Tool` or `Toolkit` in the resolution hierarchy. |

---

## 8. Parsing Algorithm

The following algorithm describes how a conforming parser handles standard resources and the two
special inferred-manifest filenames.

```
function parse(filePath, fileContent):
  1. fileName = basename(filePath)
  2. isWorkspaceFile = (fileName.toLowerCase() == "workspace.md")
  3. isContextFile   = (fileName.toLowerCase() == "agents.md")

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
  7. IF kind is not in {Workspace, Context, Agent, Skill, Command, Decision, ModelProvider, Tool, MCP, Toolkit, Plugin}:
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

| Input path                                          | Slug                  |
|-----------------------------------------------------|-----------------------|
| `/home/user/repos/platform/AGENTS.md`               | `agents`              |
| `/home/user/repos/platform/services/api/AGENTS.md`  | `services-api-agents` |
| `/home/user/repos/platform/WORKSPACE.md`            | `workspace`           |

---

## 9. Modules and Package Management

### 9.1 Overview

WARP supports a robust module system for sharing resources across repositories. Instead of relying purely on dynamic runtime fetching, WARP uses semantic module paths, global caching, and cryptographic lock files to ensure fast, deterministic, and secure resource loading.

A repository becomes a WARP package simply by including a `kind: Plugin` manifest at its root (e.g., `PLUGIN.md`).

### 9.2 Declaring Plugins in a Workspace

Consumers import external plugins by declaring them in the `spec.plugins` list of their `Workspace` resource.

```yaml
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: my-workspace
spec:
  projects: ["services/api"]
  plugins:
    - source: github.com/acme/finance-skills
      version: v1.2.0
      namespace: ext-finance  # Explicitly override the namespace
    - source: github.com/shared/dev-tools
      version: latest
      # If namespace is omitted, WARP infers it from the last segment of the URI.
      # Here, the namespace will automatically be 'dev-tools'.
```

#### `WorkspacePlugin` fields

| Field       | Type   | Required | Description                                                                                 |
|-------------|--------|:--------:|---------------------------------------------------------------------------------------------|
| `source`    | string | ✅       | Semantic URI of the repository (e.g., `github.com/org/repo`).                               |
| `version`   | string | ✅       | The git tag, branch, or commit hash to pin (e.g., `v1.2.0`, `main`).                        |
| `namespace` | string |          | Namespace under which these resources are registered. Defaults to the last URI segment.     |

### 9.3 Resolution and the Default Namespace Rule

When a workspace declares a plugin, all exported resources from that plugin are registered in the loader's registry under a specific namespace.

**The Default Namespace Rule:**
If the consumer does not explicitly provide a `namespace` in the `WORKSPACE.md` definition, the loader **must** infer the namespace using the last segment of the `source` URI.

*Example:*
- `source: github.com/acme/finance-skills` → inferred namespace: `finance-skills`
- Resources inside the plugin will be accessible via `finance-skills/Skill/analysis`.

This ensures that plugin creators do not dictate namespaces (preventing collisions), while providing a zero-configuration fallback for consumers.

### 9.4 Caching and the Lock File

To guarantee reproducibility, security, and performance, WARP implements a global caching and lock file mechanism.

1. **Global Cache**: When the loader encounters a plugin, it checks a global host cache (e.g., `~/.warp/pkg/mod/`). If the specific version is missing, it is downloaded and cached. Subsequent loads are instantaneous from disk.
   * **Local Path Bypass**: If the plugin source is a local relative (e.g. `./plugins/my-plugin`) or absolute filesystem path, WARP intentionally bypasses the global cache and links directly to the live local directory. This ensures that any modifications made to the local plugin are instantly reflected in the workspace without needing to re-fetch.
2. **`warp.lock`**: Tooling generates a machine-readable `warp.lock` file adjacent to the `WORKSPACE.md`. This file must be committed to version control. It records the exact version and a cryptographic hash (e.g., SHA-256) of the downloaded package's directory tree.
3. **Validation**: During the load phase, the loader verifies the cached package against the hash in `warp.lock`. If the hash does not match (indicating tampering or a network failure), the loader aborts, preventing supply-chain attacks.

#### Example `warp.lock`
```text
# This file is automatically generated by warp. DO NOT EDIT.
github.com/acme/finance-skills v1.2.0 h1:9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b
github.com/acme/finance-skills v1.2.0/PLUGIN.md h1:1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b
```

### 9.5 CLI Workflow (`warp get`)

The WARP CLI provides an ergonomic developer experience for managing plugins, actively assisting the user in curating their workspace.

When a user runs `warp get <source>@<version>`, the CLI performs the following steps:

1. **Fetch**: Downloads the repository to the global cache and parses the `PLUGIN.md` to identify available `exports`.
2. **Interactive Filtering**: If the plugin exports multiple resources, the CLI presents an interactive prompt allowing the user to select exactly which skills, commands, or tools they wish to import.
3. **Mutation**: The CLI automatically rewrites the `WORKSPACE.md` file, appending the plugin definition and scaffolding the `imports` filter block based on the user's selections.
4. **Locking**: The CLI computes the cryptographic hashes and updates the `warp.lock` file.

To perform operations non-interactively (e.g., in CI/CD environments), the CLI supports flag-based filtering:
```bash
warp get github.com/acme/finance-skills@v1.2.0 --include="Skill/analysis,Command/*"
```

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

---

## 11. Templating

WARP provides a unified templating system for rendering the `instructions` field of any resource. Runtimes **should** expand these templates before sending prompts to the LLM.

The reference implementation uses Go's `text/template` engine, with a preprocessing step to support a developer-friendly shorthand syntax.

### Shorthand Substitution

You can use shell-style variables directly in your Markdown instructions:

- **Named Variables:** `$Name`, `$DisplayName`, `${Description}`
- **Built-in Path Shorthands:** `$WorkspaceDir`, `$WorkspacePath`, `$ProjectDir`, `$ContextPath`
- **Positional Arguments:** `$1`, `$2` (used in `Command`, `Decision`, and `Skill` resources)
- **Argument References:** When a resource defines arguments (e.g. `arguments: [{name: "ticker"}]`), you can use `$ticker` (or `{{.ticker}}`) to refer to the argument.
- **Escaping:** Use `$$` to output a literal `$`.

_Under the hood, the runtime translates `$Var` into Go template syntax `{{.Var}}` and `$1` into `{{index . "1"}}`._

### Flattened Resource Context

To minimize boilerplate, the template context lifts common metadata and spec fields to the top level:

* `Name` (from `metadata.name`)
* `DisplayName` (from `metadata.displayName`, falls back to `Name`)
* `Description` (from `metadata.description`)
* `Labels` (from `metadata.labels`)
* `Models`, `Skills`, `Tools`, `Commands`, `Subagents`, `Triggers`, `Questions`, `Temperature`, `Args`, `ArgumentHint`, `Arguments` (lifted from the respective `spec` fields and render arguments)

### Scoped Template Objects

When rendering resources via their typed `Render` methods (`Workspace.Render`, `Context.Render`, `Agent.Render`, `Skill.Render`, `Command.Render`, `Decision.Render`), structured view models are injected into the template context:

| Object | Fields | Description |
|--------|--------|-------------|
| `{{.Workspace}}` | `.Dir`, `.Path` | Workspace root directory and path to `WORKSPACE.md` (also available as `$WorkspaceDir`, `$WorkspacePath`). |
| `{{.Project}}` | `.Name`, `.DisplayName`, `.Dir` | Active project slug, display name, and absolute directory path (also available as `$ProjectDir`). |
| `{{.Context}}` | `.Path`, `.Instructions` | Path to the project's `AGENTS.md` file and its raw instructions (path also available as `$ContextPath`). |
| `{{.Agent}}` | `.Name`, `.Description`, `.Dir`, `.Path`, `.Skills`, `.Tools`, `.Commands`, `.Subagents` | Hydrated metadata and resolved capabilities (`[]TemplateSkill`, `[]TemplateTool`, `[]TemplateCommand`, `[]TemplateAgent`) for the agent. |
| `{{.Skill}}` | `.Name`, `.Description`, `.UseWhen`, `.Keywords`, `.ArgumentHint`, `.Arguments`, `.Dir`, `.Path` | Skill metadata, routing hints, argument definitions, directory, and file path. |
| `{{.Command}}` | `.Name`, `.Description`, `.Agent`, `.Context`, `.ArgumentHint`, `.Arguments`, `.Dir`, `.Path`, `.Tools` | Command metadata, executing agent, context mode, argument definitions, directory, file path, and tools. |
| `{{.Decision}}` | `.Name`, `.Description`, `.ArgumentHint`, `.Arguments`, `.Dir`, `.Path` | Decision metadata, argument definitions, directory, and file path. |

### Advanced Templating

Because it is backed by Go templates, you can write complex logic for arrays or conditionals:

```markdown
I am $DisplayName operating in {{.Project.DisplayName}} ($ProjectDir).
My skills are:
{{range .Agent.Skills}}
- {{.Name}}: {{.Description}} ({{.Path}})
{{end}}
```

### Custom Globals

Implementors (runtimes) can pass a custom `Globals` map in the render options (e.g., `RenderOptions`, `AgentRenderOptions`, `ContextRenderOptions`) to expose additional contextual variables (such as `$PWD` or `$CI_ENV`).


