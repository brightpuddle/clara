#!/usr/bin/env bun
// scripts/mail_triage/sweep.ts
// Fast-Path Historical Mailbox Sweeper (100% Deterministic YAML Rules, 0 AI tokens, High Throughput).

import { parseArgs } from "node:util";
import { TriageAuditDatabase, getDefaultDbPath } from "./db.ts";
import { YamlRulesLoader } from "./rules_loader.ts";
import { DeterministicEngine } from "./deterministic.ts";
import { AppleMailClient } from "./mail_client.ts";
import type { MailSummary, MailDetail, YamlRule, TriageDecision } from "./types.ts";

const { values } = parseArgs({
  args: Bun.argv.slice(2),
  options: {
    mailbox: { type: "string", default: "Inbox" },
    account: { type: "string" },
    batch: { type: "string", default: "250" },
    limit: { type: "string", default: "1000" },
    "dry-run": { type: "boolean", default: false },
    execute: { type: "boolean", default: false },
    "archive-folder": { type: "string", default: "Archive" },
    "trash-folder": { type: "string", default: "Deleted Items" },
    "review-folder": { type: "string", default: "@Review" },
    "rules-file": { type: "string" },
    "older-than": { type: "string" }, // e.g. "30d", "90d"
    unread: { type: "boolean", default: false },
    help: { type: "boolean", default: false },
  },
  allowPositionals: true,
});

if (values.help) {
  console.log(`
Clara Fast-Path Historical Mailbox Sweeper (YAML Rules Engine)

Usage:
  bun run scripts/mail_triage/sweep.ts [options]

Options:
  --mailbox <name>       Target mailbox to sweep (default: "Inbox")
  --account <name>       Account to sweep (e.g. "Exchange", "Google", "iCloud")
  --dry-run              Preview classifications and actions without moving mail (default)
  --execute              Actively execute batch moves and deletions
  --batch <N>            Batch transaction chunk size (default: 250)
  --limit <N>            Maximum total messages to scan (default: 1000)
  --older-than <days>    Filter messages older than X days (e.g. "30d", "90d")
  --unread               Only scan unread messages (default: all messages)
  --rules-file <path>    Custom path to mail_rules.yaml file
  --archive-folder <name> Destination folder for reference items (default: "Archive")
  --trash-folder <name>   Destination folder for noise (default: "Deleted Items")
  --review-folder <name>  Destination folder for low priority (default: "@Review")
  --help                 Show this help message
`);
  process.exit(0);
}

const isDryRun = values.execute ? false : true;
const batchChunkSize = parseInt(values.batch || "250", 10);
const maxTotalScan = parseInt(values.limit || "1000", 10);
const targetMailbox = values.mailbox || "Inbox";
const accountName = values.account;

// Reference Keywords for Historical Archive preservation
const REFERENCE_KEYWORDS_REGEX = /(license\s*key|serial\s*number|order\s*confirm|invoice\s*#?|receipt|tax\s*form|w-2|1099|itinerary|booking\s*ref|statement\s*period)/i;

// Ephemeral / Stale Noise Patterns
const NOISE_KEYWORDS_REGEX = /^(accepted|declined|tentative):\s+|(out\s*of\s*office|automatic\s*reply|daily\s*digest|weekly\s*summary|newsletter|special\s*offer|sale\s*ends|coupon|%\s*off)/i;

const rulesLoader = new YamlRulesLoader(values["rules-file"]);
const rules: YamlRule[] = rulesLoader.loadRules();
const deterministic = new DeterministicEngine();
const mailClient = new AppleMailClient();

console.log("=========================================");
console.log(` Clara Historical Mailbox Sweeper`);
console.log(` Mode: ${isDryRun ? "🛡️ DRY-RUN PREVIEW (Zero mail moved)" : "🚀 LIVE BATCH EXECUTION"}`);
console.log(` Mailbox: "${targetMailbox}" | Account: "${accountName || "All Accounts"}"`);
console.log(` Rules: ${rules.length} loaded from ${rulesLoader.getPrimaryRulesPath()}`);
console.log(` Batch Size: ${batchChunkSize} | Scan Limit: ${maxTotalScan}`);
console.log("=========================================\n");

// Parse --older-than filter if provided
let cutoffDate: Date | null = null;
if (values["older-than"]) {
  const days = parseInt(values["older-than"].replace("d", ""), 10);
  if (!isNaN(days)) {
    cutoffDate = new Date(Date.now() - days * 24 * 60 * 60 * 1000);
    console.log(`🗓️ Date Filter: Only processing emails received before ${cutoffDate.toISOString().split("T")[0]}`);
  }
}

// 1. Fetch message summaries
console.log(`🔍 Fetching up to ${maxTotalScan} messages from "${targetMailbox}"...`);
const summaries: MailSummary[] = await mailClient.listMailbox(
  targetMailbox,
  accountName,
  maxTotalScan,
  values.unread || false
);

if (summaries.length === 0) {
  console.log(`✨ No messages found in mailbox "${targetMailbox}".`);
  process.exit(0);
}

console.log(`📦 Found ${summaries.length} messages. Scanning fast-path YAML heuristics...\n`);

const toTrashIds: string[] = [];
const toArchiveIds: string[] = [];
const toReviewIds: string[] = [];
const toKeepIds: string[] = [];

const senderNoiseStats: Record<string, number> = {};
const ruleHitStats: Record<string, number> = {};

let processedCount = 0;

for (const s of summaries) {
  processedCount++;

  // Optional date filter
  if (cutoffDate && s.date_received) {
    const msgDate = new Date(s.date_received);
    if (!isNaN(msgDate.getTime()) && msgDate > cutoffDate) {
      toKeepIds.push(s.id);
      continue;
    }
  }

  // Fast-path header/subject evaluation
  let decision: TriageDecision | null = null;

  // 1. Reference check (License keys, invoices, receipts -> Archive)
  if (REFERENCE_KEYWORDS_REGEX.test(s.subject)) {
    decision = {
      action: "archive",
      targetMailbox: values["archive-folder"] || "Archive",
      reason: "Reference Pattern Matched in Subject",
      confidence: 1.0,
      tier: 1,
    };
  }

  // 2. Deterministic YAML AST rule evaluation
  if (!decision) {
    const mailStub: MailDetail = {
      ...s,
      content: "",
      account_name: accountName,
    };
    decision = deterministic.evaluate(mailStub, rules);
  }

  // 3. Fallback noise regex check
  if (!decision && NOISE_KEYWORDS_REGEX.test(s.subject)) {
    decision = {
      action: "deleted",
      targetMailbox: values["trash-folder"] || "Deleted Items",
      reason: "Historical Noise Pattern Matched in Subject",
      confidence: 0.9,
      tier: 1,
    };
  }

  // Classify into buckets
  if (decision) {
    const ruleKey = decision.ruleId || decision.reason;
    ruleHitStats[ruleKey] = (ruleHitStats[ruleKey] || 0) + 1;

    switch (decision.action) {
      case "deleted":
        toTrashIds.push(s.id);
        const domain = s.sender.split("@")[1]?.replace(">", "").trim() || s.sender;
        senderNoiseStats[domain] = (senderNoiseStats[domain] || 0) + 1;
        break;
      case "archive":
        toArchiveIds.push(s.id);
        break;
      case "review":
        toReviewIds.push(s.id);
        break;
      case "inbox":
      default:
        toKeepIds.push(s.id);
        break;
    }
  } else {
    // Default safe harbor: keep in place
    toKeepIds.push(s.id);
  }
}

// Diff Report
console.log("=========================================");
console.log("   Historical Sweep Diff Report          ");
console.log("=========================================");
console.log(`Total Scanned:            ${summaries.length}`);
console.log(`🗑️  Identified Noise (Trash):   ${toTrashIds.length} (${((toTrashIds.length / summaries.length) * 100).toFixed(1)}%)`);
console.log(`📦  Reference (Archive):        ${toArchiveIds.length} (${((toArchiveIds.length / summaries.length) * 100).toFixed(1)}%)`);
console.log(`📑  Low Priority (@Review):     ${toReviewIds.length} (${((toReviewIds.length / summaries.length) * 100).toFixed(1)}%)`);
console.log(`⭐  Protected / Retained in Place: ${toKeepIds.length} (${((toKeepIds.length / summaries.length) * 100).toFixed(1)}%)`);

if (Object.keys(senderNoiseStats).length > 0) {
  console.log("\nTop Noise Sender Domains:");
  const topSenders = Object.entries(senderNoiseStats)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 8);
  for (const [sender, count] of topSenders) {
    console.log(`  • ${sender}: ${count} email(s)`);
  }
}

console.log("=========================================");

// Execution in Live Mode
if (!isDryRun) {
  console.log("\n🚀 Executing Batch Operations...");

  // 1. Batch Trash
  if (toTrashIds.length > 0) {
    console.log(`🗑️ Trashing ${toTrashIds.length} noise messages in chunks of ${batchChunkSize}...`);
    for (let i = 0; i < toTrashIds.length; i += batchChunkSize) {
      const chunk = toTrashIds.slice(i, i + batchChunkSize);
      const res = await mailClient.batchDeleteMessages(chunk);
      console.log(`   Deleted chunk [${i + 1}-${i + chunk.length}]: ${res.deleted} deleted, ${res.failed} failed`);
    }
  }

  // 2. Batch Archive
  if (toArchiveIds.length > 0) {
    const archiveTarget = values["archive-folder"] || "Archive";
    console.log(`📦 Moving ${toArchiveIds.length} reference messages to "${archiveTarget}"...`);
    for (let i = 0; i < toArchiveIds.length; i += batchChunkSize) {
      const chunk = toArchiveIds.slice(i, i + batchChunkSize);
      const res = await mailClient.batchMoveMessages(chunk, archiveTarget, accountName || "");
      console.log(`   Archived chunk [${i + 1}-${i + chunk.length}]: ${res.moved} moved, ${res.failed} failed`);
    }
  }

  // 3. Batch Review
  if (toReviewIds.length > 0) {
    const reviewTarget = values["review-folder"] || "@Review";
    console.log(`📑 Moving ${toReviewIds.length} low-priority messages to "${reviewTarget}"...`);
    for (let i = 0; i < toReviewIds.length; i += batchChunkSize) {
      const chunk = toReviewIds.slice(i, i + batchChunkSize);
      const res = await mailClient.batchMoveMessages(chunk, reviewTarget, accountName || "");
      console.log(`   Moved chunk [${i + 1}-${i + chunk.length}]: ${res.moved} moved, ${res.failed} failed`);
    }
  }

  console.log("\n✅ Historical Sweep Completed Successfully!");
} else {
  console.log("\n💡 This was a DRY RUN preview. No emails were moved or deleted.");
  console.log("   To execute these actions, add --execute to the command:");
  console.log(`   bun run scripts/mail_triage/sweep.ts --mailbox "${targetMailbox}" ${accountName ? `--account "${accountName}"` : ""} --execute\n`);
}
