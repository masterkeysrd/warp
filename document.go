package warp

import (
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"
)

// blankMarker is a temporary comment inserted in front of nodes that had a
// blank line above them in the original file. yaml does not keep blank lines,
// so they are re-created from this marker after encoding.
const blankMarker = "#warp-blank-line"

var blankMarkerRe = regexp.MustCompile(`(?m)^[ \t]*` + blankMarker + `\r?\n`)

// hasBody reports whether resources of the given kind keep their Markdown body
// in Spec.Instructions.
func hasBody(k Kind) bool {
	switch k {
	case KindWorkspace, KindContext, KindAgent, KindSkill, KindCommand,
		KindDecision, KindRule, KindTool, KindPlugin:
		return true
	}
	return false
}

// Document is a typed WARP resource that remembers the file it was parsed
// from. Editors mutate Resource using the regular types and call Bytes (or
// Format) to write the file back. Only the values that actually changed are
// patched into the original YAML tree, so comments, key order, blank lines,
// quote styles, unknown fields, line endings and the Markdown body are
// preserved.
//
//	doc, err := warp.ParseDocument(path, content)
//	agent := doc.Resource.(*warp.Agent)
//	agent.Spec.Extends = "base-agent"
//	out, err := doc.Bytes()
//
// A Document is not safe for concurrent use.
type Document struct {
	// Resource is the typed resource. Mutate it freely before calling Bytes.
	Resource Resource
	// Kind is the resource kind.
	Kind Kind
	// Inferred is true when the file had no front-matter (WORKSPACE.md or
	// AGENTS.md); such files only carry instructions.
	Inferred bool

	original  string
	isYAML    bool
	crlf      bool
	fresh     bool       // built from a Resource, no original file
	prefix    string     // whitespace before the opening delimiter
	root      *yaml.Node // original YAML document node (nil when Inferred/fresh)
	body      string     // raw Markdown body, exactly as in the file
	snapshot  *yaml.Node // encoding of Resource at parse time
	snapInstr string     // instructions at parse time
}

// ParseDocument parses content like Parse, but keeps the original file
// structure so it can be written back losslessly. See Document.
func ParseDocument(filePath, content string) (*Document, error) {
	parsed, err := Parse(filePath, content)
	if err != nil {
		return nil, err
	}
	res, ok := parsed.Resource.(Resource)
	if !ok {
		return nil, fmt.Errorf("%s is not a document-capable resource", filePath)
	}
	ext := strings.ToLower(path.Ext(filePath))
	d := &Document{
		Resource: res,
		Kind:     parsed.Kind,
		Inferred: parsed.Inferred,
		original: content,
		isYAML:   ext == ".yaml" || ext == ".yml",
		crlf:     strings.Contains(content, "\r\n"),
	}
	if d.snapshot, d.snapInstr, err = encodeResource(res, d.strip()); err != nil {
		return nil, err
	}
	if parsed.Inferred {
		return d, nil
	}

	yamlText := content
	if !d.isYAML {
		var ok bool
		d.prefix, yamlText, d.body, ok = splitFrontMatter(content)
		if !ok {
			return nil, ErrMissingFrontMatter
		}
	}

	// Line endings are restored on output (see finish), so work on LF text.
	yamlText = strings.ReplaceAll(yamlText, "\r\n", "\n")
	dec := yaml.NewDecoder(strings.NewReader(yamlText))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("failed to parse front-matter: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("multiple YAML documents in one file are not supported")
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("front-matter must be a YAML mapping")
	}
	markBlankLines(&root, strings.Split(yamlText, "\n"))
	d.root = &root
	return d, nil
}

// strip reports whether spec.instructions lives in the Markdown body (and so
// must be removed from the YAML tree).
func (d *Document) strip() bool { return hasBody(d.Kind) && !d.isYAML }

// Bytes serializes the document, patching only what changed since parsing.
// If nothing changed, the original content is returned byte-for-byte.
func (d *Document) Bytes() ([]byte, error) { return d.render(false) }

// Format serializes the document with canonical YAML layout (2-space indent,
// normalized trailing newline) while still preserving comments, blank lines,
// key order and unknown fields.
func (d *Document) Format() ([]byte, error) { return d.render(true) }

func (d *Document) render(canonical bool) ([]byte, error) {
	cur, curInstr, err := encodeResource(d.Resource, d.strip())
	if err != nil {
		return nil, err
	}

	var rootNode *yaml.Node
	var body string
	useDelims := !d.isYAML
	instrChanged := false

	switch {
	case d.fresh:
		rootNode = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{cur}}
		body = curInstr
		useDelims = hasBody(d.Kind) && (curInstr != "" || RecommendedExtension(d.Kind) == ".md")
	case d.Inferred:
		if curInstr == d.snapInstr {
			return []byte(d.original), nil
		}
		return []byte(d.finish(curInstr + "\n")), nil
	default:
		changed := !nodesEqual(d.snapshot, cur)
		instrChanged = d.strip() && curInstr != d.snapInstr
		if !canonical && !changed && !instrChanged {
			return []byte(d.original), nil
		}
		if changed {
			mergeNode(d.root.Content[0], d.snapshot, cur)
		}
		rootNode = d.root
		body = d.body
		if instrChanged {
			body = "\n" + curInstr
		}
	}

	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(rootNode); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	yamlOut := blankMarkerRe.ReplaceAllString(buf.String(), "\n")

	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	out := yamlOut
	if useDelims {
		out = d.prefix + "---\n" + yamlOut + "---\n" + body
	}
	out = d.finish(out)

	if d.fresh {
		return []byte(out), nil
	}

	// Remember the new state so further edits patch against it.
	d.original = out
	d.body = body
	if d.snapshot, d.snapInstr, err = encodeResource(d.Resource, d.strip()); err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// finish applies the original file's line-ending style.
func (d *Document) finish(s string) string {
	if d.crlf {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// splitFrontMatter splits a Markdown file into the whitespace prefix, the YAML
// front-matter text and the raw body. Delimiters must be whole lines.
func splitFrontMatter(content string) (prefix, yamlText, body string, ok bool) {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	prefix = content[:len(content)-len(trimmed)]
	lines := strings.SplitAfter(trimmed, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t\r\n") != "---" {
		return "", "", "", false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t\r\n") == "---" {
			return prefix, strings.Join(lines[1:i], ""), strings.Join(lines[i+1:], ""), true
		}
	}
	return "", "", "", false
}

// encodeResource encodes res to a YAML mapping node. When strip is set,
// spec.instructions (which lives in the Markdown body) is removed from the node
// and returned separately.
func encodeResource(res Resource, strip bool) (*yaml.Node, string, error) {
	var n yaml.Node
	if err := n.Encode(res); err != nil {
		return nil, "", err
	}
	var instr string
	if strip {
		if spec := mapValue(&n, "spec"); spec != nil && spec.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(spec.Content); i += 2 {
				if spec.Content[i].Value == "instructions" {
					instr = spec.Content[i+1].Value
					spec.Content = append(spec.Content[:i], spec.Content[i+2:]...)
					break
				}
			}
		}
	}
	return &n, instr, nil
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func nodesEqual(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind || len(a.Content) != len(b.Content) {
		return false
	}
	if a.Kind == yaml.ScalarNode && (a.Value != b.Value || a.ShortTag() != b.ShortTag()) {
		return false
	}
	for i := range a.Content {
		if !nodesEqual(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}

// markBlankLines tags every mapping key and sequence item that had a blank line
// above it (ignoring comment lines directly above it) so the blank line can be
// restored after encoding. The first entry of a collection is never tagged.
func markBlankLines(n *yaml.Node, lines []string) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			markBlankLines(c, lines)
		}
	case yaml.MappingNode:
		flow := n.Style&yaml.FlowStyle != 0
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 && !flow {
				markBlank(n.Content[i], lines)
			}
			markBlankLines(n.Content[i+1], lines)
		}
	case yaml.SequenceNode:
		flow := n.Style&yaml.FlowStyle != 0
		for i, c := range n.Content {
			if i > 0 && !flow {
				markBlank(c, lines)
			}
			markBlankLines(c, lines)
		}
	}
}

func markBlank(n *yaml.Node, lines []string) {
	i := n.Line - 2 // 0-based index of the line above the node
	skipped := 0
	for i >= 0 && i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
		i--
		skipped++
	}
	if i < 0 || i >= len(lines) || strings.TrimSpace(lines[i]) != "" {
		return
	}
	// "# comment", blank, node: the comment is a foot comment of the previous
	// node and the encoder already emits the blank line after it.
	if skipped == 0 && i > 0 && strings.HasPrefix(strings.TrimSpace(lines[i-1]), "#") {
		return
	}
	if n.HeadComment == "" {
		n.HeadComment = blankMarker
	} else {
		n.HeadComment = blankMarker + "\n" + n.HeadComment
	}
}

// mergeNode patches orig so it reflects cur. snap is what the typed struct
// encoded to when the file was parsed: keys present in orig but absent from
// snap are unknown to the types and are kept; keys present in snap but absent
// from cur were removed by the editor and are deleted.
func mergeNode(orig, snap, cur *yaml.Node) {
	if snap != nil && nodesEqual(snap, cur) {
		return // untouched: keep original formatting and comments
	}
	switch {
	case orig.Kind == yaml.MappingNode && cur.Kind == yaml.MappingNode &&
		(snap == nil || snap.Kind == yaml.MappingNode):
		mergeMapping(orig, snap, cur)
	case orig.Kind == yaml.SequenceNode && cur.Kind == yaml.SequenceNode &&
		(snap == nil || snap.Kind == yaml.SequenceNode):
		mergeSequence(orig, snap, cur)
	default:
		// Scalars, kind changes and aliases (editing an aliased field expands
		// the alias for that field only).
		replaceNode(orig, cur)
	}
}

func mergeMapping(orig, snap, cur *yaml.Node) {
	for i := 0; i+1 < len(cur.Content); i += 2 {
		key, val := cur.Content[i], cur.Content[i+1]
		if existing := mapValue(orig, key.Value); existing != nil {
			mergeNode(existing, mapValue(snap, key.Value), val)
			continue
		}
		// Not written in the file. If the typed value is unchanged it came from
		// a YAML merge key (<<) or a decoder default; don't write it out.
		if sv := mapValue(snap, key.Value); sv != nil && nodesEqual(sv, val) {
			continue
		}
		orig.Content = append(orig.Content, key, val)
	}
	for i := len(orig.Content) - 2; i >= 0; i -= 2 {
		k := orig.Content[i].Value
		if mapValue(cur, k) == nil && mapValue(snap, k) != nil {
			orig.Content = append(orig.Content[:i], orig.Content[i+2:]...)
		}
	}
}

// seqKey returns an identity for a sequence item: the value of a scalar, or the
// name/id of a mapping. Items without identity are matched by position.
func seqKey(n *yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		return "s:" + n.ShortTag() + ":" + n.Value
	case yaml.MappingNode:
		for _, k := range []string{"name", "id"} {
			if v := mapValue(n, k); v != nil && v.Kind == yaml.ScalarNode {
				return "m:" + k + ":" + v.Value
			}
		}
	}
	return ""
}

// mergeSequence patches orig item by item so comments attached to surviving
// items are kept. New items are inserted, removed items dropped, and the order
// follows cur.
func mergeSequence(orig, snap, cur *yaml.Node) {
	used := make([]bool, len(orig.Content))
	find := func(key string, idx int) int {
		if key != "" {
			for j, o := range orig.Content {
				if !used[j] && seqKey(o) == key {
					return j
				}
			}
			return -1
		}
		if idx < len(orig.Content) && !used[idx] && seqKey(orig.Content[idx]) == "" {
			return idx
		}
		return -1
	}
	snapFor := func(key string, idx int) *yaml.Node {
		if snap == nil {
			return nil
		}
		if key != "" {
			for _, s := range snap.Content {
				if seqKey(s) == key {
					return s
				}
			}
			return nil
		}
		if idx < len(snap.Content) && seqKey(snap.Content[idx]) == "" {
			return snap.Content[idx]
		}
		return nil
	}

	out := make([]*yaml.Node, 0, len(cur.Content))
	for i, c := range cur.Content {
		key := seqKey(c)
		if j := find(key, i); j >= 0 {
			used[j] = true
			mergeNode(orig.Content[j], snapFor(key, i), c)
			out = append(out, orig.Content[j])
		} else {
			out = append(out, c)
		}
	}
	orig.Content = out
}

// replaceNode overwrites orig with cur, keeping orig's comments, anchor and
// (for strings) its quoting style.
func replaceNode(orig, cur *yaml.Node) {
	head, line, foot, anchor, style := orig.HeadComment, orig.LineComment, orig.FootComment, orig.Anchor, orig.Style
	keepStyle := orig.Kind == yaml.ScalarNode && cur.Kind == yaml.ScalarNode &&
		cur.Style == 0 && orig.ShortTag() == cur.ShortTag() &&
		style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0

	*orig = *cur
	if orig.HeadComment == "" {
		orig.HeadComment = head
	}
	if orig.LineComment == "" {
		orig.LineComment = line
	}
	if orig.FootComment == "" {
		orig.FootComment = foot
	}
	if orig.Anchor == "" {
		orig.Anchor = anchor
	}
	if keepStyle {
		orig.Style = style
	}
}
