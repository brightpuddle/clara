import { useParams, Link } from 'react-router-dom';
import { useTrigger, useRunTrigger, useDeleteTrigger } from '@/api';
import { PageHeader, StatusBadge } from '@/components/Common';
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
import { Loader2, Play, Trash2, Edit3, ArrowLeft } from 'lucide-react';

export function TriggerDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { data, isLoading, error } = useTrigger(id || '');
  const runMutation = useRunTrigger();
  const deleteMutation = useDeleteTrigger();

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
        Failed to load trigger: {error ? (error as Error).message : 'Unknown error'}
      </div>
    );
  }

  const { trigger, recent_runs, file_path } = data;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <div className="flex items-center gap-3">
          <Button variant="ghost" size="icon" asChild className="rounded-full">
            <Link to="/ui/triggers">
              <ArrowLeft className="h-5 w-5" />
            </Link>
          </Button>
          <PageHeader
            title={trigger.name || trigger.id}
            subtitle={trigger.description || `Trigger: ${trigger.id}`}
          />
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            onClick={() => runMutation.mutate(trigger.id)}
            disabled={runMutation.isPending}
            className="rounded-full gap-1.5"
          >
            <Play className="h-4 w-4" />
            Run Trigger
          </Button>
          <Button asChild className="rounded-full gap-1.5">
            <Link to={`/ui/triggers/${trigger.id}/edit`}>
              <Edit3 className="h-4 w-4" />
              Edit
            </Link>
          </Button>
          <Button
            variant="destructive"
            size="icon"
            onClick={() => {
              if (confirm('Delete this trigger?')) {
                deleteMutation.mutate(trigger.id, {
                  onSuccess: () => {
                    window.location.href = '/ui/triggers';
                  },
                });
              }
            }}
            className="rounded-full"
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* Details Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <Card className="rounded-2xl border shadow-xs">
          <CardHeader>
            <CardTitle className="text-base font-semibold">General Information</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            <div className="flex justify-between border-b pb-2">
              <span className="text-muted-foreground">ID</span>
              <span className="font-mono font-medium">{trigger.id}</span>
            </div>
            <div className="flex justify-between border-b pb-2">
              <span className="text-muted-foreground">Type</span>
              <span className="font-medium capitalize">{trigger.type}</span>
            </div>
            <div className="flex justify-between border-b pb-2">
              <span className="text-muted-foreground">Status</span>
              <span className="font-medium">{trigger.enabled ? 'Enabled' : 'Disabled'}</span>
            </div>
            {file_path && (
              <div className="flex justify-between border-b pb-2">
                <span className="text-muted-foreground">File Path</span>
                <span className="font-mono text-xs truncate max-w-xs">{file_path}</span>
              </div>
            )}
            {trigger.schedule && (
              <div className="flex justify-between border-b pb-2">
                <span className="text-muted-foreground">Schedule</span>
                <span className="font-mono">{trigger.schedule}</span>
              </div>
            )}
          </CardContent>
        </Card>

        <Card className="rounded-2xl border shadow-xs">
          <CardHeader>
            <CardTitle className="text-base font-semibold">Action Configuration</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            <div className="flex justify-between border-b pb-2">
              <span className="text-muted-foreground">Command</span>
              <span className="font-mono text-xs">{trigger.action.exec}</span>
            </div>
            {trigger.action.args && trigger.action.args.length > 0 && (
              <div className="flex justify-between border-b pb-2">
                <span className="text-muted-foreground">Arguments</span>
                <span className="font-mono text-xs">{trigger.action.args.join(' ')}</span>
              </div>
            )}
            {trigger.action.restart && (
              <div className="flex justify-between border-b pb-2">
                <span className="text-muted-foreground">Restart Policy</span>
                <span className="font-medium">{trigger.action.restart}</span>
              </div>
            )}
            {trigger.action.timeout && (
              <div className="flex justify-between border-b pb-2">
                <span className="text-muted-foreground">Timeout</span>
                <span className="font-mono">{trigger.action.timeout}ns</span>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Match Rule AST View */}
      {trigger.match && (
        <Card className="rounded-2xl border shadow-xs">
          <CardHeader>
            <CardTitle className="text-base font-semibold">Match Condition AST</CardTitle>
            <CardDescription className="text-xs">Boolean rule tree evaluated against incoming CloudEvents</CardDescription>
          </CardHeader>
          <CardContent>
            <pre className="p-4 bg-muted/40 rounded-xl text-xs font-mono overflow-x-auto">
              {JSON.stringify(trigger.match, null, 2)}
            </pre>
          </CardContent>
        </Card>
      )}

      {/* Recent Runs */}
      <Card className="rounded-2xl border shadow-xs overflow-hidden">
        <CardHeader className="px-6 py-4 border-b">
          <CardTitle className="text-lg font-semibold">Recent Executions</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {recent_runs.length === 0 ? (
            <div className="p-6 text-center text-muted-foreground text-sm">
              No executions recorded for this trigger yet.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="text-xs uppercase bg-muted/40">
                  <TableHead className="py-3 pl-6">Run ID</TableHead>
                  <TableHead className="py-3">Started</TableHead>
                  <TableHead className="py-3">Duration</TableHead>
                  <TableHead className="py-3">Exit Code</TableHead>
                  <TableHead className="py-3 pr-6 text-right">Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {recent_runs.map((r) => (
                  <TableRow key={r.id}>
                    <TableCell className="pl-6 py-3 font-mono text-xs">{r.id.slice(0, 8)}</TableCell>
                    <TableCell className="py-3 text-xs text-muted-foreground">
                      {new Date(r.started_at).toLocaleString()}
                    </TableCell>
                    <TableCell className="py-3 text-xs font-mono">{r.duration_ms}ms</TableCell>
                    <TableCell className="py-3 text-xs font-mono">{r.exit_code}</TableCell>
                    <TableCell className="pr-6 py-3 text-right">
                      <StatusBadge status={r.status} />
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
