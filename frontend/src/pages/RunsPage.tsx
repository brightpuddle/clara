import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useRuns } from '@/api';
import { PageHeader, StatusBadge } from '@/components/Common';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Loader2 } from 'lucide-react';

export function RunsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedId = searchParams.get('id') || '';
  const triggerFilter = searchParams.get('trigger') || '';

  const { data, isLoading, error } = useRuns(triggerFilter, selectedId);
  const [activeTab, setActiveTab] = useState<'stdout' | 'stderr' | 'tools' | 'event'>('stdout');

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
        Failed to load execution runs: {error ? (error as Error).message : 'Unknown error'}
      </div>
    );
  }

  const { runs, selected_run, tool_calls } = data;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Execution Runs"
        subtitle="Audit logs of all supervised trigger executions and tool calls"
      />

      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
        {/* Runs List */}
        <div className="lg:col-span-5">
          <Card className="rounded-2xl border shadow-xs overflow-hidden">
            <CardHeader className="px-5 py-4 border-b">
              <CardTitle className="text-base font-semibold">History</CardTitle>
              <CardDescription className="text-xs">
                {runs.length} recent executions {triggerFilter && `(filter: ${triggerFilter})`}
              </CardDescription>
            </CardHeader>
            <CardContent className="p-0">
              {runs.length === 0 ? (
                <div className="p-6 text-center text-muted-foreground text-sm">
                  No execution runs found.
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow className="text-xs uppercase bg-muted/40">
                      <TableHead className="py-2.5 pl-4">Trigger</TableHead>
                      <TableHead className="py-2.5">Time</TableHead>
                      <TableHead className="py-2.5 pr-4 text-right">Status</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {runs.map((r) => {
                      const isSelected = r.id === selectedId;
                      return (
                        <TableRow
                          key={r.id}
                          onClick={() => {
                            const newParams = new URLSearchParams(searchParams);
                            newParams.set('id', r.id);
                            setSearchParams(newParams);
                          }}
                          className={`cursor-pointer transition-colors ${
                            isSelected ? 'bg-primary/10' : 'hover:bg-muted/50'
                          }`}
                        >
                          <TableCell className="pl-4 py-2.5">
                            <div className="font-medium text-xs text-foreground truncate max-w-[120px]">
                              {r.trigger_id}
                            </div>
                            <div className="font-mono text-[10px] text-muted-foreground">
                              {r.id.slice(0, 8)}
                            </div>
                          </TableCell>
                          <TableCell className="py-2.5 text-xs text-muted-foreground">
                            {new Date(r.started_at).toLocaleTimeString()}
                          </TableCell>
                          <TableCell className="pr-4 py-2.5 text-right">
                            <StatusBadge status={r.status} />
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </div>

        {/* Selected Run Details Inspector */}
        <div className="lg:col-span-7">
          {selected_run ? (
            <Card className="rounded-2xl border shadow-xs overflow-hidden">
              <CardHeader className="px-6 py-4 border-b">
                <div className="flex items-center justify-between">
                  <div>
                    <CardTitle className="text-lg font-semibold">{selected_run.trigger_id}</CardTitle>
                    <CardDescription className="text-xs font-mono">{selected_run.id}</CardDescription>
                  </div>
                  <StatusBadge status={selected_run.status} />
                </div>
                <div className="grid grid-cols-3 gap-2 mt-3 pt-3 border-t text-xs">
                  <div>
                    <span className="text-muted-foreground">Exit Code: </span>
                    <span className="font-mono font-medium">{selected_run.exit_code}</span>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Duration: </span>
                    <span className="font-mono font-medium">{selected_run.duration_ms}ms</span>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Started: </span>
                    <span className="font-mono">{new Date(selected_run.started_at).toLocaleTimeString()}</span>
                  </div>
                </div>
              </CardHeader>

              {/* Inspector Tabs */}
              <div className="flex border-b bg-muted/20 px-6 gap-2 pt-2">
                <Button
                  variant={activeTab === 'stdout' ? 'default' : 'ghost'}
                  size="sm"
                  onClick={() => setActiveTab('stdout')}
                  className="rounded-t-lg rounded-b-none text-xs h-8"
                >
                  stdout
                </Button>
                <Button
                  variant={activeTab === 'stderr' ? 'default' : 'ghost'}
                  size="sm"
                  onClick={() => setActiveTab('stderr')}
                  className="rounded-t-lg rounded-b-none text-xs h-8"
                >
                  stderr
                </Button>
                <Button
                  variant={activeTab === 'tools' ? 'default' : 'ghost'}
                  size="sm"
                  onClick={() => setActiveTab('tools')}
                  className="rounded-t-lg rounded-b-none text-xs h-8"
                >
                  Tool Calls ({tool_calls?.length || 0})
                </Button>
                <Button
                  variant={activeTab === 'event' ? 'default' : 'ghost'}
                  size="sm"
                  onClick={() => setActiveTab('event')}
                  className="rounded-t-lg rounded-b-none text-xs h-8"
                >
                  Event Payload
                </Button>
              </div>

              <CardContent className="p-4">
                {activeTab === 'stdout' && (
                  <pre className="p-4 bg-muted/40 rounded-xl text-xs font-mono whitespace-pre-wrap max-h-96 overflow-y-auto">
                    {selected_run.stdout || '(no stdout output)'}
                  </pre>
                )}
                {activeTab === 'stderr' && (
                  <pre className="p-4 bg-muted/40 rounded-xl text-xs font-mono whitespace-pre-wrap max-h-96 overflow-y-auto text-destructive">
                    {selected_run.stderr || '(no stderr output)'}
                  </pre>
                )}
                {activeTab === 'tools' && (
                  <div className="space-y-3">
                    {(!tool_calls || tool_calls.length === 0) ? (
                      <div className="p-6 text-center text-muted-foreground text-sm">
                        No tool calls recorded during this execution.
                      </div>
                    ) : (
                      tool_calls.map((tc) => (
                        <div key={tc.id} className="p-3 border rounded-xl bg-card space-y-2 text-xs">
                          <div className="flex justify-between items-center font-mono font-semibold">
                            <span>{tc.tool_name}</span>
                            <span className="text-muted-foreground">{tc.duration_ms}ms</span>
                          </div>
                          <div>
                            <div className="text-muted-foreground text-[10px] uppercase font-semibold">Input:</div>
                            <pre className="p-2 bg-muted/30 rounded text-[11px] overflow-x-auto">{tc.input}</pre>
                          </div>
                          <div>
                            <div className="text-muted-foreground text-[10px] uppercase font-semibold">Output:</div>
                            <pre className="p-2 bg-muted/30 rounded text-[11px] overflow-x-auto">{tc.output}</pre>
                          </div>
                        </div>
                      ))
                    )}
                  </div>
                )}
                {activeTab === 'event' && (
                  <pre className="p-4 bg-muted/40 rounded-xl text-xs font-mono whitespace-pre-wrap max-h-96 overflow-y-auto">
                    {selected_run.event ? JSON.stringify(selected_run.event, null, 2) : '(no event payload)'}
                  </pre>
                )}
              </CardContent>
            </Card>
          ) : (
            <div className="p-12 text-center border-2 border-dashed rounded-2xl text-muted-foreground text-sm">
              Select an execution run from the list to view its output, logs, and tool calls.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
