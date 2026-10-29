import React, { useState, useEffect } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { useTrigger, useCreateTrigger, useUpdateTrigger } from '@/api';
import { PageHeader } from '@/components/Common';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { ArrowLeft, Loader2, Save } from 'lucide-react';

export function TriggerEditPage() {
  const { id } = useParams<{ id: string }>();
  const isNew = !id;
  const navigate = useNavigate();

  const { data: existingData, isLoading } = useTrigger(id || '');
  const createMutation = useCreateTrigger();
  const updateMutation = useUpdateTrigger(id || '');

  const [tab, setTab] = useState<'structured' | 'raw'>('structured');
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  // Form State
  const [formId, setFormId] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [type, setType] = useState('event');
  const [schedule, setSchedule] = useState('');
  const [debounce, setDebounce] = useState('');
  const [throttle, setThrottle] = useState('');
  const [ruleField, setRuleField] = useState('type');
  const [ruleOp, setRuleOp] = useState('equals');
  const [ruleValue, setRuleValue] = useState('');
  const [exec, setExec] = useState('bun run scripts/handle.ts');
  const [timeout, setTimeoutVal] = useState('30s');
  const [restart, setRestart] = useState('always');
  const [rawYaml, setRawYaml] = useState('');

  useEffect(() => {
    if (existingData?.trigger) {
      const t = existingData.trigger;
      setFormId(t.id);
      setName(t.name || '');
      setDescription(t.description || '');
      setEnabled(t.enabled);
      setType(t.type);
      setSchedule(t.schedule || '');
      setExec(t.action.exec || '');
      if (t.match) {
        setRuleField(t.match.field || 'type');
        setRuleOp(t.match.op || 'equals');
        setRuleValue(String(t.match.value || ''));
      }
      if (t.debounce) setDebounce(`${t.debounce / 1e9}s`);
      if (t.throttle) setThrottle(`${t.throttle / 1e9}s`);
      if (t.action.restart) setRestart(t.action.restart);
      if (t.action.timeout) setTimeoutVal(`${t.action.timeout / 1e9}s`);
    }
  }, [existingData]);

  if (!isNew && isLoading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
      </div>
    );
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMsg(null);

    try {
      if (tab === 'raw') {
        if (!rawYaml.trim()) {
          setErrorMsg('YAML content is required');
          return;
        }
        if (isNew) {
          await createMutation.mutateAsync({ yaml: rawYaml });
        } else {
          await updateMutation.mutateAsync({ yaml: rawYaml });
        }
      } else {
        const payload: any = {
          id: formId.trim(),
          name: name.trim(),
          description: description.trim(),
          enabled,
          type,
          schedule: type === 'schedule' ? schedule : undefined,
          debounce: debounce || undefined,
          throttle: throttle || undefined,
          rule_field: type === 'event' ? ruleField : undefined,
          rule_op: type === 'event' ? ruleOp : undefined,
          rule_value: type === 'event' ? ruleValue : undefined,
          exec: exec.trim(),
          timeout,
          restart: type === 'worker' ? restart : undefined,
          args: [],
          env: [],
        };

        const triggerJSON = JSON.stringify(payload);
        if (isNew) {
          await createMutation.mutateAsync({ trigger_json: triggerJSON });
        } else {
          await updateMutation.mutateAsync({ trigger_json: triggerJSON });
        }
      }
      navigate('/ui/triggers');
    } catch (err: any) {
      setErrorMsg(err.message || 'Failed to save trigger');
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="icon" asChild className="rounded-full">
          <Link to="/ui/triggers">
            <ArrowLeft className="h-5 w-5" />
          </Link>
        </Button>
        <PageHeader
          title={isNew ? 'Create Trigger' : `Edit Trigger: ${id}`}
          subtitle="Configure event routing rules or cron schedules"
        />
      </div>

      {errorMsg && (
        <div className="p-4 rounded-xl bg-destructive/10 text-destructive text-sm font-medium">
          {errorMsg}
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
          Structured Form
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

      <form onSubmit={handleSave} className="flex flex-col gap-6">
        {tab === 'structured' ? (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <Card className="rounded-2xl border shadow-xs">
              <CardHeader>
                <CardTitle className="text-base font-semibold">General Metadata</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <label className="text-xs font-semibold uppercase text-muted-foreground">ID</label>
                  <Input
                    value={formId}
                    onChange={(e) => setFormId(e.target.value)}
                    placeholder="my-trigger-id"
                    required
                    className="mt-1"
                  />
                </div>
                <div>
                  <label className="text-xs font-semibold uppercase text-muted-foreground">Name</label>
                  <Input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Human readable title"
                    className="mt-1"
                  />
                </div>
                <div>
                  <label className="text-xs font-semibold uppercase text-muted-foreground">Description</label>
                  <Textarea
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder="Brief description of what this automation does"
                    rows={3}
                    className="mt-1"
                  />
                </div>
                <div>
                  <label className="text-xs font-semibold uppercase text-muted-foreground">Type</label>
                  <select
                    value={type}
                    onChange={(e) => setType(e.target.value)}
                    className="w-full mt-1 px-3 py-2 border rounded-md bg-background text-sm"
                  >
                    <option value="event">Event (CloudEvent AST rule)</option>
                    <option value="schedule">Schedule (Cron expression)</option>
                    <option value="worker">Worker (Supervised process)</option>
                    <option value="manual">Manual (Explicit trigger)</option>
                  </select>
                </div>
              </CardContent>
            </Card>

            <Card className="rounded-2xl border shadow-xs">
              <CardHeader>
                <CardTitle className="text-base font-semibold">Execution & Conditions</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                {type === 'event' && (
                  <div className="space-y-3 p-3 bg-muted/40 rounded-xl">
                    <div className="text-xs font-semibold text-muted-foreground uppercase">Event Match Rule</div>
                    <div className="grid grid-cols-3 gap-2">
                      <Input
                        placeholder="Field (e.g. type)"
                        value={ruleField}
                        onChange={(e) => setRuleField(e.target.value)}
                      />
                      <select
                        value={ruleOp}
                        onChange={(e) => setRuleOp(e.target.value)}
                        className="px-2 py-1 border rounded bg-background text-xs"
                      >
                        <option value="equals">equals</option>
                        <option value="contains">contains</option>
                        <option value="regex">regex</option>
                        <option value="starts_with">starts_with</option>
                      </select>
                      <Input
                        placeholder="Value"
                        value={ruleValue}
                        onChange={(e) => setRuleValue(e.target.value)}
                      />
                    </div>
                  </div>
                )}

                {type === 'schedule' && (
                  <div>
                    <label className="text-xs font-semibold uppercase text-muted-foreground">Cron Schedule</label>
                    <Input
                      value={schedule}
                      onChange={(e) => setSchedule(e.target.value)}
                      placeholder="*/5 * * * *"
                      className="mt-1 font-mono"
                    />
                  </div>
                )}

                <div>
                  <label className="text-xs font-semibold uppercase text-muted-foreground">Action Exec Command</label>
                  <Input
                    value={exec}
                    onChange={(e) => setExec(e.target.value)}
                    placeholder="bun run scripts/task.ts"
                    required
                    className="mt-1 font-mono text-sm"
                  />
                </div>

                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="text-xs font-semibold uppercase text-muted-foreground">Timeout</label>
                    <Input
                      value={timeout}
                      onChange={(e) => setTimeoutVal(e.target.value)}
                      placeholder="30s"
                      className="mt-1"
                    />
                  </div>
                  {type === 'worker' && (
                    <div>
                      <label className="text-xs font-semibold uppercase text-muted-foreground">Restart Policy</label>
                      <select
                        value={restart}
                        onChange={(e) => setRestart(e.target.value)}
                        className="w-full mt-1 px-3 py-2 border rounded-md bg-background text-sm"
                      >
                        <option value="always">always</option>
                        <option value="on_failure">on_failure</option>
                        <option value="never">never</option>
                      </select>
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>
          </div>
        ) : (
          <Card className="rounded-2xl border shadow-xs">
            <CardHeader>
              <CardTitle className="text-base font-semibold">Raw Trigger YAML</CardTitle>
            </CardHeader>
            <CardContent>
              <Textarea
                value={rawYaml}
                onChange={(e) => setRawYaml(e.target.value)}
                rows={16}
                placeholder={`id: my-trigger\nname: My Trigger\ntype: event\nmatch:\n  field: type\n  op: equals\n  value: custom.event\naction:\n  exec: bun run scripts/handle.ts`}
                className="font-mono text-xs"
              />
            </CardContent>
          </Card>
        )}

        <div className="flex justify-end gap-3">
          <Button variant="ghost" asChild className="rounded-full">
            <Link to="/ui/triggers">Cancel</Link>
          </Button>
          <Button
            type="submit"
            disabled={createMutation.isPending || updateMutation.isPending}
            className="rounded-full gap-2 px-6"
          >
            <Save className="h-4 w-4" />
            {isNew ? 'Create Trigger' : 'Save Changes'}
          </Button>
        </div>
      </form>
    </div>
  );
}
