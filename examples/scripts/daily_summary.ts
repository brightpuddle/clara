#!/usr/bin/env bun
// examples/scripts/daily_summary.ts
// Scheduled morning digest workflow executed on cron trigger in TypeScript.

import { callTool, runID } from "../../scripts/clara";

console.log("=== Starting Morning Digest (TypeScript / Bun) ===");
console.log(`Run ID: ${runID()}`);

console.log("Querying database records...");
const recentEntries = await callTool("db.query", {
  sql: "SELECT datetime('now') as generated_at, 'System healthy' as status",
});
console.log("Query results:", JSON.stringify(recentEntries));

console.log("Sending daily digest summary notification...");
await callTool("notify.send", {
  message: `Morning Digest compiled at ${new Date().toLocaleTimeString()}`,
});

console.log("=== Digest Compiled and Posted ===");
