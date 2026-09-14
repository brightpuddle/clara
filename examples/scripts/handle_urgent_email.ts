#!/usr/bin/env bun
// examples/scripts/handle_urgent_email.ts
// Demonstrates external workflow script handling an urgent email event in TypeScript.
// Follows BEAM 'let it fail' philosophy: errors crash the script and are recorded in Clara audit store.

import { readEvent, callTool, runID, triggerID } from "../../scripts/clara";

interface EmailEventData {
  from: string;
  mailbox: string;
  subject: string;
  body?: string;
  priority?: number;
}

console.log("=== Starting Urgent Email Handler (TypeScript / Bun) ===");
console.log(`Run ID: ${runID()}`);
console.log(`Trigger ID: ${triggerID()}`);

// Read strongly-typed incoming CloudEvent from supervisor
const event = await readEvent<EmailEventData>();
console.log("Received Event Subject:", event.data?.subject || "N/A");
console.log("From:", event.data?.from || "Unknown");

// Real-world workflow logic with auto-completed and type-checked Clara MCP tools
console.log("Dispatching incident alert notification...");
await callTool("notify.send", {
  message: `CRITICAL ALERT: [${event.data?.subject}] from ${event.data?.from}`,
});

// Example logging to filesystem or sqlite store
await callTool("fs.write_file", {
  path: `/tmp/incident-${runID()}.json`,
  content: JSON.stringify(event, null, 2),
});

console.log("=== Urgent Email Workflow Completed Successfully ===");
