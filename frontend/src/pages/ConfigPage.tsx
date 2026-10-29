import React, { useState, useEffect } from 'react';
import { useConfig, useSaveConfig } from '@/api';
import { PageHeader } from '@/components/Common';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Loader2, Plus, Trash2, Save } from 'lucide-react';

export function ConfigPage() {
  const { data, isLoading, error } = useConfig();
  const saveMutation = useSaveConfig();

  const [tab, setTab] = useState<'structured' | 'raw'>('structured');
  const [feedback, setFeedback] = useState<{ kind: 'success' | 'error'; msg: string } | null>(null);

  // Structured form state
  const [logLevel, setLogLevel] = useState('info');
  const [dataDir, setDataDir] = useState('');
  const [listenAddr, setListenAddr] = useState(':3333');
  const [taskDirs, setTaskDirs] = useState<string[]>([]);
  const [rawYaml, setRawYaml] = useState('');

  useEffect(() => {
    if (data) {
      setRawYaml(data.raw_yaml || '');
      if (data.structured) {
        setLogLevel(data.structured.log_level || 'info');
        setDataDir(data.structured.data_dir || '');
        if (data.structured.server) {
          setListenAddr(data.structured.server.listen_addr || ':3333');
        }
        if (Array.isArray(data.structured.task_dirs)) {
          setTaskDirs(data.structured.task_dirs);
        }
      }
    }
  }, [data]);

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
        Failed to load config: {error ? (error as Error).message : 'Unknown error'}
      </div>
    );
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setFeedback(null);

    try {
      if (tab === 'raw') {
        await saveMutation.mutateAsync({ yaml: rawYaml });
      } else {
        const sc = {
          ...(data.structured || {}),
          log_level: logLevel,
          data_dir: dataDir,
          server: {
            ...(data.structured?.server || {}),
            listen_addr: listenAddr,
          },
          task_dirs: taskDirs.filter((d) => d.trim() !== ''),
        };
        await saveMutation.mutateAsync({ config_json: JSON.stringify(sc) });
      }
      setFeedback({ kind: 'success', msg: 'Configuration saved. Restart the agent to apply changes.' });
    } catch (err: any) {
      setFeedback({ kind: 'error', msg: err.message || 'Failed to save configuration' });
    }
  };

  const addTaskDir = () => {
    setTaskDirs([...taskDirs, '']);
  };

  const removeTaskDir = (index: number) => {
    setTaskDirs(taskDirs.filter((_, i) => i !== index));
  };

  const updateTaskDir = (index: number, val: string) => {
    const next = [...taskDirs];
    next[index] = val;
    setTaskDirs(next);
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Configuration"
        subtitle={`Editing ${data.config_path || 'config.yaml'}`}
      />

      {feedback && (
        <div
          className={`p-4 rounded-xl text-sm font-medium ${
            feedback.kind === 'success'
              ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300'
              : 'bg-destructive/10 text-destructive'
          }`}
        >
          {feedback.msg}
        </div>
      )}

      {/* Editor Mode Tabs */}
      <div className="flex gap-2 border-b pb-2">
        <Button
          variant={tab === 'structured' ? 'default' : 'ghost'}
          size="sm"
          onClick={() => setTab('structured')}
          className="rounded-full"
        >
          Structured Settings
        </Button>
        <Button
          variant={tab === 'raw' ? 'default' : 'ghost'}
          size="sm"
          onClick={() => setTab('raw')}
          className="rounded-full"
        >
          Raw YAML
        </Button>
      </div>

      {tab === 'structured' ? (
        <form id="structuredForm" onSubmit={handleSave} className="flex flex-col gap-6">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            {/* Core Settings */}
            <Card className="rounded-2xl border shadow-xs">
              <CardHeader>
                <CardTitle className="text-base font-semibold">Core Daemon Settings</CardTitle>
                <CardDescription className="text-xs">Log level and storage locations</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <label className="text-xs font-semibold uppercase text-muted-foreground">Log Level</label>
                  <select
                    value={logLevel}
                    onChange={(e) => setLogLevel(e.target.value)}
                    className="w-full mt-1 px-3 py-2 border rounded-md bg-background text-sm"
                  >
                    <option value="trace">Trace</option>
                    <option value="debug">Debug</option>
                    <option value="info">Info</option>
                    <option value="warn">Warn</option>
                    <option value="error">Error</option>
                  </select>
                </div>

                <div>
                  <label className="text-xs font-semibold uppercase text-muted-foreground">Data Directory</label>
                  <Input
                    value={dataDir}
                    onChange={(e) => setDataDir(e.target.value)}
                    placeholder="~/.local/share/clara"
                    className="mt-1"
                  />
                </div>

                <div>
                  <label className="text-xs font-semibold uppercase text-muted-foreground">HTTP Server Listen Address</label>
                  <Input
                    value={listenAddr}
                    onChange={(e) => setListenAddr(e.target.value)}
                    placeholder=":3333"
                    className="mt-1 font-mono text-sm"
                  />
                </div>
              </CardContent>
            </Card>

            {/* Task Directories */}
            <Card className="rounded-2xl border shadow-xs">
              <CardHeader className="flex flex-row items-center justify-between">
                <div>
                  <CardTitle className="text-base font-semibold">Task & Trigger Directories</CardTitle>
                  <CardDescription className="text-xs">Paths searched for trigger definitions</CardDescription>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={addTaskDir}
                  className="rounded-full h-8 text-xs gap-1"
                >
                  <Plus className="h-3 w-3" />
                  + Add Directory
                </Button>
              </CardHeader>
              <CardContent className="space-y-3">
                {taskDirs.length === 0 ? (
                  <div className="text-xs text-muted-foreground">No custom task directories added.</div>
                ) : (
                  taskDirs.map((dir, idx) => (
                    <div key={idx} className="flex gap-2 items-center">
                      <Input
                        value={dir}
                        onChange={(e) => updateTaskDir(idx, e.target.value)}
                        placeholder="/path/to/tasks"
                        className="font-mono text-xs"
                      />
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        onClick={() => removeTaskDir(idx)}
                        className="text-destructive h-8 w-8 shrink-0"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  ))
                )}
              </CardContent>
            </Card>
          </div>

          <div className="flex justify-end">
            <Button
              type="submit"
              disabled={saveMutation.isPending}
              className="rounded-full gap-2 px-6"
            >
              <Save className="h-4 w-4" />
              Save Configuration
            </Button>
          </div>
        </form>
      ) : (
        <form onSubmit={handleSave} className="flex flex-col gap-4">
          <Card className="rounded-2xl border shadow-xs">
            <CardHeader>
              <CardTitle className="text-base font-semibold">Raw Configuration YAML</CardTitle>
            </CardHeader>
            <CardContent>
              <Textarea
                name="yaml"
                value={rawYaml}
                onChange={(e) => setRawYaml(e.target.value)}
                rows={18}
                className="font-mono text-xs leading-relaxed"
              />
            </CardContent>
          </Card>
          <div className="flex justify-end">
            <Button
              type="submit"
              disabled={saveMutation.isPending}
              className="rounded-full gap-2 px-6"
            >
              <Save className="h-4 w-4" />
              Save YAML
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}
