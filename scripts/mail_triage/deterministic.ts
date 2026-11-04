// scripts/mail_triage/deterministic.ts
// Tier 1 Fast-Path Deterministic Rule Matcher (<1ms execution, 0 LLM tokens).
// Evaluates declarative YAML AST match trees (and, or, not, field comparisons).

import type { YamlRule, MatchNode, MailDetail, TriageDecision } from "./types.ts";

export class DeterministicEngine {
  /**
   * Evaluate a single email against an ordered list of YAML rules.
   */
  public evaluate(mail: MailDetail, rules: YamlRule[]): TriageDecision | null {
    const directRecipient = this.checkDirectRecipient(mail);

    for (const rule of rules) {
      if (rule.enabled === false) continue;

      if (this.evaluateNode(rule.match, mail, directRecipient)) {
        return {
          action: rule.action,
          targetMailbox: rule.target_folder || this.getDefaultFolder(rule.action),
          reason: `Tier 1: Matched YAML rule "${rule.name || rule.id}"`,
          confidence: rule.confidence ?? 1.0,
          tier: 1,
          ruleId: rule.id,
        };
      }
    }

    return null;
  }

  /**
   * Recursively evaluate boolean AST nodes (and, or, not, leaf conditions).
   */
  public evaluateNode(node: MatchNode, mail: MailDetail, isDirect: boolean): boolean {
    if (!node) return false;

    // AND clause: all children must be true
    if (node.and && Array.isArray(node.and)) {
      return node.and.every(child => this.evaluateNode(child, mail, isDirect));
    }

    // OR clause: at least one child must be true
    if (node.or && Array.isArray(node.or)) {
      return node.or.some(child => this.evaluateNode(child, mail, isDirect));
    }

    // NOT clause: child must be false
    if (node.not) {
      return !this.evaluateNode(node.not, mail, isDirect);
    }

    // Leaf condition
    if (node.field && node.op) {
      return this.evalLeafCondition(node.field, node.op, node.value, mail, isDirect);
    }

    return false;
  }

  private evalLeafCondition(
    field: string,
    op: string,
    expectedVal: string | boolean | number | undefined,
    mail: MailDetail,
    isDirect: boolean
  ): boolean {
    let actualVal: string | boolean | number = "";

    switch (field.toLowerCase()) {
      case "sender":
      case "from":
        actualVal = (mail.sender || "").trim().replace(/^[,"]+|[,"]+$/g, "").trim();
        break;
      case "subject":
        actualVal = mail.subject || "";
        break;
      case "to":
        actualVal = mail.to || "";
        break;
      case "cc":
        actualVal = mail.cc || "";
        break;
      case "is_direct_recipient":
        actualVal = isDirect;
        break;
      case "content":
      case "body":
        actualVal = mail.content || "";
        break;
      case "headers.list-unsubscribe":
      case "list-unsubscribe":
        actualVal = mail.headers?.["list-unsubscribe"] || "";
        break;
      default:
        if (field.startsWith("headers.")) {
          const headerKey = field.replace("headers.", "").toLowerCase();
          actualVal = mail.headers?.[headerKey] || "";
        } else {
          actualVal = "";
        }
    }

    if (op === "exists") {
      return String(actualVal).trim().length > 0;
    }

    // Boolean equality
    if (typeof expectedVal === "boolean") {
      return Boolean(actualVal) === expectedVal;
    }

    const sActual = String(actualVal).trim();
    const sExpected = String(expectedVal ?? "").trim();

    switch (op) {
      case "equals":
        return sActual.toLowerCase() === sExpected.toLowerCase();
      case "contains":
        return sActual.toLowerCase().includes(sExpected.toLowerCase());
      case "starts_with":
        return sActual.toLowerCase().startsWith(sExpected.toLowerCase());
      case "ends_with":
        return sActual.toLowerCase().endsWith(sExpected.toLowerCase());
      case "regex":
        try {
          const cleanPattern = sExpected.replace(/^\(\?i\)/, "");
          const re = new RegExp(cleanPattern, "i");
          return re.test(sActual);
        } catch {
          return false;
        }
      default:
        return false;
    }
  }

  private checkDirectRecipient(mail: MailDetail): boolean {
    if (mail.is_direct_recipient !== undefined) {
      return mail.is_direct_recipient;
    }
    return Boolean(mail.to && mail.to.length > 0);
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
