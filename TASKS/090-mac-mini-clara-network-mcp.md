---
plan_recommended: true
---

# Mac Mini: Clara Installation + Network MCP

## Planning Context

This is a significant architectural uplift. The M4 Mac Mini (64GB RAM) is the LLM compute hub
for both my laptop and Alex's. Running full Clara on the Mac Mini with network-exposed MCP enables:
- Resource protection for Ollama
- Alex's laptop connecting to the mac mini's LLM server instead of direct Ollama HTTP
- My laptop connecting for high-memory model requests
- Potentially more services over time

Key questions to resolve during planning:

- **MCP transport protocol**
- **Authentication mechanism**
- **TLS**
- **What to expose over the network**
- **Clara config on the Mac Mini**
- **Client-side config**

## Context

The Mac Mini currently runs Ollama behind an nginx reverse proxy (open, no auth). The goal is to
replace that with a Clara-managed, authenticated network MCP endpoint that provides LLM access
with resource protection.

**Security posture**: The Mac Mini is on a private LAN. The Cisco VPN gives my laptop access when
remote. Alex's laptop is always on the same LAN. HTTPS with mkcert certs and bearer tokens is
sufficient; OAuth is not required for this use case.

## Architecture

### On the Mac Mini

1. Install Clara
2. Configure Clara with network MCP + `llm` server enabled
3. Start Clara as a macOS LaunchAgent
4. Expose only the `llm` MCP server initially

### Network MCP Server Implementation

Add a new transport to Clara's gateway/registry layer:
- HTTP server that speaks MCP over SSE (or Streamable HTTP)
- HTTPS + bearer token validation
- Re-exposes configured tools from the registry
- Logs all requests with client identity

### On Client Machines

```yaml
mcp_servers:
  - name: mac-mini-llm
    transport: http
    url: "https://mac-mini.local:8443/mcp"
    token: "${MAC_MINI_MCP_TOKEN}"
    description: "LLM services on M4 Mac Mini"
```

### Config on the Mac Mini

```yaml
network_mcp:
  enabled: true
  port: 8443
  tls_cert: "~/.config/clara/certs/server.crt"
  tls_key: "~/.config/clara/certs/server.key"
  clients:
    - name: nathans-macbook
      token: "${CLARA_TOKEN_NATHAN}"
    - name: alexs-macbook
      token: "${CLARA_TOKEN_ALEX}"
  expose_servers:
    - llm
```

## Implementation Steps

1. Add `transport: http` support to `internal/registry/`
2. Implement the network MCP server
3. Add network MCP config to the config schema
4. Document mkcert setup for LAN TLS
5. Create a Mac Mini setup flow
6. Update `config.yaml.example`

## Acceptance Criteria

- Clara runs on the Mac Mini as a LaunchAgent
- The `llm` MCP server is accessible from my laptop using a bearer token
- `clara tool list mac-mini-llm` shows the LLM tools from the mac mini
- Unauthenticated requests are rejected
- Requests with invalid tokens are rejected
- Alex's laptop can also connect using her own token
- Ollama resource protection is enforced on the mac mini
