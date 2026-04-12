# Product Guidelines: Clara

## Communication Style
- **Prose Style:** Professional & Concise. Documentation and user-facing messages should be clear, direct, and technically accurate. Use active voice and avoid fluff.
- **Branding Tone:** Minimalist Utility. Focus on efficiency, reliability, and getting things done. The brand should be seen as a powerful, silent, and dependable utility.

## User Experience (UX) Principles
- **CLI-First Design:** Prioritize a robust and efficient terminal experience. Ensure all core functionalities are accessible and ergonomic via the command-line.
- **Quiet Operation:** The system should operate silently in the background until explicitly called upon or until a critical event requires human intervention.
- **High Visibility of State:** Always provide clear, actionable feedback regarding the system's state, tool execution, and errors. The status of long-running or event-driven tasks must be easily inspectable.

## Tool & Workflow Conventions
- **Functional Namespacing & Aggregation:** MCP tools follow a `namespace.tool` format. Per SEP-986, tools registered with a dot in their name are transparently aggregated into their respective namespaces (e.g., `mail.search` from a search server and `mail.send` from a mail server both appear in the `mail` namespace).
- **Reliable Automation:** Favor deterministic execution over unpredictable AI-first approaches. All core workflows should be inspectable, versionable, and repeatable.
- **Deterministic Intents:** High-level tasks (Intents) should be authored as Starlark scripts that clearly define their inputs, state management, and tool interactions.
