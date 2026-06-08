package warp

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseResult is the output of a successful Parse call. It carries the
// detected resource Kind and the fully-decoded, typed resource value.
type ParseResult struct {
	// Kind is the resource kind decoded from the front-matter.
	Kind Kind
	// Resource is the fully-typed resource pointer (*WorkspaceDef, *Context,
	// *Agent, *Skill, or *Command).
	Resource any
	// Inferred is true when the resource metadata was inferred rather than
	// read from an explicit YAML front-matter block. This happens when a
	// WORKSPACE.md or AGENT.md file is loaded without front-matter delimiters.
	Inferred bool
}

// Parse splits a warp Markdown file into its YAML front-matter and Markdown
// body, decodes the resource kind, unmarshals the appropriate typed struct,
// and injects the body text as the resource's Instructions field.
//
// filePath is the path of the file relative to the loader root. It is used
// to detect special files that may omit the YAML front-matter block:
//   - WORKSPACE.md (case-insensitive) is inferred as a Workspace resource.
//   - AGENT.md (case-insensitive) is inferred as a Context resource.
//
// A valid warp file has the form:
//
//	---
//	apiVersion: warp/v1alpha1
//	kind: Agent
//	...
//	---
//	# Markdown instructions here
//
// Parse returns an error if the delimiters are absent (and the file is not a
// special inferred kind), the front-matter is malformed, or the kind is
// unsupported.
func Parse(filePath, content string) (*ParseResult, error) {
	fileName := path.Base(filePath)
	isWorkspaceFile := strings.EqualFold(fileName, "workspace.md")
	isContextFile := strings.EqualFold(fileName, "agent.md")
	ext := strings.ToLower(path.Ext(filePath))
	isYamlFile := ext == ".yaml" || ext == ".yml"

	var yamlPart, markdownPart string
	if isYamlFile {
		yamlPart = content
	} else {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) < 3 {
			switch {
			case isWorkspaceFile:
				// WORKSPACE.md with no front-matter: infer the Workspace metadata.
				w := &WorkspaceDef{
					BaseResource: BaseResource{
						APIVersion: APIVersion,
						Kind:       KindWorkspace,
						Metadata: Metadata{
							Name: slugifyPath(filePath),
						},
					},
					Spec: WorkspaceDefSpec{
						Instructions: strings.TrimSpace(content),
					},
				}
				return &ParseResult{Kind: KindWorkspace, Resource: w, Inferred: true}, nil
			case isContextFile:
				// AGENT.md with no front-matter: infer the Context metadata.
				c := &Context{
					BaseResource: BaseResource{
						APIVersion: "warp/v1alpha1",
						Kind:       KindContext,
						Metadata: Metadata{
							Name: slugifyPath(filePath),
						},
					},
					Spec: ContextSpec{
						Instructions: strings.TrimSpace(content),
					},
				}
				return &ParseResult{Kind: KindContext, Resource: c, Inferred: true}, nil
			default:
				return nil, fmt.Errorf("invalid file format: missing YAML front-matter delimiters")
			}
		}
		yamlPart = parts[1]
		markdownPart = strings.TrimSpace(parts[2])
	}

	// First pass: determine the kind.
	var base BaseResource
	if err := yaml.Unmarshal([]byte(yamlPart), &base); err != nil {
		return nil, fmt.Errorf("failed to parse front-matter: %w", err)
	}

	var resource any
	switch base.Kind {
	case KindWorkspace:
		var w WorkspaceDef
		if err := yaml.Unmarshal([]byte(yamlPart), &w); err != nil {
			return nil, fmt.Errorf("failed to parse Workspace spec: %w", err)
		}
		w.Spec.Instructions = markdownPart
		resource = &w
	case KindContext:
		var c Context
		if err := yaml.Unmarshal([]byte(yamlPart), &c); err != nil {
			return nil, fmt.Errorf("failed to parse Context spec: %w", err)
		}
		c.Spec.Instructions = markdownPart
		resource = &c
	case KindAgent:
		var a Agent
		if err := yaml.Unmarshal([]byte(yamlPart), &a); err != nil {
			return nil, fmt.Errorf("failed to parse Agent spec: %w", err)
		}
		a.Spec.Instructions = markdownPart
		resource = &a
	case KindSkill:
		var s Skill
		if err := yaml.Unmarshal([]byte(yamlPart), &s); err != nil {
			return nil, fmt.Errorf("failed to parse Skill spec: %w", err)
		}
		s.Spec.Instructions = markdownPart
		resource = &s
	case KindCommand:
		var c Command
		if err := yaml.Unmarshal([]byte(yamlPart), &c); err != nil {
			return nil, fmt.Errorf("failed to parse Command spec: %w", err)
		}
		c.Spec.Instructions = markdownPart
		resource = &c
	case KindModelProvider:
		var res ModelProvider
		if err := yaml.Unmarshal([]byte(yamlPart), &res); err != nil {
			return nil, fmt.Errorf("failed to parse ModelProvider spec: %w", err)
		}
		resource = &res
	case KindTool:
		var res Tool
		if err := yaml.Unmarshal([]byte(yamlPart), &res); err != nil {
			return nil, fmt.Errorf("failed to parse Tool spec: %w", err)
		}
		resource = &res
	case KindMCP:
		var res MCP
		if err := yaml.Unmarshal([]byte(yamlPart), &res); err != nil {
			return nil, fmt.Errorf("failed to parse MCP spec: %w", err)
		}
		resource = &res
	case KindToolkit:
		var res Toolkit
		if err := yaml.Unmarshal([]byte(yamlPart), &res); err != nil {
			return nil, fmt.Errorf("failed to parse Toolkit spec: %w", err)
		}
		resource = &res
	case KindPlugin:
		var p Plugin
		if err := yaml.Unmarshal([]byte(yamlPart), &p); err != nil {
			return nil, fmt.Errorf("failed to parse Plugin spec: %w", err)
		}
		resource = &p
	default:
		return nil, fmt.Errorf("unsupported resource kind: %s", base.Kind)
	}

	return &ParseResult{
		Kind:     base.Kind,
		Resource: resource,
	}, nil
}

// slugifyPath converts a file path into a name suitable for use as a resource
// identifier. It removes the extension (.md, .yaml, or .yml), replaces path
// separators with hyphens, and lower-cases the result.
//
// When called with a path relative to the workspace root (or agents directory),
// the resulting slug is stable and unique within its scope.
func slugifyPath(filePath string) string {
	name := filePath
	for _, ext := range []string{".md", ".yaml", ".yml"} {
		if strings.HasSuffix(strings.ToLower(name), ext) {
			name = name[:len(name)-len(ext)]
			break
		}
	}
	name = strings.TrimPrefix(name, "./")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	return strings.ToLower(name)
}
