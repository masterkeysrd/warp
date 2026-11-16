package warp

import (
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"strings"
)

// ModelProvider is a warp resource that describes an LLM provider configuration.
type ModelProvider struct {
	BaseResource `yaml:",inline"`
	// Spec holds the provider-specific configuration.
	Spec ModelProviderSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the ModelProvider.
func (in *ModelProvider) DeepCopy() *ModelProvider {
	if in == nil {
		return nil
	}
	out := new(ModelProvider)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

func (in *ModelProvider) matchesRef(ref string) bool {
	return in != nil && ref != "" && (in.Metadata.Name == ref || in.QualifiedName() == ref)
}

func (in *ModelProvider) findModel(ref string) *ProviderModel {
	if in == nil || ref == "" {
		return nil
	}
	for i := range in.Spec.Models {
		if m := &in.Spec.Models[i]; m.ID == ref || (m.Name != "" && m.Name == ref) {
			return m
		}
	}
	return nil
}

func (in *ModelProvider) findModelByAlias(alias string) *ProviderModel {
	if in == nil || alias == "" {
		return nil
	}
	for i := range in.Spec.Models {
		if in.Spec.Models[i].hasAlias(alias) {
			return &in.Spec.Models[i]
		}
	}
	return nil
}

func (in *ModelProvider) defaultForRole(role ModelRole) string {
	if in == nil {
		return ""
	}
	return in.Spec.DefaultModels.ForRole(role)
}

// ModelProviderSpec contains the configuration details for a ModelProvider resource.
type ModelProviderSpec struct {
	Type          string            `yaml:"type"`                                                   // e.g., "ollama", "openai", "anthropic", "typesafe"
	Endpoint      string            `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`           // API base URL
	DefaultModels *ModelDefaults    `yaml:"defaultModels,omitempty" json:"defaultModels,omitempty"` // Role-based default models (completion, decision, embedding)
	Auth          *ProviderAuth     `yaml:"auth,omitempty" json:"auth,omitempty"`                   // Authentication configuration
	Headers       map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`             // Custom HTTP headers for all provider requests
	Config        map[string]any    `yaml:"config,omitempty" json:"config,omitempty"`               // Provider-specific settings and custom configuration parameters
	Models        []ProviderModel   `yaml:"models"`                                                 // Available models from this provider
}

// DeepCopy returns a deep copy of the ModelProviderSpec.
func (in *ModelProviderSpec) DeepCopy() *ModelProviderSpec {
	if in == nil {
		return nil
	}
	out := new(ModelProviderSpec)
	*out = *in
	if in.DefaultModels != nil {
		out.DefaultModels = in.DefaultModels.DeepCopy()
	}
	if in.Auth != nil {
		out.Auth = in.Auth.DeepCopy()
	}
	if in.Headers != nil {
		out.Headers = make(map[string]string, len(in.Headers))
		maps.Copy(out.Headers, in.Headers)
	}
	if in.Config != nil {
		out.Config = make(map[string]any, len(in.Config))
		maps.Copy(out.Config, in.Config)
	}
	if in.Models != nil {
		out.Models = make([]ProviderModel, len(in.Models))
		for i, m := range in.Models {
			out.Models[i] = *m.DeepCopy()
		}
	}
	return out
}

// Standard authentication scheme constants for ProviderAuth.Type.
const (
	AuthTypeBearer            = "bearer"
	AuthTypeAPIKey            = "api-key"
	AuthTypeBasic             = "basic"
	AuthTypeOAuth2            = "oauth2"
	AuthTypeClientCredentials = "client-credentials"
	AuthTypeEnterprise        = "enterprise"
	AuthTypeNone              = "none"
)

// ProviderAuth defines how to authenticate with a model provider or remote MCP server.
type ProviderAuth struct {
	Type     string             `yaml:"type,omitempty" json:"type,omitempty"`                                                                       // The auth scheme (e.g., "bearer", "api-key", "basic", "oauth2", "client-credentials", "enterprise", "none")
	Header   string             `yaml:"header,omitempty" json:"header,omitempty"`                                                                   // Custom header name if type is "api-key" (literal or expression)
	Token    string             `yaml:"token,omitempty" json:"token,omitempty" jsonschema:"pattern=^\\$\\{[a-z][a-z0-9+.-]*:[^}\\r\\n]+\\}$"`       // Secret expression for token-based schemes (e.g., "${env:OPENAI_API_KEY}")
	Username string             `yaml:"username,omitempty" json:"username,omitempty"`                                                               // Username for "basic" auth (literal or expression)
	Password string             `yaml:"password,omitempty" json:"password,omitempty" jsonschema:"pattern=^\\$\\{[a-z][a-z0-9+.-]*:[^}\\r\\n]+\\}$"` // Secret expression for "basic" auth password (e.g., "${env:BASIC_PASSWORD}")
	OAuth    *ProviderAuthOAuth `yaml:"oauth,omitempty" json:"oauth,omitempty"`                                                                     // OAuth configuration for "oauth2", "client-credentials", and "enterprise" schemes
}

// ProviderAuthOAuth declares the authorization servers and client credentials
// for OAuth-based schemes (oauth2, client-credentials, enterprise).
type ProviderAuthOAuth struct {
	IDPIssuer       string   `yaml:"idpIssuer,omitempty" json:"idpIssuer,omitempty"`                                                                           // Enterprise IdP issuer URL used for OIDC discovery (literal URL or expression)
	IDPClientID     string   `yaml:"idpClientId,omitempty" json:"idpClientId,omitempty"`                                                                       // Client ID registered at the enterprise IdP (literal or expression)
	IDPClientSecret string   `yaml:"idpClientSecret,omitempty" json:"idpClientSecret,omitempty" jsonschema:"pattern=^\\$\\{[a-z][a-z0-9+.-]*:[^}\\r\\n]+\\}$"` // Secret expression for the enterprise IdP client (e.g., "${file:~/.secrets/idp-secret}")
	AudienceIssuer  string   `yaml:"audienceIssuer,omitempty" json:"audienceIssuer,omitempty"`                                                                 // Target authorization server issuer used as token-exchange audience and metadata discovery
	ResourceURI     string   `yaml:"resourceURI,omitempty" json:"resourceURI,omitempty"`                                                                       // RFC 9728 resource identifier sent as the `resource` parameter
	ClientID        string   `yaml:"clientId,omitempty" json:"clientId,omitempty"`                                                                             // OAuth client ID for the target authorization server (literal or expression)
	ClientSecret    string   `yaml:"clientSecret,omitempty" json:"clientSecret,omitempty" jsonschema:"pattern=^\\$\\{[a-z][a-z0-9+.-]*:[^}\\r\\n]+\\}$"`       // Secret expression for the target authorization server client (e.g., "${env:MCP_CLIENT_SECRET}")
	Scopes          []string `yaml:"scopes,omitempty,flow" json:"scopes,omitempty"`                                                                            // Scopes to request; empty means server-discovered defaults
}

// DeepCopy returns a deep copy of the ProviderAuth.
func (pa *ProviderAuth) DeepCopy() *ProviderAuth {
	if pa == nil {
		return nil
	}
	out := new(ProviderAuth)
	*out = *pa
	if pa.OAuth != nil {
		out.OAuth = pa.OAuth.DeepCopy()
	}
	return out
}

// DeepCopy returns a deep copy of the ProviderAuthOAuth.
func (pao *ProviderAuthOAuth) DeepCopy() *ProviderAuthOAuth {
	if pao == nil {
		return nil
	}
	out := new(ProviderAuthOAuth)
	*out = *pao
	if pao.Scopes != nil {
		out.Scopes = make([]string, len(pao.Scopes))
		copy(out.Scopes, pao.Scopes)
	}
	return out
}

// Validate checks ProviderAuth configuration and expression syntax using default options.
func (pa *ProviderAuth) Validate() error {
	return pa.ValidateWithOptions(nil)
}

// ValidateWithOptions checks ProviderAuth configuration, required secret
// expressions, and scheme consistency using the provided ValidateOptions.
func (pa *ProviderAuth) ValidateWithOptions(opts *ValidateOptions) error {
	if pa == nil {
		return nil
	}
	if strings.TrimSpace(pa.Type) == "" {
		return fmt.Errorf("auth.type is required")
	}

	if err := validateOptionalExprFields(opts, []namedField{
		{"auth.header", pa.Header},
		{"auth.username", pa.Username},
	}); err != nil {
		return err
	}
	if err := validateRequiredExprFields(opts, []namedField{
		{"auth.token", pa.Token},
		{"auth.password", pa.Password},
	}); err != nil {
		return err
	}
	if pa.OAuth != nil {
		if err := pa.OAuth.validateWithOptions(opts); err != nil {
			return err
		}
	}

	return pa.validateSchemeConstraints()
}

func (pa *ProviderAuth) validateSchemeConstraints() error {
	switch pa.Type {
	case AuthTypeBearer:
		if pa.OAuth != nil || pa.Header != "" || pa.Username != "" || pa.Password != "" {
			return fmt.Errorf("auth type %q only accepts token", pa.Type)
		}
		if pa.Token == "" {
			return fmt.Errorf("auth type %q requires token", pa.Type)
		}
	case AuthTypeAPIKey:
		if pa.OAuth != nil || pa.Username != "" || pa.Password != "" {
			return fmt.Errorf("auth type %q only accepts header and token", pa.Type)
		}
		if pa.Token == "" {
			return fmt.Errorf("auth type %q requires token", pa.Type)
		}
	case AuthTypeBasic:
		if pa.OAuth != nil || pa.Header != "" {
			return fmt.Errorf("auth type %q does not accept header or oauth", pa.Type)
		}
		hasPair := pa.Username != "" || pa.Password != ""
		if pa.Token != "" && hasPair {
			return fmt.Errorf("auth type %q cannot combine token with username/password", pa.Type)
		}
		if pa.Token == "" && (pa.Username == "" || pa.Password == "") {
			return fmt.Errorf("auth type %q requires either token or both username and password", pa.Type)
		}
	case AuthTypeNone:
		if pa.OAuth != nil || pa.Header != "" || pa.Token != "" || pa.Username != "" || pa.Password != "" {
			return fmt.Errorf("auth type %q does not accept credentials or oauth configuration", pa.Type)
		}
	case AuthTypeOAuth2, AuthTypeClientCredentials:
		if pa.Header != "" || pa.Token != "" || pa.Username != "" || pa.Password != "" {
			return fmt.Errorf("auth type %q does not accept header, token, username, or password", pa.Type)
		}
	case AuthTypeEnterprise:
		if pa.Header != "" || pa.Token != "" || pa.Username != "" || pa.Password != "" {
			return fmt.Errorf("auth type %q does not accept header, token, username, or password", pa.Type)
		}
		if pa.OAuth == nil || strings.TrimSpace(pa.OAuth.IDPIssuer) == "" {
			return fmt.Errorf("auth type %q requires auth.oauth.idpIssuer", pa.Type)
		}
	}
	return nil
}

func (pao *ProviderAuthOAuth) validateWithOptions(opts *ValidateOptions) error {
	if err := validateOptionalExprFields(opts, []namedField{
		{"auth.oauth.idpIssuer", pao.IDPIssuer},
		{"auth.oauth.idpClientId", pao.IDPClientID},
		{"auth.oauth.audienceIssuer", pao.AudienceIssuer},
		{"auth.oauth.resourceURI", pao.ResourceURI},
		{"auth.oauth.clientId", pao.ClientID},
	}); err != nil {
		return err
	}
	if err := validateRequiredExprFields(opts, []namedField{
		{"auth.oauth.idpClientSecret", pao.IDPClientSecret},
		{"auth.oauth.clientSecret", pao.ClientSecret},
	}); err != nil {
		return err
	}

	if err := validateLiteralHTTPURL("auth.oauth.idpIssuer", pao.IDPIssuer); err != nil {
		return err
	}
	if err := validateLiteralHTTPURL("auth.oauth.audienceIssuer", pao.AudienceIssuer); err != nil {
		return err
	}
	if err := validateLiteralURI("auth.oauth.resourceURI", pao.ResourceURI); err != nil {
		return err
	}

	if pao.AudienceIssuer != "" && pao.IDPIssuer == "" {
		return fmt.Errorf("auth.oauth.audienceIssuer requires auth.oauth.idpIssuer")
	}
	if (pao.IDPClientID != "" || pao.IDPClientSecret != "") && pao.IDPIssuer == "" {
		return fmt.Errorf("auth.oauth.idpClientId and auth.oauth.idpClientSecret require auth.oauth.idpIssuer")
	}
	if pao.IDPClientSecret != "" && pao.IDPClientID == "" {
		return fmt.Errorf("auth.oauth.idpClientSecret requires auth.oauth.idpClientId")
	}
	if pao.ClientSecret != "" && pao.ClientID == "" {
		return fmt.Errorf("auth.oauth.clientSecret requires auth.oauth.clientId")
	}
	return nil
}

type namedField struct {
	name string
	val  string
}

func validateRequiredExprFields(opts *ValidateOptions, fields []namedField) error {
	for _, f := range fields {
		if f.val == "" {
			continue
		}
		if err := ValidateExpression(f.val, opts); err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
	}
	return nil
}

func validateOptionalExprFields(opts *ValidateOptions, fields []namedField) error {
	for _, f := range fields {
		if f.val == "" {
			continue
		}
		if err := ValidateStringWithExpressions(f.val, opts); err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
	}
	return nil
}

func validateLiteralHTTPURL(fieldName, val string) error {
	if val == "" || strings.Contains(val, "${") {
		return nil
	}
	u, err := url.Parse(val)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%s must be a valid http(s) URL or expression (got %q)", fieldName, val)
	}
	return nil
}

func validateLiteralURI(fieldName, val string) error {
	if val == "" || strings.Contains(val, "${") {
		return nil
	}
	u, err := url.Parse(val)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("%s must be a valid URI or expression (got %q)", fieldName, val)
	}
	return nil
}

// ProviderModel describes a specific model available from a provider.
type ProviderModel struct {
	ID           string                     `yaml:"id"`                                              // The canonical model ID—the exact identifier sent to the provider API (e.g., "claude-3-7-sonnet-20250219")
	Name         string                     `yaml:"name"`                                            // The resource/short name within WARP (e.g., "claude-3-7-sonnet")
	Label        string                     `yaml:"label"`                                           // The human-readable display value for UIs (e.g., "Claude 3.7 Sonnet")
	Aliases      []string                   `yaml:"aliases,omitempty,flow" json:"aliases,omitempty"` // Model aliases for @ references
	Limits       ProviderModelLimits        `yaml:"limits"`                                          // Context, output token, and vector dimension limits
	Capabilities *ProviderModelCapabilities `yaml:"capabilities,omitempty"`                          // Supported features and configuration boundaries
	Cost         *ProviderModelCost         `yaml:"cost,omitempty"`                                  // Token pricing and conditional tiers
}

func (in *ProviderModel) DeepCopy() *ProviderModel {
	if in == nil {
		return nil
	}
	out := new(ProviderModel)
	*out = *in
	if in.Aliases != nil {
		out.Aliases = make([]string, len(in.Aliases))
		copy(out.Aliases, in.Aliases)
	}
	if in.Capabilities != nil {
		out.Capabilities = in.Capabilities.DeepCopy()
	}
	if in.Cost != nil {
		out.Cost = in.Cost.DeepCopy()
	}
	return out
}

// ModelAliasInherit is the reserved alias representing the delegating
// parent/caller session's active model during subagent invocation.
const ModelAliasInherit = "@inherit"

func normalizeAliasName(s string) string {
	return strings.TrimPrefix(s, "@")
}

func isInheritAlias(ref string) bool {
	return strings.EqualFold(strings.TrimSpace(ref), ModelAliasInherit)
}

func isReservedInheritAliasName(name string) bool {
	return strings.EqualFold(normalizeAliasName(strings.TrimSpace(name)), "inherit")
}

func stripModelNegation(pat string) (pattern string, negated bool) {
	if after, ok := strings.CutPrefix(pat, "!"); ok {
		return after, true
	}
	return pat, false
}

func isModelGlobEntry(ref string) bool {
	if ref == "*" {
		return true
	}
	if _, negated := stripModelNegation(ref); negated {
		return true
	}
	return hasGlobMeta(ref)
}

// compileModelGlob compiles a model glob pattern (without leading '!') into a
// regular expression. Unlike file path globs, '*' matches across '/' so
// patterns like "ollama/*" match provider-qualified model IDs containing slashes.
func compileModelGlob(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, fmt.Errorf("empty model pattern")
	}
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteByte('.')
		case '[':
			end := i + 1
			if end < len(pattern) && pattern[end] == '!' {
				end++
			}
			if end < len(pattern) && pattern[end] == ']' {
				end++
			}
			for end < len(pattern) && pattern[end] != ']' {
				end++
			}
			if end >= len(pattern) {
				return nil, fmt.Errorf("unclosed character class in model glob %q", pattern)
			}
			cls := pattern[i+1 : end]
			sb.WriteByte('[')
			if strings.HasPrefix(cls, "!") {
				sb.WriteByte('^')
				cls = cls[1:]
			}
			sb.WriteString(cls)
			sb.WriteByte(']')
			i = end
		case '.', '+', '(', ')', '|', '{', '}', '^', '$', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

func matchModelGlob(pattern, qualifiedID, modelID, rawRef string) bool {
	rx, err := compileModelGlob(pattern)
	if err != nil {
		return false
	}
	if qualifiedID != "" && rx.MatchString(qualifiedID) {
		return true
	}
	if !strings.Contains(pattern, "/") && modelID != "" && rx.MatchString(modelID) {
		return true
	}
	if rawRef != "" && rx.MatchString(rawRef) {
		return true
	}
	return false
}

func isAliasRef(ref string) (providerPrefix string, aliasName string, ok bool) {
	if after, ok := strings.CutPrefix(ref, "@"); ok {
		name := after
		if name != "" {
			return "", name, true
		}
		return "", "", false
	}
	if before, after, found := strings.Cut(ref, "/"); found && strings.HasPrefix(after, "@") {
		name := strings.TrimPrefix(after, "@")
		if before != "" && name != "" {
			return before, name, true
		}
	}
	return "", "", false
}

func (in *ProviderModel) hasAlias(alias string) bool {
	if in == nil || alias == "" {
		return false
	}
	norm := normalizeAliasName(alias)
	for _, a := range in.Aliases {
		if normalizeAliasName(a) == norm {
			return true
		}
	}
	return false
}

// ProviderModelLimits defines the token and vector dimension limits for a specific model.
type ProviderModelLimits struct {
	Context    int `yaml:"context"`              // Max context length in tokens
	Output     int `yaml:"output,omitempty"`     // Max output length in tokens
	Dimensions int `yaml:"dimensions,omitempty"` // Default/max output vector dimensions (for embedding models)
}

type ProviderModelCapabilities struct {
	Completion       *bool                             `yaml:"completion,omitempty"`       // Whether the model supports text/chat completion (defaults to true)
	Decision         *bool                             `yaml:"decision,omitempty"`         // Whether the model supports probabilistic decision evaluation (defaults to false)
	Embedding        *bool                             `yaml:"embedding,omitempty"`        // Whether the model supports vector embedding generation (defaults to false)
	Dimensions       []int                             `yaml:"dimensions,omitempty,flow"`  // Supported output vector dimensions for configurable embedding models
	Attachment       *bool                             `yaml:"attachment,omitempty"`       // Whether the model supports file/document attachments
	Tools            *bool                             `yaml:"tools,omitempty"`            // Whether the model supports function/tool calling (tool_call)
	StructuredOutput *bool                             `yaml:"structuredOutput,omitempty"` // Whether the model supports strict structured outputs
	Temperature      *bool                             `yaml:"temperature,omitempty"`      // Whether the model allows configuring the sampling temperature
	Thinking         []ProviderModelThinkingCapability `yaml:"thinking,omitempty"`         // List of supported reasoning configuration strategies
	Modalities       *ProviderModelModalities          `yaml:"modalities,omitempty"`       // Defines supported input and output data modalities
}

// SupportsCompletion reports whether the model supports text/chat completions.
// Defaults to true when Capabilities or Completion is nil.
func (in *ProviderModelCapabilities) SupportsCompletion() bool {
	if in == nil || in.Completion == nil {
		return true
	}
	return *in.Completion
}

// SupportsDecision reports whether the model supports probabilistic decision evaluation.
// Defaults to false when Capabilities or Decision is nil.
func (in *ProviderModelCapabilities) SupportsDecision() bool {
	if in == nil || in.Decision == nil {
		return false
	}
	return *in.Decision
}

// SupportsEmbedding reports whether the model supports vector embedding generation.
// Defaults to false when Capabilities or Embedding is nil.
func (in *ProviderModelCapabilities) SupportsEmbedding() bool {
	if in == nil || in.Embedding == nil {
		return false
	}
	return *in.Embedding
}

func (in *ProviderModelCapabilities) supportsRole(role ModelRole) bool {
	switch role {
	case ModelRoleCompletion:
		return in.SupportsCompletion()
	case ModelRoleDecision:
		return in.SupportsDecision()
	case ModelRoleEmbedding:
		return in.SupportsEmbedding()
	default:
		return false
	}
}

func (in *ProviderModelCapabilities) DeepCopy() *ProviderModelCapabilities {
	if in == nil {
		return nil
	}
	out := new(ProviderModelCapabilities)
	*out = *in
	if in.Completion != nil {
		b := *in.Completion
		out.Completion = &b
	}
	if in.Decision != nil {
		b := *in.Decision
		out.Decision = &b
	}
	if in.Embedding != nil {
		b := *in.Embedding
		out.Embedding = &b
	}
	if in.Dimensions != nil {
		out.Dimensions = make([]int, len(in.Dimensions))
		copy(out.Dimensions, in.Dimensions)
	}
	if in.Attachment != nil {
		b := *in.Attachment
		out.Attachment = &b
	}
	if in.Tools != nil {
		b := *in.Tools
		out.Tools = &b
	}
	if in.StructuredOutput != nil {
		b := *in.StructuredOutput
		out.StructuredOutput = &b
	}
	if in.Temperature != nil {
		b := *in.Temperature
		out.Temperature = &b
	}
	if in.Thinking != nil {
		out.Thinking = make([]ProviderModelThinkingCapability, len(in.Thinking))
		for i, t := range in.Thinking {
			out.Thinking[i] = *t.DeepCopy()
		}
	}
	if in.Modalities != nil {
		out.Modalities = in.Modalities.DeepCopy()
	}
	return out
}

type ProviderModelThinkingCapability struct {
	Type            ThinkingStrategy `yaml:"type"`                      // The strategy this model supports: toggle, effort, budget, or adaptive
	IsDefault       bool             `yaml:"isDefault,omitempty"`       // Marks this strategy as the primary default
	AllowedEfforts  []string         `yaml:"allowedEfforts,omitempty"`  // Used if type: effort. e.g., ["low", "medium", "high"]
	MaxBudgetTokens int              `yaml:"maxBudgetTokens,omitempty"` // Used if type: budget. The maximum allowed thinking tokens
	Default         any              `yaml:"default,omitempty"`         // The default value
}

func (in *ProviderModelThinkingCapability) DeepCopy() *ProviderModelThinkingCapability {
	if in == nil {
		return nil
	}
	out := new(ProviderModelThinkingCapability)
	*out = *in
	if in.AllowedEfforts != nil {
		out.AllowedEfforts = make([]string, len(in.AllowedEfforts))
		copy(out.AllowedEfforts, in.AllowedEfforts)
	}
	return out
}

type ProviderModelModalities struct {
	Input  []string `yaml:"input,omitempty"`  // Supported input modalities (e.g., ["text", "image", "video"])
	Output []string `yaml:"output,omitempty"` // Supported output modalities (e.g., ["text", "image"])
}

func (in *ProviderModelModalities) DeepCopy() *ProviderModelModalities {
	if in == nil {
		return nil
	}
	out := new(ProviderModelModalities)
	*out = *in
	if in.Input != nil {
		out.Input = make([]string, len(in.Input))
		copy(out.Input, in.Input)
	}
	if in.Output != nil {
		out.Output = make([]string, len(in.Output))
		copy(out.Output, in.Output)
	}
	return out
}

type ProviderModelCost struct {
	Input      float64                 `yaml:"input"`                // Base cost for input tokens (typically per 1M tokens)
	Output     float64                 `yaml:"output,omitempty"`     // Base cost for output tokens
	CacheRead  float64                 `yaml:"cacheRead,omitempty"`  // Cost for reading from prompt cache
	CacheWrite float64                 `yaml:"cacheWrite,omitempty"` // Cost for writing to prompt cache
	Tiers      []ProviderModelCostTier `yaml:"tiers,omitempty"`      // Array of conditional pricing tiers
}

func (in *ProviderModelCost) DeepCopy() *ProviderModelCost {
	if in == nil {
		return nil
	}
	out := new(ProviderModelCost)
	*out = *in
	if in.Tiers != nil {
		out.Tiers = make([]ProviderModelCostTier, len(in.Tiers))
		for i, t := range in.Tiers {
			out.Tiers[i] = *t.DeepCopy()
		}
	}
	return out
}

type ProviderModelCostTier struct {
	Label      string                         `yaml:"label"`                // A human-readable label for the tier (e.g., "Context over 200k")
	Tier       ProviderModelCostTierCondition `yaml:"tier"`                 // The condition that triggers this pricing
	Input      float64                        `yaml:"input"`                // Override cost for input tokens
	Output     float64                        `yaml:"output,omitempty"`     // Override cost for output tokens
	CacheRead  float64                        `yaml:"cacheRead,omitempty"`  // Override cost for cache read
	CacheWrite float64                        `yaml:"cacheWrite,omitempty"` // Override cost for cache write
}

func (in *ProviderModelCostTier) DeepCopy() *ProviderModelCostTier {
	if in == nil {
		return nil
	}
	out := new(ProviderModelCostTier)
	*out = *in
	return out
}

type ProviderModelCostTierCondition struct {
	Type string `yaml:"type"` // e.g., "context"
	Size int    `yaml:"size"` // e.g., 200000
}
