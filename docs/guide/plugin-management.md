# Plugin Management in WARP

The Workspace Agent Resource Protocol (WARP) supports a robust plugin ecosystem, allowing developers to modularize, version, and distribute their Skills, Agents, Commands, and Tools across multiple workspaces.

This guide covers how plugins are structured, how to use them, and how the caching and resolution system works.

## Consuming Plugins

Plugins are integrated into your workspace by declaring them in the `WORKSPACE.md` file under the `spec.plugins` list. 

The easiest way to import a plugin is by using the CLI:
```bash
warp get github.com/masterkeysrd/warp-stdlib@latest
```

This command will:
1. Fetch the plugin from the remote repository.
2. Interactively ask you which resources you want to import.
3. Automatically update your `WORKSPACE.md` and generate a `warp.lock` file.
4. Execute any post-install setup hooks required by the plugin.

You can also bypass interactive prompts by using the `-y` flag:
```bash
warp get -y github.com/masterkeysrd/warp-stdlib
```

### Manual Configuration

If you prefer to configure it manually, add it to your `WORKSPACE.md`:
```yaml
spec:
  plugins:
    - source: github.com/masterkeysrd/warp-stdlib
      version: latest
      namespace: std
```
Once added, an agent defined in your workspace can reference tools or skills from that plugin via its qualified namespace:
```yaml
spec:
  skills:
    - "std/Skill/web-search"
```

## Creating a Plugin

Any Git repository or local directory can act as a WARP plugin by placing a `PLUGIN.md` manifest at its root.

```markdown
---
apiVersion: warp/v1alpha1
kind: Plugin
metadata:
  name: acme-tools
spec:
  resourceDir: "resources" # Directory where your agents/skills/tools live
  exports:
    - "*" # Expose everything
  hooks:
    postInstall:
      - ["go", "install", "./cmd/..."] # Optional setup commands
---

# Acme Tools Plugin
This plugin provides official internal tools for Acme Corporation developers.
```

## Remote Caching vs Local Linking

WARP's loader intelligently handles where your plugins come from to provide the best Developer Experience (DX).

### 1. Remote Plugins (Global Cache)
When you fetch a remote plugin (e.g., `github.com/...` or `https://...`), WARP downloads it and stores it securely in a global host cache (`~/.warp/pkg/mod/`).
* **Performance**: Subsequent workspace loads are nearly instantaneous because they read directly from disk.
* **Security**: The CLI generates a `warp.lock` file containing cryptographic hashes of the plugin tree. If the remote repository is tampered with, WARP will refuse to load it, preventing supply-chain attacks.

### 2. Local Plugins (Live Linking)
When developing a plugin, you often want to test it locally before publishing. WARP natively supports this via **Local Path Bypass**.
If you provide a relative path (`./my-plugin`) or an absolute path (`/Users/dev/my-plugin`) as the `source`, WARP treats it as a live dependency.
* It **bypasses the global cache** entirely.
* Any edits you make to the files inside `./my-plugin` are instantly reflected in your workspace without needing to run `warp get` again.

```yaml
spec:
  plugins:
    - source: ./plugins/my-local-plugin
```

## Setup Hooks

Some plugins provide advanced tools that require compiled binaries, python scripts, or node modules to run. A plugin author can define a `hooks.postInstall` array in their `PLUGIN.md`. 
When a consumer runs `warp get`, the CLI will prompt them for permission to execute these commands safely, ensuring the workspace environment is fully prepared.
