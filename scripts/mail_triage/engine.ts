// scripts/mail_triage/engine.ts
// Main Triage Orchestration Engine coordinating YAML Rules, Tier 1, Tier 2, Learning, Reminders, Drafts, and Mail operations.

import type {
  TriageConfig,
  TriageDecision,
  TriageRunMetrics,
  MailSummary,
  MailDetail,
  YamlRule,
} from "./types.ts";
import { TriageAuditDatabase } from "./db.ts";
import { DeterministicEngine } from "./deterministic.ts";
import { SemanticClassifier } from "./llm.ts";
import { AppleMailClient } from "./mail_client.ts";
import { YamlRulesLoader } from "./rules_loader.ts";
import { callTool } from "../clara.ts";

export class TriageEngine {
  private db: TriageAuditDatabase;
  private rulesLoader: YamlRulesLoader;
  private deterministic: DeterministicEngine;
  private semantic: SemanticClassifier;
  private mailClient: AppleMailClient;
  private config: TriageConfig;

  constructor(config: TriageConfig, db?: TriageAuditDatabase, rulesLoader?: YamlRulesLoader) {
    this.config = config;
    this.db = db || new TriageAuditDatabase(config.dbPath);
    this.rulesLoader = rulesLoader || new YamlRulesLoader(config.rulesPath, config.learnedRulesPath);
    this.deterministic = new DeterministicEngine();
    this.semantic = new SemanticClassifier();
    this.mailClient = new AppleMailClient();
  }

  /**
   * Run triage across all configured accounts (or a single specified account).
   */
  public async run(options: {
    accountName?: string;
    limit?: number;
    shadowMode?: boolean;
  } = {}): Promise<TriageRunMetrics[]> {
    const shadow = options.shadowMode ?? this.config.shadowMode;
    const limit = options.limit ?? this.config.maxBatchSize;
    const startTime = Date.now();

    let accountsToProcess: { name: string; email: string }[] = [];
    if (options.accountName) {
      accountsToProcess = [{ name: options.accountName, email: "" }];
    } else if (this.config.accounts.length > 0) {
      accountsToProcess = this.config.accounts.filter(a => a.enabled !== false);
    } else {
      // Auto-discover accounts from Mail.app
      const mailboxes = await this.mailClient.getMailboxes();
      const uniqueAccounts = Array.from(new Set(mailboxes.map(m => m.account).filter(a => a.length > 0)));
      if (uniqueAccounts.length > 0) {
        accountsToProcess = uniqueAccounts.map(name => ({ name, email: "" }));
      } else {
        accountsToProcess = [{ name: "", email: "" }];
      }
    }

    const allMetrics: TriageRunMetrics[] = [];

    for (const acc of accountsToProcess) {
      const accMetrics = await this.processAccount(acc.name, limit, shadow);
      allMetrics.push(accMetrics);
    }

    return allMetrics;
  }

  private async processAccount(
    accountName: string,
    limit: number,
    shadow: boolean
  ): Promise<TriageRunMetrics> {
    const startTime = Date.now();
    const rules: YamlRule[] = this.rulesLoader.loadRules();

    const metrics: TriageRunMetrics = {
      timestamp: new Date().toISOString(),
      account: accountName || "Default",
      total_processed: 0,
      tier1_fastpath_hits: 0,
      tier2_llm_calls: 0,
      new_rules_learned: 0,
      reminders_created: 0,
      drafts_created: 0,
      feedback_requested: 0,
      actions: { inbox: 0, review: 0, archive: 0, deleted: 0 },
      duration_ms: 0,
    };

    console.log(
      `\n🔍 Fetching up to ${limit} unread emails for [${accountName || "All Accounts"}]...`
    );
    const summaries: MailSummary[] = await this.mailClient.listInbox(
      limit,
      accountName || undefined,
      true
    );

    if (summaries.length === 0) {
      console.log(`✨ Inbox clean! No unread messages found for [${accountName || "Default"}].`);
      metrics.duration_ms = Date.now() - startTime;
      return metrics;
    }

    console.log(`📬 Found ${summaries.length} unread messages. Evaluating against ${rules.length} YAML rules...`);

    for (const summary of summaries) {
      metrics.total_processed++;

      // 1. Fast-Path Tier 1 check on summary fields (subject, sender)
      // This avoids fetching full message content/headers over IMAP, preventing Gmail from marking messages as Important
      const summaryMail: MailDetail = {
        ...summary,
        content: "",
        account_name: accountName,
      };

      let decision: TriageDecision | null = this.deterministic.evaluate(summaryMail, rules);
      let evaluatedMail: MailDetail = summaryMail;

      if (decision) {
        metrics.tier1_fastpath_hits++;
        if (decision.ruleId) {
          this.db.recordHit(decision.ruleId);
        }
      } else {
        // 2. Only fetch full message detail if summary heuristics did not match
        const fullMail: MailDetail = await this.mailClient.getMessage(
          summary.id,
          accountName || undefined
        );
        evaluatedMail = fullMail;

        // Re-evaluate deterministic rules with full headers & recipient data
        decision = this.deterministic.evaluate(fullMail, rules);

        if (decision) {
          metrics.tier1_fastpath_hits++;
          if (decision.ruleId) {
            this.db.recordHit(decision.ruleId);
          }
        } else {
          // 3. Fall-through: Evaluate Tier 2 (LLM Semantic Classifier & Fall-through Agent)
          metrics.tier2_llm_calls++;
          decision = await this.semantic.classify(fullMail);

          // 4. If AI is uncertain, request user feedback via notification system
          if (decision.needsFeedback) {
            metrics.feedback_requested!++;
            const userChoice = await this.requestUserFeedback(evaluatedMail, decision, shadow);
            if (userChoice) {
              decision = this.applyFeedbackToDecision(decision, userChoice);
            }
          }

          // 5. Learn new rule if generalizable -> appends to learned_rules.yaml
          if (decision.proposedHeuristic && decision.confidence >= this.config.confidenceThreshold) {
            const newRule = this.rulesLoader.appendLearnedRule(decision.proposedHeuristic);
            rules.unshift(newRule); // update local rules list in memory
            metrics.new_rules_learned++;
            console.log(`🧠 [LEARNED NEW YAML RULE] "${newRule.name}" -> ${newRule.action} (${newRule.target_folder})`);
          }
        }
      }

      // Safe fallback guarantee
      if (!decision || !decision.action) {
        decision = {
          action: "inbox",
          targetMailbox: "INBOX",
          reason: "Safe fallback: Retained in Inbox",
          confidence: 1.0,
          tier: 1,
        };
      }

      metrics.actions[decision.action]++;

      // 6. Output decision
      const prefix = shadow ? "[SHADOW]" : "[EXECUTE]";
      const tierTag = decision.tier === 1 ? "⚡ Tier 1 (0 tokens)" : "🤖 Tier 2 (AI)";
      console.log(
        `${prefix} ${tierTag} -> ${decision.action.toUpperCase()} [${decision.targetMailbox}] | ` +
        `"${evaluatedMail.subject.substring(0, 45)}" from <${evaluatedMail.sender}> | Reason: ${decision.reason}`
      );

      // 7. Execute Actions (Mail Operations, Reminders, Drafts)
      if (!shadow) {
        // Mail moving/deleting
        await this.applyMailAction(evaluatedMail, decision, accountName);

        // Reminders & Task creation
        if (decision.reminder) {
          const rfcId = evaluatedMail.message_id || evaluatedMail.headers?.["message-id"];
          const cleanRfcId = rfcId ? rfcId.replace(/^<|>$/g, "").trim() : undefined;
          if (this.db.hasReminderForMessage(evaluatedMail.id, cleanRfcId)) {
            console.log(`  ⏭️ [DEDUP] Skipping reminder for message ${evaluatedMail.id}: reminder already recorded`);
          } else {
            const created = await this.applyReminderAction(evaluatedMail, decision.reminder);
            if (created) metrics.reminders_created!++;
          }
        }

        // Draft response creation
        if (decision.draft) {
          const created = await this.applyDraftAction(accountName, decision.draft);
          if (created) metrics.drafts_created!++;
        }
      } else {
        if (decision.reminder) {
          console.log(`  [SHADOW] 📝 Would create reminder: "${decision.reminder.title}" (due: ${decision.reminder.due_date || "none"})`);
        }
        if (decision.draft) {
          console.log(`  [SHADOW] ✉️ Would create draft reply to <${decision.draft.to}>: "${decision.draft.subject}"`);
        }
      }

      // 8. Record Audit Log
      this.db.logAudit(evaluatedMail, decision, shadow);
    }

    metrics.duration_ms = Date.now() - startTime;
    return metrics;
  }

  private async requestUserFeedback(
    mail: MailDetail,
    decision: TriageDecision,
    shadow: boolean
  ): Promise<string | null> {
    const question =
      decision.feedbackQuestion ||
      `Clara Mail Triage is uncertain about this email:\nFrom: ${mail.sender}\nSubject: "${mail.subject}"\nHow would you like to handle it?`;
    const options = decision.feedbackOptions || ["Actionable (Inbox)", "Review (@Review)", "Archive", "Delete"];

    console.log(`❓ [UNCERTAIN] Requesting user feedback via Clara notification system...`);
    if (shadow) {
      console.log(`  [SHADOW] Would ask user: "${question}" (Options: ${options.join(", ")})`);
      return null;
    }

    try {
      const response = await callTool("notify.ask" as any, {
        question,
        options,
        allow_text: true,
      });

      if (response && (response.selection || response.custom_text)) {
        const choice = response.custom_text || response.selection;
        console.log(`💬 User feedback received: "${choice}"`);
        return choice;
      }
    } catch (err: any) {
      console.warn(`[notify.ask] Failed to prompt user: ${err.message}. Sending fire-and-forget alert instead.`);
      try {
        await callTool("notify.send" as any, {
          message: `📬 Unresolved email triage: "${mail.subject}" from <${mail.sender}>. Retained in Inbox for manual review.`,
        });
      } catch {}
    }

    return null;
  }

  private applyFeedbackToDecision(decision: TriageDecision, feedback: string): TriageDecision {
    const normalized = feedback.toLowerCase();
    let action: "inbox" | "review" | "archive" | "deleted" = "inbox";
    let targetMailbox = "INBOX";

    if (normalized.includes("review")) {
      action = "review";
      targetMailbox = this.config.mailboxMap.review;
    } else if (normalized.includes("archive")) {
      action = "archive";
      targetMailbox = this.config.mailboxMap.archive;
    } else if (normalized.includes("delete") || normalized.includes("trash")) {
      action = "deleted";
      targetMailbox = this.config.mailboxMap.deleted;
    }

    return {
      ...decision,
      action,
      targetMailbox,
      reason: `User guided triage feedback: ${feedback}`,
      confidence: 1.0,
    };
  }

  private async applyReminderAction(
    mail: MailDetail,
    reminder: { title: string; due_date?: string; notes?: string; priority?: number; url?: string }
  ): Promise<boolean> {
    try {
      const rfcId = mail.message_id || mail.headers?.["message-id"];
      const cleanRfcId = rfcId ? rfcId.replace(/^<|>$/g, "").trim() : undefined;
      const messageUrl = reminder.url || (cleanRfcId ? `message://${encodeURIComponent("<" + cleanRfcId + ">")}` : undefined);

      const notes = [
        reminder.notes || "",
        messageUrl ? `Link: ${messageUrl}` : "",
      ].filter(Boolean).join("\n\n");

      // 1. Create macOS Reminder
      const res = await callTool("macos.reminders_create" as any, {
        title: reminder.title,
        notes,
        due_date: reminder.due_date,
        priority: reminder.priority ?? 0,
        url: messageUrl,
      });

      const reminderId = res?.identifier;
      console.log(`📝 [REMINDER CREATED] "${reminder.title}" (Due: ${reminder.due_date || "none"})${messageUrl ? ` [Linked: ${messageUrl}]` : ""}`);

      // 2. Also register in Clara task tracking
      try {
        await callTool("task.create" as any, {
          description: reminder.title,
          due: reminder.due_date,
          reminder_id: reminderId,
          tags: ["mail", "triage"],
        });
      } catch (tErr: any) {
        // Task creation is optional if macos reminder succeeded
      }

      // 3. Record in database to prevent future duplicate reminders
      this.db.recordReminderCreated(
        mail.id,
        cleanRfcId,
        mail.subject,
        mail.sender,
        reminderId,
        reminder.title
      );

      return true;
    } catch (err: any) {
      console.error(`[Error creating reminder] ${err.message}`);
      return false;
    }
  }

  private async applyDraftAction(
    accountName: string,
    draft: { subject: string; to: string; content: string }
  ): Promise<boolean> {
    try {
      await callTool("macos.mail_create_draft" as any, {
        subject: draft.subject,
        to: draft.to,
        content: draft.content,
        account_name: accountName || undefined,
      });
      console.log(`✉️ [DRAFT CREATED] Subject: "${draft.subject}" -> To: <${draft.to}>`);
      return true;
    } catch (err: any) {
      console.error(`[Error creating mail draft] ${err.message}`);
      return false;
    }
  }

  private async applyMailAction(
    mail: MailDetail,
    decision: TriageDecision,
    accountName: string
  ): Promise<void> {
    try {
      switch (decision.action) {
        case "inbox":
          // Actionable/critical: leave in inbox
          break;
        case "review":
          await this.mailClient.moveMessage(
            mail.id,
            decision.targetMailbox || this.config.mailboxMap.review,
            accountName
          );
          break;
        case "archive":
          await this.mailClient.moveMessage(
            mail.id,
            decision.targetMailbox || this.config.mailboxMap.archive,
            accountName
          );
          break;
        case "deleted":
          await this.mailClient.deleteMessage(mail.id);
          break;
      }
    } catch (err: any) {
      console.error(`[Error applying mail action ${decision.action}] ${err.message}`);
    }
  }

  public getDb(): TriageAuditDatabase {
    return this.db;
  }

  public getRulesLoader(): YamlRulesLoader {
    return this.rulesLoader;
  }
}
