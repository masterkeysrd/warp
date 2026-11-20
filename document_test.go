package warp

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

const docSample = `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: demo
# keep this parent until Q3
spec:
  extends: old-parent # pinned
  customField: 42
---

Body text.
`

func TestDocumentUnchangedIsVerbatim(t *testing.T) {
	d, err := ParseDocument("demo.md", docSample)
	if err != nil {
		t.Fatal(err)
	}
	out, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != docSample {
		t.Fatalf("expected verbatim output, got:\n%s", out)
	}
}

func TestDocumentEditPreservesEverythingElse(t *testing.T) {
	d, err := ParseDocument("demo.md", docSample)
	if err != nil {
		t.Fatal(err)
	}
	a := d.Resource.(*Agent)
	a.Spec.Extends = "new-parent"
	out, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"# keep this parent until Q3", "customField: 42", "extends: new-parent", "# pinned", "\n\nBody text.\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "old-parent") {
		t.Errorf("old value still present:\n%s", s)
	}
	// Re-parse and idempotency.
	d2, err := ParseDocument("demo.md", s)
	if err != nil {
		t.Fatal(err)
	}
	if d2.Resource.(*Agent).Spec.Extends != "new-parent" {
		t.Error("round trip lost edit")
	}
	f1, _ := d2.Format()
	d3, _ := ParseDocument("demo.md", string(f1))
	f2, _ := d3.Format()
	if string(f1) != string(f2) {
		t.Errorf("Format not idempotent:\n%s\n---\n%s", f1, f2)
	}
}

func TestDocumentEditInstructions(t *testing.T) {
	d, err := ParseDocument("demo.md", docSample)
	if err != nil {
		t.Fatal(err)
	}
	d.Resource.(*Agent).Spec.Instructions = "New body."
	out, _ := d.Bytes()
	if !strings.Contains(string(out), "# keep this parent until Q3") || !strings.HasSuffix(string(out), "---\n\nNew body.\n") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestFrontMatterDelimiterInsideValue(t *testing.T) {
	src := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: demo\n  description: \"a --- b\"\n---\nBody with --- inside.\n"

	res, err := Parse("demo.md", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a := res.Resource.(*Agent)
	if a.Metadata.Description != "a --- b" {
		t.Errorf("description = %q", a.Metadata.Description)
	}
	if a.Spec.Instructions != "Body with --- inside." {
		t.Errorf("instructions = %q", a.Spec.Instructions)
	}

	d, err := ParseDocument("demo.md", src)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	out, err := d.Bytes()
	if err != nil || string(out) != src {
		t.Fatalf("expected verbatim output, err=%v:\n%s", err, out)
	}
}

const richSample = "---\napiVersion: warp/v1alpha1\nkind: Agent\n\nmetadata:\n  name: demo\n  description: \"quoted text\"\n\n# about skills\nspec:\n  extends: a\n\n  skills:\n    - one # first\n    # second\n    - two\n---\nBody\n"

func TestBlankLinesSurviveEdit(t *testing.T) {
	d, err := ParseDocument("demo.md", richSample)
	if err != nil {
		t.Fatal(err)
	}
	d.Resource.(*Agent).Spec.Extends = "b"
	out, _ := d.Bytes()
	want := strings.Replace(richSample, "extends: a", "extends: b", 1)
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatKeepsBlankLinesAndComments(t *testing.T) {
	d, _ := ParseDocument("demo.md", richSample)
	out, err := d.Format()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != richSample {
		t.Fatalf("got:\n%s\nwant:\n%s", out, richSample)
	}
}

func TestListEditKeepsItemComments(t *testing.T) {
	d, _ := ParseDocument("demo.md", richSample)
	a := d.Resource.(*Agent)
	a.Spec.Skills = append(a.Spec.Skills, "three")
	out, _ := d.Bytes()
	s := string(out)
	for _, want := range []string{"- one # first", "# second\n    - two", "- three"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}

	// Remove the first item and reorder: comments follow their items.
	d, _ = ParseDocument("demo.md", richSample)
	d.Resource.(*Agent).Spec.Skills = []string{"two"}
	out, _ = d.Bytes()
	s = string(out)
	if strings.Contains(s, "one") || !strings.Contains(s, "# second\n    - two") {
		t.Errorf("unexpected result:\n%s", s)
	}
}

func TestEditedStringKeepsQuoteStyle(t *testing.T) {
	d, _ := ParseDocument("demo.md", richSample)
	d.Resource.(*Agent).Metadata.Description = "other text"
	out, _ := d.Bytes()
	if !strings.Contains(string(out), `description: "other text"`) {
		t.Fatalf("quote style lost:\n%s", out)
	}
}

func TestCRLFPreserved(t *testing.T) {
	src := strings.ReplaceAll(richSample, "\n", "\r\n")
	d, err := ParseDocument("demo.md", src)
	if err != nil {
		t.Fatal(err)
	}
	d.Resource.(*Agent).Spec.Extends = "b"
	out, _ := d.Bytes()
	s := string(out)
	if strings.Count(s, "\n") != strings.Count(s, "\r\n") {
		t.Fatalf("mixed line endings:\n%q", s)
	}
	if s != strings.ReplaceAll(strings.Replace(richSample, "extends: a", "extends: b", 1), "\n", "\r\n") {
		t.Fatalf("unexpected output:\n%q", s)
	}
}

func TestMultiDocumentYAMLRejected(t *testing.T) {
	src := "apiVersion: warp/v1alpha1\nkind: Tool\nmetadata:\n  name: a\n---\napiVersion: warp/v1alpha1\nkind: Tool\nmetadata:\n  name: b\n"
	if _, err := ParseDocument("tool.yaml", src); err == nil {
		t.Fatal("expected error for multi-document YAML")
	}
}

func TestMergeKeysAndAnchors(t *testing.T) {
	src := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: demo\nspec: &base\n  extends: a\n  skills:\n    - one\n---\nBody\n"
	d, err := ParseDocument("demo.md", src)
	if err != nil {
		t.Fatal(err)
	}
	d.Resource.(*Agent).Spec.Extends = "b"
	out, _ := d.Bytes()
	if !strings.Contains(string(out), "spec: &base") || !strings.Contains(string(out), "extends: b") {
		t.Fatalf("anchor lost:\n%s", out)
	}
}

func TestYAMLFileInstructionsEditable(t *testing.T) {
	src := "apiVersion: warp/v1alpha1\nkind: Tool\nmetadata:\n  name: t\nspec:\n  command: [echo]\n"
	d, err := ParseDocument("tool.yaml", src)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := d.Bytes(); string(out) != src {
		t.Fatalf("expected verbatim:\n%s", out)
	}
	// In a .yaml file there is no Markdown body, so instructions stay in YAML.
	d.Resource.(*Tool).Spec.Instructions = "hello"
	out, _ := d.Bytes()
	if !strings.Contains(string(out), "instructions: hello") || strings.Contains(string(out), "---") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

// TestExamplesRoundTrip runs every sample file through ParseDocument: an
// unchanged document must come back byte-for-byte, Format must be idempotent,
// and the formatted file must parse to the same resource.
func TestExamplesRoundTrip(t *testing.T) {
	var files []string
	for _, root := range []string{"examples", "plugins"} {
		filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
			if err == nil && !de.IsDir() {
				if ext := filepath.Ext(p); ext == ".md" || ext == ".yaml" || ext == ".yml" {
					files = append(files, p)
				}
			}
			return nil
		})
	}
	if len(files) == 0 {
		t.Skip("no sample files found")
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			src := string(data)
			d, err := ParseDocument(f, src)
			if err != nil {
				t.Skipf("not a parseable resource: %v", err)
			}
			if out, _ := d.Bytes(); string(out) != src {
				t.Errorf("unchanged document is not verbatim")
			}
			f1, err := d.Format()
			if err != nil {
				t.Fatal(err)
			}
			d2, err := ParseDocument(f, string(f1))
			if err != nil {
				t.Fatalf("formatted output does not parse: %v\n%s", err, f1)
			}
			f2, _ := d2.Format()
			if string(f1) != string(f2) {
				t.Errorf("Format not idempotent")
			}
			d0, _ := ParseDocument(f, src)
			if !reflect.DeepEqual(d0.Resource, d2.Resource) {
				t.Errorf("formatted file parses to a different resource")
			}
		})
	}
}

func mustDoc(t *testing.T, name, src string) *Document {
	t.Helper()
	d, err := ParseDocument(name, src)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	return d
}

func TestEdgeBlankLineAfterComment(t *testing.T) {
	src := "---\napiVersion: warp/v1alpha1\nkind: Agent\n# explains metadata\n\nmetadata:\n  name: demo\n---\nBody\n"
	d := mustDoc(t, "a.md", src)
	d.Resource.(*Agent).Metadata.Name = "demo2"
	out, _ := d.Bytes()
	want := strings.Replace(src, "name: demo", "name: demo2", 1)
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestEdgeFlowSequenceStaysFlow(t *testing.T) {
	src := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: demo\nspec:\n  triggers: [human, agent]\n---\nBody\n"
	d := mustDoc(t, "a.md", src)
	a := d.Resource.(*Agent)
	a.Spec.Triggers = append(a.Spec.Triggers, "cron")
	out, _ := d.Bytes()
	if !strings.Contains(string(out), "triggers: [human, agent, cron]") {
		t.Errorf("flow style lost:\n%s", out)
	}
}

func TestEdgeRemoveFieldKeepsNeighbours(t *testing.T) {
	d := mustDoc(t, "a.md", richSample)
	d.Resource.(*Agent).Spec.Extends = ""
	out, _ := d.Bytes()
	s := string(out)
	if strings.Contains(s, "extends") || !strings.Contains(s, "# about skills") || !strings.Contains(s, "- one # first") {
		t.Errorf("unexpected output:\n%s", s)
	}
}

func TestEdgeBodyWithoutTrailingNewlineAndEmptyBody(t *testing.T) {
	head := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: demo\n---\n"
	for name, body := range map[string]string{"no newline": "Body", "empty": ""} {
		d := mustDoc(t, "a.md", head+body)
		if out, _ := d.Bytes(); string(out) != head+body {
			t.Errorf("%s: unchanged not verbatim: %q", name, out)
		}
		f, err := d.Format()
		if err != nil {
			t.Fatal(err)
		}
		d2 := mustDoc(t, "a.md", string(f))
		f2, _ := d2.Format()
		if string(f) != string(f2) {
			t.Errorf("%s: Format not idempotent", name)
		}
	}
}

func TestEdgeLeadingWhitespaceAndFooterComment(t *testing.T) {
	src := "\n\n---\n# top comment\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: demo\n# trailing comment\n---\nBody\n"
	d := mustDoc(t, "a.md", src)
	d.Resource.(*Agent).Metadata.Name = "x"
	out, _ := d.Bytes()
	s := string(out)
	if !strings.HasPrefix(s, "\n\n---\n") || !strings.Contains(s, "# top comment") || !strings.Contains(s, "# trailing comment") {
		t.Errorf("unexpected output:\n%q", s)
	}
}

func TestEdgeEditTwiceOnSameDocument(t *testing.T) {
	d := mustDoc(t, "a.md", richSample)
	a := d.Resource.(*Agent)
	a.Spec.Extends = "b"
	d.Bytes()
	a.Spec.Skills = append(a.Spec.Skills, "three")
	a.Metadata.Description = "again"
	out, _ := d.Bytes()
	d2 := mustDoc(t, "a.md", string(out))
	got := d2.Resource.(*Agent)
	if got.Spec.Extends != "b" || len(got.Spec.Skills) != 3 || got.Metadata.Description != "again" {
		t.Errorf("edits lost:\n%s", out)
	}
	if !strings.Contains(string(out), "# about skills") || !strings.Contains(string(out), "# first") {
		t.Errorf("comments lost:\n%s", out)
	}
}

func TestEdgeMergeKeyFields(t *testing.T) {
	src := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: demo\nx-defaults: &d\n  extends: a\nspec:\n  <<: *d\n  skills: [one]\n---\nBody\n"
	d := mustDoc(t, "a.md", src)
	a := d.Resource.(*Agent)
	if a.Spec.Extends != "a" {
		t.Fatalf("merge key not decoded: %q", a.Spec.Extends)
	}
	// Editing only skills must not write the merged-in field out.
	a.Spec.Skills = []string{"one", "two"}
	out, _ := d.Bytes()
	if strings.Count(string(out), "extends") != 1 {
		t.Errorf("merged-in field duplicated:\n%s", out)
	}
	// Overriding a merged-in field must win after re-parse.
	d = mustDoc(t, "a.md", src)
	d.Resource.(*Agent).Spec.Extends = "b"
	out, _ = d.Bytes()
	if got := mustDoc(t, "a.md", string(out)).Resource.(*Agent).Spec.Extends; got != "b" {
		t.Errorf("override lost, got %q:\n%s", got, out)
	}
}

func TestEdgeMappingSequenceMergeByName(t *testing.T) {
	parse := func(s string) *yaml.Node {
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(s), &n); err != nil {
			t.Fatal(err)
		}
		return n.Content[0]
	}
	orig := parse("- name: a\n  v: 1 # keep\n# about b\n- name: b\n  v: 2\n")
	snap := parse("- name: a\n  v: 1\n- name: b\n  v: 2\n")
	cur := parse("- name: b\n  v: 3\n- name: a\n  v: 1\n- name: c\n  v: 9\n")
	mergeNode(orig, snap, cur)
	out, _ := yaml.Marshal(orig)
	s := string(out)
	for _, want := range []string{"# about b", "v: 1 # keep", "v: 3", "name: c"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Index(s, "name: b") > strings.Index(s, "name: a") {
		t.Errorf("order not following cur:\n%s", s)
	}
}
