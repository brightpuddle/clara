import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import type {
  DashboardData,
  TriggerDefinition,
  TriggerRunRecord,
  ToolCallRecord,
  IntegrationsData,
  ConfigData,
} from './types';

const BASE_URL = '/ui/api';

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `Request failed with status ${res.status}`);
  }
  return res.json();
}

export function useDashboard() {
  return useQuery<DashboardData>({
    queryKey: ['dashboard'],
    queryFn: () => fetchJSON<DashboardData>(`${BASE_URL}/dashboard`),
    refetchInterval: 5000,
  });
}

export function useTriggers() {
  return useQuery<{ triggers: TriggerDefinition[]; recent_runs: TriggerRunRecord[] }>({
    queryKey: ['triggers'],
    queryFn: () => fetchJSON(`${BASE_URL}/triggers`),
  });
}

export function useTrigger(id: string) {
  return useQuery<{ trigger: TriggerDefinition; recent_runs: TriggerRunRecord[]; file_path: string }>({
    queryKey: ['triggers', id],
    queryFn: () => fetchJSON(`${BASE_URL}/triggers/${encodeURIComponent(id)}`),
    enabled: !!id,
  });
}

export function useCreateTrigger() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { trigger_json?: string; yaml?: string }) =>
      fetchJSON(`${BASE_URL}/triggers`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['triggers'] });
      qc.invalidateQueries({ queryKey: ['dashboard'] });
    },
  });
}

export function useUpdateTrigger(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { trigger_json?: string; yaml?: string }) =>
      fetchJSON(`${BASE_URL}/triggers/${encodeURIComponent(id)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['triggers'] });
      qc.invalidateQueries({ queryKey: ['triggers', id] });
      qc.invalidateQueries({ queryKey: ['dashboard'] });
    },
  });
}

export function useDeleteTrigger() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      fetchJSON(`${BASE_URL}/triggers/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['triggers'] });
      qc.invalidateQueries({ queryKey: ['dashboard'] });
    },
  });
}

export function useRunTrigger() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      fetchJSON<{ success: boolean; run_id: string }>(`${BASE_URL}/triggers/${encodeURIComponent(id)}/run`, {
        method: 'POST',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['runs'] });
      qc.invalidateQueries({ queryKey: ['dashboard'] });
    },
  });
}

export function useRuns(triggerFilter?: string, id?: string) {
  return useQuery<{
    runs: TriggerRunRecord[];
    selected_run: TriggerRunRecord | null;
    tool_calls: ToolCallRecord[];
  }>({
    queryKey: ['runs', triggerFilter, id],
    queryFn: () => {
      const params = new URLSearchParams();
      if (triggerFilter) params.set('trigger', triggerFilter);
      if (id) params.set('id', id);
      return fetchJSON(`${BASE_URL}/runs?${params.toString()}`);
    },
  });
}

export function useIntegrations() {
  return useQuery<IntegrationsData>({
    queryKey: ['integrations'],
    queryFn: () => fetchJSON<IntegrationsData>(`${BASE_URL}/integrations`),
  });
}

export function useLogs(level?: string) {
  return useQuery<{ lines: string[] }>({
    queryKey: ['logs', level],
    queryFn: () => {
      const params = new URLSearchParams();
      if (level) params.set('level', level);
      return fetchJSON(`${BASE_URL}/logs?${params.toString()}`);
    },
  });
}

export function useConfig() {
  return useQuery<ConfigData>({
    queryKey: ['config'],
    queryFn: () => fetchJSON<ConfigData>(`${BASE_URL}/config`),
  });
}

export function useSaveConfig() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { config_json?: string; yaml?: string }) =>
      fetchJSON(`${BASE_URL}/config`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['config'] });
    },
  });
}
