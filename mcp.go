package warp

import (
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"strings"
)

// MCPTransport defines the transport protocol for an MCP server.
type MCPTransport string

const (
	MCPTransportStdio          MCPTransport = "stdio"
	MCPTransportSSE            MCPTransport = "sse"
	MCPTransportHTTP           MCPTransport = "http"
	MCPTransportStreamableHTTP MCPTransport = "streamable-http"
)

// MCP is a warp resource that describes an MCP server.
type MCP struct {
	BaseResource `yaml:",inline"`
	// Spec holds the MCP-specific configuration.
	Spec MCPSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the MCP.
func (m *MCP) DeepCopy() *MCP {
	if m == nil {
		return nil
	}
	out := new(MCP)
	out.BaseResource = *m.BaseResource.DeepCopy()
	out.Spec = *m.Spec.DeepCopy()
	return out
}

// MCPSpec contains the configuration details for an MCP resource.
type MCPSpec struct {
	Type        MCPTransport              `yaml:"type,omitempty" json:"type,omitempty"`               // Transport type: stdio, sse, http, or streamable-http
	Endpoint    string                    `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`       // HTTP URL for the MCP server if type is sse, http, or streamable-http
	Command     []string                  `yaml:"command,omitempty" json:"command,omitempty"`         // Command to start the MCP server via stdio
	Env         map[string]string         `yaml:"env,omitempty" json:"env,omitempty"`                 // Environment variables for the MCP server
	Auth        *ProviderAuth             `yaml:"auth,omitempty" json:"auth,omitempty"`               // Authentication configuration for remote MCP servers
	Headers     map[string]string         `yaml:"headers,omitempty" json:"headers,omitempty"`         // Custom HTTP headers for remote MCP servers
	Annotations *ToolAnnotation           `yaml:"annotations,omitempty" json:"annotations,omitempty"` // Default safety profile for all exposed tools
	Policies    *MCPPolicies              `yaml:"policies,omitempty" json:"policies,omitempty"`       // Controls which external MCP resources are exposed
	Overrides   map[string]ToolAnnotation `yaml:"overrides,omitempty" json:"overrides,omitempty"`     // Tool-specific annotation overrides (key is tool name)
}

// MCPPolicies defines inclusion and exclusion rules for resources exposed by an MCP server.
type MCPPolicies struct {
	Tools     *ResourceFilter `yaml:"tools,omitempty"`
	Prompts   *ResourceFilter `yaml:"prompts,omitempty"`
	Resources *ResourceFilter `yaml:"resources,omitempty"`
}

// DeepCopy returns a deep copy of the MCPPolicies.
func (in *MCPPolicies) DeepCopy() *MCPPolicies {
	if in == nil {
		return nil
	}
	out := new(MCPPolicies)
	out.Tools = in.Tools.DeepCopy()
	out.Prompts = in.Prompts.DeepCopy()
	out.Resources = in.Resources.DeepCopy()
	return out
}

// DeepCopy returns a deep copy of the MCPSpec.
func (in *MCPSpec) DeepCopy() *MCPSpec {
	if in == nil {
		return nil
	}
	out := new(MCPSpec)
	*out = *in
	if in.Command != nil {
		out.Command = make([]string, len(in.Command))
		copy(out.Command, in.Command)
	}
	if in.Env != nil {
		out.Env = make(map[string]string, len(in.Env))
		maps.Copy(out.Env, in.Env)
	}
	out.Auth = in.Auth.DeepCopy()
	if in.Headers != nil {
		out.Headers = make(map[string]string, len(in.Headers))
		maps.Copy(out.Headers, in.Headers)
	}
	out.Annotations = in.Annotations.DeepCopy()
	out.Policies = in.Policies.DeepCopy()
	if in.Overrides != nil {
		out.Overrides = make(map[string]ToolAnnotation, len(in.Overrides))
		maps.Copy(out.Overrides, in.Overrides)
	}
	return out
}

// MCPRenderOptions supplies external context when rendering MCP command and endpoint templates.
type MCPRenderOptions struct {
	Workspace *Workspace
	Project   *Project
	Agent     *Agent
	Globals   map[string]any
}

func (m *MCP) buildGlobals(opts *MCPRenderOptions) map[string]any {
	mergedGlobals := make(map[string]any)
	if opts != nil && opts.Globals != nil {
		maps.Copy(mergedGlobals, opts.Globals)
	}

	if opts != nil && opts.Workspace != nil {
		tw := TemplateWorkspace{
			Dir:  opts.Workspace.RootPath,
			Path: filepath.Join(opts.Workspace.RootPath, WorkspaceFileName),
		}
		mergedGlobals["Workspace"] = tw
		mergedGlobals["WorkspaceDir"] = tw.Dir
		mergedGlobals["WorkspacePath"] = tw.Path
	} else {
		mergedGlobals["Workspace"] = TemplateWorkspace{}
		mergedGlobals["WorkspaceDir"] = ""
		mergedGlobals["WorkspacePath"] = ""
	}

	if opts != nil && opts.Project != nil {
		var displayName string
		if opts.Project.Context != nil && opts.Project.Context.Metadata.DisplayName != "" {
			displayName = opts.Project.Context.Metadata.DisplayName
		} else {
			displayName = opts.Project.Name
		}

		tp := TemplateProject{
			Name:        opts.Project.Name,
			DisplayName: displayName,
			Dir:         opts.Project.AbsPath(),
		}
		mergedGlobals["Project"] = tp
		mergedGlobals["ProjectDir"] = tp.Dir

		if opts.Project.Context != nil {
			tc := TemplateContext{
				Instructions: opts.Project.Context.Spec.Instructions,
				Path:         opts.Project.Context.GetPath(),
			}
			mergedGlobals["Context"] = tc
			mergedGlobals["ContextPath"] = tc.Path
		}
	} else {
		mergedGlobals["Project"] = TemplateProject{}
		mergedGlobals["ProjectDir"] = ""
	}

	if opts != nil && opts.Agent != nil {
		ta := TemplateAgent{
			Name:        opts.Agent.Metadata.Name,
			Description: opts.Agent.Metadata.Description,
			Dir:         opts.Agent.Directory,
			Path:        opts.Agent.GetPath(),
		}
		mergedGlobals["Agent"] = ta
	} else {
		mergedGlobals["Agent"] = TemplateAgent{}
	}

	return mergedGlobals
}

// Render processes the MCP as a template.
func (m *MCP) Render(opts *MCPRenderOptions) (string, error) {
	if m == nil {
		return "", nil
	}
	renderOpts := &RenderOptions{
		Globals: m.buildGlobals(opts),
	}
	return Render(m, renderOpts)
}

// RenderCommand renders each element in MCPSpec.Command as a template using the MCP's render context.
func (m *MCP) RenderCommand(opts *MCPRenderOptions) ([]string, error) {
	if m == nil || len(m.Spec.Command) == 0 {
		return nil, nil
	}
	renderOpts := &RenderOptions{
		Globals: m.buildGlobals(opts),
	}
	data, err := buildTemplateData(m, renderOpts)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(m.Spec.Command))
	for i, cmdPart := range m.Spec.Command {
		rendered, err := renderTemplate(fmt.Sprintf("%s-command-%d", m.GetName(), i), cmdPart, data)
		if err != nil {
			return nil, fmt.Errorf("failed to render MCP command [%d] %q: %w", i, cmdPart, err)
		}
		out[i] = rendered
	}
	return out, nil
}

// RenderEndpoint renders MCPSpec.Endpoint as a template using the MCP's render context.
func (m *MCP) RenderEndpoint(opts *MCPRenderOptions) (string, error) {
	if m == nil || m.Spec.Endpoint == "" {
		return "", nil
	}
	renderOpts := &RenderOptions{
		Globals: m.buildGlobals(opts),
	}
	data, err := buildTemplateData(m, renderOpts)
	if err != nil {
		return "", err
	}
	rendered, err := renderTemplate(fmt.Sprintf("%s-endpoint", m.GetName()), m.Spec.Endpoint, data)
	if err != nil {
		return "", fmt.Errorf("failed to render MCP endpoint %q: %w", m.Spec.Endpoint, err)
	}
	return rendered, nil
}

// ToolName returns the namespaced tool name for a raw tool exposed by this MCP server,
// in the format "<mcp_name>__<tool_name>". If rawToolName already starts with this prefix,
// it is returned unchanged.
func (m *MCP) ToolName(rawToolName string) string {
	if m == nil {
		return rawToolName
	}
	prefix := m.Metadata.Name + "__"
	if strings.HasPrefix(rawToolName, prefix) {
		return rawToolName
	}
	return prefix + rawToolName
}

// ParseMCPToolName splits a materialized tool name into its MCP server name and raw tool name.
// It splits on the first occurrence of "__". Both parts must be non-empty for ok to be true.
func ParseMCPToolName(name string) (mcpName, rawToolName string, ok bool) {
	before, after, found := strings.Cut(name, "__")
	if !found || before == "" || after == "" {
		return "", "", false
	}
	return before, after, true
}

func matchesResourceFilter(filter *ResourceFilter, item string) bool {
	if filter == nil {
		return true
	}
	matchPattern := func(patterns []string, val string) bool {
		for _, pat := range patterns {
			if pat == "*" {
				return true
			}
			if m, _ := path.Match(pat, val); m {
				return true
			}
			if m, _ := path.Match(strings.ToLower(pat), strings.ToLower(val)); m {
				return true
			}
			if strings.EqualFold(pat, val) {
				return true
			}
			// Prefix matching (e.g. "file://*")
			if prefix, ok := strings.CutSuffix(pat, "*"); ok {
				if strings.HasPrefix(val, prefix) || strings.HasPrefix(strings.ToLower(val), strings.ToLower(prefix)) {
					return true
				}
			}
			// Suffix matching (e.g. "*.key")
			if suffix, ok := strings.CutPrefix(pat, "*"); ok {
				if strings.HasSuffix(val, suffix) || strings.HasSuffix(strings.ToLower(val), strings.ToLower(suffix)) {
					return true
				}
			}
		}
		return false
	}

	if len(filter.Include) > 0 {
		if !matchPattern(filter.Include, item) {
			return false
		}
	}
	if len(filter.Exclude) > 0 {
		if matchPattern(filter.Exclude, item) {
			return false
		}
	}
	return true
}

// AllowsTool checks whether a tool name is permitted by the MCP's tool filtering policies.
func (m *MCP) AllowsTool(rawToolName string) bool {
	if m == nil || m.Spec.Policies == nil || m.Spec.Policies.Tools == nil {
		return true
	}
	if mcpName, raw, ok := ParseMCPToolName(rawToolName); ok && mcpName == m.Metadata.Name {
		rawToolName = raw
	} else if after, ok := strings.CutPrefix(rawToolName, m.Metadata.Name+"__"); ok {
		rawToolName = after
	}
	return matchesResourceFilter(m.Spec.Policies.Tools, rawToolName)
}

// AllowsPrompt checks whether a prompt name is permitted by the MCP's prompt filtering policies.
func (m *MCP) AllowsPrompt(rawPromptName string) bool {
	if m == nil || m.Spec.Policies == nil || m.Spec.Policies.Prompts == nil {
		return true
	}
	return matchesResourceFilter(m.Spec.Policies.Prompts, rawPromptName)
}

// AllowsResource checks whether a resource URI is permitted by the MCP's resource filtering policies.
func (m *MCP) AllowsResource(resourceURI string) bool {
	if m == nil || m.Spec.Policies == nil || m.Spec.Policies.Resources == nil {
		return true
	}
	return matchesResourceFilter(m.Spec.Policies.Resources, resourceURI)
}

// EffectiveAnnotation returns the effective safety annotation for a tool exposed by this MCP.
// Precedence order:
//  1. Overrides matching rawToolName or m.ToolName(rawToolName)
//  2. Server-level m.Spec.Annotations
//  3. Wire annotations provided by the remote MCP server
//  4. nil
func (m *MCP) EffectiveAnnotation(rawToolName string, wire *ToolAnnotation) *ToolAnnotation {
	if m == nil {
		if wire != nil {
			return wire.DeepCopy()
		}
		return nil
	}
	cleanName := rawToolName
	if mcpName, raw, ok := ParseMCPToolName(cleanName); ok && mcpName == m.Metadata.Name {
		cleanName = raw
	} else if after, ok := strings.CutPrefix(cleanName, m.Metadata.Name+"__"); ok {
		cleanName = after
	}
	prefixedName := m.ToolName(cleanName)

	if len(m.Spec.Overrides) > 0 {
		if ov, ok := m.Spec.Overrides[cleanName]; ok {
			return ov.DeepCopy()
		}
		if ov, ok := m.Spec.Overrides[prefixedName]; ok {
			return ov.DeepCopy()
		}
	}
	if m.Spec.Annotations != nil {
		return m.Spec.Annotations.DeepCopy()
	}
	if wire != nil {
		return wire.DeepCopy()
	}
	return nil
}

// NewTool materializes a Tool resource from an MCP tool definition.
// Returns (tool, true) if allowed by policies, or (nil, false) if excluded.
func (m *MCP) NewTool(rawName, description string, inputSchema map[string]any, wire *ToolAnnotation) (*Tool, bool) {
	if m == nil {
		return nil, false
	}
	if mcpName, raw, ok := ParseMCPToolName(rawName); ok && mcpName == m.Metadata.Name {
		rawName = raw
	} else if after, ok := strings.CutPrefix(rawName, m.Metadata.Name+"__"); ok {
		rawName = after
	}
	if !m.AllowsTool(rawName) {
		return nil, false
	}
	apiVersion := m.APIVersion
	if apiVersion == "" {
		apiVersion = APIVersion
	}
	ns := m.GetNamespace()
	if ns == "" {
		ns = NamespaceLocal
	}
	t := &Tool{
		BaseResource: BaseResource{
			APIVersion: apiVersion,
			Kind:       KindTool,
			Namespace:  ns,
			Path:       m.GetPath(),
			Directory:  m.Directory,
			Metadata: Metadata{
				Name:        m.ToolName(rawName),
				Description: description,
			},
		},
		Spec: ToolSpec{
			InputSchema: inputSchema,
			Annotations: m.EffectiveAnnotation(rawName, wire),
		},
	}
	return t, true
}
