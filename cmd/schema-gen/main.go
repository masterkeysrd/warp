package main

import (
	"encoding/json"
	"maps"
	"os"

	"github.com/invopop/jsonschema"
	"github.com/masterkeysrd/warp"
)

func main() {
	r := new(jsonschema.Reflector)

	err := r.AddGoComments("github.com/masterkeysrd/warp", ".")
	if err != nil {
		panic(err)
	}
	r.FieldNameTag = "yaml"

	schema := &jsonschema.Schema{
		Version:     "http://json-schema.org/draft-07/schema#",
		ID:          "https://github.com/masterkeysrd/warp/blob/main/schema/warp.json",
		Title:       "Workspace Agent Resource Protocol (WARP)",
		Description: "Schema for WARP resources (Agents, Skills, Commands, etc.)",
		Type:        "object",
		Required:    []string{"apiVersion", "kind", "metadata"},
		Properties:  jsonschema.NewProperties(),
		Definitions: jsonschema.Definitions{},
	}

	schema.Properties.Set("apiVersion", &jsonschema.Schema{
		Type:        "string",
		Enum:        []any{"warp/v1alpha1"},
		Description: "The version of the WARP specification.",
	})

	schema.Properties.Set("kind", &jsonschema.Schema{
		Type: "string",
		Enum: []any{
			"Workspace", "Context", "Agent", "Skill", "Command", "ModelProvider", "Tool", "MCP", "Toolkit", "Plugin",
		},
		Description: "The type of WARP resource.",
	})

	// Manually reflect and store definitions so we don't rely on a dummy struct.
	// We will just reflect the types and add them to schema.Definitions.

	metaSchema := r.Reflect(&warp.Metadata{})
	maps.Copy(schema.Definitions, metaSchema.Definitions)
	schema.Properties.Set("metadata", metaSchema)

	var allOf []*jsonschema.Schema

	addKind := func(kind string, specType any) {
		specSchema := r.Reflect(specType)
		maps.Copy(schema.Definitions, specSchema.Definitions)

		schemaIf := &jsonschema.Schema{
			Properties: jsonschema.NewProperties(),
		}
		schemaIf.Properties.Set("kind", &jsonschema.Schema{Const: kind})

		schemaThen := &jsonschema.Schema{
			Properties: jsonschema.NewProperties(),
		}
		schemaThen.Properties.Set("spec", specSchema)

		allOf = append(allOf, &jsonschema.Schema{
			If:   schemaIf,
			Then: schemaThen,
		})
	}

	addKind("Workspace", &warp.WorkspaceDefSpec{})
	addKind("Agent", &warp.AgentSpec{})
	addKind("Command", &warp.CommandSpec{})
	addKind("Skill", &warp.SkillSpec{})
	addKind("ModelProvider", &warp.ModelProviderSpec{})
	addKind("Tool", &warp.ToolSpec{})
	addKind("MCP", &warp.MCPSpec{})
	addKind("Toolkit", &warp.ToolkitSpec{})
	addKind("Plugin", &warp.PluginSpec{})

	schema.AllOf = allOf

	b, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		panic(err)
	}

	err = os.WriteFile("schema/warp.json", b, 0644)
	if err != nil {
		panic(err)
	}
}
