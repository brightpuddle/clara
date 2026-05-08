# Clara MCP Network Design

## Overview
Migrate Webex and Discord integrations out of the centralized `eve` relay and into self-contained `hashicorp/go-plugin` integrations within the `clara` repository. To maintain cross-machine capabilities, Clara will be upgraded to act as a remote MCP Server and Event Streamer, allowing isolated Clara instances to form a decentralized network of tool providers.

## Component Boundaries
- **eve Deprecation**: Webex and Discord relay handlers will be removed from `eve/main`.
- **Clara Server**: Clara will expose a new HTTP/SSE server package. When configured with a shared secret, it will speak the MCP protocol over HTTP, advertising all tools loaded from its local plugins.
- **Remote MCP Clients**: Clara will support configuring a "remote" instance. For example, `sophia` can point to `eve`. To `sophia`, `eve` behaves as a massive tool provider. Tool calls execute transparently on the remote instance.

## Remote Tool Namespacing
- **Namespace Merging**: Tools discovered from a remote Clara instance will be merged directly into the local root namespace (e.g., `webex.messages.list` instead of `eve.webex.messages.list`).
- **Conflict Resolution**: If a tool name conflict occurs between a local plugin and a remote MCP server, the local tool will take precedence. This ensures transparent remote execution for missing capabilities while allowing local overrides.

## Events & Webhooks
- **Webhook Hosting**: Webex requires a public callback URL. Clara's HTTP server will natively host webhook endpoints (e.g., `/api/webex/callback`).
- **Proxy Support**: To support running behind reverse proxies (like Caddy), Clara's integration configurations will accept an explicit `public_url` (or `webhook_callback_url`) setting to use during dynamic registration, separate from its local bind port.
- **Event Streaming (SSE)**: Clara's server will expose an `/events` SSE endpoint, replacing `eve`'s current relay mechanism.
- **Event Bridging**: When a Clara instance connects to a remote Clara (e.g., Sophia to Eve), it will subscribe to the `/events` SSE stream. Remote events are injected into the local event bus, allowing agents to wake up based on triggers like `webex.message_created` regardless of where the webhook landed.

## Authentication & Approvals
- **Centralized OAuth**: OAuth is managed exclusively by the Clara instance hosting the integration. `sophia` does not perform Webex OAuth; it routes tool calls (`webex.message.reply`) to `eve`, which executes them using `eve`'s active OAuth tokens. This prevents token expiration issues on sleeping laptops.
- **Interactive Approvals**: Tools like `approval.request` will block locally. The remote Clara instance (Eve) receives the webhook (e.g., Webex `attachmentActions`), matches it to the pending request, and returns the decision (`approved`/`rejected`) down the MCP chain to unblock the caller.