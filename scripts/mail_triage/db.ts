// scripts/mail_triage/db.ts
// Local SQLite Audit Store and Telemetry Engine using Bun's built-in sqlite engine.

import { Database } from "bun:sqlite";
import { existsSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { homedir } from "node:os";
import type { TriageDecision, MailDetail } from "./types.ts";

export function getDefaultDbPath(): string {
  const dataDir = process.env.CLARA_DATA_DIR || join(homedir(), ".local", "share", "clara");
  if (!existsSync(dataDir)) {
    mkdirSync(dataDir, { recursive: true });
  }
  return join(dataDir, "mail_audit.db");
}

export class TriageAuditDatabase {
  private db: Database;

  constructor(dbPath: string = getDefaultDbPath()) {
    const parentDir = dirname(dbPath);
    if (!existsSync(parentDir)) {
      mkdirSync(parentDir, { recursive: true });
    }
    this.db = new Database(dbPath, { create: true });
    this.init();
  }

  private init() {
    this.db.run(`
      CREATE TABLE IF NOT EXISTS triage_audit_log (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        message_id TEXT NOT NULL,
        account_name TEXT,
        sender TEXT,
        subject TEXT,
        action TEXT NOT NULL,
        target_mailbox TEXT NOT NULL,
        tier INTEGER NOT NULL,
        rule_id TEXT,
        confidence REAL,
        summary TEXT,
        shadow_mode INTEGER NOT NULL,
        timestamp TEXT NOT NULL
      );
    `);

    this.db.run(`
      CREATE TABLE IF NOT EXISTS rule_hit_counters (
        rule_id TEXT PRIMARY KEY,
        hit_count INTEGER NOT NULL DEFAULT 0,
        last_hit_at TEXT NOT NULL
      );
    `);

    this.db.run(`CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON triage_audit_log(timestamp DESC);`);
    this.db.run(`CREATE INDEX IF NOT EXISTS idx_audit_rule ON triage_audit_log(rule_id);`);
  }

  public recordHit(ruleId: string) {
    const now = new Date().toISOString();
    this.db.prepare(`
      INSERT INTO rule_hit_counters (rule_id, hit_count, last_hit_at)
      VALUES (?, 1, ?)
      ON CONFLICT(rule_id) DO UPDATE SET
        hit_count = hit_count + 1,
        last_hit_at = excluded.last_hit_at
    `).run(ruleId, now);
  }

  public getRuleHits(): Record<string, { hitCount: number; lastHitAt: string }> {
    const rows = this.db.query("SELECT rule_id, hit_count, last_hit_at FROM rule_hit_counters").all() as {
      rule_id: string;
      hit_count: number;
      last_hit_at: string;
    }[];
    const out: Record<string, { hitCount: number; lastHitAt: string }> = {};
    for (const r of rows) {
      out[r.rule_id] = { hitCount: r.hit_count, lastHitAt: r.last_hit_at };
    }
    return out;
  }

  public logAudit(mail: MailDetail, decision: TriageDecision, shadowMode: boolean) {
    const now = new Date().toISOString();
    this.db.prepare(`
      INSERT INTO triage_audit_log (
        message_id, account_name, sender, subject, action, target_mailbox, tier, rule_id, confidence, summary, shadow_mode, timestamp
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `).run(
      mail.id,
      mail.account_name ?? "default",
      mail.sender,
      mail.subject,
      decision.action,
      decision.targetMailbox,
      decision.tier,
      decision.ruleId ?? null,
      decision.confidence,
      decision.summary ?? null,
      shadowMode ? 1 : 0,
      now
    );
  }

  public getStats(): {
    totalAudits: number;
    tier1Hits: number;
    tier2Calls: number;
    actions: Record<string, number>;
  } {
    const totalRow = this.db.query("SELECT COUNT(*) as c FROM triage_audit_log").get() as { c: number };
    const tier1Row = this.db.query("SELECT COUNT(*) as c FROM triage_audit_log WHERE tier = 1").get() as { c: number };
    const tier2Row = this.db.query("SELECT COUNT(*) as c FROM triage_audit_log WHERE tier = 2").get() as { c: number };

    const actionRows = this.db.query("SELECT action, COUNT(*) as count FROM triage_audit_log GROUP BY action").all() as { action: string; count: number }[];
    const actions: Record<string, number> = { inbox: 0, review: 0, archive: 0, deleted: 0 };
    for (const r of actionRows) {
      actions[r.action] = r.count;
    }

    return {
      totalAudits: totalRow.c,
      tier1Hits: tier1Row.c,
      tier2Calls: tier2Row.c,
      actions,
    };
  }

  public close() {
    this.db.close();
  }
}
