package warp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func sbBase(kind Kind, ns, name string) BaseResource {
	return BaseResource{APIVersion: APIVersion, Kind: kind, Metadata: Metadata{Name: name}, Namespace: ns}
}

func newTestSandboxProvider(ns, name, driver string) *SandboxProvider {
	return &SandboxProvider{BaseResource: sbBase(KindSandboxProvider, ns, name), Spec: SandboxProviderSpec{Driver: driver}}
}

func newTestSandbox(ns, name string, spec SandboxSpec) *Sandbox {
	return &Sandbox{BaseResource: sbBase(KindSandbox, ns, name), Spec: spec}
}

func newTestFileSystem(ns, name, driver string, config map[string]any) *FileSystem {
	return &FileSystem{BaseResource: sbBase(KindFileSystem, ns, name), Spec: FileSystemSpec{Driver: driver, Config: config}}
}

func newTestAgent(ns, name, sandbox string, tools ...string) *Agent {
	return &Agent{BaseResource: sbBase(KindAgent, ns, name), Spec: AgentSpec{Sandbox: sandbox, Tools: tools}}
}

func newTestTool(ns, name, sandbox string, command ...string) *Tool {
	return &Tool{BaseResource: sbBase(KindTool, ns, name), Spec: ToolSpec{Sandbox: sandbox, Command: command}}
}

// newSandboxTestRegistry builds a registry rooted at /ws with an optional
// workspace definition.
func newSandboxTestRegistry(wsSpec *WorkspaceDefSpec, resources ...Resource) *Registry {
	ws := &Workspace{RootPath: "/ws"}
	if wsSpec != nil {
		ws.Def = &WorkspaceDef{BaseResource: sbBase(KindWorkspace, "", "ws"), Spec: *wsSpec}
	}
	reg := NewRegistry(ws)
	for _, r := range resources {
		reg.set(r.QualifiedName(), r)
	}
	return reg
}

func wsWithSandboxPolicy(p *SandboxPolicies) *WorkspaceDefSpec {
	return &WorkspaceDefSpec{Policies: &Policies{Sandbox: p}}
}

func validationMessages(err error) []string {
	if err == nil {
		return nil
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		out := make([]string, 0, len(ve.Issues))
		for _, i := range ve.Issues {
			out = append(out, i.Error())
		}
		return out
	}
	return []string{err.Error()}
}

func containsSubstring(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ─── Parsing ─────────────────────────────────────────────────────────────────

func TestParse_SandboxKinds(t *testing.T) {
	t.Run("SandboxProvider", func(t *testing.T) {
		res, err := Parse("providers/e2b.yaml", `apiVersion: warp/v1alpha1
kind: SandboxProvider
metadata:
  name: cloud
spec:
  driver: e2b.dev/sandbox
  endpoint: https://api.e2b.dev
  auth:
    type: api-key
    header: X-API-Key
    token: ${env:E2B_API_KEY}
  config:
    template: python-3.12
    region: eu
`)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		sp, ok := res.Resource.(*SandboxProvider)
		if !ok || res.Kind != KindSandboxProvider {
			t.Fatalf("expected *SandboxProvider, got %T (%s)", res.Resource, res.Kind)
		}
		if sp.Spec.Driver != "e2b.dev/sandbox" || sp.Spec.Endpoint != "https://api.e2b.dev" {
			t.Errorf("unexpected spec: %+v", sp.Spec)
		}
		if sp.Spec.Auth == nil || sp.Spec.Auth.Token != "${env:E2B_API_KEY}" {
			t.Errorf("auth not parsed: %+v", sp.Spec.Auth)
		}
		if sp.Spec.Config["template"] != "python-3.12" || !sp.Spec.IsEnabled() {
			t.Errorf("config/enabled not parsed: %+v", sp.Spec)
		}
	})

	t.Run("Sandbox", func(t *testing.T) {
		res, err := Parse("sandboxes/s3-analyst.yaml", `apiVersion: warp/v1alpha1
kind: Sandbox
metadata:
  name: s3-analyst
spec:
  enabled: false
  extends: base
  provider: virtual
  config: {image: alpine}
  rootfs:
    mode: none
  mounts:
    - { filesystem: reports, path: /reports, mode: rw }
    - { filesystem: scratch, path: /tmp, mode: rw }
  workdir: /reports
  access:
    write: ["drafts/**", "/tmp/**"]
    deny: ["**/payroll/**"]
  exec:
    mode: allowlist
    allow: [bash, ls]
  network:
    mode: allowlist
    allow: ["*.pypi.org"]
    deny: ["bad.pypi.org"]
  env:
    passthrough: [LANG]
    set:
      TOKEN: ${env:TOKEN}
  limits:
    cpu: "2"
    memory: 4Gi
    processes: 64
    timeout: 45m
`)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		sb, ok := res.Resource.(*Sandbox)
		if !ok {
			t.Fatalf("expected *Sandbox, got %T", res.Resource)
		}
		s := sb.Spec
		if s.IsEnabled() || s.Extends != "base" || s.Provider != "virtual" || s.Config["image"] != "alpine" {
			t.Errorf("unexpected top-level spec: %+v", s)
		}
		if s.RootFS == nil || s.RootFS.Mode != SandboxRootFSNone {
			t.Errorf("rootfs not parsed: %+v", s.RootFS)
		}
		if len(s.Mounts) != 2 || s.Mounts[0] != (SandboxMount{FileSystem: "reports", Path: "/reports", Mode: SandboxMountReadWrite}) {
			t.Errorf("mounts not parsed: %+v", s.Mounts)
		}
		if s.Workdir != "/reports" || len(s.Access.Write) != 2 || s.Access.Read != nil || len(s.Access.Deny) != 1 {
			t.Errorf("workdir/access not parsed: %q %+v", s.Workdir, s.Access)
		}
		if s.Exec.Mode != SandboxExecAllowlist || !slices.Equal(s.Exec.Allow, []string{"bash", "ls"}) {
			t.Errorf("exec not parsed: %+v", s.Exec)
		}
		if s.Network.Mode != SandboxNetworkAllowlist || s.Network.Deny[0] != "bad.pypi.org" {
			t.Errorf("network not parsed: %+v", s.Network)
		}
		if s.Env.Set["TOKEN"] != "${env:TOKEN}" || s.Env.Passthrough[0] != "LANG" {
			t.Errorf("env not parsed: %+v", s.Env)
		}
		if *s.Limits != (SandboxLimits{CPU: "2", Memory: "4Gi", Processes: 64, Timeout: "45m"}) {
			t.Errorf("limits not parsed: %+v", s.Limits)
		}

		// DeepCopy is independent.
		cp := sb.DeepCopy()
		cp.Spec.Mounts[0].Path = "/changed"
		cp.Spec.Access.Write[0] = "x"
		cp.Spec.Env.Set["TOKEN"] = "y"
		cp.Spec.Config["image"] = "z"
		if sb.Spec.Mounts[0].Path != "/reports" || sb.Spec.Access.Write[0] != "drafts/**" || sb.Spec.Env.Set["TOKEN"] != "${env:TOKEN}" || sb.Spec.Config["image"] != "alpine" {
			t.Error("DeepCopy shares state with the original")
		}
	})

	t.Run("FileSystem", func(t *testing.T) {
		res, err := Parse("filesystems/reports.yml", `apiVersion: warp/v1alpha1
kind: FileSystem
metadata: { name: reports }
spec:
  driver: acme.com/s3fs
  persistence: persistent
  readOnly: true
  auth:
    type: bearer
    token: ${env:AWS_SESSION_TOKEN}
  config:
    bucket: acme-reports
    nested: {prefix: finance/}
`)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		fsr, ok := res.Resource.(*FileSystem)
		if !ok {
			t.Fatalf("expected *FileSystem, got %T", res.Resource)
		}
		if fsr.Spec.Driver != "acme.com/s3fs" || fsr.Spec.Persistence != FileSystemPersistencePersistent || !fsr.Spec.ReadOnly {
			t.Errorf("unexpected spec: %+v", fsr.Spec)
		}
		cp := fsr.DeepCopy()
		cp.Spec.Config["nested"].(map[string]any)["prefix"] = "changed"
		if fsr.Spec.Config["nested"].(map[string]any)["prefix"] != "finance/" {
			t.Error("DeepCopy did not deep-copy nested config")
		}
	})

	t.Run("sandbox fields on existing kinds", func(t *testing.T) {
		ws, err := Parse("WORKSPACE.md", `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: ws
spec:
  defaultSandbox: dev
  policies:
    sandbox:
      required: true
      drivers:
        exclude: [local]
      hostPaths: ["$WorkspaceDir/**"]
      hostWrite: deny
      rootfs: {mode: ro}
      network: {mode: allowlist, allow: [api.github.com]}
      env: {passthrough: [PATH]}
      limits: {memory: 8Gi}
---
`)
		if err != nil {
			t.Fatalf("Parse workspace: %v", err)
		}
		def := ws.Resource.(*WorkspaceDef)
		p := def.Spec.Policies.Sandbox
		if def.Spec.DefaultSandbox != "dev" || p == nil || !p.Required || p.HostWrite != SandboxHostWriteDeny ||
			p.Drivers.Exclude[0] != "local" || p.RootFS.Mode != SandboxRootFSReadOnly || p.Limits.Memory != "8Gi" {
			t.Errorf("workspace sandbox fields not parsed: %+v / %+v", def.Spec, p)
		}
		cp := def.DeepCopy()
		cp.Spec.Policies.Sandbox.Drivers.Exclude[0] = "x"
		if p.Drivers.Exclude[0] != "local" {
			t.Error("Policies.DeepCopy shares sandbox policies")
		}

		ag, err := Parse("agents/a.md", "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: a\nspec:\n  sandbox: none\n---\n")
		if err != nil || ag.Resource.(*Agent).Spec.Sandbox != SandboxNone {
			t.Errorf("agent sandbox not parsed: %v", err)
		}
		tl, err := Parse("tools/t.yaml", "apiVersion: warp/v1alpha1\nkind: Tool\nmetadata:\n  name: t\nspec:\n  command: [ls]\n  sandbox: dev\n")
		if err != nil || tl.Resource.(*Tool).Spec.Sandbox != "dev" {
			t.Errorf("tool sandbox not parsed: %v", err)
		}
		mc, err := Parse("mcps/m.yaml", "apiVersion: warp/v1alpha1\nkind: MCP\nmetadata:\n  name: m\nspec:\n  command: [node]\n  sandbox: dev\n")
		if err != nil || mc.Resource.(*MCP).Spec.Sandbox != "dev" {
			t.Errorf("mcp sandbox not parsed: %v", err)
		}
	})

	t.Run("format round trip", func(t *testing.T) {
		for _, content := range []string{
			"apiVersion: warp/v1alpha1\nkind: SandboxProvider\nmetadata:\n  name: p\nspec:\n  driver: container\n  config:\n    image: alpine\n",
			"apiVersion: warp/v1alpha1\nkind: Sandbox\nmetadata:\n  name: s\nspec:\n  provider: p\n  mounts:\n    - filesystem: src\n      path: /src\n      mode: rw\n  network:\n    mode: open\n",
			"apiVersion: warp/v1alpha1\nkind: FileSystem\nmetadata:\n  name: f\nspec:\n  driver: memory\n  config:\n    size: 1Gi\n",
		} {
			res, err := Parse("x.yaml", content)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			out, err := Format(res.Resource.(Resource))
			if err != nil {
				t.Fatalf("Format: %v", err)
			}
			if string(out) != content {
				t.Errorf("round trip mismatch:\n got: %q\nwant: %q", out, content)
			}
			if RecommendedExtension(res.Kind) != ".yaml" {
				t.Errorf("RecommendedExtension(%s) != .yaml", res.Kind)
			}
		}
	})

	t.Run("ValidateBase accepts new kinds", func(t *testing.T) {
		for _, k := range []Kind{KindSandboxProvider, KindSandbox, KindFileSystem} {
			b := sbBase(k, "", "x")
			if err := b.ValidateBase(); err != nil {
				t.Errorf("ValidateBase(%s): %v", k, err)
			}
		}
	})
}

func TestLoadWorkspace_SandboxKindsAndRuntimeDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(tmpDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("WORKSPACE.md", "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: ws\nspec:\n  projects: [\".\"]\n---\n")
	write(".agents/providers/container.yaml", "apiVersion: warp/v1alpha1\nkind: SandboxProvider\nmetadata:\n  name: docker\nspec:\n  driver: container\n")
	write(".agents/filesystems/src.yaml", "apiVersion: warp/v1alpha1\nkind: FileSystem\nmetadata:\n  name: src\nspec:\n  driver: host\n  config: { path: $ProjectDir }\n")
	write(".agents/sandboxes/dev.yaml", "apiVersion: warp/v1alpha1\nkind: Sandbox\nmetadata:\n  name: dev\nspec:\n  provider: docker\n  mounts:\n    - filesystem: src\n      path: /workspace\n      mode: rw\n  network:\n    mode: open\n")
	write(".agents/agents/coder.md", "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: coder\nspec:\n  sandbox: dev\n---\n")

	// Runtime-provided system resources.
	system := NewFSResourceProvider(fstest.MapFS{
		"filesystems/cwd.yaml":   {Data: []byte("apiVersion: warp/v1alpha1\nkind: FileSystem\nmetadata:\n  name: cwd\nspec:\n  driver: host\n  config: { path: $WorkspaceDir }\n")},
		"providers/native.yaml":  {Data: []byte("apiVersion: warp/v1alpha1\nkind: SandboxProvider\nmetadata:\n  name: native\nspec:\n  driver: native\n")},
		"sandboxes/default.yaml": {Data: []byte("apiVersion: warp/v1alpha1\nkind: Sandbox\nmetadata:\n  name: default\nspec:\n  provider: native\n  mounts:\n    - filesystem: cwd\n      mode: rw\n  access:\n    deny: [\"**/.env*\"]\n")},
	}, "")

	reg, err := LoadWorkspace(tmpDir, system)
	if err != nil {
		t.Fatalf("LoadWorkspace: %v", err)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	projName := strings.ToLower(filepath.Base(tmpDir))
	scoped := reg.Project(projName)
	for _, k := range []Kind{KindSandboxProvider, KindSandbox, KindFileSystem} {
		if got := scoped.ListResources(QueryOptions{Kinds: []Kind{k}}); len(got) != 2 {
			t.Errorf("expected 2 loaded %s resources (project + system), got %d", k, len(got))
		}
	}
	if _, ok := reg.ResolveResource(SandboxDefault); !ok {
		t.Fatal("system/Sandbox/default not loaded from system provider")
	}

	ra, err := scoped.ResolveAgent("coder")
	if err != nil {
		t.Fatalf("ResolveAgent: %v", err)
	}
	rs, err := ra.Sandbox()
	if err != nil {
		t.Fatalf("Sandbox: %v", err)
	}
	if rs.Sandbox.GetName() != "dev" || rs.Driver != SandboxDriverContainer || rs.Workdir != "/workspace" || !rs.Explicit {
		t.Errorf("unexpected agent sandbox: name=%s driver=%s workdir=%s explicit=%v", rs.Sandbox.GetName(), rs.Driver, rs.Workdir, rs.Explicit)
	}
	if len(rs.Mounts) != 1 || rs.Mounts[0].HostPath != filepath.ToSlash(filepath.Clean(tmpDir)) || rs.Mounts[0].Mode != SandboxMountReadWrite {
		t.Errorf("unexpected mounts: %+v", rs.Mounts)
	}

	// MCP without its own sandbox falls back to the runtime default, whose cwd
	// mount defaults to the rendered host path and becomes the workdir.
	mcp := &MCP{BaseResource: sbBase(KindMCP, projName, "srv"), Spec: MCPSpec{Command: []string{"node"}}}
	mrs, err := scoped.ResolveMCPSandbox(mcp)
	if err != nil {
		t.Fatalf("ResolveMCPSandbox: %v", err)
	}
	cwd := filepath.ToSlash(filepath.Clean(tmpDir))
	if mrs.Source != SandboxSourceSystem || mrs.Explicit || mrs.Workdir != cwd || mrs.Driver != SandboxDriverNative {
		t.Errorf("unexpected MCP sandbox: source=%s explicit=%v workdir=%s driver=%s", mrs.Source, mrs.Explicit, mrs.Workdir, mrs.Driver)
	}
	if mrs.AllowsPath(".env.local", SandboxAccessRead) {
		t.Error("default sandbox should deny .env files under the workdir")
	}
}

// ─── Glob & driver names ─────────────────────────────────────────────────────

func TestMatchSandboxGlob(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"/ws/**", "/ws", true},
		{"/ws/**", "/ws/a/b/c", true},
		{"/ws/**", "/wsx/a", false},
		{"/a/**/b", "/a/b", true},
		{"/a/**/b", "/a/x/y/b", true},
		{"/a/**/b", "/a/x/y/c", false},
		{"**/.env", ".env", true},
		{"**/.env", "x/y/.env", true},
		{"/r/**/payroll/**", "/r/2024/payroll/q1.csv", true},
		{"/r/*.md", "/r/a.md", true},
		{"/r/*.md", "/r/sub/a.md", false},
		{"/r/?.md", "/r/a.md", true},
		{"/r/[ab].md", "/r/c.md", false},
		{"relative/**", "/relative/x", false},
		{"/abs", "abs", false},
		{"acme.com/*", "acme.com/s3fs", true},
		{"/r/[", "/r/[", false},
		{"", "", false},
	}
	for _, tc := range tests {
		if got := matchSandboxGlob(tc.pattern, tc.name); got != tc.want {
			t.Errorf("matchSandboxGlob(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

func TestValidateDriverName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
	}{
		{"container", ""},
		{"virtual", ""},
		{"e2b.dev/sandbox", ""},
		{"", "spec.driver is required"},
		{"docker", "unknown sandbox driver"},
		{"host", "unknown sandbox driver"},
		{"acme/sandbox", "domain-qualified"},
		{"acme.com", "unknown sandbox driver"},
		{"acme.com/", "domain-qualified"},
	}
	for _, tc := range tests {
		err := validateDriverName("sandbox", tc.name, ReservedSandboxDrivers)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("validateDriverName(%q): unexpected error %v", tc.name, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("validateDriverName(%q) = %v, want error containing %q", tc.name, err, tc.wantErr)
		}
	}
	if err := validateDriverName("filesystem", "host", ReservedFileSystemDrivers); err != nil {
		t.Errorf("host should be a reserved filesystem driver: %v", err)
	}
}

// ─── Inheritance ─────────────────────────────────────────────────────────────

func sandboxBaseResources() []Resource {
	return []Resource{
		newTestFileSystem(NamespaceSystem, "cwd", FileSystemDriverHost, map[string]any{"path": "/ws"}),
		newTestSandboxProvider(NamespaceSystem, "native", SandboxDriverNative),
		newTestSandbox(NamespaceSystem, "default", SandboxSpec{
			Provider: "native",
			Config:   map[string]any{"a": 1, "shared": "parent"},
			Mounts:   []SandboxMount{{FileSystem: "cwd", Mode: SandboxMountReadWrite}},
			Access:   &SandboxAccess{Deny: []string{"**/.env"}},
		}),
	}
}

func TestSandbox_ExtendsMerge(t *testing.T) {
	resources := append(sandboxBaseResources(),
		newTestFileSystem(NamespaceWorkspace, "datasets", FileSystemDriverHost, map[string]any{"path": "$WorkspaceDir/data"}),
		newTestFileSystem(NamespaceWorkspace, "scratch", FileSystemDriverMemory, nil),
		newTestSandboxProvider(NamespaceWorkspace, "docker", SandboxDriverContainer),
		newTestSandbox(NamespaceWorkspace, "ml", SandboxSpec{
			Extends: SandboxDefault,
			Config:  map[string]any{"b": 2, "shared": "child"},
			Mounts:  []SandboxMount{{FileSystem: "datasets", Path: "/data"}},
			Limits:  &SandboxLimits{Memory: "2Gi"},
		}),
		newTestSandbox(NamespaceWorkspace, "override", SandboxSpec{
			Extends:  "ml",
			Provider: "docker",
			Mounts:   []SandboxMount{{FileSystem: "scratch", Path: "/ws", Mode: SandboxMountReadWrite}},
			Workdir:  "/data",
			Access:   &SandboxAccess{Write: []string{"out/**"}},
		}),
		newTestSandbox(NamespaceWorkspace, "loop-a", SandboxSpec{Extends: "loop-b"}),
		newTestSandbox(NamespaceWorkspace, "loop-b", SandboxSpec{Extends: "loop-a"}),
		newTestSandbox(NamespaceWorkspace, "self", SandboxSpec{Extends: "self"}),
	)
	reg := newSandboxTestRegistry(&WorkspaceDefSpec{}, resources...)

	rs, err := reg.ResolveSandbox("ml")
	if err != nil {
		t.Fatalf("ResolveSandbox(ml): %v", err)
	}
	if rs.Provider == nil || rs.Provider.GetName() != "native" || rs.Driver != SandboxDriverNative {
		t.Errorf("provider not inherited: %+v", rs.Provider)
	}
	if rs.Config["a"] != 1 || rs.Config["b"] != 2 || rs.Config["shared"] != "child" {
		t.Errorf("config not shallow-merged: %v", rs.Config)
	}
	if len(rs.Mounts) != 2 || rs.Mounts[0].Path != "/ws" || rs.Mounts[1].Path != "/data" || rs.Mounts[1].Mode != SandboxMountReadOnly {
		t.Errorf("mounts not merged: %+v", rs.Mounts)
	}
	if rs.Workdir != "/ws" {
		t.Errorf("workdir should default to the cwd mount, got %q", rs.Workdir)
	}
	if len(rs.Access) != 1 || rs.Access[0].Deny[0] != "/ws/**/.env" {
		t.Errorf("access not inherited/absolutized: %+v", rs.Access)
	}
	if rs.Limits.Memory != "2Gi" || rs.Network.Mode != SandboxNetworkNone {
		t.Errorf("limits/network unexpected: %+v %+v", rs.Limits, rs.Network)
	}

	rs, err = reg.ResolveSandbox("workspace/override")
	if err != nil {
		t.Fatalf("ResolveSandbox(override): %v", err)
	}
	if rs.Driver != SandboxDriverContainer {
		t.Errorf("provider not replaced: %s", rs.Driver)
	}
	if len(rs.Mounts) != 2 || rs.Mounts[0].FileSystem.GetName() != "scratch" || rs.Mounts[0].Path != "/ws" {
		t.Errorf("mount at same path not replaced: %+v", rs.Mounts)
	}
	if rs.Workdir != "/data" || len(rs.Access) != 1 || rs.Access[0].Deny != nil || rs.Access[0].Write[0] != "/data/out/**" {
		t.Errorf("workdir/access not replaced: %q %+v", rs.Workdir, rs.Access)
	}
	if rs.Limits.Memory != "2Gi" {
		t.Errorf("limits should be inherited through the chain: %+v", rs.Limits)
	}

	for _, ref := range []string{"loop-a", "self"} {
		if _, err := reg.ResolveSandbox(ref); err == nil || !strings.Contains(err.Error(), "circular") {
			t.Errorf("ResolveSandbox(%s): expected circular error, got %v", ref, err)
		}
	}
	msgs := validationMessages(reg.Validate())
	if !containsSubstring(msgs, "circular sandbox inheritance") {
		t.Errorf("Validate should report circular inheritance, got %v", msgs)
	}
}

// ─── Selection ───────────────────────────────────────────────────────────────

func TestSandbox_SelectionOrder(t *testing.T) {
	base := func(wsDefault string, withSystem bool, extra ...Resource) *Registry {
		res := []Resource{
			newTestSandbox(NamespaceWorkspace, "tool-sb", SandboxSpec{Network: &SandboxNetwork{Mode: SandboxNetworkOpen}}),
			newTestSandbox(NamespaceWorkspace, "agent-sb", SandboxSpec{Network: &SandboxNetwork{Mode: SandboxNetworkOpen}}),
			newTestSandbox(NamespaceWorkspace, "ws-sb", SandboxSpec{}),
			newTestSandbox(NamespaceWorkspace, "mcp-sb", SandboxSpec{}),
			newTestAgent(NamespaceWorkspace, "with-sb", "agent-sb"),
			newTestAgent(NamespaceWorkspace, "plain", ""),
			newTestAgent(NamespaceWorkspace, "unsandboxed", SandboxNone),
		}
		if withSystem {
			res = append(res, newTestSandbox(NamespaceSystem, "default", SandboxSpec{}))
		}
		res = append(res, extra...)
		return newSandboxTestRegistry(&WorkspaceDefSpec{DefaultSandbox: wsDefault}, res...)
	}
	toolWithSB := newTestTool(NamespaceWorkspace, "t1", "tool-sb", "ls")
	toolPlain := newTestTool(NamespaceWorkspace, "t2", "", "ls")
	toolNone := newTestTool(NamespaceWorkspace, "t3", SandboxNone, "ls")

	tests := []struct {
		name       string
		reg        *Registry
		agent      string
		tool       *Tool
		wantName   string
		wantSource SandboxSource
		wantNil    bool
		wantErr    error
	}{
		{"tool sandbox wins", base("ws-sb", true), "with-sb", toolWithSB, "tool-sb", SandboxSourceTool, false, nil},
		{"agent sandbox", base("ws-sb", true), "with-sb", toolPlain, "agent-sb", SandboxSourceAgent, false, nil},
		{"workspace default", base("ws-sb", true), "plain", toolPlain, "ws-sb", SandboxSourceWorkspace, false, nil},
		{"system default", base("", true), "plain", toolPlain, "default", SandboxSourceSystem, false, nil},
		{"no default registered", base("", false), "plain", toolPlain, "", "", false, ErrNoDefaultSandbox},
		{"agent none", base("ws-sb", true), "unsandboxed", toolPlain, "", "", true, nil},
		{"tool none", base("ws-sb", true), "with-sb", toolNone, "", "", true, nil},
		{"tool sandbox without defaults", base("", false), "plain", toolWithSB, "tool-sb", SandboxSourceTool, false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ra, err := tc.reg.ResolveAgent(tc.agent)
			if err != nil {
				t.Fatalf("ResolveAgent: %v", err)
			}
			rs, err := ra.SandboxForTool(tc.tool)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SandboxForTool: %v", err)
			}
			if tc.wantNil {
				if rs != nil {
					t.Fatalf("expected no sandbox, got %s", rs.Sandbox.GetName())
				}
				return
			}
			if rs == nil || rs.Sandbox.GetName() != tc.wantName || rs.Source != tc.wantSource {
				t.Fatalf("got %+v, want %s from %s", rs, tc.wantName, tc.wantSource)
			}
			if wantExplicit := tc.wantSource != SandboxSourceSystem; rs.Explicit != wantExplicit {
				t.Errorf("Explicit = %v, want %v", rs.Explicit, wantExplicit)
			}
		})
	}

	t.Run("agent-level Sandbox", func(t *testing.T) {
		ra, _ := base("ws-sb", true).ResolveAgent("with-sb")
		rs, err := ra.Sandbox()
		if err != nil || rs.Sandbox.GetName() != "agent-sb" {
			t.Fatalf("Sandbox() = %v, %v", rs, err)
		}
	})

	t.Run("extends merges agent sandbox as replaced scalar", func(t *testing.T) {
		child := newTestAgent(NamespaceWorkspace, "child", "")
		child.Spec.Extends = "with-sb"
		override := newTestAgent(NamespaceWorkspace, "override", "ws-sb")
		override.Spec.Extends = "with-sb"
		reg := base("", true, child, override)
		for name, want := range map[string]string{"child": "agent-sb", "override": "ws-sb"} {
			ra, err := reg.ResolveAgent(name)
			if err != nil {
				t.Fatal(err)
			}
			if ra.Agent.Spec.Sandbox != want {
				t.Errorf("%s: merged sandbox = %q, want %q", name, ra.Agent.Spec.Sandbox, want)
			}
		}
	})

	t.Run("required rejects none and missing default", func(t *testing.T) {
		reg := newSandboxTestRegistry(&WorkspaceDefSpec{Policies: &Policies{Sandbox: &SandboxPolicies{Required: true}}},
			newTestAgent(NamespaceWorkspace, "unsandboxed", SandboxNone),
			newTestAgent(NamespaceWorkspace, "plain", ""),
		)
		ra, _ := reg.ResolveAgent("unsandboxed")
		if _, err := ra.SandboxForTool(toolPlain); err == nil || !strings.Contains(err.Error(), "required") {
			t.Errorf("expected required error for none, got %v", err)
		}
		ra, _ = reg.ResolveAgent("plain")
		rs, err := ra.SandboxForTool(toolPlain)
		if !errors.Is(err, ErrNoDefaultSandbox) || rs != nil || !strings.Contains(err.Error(), "required") {
			t.Errorf("expected fatal ErrNoDefaultSandbox, got %v", err)
		}
	})

	t.Run("required makes the implicit default explicit", func(t *testing.T) {
		reg := newSandboxTestRegistry(&WorkspaceDefSpec{Policies: &Policies{Sandbox: &SandboxPolicies{Required: true}}},
			newTestAgent(NamespaceWorkspace, "plain", ""),
			newTestSandbox(NamespaceSystem, "default", SandboxSpec{}),
		)
		ra, _ := reg.ResolveAgent("plain")
		rs, err := ra.Sandbox()
		if err != nil || !rs.Explicit || rs.Source != SandboxSourceSystem {
			t.Errorf("expected explicit system default, got %+v, %v", rs, err)
		}
	})

	t.Run("disabled and missing references fail closed", func(t *testing.T) {
		disabled := newTestSandbox(NamespaceWorkspace, "off", SandboxSpec{Enabled: new(bool)})
		reg := base("", true, disabled, newTestAgent(NamespaceWorkspace, "bad", "off"), newTestAgent(NamespaceWorkspace, "missing", "nope"))
		for _, name := range []string{"bad", "missing"} {
			ra, _ := reg.ResolveAgent(name)
			if _, err := ra.Sandbox(); err == nil {
				t.Errorf("%s: expected error", name)
			}
		}
	})

	t.Run("MCP selection", func(t *testing.T) {
		reg := base("ws-sb", true)
		stdio := &MCP{BaseResource: sbBase(KindMCP, NamespaceWorkspace, "m1"), Spec: MCPSpec{Command: []string{"node"}, Sandbox: "mcp-sb"}}
		rs, err := reg.ResolveMCPSandbox(stdio)
		if err != nil || rs.Sandbox.GetName() != "mcp-sb" || rs.Source != SandboxSourceMCP {
			t.Errorf("mcp sandbox: %+v, %v", rs, err)
		}
		plain := &MCP{BaseResource: sbBase(KindMCP, NamespaceWorkspace, "m2"), Spec: MCPSpec{Command: []string{"node"}}}
		rs, err = reg.ResolveMCPSandbox(plain)
		if err != nil || rs.Sandbox.GetName() != "ws-sb" || rs.Source != SandboxSourceWorkspace {
			t.Errorf("mcp default: %+v, %v", rs, err)
		}
		remote := &MCP{BaseResource: sbBase(KindMCP, NamespaceWorkspace, "m3"), Spec: MCPSpec{Type: MCPTransportSSE, Endpoint: "https://x"}}
		if rs, err := reg.ResolveMCPSandbox(remote); rs != nil || err != nil {
			t.Errorf("remote MCP should have no sandbox: %+v, %v", rs, err)
		}
		// Agent sandboxes never apply to MCP servers.
		if rs, err := reg.Project("p").ResolveMCPSandbox(plain); err != nil || rs.Sandbox.GetName() != "ws-sb" {
			t.Errorf("scoped mcp: %+v, %v", rs, err)
		}
	})
}

// ─── Intersection ────────────────────────────────────────────────────────────

func TestSandbox_RuleIntersection(t *testing.T) {
	resolve := func(t *testing.T, ceiling *SandboxPolicies, agentSpec, toolSpec SandboxSpec) *ResolvedSandbox {
		t.Helper()
		reg := newSandboxTestRegistry(wsWithSandboxPolicy(ceiling),
			newTestFileSystem(NamespaceWorkspace, "work", FileSystemDriverMemory, nil),
			newTestSandbox(NamespaceWorkspace, "agent-sb", agentSpec),
			newTestSandbox(NamespaceWorkspace, "tool-sb", toolSpec),
			newTestAgent(NamespaceWorkspace, "agent", "agent-sb"),
		)
		ra, err := reg.ResolveAgent("agent")
		if err != nil {
			t.Fatal(err)
		}
		rs, err := ra.SandboxForTool(newTestTool(NamespaceWorkspace, "tool", "tool-sb", "curl"))
		if err != nil {
			t.Fatalf("SandboxForTool: %v", err)
		}
		return rs
	}
	open := &SandboxNetwork{Mode: SandboxNetworkOpen}

	t.Run("tool gets no network because agent has none", func(t *testing.T) {
		rs := resolve(t, nil, SandboxSpec{}, SandboxSpec{Network: open, Workdir: "/tool"})
		if rs.Sandbox.GetName() != "tool-sb" || rs.Workdir != "/tool" {
			t.Errorf("selection should come from the tool sandbox: %s %s", rs.Sandbox.GetName(), rs.Workdir)
		}
		if rs.Network.Mode != SandboxNetworkNone || rs.AllowsHost("example.com") {
			t.Errorf("expected no network, got %+v", rs.Network)
		}
	})

	t.Run("allowlists intersect", func(t *testing.T) {
		rs := resolve(t, nil,
			SandboxSpec{Network: &SandboxNetwork{Mode: SandboxNetworkAllowlist, Allow: []string{"api.github.com", "*.pypi.org"}}},
			SandboxSpec{Network: &SandboxNetwork{Mode: SandboxNetworkAllowlist, Allow: []string{"*.pypi.org", "registry.npmjs.org"}}})
		for host, want := range map[string]bool{"files.pypi.org": true, "api.github.com": false, "registry.npmjs.org": false} {
			if got := rs.AllowsHost(host); got != want {
				t.Errorf("AllowsHost(%s) = %v, want %v", host, got, want)
			}
		}
	})

	t.Run("ceiling deny wins over open layers", func(t *testing.T) {
		rs := resolve(t, &SandboxPolicies{Network: &SandboxNetwork{Deny: []string{"evil.com"}}}, SandboxSpec{Network: open}, SandboxSpec{Network: open})
		if rs.Network.Mode != SandboxNetworkOpen || rs.AllowsHost("evil.com") || !rs.AllowsHost("good.com") {
			t.Errorf("unexpected network: %+v", rs.Network)
		}
	})

	t.Run("ceiling allowlist without mode", func(t *testing.T) {
		rs := resolve(t, &SandboxPolicies{Network: &SandboxNetwork{Allow: []string{"good.com"}}}, SandboxSpec{Network: open}, SandboxSpec{Network: open})
		if rs.Network.Mode != SandboxNetworkAllowlist || !rs.AllowsHost("good.com") || rs.AllowsHost("other.com") {
			t.Errorf("unexpected network: %+v", rs.Network)
		}
	})

	t.Run("exec strictest mode and every allowlist", func(t *testing.T) {
		rs := resolve(t, nil, SandboxSpec{Exec: &SandboxExec{Mode: SandboxExecAllowlist, Allow: []string{"git", "ls"}}}, SandboxSpec{})
		if rs.Exec.Mode != SandboxExecAllowlist || !rs.AllowsCommand("git") || rs.AllowsCommand("rm") {
			t.Errorf("unexpected exec: %+v", rs.Exec)
		}
		rs = resolve(t, &SandboxPolicies{Exec: &SandboxExec{Mode: SandboxExecAllowlist, Allow: []string{"ls", "cat"}}},
			SandboxSpec{Exec: &SandboxExec{Mode: SandboxExecAllowlist, Allow: []string{"git", "ls"}}},
			SandboxSpec{Exec: &SandboxExec{Mode: SandboxExecAllowlist, Allow: []string{"ls", "git"}}})
		if !rs.AllowsCommand("ls") || rs.AllowsCommand("git") || rs.AllowsCommand("cat") {
			t.Errorf("command must be allowed by every allowlist: %+v", rs.Exec)
		}
		rs = resolve(t, nil, SandboxSpec{Exec: &SandboxExec{Mode: SandboxExecNone}}, SandboxSpec{Exec: &SandboxExec{Mode: SandboxExecAny}})
		if rs.Exec.Mode != SandboxExecNone || rs.AllowsCommand("ls") {
			t.Errorf("exec none must win: %+v", rs.Exec)
		}
	})

	t.Run("rootfs strictest", func(t *testing.T) {
		rw := &SandboxRootFS{Mode: SandboxRootFSReadWrite}
		rs := resolve(t, &SandboxPolicies{RootFS: &SandboxRootFS{Mode: SandboxRootFSReadOnly}}, SandboxSpec{RootFS: rw}, SandboxSpec{RootFS: rw})
		if rs.RootFS != SandboxRootFSReadOnly {
			t.Errorf("RootFS = %q, want ro", rs.RootFS)
		}
		rs = resolve(t, nil, SandboxSpec{}, SandboxSpec{RootFS: rw})
		if rs.RootFS != "" {
			t.Errorf("unset agent rootfs (driver default) must cap rw, got %q", rs.RootFS)
		}
		rs = resolve(t, nil, SandboxSpec{RootFS: rw}, SandboxSpec{RootFS: rw})
		if rs.RootFS != SandboxRootFSReadWrite {
			t.Errorf("RootFS = %q, want rw", rs.RootFS)
		}
	})

	t.Run("limits minimum per field", func(t *testing.T) {
		rs := resolve(t, &SandboxPolicies{Limits: &SandboxLimits{Memory: "4Gi", Timeout: "30m", Processes: 100}},
			SandboxSpec{Limits: &SandboxLimits{Memory: "2Gi", CPU: "2"}},
			SandboxSpec{Limits: &SandboxLimits{Memory: "1024Mi", CPU: "500m", Timeout: "1h", Processes: 10}})
		want := SandboxLimits{CPU: "500m", Memory: "1024Mi", Processes: 10, Timeout: "30m"}
		if rs.Limits != want {
			t.Errorf("Limits = %+v, want %+v", rs.Limits, want)
		}
	})

	t.Run("env passthrough intersected and set merged", func(t *testing.T) {
		rs := resolve(t, &SandboxPolicies{Env: &SandboxEnv{Passthrough: []string{"PATH", "LANG"}}},
			SandboxSpec{Env: &SandboxEnv{Passthrough: []string{"PATH", "HOME", "LANG"}, Set: map[string]string{"A": "agent", "B": "b"}}},
			SandboxSpec{Env: &SandboxEnv{Passthrough: []string{"P*"}, Set: map[string]string{"A": "tool"}}})
		if !rs.AllowsEnv("PATH") || rs.AllowsEnv("LANG") || rs.AllowsEnv("HOME") {
			t.Errorf("unexpected passthrough: %+v", rs.Env.Passthrough)
		}
		if rs.Env.Set["A"] != "tool" || rs.Env.Set["B"] != "b" || !rs.Env.Declared {
			t.Errorf("unexpected env.set: %+v", rs.Env)
		}
		rs = resolve(t, nil, SandboxSpec{}, SandboxSpec{Env: &SandboxEnv{Passthrough: []string{"PATH"}}})
		if rs.AllowsEnv("PATH") {
			t.Error("agent sandbox without env passes no host variables")
		}
	})

	t.Run("access every layer and deny wins", func(t *testing.T) {
		mount := []SandboxMount{{FileSystem: "work", Path: "/work", Mode: SandboxMountReadWrite}}
		rs := resolve(t, &SandboxPolicies{Access: &SandboxAccess{Deny: []string{"/work/**/*.pem"}}},
			SandboxSpec{Mounts: mount, Access: &SandboxAccess{Deny: []string{"secret/**"}}},
			SandboxSpec{Mounts: mount, Access: &SandboxAccess{Write: []string{"out/**"}}})
		tests := []struct {
			p    string
			op   SandboxAccessOp
			want bool
		}{
			{"/work/out/x", SandboxAccessWrite, true},
			{"out/y", SandboxAccessWrite, true},
			{"/work/secret/a", SandboxAccessRead, false},
			{"/work/other", SandboxAccessWrite, false},
			{"/work/other", SandboxAccessRead, true},
			{"/work/out/key.pem", SandboxAccessRead, false},
		}
		for _, tc := range tests {
			if got := rs.AllowsPath(tc.p, tc.op); got != tc.want {
				t.Errorf("AllowsPath(%s, %s) = %v, want %v", tc.p, tc.op, got, tc.want)
			}
		}
		if len(rs.Access) != 3 {
			t.Errorf("expected 3 access layers, got %d", len(rs.Access))
		}
	})

	t.Run("same sandbox for agent and tool counts once", func(t *testing.T) {
		reg := newSandboxTestRegistry(&WorkspaceDefSpec{},
			newTestSandbox(NamespaceWorkspace, "sb", SandboxSpec{Access: &SandboxAccess{Deny: []string{"/x"}}}),
			newTestAgent(NamespaceWorkspace, "agent", "sb"),
		)
		ra, _ := reg.ResolveAgent("agent")
		rs, err := ra.SandboxForTool(newTestTool(NamespaceWorkspace, "tool", "sb", "ls"))
		if err != nil || len(rs.Access) != 1 {
			t.Errorf("expected a single layer, got %+v, %v", rs, err)
		}
	})
}

func TestSandbox_SubagentDelegation(t *testing.T) {
	parent := newTestAgent(NamespaceWorkspace, "parent", "p-sb")
	parent.Spec.Subagents = []string{"child", "free"}
	reg := newSandboxTestRegistry(&WorkspaceDefSpec{},
		newTestSandbox(NamespaceWorkspace, "p-sb", SandboxSpec{
			Network: &SandboxNetwork{Mode: SandboxNetworkAllowlist, Allow: []string{"a.com", "b.com"}},
			Limits:  &SandboxLimits{Timeout: "10m"},
		}),
		newTestSandbox(NamespaceWorkspace, "c-sb", SandboxSpec{
			Network: &SandboxNetwork{Mode: SandboxNetworkAllowlist, Allow: []string{"a.com", "c.com"}},
			Workdir: "/child",
		}),
		parent,
		newTestAgent(NamespaceWorkspace, "child", "c-sb"),
		newTestAgent(NamespaceWorkspace, "free", SandboxNone),
	)
	ra, err := reg.ResolveAgent("parent")
	if err != nil {
		t.Fatal(err)
	}
	child, err := ra.ResolveSubagent(reg, "child")
	if err != nil {
		t.Fatalf("ResolveSubagent: %v", err)
	}
	if child.ParentSandbox == nil || child.ParentSandbox.Sandbox.GetName() != "p-sb" {
		t.Fatalf("ParentSandbox not set: %+v", child.ParentSandbox)
	}
	rs, err := child.Sandbox()
	if err != nil {
		t.Fatal(err)
	}
	if rs.Sandbox.GetName() != "c-sb" || rs.Workdir != "/child" {
		t.Errorf("selection must stay with the child: %s %s", rs.Sandbox.GetName(), rs.Workdir)
	}
	if !rs.AllowsHost("a.com") || rs.AllowsHost("b.com") || rs.AllowsHost("c.com") || rs.Limits.Timeout != "10m" {
		t.Errorf("delegation widened access: %+v %+v", rs.Network, rs.Limits)
	}

	free, err := ra.ResolveSubagent(reg, "free")
	if err != nil {
		t.Fatal(err)
	}
	rs, err = free.SandboxForTool(newTestTool(NamespaceWorkspace, "t", "", "ls"))
	if err != nil || rs == nil || rs.Source != SandboxSourceDelegator || rs.AllowsHost("c.com") {
		t.Errorf("a subagent selecting none must keep the delegator's sandbox: %+v, %v", rs, err)
	}

	// Intersect directly.
	a := &ResolvedSandbox{Exec: ResolvedSandboxExec{Mode: SandboxExecAny}, Network: ResolvedSandboxNetwork{Mode: SandboxNetworkOpen}, RootFS: SandboxRootFSReadWrite}
	b := &ResolvedSandbox{Exec: ResolvedSandboxExec{Mode: SandboxExecAllowlist, Allowlists: [][]string{{"ls"}}}, Network: ResolvedSandboxNetwork{Mode: SandboxNetworkOpen, Deny: []string{"x.com"}}, RootFS: SandboxRootFSReadOnly}
	c := a.Intersect(b)
	if c.RootFS != SandboxRootFSReadOnly || c.AllowsCommand("git") || !c.AllowsCommand("ls") || c.AllowsHost("x.com") {
		t.Errorf("Intersect: %+v", c)
	}
	if a.Exec.Mode != SandboxExecAny {
		t.Error("Intersect must not mutate the receiver")
	}
}

// ─── Enforcement helpers ─────────────────────────────────────────────────────

func TestResolvedSandbox_AllowsPath(t *testing.T) {
	fs := func(name string) *FileSystem {
		return newTestFileSystem(NamespaceWorkspace, name, FileSystemDriverMemory, nil)
	}
	analyst := &ResolvedSandbox{
		RootFS:  SandboxRootFSNone,
		Workdir: "/reports",
		Mounts: []ResolvedSandboxMount{
			{FileSystem: fs("reports"), Path: "/reports", Mode: SandboxMountReadWrite},
			{FileSystem: fs("scratch"), Path: "/tmp", Mode: SandboxMountReadWrite},
			{FileSystem: fs("data"), Path: "/data", Mode: SandboxMountReadOnly},
		},
		Access: []SandboxAccess{absolutizeAccess(&SandboxAccess{Write: []string{"drafts/**", "/tmp/**"}, Deny: []string{"**/payroll/**"}}, "/reports")},
	}
	readList := &ResolvedSandbox{
		RootFS:  SandboxRootFSReadWrite,
		Workdir: "/",
		Access:  []SandboxAccess{{Read: []string{"/src/**"}, Write: []string{"/src/out/**", "/b/**"}}},
	}
	driverDefault := &ResolvedSandbox{Workdir: "/"}

	tests := []struct {
		name string
		rs   *ResolvedSandbox
		p    string
		op   SandboxAccessOp
		want bool
	}{
		{"relative write in write list", analyst, "drafts/a.md", SandboxAccessWrite, true},
		{"write outside write list", analyst, "/reports/final.md", SandboxAccessWrite, false},
		{"read unrestricted when only write set", analyst, "/reports/final.md", SandboxAccessRead, true},
		{"deny under workdir", analyst, "/reports/2024/payroll/q1.csv", SandboxAccessRead, false},
		{"deny is relative to workdir", analyst, "/tmp/payroll/x", SandboxAccessWrite, true},
		{"ro mount blocks write", analyst, "/data/file", SandboxAccessWrite, false},
		{"ro mount read", analyst, "/data/file", SandboxAccessRead, true},
		{"rootfs none hides host", analyst, "/etc/passwd", SandboxAccessRead, false},
		{"rootfs none root listing", analyst, "/", SandboxAccessRead, true},
		{"rootfs none no write", analyst, "/newfile", SandboxAccessWrite, false},
		{"scratch write", analyst, "/tmp/a", SandboxAccessWrite, true},
		{"dotdot cleaned", analyst, "drafts/../final.md", SandboxAccessWrite, false},
		{"invalid op", analyst, "/reports/a", SandboxAccessOp("exec"), false},
		{"read list allows", readList, "/src/a.go", SandboxAccessRead, true},
		{"read list blocks", readList, "/etc/hosts", SandboxAccessRead, false},
		{"write implies read", readList, "/b/x", SandboxAccessRead, true},
		{"write list blocks", readList, "/src/a.go", SandboxAccessWrite, false},
		{"write list allows", readList, "/src/out/bin", SandboxAccessWrite, true},
		{"driver default root read", driverDefault, "/etc/hosts", SandboxAccessRead, true},
		{"driver default root treated ro", driverDefault, "/etc/hosts", SandboxAccessWrite, false},
		{"nil sandbox allows", nil, "/anything", SandboxAccessWrite, true},
	}
	for _, tc := range tests {
		if got := tc.rs.AllowsPath(tc.p, tc.op); got != tc.want {
			t.Errorf("%s: AllowsPath(%q, %s) = %v, want %v", tc.name, tc.p, tc.op, got, tc.want)
		}
	}

	withDefaults := driverDefault.ApplyDriverDefaults(DriverInfo{Capabilities: []string{SandboxCapabilityRootFSPrivate}})
	if withDefaults.RootFS != SandboxRootFSReadWrite || !withDefaults.AllowsPath("/etc/x", SandboxAccessWrite) || driverDefault.RootFS != "" {
		t.Errorf("ApplyDriverDefaults: %+v", withDefaults)
	}
}

func TestResolvedSandbox_AllowsCommand(t *testing.T) {
	rs := &ResolvedSandbox{Exec: ResolvedSandboxExec{Mode: SandboxExecAllowlist, Allowlists: [][]string{{"bash", "ls", "/usr/bin/python3"}}}}
	tests := map[string]bool{
		"ls": true, "/bin/ls": true, "/usr/bin/python3": true, "python3": false, "/opt/python3": false, "rm": false, "": false,
	}
	for name, want := range tests {
		if got := rs.AllowsCommand(name); got != want {
			t.Errorf("AllowsCommand(%q) = %v, want %v", name, got, want)
		}
	}
	two := &ResolvedSandbox{Exec: ResolvedSandboxExec{Mode: SandboxExecAllowlist, Allowlists: [][]string{{"bash", "ls"}, {"ls"}}}}
	if two.AllowsCommand("bash") || !two.AllowsCommand("ls") {
		t.Error("command must be in every allowlist")
	}
	if (&ResolvedSandbox{Exec: ResolvedSandboxExec{Mode: SandboxExecNone}}).AllowsCommand("ls") {
		t.Error("exec none must block")
	}
	if !(&ResolvedSandbox{Exec: ResolvedSandboxExec{Mode: SandboxExecAny}}).AllowsCommand("anything") {
		t.Error("exec any must allow")
	}
}

func TestResolvedSandbox_AllowsHost(t *testing.T) {
	allow := &ResolvedSandbox{Network: ResolvedSandboxNetwork{
		Mode:       SandboxNetworkAllowlist,
		Allowlists: [][]string{{"api.github.com", "*.pypi.org", "registry.npmjs.org:443"}},
		Deny:       []string{"bad.pypi.org"},
	}}
	tests := map[string]bool{
		"api.github.com":         true,
		"API.GitHub.com":         true,
		"api.github.com:443":     true,
		"files.pypi.org":         true,
		"a.b.pypi.org":           true,
		"pypi.org":               false,
		"bad.pypi.org":           false,
		"registry.npmjs.org:443": true,
		"registry.npmjs.org:80":  false,
		"registry.npmjs.org":     false,
		"github.com":             false,
		"":                       false,
	}
	for host, want := range tests {
		if got := allow.AllowsHost(host); got != want {
			t.Errorf("allowlist AllowsHost(%q) = %v, want %v", host, got, want)
		}
	}
	open := &ResolvedSandbox{Network: ResolvedSandboxNetwork{Mode: SandboxNetworkOpen, Deny: []string{"*.evil.com:443", "[::1]"}}}
	for host, want := range map[string]bool{"x.evil.com": false, "x.evil.com:443": false, "x.evil.com:80": true, "good.com": true, "[::1]:8080": false} {
		if got := open.AllowsHost(host); got != want {
			t.Errorf("open AllowsHost(%q) = %v, want %v", host, got, want)
		}
	}
	if (&ResolvedSandbox{Network: ResolvedSandboxNetwork{Mode: SandboxNetworkNone}}).AllowsHost("good.com") {
		t.Error("network none must block")
	}
}

func TestSandbox_WorkdirDefaults(t *testing.T) {
	resources := append(sandboxBaseResources(),
		newTestFileSystem(NamespaceWorkspace, "data", FileSystemDriverMemory, nil),
		newTestFileSystem(NamespaceWorkspace, "more", FileSystemDriverMemory, nil),
		newTestSandbox(NamespaceWorkspace, "declared", SandboxSpec{Workdir: "/explicit/", Mounts: []SandboxMount{{FileSystem: "data", Path: "/data"}}}),
		newTestSandbox(NamespaceWorkspace, "with-cwd", SandboxSpec{Mounts: []SandboxMount{{FileSystem: "data", Path: "/data"}, {FileSystem: "cwd", Path: "/project"}}}),
		newTestSandbox(NamespaceWorkspace, "single", SandboxSpec{Mounts: []SandboxMount{{FileSystem: "data", Path: "/data"}}}),
		newTestSandbox(NamespaceWorkspace, "multi", SandboxSpec{Mounts: []SandboxMount{{FileSystem: "data", Path: "/data"}, {FileSystem: "more", Path: "/more"}}}),
		newTestSandbox(NamespaceWorkspace, "empty", SandboxSpec{}),
		newTestSandbox(NamespaceWorkspace, "inherits", SandboxSpec{Extends: "declared", Mounts: []SandboxMount{{FileSystem: "cwd", Path: "/project"}}}),
	)
	reg := newSandboxTestRegistry(&WorkspaceDefSpec{}, resources...)
	for ref, want := range map[string]string{
		"declared": "/explicit",
		"with-cwd": "/project",
		"single":   "/data",
		"multi":    "/",
		"empty":    "/",
		"inherits": "/explicit",
		"default":  "/ws",
	} {
		rs, err := reg.ResolveSandbox(ref)
		if err != nil {
			t.Fatalf("ResolveSandbox(%s): %v", ref, err)
		}
		if rs.Workdir != want {
			t.Errorf("%s: Workdir = %q, want %q", ref, rs.Workdir, want)
		}
	}
}

// ─── Capabilities ────────────────────────────────────────────────────────────

func TestResolvedSandbox_Capabilities(t *testing.T) {
	rs := &ResolvedSandbox{
		Driver:  "container",
		RootFS:  SandboxRootFSReadOnly,
		Mounts:  []ResolvedSandboxMount{{Path: "/workspace", HostPath: "/ws/src"}, {Path: "/tmp"}},
		Access:  []SandboxAccess{{Deny: []string{"/x"}}},
		Exec:    ResolvedSandboxExec{Mode: SandboxExecAllowlist, Allowlists: [][]string{{"ls"}}},
		Network: ResolvedSandboxNetwork{Mode: SandboxNetworkAllowlist},
		Env:     ResolvedSandboxEnv{Declared: true},
		Limits:  SandboxLimits{CPU: "1", Memory: "1Gi", Processes: 4, Timeout: "1m"},
	}
	want := []string{
		SandboxCapabilityExec, SandboxCapabilityExecAllowlist, SandboxCapabilityRootFSReadOnly, SandboxCapabilityMounts,
		SandboxCapabilityMountsRemap, SandboxCapabilityAccessRules, SandboxCapabilityNetworkAllowlist, SandboxCapabilityEnvIsolation,
		SandboxCapabilityLimitsCPU, SandboxCapabilityLimitsMemory, SandboxCapabilityLimitsProcesses, SandboxCapabilityLimitsTimeout,
	}
	if got := rs.RequiredCapabilities(true); !slices.Equal(got, want) {
		t.Errorf("RequiredCapabilities = %v, want %v", got, want)
	}
	if err := rs.CheckCapabilities(DriverInfo{Capabilities: want}, true); err != nil {
		t.Errorf("CheckCapabilities with all capabilities: %v", err)
	}
	err := rs.CheckCapabilities(DriverInfo{Capabilities: []string{SandboxCapabilityExec}}, true)
	if err == nil || !strings.Contains(err.Error(), SandboxCapabilityMountsRemap) || strings.Contains(err.Error(), "exec,") {
		t.Errorf("expected missing capabilities error, got %v", err)
	}

	minimal := &ResolvedSandbox{Network: ResolvedSandboxNetwork{Mode: SandboxNetworkNone}, Exec: ResolvedSandboxExec{Mode: SandboxExecAny}}
	if got := minimal.RequiredCapabilities(false); !slices.Equal(got, []string{SandboxCapabilityNetworkNone}) {
		t.Errorf("minimal RequiredCapabilities = %v", got)
	}
	none := &ResolvedSandbox{RootFS: SandboxRootFSNone, Network: ResolvedSandboxNetwork{Mode: SandboxNetworkOpen, Deny: []string{"x"}}}
	if got := none.RequiredCapabilities(false); !slices.Equal(got, []string{SandboxCapabilityRootFSNone, SandboxCapabilityNetworkAllowlist}) {
		t.Errorf("rootfs none RequiredCapabilities = %v", got)
	}
	rw := &ResolvedSandbox{RootFS: SandboxRootFSReadWrite, Network: ResolvedSandboxNetwork{Mode: SandboxNetworkOpen}}
	if got := rw.RequiredCapabilities(false); !slices.Equal(got, []string{SandboxCapabilityRootFSPrivate}) {
		t.Errorf("rootfs rw RequiredCapabilities = %v", got)
	}
}

// ─── Validation ──────────────────────────────────────────────────────────────

func TestSandbox_Validation(t *testing.T) {
	falseVal := false
	ro := func(f *FileSystem) *FileSystem { f.Spec.ReadOnly = true; return f }
	withPersistence := func(f *FileSystem, p FileSystemPersistence) *FileSystem { f.Spec.Persistence = p; return f }
	openNet := &SandboxNetwork{Mode: SandboxNetworkOpen}

	tests := []struct {
		name      string
		ws        *WorkspaceDefSpec
		resources []Resource
		opts      *ValidateOptions
		wantErrs  []string
		wantWarn  string
	}{
		{
			name: "valid configuration",
			ws:   &WorkspaceDefSpec{DefaultSandbox: "dev", Policies: &Policies{Sandbox: &SandboxPolicies{Required: true, HostWrite: SandboxHostWriteDeny, Drivers: &ResourceFilter{Exclude: []string{"local"}}}}},
			resources: []Resource{
				newTestSandboxProvider(NamespaceWorkspace, "docker", SandboxDriverContainer),
				newTestFileSystem(NamespaceWorkspace, "src", FileSystemDriverHost, map[string]any{"path": "$WorkspaceDir/src"}),
				newTestFileSystem(NamespaceWorkspace, "src-overlay", FileSystemDriverOverlay, map[string]any{"source": "src"}),
				newTestSandbox(NamespaceWorkspace, "dev", SandboxSpec{
					Provider: "docker",
					Mounts:   []SandboxMount{{FileSystem: "src-overlay", Path: "/workspace", Mode: SandboxMountReadWrite}},
					Workdir:  "/workspace",
					Access:   &SandboxAccess{Deny: []string{"**/.env"}},
					Network:  &SandboxNetwork{Mode: SandboxNetworkAllowlist, Allow: []string{"proxy.golang.org"}},
					Env:      &SandboxEnv{Set: map[string]string{"GITHUB_TOKEN": "${env:GITHUB_TOKEN}", "MODE": "ci"}},
				}),
				newTestAgent(NamespaceWorkspace, "coder", "dev", "build"),
				newTestTool(NamespaceWorkspace, "build", "", "go", "build"),
			},
		},
		{
			name:      "provider driver missing and unknown",
			resources: []Resource{newTestSandboxProvider(NamespaceWorkspace, "p1", ""), newTestSandboxProvider(NamespaceWorkspace, "p2", "docker"), newTestSandboxProvider(NamespaceWorkspace, "p3", "acme/sandbox")},
			wantErrs:  []string{"spec.driver is required", `unknown sandbox driver "docker"`, `invalid sandbox driver "acme/sandbox"`},
		},
		{
			name:      "driver excluded by policy",
			ws:        wsWithSandboxPolicy(&SandboxPolicies{Drivers: &ResourceFilter{Exclude: []string{"local"}, Include: []string{"local", "acme.com/*"}}}),
			resources: []Resource{newTestSandboxProvider(NamespaceWorkspace, "p", "local"), newTestFileSystem(NamespaceWorkspace, "f", FileSystemDriverMemory, nil)},
			wantErrs:  []string{`sandbox provider workspace/SandboxProvider/p: driver "local" is not allowed`, `filesystem workspace/FileSystem/f: driver "memory" is not allowed`},
		},
		{
			name:      "strict unknown driver",
			resources: []Resource{newTestSandboxProvider(NamespaceWorkspace, "p", "e2b.dev/sandbox"), newTestSandboxProvider(NamespaceWorkspace, "q", "container")},
			opts:      &ValidateOptions{StrictDrivers: true, SandboxDrivers: map[string]DriverInfo{"container": {}}},
			wantErrs:  []string{`sandbox driver "e2b.dev/sandbox" is not registered`},
		},
		{
			name:      "non-strict unknown qualified driver warns",
			resources: []Resource{newTestSandboxProvider(NamespaceWorkspace, "p", "e2b.dev/sandbox"), newTestFileSystem(NamespaceWorkspace, "f", "acme.com/s3fs", nil)},
			wantWarn:  `sandbox driver "e2b.dev/sandbox" is not registered`,
		},
		{
			name: "driver rejects config and plaintext auth",
			resources: []Resource{func() Resource {
				p := newTestSandboxProvider(NamespaceWorkspace, "p", "e2b.dev/sandbox")
				p.Spec.Config = map[string]any{"region": "mars"}
				p.Spec.Auth = &ProviderAuth{Type: AuthTypeBearer, Token: "plaintext"}
				return p
			}()},
			opts:     &ValidateOptions{SandboxDrivers: map[string]DriverInfo{"e2b.dev/sandbox": {ValidateConfig: func(c map[string]any) error { return fmt.Errorf("unknown region %v", c["region"]) }}}},
			wantErrs: []string{"rejected spec.config: unknown region mars", "auth.token"},
		},
		{
			name: "sandbox references",
			resources: []Resource{
				newTestSandbox(NamespaceWorkspace, "a", SandboxSpec{Extends: "missing", Provider: "nope", Mounts: []SandboxMount{{FileSystem: "ghost", Path: "/g"}}}),
				newTestSandbox(NamespaceWorkspace, "b", SandboxSpec{Provider: "off"}),
				&SandboxProvider{BaseResource: sbBase(KindSandboxProvider, NamespaceWorkspace, "off"), Spec: SandboxProviderSpec{Driver: "native", Enabled: &falseVal}},
			},
			wantErrs: []string{"extends missing sandbox: missing", "references missing sandbox provider: nope", "references missing filesystem: ghost", "references disabled sandbox provider"},
		},
		{
			name: "mount paths and modes",
			ws:   wsWithSandboxPolicy(&SandboxPolicies{HostWrite: SandboxHostWriteDeny}),
			resources: []Resource{
				newTestFileSystem(NamespaceWorkspace, "mem", FileSystemDriverMemory, nil),
				ro(newTestFileSystem(NamespaceWorkspace, "frozen", FileSystemDriverMemory, nil)),
				newTestFileSystem(NamespaceWorkspace, "src", FileSystemDriverHost, map[string]any{"path": "/ws/src"}),
				newTestSandbox(NamespaceWorkspace, "s", SandboxSpec{Mounts: []SandboxMount{
					{FileSystem: "mem", Path: "relative"},
					{FileSystem: "mem"},
					{FileSystem: "mem", Path: "/dup", Mode: "rwx"},
					{FileSystem: "frozen", Path: "/dup", Mode: SandboxMountReadWrite},
					{FileSystem: "src", Mode: SandboxMountReadWrite},
				}}),
			},
			wantErrs: []string{`mount path "relative" must be absolute`, "requires a path", `duplicate mount path "/dup"`, `invalid mount mode "rwx"`, "is readOnly and cannot be mounted rw", "hostWrite is deny"},
		},
		{
			name: "filesystem driver lacks write and sandbox driver lacks remap",
			resources: []Resource{
				newTestSandboxProvider(NamespaceWorkspace, "native", SandboxDriverNative),
				newTestFileSystem(NamespaceWorkspace, "bucket", "acme.com/s3fs", nil),
				newTestFileSystem(NamespaceWorkspace, "src", FileSystemDriverHost, map[string]any{"path": "/ws/src"}),
				newTestSandbox(NamespaceWorkspace, "s", SandboxSpec{Provider: "native", Network: openNet, Mounts: []SandboxMount{
					{FileSystem: "bucket", Path: "/bucket", Mode: SandboxMountReadWrite},
					{FileSystem: "src", Path: "/workspace"},
				}}),
			},
			opts: &ValidateOptions{
				SandboxDrivers:    map[string]DriverInfo{"native": {Capabilities: []string{SandboxCapabilityExec, SandboxCapabilityMounts}}},
				FileSystemDrivers: map[string]DriverInfo{"acme.com/s3fs": {Capabilities: []string{FileSystemCapabilityRename}}, "host": {Capabilities: []string{FileSystemCapabilityWrite}}},
			},
			wantErrs: []string{`driver "acme.com/s3fs" lacks the write capability`, `lacks the mounts.remap capability`},
		},
		{
			name: "workdir and modes",
			resources: []Resource{
				newTestFileSystem(NamespaceWorkspace, "mem", FileSystemDriverMemory, nil),
				newTestSandbox(NamespaceWorkspace, "rel", SandboxSpec{Workdir: "work"}),
				newTestSandbox(NamespaceWorkspace, "outside", SandboxSpec{Workdir: "/elsewhere", RootFS: &SandboxRootFS{Mode: SandboxRootFSNone}, Mounts: []SandboxMount{{FileSystem: "mem", Path: "/data"}}}),
				newTestSandbox(NamespaceWorkspace, "modes", SandboxSpec{
					RootFS:  &SandboxRootFS{Mode: "writable"},
					Exec:    &SandboxExec{Mode: "some", Allow: []string{"ls"}},
					Network: &SandboxNetwork{Mode: "wide", Allow: []string{"x.com"}},
					Access:  &SandboxAccess{Deny: []string{"/bad/["}},
				}),
				newTestSandbox(NamespaceWorkspace, "lists", SandboxSpec{Exec: &SandboxExec{Allow: []string{"ls"}}, Network: &SandboxNetwork{Allow: []string{"x.com"}}}),
			},
			wantErrs: []string{`workdir "work" must be absolute`, "is outside every mount while rootfs.mode is none", `invalid rootfs.mode "writable"`, `invalid exec.mode "some"`, `invalid network.mode "wide"`, "exec.allow is set but exec.mode is not allowlist", "network.allow is set but network.mode is not allowlist", `invalid access.deny glob "/bad/["`},
		},
		{
			name: "capabilities and env.set expressions",
			resources: []Resource{
				newTestSandboxProvider(NamespaceWorkspace, "native", SandboxDriverNative),
				newTestSandbox(NamespaceWorkspace, "s", SandboxSpec{
					Provider: "native",
					RootFS:   &SandboxRootFS{Mode: SandboxRootFSNone},
					Network:  &SandboxNetwork{Mode: SandboxNetworkAllowlist, Allow: []string{"x.com"}},
					Env:      &SandboxEnv{Set: map[string]string{"BAD": "${broken"}},
				}),
			},
			opts:     &ValidateOptions{SandboxDrivers: map[string]DriverInfo{"native": {Capabilities: []string{SandboxCapabilityExec, SandboxCapabilityNetworkNone}}}},
			wantErrs: []string{"lacks capabilities required by the sandbox: rootfs.none, network.allowlist, env.isolation", "env.set.BAD"},
		},
		{
			name: "filesystem rules",
			ws:   wsWithSandboxPolicy(&SandboxPolicies{}),
			resources: []Resource{
				newTestFileSystem(NamespaceWorkspace, "badp", FileSystemDriverMemory, nil),
				withPersistence(newTestFileSystem(NamespaceWorkspace, "eph-host", FileSystemDriverHost, map[string]any{"path": "/ws/a"}), FileSystemPersistenceEphemeral),
				withPersistence(newTestFileSystem(NamespaceWorkspace, "persist-mem", FileSystemDriverMemory, nil), FileSystemPersistencePersistent),
				withPersistence(newTestFileSystem(NamespaceWorkspace, "weird", FileSystemDriverMemory, nil), "forever"),
				newTestFileSystem(NamespaceWorkspace, "nopath", FileSystemDriverHost, nil),
				newTestFileSystem(NamespaceWorkspace, "etc", FileSystemDriverHost, map[string]any{"path": "/etc"}),
				newTestFileSystem(NamespaceWorkspace, "home", FileSystemDriverHost, map[string]any{"path": "$HOME/x"}),
				newTestFileSystem(NamespaceWorkspace, "orphan", FileSystemDriverOverlay, map[string]any{"source": "missing"}),
				newTestFileSystem(NamespaceWorkspace, "loop1", FileSystemDriverOverlay, map[string]any{"source": "loop2"}),
				newTestFileSystem(NamespaceWorkspace, "loop2", FileSystemDriverOverlay, map[string]any{"source": "loop1"}),
				newTestFileSystem(NamespaceWorkspace, "repo", FileSystemDriverGit, map[string]any{"ref": "main"}),
			},
			wantErrs: []string{"host filesystems cannot be ephemeral", "memory filesystems cannot be persistent", `invalid persistence "forever"`, "host filesystems require a string config.path", `host path "/etc" is outside`, `config.path "$HOME/x" cannot be rendered`, "overlay config.source references missing filesystem: missing", "circular overlay sources", "git filesystems require a string config.url"},
		},
		{
			name: "workspace defaults and ceiling values",
			ws: &WorkspaceDefSpec{DefaultSandbox: "ghost", Policies: &Policies{Sandbox: &SandboxPolicies{
				HostWrite: "maybe",
				Exec:      &SandboxExec{Mode: "sometimes"},
				Env:       &SandboxEnv{Set: map[string]string{"A": "b"}},
				HostPaths: []string{""},
			}}},
			wantErrs: []string{"workspace defaultSandbox references missing sandbox: ghost", `invalid hostWrite "maybe"`, `invalid exec.mode "sometimes"`, "env.set is not allowed in the workspace ceiling", "invalid hostPaths glob"},
		},
		{
			name:     "workspace defaultSandbox none",
			ws:       &WorkspaceDefSpec{DefaultSandbox: SandboxNone},
			wantErrs: []string{`"none" is not allowed; spec.defaultSandbox must reference a Sandbox`},
		},
		{
			name: "required rejects none everywhere",
			ws:   wsWithSandboxPolicy(&SandboxPolicies{Required: true}),
			resources: []Resource{
				newTestAgent(NamespaceWorkspace, "a", SandboxNone),
				newTestTool(NamespaceWorkspace, "t", SandboxNone, "ls"),
				&MCP{BaseResource: sbBase(KindMCP, NamespaceWorkspace, "m"), Spec: MCPSpec{Command: []string{"node"}, Sandbox: SandboxNone}},
				newTestTool(NamespaceWorkspace, "implicit", "", "ls"),
			},
			wantErrs: []string{"agent workspace/Agent/a: sandbox \"none\" is not allowed", "tool workspace/Tool/t: sandbox \"none\" is not allowed", "mcp workspace/MCP/m: sandbox \"none\" is not allowed"},
			wantWarn: "workspace/Tool/implicit",
		},
		{
			name: "agent, tool, and mcp references",
			resources: []Resource{
				func() Resource {
					a := newTestAgent(NamespaceWorkspace, "a", "ghost")
					a.Spec.Policies = &Policies{Sandbox: &SandboxPolicies{}}
					return a
				}(),
				newTestTool(NamespaceWorkspace, "t", "ghost", "ls"),
				&MCP{BaseResource: sbBase(KindMCP, NamespaceWorkspace, "remote"), Spec: MCPSpec{Type: MCPTransportSSE, Endpoint: "https://x", Sandbox: "s"}},
				&MCP{BaseResource: sbBase(KindMCP, NamespaceWorkspace, "local"), Spec: MCPSpec{Command: []string{"node"}, Sandbox: "ghost"}},
				newTestSandbox(NamespaceWorkspace, "s", SandboxSpec{}),
			},
			wantErrs: []string{"agent workspace/Agent/a references missing sandbox: ghost", "policies.sandbox is only supported on the Workspace", "tool workspace/Tool/t references missing sandbox: ghost", "spec.sandbox is only valid for stdio transport", "mcp workspace/MCP/local references missing sandbox: ghost"},
		},
		{
			name: "tool executable in sandbox",
			ws:   &WorkspaceDefSpec{DefaultSandbox: "locked"},
			resources: []Resource{
				newTestSandboxProvider(NamespaceWorkspace, "virt", SandboxDriverVirtual),
				newTestSandbox(NamespaceWorkspace, "locked", SandboxSpec{Exec: &SandboxExec{Mode: SandboxExecNone}}),
				newTestSandbox(NamespaceWorkspace, "allow", SandboxSpec{Exec: &SandboxExec{Mode: SandboxExecAllowlist, Allow: []string{"ls"}}}),
				newTestSandbox(NamespaceWorkspace, "virtual", SandboxSpec{Provider: "virt"}),
				newTestTool(NamespaceWorkspace, "uses-default", "", "ls"),
				newTestTool(NamespaceWorkspace, "not-allowed", "allow", "rm", "-rf"),
				newTestTool(NamespaceWorkspace, "templated", "allow", "$ToolDir/run.sh"),
				newTestTool(NamespaceWorkspace, "not-offered", "virtual", "python"),
				newTestTool(NamespaceWorkspace, "via-agent", "", "curl"),
				newTestAgent(NamespaceWorkspace, "agent", "allow", "via-*"),
				newTestTool(NamespaceWorkspace, "builtin", ""),
			},
			opts:     &ValidateOptions{SandboxDrivers: map[string]DriverInfo{"virtual": {Capabilities: []string{SandboxCapabilityExec, SandboxCapabilityNetworkNone}, Commands: []string{"bash", "ls"}}}},
			wantErrs: []string{"tool workspace/Tool/uses-default: spec.command cannot run in sandbox workspace/Sandbox/locked: exec.mode is none", `tool workspace/Tool/not-allowed: command "rm" is not allowed by the exec allowlist`, `tool workspace/Tool/not-offered: command "python" is not offered by sandbox driver "virtual"`, `tool workspace/Tool/via-agent: command "curl" is not allowed by the exec allowlist of sandbox workspace/Sandbox/allow`},
		},
		{
			name: "driver lacking exec",
			resources: []Resource{
				newTestSandboxProvider(NamespaceWorkspace, "virt", SandboxDriverVirtual),
				newTestSandbox(NamespaceWorkspace, "virtual", SandboxSpec{Provider: "virt"}),
				newTestTool(NamespaceWorkspace, "t", "virtual", "ls"),
				&MCP{BaseResource: sbBase(KindMCP, NamespaceWorkspace, "m"), Spec: MCPSpec{Command: []string{"node"}, Sandbox: "virtual"}},
			},
			opts:     &ValidateOptions{SandboxDrivers: map[string]DriverInfo{"virtual": {Capabilities: []string{SandboxCapabilityNetworkNone}}}},
			wantErrs: []string{`tool workspace/Tool/t: spec.command cannot run in sandbox workspace/Sandbox/virtual: driver "virtual" lacks the exec capability`, `mcp workspace/MCP/m: server command cannot run in sandbox workspace/Sandbox/virtual: driver "virtual" lacks the exec capability`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ws := tc.ws
			if ws == nil {
				ws = &WorkspaceDefSpec{}
			}
			reg := newSandboxTestRegistry(ws, tc.resources...)
			err := reg.ValidateWithOptions(tc.opts)
			msgs := validationMessages(err)
			if len(tc.wantErrs) == 0 && err != nil {
				t.Fatalf("unexpected validation errors:\n%s", strings.Join(msgs, "\n"))
			}
			for _, want := range tc.wantErrs {
				if !containsSubstring(msgs, want) {
					t.Errorf("missing error containing %q; got:\n%s", want, strings.Join(msgs, "\n"))
				}
			}
			if tc.wantWarn != "" && !containsSubstring(reg.Warnings(), tc.wantWarn) {
				t.Errorf("missing warning containing %q; got %v", tc.wantWarn, reg.Warnings())
			}
		})
	}
}

func TestSandbox_ValidationDeterministic(t *testing.T) {
	build := func() *Registry {
		return newSandboxTestRegistry(&WorkspaceDefSpec{},
			newTestSandboxProvider(NamespaceWorkspace, "p1", "bad"),
			newTestSandboxProvider(NamespaceWorkspace, "p2", "worse"),
			newTestSandbox(NamespaceWorkspace, "s1", SandboxSpec{Provider: "nope", Workdir: "rel"}),
			newTestFileSystem(NamespaceWorkspace, "f1", FileSystemDriverHost, nil),
		)
	}
	first := validationMessages(build().Validate())
	for range 5 {
		if got := validationMessages(build().Validate()); !slices.Equal(got, first) {
			t.Fatalf("non-deterministic validation:\n%v\n%v", first, got)
		}
	}
	var ve *ValidationError
	if err := build().Validate(); !errors.As(err, &ve) || ve.Issues[0].Kind == "" || ve.Issues[0].Field == "" {
		t.Errorf("expected structured issues, got %v", err)
	}
}

func TestSandbox_ResolutionPolicyChecks(t *testing.T) {
	t.Run("hostWrite deny forces host mounts ro", func(t *testing.T) {
		reg := newSandboxTestRegistry(wsWithSandboxPolicy(&SandboxPolicies{HostWrite: SandboxHostWriteDeny}),
			newTestFileSystem(NamespaceWorkspace, "src", FileSystemDriverHost, map[string]any{"path": "/ws/src"}),
			newTestFileSystem(NamespaceWorkspace, "ov", FileSystemDriverOverlay, map[string]any{"source": "src"}),
			newTestSandbox(NamespaceWorkspace, "s", SandboxSpec{Mounts: []SandboxMount{
				{FileSystem: "src", Mode: SandboxMountReadWrite},
				{FileSystem: "ov", Path: "/work", Mode: SandboxMountReadWrite},
			}}),
		)
		rs, err := reg.ResolveSandbox("s")
		if err != nil {
			t.Fatal(err)
		}
		if rs.Mounts[0].Path != "/ws/src" || rs.Mounts[0].Mode != SandboxMountReadOnly {
			t.Errorf("host mount: %+v", rs.Mounts[0])
		}
		if rs.Mounts[1].Mode != SandboxMountReadWrite || rs.Mounts[1].HostPath != "/ws/src" {
			t.Errorf("overlay mount should stay rw: %+v", rs.Mounts[1])
		}
		if got := rs.RequiredCapabilities(false); !slices.Contains(got, SandboxCapabilityMountsRemap) {
			t.Errorf("overlay remapped to /work requires mounts.remap: %v", got)
		}
	})

	t.Run("hostPaths and drivers fail closed", func(t *testing.T) {
		reg := newSandboxTestRegistry(wsWithSandboxPolicy(&SandboxPolicies{Drivers: &ResourceFilter{Exclude: []string{"memory"}}}),
			newTestFileSystem(NamespaceWorkspace, "etc", FileSystemDriverHost, map[string]any{"path": "/etc"}),
			newTestFileSystem(NamespaceWorkspace, "mem", FileSystemDriverMemory, nil),
			newTestSandbox(NamespaceWorkspace, "outside", SandboxSpec{Mounts: []SandboxMount{{FileSystem: "etc"}}}),
			newTestSandbox(NamespaceWorkspace, "excluded", SandboxSpec{Mounts: []SandboxMount{{FileSystem: "mem", Path: "/m"}}}),
		)
		if _, err := reg.ResolveSandbox("outside"); err == nil || !strings.Contains(err.Error(), "hostPaths") {
			t.Errorf("expected hostPaths error, got %v", err)
		}
		if _, err := reg.ResolveSandbox("excluded"); err == nil || !strings.Contains(err.Error(), "drivers") {
			t.Errorf("expected drivers error, got %v", err)
		}
	})

	t.Run("readOnly filesystem forces ro", func(t *testing.T) {
		f := newTestFileSystem(NamespaceWorkspace, "mem", FileSystemDriverMemory, nil)
		f.Spec.ReadOnly = true
		reg := newSandboxTestRegistry(&WorkspaceDefSpec{}, f,
			newTestSandbox(NamespaceWorkspace, "s", SandboxSpec{Mounts: []SandboxMount{{FileSystem: "mem", Path: "/m", Mode: SandboxMountReadWrite}}}))
		rs, err := reg.ResolveSandbox("s")
		if err != nil || rs.Mounts[0].Mode != SandboxMountReadOnly || rs.AllowsPath("/m/x", SandboxAccessWrite) {
			t.Errorf("readOnly not enforced: %+v, %v", rs, err)
		}
	})

	t.Run("resolver without registry", func(t *testing.T) {
		ra := &ResolvedAgent{Agent: newTestAgent("", "a", "")}
		if _, err := ra.Sandbox(); err == nil {
			t.Error("expected error for agent without resolver")
		}
		if _, err := (&ResolvedAgent{}).SandboxForTool(nil); err == nil {
			t.Error("expected error for nil agent")
		}
	})
}

func TestSandbox_ReferenceFormsAndScopes(t *testing.T) {
	reg := newSandboxTestRegistry(&WorkspaceDefSpec{Projects: []string{"services/api", "services/web"}},
		newTestSandbox(NamespaceWorkspace, "dev", SandboxSpec{Workdir: "/ws-dev"}),
		newTestSandbox("services-api", "dev", SandboxSpec{Workdir: "/api-dev"}),
		newTestSandbox(NamespaceSystem, "readonly", SandboxSpec{Workdir: "/ro"}),
		newTestFileSystem("services-api", "src", FileSystemDriverHost, map[string]any{"path": "$ProjectDir"}),
		newTestSandbox("services-api", "proj", SandboxSpec{Mounts: []SandboxMount{{FileSystem: "src"}}}),
		newTestAgent(NamespaceWorkspace, "dev", ""), // same short name, different kind
	)
	reg.AddProject(&Project{Name: "services-api", Path: "services/api", RootPath: "/ws"})
	reg.AddProject(&Project{Name: "services-web", Path: "services/web", RootPath: "/ws"})

	tests := []struct {
		name     string
		resolver interface {
			ResolveSandbox(string) (*ResolvedSandbox, error)
		}
		ref     string
		workdir string
		wantErr bool
	}{
		{"short name at workspace root ignores other kinds", reg, "dev", "/ws-dev", false},
		{"short name prefers project scope", reg.Project("services-api"), "dev", "/api-dev", false},
		{"other project falls back to workspace", reg.Project("services-web"), "dev", "/ws-dev", false},
		{"local alias", reg.Project("services-api"), "local/Sandbox/dev", "/api-dev", false},
		{"local short alias", reg.Project("services-api"), "local/dev", "/api-dev", false},
		{"namespace-prefixed", reg, "system/readonly", "/ro", false},
		{"qualified", reg.Project("services-api"), "workspace/Sandbox/dev", "/ws-dev", false},
		{"file path", reg, "sandboxes/dev.yaml", "/ws-dev", false},
		{"namespace-prefixed miss does not fall back", reg, "system/dev", "", true},
		{"wrong kind in qualified name", reg, "workspace/Agent/dev", "", true},
		{"none is reserved", reg, SandboxNone, "", true},
		{"project-rendered host mount", reg.Project("services-api"), "proj", "/ws/services/api", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rs, err := tc.resolver.ResolveSandbox(tc.ref)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", rs)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveSandbox(%q): %v", tc.ref, err)
			}
			if rs.Workdir != tc.workdir {
				t.Errorf("Workdir = %q, want %q", rs.Workdir, tc.workdir)
			}
		})
	}
}

func TestSandbox_ReferenceSeparatorAndCasingValidation(t *testing.T) {
	base := func() *Registry {
		return newSandboxTestRegistry(&WorkspaceDefSpec{},
			newTestSandboxProvider(NamespaceSystem, "docker", SandboxDriverContainer),
			newTestFileSystem(NamespaceSystem, "src", FileSystemDriverHost, map[string]any{"path": "$WorkspaceDir"}),
			newTestSandbox(NamespaceSystem, "base", SandboxSpec{Provider: "docker", Workdir: "/base"}),
			newTestSandbox(NamespaceWorkspace, "sandboxes-dev", SandboxSpec{Provider: "docker", Workdir: "/dev"}),
		)
	}

	t.Run("valid qualified, namespace-prefixed, and relative file path refs pass", func(t *testing.T) {
		reg := base()
		reg.workspace.Def.Spec.DefaultSandbox = "sandboxes/dev.yaml"
		reg.set("workspace/Sandbox/child", newTestSandbox(NamespaceWorkspace, "child", SandboxSpec{
			Extends:  "system/Sandbox/base",
			Provider: "system/docker",
			Mounts:   []SandboxMount{{FileSystem: "system/FileSystem/src", Path: "/src"}},
		}))
		reg.set("workspace/FileSystem/ov", newTestFileSystem(NamespaceWorkspace, "ov", FileSystemDriverOverlay, map[string]any{"source": "system/src"}))
		reg.set("workspace/Agent/ag", newTestAgent(NamespaceWorkspace, "ag", "system/base"))
		reg.set("workspace/Tool/tl", newTestTool(NamespaceWorkspace, "tl", "system/Sandbox/base", "ls"))
		reg.set("workspace/MCP/mc", &MCP{
			BaseResource: sbBase(KindMCP, NamespaceWorkspace, "mc"),
			Spec:         MCPSpec{Type: MCPTransportStdio, Sandbox: "system/base", Command: []string{"srv"}},
		})
		if err := reg.Validate(); err != nil {
			t.Fatalf("expected valid sandbox references to pass, got: %v", err)
		}
	})

	cases := []struct {
		name  string
		setup func(reg *Registry)
		want  string
	}{
		{
			name: "colon in workspace.spec.defaultSandbox",
			setup: func(reg *Registry) {
				reg.workspace.Def.Spec.DefaultSandbox = "system:Sandbox/base"
			},
			want: "':' is not allowed",
		},
		{
			name: "lowercase kind in agent.spec.sandbox",
			setup: func(reg *Registry) {
				reg.set("workspace/Agent/a", newTestAgent(NamespaceWorkspace, "a", "system/sandbox/base"))
			},
			want: `unknown kind "sandbox"`,
		},
		{
			name: "uppercase namespace in tool.spec.sandbox",
			setup: func(reg *Registry) {
				reg.set("workspace/Tool/t", newTestTool(NamespaceWorkspace, "t", "System/base", "ls"))
			},
			want: "invalid namespace",
		},
		{
			name: "colon in mcp.spec.sandbox",
			setup: func(reg *Registry) {
				reg.set("workspace/MCP/m", &MCP{
					BaseResource: sbBase(KindMCP, NamespaceWorkspace, "m"),
					Spec:         MCPSpec{Type: MCPTransportStdio, Sandbox: "system:base", Command: []string{"srv"}},
				})
			},
			want: "':' is not allowed",
		},
		{
			name: "unknown kind in sandbox.spec.extends",
			setup: func(reg *Registry) {
				reg.set("workspace/Sandbox/s", newTestSandbox(NamespaceWorkspace, "s", SandboxSpec{Extends: "system/Unknown/base"}))
			},
			want: `unknown kind "Unknown"`,
		},
		{
			name: "lowercase kind in sandbox.spec.provider",
			setup: func(reg *Registry) {
				reg.set("workspace/Sandbox/s", newTestSandbox(NamespaceWorkspace, "s", SandboxSpec{Provider: "system/sandboxprovider/docker"}))
			},
			want: `unknown kind "sandboxprovider"`,
		},
		{
			name: "uppercase namespace in sandbox.spec.mounts[].filesystem",
			setup: func(reg *Registry) {
				reg.set("workspace/Sandbox/s", newTestSandbox(NamespaceWorkspace, "s", SandboxSpec{
					Provider: "docker",
					Mounts:   []SandboxMount{{FileSystem: "System/FileSystem/src", Path: "/src"}},
				}))
			},
			want: "invalid namespace",
		},
		{
			name: "colon in overlay filesystem config.source",
			setup: func(reg *Registry) {
				reg.set("workspace/FileSystem/ov", newTestFileSystem(NamespaceWorkspace, "ov", FileSystemDriverOverlay, map[string]any{"source": "system:src"}))
			},
			want: "':' is not allowed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := base()
			tc.setup(reg)
			err := reg.Validate()
			if err == nil {
				t.Fatalf("expected validation error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error containing %q, got: %v", tc.want, err)
			}
		})
	}
}
