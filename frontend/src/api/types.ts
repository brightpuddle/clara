export interface TriggerDefinition {
  id: string;
  name: string;
  description?: string;
  enabled: boolean;
  type: 'event' | 'schedule' | 'worker' | 'manual';
  match?: any;
  schedule?: string;
  debounce?: number;
  throttle?: number;
  action: {
    exec: string;
    args?: string[];
    env?: Record<string, string>;
    dir?: string;
    pass_event?: string;
    timeout?: number;
    restart?: string;
    restart_delay?: number;
    max_restarts?: number;
  };
}

export interface TriggerRunRecord {
  id: string;
  trigger_id: string;
  trigger_type: string;
  started_at: string;
  finished_at: string;
  duration_ms: number;
  exit_code: number;
  stdout: string;
  stderr: string;
  status: string;
  event?: any;
}

export interface ToolCallRecord {
  id: string;
  run_id?: string;
  tool_name: string;
  started_at: string;
  finished_at: string;
  duration_ms: number;
  input: string;
  output: string;
  is_error: boolean;
}

export interface DashboardData {
  triggers_count: number;
  event_triggers_count: number;
  schedule_triggers_count: number;
  worker_triggers_count: number;
  manual_triggers_count: number;
  tools_count: number;
  integrations_count: number;
  triggers: TriggerDefinition[];
  recent_runs: TriggerRunRecord[];
  integrations: Array<{ name: string; status?: string; description?: string }>;
  recent_logs: string[];
}

export interface IntegrationsData {
  plugins: Array<Record<string, any>>;
  mcp_servers: Array<{ name: string; status: string }>;
}

export interface ConfigData {
  raw_yaml: string;
  config_path: string;
  structured: any;
}
