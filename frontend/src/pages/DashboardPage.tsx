import { Link } from 'react-router-dom';
import { useDashboard } from '@/api';
import { PageHeader, StatCard, StatusBadge } from '@/components/Common';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Loader2 } from 'lucide-react';

export function DashboardPage() {
  const { data, isLoading, error } = useDashboard();

  if (isLoading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="p-4 text-destructive">
        Failed to load dashboard data: {error ? (error as Error).message : 'Unknown error'}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <PageHeader
          title="Dashboard"
          subtitle="Supervisor overview, triggers, and execution status"
        />
        <Button asChild className="rounded-full px-5 text-sm font-medium shadow-sm">
          <Link to="/ui/triggers">Manage Triggers</Link>
        </Button>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          title="Triggers"
          value={data.triggers_count}
          subtitle={`${data.event_triggers_count} event, ${data.schedule_triggers_count} sched, ${data.worker_triggers_count} worker, ${data.manual_triggers_count} manual`}
        />
        <StatCard
          title="Workers"
          value={data.worker_triggers_count}
          subtitle="supervised background"
        />
        <StatCard
          title="MCP Tools"
          value={data.tools_count}
          subtitle="registered callable tools"
        />
        <StatCard
          title="Sensors / MCP"
          value={data.integrations_count}
          subtitle="integrations active"
        />
      </div>

      {/* Configured Triggers Table */}
      <Card className="rounded-2xl border shadow-xs overflow-hidden">
        <CardHeader className="px-6 py-4 border-b flex flex-row items-center justify-between">
          <div>
            <CardTitle className="text-lg font-semibold">Configured Triggers</CardTitle>
            <CardDescription className="text-sm">Event rules, schedules, and worker processes</CardDescription>
          </div>
          <Button variant="ghost" size="sm" asChild className="rounded-full text-primary font-medium">
            <Link to="/ui/triggers">View all &rarr;</Link>
          </Button>
        </CardHeader>
        <CardContent className="p-0">
          {(data.triggers || []).length === 0 ? (
            <div className="p-8 text-center text-muted-foreground text-sm">
              No triggers configured.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="text-xs uppercase bg-muted/40">
                  <TableHead className="py-3 pl-6">Trigger ID</TableHead>
                  <TableHead className="py-3">Type</TableHead>
                  <TableHead className="py-3">Match / Schedule</TableHead>
                  <TableHead className="py-3">Command</TableHead>
                  <TableHead className="py-3 pr-6 text-right"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(data.triggers || []).map((t) => (
                  <TableRow key={t.id} className="hover:bg-muted/50 transition-colors">
                    <TableCell className="pl-6 py-3">
                      <div className="font-medium text-sm text-foreground">{t.id}</div>
                      {t.name && t.name !== t.id && (
                        <div className="text-xs text-muted-foreground">{t.name}</div>
                      )}
                    </TableCell>
                    <TableCell className="py-3">
                      <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold bg-secondary text-secondary-foreground">
                        {t.type}
                      </span>
                    </TableCell>
                    <TableCell className="py-3 font-mono text-xs">
                      {t.type === 'schedule'
                        ? t.schedule
                        : t.type === 'event'
                        ? t.match?.field
                          ? `${t.match.field} ${t.match.op} ${t.match.value}`
                          : '*'
                        : t.type === 'worker'
                        ? `restart:${t.action.restart || 'always'}`
                        : 'manual'}
                    </TableCell>
                    <TableCell className="text-xs font-mono py-3 truncate max-w-xs">
                      {t.action.exec}
                    </TableCell>
                    <TableCell className="pr-6 py-3 text-right">
                      <Button variant="ghost" size="sm" asChild className="rounded-full text-primary h-7 text-xs">
                        <Link to={`/ui/triggers/${t.id}`}>Details &rarr;</Link>
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Recent Script Executions */}
      <Card className="rounded-2xl border shadow-xs overflow-hidden">
        <CardHeader className="px-6 py-4 border-b flex flex-row items-center justify-between">
          <div>
            <CardTitle className="text-lg font-semibold">Recent Script Executions</CardTitle>
            <CardDescription className="text-sm">Supervised script runs and exit codes</CardDescription>
          </div>
          <Button variant="ghost" size="sm" asChild className="rounded-full text-primary font-medium">
            <Link to="/ui/runs">View all &rarr;</Link>
          </Button>
        </CardHeader>
        <CardContent className="p-0">
          {(data.recent_runs || []).length === 0 ? (
            <div className="p-8 text-center text-muted-foreground text-sm">
              No executions recorded yet.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="text-xs uppercase bg-muted/40">
                  <TableHead className="py-3 pl-6">Run ID</TableHead>
                  <TableHead className="py-3">Trigger</TableHead>
                  <TableHead className="py-3">Started</TableHead>
                  <TableHead className="py-3">Duration</TableHead>
                  <TableHead className="py-3">Status</TableHead>
                  <TableHead className="py-3 pr-6 text-right"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(data.recent_runs || []).map((r) => (
                  <TableRow key={r.id} className="hover:bg-muted/50 transition-colors">
                    <TableCell className="pl-6 py-3 font-mono text-xs">{r.id.slice(0, 8)}</TableCell>
                    <TableCell className="py-3 text-sm font-medium">{r.trigger_id}</TableCell>
                    <TableCell className="py-3 text-xs text-muted-foreground">
                      {new Date(r.started_at).toLocaleTimeString()}
                    </TableCell>
                    <TableCell className="py-3 text-xs font-mono">{r.duration_ms}ms</TableCell>
                    <TableCell className="py-3">
                      <StatusBadge status={r.status} />
                    </TableCell>
                    <TableCell className="pr-6 py-3 text-right">
                      <Button variant="ghost" size="sm" asChild className="rounded-full text-primary h-7 text-xs">
                        <Link to={`/ui/runs?id=${r.id}`}>Inspect &rarr;</Link>
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
