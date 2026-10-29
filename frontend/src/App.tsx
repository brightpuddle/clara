import { Routes, Route, Navigate } from 'react-router-dom';
import { AppLayout } from '@/components/AppLayout';
import { DashboardPage } from '@/pages/DashboardPage';
import { TriggersPage } from '@/pages/TriggersPage';
import { TriggerDetailPage } from '@/pages/TriggerDetailPage';
import { TriggerEditPage } from '@/pages/TriggerEditPage';
import { RunsPage } from '@/pages/RunsPage';
import { IntegrationsPage } from '@/pages/IntegrationsPage';
import { LogsPage } from '@/pages/LogsPage';
import { ConfigPage } from '@/pages/ConfigPage';

export function App() {
  return (
    <Routes>
      <Route path="/" element={<Navigate to="/ui/" replace />} />
      <Route path="/ui" element={<AppLayout />}>
        <Route index element={<DashboardPage />} />
        <Route path="triggers" element={<TriggersPage />} />
        <Route path="triggers/new" element={<TriggerEditPage />} />
        <Route path="triggers/:id" element={<TriggerDetailPage />} />
        <Route path="triggers/:id/edit" element={<TriggerEditPage />} />
        <Route path="runs" element={<RunsPage />} />
        <Route path="integrations" element={<IntegrationsPage />} />
        <Route path="logs" element={<LogsPage />} />
        <Route path="config" element={<ConfigPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/ui/" replace />} />
    </Routes>
  );
}
