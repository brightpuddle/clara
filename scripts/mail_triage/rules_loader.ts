// scripts/mail_triage/rules_loader.ts
// Loads and saves declarative YAML rules with fallback resolution and schema support.

import { parse, stringify } from "yaml";
import { existsSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, dirname } from "node:path";
import { homedir } from "node:os";
import type { YamlRule, LearnedRuleCandidate } from "./types.ts";

export function getDefaultRulesPath(): string {
  const userPath = join(homedir(), ".config", "clara", "mail_rules.yaml");
  if (existsSync(userPath)) {
    return userPath;
  }
  // Local project fallback
  const localPath = join(import.meta.dir, "rules.yaml");
  if (existsSync(localPath)) {
    return localPath;
  }
  return join(import.meta.dir, "../../examples/mail_rules.yaml");
}

export function getDefaultLearnedRulesPath(): string {
  const userDir = join(homedir(), ".config", "clara");
  if (!existsSync(userDir)) {
    mkdirSync(userDir, { recursive: true });
  }
  return join(userDir, "learned_rules.yaml");
}

export class YamlRulesLoader {
  private rulesPath: string;
  private learnedRulesPath: string;

  constructor(rulesPath?: string, learnedRulesPath?: string) {
    this.rulesPath = rulesPath || getDefaultRulesPath();
    this.learnedRulesPath = learnedRulesPath || getDefaultLearnedRulesPath();
  }

  /**
   * Load all active rules from primary rules.yaml and learned_rules.yaml.
   */
  public loadRules(): YamlRule[] {
    const allRules: YamlRule[] = [];

    // 1. Load primary rules
    if (existsSync(this.rulesPath)) {
      try {
        const content = readFileSync(this.rulesPath, "utf8");
        const parsed = parse(content);
        if (parsed && Array.isArray(parsed.rules)) {
          allRules.push(...parsed.rules.filter((r: YamlRule) => r.enabled !== false));
        }
      } catch (err: any) {
        console.warn(`[YamlRulesLoader] Failed to parse ${this.rulesPath}: ${err.message}`);
      }
    }

    // 2. Load learned rules if available
    if (existsSync(this.learnedRulesPath)) {
      try {
        const content = readFileSync(this.learnedRulesPath, "utf8");
        const parsed = parse(content);
        if (parsed && Array.isArray(parsed.rules)) {
          allRules.push(...parsed.rules.filter((r: YamlRule) => r.enabled !== false));
        }
      } catch (err: any) {
        console.warn(`[YamlRulesLoader] Failed to parse ${this.learnedRulesPath}: ${err.message}`);
      }
    }

    return allRules;
  }

  /**
   * Append a newly synthesized AI rule to learned_rules.yaml.
   */
  public appendLearnedRule(candidate: LearnedRuleCandidate): YamlRule {
    const id = `learned-${Date.now().toString(36)}-${Math.random().toString(36).substring(2, 6)}`;
    const newRule: YamlRule = {
      id,
      name: candidate.name,
      description: candidate.rationale,
      enabled: true,
      action: candidate.action,
      target_folder: candidate.target_folder,
      confidence: 0.95,
      match: candidate.match,
    };

    let existingRules: YamlRule[] = [];
    if (existsSync(this.learnedRulesPath)) {
      try {
        const content = readFileSync(this.learnedRulesPath, "utf8");
        const parsed = parse(content);
        if (parsed && Array.isArray(parsed.rules)) {
          existingRules = parsed.rules;
        }
      } catch {
        existingRules = [];
      }
    } else {
      const parentDir = dirname(this.learnedRulesPath);
      if (!existsSync(parentDir)) {
        mkdirSync(parentDir, { recursive: true });
      }
    }

    existingRules.push(newRule);

    const fileContent = `# yaml-language-server: $schema=./schemas/mail_rules.schema.json
# Clara AI-Synthesized Learned Rules (Auto-generated).
# Review, edit, or merge into ~/.config/clara/mail_rules.yaml as desired.

rules:
${existingRules.map(r => this.formatRuleYaml(r)).join("\n")}
`;

    writeFileSync(this.learnedRulesPath, fileContent, "utf8");
    return newRule;
  }

  private formatRuleYaml(rule: YamlRule): string {
    const doc = {
      id: rule.id,
      name: rule.name,
      description: rule.description,
      enabled: rule.enabled ?? true,
      action: rule.action,
      target_folder: rule.target_folder,
      confidence: rule.confidence ?? 1.0,
      match: rule.match,
    };
    return stringify([doc]).trim().replace(/^/gm, "  ");
  }

  public getPrimaryRulesPath(): string {
    return this.rulesPath;
  }

  public getLearnedRulesPath(): string {
    return this.learnedRulesPath;
  }
}
