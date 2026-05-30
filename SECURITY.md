# Security Policy

## Reporting a Vulnerability

We take the security of the Warp Open Agent Specification and its reference implementation seriously. If you discover a security vulnerability, please do not open a public issue. Instead, please report it privately to:

## Security Model

Warp is a declarative protocol for defining agents and resources. While the specification itself is a set of data structures, the **runtimes** that execute these resources bear the primary responsibility for security.

### Tool Execution Safety

The `Tool` resource allows agents to execute arbitrary commands on a host system. To maintain a secure environment, runtimes **should** implement the following safeguards:

1.  **Human-in-the-Loop (HITL):** Resources marked with `annotations.isDangerous: true` or `annotations.isOpenWorld: true` should require explicit user approval before execution.
2.  **Sandboxing:** Whenever possible, tools should be executed in restricted environments (e.g., Docker containers, gVisor, or low-privilege user accounts).
3.  **Path Sanitization:** Runtimes must sanitize any file paths provided as arguments to tools to prevent path traversal attacks.
4.  **Least Privilege:** Tools should only be granted the minimum permissions necessary to perform their specific task.

### Prompt & Instruction Injection

The `instructions` field in Warp resources becomes the system prompt for the LLM. Runtimes must be aware of the risk of prompt injection, where user input or tool outputs might contain malicious instructions designed to override the agent's core persona.

-   **Separation:** Runtimes should clearly separate system instructions from user-provided content in the LLM context window.
-   **Validation:** Use the `triggers` field to restrict which agents can be invoked by untrusted sources.

### Credential Management

**Never store secrets, API keys, or passwords directly in a WARP file.**

-   WARP files are designed to be committed to source control.
-   Use environment variable references (`spec.env` in Tools) and ensure the runtime manages the injection of these variables from a secure secret store (e.g., `.env` files, HashiCorp Vault, or AWS Secrets Manager).

## Secure Configuration Best Practices

-   **Use Qualified Names:** When extending agents or referencing skills, use qualified names (`namespace/Kind/name`) to ensure you are loading exactly the resource you expect.
-   **Regular Audits:** Periodically review the `tools` and `commands` assigned to agents to ensure they align with the principle of least privilege.
