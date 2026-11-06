// scripts/mail_triage/llm.ts
// Tier 2 Semantic Classifier, Action Orchestrator, and Rule Synthesizer using Clara LLM MCP tool.

import type { MailDetail, TriageDecision, LearnedRuleCandidate } from "./types.ts";
import { callTool } from "../clara.ts";

const SYSTEM_PROMPT = `You are Clara's Email Triage, Action Orchestration, and Rule Synthesis Agent.
Your role is to evaluate unclassified emails based on actionability, urgency, cognitive demand, and long-term utility, and to synthesize deterministic YAML rules for recurring patterns.

Categories:
1. "inbox": The email is actionable, time-sensitive, or directly requires the user's attention/decision.
2. "review": Informational, important digests, newsletters, CC'd discussions, team broadcasts, status reports, CI/CD notifications. Low immediate cognitive demand; can be read and reviewed later.
3. "archive": Important reference, completed receipts, order confirmations, tickets, tax docs, specs with zero immediate action required.
4. "deleted": Outright spam, expired marketing, calendar automated noise, routine ephemeral system alerts.

Actions and Fall-through Policies:
- If this email is actionable:
    Set action to "inbox".
    Provide a "reminder" object with a clear, concise "title", optional "notes", and an ISO-8601 "due_date" (or realistic relative date) inferred from the email text or urgency.
- If this warrants a response:
    Set action to "inbox".
    Provide BOTH a "reminder" object (e.g. "Reply to [Sender]: [Topic]") AND a "draft" object with professional, context-appropriate "subject", "to", and "content" response draft.
- If this is informational and important:
    Set action to "review" (target mailbox "@Review").
- If this is unimportant / noise / spam:
    Set action to "deleted" (target mailbox "Deleted Items") or "archive" (target mailbox "Archive").
- If you are uncertain about how to classify or handle this email:
    Set "needs_feedback": true.
    Provide "feedback_question": a clear question summarizing the email and asking the user what to do.
    Provide "feedback_options": e.g. ["Actionable (Inbox)", "Review (@Review)", "Archive", "Delete"].
- If a recurring heuristic or deterministic pattern can be derived from this email (e.g. specific sender domain, subject prefix, automated alert headers):
    Provide "proposed_heuristic" with can_generalize = true and a valid match AST. If the email is unique or personal, set can_generalize to false.

Output MUST be valid JSON conforming exactly to this structure:
{
  "action": "inbox" | "review" | "archive" | "deleted",
  "target_mailbox": "INBOX" | "@Review" | "Archive" | "Deleted Items",
  "summary": "One-line context of what this email actually is/wants",
  "confidence": 0.95,
  "has_sensitive_data": false,
  "rationale": "Why this category was chosen",
  "reminder": {
    "title": "Follow up on invoice #123",
    "due_date": "2026-11-06T17:00:00Z",
    "notes": "Action required by Friday"
  },
  "draft": {
    "subject": "Re: Project Sync",
    "to": "sender@example.com",
    "content": "Hi Team,\\n\\nThanks for the update..."
  },
  "needs_feedback": false,
  "feedback_question": "Should emails from X be deleted or archived?",
  "feedback_options": ["Review", "Archive", "Delete"],
  "proposed_heuristic": {
    "can_generalize": true,
    "name": "Datadog Alert Rule",
    "action": "review",
    "target_folder": "@Review",
    "rationale": "Automated alert from Datadog",
    "match": {
      "and": [
        { "field": "sender", "op": "contains", "value": "@datadoghq.com" },
        { "field": "is_direct_recipient", "op": "equals", "value": false }
      ]
    }
  }
}
`;

export class SemanticClassifier {
  /**
   * Classify an email using Clara's LLM plugin and extract any generalizable heuristic.
   */
  public async classify(mail: MailDetail): Promise<TriageDecision> {
    const userPrompt = `Classify and triage this email:
From: ${mail.sender}
Subject: ${mail.subject}
To: ${mail.to || "Unknown"}
CC: ${mail.cc || "None"}
Date: ${mail.date_received}
Body Snippet (first 1500 chars):
${(mail.content || "").substring(0, 1500)}
`;

    try {
      // Call Clara LLM MCP tool with category and message parameters
      const response = await callTool("llm.generate" as any, {
        category: "fast",
        message: `${SYSTEM_PROMPT}\n\n${userPrompt}`,
        temperature: 0.1,
      });

      const parsed = this.parseResponse(response);
      if (parsed && typeof parsed.action === "string" && ["inbox", "review", "archive", "deleted"].includes(parsed.action)) {
        return {
          action: parsed.action as any,
          targetMailbox: parsed.target_mailbox || this.getDefaultFolder(parsed.action),
          reason: `Tier 2 (AI): ${parsed.rationale || parsed.summary || "Classified by AI"}`,
          confidence: typeof parsed.confidence === "number" ? parsed.confidence : 0.9,
          tier: 2,
          summary: parsed.summary || "",
          hasSensitiveData: parsed.has_sensitive_data ?? false,
          proposedHeuristic: parsed.proposed_heuristic?.can_generalize ? parsed.proposed_heuristic : undefined,
          reminder: parsed.reminder?.title ? parsed.reminder : undefined,
          draft: parsed.draft?.to && parsed.draft?.content ? parsed.draft : undefined,
          needsFeedback: Boolean(parsed.needs_feedback || (typeof parsed.confidence === "number" && parsed.confidence < 0.65)),
          feedbackQuestion: parsed.feedback_question,
          feedbackOptions: Array.isArray(parsed.feedback_options) ? parsed.feedback_options : undefined,
        };
      }
    } catch (err: any) {
      console.warn(`[Tier 2 LLM] Failed to invoke LLM: ${err.message}. Falling back to safe default (Inbox).`);
    }

    // Safe fallback if LLM is unavailable: keep in Inbox
    return {
      action: "inbox",
      targetMailbox: "INBOX",
      reason: "Tier 2 (AI Fallback): Unclassified / LLM error, safely retained in Inbox",
      confidence: 0.5,
      tier: 2,
    };
  }

  private parseResponse(res: any): any {
    if (!res) return null;
    let text = "";
    if (typeof res === "string") {
      text = res;
    } else if (res && typeof res === "object") {
      if (res.message?.content) {
        text = res.message.content;
      } else if (res.text) {
        text = res.text;
      } else if (res.content) {
        text = typeof res.content === "string" ? res.content : JSON.stringify(res.content);
      } else {
        text = JSON.stringify(res);
      }
    }

    // Extract JSON block if wrapped in markdown
    const jsonMatch = text.match(/\{[\s\S]*\}/);
    if (jsonMatch) {
      text = jsonMatch[0];
    }

    try {
      return JSON.parse(text);
    } catch {
      return null;
    }
  }

  private getDefaultFolder(action: string): string {
    switch (action) {
      case "review":
        return "@Review";
      case "archive":
        return "Archive";
      case "deleted":
        return "Deleted Items";
      default:
        return "INBOX";
    }
  }
}
