// scripts/mail_triage/types.ts
// Strongly typed models for Clara Mail Triage Engine with YAML AST support.

export type TriageAction = "inbox" | "review" | "archive" | "deleted";

export interface ReminderProposal {
  title: string;
  notes?: string;
  due_date?: string; // ISO-8601 string or relative
  priority?: number;
  url?: string;
}

export interface DraftProposal {
  subject: string;
  to: string;
  content: string;
  account_name?: string;
}

export interface MailAccountConfig {
  name: string;
  email: string;
  aliases?: string[];
  enabled?: boolean;
}

export interface MailSummary {
  id: string;
  message_id?: string;
  subject: string;
  sender: string;
  date_received: string;
  read_status: boolean;
  account_name?: string;
}

export interface MailDetail extends MailSummary {
  content: string;
  to?: string;
  cc?: string;
  headers?: Record<string, string>;
  raw_headers?: string;
  is_direct_recipient?: boolean;
}

export type MatchOperator =
  | "equals"
  | "contains"
  | "regex"
  | "starts_with"
  | "ends_with"
  | "exists";

export interface MatchNode {
  and?: MatchNode[];
  or?: MatchNode[];
  not?: MatchNode;
  field?: string;
  op?: MatchOperator;
  value?: string | boolean | number;
}

export interface YamlRule {
  id: string;
  name?: string;
  description?: string;
  enabled?: boolean;
  action: TriageAction;
  target_folder?: string;
  confidence?: number;
  match: MatchNode;
}

export interface LearnedRuleCandidate {
  can_generalize: boolean;
  name: string;
  match: MatchNode;
  action: TriageAction;
  target_folder: string;
  rationale: string;
}

export interface TriageDecision {
  action: TriageAction;
  targetMailbox: string;
  reason: string;
  confidence: number;
  tier: 1 | 2;
  ruleId?: string;
  summary?: string;
  hasSensitiveData?: boolean;
  proposedHeuristic?: LearnedRuleCandidate;
  reminder?: ReminderProposal;
  draft?: DraftProposal;
  needsFeedback?: boolean;
  feedbackQuestion?: string;
  feedbackOptions?: string[];
}

export interface TriageConfig {
  accounts: MailAccountConfig[];
  mailboxMap: {
    inbox: string;
    review: string;
    archive: string;
    deleted: string;
  };
  rulesPath?: string;
  learnedRulesPath?: string;
  confidenceThreshold: number;
  maxBatchSize: number;
  dbPath: string;
  shadowMode: boolean;
}

export interface TriageRunMetrics {
  timestamp: string;
  account: string;
  total_processed: number;
  tier1_fastpath_hits: number;
  tier2_llm_calls: number;
  new_rules_learned: number;
  reminders_created?: number;
  drafts_created?: number;
  feedback_requested?: number;
  actions: {
    inbox: number;
    review: number;
    archive: number;
    deleted: number;
  };
  duration_ms: number;
}
