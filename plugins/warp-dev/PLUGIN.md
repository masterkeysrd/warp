---
apiVersion: warp/v1alpha1
kind: Plugin
metadata:
  name: warp-dev
  description: Official WARP development tools, skills, and agents for bootstrapping your workspace.
spec:
  resourceDir: "resources"
  exports:
    - "*"
  hooks:
    postInstall:
      - ["go", "install", "github.com/masterkeysrd/warp/cmd/warp@latest"]
---

# Official WARP Development Plugin

This plugin provides the official `warp-expert` agent, along with fundamental skills, tools, and commands required to build, test, and manage WARP ecosystems.
