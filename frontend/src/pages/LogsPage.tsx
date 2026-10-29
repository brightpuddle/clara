import { useState, useEffect, useRef } from 'react';
import { useLogs } from '@/api';
import { PageHeader } from '@/components/Common';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Loader2 } from 'lucide-react';

export function LogsPage() {
  const [levelFilter, setLevelFilter] = useState('');
  const { data, isLoading } = useLogs(levelFilter);
  const [liveLogs, setLiveLogs] = useState<string[]>([]);
  const [connected, setConnected] = useState(false);
  const preRef = useRef<HTMLPreElement>(null);

  // Sync initial log lines
  useEffect(() => {
    if (data?.lines) {
      setLiveLogs(data.lines);
    }
  }, [data]);

  // Connect SSE for streaming
  useEffect(() => {
    const url = `/ui/logs/stream${levelFilter ? `?level=${encodeURIComponent(levelFilter)}` : ''}`;
    const es = new EventSource(url);

    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);
    es.onmessage = (e) => {
      if (e.data) {
        setLiveLogs((prev) => [...prev, e.data]);
      }
    };

    return () => {
      es.close();
      setConnected(false);
    };
  }, [levelFilter]);

  // Auto scroll to bottom
  useEffect(() => {
    if (preRef.current) {
      preRef.current.scrollTop = preRef.current.scrollHeight;
    }
  }, [liveLogs]);

  const levels = ['', 'trace', 'debug', 'info', 'warn', 'error'];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Agent Observability"
        subtitle="Real-time streaming log of the Clara daemon"
      />

      {/* Filter buttons */}
      <div className="flex gap-2 flex-wrap items-center">
        <span className="text-xs font-semibold uppercase text-muted-foreground mr-2">Level:</span>
        {levels.map((lvl) => (
          <Button
            key={lvl}
            variant={levelFilter === lvl ? 'default' : 'outline'}
            size="sm"
            onClick={() => setLevelFilter(lvl)}
            className="rounded-full px-3 text-xs capitalize h-7"
          >
            {lvl || 'All'}
          </Button>
        ))}
      </div>

      {/* Log Console Output Card */}
      <Card className="rounded-2xl border shadow-xs overflow-hidden">
        <CardHeader className="px-6 py-4 border-b flex flex-row items-center justify-between">
          <div className="flex items-center gap-2">
            <span
              className={`w-2.5 h-2.5 rounded-full ${
                connected ? 'bg-emerald-500 animate-pulse' : 'bg-amber-500'
              }`}
            />
            <CardTitle className="text-xs font-semibold uppercase tracking-wider">
              Console Output
            </CardTitle>
          </div>
          <span className="text-xs font-mono text-muted-foreground" id="log-status">
            {connected ? 'connected' : 'connecting...'}
          </span>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading && liveLogs.length === 0 ? (
            <div className="flex h-64 items-center justify-center">
              <Loader2 className="h-8 w-8 animate-spin text-primary" />
            </div>
          ) : (
            <pre
              id="log-output"
              ref={preRef}
              className="text-xs font-mono p-5 min-h-80 max-h-[70vh] overflow-y-auto whitespace-pre-wrap bg-muted/30 text-foreground"
            >
              {liveLogs.map((l, i) => (
                <div key={i}>{l}</div>
              ))}
            </pre>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
