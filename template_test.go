package warp

import (
	"testing"
)

func TestTemplateRender_Shorthand(t *testing.T) {
	cmd := &Command{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "test-cmd"},
		},
		Spec: CommandSpec{
			Instructions: "Hello $1, this is $hint1 and ${hint2}.",
			Hints:        []string{"hint1", "hint2"},
		},
	}

	opts := &RenderOptions{
		Args: []string{"Pos1", "HintVal2"},
	}

	res, err := Render(cmd, opts)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	expected := "Hello Pos1, this is Pos1 and HintVal2."
	if res != expected {
		t.Errorf("Expected %q, got %q", expected, res)
	}
}

func TestTemplateRender_Fields(t *testing.T) {
	agent := &Agent{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "my-agent", DisplayName: "My Agent"},
		},
		Spec: AgentSpec{
			Instructions: "I am $DisplayName. I know {{range .Skills}}{{.}} {{end}}",
			Skills:       []string{"go", "rust"},
		},
	}

	res, err := Render(agent, nil)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	expected := "I am My Agent. I know go rust "
	if res != expected {
		t.Errorf("Expected %q, got %q", expected, res)
	}
}

func TestTemplateRender_Globals(t *testing.T) {
	skill := &Skill{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "test"},
		},
		Spec: SkillSpec{
			Instructions: "Path is $PWD.",
		},
	}

	opts := &RenderOptions{
		Globals: map[string]any{"PWD": "/usr/src"},
	}

	res, err := Render(skill, opts)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	expected := "Path is /usr/src."
	if res != expected {
		t.Errorf("Expected %q, got %q", expected, res)
	}
}
