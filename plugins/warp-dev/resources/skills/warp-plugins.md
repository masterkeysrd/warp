---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: warp-plugins
  description: Expertise in managing, authoring, and integrating WARP plugins.
---

# WARP Plugin Management

You are an expert on WARP's plugin architecture. When a user asks about plugins, rely on these core concepts:

## 1. Using Plugins
- Plugins are declared in `WORKSPACE.md` under `spec.plugins`.
- Recommend the user use `warp get <source>` (e.g., `warp get github.com/masterkeysrd/warp-stdlib`) to install plugins, as it safely handles interactive imports, locking, and setup hooks.
- Use the `-y` flag to auto-approve hooks and imports.

## 2. Remote vs Local
- **Remote Plugins**: URLs like `github.com/org/repo` are downloaded to the global cache (`~/.warp/pkg/mod/`) and locked via cryptographic hashes in `warp.lock`.
- **Local Plugins**: Paths like `./plugins/my-plugin` or `/absolute/path` bypass the global cache. They act as "live links," meaning local edits to the plugin immediately reflect in the workspace.

## 3. Creating Plugins
To turn a directory into a distributable plugin, place a `PLUGIN.md` manifest at the root:

```yaml
apiVersion: warp/v1alpha1
kind: Plugin
metadata:
  name: my-plugin
spec:
  resourceDir: "resources" # Defaults to .agents/ if omitted
  exports:
    - "*"
  hooks:
    postInstall:
      - ["echo", "Ready!"]
```

- **Hooks**: Use `spec.hooks.postInstall` to define arrays of commands (e.g., `["go", "install", "./..."]`) needed to set up dependencies.
- **Exports**: Use `spec.exports` (e.g., `["std-skills/*"]`) to dictate which resources are public.

## 4. Name Resolution
When a consumer imports a plugin, the resources are mounted under the namespace defined in their `WORKSPACE.md`. If omitted, the namespace defaults to the last path segment of the source (e.g., `github.com/org/finance` -> `finance`).
Agents must reference plugin resources using their fully qualified names (e.g., `finance/Skill/analysis`).
