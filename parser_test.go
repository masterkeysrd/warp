package warp

import (
	"reflect"
	"strings"
	"testing"
)

func TestFormatRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "Agent with instructions",
			content: `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: test-agent
spec:
  models:
    - gpt-4
---
Helpful assistant.
`,
		},
		{
			name: "Tool without instructions",
			content: `apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: test-tool
spec:
  command:
    - ls
    - -la
`,
		},
		{
			name: "Tool with instructions",
			content: `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: script-tool
spec:
  command:
    - python
---
Run this python script.
`,
		},
		{
			name: "Agent with empty instructions",
			content: `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: empty-agent
spec:
  instructions: ""
  models:
    - gpt-4
  temperature: 0
---
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := "test" + RecommendedExtension(Kind(tt.name)) // Simple hack for test mapping
			switch tt.name {
			case "Agent with instructions", "Agent with empty instructions", "Tool with instructions":
				filePath = "test.md"
			case "Tool without instructions":
				filePath = "test.yaml"
			}

			res, err := Parse(filePath, tt.content)
			if err != nil {
				t.Fatalf("Initial Parse() failed: %v", err)
			}

			formatted, err := Format(res.Resource.(Resource))
			if err != nil {
				t.Fatalf("Format() failed: %v", err)
			}
			res2, err := Parse(filePath, string(formatted))
			if err != nil {
				t.Fatalf("Second Parse() failed: %v", err)
			}

			// Compare resources
			if !reflect.DeepEqual(res.Resource, res2.Resource) {
				t.Errorf("Round-trip failed: resources are not equal")
			}
		})
	}
}

func TestMCPPoliciesRoundTrip(t *testing.T) {
	content := `apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: test-mcp
spec:
  command: ["npx", "@modelcontextprotocol/server-everything"]
  policies:
    tools:
      include: ["echo", "add"]
    prompts:
      exclude: ["secret-*"]
    resources:
      include: ["file://*"]
`
	res, err := Parse("test-mcp.yaml", content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	mcp, ok := res.Resource.(*MCP)
	if !ok {
		t.Fatalf("Expected *MCP, got %T", res.Resource)
	}

	if mcp.Spec.Policies == nil {
		t.Fatal("Policies should not be nil")
	}
	if len(mcp.Spec.Policies.Tools.Include) != 2 {
		t.Errorf("Expected 2 included tools, got %d", len(mcp.Spec.Policies.Tools.Include))
	}

	formatted, err := Format(mcp)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}

	res2, err := Parse("test-mcp.yaml", string(formatted))
	if err != nil {
		t.Fatalf("Second Parse failed: %v", err)
	}

	if !reflect.DeepEqual(res.Resource, res2.Resource) {
		t.Errorf("Round-trip failed: MCP resources are not equal")
	}
}

func TestMCPSseRoundTrip(t *testing.T) {
	content := `apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: sse-mcp
spec:
  type: sse
  endpoint: "https://example.com/mcp/sse"
  policies:
    tools:
      include: ["*"]
`
	res, err := Parse("sse-mcp.yaml", content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	mcp, ok := res.Resource.(*MCP)
	if !ok {
		t.Fatalf("Expected *MCP, got %T", res.Resource)
	}

	if mcp.Spec.Type != "sse" {
		t.Errorf("Expected Type to be 'sse', got %q", mcp.Spec.Type)
	}
	if mcp.Spec.Endpoint != "https://example.com/mcp/sse" {
		t.Errorf("Expected Endpoint to be 'https://example.com/mcp/sse', got %q", mcp.Spec.Endpoint)
	}

	formatted, err := Format(mcp)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}

	res2, err := Parse("sse-mcp.yaml", string(formatted))
	if err != nil {
		t.Fatalf("Second Parse failed: %v", err)
	}

	if !reflect.DeepEqual(res.Resource, res2.Resource) {
		t.Errorf("Round-trip failed: MCP resources are not equal")
	}
}

func TestModelProviderDecisionCapabilitiesRoundTrip(t *testing.T) {
	content := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: test-provider
spec:
  type: ollama
  endpoint: http://localhost:11434
  defaultModel: llama3.3
  models:
    - id: llama3.3
      name: llama3.3
      label: Llama 3.3
      limits:
        context: 128000
        output: 4096
    - id: nimble
      name: nimble
      label: Nimble
      limits:
        context: 64000
      capabilities:
        completion: false
        decision: true
`
	res, err := Parse("test-provider.yaml", content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	mp, ok := res.Resource.(*ModelProvider)
	if !ok {
		t.Fatalf("Expected *ModelProvider, got %T", res.Resource)
	}

	if len(mp.Spec.Models) != 2 {
		t.Fatalf("Expected 2 models, got %d", len(mp.Spec.Models))
	}

	// Default model (capabilities omitted): completion defaults to true, decision defaults to false
	m0 := mp.Spec.Models[0]
	if !m0.Capabilities.SupportsCompletion() {
		t.Errorf("Expected default model SupportsCompletion() to be true")
	}
	if m0.Capabilities.SupportsDecision() {
		t.Errorf("Expected default model SupportsDecision() to be false")
	}

	// Pure decision model: completion is false, decision is true, output limit is 0 (omitted)
	m1 := mp.Spec.Models[1]
	if m1.Capabilities.SupportsCompletion() {
		t.Errorf("Expected decision model SupportsCompletion() to be false")
	}
	if !m1.Capabilities.SupportsDecision() {
		t.Errorf("Expected decision model SupportsDecision() to be true")
	}
	if m1.Limits.Output != 0 {
		t.Errorf("Expected decision model Limits.Output to be 0, got %d", m1.Limits.Output)
	}

	formatted, err := Format(mp)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}

	res2, err := Parse("test-provider.yaml", string(formatted))
	if err != nil {
		t.Fatalf("Second Parse failed: %v", err)
	}

	if !reflect.DeepEqual(res.Resource, res2.Resource) {
		t.Errorf("Round-trip failed: ModelProvider resources are not equal")
	}
}

func TestDecisionRoundTrip(t *testing.T) {
	content := `---
apiVersion: warp/v1alpha1
kind: Decision
metadata:
  name: pr-triage
  description: Triage pull requests
spec:
  models: ["typesafe/jev-latest", "ollama/nimble"]
  arguments:
    - name: repo
  questions:
    - name: is_security_sensitive
      type: predicate
      instructions: Does this diff touch auth or crypto?
      threshold: 0.7
      criteria:
        true: Modifies auth, RBAC, or crypto.
        false: Only touches UI or docs.
    - name: reviewer_team
      type: choice
      instructions: Which team should review?
      minConfidence: 0.6
      options:
        - value: security
          description: Security team
        - value: backend
          description: Backend team
    - name: urgency
      type: score
      instructions: How urgent is this PR?
      levels:
        - label: low
          description: Minor chore
        - label: high
          description: Critical hotfix
---

# Pull Request Triage

Evaluate the diff for repository $repo.`

	res, err := Parse("pr-triage.md", content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	dec, ok := res.Resource.(*Decision)
	if !ok {
		t.Fatalf("Expected *Decision, got %T", res.Resource)
	}

	if RecommendedExtension(dec.GetKind()) != ".md" {
		t.Errorf("Expected RecommendedExtension(KindDecision) == .md, got %s", RecommendedExtension(dec.GetKind()))
	}
	if len(dec.Spec.Questions) != 3 {
		t.Fatalf("Expected 3 questions, got %d", len(dec.Spec.Questions))
	}
	if dec.Spec.Questions[0].Type != DecisionQuestionPredicate {
		t.Errorf("Expected predicate, got %s", dec.Spec.Questions[0].Type)
	}
	if dec.Spec.Questions[1].Type != DecisionQuestionChoice || len(dec.Spec.Questions[1].Options) != 2 {
		t.Errorf("Expected choice with 2 options, got %+v", dec.Spec.Questions[1])
	}
	if dec.Spec.Questions[2].Type != DecisionQuestionScore || len(dec.Spec.Questions[2].Levels) != 2 {
		t.Errorf("Expected score with 2 levels, got %+v", dec.Spec.Questions[2])
	}

	formatted, err := Format(dec)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}

	res2, err := Parse("pr-triage.md", string(formatted))
	if err != nil {
		t.Fatalf("Second Parse failed: %v", err)
	}

	if !reflect.DeepEqual(res.Resource, res2.Resource) {
		t.Errorf("Round-trip failed: Decision resources are not equal")
	}
}

func TestEmbeddingAndDefaultModelsRoundTrip(t *testing.T) {
	providerContent := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: openai
spec:
  type: openai
  endpoint: https://api.openai.com/v1
  defaultModels:
    completion: gpt-4o
    decision: gpt-6-luna
    embedding: text-embedding-3-small
  models:
    - id: text-embedding-3-small
      name: text-embedding-3-small
      label: Text Embedding 3 Small
      limits:
        context: 8191
        dimensions: 1536
      capabilities:
        completion: false
        embedding: true
        dimensions: [256, 512, 1536]
      cost:
        input: 0.02
`
	res, err := Parse("openai.yaml", providerContent)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	mp, ok := res.Resource.(*ModelProvider)
	if !ok {
		t.Fatalf("Expected *ModelProvider, got %T", res.Resource)
	}
	if mp.Spec.DefaultModels == nil || mp.Spec.DefaultModels.Embedding != "text-embedding-3-small" {
		t.Fatalf("Expected defaultModels.embedding to be text-embedding-3-small, got %+v", mp.Spec.DefaultModels)
	}
	m := mp.Spec.Models[0]
	if m.Capabilities.SupportsCompletion() {
		t.Errorf("Expected embedding model SupportsCompletion() == false")
	}
	if !m.Capabilities.SupportsEmbedding() {
		t.Errorf("Expected embedding model SupportsEmbedding() == true")
	}
	if m.Limits.Dimensions != 1536 {
		t.Errorf("Expected Limits.Dimensions == 1536, got %d", m.Limits.Dimensions)
	}
	if len(m.Capabilities.Dimensions) != 3 {
		t.Errorf("Expected 3 configurable dimensions, got %v", m.Capabilities.Dimensions)
	}

	formatted, err := Format(mp)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	res2, err := Parse("openai.yaml", string(formatted))
	if err != nil {
		t.Fatalf("Second Parse failed: %v", err)
	}
	if !reflect.DeepEqual(res.Resource, res2.Resource) {
		t.Errorf("Round-trip failed: ModelProvider resources are not equal")
	}
}

func TestOmitemptyFormatting(t *testing.T) {
	// 1. Agent without temperature should omit temperature when formatted
	ag := &Agent{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindAgent,
			Metadata:   Metadata{Name: "no-temp"},
		},
		Spec: AgentSpec{
			Models: []string{"gpt-4o"},
		},
	}
	agBytes, err := Format(ag)
	if err != nil {
		t.Fatalf("Format(Agent) failed: %v", err)
	}
	if strings.Contains(string(agBytes), "temperature:") {
		t.Errorf("expected omitted temperature in formatted Agent, got:\n%s", string(agBytes))
	}

	// 2. Workspace with only DefaultModel should omit empty Projects, DefaultProvider, DefaultAgent, Plugins
	ws := &WorkspaceDef{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindWorkspace,
			Metadata:   Metadata{Name: "clean-ws"},
		},
		Spec: WorkspaceDefSpec{
			DefaultModel: "claude-3-7-sonnet",
		},
	}
	wsBytes, err := Format(ws)
	if err != nil {
		t.Fatalf("Format(WorkspaceDef) failed: %v", err)
	}
	wsOut := string(wsBytes)
	for _, unwanted := range []string{"projects:", "defaultProvider:", "defaultAgent:", "plugins:"} {
		if strings.Contains(wsOut, unwanted) {
			t.Errorf("expected %q to be omitted in formatted WorkspaceDef, got:\n%s", unwanted, wsOut)
		}
	}
	if !strings.Contains(wsOut, "defaultModel: claude-3-7-sonnet") {
		t.Errorf("expected defaultModel to be serialized in formatted WorkspaceDef, got:\n%s", wsOut)
	}

	// 3. ToolAnnotation with only IsReadOnly=true should omit false booleans and empty userHint
	tool := &Tool{
		BaseResource: BaseResource{
			APIVersion: APIVersion,
			Kind:       KindTool,
			Metadata:   Metadata{Name: "read-tool"},
		},
		Spec: ToolSpec{
			Command: []string{"cat"},
			Annotations: &ToolAnnotation{
				IsReadOnly: true,
			},
		},
	}
	toolBytes, err := Format(tool)
	if err != nil {
		t.Fatalf("Format(Tool) failed: %v", err)
	}
	toolOut := string(toolBytes)
	for _, unwanted := range []string{"isOpenWorld:", "isDangerous:", "isIdempotent:", "userHint:"} {
		if strings.Contains(toolOut, unwanted) {
			t.Errorf("expected %q to be omitted in formatted Tool, got:\n%s", unwanted, toolOut)
		}
	}
	if !strings.Contains(toolOut, "isReadOnly: true") {
		t.Errorf("expected isReadOnly: true in formatted Tool, got:\n%s", toolOut)
	}
}

func TestMCPStreamableHttpWithAuthAndHeadersRoundTrip(t *testing.T) {
	content := `apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: remote-mcp
spec:
  type: streamable-http
  endpoint: https://mcp.example.com/v1
  auth:
    type: bearer
    env: MCP_API_TOKEN
  headers:
    X-Custom-Tenant: acme
`
	res, err := Parse("remote-mcp.yaml", content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	mcp, ok := res.Resource.(*MCP)
	if !ok {
		t.Fatalf("Expected *MCP, got %T", res.Resource)
	}
	if mcp.Spec.Type != "streamable-http" {
		t.Errorf("expected type streamable-http, got %q", mcp.Spec.Type)
	}
	if mcp.Spec.Auth == nil || mcp.Spec.Auth.Type != "bearer" || mcp.Spec.Auth.Env != "MCP_API_TOKEN" {
		t.Errorf("unexpected auth: %+v", mcp.Spec.Auth)
	}
	if mcp.Spec.Headers["X-Custom-Tenant"] != "acme" {
		t.Errorf("unexpected headers: %v", mcp.Spec.Headers)
	}

	formatted, err := Format(mcp)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	res2, err := Parse("remote-mcp.yaml", string(formatted))
	if err != nil {
		t.Fatalf("Second Parse failed: %v", err)
	}
	if !reflect.DeepEqual(res.Resource, res2.Resource) {
		t.Errorf("Round-trip failed: MCP resources are not equal")
	}
}

func TestParseRule(t *testing.T) {
	content := `---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: go-error-wrapping
  description: Enforce fmt.Errorf wrapping in Go
spec:
  priority: 100
  triggers:
    always: false
    globs:
      - "**/*.go"
      - "!**/vendor/**"
    useWhen: "When writing Go error handling code"
    keywords: ["error", "wrap", "fmt"]
    decision: "eval-error-context"
    tools: ["bash", "file-editor"]
---
# Go Error Wrapping

Always wrap errors using fmt.Errorf with %w.
`
	res, err := Parse("go-error-wrapping.md", content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	rule, ok := res.Resource.(*Rule)
	if !ok {
		t.Fatalf("Expected *Rule, got %T", res.Resource)
	}

	if RecommendedExtension(rule.GetKind()) != ".md" {
		t.Errorf("Expected RecommendedExtension(KindRule) == .md, got %s", RecommendedExtension(rule.GetKind()))
	}
	if rule.Spec.Priority != 100 {
		t.Errorf("Expected priority 100, got %d", rule.Spec.Priority)
	}
	if rule.Spec.Triggers.Always {
		t.Errorf("Expected Always == false")
	}
	if len(rule.Spec.Triggers.Globs) != 2 {
		t.Errorf("Expected 2 globs, got %d", len(rule.Spec.Triggers.Globs))
	}
	if rule.Spec.Triggers.UseWhen != "When writing Go error handling code" {
		t.Errorf("Unexpected useWhen: %s", rule.Spec.Triggers.UseWhen)
	}
	if rule.Spec.Triggers.Decision != "eval-error-context" {
		t.Errorf("Unexpected decision: %s", rule.Spec.Triggers.Decision)
	}
	if len(rule.Spec.Triggers.Tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(rule.Spec.Triggers.Tools))
	}
	if !strings.Contains(rule.Spec.Instructions, "Always wrap errors using fmt.Errorf with %w.") {
		t.Errorf("Unexpected instructions: %q", rule.Spec.Instructions)
	}

	// Test matches
	if !rule.Spec.Triggers.MatchesPath("pkg/api/server.go") {
		t.Errorf("Expected MatchesPath to match pkg/api/server.go")
	}
	if rule.Spec.Triggers.MatchesPath("vendor/github.com/foo/bar.go") {
		t.Errorf("Expected MatchesPath to exclude vendor path")
	}
	if rule.Spec.Triggers.MatchesPath("pkg/api/server.js") {
		t.Errorf("Expected MatchesPath not to match .js file")
	}
	if !rule.Spec.Triggers.MatchesTool("bash") {
		t.Errorf("Expected MatchesTool to match bash")
	}

	formatted, err := Format(rule)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	res2, err := Parse("go-error-wrapping.md", string(formatted))
	if err != nil {
		t.Fatalf("Second Parse failed: %v", err)
	}
	rule2, ok := res2.Resource.(*Rule)
	if !ok {
		t.Fatalf("Expected *Rule, got %T", res2.Resource)
	}
	if rule2.Spec.Priority != rule.Spec.Priority {
		t.Errorf("Priority mismatch after format: %d vs %d", rule2.Spec.Priority, rule.Spec.Priority)
	}
	if strings.TrimSpace(rule2.Spec.Instructions) != strings.TrimSpace(rule.Spec.Instructions) {
		t.Errorf("Instructions mismatch after format: %q vs %q", rule2.Spec.Instructions, rule.Spec.Instructions)
	}
}

func TestParseSkillWithArguments(t *testing.T) {
	content := `---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: code-review
  description: Review code files
spec:
  useWhen: Use when reviewing a file
  argumentHint: Provide the file path to review
  arguments:
    - name: file
      description: Path to the file to review
      required: true
    - name: focus
      description: What to focus on
      required: false
      default: all
---
Review file $file focusing on $focus.
`
	res, err := Parse("code-review.md", content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	skill, ok := res.Resource.(*Skill)
	if !ok {
		t.Fatalf("Expected *Skill, got %T", res.Resource)
	}

	if skill.Spec.ArgumentHint != "Provide the file path to review" {
		t.Errorf("Unexpected argumentHint: %s", skill.Spec.ArgumentHint)
	}
	if len(skill.Spec.Arguments) != 2 {
		t.Fatalf("Expected 2 arguments, got %d", len(skill.Spec.Arguments))
	}
	if skill.Spec.Arguments[0].Name != "file" || !skill.Spec.Arguments[0].Required {
		t.Errorf("Unexpected first argument: %+v", skill.Spec.Arguments[0])
	}
	if skill.Spec.Arguments[1].Name != "focus" || skill.Spec.Arguments[1].Default != "all" {
		t.Errorf("Unexpected second argument: %+v", skill.Spec.Arguments[1])
	}
}
