import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import {
  ArrowLeft,
  Check,
  ChevronDown,
  Copy,
  EyeOff,
  FilePlus2,
  FileText,
  FileX2,
  GitBranch,
  GitFork,
  ListTree,
  MessageSquarePlus,
  MoreHorizontal,
  Send,
  Tag,
  Trash2,
  Undo2,
  UserPlus,
  X,
} from "lucide-react";
import {
  api,
  type AccountInfo,
  type ChangeInfo,
  type ChangeMessageInfo,
  type CommentInfo,
  type FileDiff,
} from "@/lib/api";
import { useNavigate } from "react-router-dom";
import { useAuth } from "@/auth";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn, timeAgo } from "@/lib/utils";
import { StatusBadge } from "@/pages/ChangesPage";

export default function ChangeDetailPage() {
  const { num } = useParams();
  const { user } = useAuth();
  const navigate = useNavigate();
  const [change, setChange] = useState<ChangeInfo | null>(null);
  const [files, setFiles] = useState<FileDiff[] | null>(null);
  const [comments, setComments] = useState<CommentInfo[]>([]);
  const [messages, setMessages] = useState<ChangeMessageInfo[]>([]);
  const [patchSet, setPatchSet] = useState<number | "current">("current");
  const [error, setError] = useState("");
  const [actionMsg, setActionMsg] = useState("");
  const [cherryOpen, setCherryOpen] = useState(false);

  const load = useCallback(async () => {
    if (!num) return;
    try {
      const [detail, cmts, msgs] = await Promise.all([
        api.changeDetail(num),
        api.comments(num),
        api.messages(num),
      ]);
      setChange(detail);
      setComments(cmts ?? []);
      setMessages(msgs ?? []);
    } catch (err) {
      setError((err as Error).message);
    }
  }, [num]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (!num) return;
    setFiles(null);
    api
      .revisionFiles(num, patchSet)
      .then((data) => setFiles(data ?? []))
      .catch((err) => setError((err as Error).message));
  }, [num, patchSet]);

  if (error && !change) {
    return (
      <div className="py-16 text-center">
        <p className="text-destructive">{error}</p>
        <Button asChild variant="outline" className="mt-4">
          <Link to="/">Back to changes</Link>
        </Button>
      </div>
    );
  }
  if (!change) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-8 w-2/3" />
        <Skeleton className="h-4 w-1/3" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  const revisions = Object.values(change.revisions ?? {}).sort((a, b) => a._number - b._number);
  const currentPS = revisions.find((r) => r.commit === change.current_revision)?._number ?? change.current_ps ?? 1;

  const runAction = async (fn: () => Promise<unknown>, okMsg: string) => {
    setActionMsg("");
    setError("");
    try {
      await fn();
      setActionMsg(okMsg);
      await load();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const checkoutCmd = `git fetch ${window.location.origin}/git/${change.project}.git refs/changes/${String(change._number % 100).padStart(2, "0")}/${change._number}/${currentPS} && git checkout FETCH_HEAD`;

  return (
    <div className="flex flex-col gap-5">
      {/* header */}
      <div className="flex flex-col gap-3">
        <div className="flex items-start gap-3">
          <Button asChild variant="ghost" size="icon" className="mt-0.5 shrink-0">
            <Link to="/">
              <ArrowLeft />
            </Link>
          </Button>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-xl font-semibold">{change.subject}</h1>
              <StatusBadge status={change.status} />
              {change.work_in_progress && (
                <Badge variant="muted" className="gap-1">
                  <EyeOff className="size-3" /> WIP
                </Badge>
              )}
              {change.private && <Badge variant="outline">Private</Badge>}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
              <span className="font-mono text-xs">#{change._number}</span>
              <Link
                to={`/projects/${encodeURIComponent(change.project)}`}
                className="hover:underline"
              >
                {change.project}
              </Link>
              <span className="font-mono text-xs">→ {change.branch}</span>
              {change.topic && (
                <Link
                  to={`/?q=${encodeURIComponent("topic:" + change.topic)}`}
                  className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-xs text-foreground hover:underline"
                >
                  <Tag className="size-3" /> {change.topic}
                </Link>
              )}
              <span>
                owner <span className="text-foreground">{change.owner.name}</span>
              </span>
              <span>updated {timeAgo(change.updated)}</span>
            </div>
          </div>
        </div>

        {/* actions */}
        <div className="flex flex-wrap items-center gap-2">
          {user && change.status === "NEW" && (
            <>
              <ReviewDialog
                num={change._number}
                onDone={async (msg) => {
                  setActionMsg(msg);
                  await load();
                }}
              />
              <Button
                size="sm"
                disabled={!change.submittable}
                title={
                  change.submittable
                    ? `Submit (${change.submit_type ?? "REBASE_IF_NECESSARY"})`
                    : change.submit_blocked || "Change is not submittable yet"
                }
                onClick={() => runAction(() => api.submit(change._number), "Change submitted")}
              >
                <Check className="size-4" />
                Submit
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => runAction(() => api.abandon(change._number), "Change abandoned")}
              >
                <X className="size-4" />
                Abandon
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  runAction(
                    () =>
                      change.work_in_progress
                        ? api.clearWIP(change._number)
                        : api.setWIP(change._number),
                    change.work_in_progress ? "Marked ready for review" : "Marked work-in-progress",
                  )
                }
              >
                <EyeOff className="size-4" />
                {change.work_in_progress ? "Mark ready" : "Mark WIP"}
              </Button>
              <TopicDialog
                num={change._number}
                topic={change.topic ?? ""}
                onDone={async (msg) => {
                  setActionMsg(msg);
                  await load();
                }}
              />
            </>
          )}
          {user && change.status === "ABANDONED" && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => runAction(() => api.restore(change._number), "Change restored")}
            >
              Restore
            </Button>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="sm" variant="secondary">
                Download
                <ChevronDown className="size-3" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="max-w-sm">
              <DropdownMenuItem
                className="font-mono text-xs"
                onSelect={() => navigator.clipboard.writeText(checkoutCmd)}
              >
                <Copy className="size-3" />
                copy checkout command
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          {user && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="sm" variant="secondary">
                  <MoreHorizontal className="size-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                {change.status === "NEW" && (
                  <DropdownMenuItem
                    onSelect={() =>
                      runAction(() => api.rebase(change._number), "Rebased onto target branch")
                    }
                  >
                    <GitBranch className="size-4" />
                    Rebase
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem onSelect={() => setCherryOpen(true)}>
                  <GitFork className="size-4" />
                  Cherry-pick
                </DropdownMenuItem>
                {change.status === "MERGED" && (
                  <DropdownMenuItem
                    onSelect={async () => {
                      setActionMsg("");
                      setError("");
                      try {
                        const created = await api.revert(change._number);
                        navigate(`/c/${created._number}`);
                      } catch (err) {
                        setError((err as Error).message);
                      }
                    }}
                  >
                    <Undo2 className="size-4" />
                    Revert
                  </DropdownMenuItem>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          <CherryPickDialog
            open={cherryOpen}
            onOpenChange={setCherryOpen}
            num={change._number}
            project={change.project}
            defaultBranch={change.branch}
            onCreated={(newNum) => navigate(`/c/${newNum}`)}
            onError={setError}
          />
          <div className="ml-auto flex items-center gap-2">
            {revisions.length > 1 && (
              <select
                className="h-8 rounded-md border border-input bg-transparent px-2 text-xs shadow-xs outline-none"
                value={patchSet === "current" ? "current" : String(patchSet)}
                onChange={(e) =>
                  setPatchSet(e.target.value === "current" ? "current" : Number(e.target.value))
                }
              >
                <option value="current">Patch set: latest (#{currentPS})</option>
                {revisions.map((r) => (
                  <option key={r._number} value={String(r._number)}>
                    Patch set #{r._number} · {r.commit.slice(0, 8)}
                  </option>
                ))}
              </select>
            )}
          </div>
        </div>

        {actionMsg && <p className="text-sm text-emerald-600">{actionMsg}</p>}
        {error && <p className="text-sm text-destructive">{error}</p>}
        {!error && change.status === "NEW" && change.submit_blocked && (
          <p className="text-sm text-muted-foreground">
            Not submittable: {change.submit_blocked}
          </p>
        )}
      </div>

      <div className="grid gap-5 lg:grid-cols-[1fr_320px]">
        {/* main column: files + diff */}
        <div className="flex min-w-0 flex-col gap-4">
          <Card className="gap-0 py-0">
            <CardHeader className="border-b py-3">
              <CardTitle className="text-sm">
                Files{" "}
                <span className="ml-1 font-normal text-muted-foreground">
                  ({files?.length ?? 0})
                </span>
              </CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {files === null ? (
                <div className="flex flex-col gap-1 p-4">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} className="h-7 w-full" />
                  ))}
                </div>
              ) : (
                <div className="divide-y">
                  {files.map((f) => (
                    <div key={f.path + (f.old_path ?? "")}>
                      <div className="flex items-center gap-2 px-4 py-1.5 text-sm">
                        <FileStatusIcon status={f.status} />
                        <span className="font-mono text-xs">{f.path}</span>
                        {f.old_path && (
                          <span className="font-mono text-xs text-muted-foreground">
                            (renamed from {f.old_path})
                          </span>
                        )}
                        <span className="ml-auto flex gap-2 font-mono text-xs">
                          <span className="text-emerald-600">+{f.add_count}</span>
                          <span className="text-red-600">−{f.del_count}</span>
                        </span>
                      </div>
                      <DiffView
                        file={f}
                        comments={comments.filter((c) => c.path === f.path)}
                        canComment={!!user && change.status === "NEW"}
                        onAddComment={async (line, message) => {
                          await api.review(change._number, {
                            comments: { [f.path]: [{ line, message }] },
                          });
                          await load();
                        }}
                      />
                    </div>
                  ))}
                  {files.length === 0 && (
                    <p className="p-6 text-center text-sm text-muted-foreground">
                      No file changes in this patch set.
                    </p>
                  )}
                </div>
              )}
            </CardContent>
          </Card>

          {/* unified change timeline */}
          <Card className="gap-0 py-0">
            <CardHeader className="border-b py-3">
              <CardTitle className="text-sm">Change messages ({messages.length})</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {messages.length === 0 ? (
                <p className="p-6 text-center text-sm text-muted-foreground">No messages yet.</p>
              ) : (
                <ul className="divide-y">
                  {messages.map((m) => {
                    const positive = m.type === "submitted" || m.type === "vote";
                    const negative = m.type === "abandoned";
                    return (
                      <li
                        key={m.id}
                        className={cn(
                          "flex flex-col gap-1 border-l-2 px-4 py-3",
                          positive && "border-emerald-500",
                          negative && "border-red-500",
                          !positive && !negative && "border-transparent",
                        )}
                      >
                        <div className="flex flex-wrap items-center gap-2 text-sm">
                          <span className="font-medium">{m.author?.name ?? "System"}</span>
                          <Badge variant="muted" className="text-[10px]">
                            {messageTypeLabel(m.type)}
                          </Badge>
                          {m.patch_set > 0 && (
                            <Badge variant="muted" className="text-[10px]">
                              PS {m.patch_set}
                            </Badge>
                          )}
                          <span className="text-xs text-muted-foreground">{timeAgo(m.date)}</span>
                        </div>
                        <p className="whitespace-pre-wrap text-sm text-muted-foreground">{m.message}</p>
                      </li>
                    );
                  })}
                </ul>
              )}
            </CardContent>
          </Card>
        </div>

        {/* sidebar */}
        <div className="flex flex-col gap-4">
          <ReviewersCard
            change={change}
            canEdit={!!user && change.status === "NEW"}
            ownerId={change.owner._account_id}
            onDone={load}
          />
          <Card className="gap-3 py-4">
            <CardHeader className="px-4 py-0">
              <CardTitle className="text-sm">Votes</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-3 px-4">
              {["Code-Review", "Verified"].map((label) => {
                const info = change.labels?.[label];
                return (
                  <div key={label} className="flex flex-col gap-1.5">
                    <div className="text-xs font-medium text-muted-foreground">{label}</div>
                    {info && info.all.length > 0 ? (
                      <ul className="flex flex-col gap-1">
                        {info.all.map((v, i) => (
                          <li key={i} className="flex items-center gap-2 text-sm">
                            <span
                              className={cn(
                                "w-8 text-center font-mono text-xs font-semibold",
                                v.value > 0 && "text-emerald-600",
                                v.value < 0 && "text-red-600",
                                v.value === 0 && "text-muted-foreground",
                              )}
                            >
                              {v.value > 0 ? `+${v.value}` : v.value}
                            </span>
                            <span>{v.name}</span>
                            <span className="ml-auto text-xs text-muted-foreground">
                              PS {v.patch_set}
                            </span>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <span className="text-sm text-muted-foreground">No votes</span>
                    )}
                  </div>
                );
              })}
              <Separator />
              <div className="text-xs text-muted-foreground">
                Submit strategy:{" "}
                <span className="font-medium text-foreground">
                  {change.submit_type ?? "REBASE_IF_NECESSARY"}
                </span>
                . Requires <span className="font-medium text-foreground">Code-Review +2</span>.
              </div>
            </CardContent>
          </Card>

          {change.relation_chain && change.relation_chain.length > 0 && (
            <Card className="gap-3 py-4">
              <CardHeader className="px-4 py-0">
                <CardTitle className="flex items-center gap-1.5 text-sm">
                  <ListTree className="size-4 text-muted-foreground" />
                  Relation chain
                </CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-1 px-4 text-sm">
                {change.relation_chain.map((rel) => (
                  <Link
                    key={rel._number}
                    to={`/c/${rel._number}`}
                    className={cn(
                      "flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-accent",
                      rel.self && "bg-accent font-medium",
                    )}
                  >
                    <span className="font-mono text-xs text-muted-foreground">#{rel._number}</span>
                    <span className="truncate">{rel.subject}</span>
                    <span className="ml-auto shrink-0">
                      <StatusBadge status={rel.status} />
                    </span>
                  </Link>
                ))}
              </CardContent>
            </Card>
          )}

          <Card className="gap-3 py-4">
            <CardHeader className="px-4 py-0">
              <CardTitle className="text-sm">Details</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2 px-4 text-sm">
              <DetailRow
                label="Change-Id"
                value={change.change_id}
                mono
                copy={change.change_id}
              />
              <DetailRow
                label="Commit"
                value={change.current_revision?.slice(0, 10) ?? "—"}
                mono
                copy={change.current_revision}
              />
              <DetailRow label="Branch" value={change.branch} mono />
              <DetailRow label="Created" value={new Date(change.created).toLocaleString()} />
              {change.submitted && (
                <DetailRow label="Submitted" value={new Date(change.submitted).toLocaleString()} />
              )}
            </CardContent>
          </Card>

          <Card className="gap-3 py-4">
            <CardHeader className="px-4 py-0">
              <CardTitle className="text-sm">Patch sets</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-1 px-4 text-sm">
              {revisions.map((r) => (
                <button
                  key={r._number}
                  className={cn(
                    "flex items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-accent",
                    (patchSet === r._number || (patchSet === "current" && r._number === currentPS)) &&
                      "bg-accent",
                  )}
                  onClick={() => setPatchSet(r._number)}
                >
                  <span className="font-mono text-xs">#{r._number}</span>
                  <span className="font-mono text-xs text-muted-foreground">
                    {r.commit.slice(0, 8)}
                  </span>
                  <span className="ml-auto text-xs text-muted-foreground">{timeAgo(r.created)}</span>
                </button>
              ))}
              {revisions.length === 0 && (
                <span className="text-muted-foreground">None</span>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

function messageTypeLabel(type: string): string {
  switch (type) {
    case "patchset-uploaded":
      return "Patch set";
    case "vote":
      return "Vote";
    case "comment":
      return "Comment";
    case "submitted":
      return "Merged";
    case "abandoned":
      return "Abandoned";
    case "restored":
      return "Restored";
    case "reviewer-added":
      return "Reviewer +";
    case "reviewer-removed":
      return "Reviewer −";
    case "topic":
      return "Topic";
    case "wip":
      return "WIP";
    default:
      return type;
  }
}

function ReviewersCard({
  change,
  canEdit,
  ownerId,
  onDone,
}: {
  change: ChangeInfo;
  canEdit: boolean;
  ownerId: number;
  onDone: () => Promise<void>;
}) {
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const reviewers: AccountInfo[] = change.reviewers ?? [];

  const add = async () => {
    if (!value.trim()) return;
    setBusy(true);
    setErr("");
    try {
      await api.addReviewer(change._number, value.trim());
      setValue("");
      await onDone();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: number) => {
    setBusy(true);
    setErr("");
    try {
      await api.removeReviewer(change._number, id);
      await onDone();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card className="gap-3 py-4">
      <CardHeader className="px-4 py-0">
        <CardTitle className="text-sm">Reviewers</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 px-4">
        {reviewers.length === 0 ? (
          <span className="text-sm text-muted-foreground">No reviewers</span>
        ) : (
          <ul className="flex flex-col gap-1">
            {reviewers.map((rv) => (
              <li key={rv._account_id} className="flex items-center gap-2 text-sm">
                <span className="truncate">{rv.name}</span>
                {rv._account_id === ownerId && (
                  <Badge variant="muted" className="text-[10px]">
                    owner
                  </Badge>
                )}
                {canEdit && rv._account_id !== ownerId && (
                  <button
                    className="ml-auto text-muted-foreground hover:text-destructive"
                    onClick={() => remove(rv._account_id)}
                    title="Remove reviewer"
                    disabled={busy}
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
        {canEdit && (
          <div className="flex gap-1.5">
            <Input
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder="username"
              className="h-8 text-xs"
              onKeyDown={(e) => {
                if (e.key === "Enter") add();
              }}
            />
            <Button size="sm" variant="outline" disabled={busy || !value.trim()} onClick={add}>
              <UserPlus className="size-4" />
            </Button>
          </div>
        )}
        {err && <p className="text-xs text-destructive">{err}</p>}
      </CardContent>
    </Card>
  );
}

function TopicDialog({
  num,
  topic,
  onDone,
}: {
  num: number;
  topic: string;
  onDone: (msg: string) => Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState(topic);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (open) setValue(topic);
  }, [open, topic]);

  const submit = async () => {
    setBusy(true);
    setErr("");
    try {
      const t = value.trim();
      if (t) await api.setTopic(num, t);
      else await api.deleteTopic(num);
      setOpen(false);
      await onDone(t ? `Topic set to ${t}` : "Topic cleared");
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          <Tag className="size-4" />
          Topic
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Set topic</DialogTitle>
          <DialogDescription>
            Group related changes under a topic. Leave empty to clear.
          </DialogDescription>
        </DialogHeader>
        <Input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="e.g. release-2.0"
          autoFocus
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
          }}
        />
        {err && <p className="text-sm text-destructive">{err}</p>}
        <DialogFooter>
          <Button variant="ghost" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? "Saving…" : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CherryPickDialog({
  open,
  onOpenChange,
  num,
  project,
  defaultBranch,
  onCreated,
  onError,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  num: number;
  project: string;
  defaultBranch: string;
  onCreated: (newNum: number) => void;
  onError: (msg: string) => void;
}) {
  const [branches, setBranches] = useState<string[]>([]);
  const [dest, setDest] = useState(defaultBranch);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!open) return;
    setDest(defaultBranch);
    setErr("");
    api
      .branches(project)
      .then((b) => setBranches((b ?? []).map((x) => x.name)))
      .catch(() => setBranches([]));
  }, [open, project, defaultBranch]);

  const submit = async () => {
    if (!dest.trim()) return;
    setBusy(true);
    setErr("");
    try {
      const created = await api.cherryPick(num, dest.trim());
      onOpenChange(false);
      onCreated(created._number);
    } catch (e) {
      const msg = (e as Error).message;
      setErr(msg);
      onError(msg);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Cherry-pick change {num}</DialogTitle>
          <DialogDescription>
            Apply this change's current commit onto another branch as a new change.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-2">
          <label className="text-sm font-medium" htmlFor="cp-dest">
            Destination branch
          </label>
          <Input
            id="cp-dest"
            list="cp-branches"
            value={dest}
            onChange={(e) => setDest(e.target.value)}
            placeholder="e.g. release-1.0"
            autoFocus
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
          />
          <datalist id="cp-branches">
            {branches.map((b) => (
              <option key={b} value={b} />
            ))}
          </datalist>
        </div>
        {err && <p className="text-sm text-destructive">{err}</p>}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !dest.trim()}>
            {busy ? "Cherry-picking…" : "Cherry-pick"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DetailRow({
  label,
  value,
  mono,
  copy,
}: {
  label: string;
  value: string;
  mono?: boolean;
  copy?: string;
}) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex items-center gap-2">
      <span className="w-20 shrink-0 text-xs text-muted-foreground">{label}</span>
      <span className={cn("truncate", mono && "font-mono text-xs")}>{value}</span>
      {copy && (
        <button
          className="ml-auto shrink-0 text-muted-foreground hover:text-foreground"
          onClick={() => {
            navigator.clipboard.writeText(copy);
            setCopied(true);
            setTimeout(() => setCopied(false), 1200);
          }}
          title="Copy"
        >
          {copied ? <Check className="size-3.5 text-emerald-600" /> : <Copy className="size-3.5" />}
        </button>
      )}
    </div>
  );
}

function FileStatusIcon({ status }: { status: FileDiff["status"] }) {
  switch (status) {
    case "A":
      return <FilePlus2 className="size-4 text-emerald-600" />;
    case "D":
      return <FileX2 className="size-4 text-red-600" />;
    default:
      return <FileText className="size-4 text-muted-foreground" />;
  }
}

function DiffView({
  file,
  comments,
  canComment,
  onAddComment,
}: {
  file: FileDiff;
  comments: CommentInfo[];
  canComment: boolean;
  onAddComment: (line: number, message: string) => Promise<void>;
}) {
  const [draftLine, setDraftLine] = useState<number | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);

  if (file.binary) {
    return <p className="bg-muted/30 px-4 py-3 text-xs text-muted-foreground">Binary file changed.</p>;
  }

  const submitDraft = async () => {
    if (draftLine === null || !draft.trim()) return;
    setBusy(true);
    try {
      await onAddComment(draftLine, draft.trim());
      setDraft("");
      setDraftLine(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="overflow-x-auto border-t bg-card font-mono text-xs leading-5">
      {file.hunks.map((hunk, hi) => (
        <div key={hi}>
          <div className="bg-blue-50 px-4 py-0.5 text-[11px] text-blue-700 dark:bg-blue-950/40 dark:text-blue-300">
            {hunk.header}
          </div>
          {hunk.lines.map((line, li) => {
            const lineNo = line.type === "del" ? line.old_no ?? 0 : line.new_no ?? 0;
            const lineComments = comments.filter(
              (c) => (c.line === line.new_no || c.line === line.old_no) && c.line !== 0,
            );
            return (
              <div key={li}>
                <div
                  className={cn(
                    "group flex min-w-max cursor-pointer hover:bg-accent/60",
                    line.type === "add" && "bg-emerald-50 dark:bg-emerald-950/30",
                    line.type === "del" && "bg-red-50 dark:bg-red-950/30",
                  )}
                  onClick={() => canComment && setDraftLine(draftLine === lineNo ? null : lineNo)}
                >
                  <span className="w-12 shrink-0 select-none border-r px-2 text-right text-muted-foreground/70">
                    {line.old_no ?? ""}
                  </span>
                  <span className="w-12 shrink-0 select-none border-r px-2 text-right text-muted-foreground/70">
                    {line.new_no ?? ""}
                  </span>
                  <span
                    className={cn(
                      "w-5 shrink-0 select-none text-center font-bold",
                      line.type === "add" && "text-emerald-600",
                      line.type === "del" && "text-red-600",
                    )}
                  >
                    {line.type === "add" ? "+" : line.type === "del" ? "−" : " "}
                  </span>
                  <span className="whitespace-pre pr-4">{line.text}</span>
                  {canComment && (
                    <span className="ml-auto hidden shrink-0 items-center pr-2 text-muted-foreground group-hover:flex">
                      <MessageSquarePlus className="size-3.5" />
                    </span>
                  )}
                </div>
                {lineComments.map((c) => (
                  <div key={c.id} className="flex min-w-max gap-2 border-y bg-amber-50/70 px-14 py-2 dark:bg-amber-950/20">
                    <div className="min-w-0 flex-1">
                      <div className="font-sans text-xs">
                        <span className="font-semibold">{c.author.name}</span>{" "}
                        <span className="text-muted-foreground">· PS {c.patch_set} · {timeAgo(c.updated)}</span>
                      </div>
                      <p className="whitespace-pre-wrap font-sans text-xs text-muted-foreground">{c.message}</p>
                    </div>
                  </div>
                ))}
                {draftLine === lineNo && canComment && (
                  <div className="border-y bg-muted/40 px-14 py-2" onClick={(e) => e.stopPropagation()}>
                    <Textarea
                      autoFocus
                      value={draft}
                      onChange={(e) => setDraft(e.target.value)}
                      placeholder={`Comment on line ${lineNo}…`}
                      className="min-h-14 font-sans text-xs"
                      onKeyDown={(e) => {
                        if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submitDraft();
                        if (e.key === "Escape") setDraftLine(null);
                      }}
                    />
                    <div className="mt-1.5 flex justify-end gap-2">
                      <Button size="sm" variant="ghost" onClick={() => setDraftLine(null)}>
                        Cancel
                      </Button>
                      <Button size="sm" disabled={busy || !draft.trim()} onClick={submitDraft}>
                        <Send className="size-3.5" />
                        Comment
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      ))}
      {file.hunks.length === 0 && (
        <p className="px-4 py-3 text-muted-foreground">No textual changes.</p>
      )}
    </div>
  );
}

function ReviewDialog({ num, onDone }: { num: number; onDone: (msg: string) => Promise<void> }) {
  const [open, setOpen] = useState(false);
  const [cr, setCr] = useState(0);
  const [verified, setVerified] = useState(0);
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async () => {
    setBusy(true);
    setError("");
    try {
      const labels: Record<string, number> = {};
      if (cr !== 0) labels["Code-Review"] = cr;
      if (verified !== 0) labels["Verified"] = verified;
      await api.review(num, { labels, message: message.trim() || undefined });
      setOpen(false);
      setCr(0);
      setVerified(0);
      setMessage("");
      await onDone("Review saved");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <VoteChipIcon />
          Review
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Review change {num}</DialogTitle>
          <DialogDescription>
            Vote on labels and optionally leave a cover message.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <VoteRow label="Code-Review" value={cr} onChange={setCr} options={[-2, -1, 0, 1, 2]} />
          <VoteRow label="Verified" value={verified} onChange={setVerified} options={[-1, 0, 1]} />
          <Textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            placeholder="Cover message (optional)"
            rows={3}
          />
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? "Sending…" : "Send review"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function VoteChipIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M12 3l2.5 5.5L20 11l-5.5 2.5L12 19l-2.5-5.5L4 11l5.5-2.5z" />
    </svg>
  );
}

function VoteRow({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: number;
  onChange: (v: number) => void;
  options: number[];
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <span className="text-sm font-medium">{label}</span>
      <div className="flex gap-1">
        {options.map((opt) => (
          <button
            key={opt}
            type="button"
            onClick={() => onChange(opt)}
            className={cn(
              "h-8 w-9 rounded-md border text-xs font-semibold transition-colors",
              value === opt
                ? opt > 0
                  ? "border-emerald-600 bg-emerald-600 text-white"
                  : opt < 0
                    ? "border-red-600 bg-red-600 text-white"
                    : "border-primary bg-primary text-primary-foreground"
                : "hover:bg-accent",
            )}
          >
            {opt > 0 ? `+${opt}` : opt}
          </button>
        ))}
      </div>
    </div>
  );
}
