// Package warp implements the Workspace Agent Resource Protocol (WARP) — a
// provider-agnostic, declarative format for defining AI agents and their
// supporting resources.
//
// Warp is not tied to any particular LLM provider, orchestration framework,
// or runtime. Any tool that understands the format can load and execute the
// same resource files unchanged.
//
// # File Format
//
// Each warp resource is either a Markdown file (delimited by "---") or a
// pure YAML/YML configuration file.
//
// For Markdown files, the YAML front-matter carries metadata and
// configuration, while the Markdown body below the closing delimiter
// becomes the resource's Instructions field.
//
//	---
//	apiVersion: warp/v1alpha1
//	kind: Agent
//	metadata:
//	  name: my-agent
//	  description: A helpful assistant.
//	spec:
//	  model: gpt-4o
//	  temperature: 0.7
//	  skills:
//	    - skills/finance.md
//	  commands:
//	    - cmd/report.md
//	---
//
//	# My Agent
//
//	You are a helpful assistant that specialises in...
//
// # Resource Kinds
//
// Nine resource kinds are supported:
//
//	Workspace      – root authority for the session; declares active projects
//	                 (WORKSPACE.md, optionally without front-matter).
//	Context        – identity and instructions for a project scope
//	                 (AGENT.md, optionally without front-matter).
//	Agent          – an autonomous agent with model configuration and references
//	                 to skills and commands it may use.
//	Skill          – a bundle of expertise guidelines for a specific domain.
//	Command        – a discrete, reusable operation an agent can invoke.
//	ModelProvider  – configuration for an LLM provider.
//	Tool           – a custom tool definition.
//	MCP            – a Model Context Protocol server configuration.
//	Toolkit        – a collection of referenced or inline tools.
//
// # Loading Resources
//
// Use [LoadDefault] to start discovery from the current working directory:
//
//	ws, err := warp.LoadDefault()
//
// Use [Load] to specify a custom starting directory:
//
//	ws, err := warp.Load("/custom/path")
//
// Use [LoadWorkspace] for explicit control over the starting directory:
//
//	ws, err := warp.LoadWorkspace("/absolute/path")
//
// After loading, call [Workspace.Validate] to verify structural correctness
// and resolve cross-references declared inside Agent specs.
package warp
