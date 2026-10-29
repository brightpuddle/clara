import { useIntegrations } from '@/api';
import { PageHeader, StatusBadge } from '@/components/Common';
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

export function IntegrationsPage() {
  const { data, isLoading, error } = useIntegrations();

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
        Failed to load integrations: {error ? (error as Error).message : 'Unknown error'}
      </div>
    );
  }

  const { plugins, mcp_servers } = data;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Integrations"
        subtitle="Sensor plugins and external MCP tool providers"
      />

      {/* Sensor Integrations */}
      <Card className="rounded-2xl border shadow-xs overflow-hidden">
        <CardHeader className="px-6 py-4 border-b">
          <CardTitle className="text-lg font-semibold">Sensor Integration Plugins</CardTitle>
          <CardDescription className="text-sm">
            Native Go plugins exposing event sensors and RPC tools
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {(plugins || []).length === 0 ? (
            <div className="p-8 text-center text-muted-foreground text-sm">
              No native plugins configured.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="text-xs uppercase bg-muted/40">
                  <TableHead className="py-3 pl-6">Plugin Name</TableHead>
                  <TableHead className="py-3">Description</TableHead>
                  <TableHead className="py-3 pr-6 text-right">Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(plugins || []).map((p, idx) => (
                  <TableRow key={p.name || idx} className="hover:bg-muted/50 transition-colors">
                    <TableCell className="pl-6 py-3 font-mono text-sm font-medium">
                      {p.name}
                    </TableCell>
                    <TableCell className="py-3 text-sm text-muted-foreground">
                      {p.description || '-'}
                    </TableCell>
                    <TableCell className="pr-6 py-3 text-right">
                      <StatusBadge status={p.status || 'running'} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* MCP Servers */}
      <Card className="rounded-2xl border shadow-xs overflow-hidden">
        <CardHeader className="px-6 py-4 border-b">
          <CardTitle className="text-lg font-semibold">MCP Servers</CardTitle>
          <CardDescription className="text-sm">
            Model Context Protocol endpoints connected to Clara
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {(mcp_servers || []).length === 0 ? (
            <div className="p-8 text-center text-muted-foreground text-sm">
              No MCP servers configured.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="text-xs uppercase bg-muted/40">
                  <TableHead className="py-3 pl-6">Server Identifier</TableHead>
                  <TableHead className="py-3 pr-6 text-right">Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(mcp_servers || []).map((s, idx) => (
                  <TableRow key={s.name || idx} className="hover:bg-muted/50 transition-colors">
                    <TableCell className="pl-6 py-3 font-mono text-sm font-medium">
                      {s.name}
                    </TableCell>
                    <TableCell className="pr-6 py-3 text-right">
                      <StatusBadge status={s.status} />
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
