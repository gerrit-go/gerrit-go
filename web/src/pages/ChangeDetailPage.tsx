import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import {
  ArrowLeft,
  Check,
  ChevronDown,
  Copy,
  FilePlus2,
  FileText,
  FileX2,
  MessageSquarePlus,
  Send,
  X,
} from "lucide-react";
import {
  api,
  type ChangeInfo,
  type CommentInfo,
  type FileDiff,
} from "@/lib/api";
import { useAuth } from "@/auth";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
  const [change, setChange] = useState<ChangeInfo | null>(null);
  const [files, setFiles] = useState<FileDiff[] | null>(null);
  const [comments, setComments] = useState<CommentInfo[]>([]);
  const [patchSet, setPatchSet] = useState<number | "current">("current");
  const [error, setError] = useState("");
  const [actionMsg, setActionMsg] = useState("");

  const load = useCallback(async () => {
    if (!num) return;
    try {
      const [detail, cmts] = await Promise.all([api.changeDetail(num), api.comments(num)]);
      setChange(detail);
      setComments(cmts ?? []);
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
  const changeMessages = comments.filter((c) => !c.path);

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
                    ? "Submit (fast-forward merge)"
                    : "Requires Code-Review +2 and must be a fast-forward of the target branch"
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

          {/* change messages */}
          <Card className="gap-0 py-0">
            <CardHeader className="border-b py-3">
              <CardTitle className="text-sm">Change messages ({changeMessages.length})</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {changeMessages.length === 0 ? (
                <p className="p-6 text-center text-sm text-muted-foreground">No messages yet.</p>
              ) : (
                <ul className="divide-y">
                  {changeMessages.map((m) => (
                    <li key={m.id} className="flex flex-col gap-1 px-4 py-3">
                      <div className="flex items-center gap-2 text-sm">
                        <span className="font-medium">{m.author.name}</span>
                        <span className="text-xs text-muted-foreground">{timeAgo(m.updated)}</span>
                        <Badge variant="muted" className="text-[10px]">PS {m.patch_set}</Badge>
                      </div>
                      <p className="whitespace-pre-wrap text-sm text-muted-foreground">{m.message}</p>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
        </div>

        {/* sidebar */}
        <div className="flex flex-col gap-4">
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
                Submit requires <span className="font-medium text-foreground">Code-Review +2</span>{" "}
                and a fast-forward merge.
              </div>
            </CardContent>
          </Card>

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
