package warp

import (
	"reflect"
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
  description: A test tool
  command:
    - ls
    - -la
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
			case "Agent with instructions", "Agent with empty instructions":
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
