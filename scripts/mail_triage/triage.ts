#!/usr/bin/env bun
// scripts/mail_triage/triage.ts
// CLI entrypoint for Clara Apple Mail Triage Engine (with Declarative YAML Rules).

import { parseArgs } from "node:util";
import { TriageAuditDatabase, getDefaultDbPath } from "./db.ts";
import { YamlRulesLoader, getDefaultRulesPath } from "./rules_loader.ts";
import { TriageEngine } from "./engine.ts";
import type { TriageConfig } from "./types.ts";

const { values } = parseArgs({
  args: Bun.argv.slice(2),
  options: {
    shadow: { type: "boolean", default: false },
    execute: { type: "boolean", default: false },
    limit: { type: "string", default: "25" },
    account: { type: "string" },
    rules: { type: "boolean", default: false },
    stats: { type: "boolean", default: false },
    "rules-file": { type: "string" },
    help: { type: "boolean", default: false },
  },
  allowPositionals: true,
});

if (values.help) {
  console.log(`
Clara Apple Mail Triage CLI (YAML Rules Engine)

Usage:
  bun run scripts/mail_triage/triage.ts [options]

Options:
  --shadow            Run in shadow / dry-run mode (default unless --execute)
  --execute           Enable active moving and trashing of mail
  --limit <N>         Maximum unread emails to process per account (default: 25)
  --account <name>    Process only a specific account (e.g. "Exchange", "Google")
  --rules             List all loaded YAML rules from ~/.config/clara/mail_rules.yaml
  --stats             Display cumulative database stats and triage telemetry
  --rules-file <path> Custom path to mail_rules.yaml file
  --help              Show this help message
`);
  process.exit(0);
}

const db = new TriageAuditDatabase();
const rulesLoader = new YamlRulesLoader(values["rules-file"]);

if (values.stats) {
  const stats = db.getStats();
  console.log("\n=========================================");
  console.log("   Clara Mail Triage Telemetry Stats     ");
  console.log("=========================================");
  console.log(`Total Emails Audited:       ${stats.totalAudits}`);
  console.log(`Tier 1 (YAML Rules) Hits:   ${stats.tier1Hits} (Saved ${(stats.tier1Hits * 0.002).toFixed(3)}$ in API cost)`);
  console.log(`Tier 2 (AI Semantic) Calls: ${stats.tier2Calls}`);
  console.log("\nAction Distribution:");
  console.log(`  • Inbox (Actionable):    ${stats.actions.inbox || 0}`);
  console.log(`  • @Review (FYI/Read):    ${stats.actions.review || 0}`);
  console.log(`  • Archive (Reference):   ${stats.actions.archive || 0}`);
  console.log(`  • Deleted Items:         ${stats.actions.deleted || 0}`);
  console.log("=========================================\n");
  process.exit(0);
}

if (values.rules) {
  const rules = rulesLoader.loadRules();
  console.log("\n=========================================");
  console.log(`   Loaded YAML Rules (${rules.length})`);
  console.log(`   Primary: ${rulesLoader.getPrimaryRulesPath()}`);
  console.log(`   Learned: ${rulesLoader.getLearnedRulesPath()}`);
  console.log("=========================================\n");
  for (const r of rules) {
    console.log(`[${r.id}] ${r.name || "Untitled Rule"}`);
    if (r.description) console.log(`  Description: ${r.description}`);
    console.log(`  Action: ${r.action.toUpperCase()} -> ${r.target_folder || r.action}`);
    console.log(`  Confidence: ${r.confidence ?? 1.0} | Enabled: ${r.enabled !== false}`);
    console.log(`  Match AST: ${JSON.stringify(r.match)}`);
    console.log("");
  }
  process.exit(0);
}

// By default, if --execute is not explicitly passed, run in shadow mode for safety
const isShadow = values.execute ? false : true;
const limit = parseInt(values.limit || "25", 10);

const config: TriageConfig = {
  accounts: [], // Empty means auto-discover all accounts in Mail.app
  mailboxMap: {
    inbox: "INBOX",
    review: "@Review",
    archive: "Archive",
    deleted: "Deleted Items",
  },
  confidenceThreshold: 0.85,
  maxBatchSize: limit,
  dbPath: getDefaultDbPath(),
  rulesPath: values["rules-file"],
  shadowMode: isShadow,
};

const engine = new TriageEngine(config, db, rulesLoader);

console.log("=========================================");
console.log(` Clara Mail Triage Engine (${isShadow ? "🛡️ SHADOW MODE" : "🚀 LIVE EXECUTION"})`);
console.log(` Rules Source: ${rulesLoader.getPrimaryRulesPath()}`);
console.log("=========================================");

const metrics = await engine.run({
  accountName: values.account,
  limit,
  shadowMode: isShadow,
});

console.log("\n=========================================");
console.log("   Execution Run Summary                 ");
console.log("=========================================");
for (const m of metrics) {
  console.log(`Account: [${m.account}] | Processed: ${m.total_processed} (${m.duration_ms}ms)`);
  console.log(`  ⚡ Tier 1 YAML Fast-Path Hits: ${m.tier1_fastpath_hits}`);
  console.log(`  🤖 Tier 2 AI Invocations:      ${m.tier2_llm_calls}`);
  console.log(`  🧠 New YAML Rules Learned:     ${m.new_rules_learned}`);
  if (m.reminders_created) {
    console.log(`  📝 Reminders Created:          ${m.reminders_created}`);
  }
  if (m.drafts_created) {
    console.log(`  ✉️ Draft Replies Created:      ${m.drafts_created}`);
  }
  if (m.feedback_requested) {
    console.log(`  ❓ Feedback Prompts Sent:      ${m.feedback_requested}`);
  }
  console.log(`  📊 Routing Breakdown: Inbox=${m.actions.inbox}, Review=${m.actions.review}, Archive=${m.actions.archive}, Deleted=${m.actions.deleted}`);
}
console.log("=========================================\n");
