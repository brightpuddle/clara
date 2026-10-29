import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useTriggers, useRunTrigger, useDeleteTrigger } from '@/api';
import { PageHeader } from '@/components/Common';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Loader2, Play, Trash2, Plus, Search } from 'lucide-react';

export function TriggersPage() {
  const { data, isLoading, error } = useTriggers();
  const runMutation = useRunTrigger();
  const deleteMutation = useDeleteTrigger();
  const [search, setSearch] = useState('');
  const [filterType, setFilterType] = useState<string>('all');

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
        Failed to load triggers: {error ? (error as Error).message : 'Unknown error'}
      </div>
    );
  }

  const triggers = data.triggers.filter((t) => {
    const matchesSearch =
      t.id.toLowerCase().includes(search.toLowerCase()) ||
      (t.name && t.name.toLowerCase().includes(search.toLowerCase())) ||
      (t.description && t.description.toLowerCase().includes(search.toLowerCase()));
    const matchesType = filterType === 'all' || t.type === filterType;
    return matchesSearch && matchesType;
  });

  const handleDelete = (id: string) => {
    if (confirm(`Are you sure you want to delete trigger "${id}"?`)) {
      deleteMutation.mutate(id);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <PageHeader
          title="Triggers"
          subtitle="Event match rules, schedules, and background worker lifecycles"
        />
        <Button asChild className="rounded-full px-5 text-sm font-medium shadow-sm gap-2">
          <Link to="/ui/triggers/new">
            <Plus className="h-4 w-4" />
            Create Trigger
          </Link>
        </Button>
      </div>

      {/* Filter and Search Bar */}
      <div className="flex flex-col sm:flex-row gap-3 items-stretch sm:items-center justify-between">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search triggers..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-9 rounded-full bg-card"
          />
        </div>
        <div className="flex gap-1.5 flex-wrap">
          {['all', 'event', 'schedule', 'worker', 'manual'].map((type) => (
            <Button
              key={type}
              variant={filterType === type ? 'default' : 'outline'}
              size="sm"
              onClick={() => setFilterType(type)}
              className="rounded-full capitalize text-xs"
            >
              {type}
            </Button>
          ))}
        </div>
      </div>

      {/* Triggers Table */}
      <Card className="rounded-2xl border shadow-xs overflow-hidden">
        <CardHeader className="px-6 py-4 border-b">
          <CardTitle className="text-lg font-semibold">Active Trigger Definitions</CardTitle>
          <CardDescription className="text-sm">Configured in tasks directory</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {triggers.length === 0 ? (
            <div className="p-8 text-center text-muted-foreground text-sm">
              No matching triggers found.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="text-xs uppercase bg-muted/40">
                  <TableHead className="py-3 pl-6">Trigger ID / Name</TableHead>
                  <TableHead className="py-3">Type</TableHead>
                  <TableHead className="py-3">Match / Schedule</TableHead>
                  <TableHead className="py-3">Action Command</TableHead>
                  <TableHead className="py-3 pr-6 text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {triggers.map((t) => (
                  <TableRow key={t.id} className="hover:bg-muted/50 transition-colors">
                    <TableCell className="pl-6 py-3">
                      <Link to={`/ui/triggers/${t.id}`} className="font-semibold text-sm hover:underline text-foreground">
                        {t.id}
                      </Link>
                      {t.name && t.name !== t.id && (
                        <div className="text-xs text-muted-foreground">{t.name}</div>
                      )}
                    </TableCell>
                    <TableCell className="py-3">
                      <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold bg-secondary text-secondary-foreground">
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
                      <div className="flex items-center justify-end gap-1.5">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => runMutation.mutate(t.id)}
                          disabled={runMutation.isPending}
                          className="rounded-full h-8 px-2.5 text-xs gap-1"
                          title="Run trigger now"
                        >
                          <Play className="h-3 w-3" />
                          Run
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          asChild
                          className="rounded-full h-8 px-2.5 text-xs"
                        >
                          <Link to={`/ui/triggers/${t.id}`}>View</Link>
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => handleDelete(t.id)}
                          className="rounded-full h-8 w-8 text-destructive hover:text-destructive"
                          title="Delete trigger"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
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
