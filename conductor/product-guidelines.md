# Clara Product Guidelines

- **MCP-First:** All capabilities available to workflows must be exposed via MCP
  servers.
- **Deterministic Intents:** High-level tasks (Intents) should be authored as Go
  plugins that clearly define their inputs, state management, and tool
  interactions.
- **Human-in-the-Loop:** Orchestration should gracefully handle transitions
  between automated steps and human intervention (TUI notifications/Q&A).
- **Stability and Performance:** The daemon and CLI must remain lightweight and
  stable, suitable for continuous background operation.
- **Security:** Strict isolation between daemon state and user intent logic;
  protecting credentials and local data.
