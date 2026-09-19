import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { GitBranch, Play, Plus, ScrollText, Trash2 } from "lucide-react";
import {
  api,
  type PipelineConfig,
  type PipelineConfigInput,
  type PipelineRun,
} from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { useToast } from "@/components/ui/toast";

const NAME_RE = /^[A-Za-z0-9_.-]+$/;
const TRIGGERS = ["patchset-created", "*"];

function statusVariant(status: string) {
  switch (status) {
    case "SUCCESS":
      return "success" as const;
    case "FAILURE":
    case "ERROR":
      return "destructive" as const;
    default:
      return "secondary" as const;
  }
}

function duration(started?: string, finished?: string) {
  if (!started || !finished) return "";
  const ms = Date.parse(finished) - Date.parse(started);
  if (!Number.isFinite(ms) || ms < 0) return "";
  if (ms < 1000) return `${ms}ms`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m${String(s % 60).padStart(2, "0")}s`;
}

export default function ProjectPipelinesPanel({ project, canEdit }: { project: string; canEdit: boolean }) {
  const { t } = useTranslation("pipelines");
  const toast = useToast();
  const [configs, setConfigs] = useState<PipelineConfig[] | null>(null);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<PipelineConfig | null>(null);
  const [creating, setCreating] = useState(false);
  const [runsFor, setRunsFor] = useState<PipelineConfig | null>(null);

  const load = useCallback(() =>
    api.listPipelines(project).then((c) => setConfigs(c ?? [])).catch((err) => {
      setError((err as Error).message);
      setConfigs([]);
    }), [project]);

  useEffect(() => {
    setConfigs(null);
    load();
  }, [load]);

  const remove = async (cfg: PipelineConfig) => {
    if (!confirm(t("deleteConfirm", { name: cfg.name }))) return;
    setError("");
    try {
      await api.deletePipeline(cfg.id);
      await load();
      toast(t("toastDeleted"), "success");
    } catch (err) {
      setError((err as Error).message);
      toast((err as Error).message, "error");
    }
  };

  if (configs === null) {
    return <Skeleton className="h-32 w-full" />;
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <h2 className="text-sm font-semibold">{t("title")}</h2>
        {canEdit && (
          <Button size="sm" className="ml-auto" onClick={() => setCreating(true)}>
            <Plus className="size-4" />
            {t("newPipeline")}
          </Button>
        )}
      </div>
      <p className="text-xs text-muted-foreground">{t("explainer")}</p>
      {error && <p className="text-sm text-destructive">{error}</p>}

      {configs.length === 0 ? (
        <div className="rounded-lg border border-dashed p-10 text-center text-sm text-muted-foreground">
          {t("empty")}
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {configs.map((cfg) => (
            <Card key={cfg.id} className="min-w-0 gap-3 py-4">
              <CardHeader className="px-4">
                <CardTitle className="flex flex-wrap items-center gap-2 text-base">
                  <GitBranch className="size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 truncate">{cfg.name}</span>
                  <Badge variant={cfg.enabled ? "success" : "muted"} className="text-xs">
                    {cfg.enabled ? t("enabled") : t("disabled")}
                  </Badge>
                  {cfg.required && <Badge variant="outline" className="text-xs">{t("required")}</Badge>}
                  {!!cfg.vote_label && (
                    <Badge variant="secondary" className="text-xs">
                      {cfg.vote_label} {cfg.pass_vote >= 0 ? "+" : ""}{cfg.pass_vote} / {cfg.fail_vote}
                    </Badge>
                  )}
                </CardTitle>
                <CardDescription className="flex flex-wrap gap-x-3 text-xs">
                  <span>{t("triggers")}: {cfg.triggers?.join(", ") || "patchset-created"}</span>
                  <span>{t("stepCount", { count: cfg.steps?.length ?? 0 })}</span>
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-2 px-4">
                <pre className="max-h-32 overflow-auto rounded bg-muted px-3 py-2 font-mono text-xs">
                  {(cfg.steps ?? []).map((s, i) => `${i + 1}. ${s}`).join("\n") || "—"}
                </pre>
                <div className="flex items-center gap-2">
                  <Button size="sm" variant="outline" onClick={() => setRunsFor(cfg)}>
                    <ScrollText className="size-3.5" />
                    {t("runs")}
                  </Button>
                  {canEdit && (
                    <>
                      <Button size="sm" variant="outline" onClick={() => setEditing(cfg)}>{t("common:action.edit")}</Button>
                      <Button size="sm" variant="destructive" onClick={() => remove(cfg)}>
                        <Trash2 className="size-3.5" />
                      </Button>
                    </>
                  )}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {(creating || editing) && (
        <PipelineFormDialog
          project={project}
          config={editing}
          onClose={() => { setCreating(false); setEditing(null); }}
          onSaved={async () => { setCreating(false); setEditing(null); await load(); }}
        />
      )}
      {runsFor && <RunsDialog config={runsFor} onClose={() => setRunsFor(null)} />}
    </div>
  );
}

function parseEnv(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const t = line.trim();
    if (!t) continue;
    const i = t.indexOf("=");
    if (i <= 0) continue;
    out[t.slice(0, i).trim()] = t.slice(i + 1).trim();
  }
  return out;
}

function envToText(env?: Record<string, string>) {
  return Object.entries(env ?? {}).map(([k, v]) => `${k}=${v}`).join("\n");
}

function PipelineFormDialog({ project, config, onClose, onSaved }: {
  project: string;
  config: PipelineConfig | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation("pipelines");
  const toast = useToast();
  const [name, setName] = useState(config?.name ?? "");
  const [steps, setSteps] = useState((config?.steps ?? [""]).join("\n"));
  const [env, setEnv] = useState(envToText(config?.env));
  const [triggers, setTriggers] = useState<string[]>(config?.triggers?.length ? config.triggers : ["patchset-created"]);
  const [voteLabel, setVoteLabel] = useState(config?.vote_label ?? "Verified");
  const [passVote, setPassVote] = useState(String(config?.pass_vote ?? 1));
  const [failVote, setFailVote] = useState(String(config?.fail_vote ?? -1));
  const [required, setRequired] = useState(config?.required ?? false);
  const [enabled, setEnabled] = useState(config?.enabled ?? true);
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const stepList = steps.split("\n").map((s) => s.trim()).filter(Boolean);
  const nameInvalid = !NAME_RE.test(name.trim());
  const showNameError = touched && nameInvalid;
  const canSubmit = !nameInvalid && stepList.length > 0 && stepList.length <= 50;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!canSubmit) {
      setError(stepList.length ? t("nameRule") : t("stepsRequired"));
      return;
    }
    const body: PipelineConfigInput = {
      project,
      name: name.trim(),
      triggers,
      steps: stepList,
      env: parseEnv(env),
      vote_label: voteLabel.trim(),
      pass_vote: Number(passVote) || 0,
      fail_vote: Number(failVote) || 0,
      required,
      enabled,
    };
    setBusy(true);
    setError("");
    try {
      if (config) {
        await api.updatePipeline(config.id, body);
        toast(t("toastSaved"), "success");
      } else {
        await api.createPipeline(body);
        toast(t("toastCreated"), "success");
      }
      await onSaved();
    } catch (err) {
      setError((err as Error).message);
      toast((err as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{config ? t("editPipeline") : t("createPipeline")}</DialogTitle>
          <DialogDescription>{t("formDescription")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="pl-name">{t("pipelineName")}</Label>
            <Input id="pl-name" value={name} onChange={(e) => setName(e.target.value)} onBlur={() => setTouched(true)}
              placeholder="build" required disabled={!!config} aria-invalid={showNameError} />
            <p className={`text-xs ${showNameError ? "text-destructive" : "text-muted-foreground"}`}>{t("nameRule")}</p>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="pl-steps">{t("steps")}</Label>
            <Textarea id="pl-steps" value={steps} onChange={(e) => setSteps(e.target.value)} rows={6}
              className="font-mono text-xs" placeholder={"make defconfig\nmake -j$(nproc)"} />
            <p className={`text-xs ${stepList.length === 0 ? "text-destructive" : "text-muted-foreground"}`}>
              {t("stepsHint", { count: stepList.length })}
            </p>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="pl-env">{t("env")}</Label>
            <Textarea id="pl-env" value={env} onChange={(e) => setEnv(e.target.value)} rows={3}
              className="font-mono text-xs" placeholder="CC=gcc" />
          </div>
          <div className="flex flex-wrap items-end gap-4">
            <div className="flex flex-col gap-1">
              <Label>{t("triggers")}</Label>
              {TRIGGERS.map((tr) => (
                <label key={tr} className="flex cursor-pointer items-center gap-2 text-sm">
                  <input type="checkbox" checked={triggers.includes(tr)} onChange={(e) =>
                    setTriggers(e.target.checked ? [...triggers, tr] : triggers.filter((x) => x !== tr))} />
                  <code className="text-xs">{tr}</code>
                </label>
              ))}
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="pl-vote">{t("voteLabel")}</Label>
              <Input id="pl-vote" value={voteLabel} onChange={(e) => setVoteLabel(e.target.value)}
                placeholder="Verified" className="w-32" />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="pl-pass">{t("passVote")}</Label>
              <Input id="pl-pass" type="number" value={passVote} onChange={(e) => setPassVote(e.target.value)} className="w-20" />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="pl-fail">{t("failVote")}</Label>
              <Input id="pl-fail" type="number" value={failVote} onChange={(e) => setFailVote(e.target.value)} className="w-20" />
            </div>
          </div>
          <div className="flex flex-wrap gap-4">
            <label className="flex cursor-pointer items-center gap-2 text-sm">
              <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
              {t("enabled")}
            </label>
            <label className="flex cursor-pointer items-center gap-2 text-sm">
              <input type="checkbox" checked={required} onChange={(e) => setRequired(e.target.checked)} />
              {t("requiredHint")}
            </label>
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <DialogFooter>
            <Button type="submit" disabled={busy || (touched && !canSubmit)}>
              {busy ? t("common:action.saving") : config ? t("common:action.save") : t("common:action.create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RunsDialog({ config, onClose }: { config: PipelineConfig; onClose: () => void }) {
  const { t } = useTranslation("pipelines");
  const toast = useToast();
  const [runs, setRuns] = useState<PipelineRun[] | null>(null);
  const [logFor, setLogFor] = useState<PipelineRun | null>(null);
  const [triggerNum, setTriggerNum] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = () => api.listPipelineRuns(config.id).then((r) => setRuns(r ?? [])).catch((e) => setError((e as Error).message));
  useEffect(() => { load(); /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [config.id]);

  const showLog = async (run: PipelineRun) => {
    setError("");
    try {
      setLogFor(await api.getPipelineRun(run.id));
    } catch (err) {
      setError((err as Error).message);
      toast((err as Error).message, "error");
    }
  };

  const trigger = async () => {
    const num = Number(triggerNum.trim());
    if (!num || busy) return;
    setBusy(true);
    setError("");
    try {
      await api.triggerPipeline(config.id, num);
      toast(t("toastTriggered"), "success");
      setTriggerNum("");
      setTimeout(load, 800);
    } catch (err) {
      setError((err as Error).message);
      toast((err as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("runsTitle", { name: config.name })}</DialogTitle>
          <DialogDescription>{t("runsDescription")}</DialogDescription>
        </DialogHeader>
        {error && <p className="text-sm text-destructive">{error}</p>}
        <div className="flex items-end gap-2">
          <div className="flex flex-1 flex-col gap-1">
            <Label className="text-xs">{t("triggerOnChange")}</Label>
            <Input value={triggerNum} onChange={(e) => setTriggerNum(e.target.value)} placeholder="1234" className="w-40" />
          </div>
          <Button size="sm" variant="outline" onClick={trigger} disabled={!triggerNum.trim() || busy}>
            <Play className="size-3.5" />
            {busy ? t("common:action.saving") : t("run")}
          </Button>
        </div>
        {runs === null ? (
          <Skeleton className="h-24 w-full" />
        ) : runs.length === 0 ? (
          <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">{t("noRuns")}</p>
        ) : (
          <div className="flex max-h-72 flex-col gap-1 overflow-y-auto">
            {runs.map((run) => (
              <div key={run.id} className="flex flex-wrap items-center gap-2 border-b px-1 py-2 text-sm last:border-0">
                <Badge variant={statusVariant(run.status)} className="text-xs">{run.status}</Badge>
                <span className="text-muted-foreground">
                  <Link className="hover:underline" to={`/c/${run.change_number}`}>#{run.change_number}</Link>
                  {" / "}{t("patchSet", { n: run.patch_set })}
                </span>
                <span className="text-xs text-muted-foreground">{run.runner}</span>
                {duration(run.started, run.finished) && (
                  <span className="text-xs text-muted-foreground">{duration(run.started, run.finished)}</span>
                )}
                <Button size="sm" variant="ghost" className="ml-auto" onClick={() => showLog(run)}>
                  <ScrollText className="size-3.5" />
                  {t("log")}
                </Button>
              </div>
            ))}
          </div>
        )}
        {logFor && (
          <div className="flex flex-col gap-1">
            <p className="text-xs font-medium text-muted-foreground">{t("logFor", { id: logFor.id })}</p>
            <pre className="max-h-64 overflow-auto rounded bg-muted p-3 font-mono text-xs whitespace-pre-wrap">
              {logFor.log || t("noLog")}
            </pre>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
