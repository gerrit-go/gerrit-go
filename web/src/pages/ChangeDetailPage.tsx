import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import {
  ArrowLeft,
  BellRing,
  Check,
  ChevronDown,
  CircleCheck,
  RefreshCw,
  ScrollText,
  CircleDot,
  CircleX,
  ClipboardCheck,
  Copy,
  CornerDownRight,
  ExternalLink,
  EyeOff,
  FilePlus2,
  FileText,
  FileX2,
  GitBranch,
  GitFork,
  ListTree,
  MessageSquarePlus,
  MoreHorizontal,
  Pencil,
  Send,
  Star,
  Tag,
  Trash2,
  Undo2,
  User,
  UserPlus,
  X,
} from "lucide-react";
import {
  api,
  type AccountInfo,
  type AttentionEntry,
  type ChangeInfo,
  type ChangeMessageInfo,
  type CheckRun,
  type CommentDraftInfo,
  type CommentInfo,
  type ConflictFile,
  type DiffHunk,
  type DiffLine,
  type EditInfo,
  type FileDiff,
  type PipelineRun,
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
import { highlightLine, langForPath } from "@/lib/highlight";
import { applySuggestionToLines, parseSuggestion } from "@/lib/suggestion";
import { useHotkey } from "@/lib/hotkey";
import { StatusBadge } from "@/pages/ChangesPage";

export default function ChangeDetailPage() {
  const { num } = useParams();
  const { user } = useAuth();
  const { t } = useTranslation("changeDetail");
  const navigate = useNavigate();
  const [change, setChange] = useState<ChangeInfo | null>(null);
  const [files, setFiles] = useState<FileDiff[] | null>(null);
  const [comments, setComments] = useState<CommentInfo[]>([]);
  const [drafts, setDrafts] = useState<CommentDraftInfo[]>([]);
  const [messages, setMessages] = useState<ChangeMessageInfo[]>([]);
  const [patchSet, setPatchSet] = useState<number | "current">("current");
  const [diffMode, setDiffMode] = useState<"unified" | "split">("unified");
  const [error, setError] = useState("");
  const [actionMsg, setActionMsg] = useState("");
  const [cherryOpen, setCherryOpen] = useState(false);
  const [conflictOpen, setConflictOpen] = useState(false);
  const [edit, setEdit] = useState<EditInfo | null>(null);
  const [editFilePath, setEditFilePath] = useState<string | null>(null);
  const [editFileContent, setEditFileContent] = useState("");
  const [downloadOpen, setDownloadOpen] = useState(false);
  const [sshPort, setSshPort] = useState("");
  const [fileIdx, setFileIdx] = useState(-1);
  const [applyingSuggestion, setApplyingSuggestion] = useState(false);

  useHotkey((e) => {
    const count = files?.length ?? 0;
    if (e.key === "]" && count > 0) {
      const next = Math.min(fileIdx + 1, count - 1);
      setFileIdx(next);
      document.querySelector(`[data-file-idx="${next}"]`)?.scrollIntoView({ behavior: "smooth", block: "start" });
    } else if (e.key === "[" && count > 0) {
      const prev = Math.max(fileIdx - 1, 0);
      setFileIdx(prev);
      document.querySelector(`[data-file-idx="${prev}"]`)?.scrollIntoView({ behavior: "smooth", block: "start" });
    } else if (e.key === "r") {
      document.querySelector("[data-review-box]")?.scrollIntoView({ behavior: "smooth", block: "center" });
    }
  });

  useEffect(() => {
    api
      .getConfig()
      .then((c) => setSshPort(c?.ssh?.port ?? ""))
      .catch(() => setSshPort(""));
  }, []);

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
      if (user) {
        const dr = await api.listDrafts(num).catch(() => [] as CommentDraftInfo[]);
        setDrafts(dr ?? []);
        const ed = await api.getEdit(num).catch(() => null);
        setEdit(ed);
      } else {
        setEdit(null);
      }
    } catch (err) {
      setError((err as Error).message);
    }
  }, [num, user]);

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
          <Link to="/">{t("common:notFound.back")}</Link>
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

  const applySuggestion = async (comment: CommentInfo, suggestion: string) => {
    if (!change || change.status !== "NEW") return;
    setApplyingSuggestion(true);
    setError("");
    try {
      const content = await api.revisionFileContent(change._number, "current", comment.path);
      const updated = applySuggestionToLines(content, comment.line, suggestion);
      const ed = await api.putEditFile(change._number, comment.path, updated);
      setEdit(ed);
      setActionMsg(t("comments.suggestionApplied"));
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setApplyingSuggestion(false);
    }
  };

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
          {user && (
            <button
              className="mt-0.5 shrink-0 text-muted-foreground transition-colors hover:text-foreground"
              title={change.starred ? t("header.unstarChange") : t("header.starChange")}
              aria-label={change.starred ? t("header.unstarChange") : t("header.starChange")}
              onClick={async () => {
                const next = !change.starred;
                setChange({ ...change, starred: next });
                try {
                  if (next) await api.star(change._number);
                  else await api.unstar(change._number);
                } catch {
                  setChange({ ...change, starred: !next });
                }
              }}
            >
              <Star
                className={cn(
                  "size-5",
                  change.starred && "fill-amber-400 text-amber-400",
                )}
              />
            </button>
          )}
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-xl font-semibold">{change.subject}</h1>
              <StatusBadge status={change.status} />
              {change.work_in_progress && (
                <Badge variant="muted" className="gap-1">
                  <EyeOff className="size-3" /> {t("header.wip")}
                </Badge>
              )}
              {change.private && <Badge variant="outline">{t("header.private")}</Badge>}
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
                {t("common:common.owner")} <span className="text-foreground">{change.owner.name}</span>
              </span>
              <span>{t("header.updated", { time: timeAgo(change.updated) })}</span>
            </div>
          </div>
        </div>

        {/* actions */}
        <div className="flex flex-wrap items-center gap-2">
          {user && change.status === "NEW" && (
            <>
              <ReviewDialog
                num={change._number}
                draftCount={drafts.length}
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
                    ? t("actions.submitTooltip", {
                        strategy: change.submit_type ?? "REBASE_IF_NECESSARY",
                      })
                    : change.submit_blocked || t("actions.notSubmittable")
                }
                onClick={() => runAction(() => api.submit(change._number), t("actions.submitted"))}
              >
                <Check className="size-4" />
                {t("actions.submit")}
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => runAction(() => api.abandon(change._number), t("actions.abandoned"))}
              >
                <X className="size-4" />
                {t("actions.abandon")}
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
                    change.work_in_progress ? t("actions.markedReady") : t("actions.markedWip"),
                  )
                }
              >
                <EyeOff className="size-4" />
                {change.work_in_progress ? t("actions.markReady") : t("actions.markWip")}
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
              onClick={() => runAction(() => api.restore(change._number), t("actions.restored"))}
            >
              {t("actions.restore")}
            </Button>
          )}
          <Button size="sm" variant="secondary" onClick={() => setDownloadOpen(true)}>
            {t("actions.download")}
            <ChevronDown className="size-3" />
          </Button>
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
                    onSelect={async () => {
                      setActionMsg("");
                      setError("");
                      try {
                        await api.rebase(change._number);
                        setActionMsg(t("actions.rebased"));
                        await load();
                      } catch (err) {
                        try {
                          const conflicts = await api.rebaseConflicts(change._number);
                          if (conflicts && conflicts.length > 0) {
                            setConflictOpen(true);
                            return;
                          }
                        } catch {
                          /* fall through to the original error */
                        }
                        setError((err as Error).message);
                      }
                    }}
                  >
                    <GitBranch className="size-4" />
                    {t("actions.rebase")}
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem onSelect={() => setCherryOpen(true)}>
                  <GitFork className="size-4" />
                  {t("actions.cherryPick")}
                </DropdownMenuItem>
                {change.status === "NEW" && !edit && (
                  <DropdownMenuItem
                    onSelect={async () => {
                      setActionMsg("");
                      setError("");
                      try {
                        const ed = await api.createEdit(change._number);
                        setEdit(ed);
                      } catch (err) {
                        setError((err as Error).message);
                      }
                    }}
                  >
                    <Pencil className="size-4" />
                    {t("actions.edit")}
                  </DropdownMenuItem>
                )}
                {change.status === "NEW" && edit && (
                  <>
                    <DropdownMenuItem
                      disabled={edit.stale}
                      onSelect={async () => {
                        setActionMsg("");
                        setError("");
                        try {
                          await api.publishEdit(change._number);
                          setEdit(null);
                          setActionMsg(t("actions.editPublished"));
                          await load();
                        } catch (err) {
                          setError((err as Error).message);
                        }
                      }}
                    >
                      <Send className="size-4" />
                      {t("actions.publishEdit")}
                    </DropdownMenuItem>
                    {edit.stale && (
                      <DropdownMenuItem
                        onSelect={async () => {
                          setActionMsg("");
                          setError("");
                          try {
                            const ed = await api.rebaseEdit(change._number);
                            setEdit(ed);
                            setActionMsg(t("actions.editRebased"));
                          } catch (err) {
                            setError((err as Error).message);
                          }
                        }}
                      >
                        <GitBranch className="size-4" />
                        {t("actions.rebaseEdit")}
                      </DropdownMenuItem>
                    )}
                    <DropdownMenuItem
                      onSelect={async () => {
                        setActionMsg("");
                        setError("");
                        try {
                          await api.deleteEdit(change._number);
                          setEdit(null);
                          setActionMsg(t("actions.editDeleted"));
                        } catch (err) {
                          setError((err as Error).message);
                        }
                      }}
                    >
                      <Trash2 className="size-4" />
                      {t("actions.deleteEdit")}
                    </DropdownMenuItem>
                  </>
                )}
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
                    {t("actions.revert")}
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
          <ConflictDialog
            open={conflictOpen}
            onOpenChange={setConflictOpen}
            num={change._number}
            onResolved={async () => {
              setActionMsg(t("conflict.resolved"));
              await load();
            }}
            onError={setError}
          />
          <ChangeEditDialog
            path={editFilePath}
            initial={editFileContent}
            onClose={() => setEditFilePath(null)}
            onSave={async (path, content) => {
              const ed = await api.putEditFile(change._number, path, content);
              setEdit(ed);
              setEditFilePath(null);
            }}
            onError={setError}
          />
          <DownloadDialog
            open={downloadOpen}
            onOpenChange={setDownloadOpen}
            change={change}
            currentPS={currentPS}
            sshPort={sshPort}
            username={user?.username ?? ""}
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
                <option value="current">{t("header.psLatest", { ps: currentPS })}</option>
                {revisions.map((r) => (
                  <option key={r._number} value={String(r._number)}>
                    {t("header.psOption", { ps: r._number, sha: r.commit.slice(0, 8) })}
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
            {t("header.notSubmittable", { reason: change.submit_blocked })}
          </p>
        )}
      </div>

      <div className="grid gap-5 lg:grid-cols-[1fr_320px]">
        {/* main column: files + diff */}
        <div className="flex min-w-0 flex-col gap-4">
          <Card className="gap-0 py-0">
            <CardHeader className="flex-row items-center justify-between border-b py-3">
              <CardTitle className="text-sm">
                {t("files.title")}{" "}
                <span className="ml-1 font-normal text-muted-foreground">
                  ({files?.length ?? 0})
                </span>
                {drafts.length > 0 && (
                  <Badge variant="secondary" className="ml-2 gap-1 text-[10px]">
                    <MessageSquarePlus className="size-3" />
                    {t("files.draftsBadge", { count: drafts.length })}
                  </Badge>
                )}
              </CardTitle>
              <div className="flex items-center gap-1">
                <Button
                  size="sm"
                  variant={diffMode === "unified" ? "secondary" : "ghost"}
                  className="h-7 px-2 text-xs"
                  onClick={() => setDiffMode("unified")}
                >
                  {t("diff.unified")}
                </Button>
                <Button
                  size="sm"
                  variant={diffMode === "split" ? "secondary" : "ghost"}
                  className="h-7 px-2 text-xs"
                  onClick={() => setDiffMode("split")}
                >
                  {t("diff.split")}
                </Button>
              </div>
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
                  {files.map((f, fi) => (
                    <div key={f.path + (f.old_path ?? "")} data-file-idx={fi}>
                      <div className="flex items-center gap-2 px-4 py-1.5 text-sm">
                        <FileStatusIcon status={f.status} />
                        <span className="font-mono text-xs">{f.path}</span>
                        {f.old_path && (
                          <span className="font-mono text-xs text-muted-foreground">
                            {t("files.renamedFrom", { path: f.old_path })}
                          </span>
                        )}
                        <span className="ml-auto flex items-center gap-2 font-mono text-xs">
                          {edit && change.status === "NEW" && (
                            <Button
                              size="sm"
                              variant="ghost"
                              className="h-6 px-1.5"
                              title={t("actions.editFile")}
                              onClick={async () => {
                                setError("");
                                try {
                                  const content = await api.revisionFileContent(change._number, "current", f.path);
                                  setEditFileContent(content);
                                  setEditFilePath(f.path);
                                } catch (err) {
                                  setError((err as Error).message);
                                }
                              }}
                            >
                              <Pencil className="size-3.5" />
                            </Button>
                          )}
                          <span className="text-emerald-600">+{f.add_count}</span>
                          <span className="text-red-600">−{f.del_count}</span>
                        </span>
                      </div>
                      <DiffView
                        file={f}
                        num={change._number}
                        mode={diffMode}
                        comments={comments.filter((c) => c.path === f.path)}
                        drafts={drafts.filter((d) => d.path === f.path)}
                        canComment={!!user && change.status === "NEW"}
                        onApplySuggestion={change.status === "NEW" ? applySuggestion : undefined}
                        applyingSuggestion={applyingSuggestion}
                        reload={load}
                      />
                    </div>
                  ))}
                  {files.length === 0 && (
                    <p className="p-6 text-center text-sm text-muted-foreground">
                      {t("files.empty")}
                    </p>
                  )}
                </div>
              )}
            </CardContent>
          </Card>

          {/* unified change timeline */}
          <Card className="gap-0 py-0" data-review-box>
            <CardHeader className="border-b py-3">
              <CardTitle className="text-sm">{t("messages.title", { num: messages.length })}</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {messages.length === 0 ? (
                <p className="p-6 text-center text-sm text-muted-foreground">{t("messages.empty")}</p>
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
                          <span className="font-medium">{m.author?.name ?? t("messages.system")}</span>
                          <Badge variant="muted" className="text-[10px]">
                            {messageTypeLabel(m.type, t)}
                          </Badge>
                          {m.patch_set > 0 && (
                            <Badge variant="muted" className="text-[10px]">
                              {t("ps", { ps: m.patch_set })}
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
          <AssigneeCard
            change={change}
            canEdit={!!user && change.status === "NEW"}
            onDone={load}
          />
          <AttentionCard
            change={change}
            canEdit={!!user && change.status === "NEW"}
            onDone={load}
          />
          <HashtagsCard
            change={change}
            canEdit={!!user && change.status === "NEW"}
            onDone={load}
          />
          <ChecksCard
            change={change}
            canEdit={!!user && change.status === "NEW"}
          />
          <Card className="gap-3 py-4">
            <CardHeader className="px-4 py-0">
              <CardTitle className="text-sm">{t("votes.title")}</CardTitle>
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
                              {t("ps", { ps: v.patch_set })}
                            </span>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <span className="text-sm text-muted-foreground">{t("votes.empty")}</span>
                    )}
                  </div>
                );
              })}
              <Separator />
              <div className="text-xs text-muted-foreground">
                {t("votes.submitStrategy")}{" "}
                <span className="font-medium text-foreground">
                  {change.submit_type ?? "REBASE_IF_NECESSARY"}
                </span>
                {t("votes.requiresLead")}
                <span className="font-medium text-foreground">Code-Review +2</span>
                {t("votes.requiresTail")}
              </div>
            </CardContent>
          </Card>

          {change.relation_chain && change.relation_chain.length > 0 && (
            <Card className="gap-3 py-4">
              <CardHeader className="px-4 py-0">
                <CardTitle className="flex items-center gap-1.5 text-sm">
                  <ListTree className="size-4 text-muted-foreground" />
                  {t("relations.title")}
                </CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col px-4 text-sm">
                {change.relation_chain.map((rel, i) => {
                  const isSelf = rel.self || rel.relation === "self";
                  const isLast = i === change.relation_chain!.length - 1;
                  return (
                    <div key={rel._number} className="flex gap-2">
                      <div className="flex w-4 shrink-0 flex-col items-center">
                        <span className={cn("w-px flex-1 bg-border", i === 0 && "bg-transparent")} />
                        <span
                          className={cn(
                            "my-0.5 size-2.5 shrink-0 rounded-full border-2",
                            isSelf
                              ? "border-primary bg-primary"
                              : rel.status === "MERGED"
                                ? "border-emerald-500 bg-emerald-500"
                                : rel.status === "ABANDONED"
                                  ? "border-muted-foreground/40 bg-muted-foreground/40"
                                  : "border-muted-foreground/50 bg-background",
                          )}
                        />
                        <span className={cn("w-px flex-1 bg-border", isLast && "bg-transparent")} />
                      </div>
                      <Link
                        to={`/c/${rel._number}`}
                        className={cn(
                          "flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1.5 hover:bg-accent",
                          isSelf && "bg-accent font-medium",
                        )}
                      >
                        <span className="font-mono text-xs text-muted-foreground">#{rel._number}</span>
                        <span className="truncate">{rel.subject}</span>
                        <span className="ml-auto shrink-0">
                          <StatusBadge status={rel.status} />
                        </span>
                      </Link>
                    </div>
                  );
                })}
              </CardContent>
            </Card>
          )}

          <Card className="gap-3 py-4">
            <CardHeader className="px-4 py-0">
              <CardTitle className="text-sm">{t("details.title")}</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2 px-4 text-sm">
              <DetailRow
                label="Change-Id"
                value={change.change_id}
                mono
                copy={change.change_id}
              />
              <DetailRow
                label={t("details.commit")}
                value={change.current_revision?.slice(0, 10) ?? "—"}
                mono
                copy={change.current_revision}
              />
              <DetailRow label={t("common:common.branch")} value={change.branch} mono />
              <DetailRow label={t("details.created")} value={new Date(change.created).toLocaleString()} />
              {change.submitted && (
                <DetailRow label={t("details.submitted")} value={new Date(change.submitted).toLocaleString()} />
              )}
            </CardContent>
          </Card>

          <Card className="gap-3 py-4">
            <CardHeader className="px-4 py-0">
              <CardTitle className="text-sm">{t("patchSets.title")}</CardTitle>
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
                <span className="text-muted-foreground">{t("common:common.none")}</span>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

function messageTypeLabel(type: string, t: TFunction<"changeDetail">): string {
  switch (type) {
    case "patchset-uploaded":
      return t("messages.types.patchSet");
    case "vote":
      return t("messages.types.vote");
    case "comment":
      return t("messages.types.comment");
    case "submitted":
      return t("common:status.merged");
    case "abandoned":
      return t("common:status.abandoned");
    case "restored":
      return t("messages.types.restored");
    case "reviewer-added":
      return t("messages.types.reviewerAdded");
    case "reviewer-removed":
      return t("messages.types.reviewerRemoved");
    case "topic":
      return t("messages.types.topic");
    case "wip":
      return t("messages.types.wip");
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
  const { t } = useTranslation("changeDetail");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [suggestions, setSuggestions] = useState<AccountInfo[]>([]);
  const reviewers: AccountInfo[] = change.reviewers ?? [];

  useEffect(() => {
    if (!canEdit) return;
    api
      .suggestReviewers(change._number)
      .then((s) => setSuggestions(s ?? []))
      .catch(() => setSuggestions([]));
  }, [change._number, canEdit]);

  const add = async (name?: string) => {
    const reviewer = (name ?? value).trim();
    if (!reviewer) return;
    setBusy(true);
    setErr("");
    try {
      await api.addReviewer(change._number, reviewer);
      setValue("");
      setSuggestions((s) => s.filter((a) => a.username !== reviewer));
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
        <CardTitle className="text-sm">{t("reviewers.title")}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 px-4">
        {reviewers.length === 0 ? (
          <span className="text-sm text-muted-foreground">{t("reviewers.empty")}</span>
        ) : (
          <ul className="flex flex-col gap-1">
            {reviewers.map((rv) => (
              <li key={rv._account_id} className="flex items-center gap-2 text-sm">
                <span className="truncate">{rv.name}</span>
                {rv._account_id === ownerId && (
                  <Badge variant="muted" className="text-[10px]">
                    {t("common:common.owner")}
                  </Badge>
                )}
                {canEdit && rv._account_id !== ownerId && (
                  <button
                    className="ml-auto text-muted-foreground hover:text-destructive"
                    onClick={() => remove(rv._account_id)}
                    title={t("reviewers.remove")}
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
          <div className="flex flex-col gap-1.5">
            {suggestions.filter((a) => a._account_id !== ownerId && !reviewers.some((rv) => rv._account_id === a._account_id)).length > 0 && (
              <div className="flex flex-wrap items-center gap-1">
                <span className="text-[11px] text-muted-foreground">{t("reviewers.suggested")}</span>
                {suggestions
                  .filter((a) => a._account_id !== ownerId && !reviewers.some((rv) => rv._account_id === a._account_id))
                  .map((a) => (
                    <button
                      key={a._account_id}
                      className="flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] text-muted-foreground hover:border-primary hover:text-foreground disabled:opacity-50"
                      onClick={() => add(a.username)}
                      disabled={busy}
                    >
                      <UserPlus className="size-3" />
                      {a.name || a.username}
                    </button>
                  ))}
              </div>
            )}
            <div className="flex gap-1.5">
              <Input
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder={t("placeholder.username")}
                className="h-8 text-xs"
                onKeyDown={(e) => {
                  if (e.key === "Enter") add();
                }}
              />
              <Button size="sm" variant="outline" disabled={busy || !value.trim()} onClick={() => add()}>
                <UserPlus className="size-4" />
              </Button>
            </div>
          </div>
        )}
        {err && <p className="text-xs text-destructive">{err}</p>}
      </CardContent>
    </Card>
  );
}

function AssigneeCard({
  change,
  canEdit,
  onDone,
}: {
  change: ChangeInfo;
  canEdit: boolean;
  onDone: () => Promise<void>;
}) {
  const { t } = useTranslation("changeDetail");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const assignee = change.assignee;

  const set = async () => {
    if (!value.trim()) return;
    setBusy(true);
    setErr("");
    try {
      await api.setAssignee(change._number, value.trim());
      setValue("");
      await onDone();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const clear = async () => {
    setBusy(true);
    setErr("");
    try {
      await api.deleteAssignee(change._number);
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
        <CardTitle className="flex items-center gap-1.5 text-sm">
          <User className="size-4 text-muted-foreground" />
          {t("assignee.title")}
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 px-4">
        {assignee ? (
          <div className="flex items-center gap-2 text-sm">
            <span className="truncate">{assignee.name}</span>
            {canEdit && (
              <button
                className="ml-auto text-muted-foreground hover:text-destructive"
                onClick={clear}
                title={t("assignee.remove")}
                disabled={busy}
              >
                <X className="size-3.5" />
              </button>
            )}
          </div>
        ) : (
          <span className="text-sm text-muted-foreground">{t("assignee.unassigned")}</span>
        )}
        {canEdit && (
          <div className="flex gap-1.5">
            <Input
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder={t("placeholder.username")}
              className="h-8 text-xs"
              onKeyDown={(e) => {
                if (e.key === "Enter") set();
              }}
            />
            <Button size="sm" variant="outline" disabled={busy || !value.trim()} onClick={set}>
              <Check className="size-4" />
            </Button>
          </div>
        )}
        {err && <p className="text-xs text-destructive">{err}</p>}
      </CardContent>
    </Card>
  );
}

function AttentionCard({
  change,
  canEdit,
  onDone,
}: {
  change: ChangeInfo;
  canEdit: boolean;
  onDone: () => Promise<void>;
}) {
  const { t } = useTranslation("changeDetail");
  const [value, setValue] = useState("");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const entries: AttentionEntry[] = change.attention_set ?? [];

  const add = async () => {
    if (!value.trim()) return;
    setBusy(true);
    setErr("");
    try {
      await api.addAttention(change._number, value.trim(), reason.trim() || undefined);
      setValue("");
      setReason("");
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
      await api.removeAttention(change._number, id);
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
        <CardTitle className="flex items-center gap-1.5 text-sm">
          <BellRing className="size-4 text-muted-foreground" />
          {t("attention.title")}
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 px-4">
        {entries.length === 0 ? (
          <span className="text-sm text-muted-foreground">{t("attention.empty")}</span>
        ) : (
          <ul className="flex flex-col gap-1">
            {entries.map((e) => (
              <li key={e.account._account_id} className="flex items-center gap-2 text-sm">
                <span className="truncate">{e.account.name}</span>
                {e.reason && (
                  <span className="truncate text-xs text-muted-foreground" title={e.reason}>
                    {e.reason}
                  </span>
                )}
                {canEdit && (
                  <button
                    className="ml-auto text-muted-foreground hover:text-destructive"
                    onClick={() => remove(e.account._account_id)}
                    title={t("attention.remove")}
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
          <div className="flex flex-col gap-1.5">
            <div className="flex gap-1.5">
              <Input
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder={t("placeholder.username")}
                className="h-8 text-xs"
                onKeyDown={(e) => {
                  if (e.key === "Enter") add();
                }}
              />
              <Button size="sm" variant="outline" disabled={busy || !value.trim()} onClick={add}>
                <BellRing className="size-4" />
              </Button>
            </div>
            <Input
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t("placeholder.reason")}
              className="h-8 text-xs"
            />
          </div>
        )}
        {err && <p className="text-xs text-destructive">{err}</p>}
      </CardContent>
    </Card>
  );
}

function HashtagsCard({
  change,
  canEdit,
  onDone,
}: {
  change: ChangeInfo;
  canEdit: boolean;
  onDone: () => Promise<void>;
}) {
  const { t } = useTranslation("changeDetail");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const tags: string[] = change.hashtags ?? [];

  const add = async () => {
    const tag = value.trim().replace(/^#/, "");
    if (!tag) return;
    setBusy(true);
    setErr("");
    try {
      await api.setHashtags(change._number, [tag]);
      setValue("");
      await onDone();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (tag: string) => {
    setBusy(true);
    setErr("");
    try {
      await api.setHashtags(change._number, [], [tag]);
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
        <CardTitle className="flex items-center gap-1.5 text-sm">
          <Tag className="size-4 text-muted-foreground" />
          {t("hashtags.title")}
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 px-4">
        {tags.length === 0 ? (
          <span className="text-sm text-muted-foreground">{t("hashtags.empty")}</span>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {tags.map((tag) => (
              <Badge key={tag} variant="secondary" className="gap-1 text-xs">
                #{tag}
                {canEdit && (
                  <button
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() => remove(tag)}
                    title={t("hashtags.remove")}
                    disabled={busy}
                  >
                    <X className="size-3" />
                  </button>
                )}
              </Badge>
            ))}
          </div>
        )}
        {canEdit && (
          <div className="flex gap-1.5">
            <Input
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder={t("placeholder.hashtag")}
              className="h-8 text-xs"
              onKeyDown={(e) => {
                if (e.key === "Enter") add();
              }}
            />
            <Button size="sm" variant="outline" disabled={busy || !value.trim()} onClick={add}>
              <Tag className="size-4" />
            </Button>
          </div>
        )}
        {err && <p className="text-xs text-destructive">{err}</p>}
      </CardContent>
    </Card>
  );
}

const CHECK_STATES = ["NOT_STARTED", "SCHEDULED", "RUNNING", "SUCCESSFUL", "FAILED"];

function checkStateIcon(state: string) {
  switch (state) {
    case "SUCCESSFUL":
      return <CircleCheck className="size-4 text-emerald-600" />;
    case "FAILED":
      return <CircleX className="size-4 text-red-600" />;
    case "RUNNING":
    case "SCHEDULED":
      return <CircleDot className="size-4 text-blue-600" />;
    default:
      return <CircleDot className="size-4 text-muted-foreground" />;
  }
}

function runStateToCheck(status: string) {
  switch (status) {
    case "SUCCESS":
      return "SUCCESSFUL";
    case "FAILURE":
    case "ERROR":
      return "FAILED";
    case "RUNNING":
    case "QUEUED":
      return "RUNNING";
    default:
      return "NOT_STARTED";
  }
}

function ChecksCard({ change, canEdit }: { change: ChangeInfo; canEdit: boolean }) {
  const { t } = useTranslation("changeDetail");
  const [runs, setRuns] = useState<CheckRun[]>([]);
  const [pipelines, setPipelines] = useState<PipelineRun[]>([]);
  const [logFor, setLogFor] = useState<PipelineRun | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [name, setName] = useState("");
  const [state, setState] = useState("SUCCESSFUL");
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const loadRuns = useCallback(() => {
    api
      .listCheckRuns(change._number)
      .then((r) => setRuns(r ?? []))
      .catch(() => setRuns([]))
      .finally(() => setLoaded(true));
    api
      .listChangePipelines(change._number)
      .then((p) => setPipelines(p ?? []))
      .catch(() => setPipelines([]));
  }, [change._number]);

  useEffect(loadRuns, [loadRuns]);

  const openLog = async (run: PipelineRun) => {
    setErr("");
    try {
      setLogFor(await api.getPipelineRun(run.id));
    } catch (e) {
      setErr((e as Error).message);
    }
  };

  const refresh = async () => {
    setRefreshing(true);
    loadRuns();
    setTimeout(() => setRefreshing(false), 500);
  };

  const save = async () => {
    const n = name.trim();
    if (!n) return;
    setBusy(true);
    setErr("");
    try {
      await api.upsertCheckRun(change._number, "current", {
        check_name: n,
        state,
        url: url.trim() || undefined,
      });
      setName("");
      setUrl("");
      loadRuns();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (checkName: string) => {
    setBusy(true);
    setErr("");
    try {
      await api.deleteCheckRun(change._number, "current", checkName);
      loadRuns();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card className="gap-3 py-4">
      <CardHeader className="px-4 py-0">
        <CardTitle className="flex items-center gap-1.5 text-sm">
          <ClipboardCheck className="size-4 text-muted-foreground" />
          {t("checks.title")}
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 px-4">
        {!loaded ? (
          <Skeleton className="h-6 w-full" />
        ) : runs.length === 0 ? (
          <span className="text-sm text-muted-foreground">{t("checks.empty")}</span>
        ) : (
          <ul className="flex flex-col gap-1.5">
            {runs.map((run) => (
              <li key={run.check_name} className="flex items-center gap-2 text-sm">
                {checkStateIcon(run.state)}
                <span className="font-medium">{run.check_name}</span>
                <span className="text-xs text-muted-foreground">{run.state}</span>
                {run.url && (
                  <a
                    href={run.url}
                    target="_blank"
                    rel="noreferrer"
                    className="text-muted-foreground hover:text-foreground"
                    title={run.url}
                  >
                    <ExternalLink className="size-3.5" />
                  </a>
                )}
                {canEdit && (
                  <button
                    className="ml-auto text-muted-foreground hover:text-destructive"
                    onClick={() => remove(run.check_name)}
                    title={t("checks.remove")}
                    disabled={busy}
                  >
                    <X className="size-3.5" />
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
        {pipelines.length > 0 && (
          <div className="flex flex-col gap-1.5 border-t pt-2">
            <div className="flex items-center gap-2">
              <span className="text-xs font-medium text-muted-foreground">{t("checks.pipelines")}</span>
              <button
                type="button"
                className="ml-auto text-muted-foreground hover:text-foreground disabled:opacity-50"
                title={t("checks.refresh")}
                aria-label={t("checks.refresh")}
                disabled={refreshing}
                onClick={refresh}
              >
                <RefreshCw className={`size-3.5 ${refreshing ? "animate-spin" : ""}`} />
              </button>
            </div>
            {pipelines.map((p) => (
              <div key={p.id} className="flex items-center gap-2 text-sm">
                {checkStateIcon(runStateToCheck(p.status))}
                <span className="font-medium">{p.config_name || `pipeline#${p.config_id}`}</span>
                <span className="text-xs text-muted-foreground">{p.status}</span>
                {!!p.runner && <span className="text-xs text-muted-foreground">{p.runner}</span>}
                <button
                  className="ml-auto flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                  onClick={() => openLog(p)}
                >
                  <ScrollText className="size-3.5" />
                  {t("checks.log")}
                </button>
              </div>
            ))}
          </div>
        )}
        {logFor && (
          <Dialog open onOpenChange={(v) => { if (!v) setLogFor(null); }}>
            <DialogContent className="max-w-3xl">
              <DialogHeader>
                <DialogTitle className="text-base">
                  {t("checks.logTitle", { name: logFor.config_name || `#${logFor.config_id}` })}
                </DialogTitle>
                <DialogDescription className="text-xs">
                  {logFor.status} · {logFor.runner} · #{logFor.change_number}/{logFor.patch_set}
                </DialogDescription>
              </DialogHeader>
              <pre className="max-h-[60vh] overflow-auto rounded bg-muted p-3 font-mono text-xs whitespace-pre-wrap">
                {logFor.log || t("checks.noLog")}
              </pre>
            </DialogContent>
          </Dialog>
        )}
        {canEdit && (
          <div className="flex flex-col gap-1.5 border-t pt-2">
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("placeholder.checkName")}
              className="h-8 text-xs"
            />
            <div className="flex gap-1.5">
              <select
                value={state}
                onChange={(e) => setState(e.target.value)}
                className="h-8 rounded-md border border-input bg-transparent px-2 text-xs outline-none focus-visible:border-ring"
              >
                {CHECK_STATES.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
              <Input
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder={t("placeholder.url")}
                className="h-8 flex-1 text-xs"
              />
            </div>
            <Button
              size="sm"
              variant="outline"
              disabled={busy || !name.trim()}
              onClick={save}
              className="self-start"
            >
              <Check className="size-4" />
              {t("checks.report")}
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
  const { t } = useTranslation("changeDetail");
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
      const trimmed = value.trim();
      if (trimmed) await api.setTopic(num, trimmed);
      else await api.deleteTopic(num);
      setOpen(false);
      await onDone(trimmed ? t("topic.set", { topic: trimmed }) : t("topic.cleared"));
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
          {t("topic.button")}
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{t("topic.title")}</DialogTitle>
          <DialogDescription>
            {t("topic.description")}
          </DialogDescription>
        </DialogHeader>
        <Input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder={t("placeholder.topic")}
          autoFocus
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
          }}
        />
        {err && <p className="text-sm text-destructive">{err}</p>}
        <DialogFooter>
          <Button variant="ghost" onClick={() => setOpen(false)}>
            {t("common:action.cancel")}
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? t("common:action.saving") : t("common:action.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ChangeEditDialog({
  path,
  initial,
  onClose,
  onSave,
  onError,
}: {
  path: string | null;
  initial: string;
  onClose: () => void;
  onSave: (path: string, content: string) => Promise<void>;
  onError: (msg: string) => void;
}) {
  const { t } = useTranslation("changeDetail");
  const [content, setContent] = useState(initial);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (path !== null) setContent(initial);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, initial]);

  const save = async () => {
    if (path === null) return;
    setBusy(true);
    try {
      await onSave(path, content);
    } catch (err) {
      onError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={path !== null} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("actions.editFile")}</DialogTitle>
          <DialogDescription className="font-mono text-xs">{path}</DialogDescription>
        </DialogHeader>
        <Textarea
          className="h-72 font-mono text-xs"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          spellCheck={false}
        />
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={busy}>
            {t("common:cancel")}
          </Button>
          <Button onClick={save} disabled={busy}>
            {t("actions.edit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DownloadDialog({
  open,
  onOpenChange,
  change,
  currentPS,
  sshPort,
  username,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  change: ChangeInfo;
  currentPS: number;
  sshPort: string;
  username: string;
}) {
  const { t } = useTranslation("changeDetail");
  const [scheme, setScheme] = useState<"http" | "ssh">("http");
  const [copied, setCopied] = useState("");

  const ref = `refs/changes/${String(change._number % 100).padStart(2, "0")}/${change._number}/${currentPS}`;
  const httpURL = `${window.location.origin}/git/${change.project}.git`;
  const sshURL = sshPort
    ? `ssh://${username ? username + "@" : ""}${window.location.hostname}:${sshPort}/${change.project}.git`
    : "";
  const hasSSH = sshPort !== "";
  const base = scheme === "ssh" && hasSSH ? sshURL : httpURL;

  const commands: { key: string; label: string; cmd: string }[] = [
    { key: "checkout", label: "Checkout", cmd: `git fetch ${base} ${ref} && git checkout FETCH_HEAD` },
    { key: "fetch", label: "Fetch", cmd: `git fetch ${base} ${ref}` },
    { key: "cherrypick", label: "Cherry Pick", cmd: `git fetch ${base} ${ref} && git cherry-pick FETCH_HEAD` },
    { key: "pull", label: "Pull", cmd: `git pull ${base} ${ref}` },
    { key: "patch", label: "Patch", cmd: `git fetch ${base} ${ref} && git format-patch -1 --stdout FETCH_HEAD` },
  ];

  const copy = async (key: string, cmd: string) => {
    try {
      await navigator.clipboard.writeText(cmd);
      setCopied(key);
      setTimeout(() => setCopied(""), 1500);
    } catch {
      /* clipboard unavailable */
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("download.title")}</DialogTitle>
          <DialogDescription>{t("download.desc")}</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-0.5 rounded-md border p-0.5 self-start">
          <Button size="sm" variant={scheme === "http" ? "secondary" : "ghost"} className="h-7 px-2 text-xs" onClick={() => setScheme("http")}>
            HTTP
          </Button>
          <Button size="sm" variant={scheme === "ssh" ? "secondary" : "ghost"} className="h-7 px-2 text-xs" onClick={() => setScheme("ssh")} disabled={!hasSSH}>
            SSH
          </Button>
        </div>
        <div className="flex flex-col gap-2">
          {commands.map((c) => (
            <div key={c.key} className="flex items-center gap-2">
              <span className="w-24 shrink-0 text-xs text-muted-foreground">{c.label}</span>
              <code className="min-w-0 flex-1 truncate rounded bg-muted/40 px-2 py-1 font-mono text-[11px]" title={c.cmd}>
                {c.cmd}
              </code>
              <Button size="sm" variant="ghost" className="h-7 shrink-0 px-2" onClick={() => copy(c.key, c.cmd)}>
                {copied === c.key ? <ClipboardCheck className="size-3.5 text-emerald-600" /> : <Copy className="size-3.5" />}
              </Button>
            </div>
          ))}
        </div>
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
  const { t } = useTranslation("changeDetail");
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
          <DialogTitle>{t("cherryPick.title", { num })}</DialogTitle>
          <DialogDescription>
            {t("cherryPick.description")}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-2">
          <label className="text-sm font-medium" htmlFor="cp-dest">
            {t("cherryPick.destination")}
          </label>
          <Input
            id="cp-dest"
            list="cp-branches"
            value={dest}
            onChange={(e) => setDest(e.target.value)}
            placeholder={t("placeholder.branch")}
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
            {t("common:action.cancel")}
          </Button>
          <Button onClick={submit} disabled={busy || !dest.trim()}>
            {busy ? t("cherryPick.busy") : t("actions.cherryPick")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ConflictDialog({
  open,
  onOpenChange,
  num,
  onResolved,
  onError,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  num: number;
  onResolved: () => void | Promise<void>;
  onError: (msg: string) => void;
}) {
  const { t } = useTranslation("changeDetail");
  const [conflicts, setConflicts] = useState<ConflictFile[]>([]);
  const [resolutions, setResolutions] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    setErr("");
    setResolutions({});
    api
      .rebaseConflicts(num)
      .then((files) => {
        const list = files ?? [];
        setConflicts(list);
        setResolutions(
          Object.fromEntries(list.map((f) => [f.path, f.conflict])),
        );
      })
      .catch((e) => {
        const msg = (e as Error).message;
        setErr(msg);
        onError(msg);
      })
      .finally(() => setLoading(false));
  }, [open, num, onError]);

  const setRes = (path: string, content: string) =>
    setResolutions((r) => ({ ...r, [path]: content }));

  const resolvedAll =
    conflicts.length > 0 &&
    conflicts.every(
      (f) => !(resolutions[f.path] ?? "").includes("<<<<<<<"),
    );

  const submit = async () => {
    setBusy(true);
    setErr("");
    try {
      await api.resolveRebase(num, resolutions);
      onOpenChange(false);
      await onResolved();
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
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t("conflict.title", { num })}</DialogTitle>
          <DialogDescription>{t("conflict.description")}</DialogDescription>
        </DialogHeader>
        {loading ? (
          <div className="space-y-2">
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-24 w-full" />
          </div>
        ) : (
          <div className="max-h-[60vh] space-y-4 overflow-y-auto pr-1">
            {conflicts.map((f) => (
              <div key={f.path} className="space-y-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-mono text-sm font-medium">{f.path}</span>
                  <div className="ml-auto flex gap-1.5">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setRes(f.path, f.ours)}
                      disabled={f.ours === ""}
                    >
                      {t("conflict.useOurs")}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setRes(f.path, f.theirs)}
                      disabled={f.theirs === ""}
                    >
                      {t("conflict.useTheirs")}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setRes(f.path, f.base)}
                      disabled={f.base === ""}
                    >
                      {t("conflict.useBase")}
                    </Button>
                  </div>
                </div>
                <Textarea
                  className="font-mono text-xs"
                  rows={10}
                  value={resolutions[f.path] ?? ""}
                  onChange={(e) => setRes(f.path, e.target.value)}
                />
              </div>
            ))}
            {conflicts.length === 0 && (
              <p className="text-sm text-muted-foreground">{t("conflict.empty")}</p>
            )}
          </div>
        )}
        {err && <p className="text-sm text-destructive">{err}</p>}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t("common:action.cancel")}
          </Button>
          <Button
            onClick={submit}
            disabled={busy || loading || conflicts.length === 0 || !resolvedAll}
          >
            {busy ? t("conflict.busy") : t("conflict.submit")}
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
  const { t } = useTranslation("changeDetail");
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
          title={t("common:action.copy")}
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

type SplitRow = { left: DiffLine | null; right: DiffLine | null };

function splitRows(hunk: DiffHunk): SplitRow[] {
  const rows: SplitRow[] = [];
  let dels: DiffLine[] = [];
  let adds: DiffLine[] = [];
  const flush = () => {
    const n = Math.max(dels.length, adds.length);
    for (let i = 0; i < n; i++) rows.push({ left: dels[i] ?? null, right: adds[i] ?? null });
    dels = [];
    adds = [];
  };
  for (const line of hunk.lines) {
    if (line.type === "del") dels.push(line);
    else if (line.type === "add") adds.push(line);
    else {
      flush();
      rows.push({ left: line, right: line });
    }
  }
  flush();
  return rows;
}

interface EditorState {
  line: number;
  draftId?: number;
  message: string;
  inReplyTo?: number;
}

function DiffView({
  file,
  num,
  mode,
  comments,
  drafts,
  canComment,
  onApplySuggestion,
  applyingSuggestion,
  reload,
}: {
  file: FileDiff;
  num: number;
  mode: "unified" | "split";
  comments: CommentInfo[];
  drafts: CommentDraftInfo[];
  canComment: boolean;
  onApplySuggestion?: (comment: CommentInfo, suggestion: string) => void;
  applyingSuggestion?: boolean;
  reload: () => Promise<void>;
}) {
  const { t } = useTranslation("changeDetail");
  const [editor, setEditor] = useState<EditorState | null>(null);
  const [busy, setBusy] = useState(false);
  const lang = langForPath(file.path);

  if (file.binary) {
    return <p className="bg-muted/30 px-4 py-3 text-xs text-muted-foreground">{t("diff.binary")}</p>;
  }

  const itemsFor = (line: DiffLine) => {
    const cs = comments.filter((c) => c.line !== 0 && (c.line === line.new_no || c.line === line.old_no));
    const ds = drafts.filter((d) => d.line !== 0 && (d.line === line.new_no || d.line === line.old_no));
    return { cs, ds };
  };

  const lineAnchor = (line: DiffLine) => (line.type === "del" ? line.old_no ?? 0 : line.new_no ?? 0);

  // Map each anchor line number to the last (hunk,line) that claims it, so a
  // comment is rendered once even when a del(old N) and add(new N) collide.
  const hostFor = new Map<number, string>();
  file.hunks.forEach((h, hi) =>
    h.lines.forEach((line, li) => {
      const a = lineAnchor(line);
      if (a !== 0) hostFor.set(a, `${hi}:${li}`);
    }),
  );

  const openNew = (line: DiffLine) => {
    if (!canComment) return;
    const anchor = lineAnchor(line);
    if (editor?.line === anchor) {
      setEditor(null);
      return;
    }
    setEditor({ line: anchor, message: "" });
  };

  const saveDraft = async () => {
    if (!editor || !editor.message.trim()) return;
    setBusy(true);
    try {
      await api.putDraft(num, {
        id: editor.draftId,
        path: file.path,
        line: editor.line,
        message: editor.message.trim(),
        in_reply_to: editor.inReplyTo,
      });
      setEditor(null);
      await reload();
    } finally {
      setBusy(false);
    }
  };

  const deleteDraft = async (id: number) => {
    setBusy(true);
    try {
      await api.deleteDraft(num, id);
      if (editor?.draftId === id) setEditor(null);
      await reload();
    } finally {
      setBusy(false);
    }
  };

  const toggleResolve = async (c: CommentInfo) => {
    setBusy(true);
    try {
      await api.resolveComment(num, c.id, !c.resolved);
      await reload();
    } finally {
      setBusy(false);
    }
  };

  const renderThread = (cs: CommentInfo[], ds: CommentDraftInfo[], anchor: number) => (
    <>
      {cs.map((c) => (
        <CommentCard
          key={c.id}
          comment={c}
          canComment={canComment}
          busy={busy}
          onResolve={() => toggleResolve(c)}
          onReply={() => setEditor({ line: anchor, message: "", inReplyTo: c.id })}
          onApplySuggestion={onApplySuggestion}
          applyingSuggestion={applyingSuggestion}
        />
      ))}
      {ds.map((d) => (
        <DraftCard
          key={d.id}
          draft={d}
          busy={busy}
          onEdit={() => setEditor({ line: d.line, draftId: d.id, message: d.message, inReplyTo: d.in_reply_to })}
          onDelete={() => deleteDraft(d.id)}
        />
      ))}
      {editor?.line === anchor && (
        <CommentEditor
          editor={editor}
          busy={busy}
          onChange={(m) => setEditor({ ...editor, message: m })}
          onCancel={() => setEditor(null)}
          onSave={saveDraft}
        />
      )}
    </>
  );

  if (mode === "split") {
    return (
      <div className="overflow-x-auto border-t bg-card font-mono text-xs leading-5">
        {file.hunks.map((hunk, hi) => (
          <div key={hi}>
            <div className="bg-blue-50 px-4 py-0.5 text-[11px] text-blue-700 dark:bg-blue-950/40 dark:text-blue-300">
              {hunk.header}
            </div>
            {splitRows(hunk).map((row, ri) => {
              const anchor = row.right ? lineAnchor(row.right) : row.left ? lineAnchor(row.left) : 0;
              const cs = comments.filter(
                (c) => c.line !== 0 && (c.line === row.left?.old_no || c.line === row.right?.new_no),
              );
              const ds = drafts.filter(
                (d) => d.line !== 0 && (d.line === row.left?.old_no || d.line === row.right?.new_no),
              );
              return (
                <div key={ri}>
                  <div className="grid grid-cols-2">
                    <SplitCell line={row.left} side="left" lang={lang} onClick={() => row.left && openNew(row.left)} canComment={canComment} />
                    <SplitCell line={row.right} side="right" lang={lang} onClick={() => row.right && openNew(row.right)} canComment={canComment} />
                  </div>
                  {(cs.length > 0 || ds.length > 0 || editor?.line === anchor) && (
                    <div className="border-y bg-muted/20">{renderThread(cs, ds, anchor)}</div>
                  )}
                </div>
              );
            })}
          </div>
        ))}
        {file.hunks.length === 0 && (
          <p className="px-4 py-3 text-muted-foreground">{t("diff.noTextChanges")}</p>
        )}
      </div>
    );
  }

  return (
    <div className="overflow-x-auto border-t bg-card font-mono text-xs leading-5">
      {file.hunks.map((hunk, hi) => (
        <div key={hi}>
          <div className="bg-blue-50 px-4 py-0.5 text-[11px] text-blue-700 dark:bg-blue-950/40 dark:text-blue-300">
            {hunk.header}
          </div>
          {hunk.lines.map((line, li) => {
            const anchor = lineAnchor(line);
            const { cs, ds } = itemsFor(line);
            // A del(old N) and add(new N) share an anchor; render the thread once,
            // on the last line claiming it (the new/add side).
            const isHost = hostFor.get(anchor) === `${hi}:${li}`;
            return (
              <div key={li}>
                <div
                  className={cn(
                    "group flex min-w-max cursor-pointer hover:bg-accent/60",
                    line.type === "add" && "bg-emerald-50 dark:bg-emerald-950/30",
                    line.type === "del" && "bg-red-50 dark:bg-red-950/30",
                  )}
                  onClick={() => openNew(line)}
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
                  <span
                    className="whitespace-pre pr-4"
                    dangerouslySetInnerHTML={{ __html: highlightLine(line.text, lang) }}
                  />
                  {canComment && (
                    <span className="ml-auto hidden shrink-0 items-center pr-2 text-muted-foreground group-hover:flex">
                      <MessageSquarePlus className="size-3.5" />
                    </span>
                  )}
                </div>
                {isHost && (cs.length > 0 || ds.length > 0 || editor?.line === anchor) && renderThread(cs, ds, anchor)}
              </div>
            );
          })}
        </div>
      ))}
      {file.hunks.length === 0 && (
        <p className="px-4 py-3 text-muted-foreground">{t("diff.noTextChanges")}</p>
      )}
    </div>
  );
}

function SplitCell({
  line,
  side,
  canComment,
  lang,
  onClick,
}: {
  line: DiffLine | null;
  side: "left" | "right";
  canComment: boolean;
  lang?: string;
  onClick: () => void;
}) {
  if (!line) {
    return <div className="min-w-0 border-r bg-muted/20 px-2 last:border-r-0" />;
  }
  const no = side === "left" ? line.old_no : line.new_no;
  const show = side === "left" ? line.type !== "add" : line.type !== "del";
  return (
    <div
      className={cn(
        "group flex min-w-0 cursor-pointer border-r px-1 hover:bg-accent/60 last:border-r-0",
        show && line.type === "add" && "bg-emerald-50 dark:bg-emerald-950/30",
        show && line.type === "del" && "bg-red-50 dark:bg-red-950/30",
        !show && "bg-muted/10",
      )}
      onClick={onClick}
    >
      <span className="w-10 shrink-0 select-none text-right text-muted-foreground/70">{show ? no ?? "" : ""}</span>
      <span
        className={cn(
          "w-4 shrink-0 select-none text-center font-bold",
          show && line.type === "add" && "text-emerald-600",
          show && line.type === "del" && "text-red-600",
        )}
      >
        {show ? (line.type === "add" ? "+" : line.type === "del" ? "−" : " ") : ""}
      </span>
      <span
        className="whitespace-pre-wrap break-all pr-2"
        dangerouslySetInnerHTML={{ __html: show ? highlightLine(line.text, lang) : "" }}
      />
      {canComment && (
        <span className="ml-auto hidden shrink-0 items-center text-muted-foreground group-hover:flex">
          <MessageSquarePlus className="size-3.5" />
        </span>
      )}
    </div>
  );
}

function CommentCard({
  comment,
  canComment,
  busy,
  onResolve,
  onReply,
  onApplySuggestion,
  applyingSuggestion,
}: {
  comment: CommentInfo;
  canComment: boolean;
  busy: boolean;
  onResolve: () => void;
  onReply: () => void;
  onApplySuggestion?: (comment: CommentInfo, suggestion: string) => void;
  applyingSuggestion?: boolean;
}) {
  const { t } = useTranslation("changeDetail");
  const suggestion = parseSuggestion(comment.message);
  return (
    <div className="flex min-w-0 max-w-[calc(100vw-2rem)] gap-2 border-b bg-amber-50/70 px-4 py-2 last:border-b-0 dark:bg-amber-950/20 sm:px-14">
      <div className="min-w-0 flex-1 font-sans">
        <div className="flex flex-wrap items-center gap-1.5 text-xs">
          <span className="font-semibold">{comment.author.name}</span>
          {comment.robot_id && (
            <Badge variant="muted" className="text-[10px]" title={comment.robot_run_id || comment.robot_id}>
              {comment.robot_id}
            </Badge>
          )}
          <span className="text-muted-foreground">
            · {t("ps", { ps: comment.patch_set })} · {timeAgo(comment.updated)}
          </span>
          {comment.in_reply_to ? (
            <Badge variant="muted" className="text-[10px]">{t("comments.replyBadge")}</Badge>
          ) : comment.resolved ? (
            <Badge variant="success" className="gap-1 text-[10px]"><Check className="size-3" /> {t("comments.resolved")}</Badge>
          ) : null}
        </div>
        <p className={cn("whitespace-pre-wrap text-xs text-muted-foreground", comment.resolved && "opacity-60 line-through")}>
          {comment.message}
        </p>
        {suggestion !== null && (
          <pre className="mt-1 overflow-x-auto rounded border bg-muted/40 p-2 font-mono text-[11px] leading-4 text-foreground">
            {suggestion}
          </pre>
        )}
        <div className="mt-1 flex items-center gap-3 text-[11px]">
          <button
            className="flex items-center gap-1 text-muted-foreground hover:text-foreground disabled:opacity-50"
            onClick={onResolve}
            disabled={busy}
          >
            <Check className="size-3" />
            {comment.resolved ? t("comments.unresolve") : t("comments.resolve")}
          </button>
          {canComment && (
            <button
              className="flex items-center gap-1 text-muted-foreground hover:text-foreground"
              onClick={onReply}
            >
              <CornerDownRight className="size-3" />
              {t("comments.reply")}
            </button>
          )}
          {suggestion !== null && onApplySuggestion && (
            <button
              className="flex items-center gap-1 text-primary hover:underline disabled:opacity-50"
              onClick={() => onApplySuggestion(comment, suggestion)}
              disabled={busy || applyingSuggestion}
            >
              <Check className="size-3" />
              {applyingSuggestion ? t("comments.applyingSuggestion") : t("comments.applySuggestion")}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function DraftCard({
  draft,
  busy,
  onEdit,
  onDelete,
}: {
  draft: CommentDraftInfo;
  busy: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation("changeDetail");
  return (
    <div className="flex min-w-0 max-w-[calc(100vw-2rem)] gap-2 border-b bg-sky-50/70 px-4 py-2 last:border-b-0 dark:bg-sky-950/20 sm:px-14">
      <div className="min-w-0 flex-1 font-sans">
        <div className="flex items-center gap-1.5 text-xs">
          <Badge variant="secondary" className="gap-1 text-[10px]">
            <Pencil className="size-3" /> {t("drafts.badge")}
          </Badge>
          {draft.in_reply_to ? <Badge variant="muted" className="text-[10px]">{t("comments.replyBadge")}</Badge> : null}
          <span className="text-muted-foreground">{timeAgo(draft.updated)}</span>
        </div>
        <p className="whitespace-pre-wrap text-xs text-muted-foreground">{draft.message}</p>
        <div className="mt-1 flex items-center gap-3 text-[11px]">
          <button className="flex items-center gap-1 text-muted-foreground hover:text-foreground" onClick={onEdit}>
            <Pencil className="size-3" /> {t("common:action.edit")}
          </button>
          <button
            className="flex items-center gap-1 text-muted-foreground hover:text-destructive disabled:opacity-50"
            onClick={onDelete}
            disabled={busy}
          >
            <Trash2 className="size-3" /> {t("drafts.discard")}
          </button>
        </div>
      </div>
    </div>
  );
}

function CommentEditor({
  editor,
  busy,
  onChange,
  onCancel,
  onSave,
}: {
  editor: EditorState;
  busy: boolean;
  onChange: (msg: string) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const { t } = useTranslation("changeDetail");
  return (
    <div className="max-w-[calc(100vw-2rem)] border-y bg-muted/40 px-4 py-2 sm:px-14" onClick={(e) => e.stopPropagation()}>
      <Textarea
        autoFocus
        value={editor.message}
        onChange={(e) => onChange(e.target.value)}
        placeholder={
          editor.inReplyTo
            ? t("placeholder.reply")
            : t("placeholder.commentLine", { line: editor.line })
        }
        className="min-h-14 font-sans text-xs"
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) onSave();
          if (e.key === "Escape") onCancel();
        }}
      />
      <div className="mt-1.5 flex items-center justify-end gap-2">
        <span className="mr-auto font-sans text-[11px] text-muted-foreground">
          {t("editor.hint")}
        </span>
        <Button size="sm" variant="ghost" onClick={onCancel}>
          {t("common:action.cancel")}
        </Button>
        <Button size="sm" disabled={busy || !editor.message.trim()} onClick={onSave}>
          <Send className="size-3.5" />
          {t("editor.saveDraft")}
        </Button>
      </div>
    </div>
  );
}

function ReviewDialog({
  num,
  draftCount,
  onDone,
}: {
  num: number;
  draftCount: number;
  onDone: (msg: string) => Promise<void>;
}) {
  const { t } = useTranslation("changeDetail");
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
      await onDone(t("review.saved"));
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
          {t("review.button")}
          {draftCount > 0 && (
            <Badge variant="secondary" className="ml-1 px-1.5 text-[10px]">
              {draftCount}
            </Badge>
          )}
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("review.title", { num })}</DialogTitle>
          <DialogDescription>
            {t("review.description")}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          {draftCount > 0 && (
            <p className="rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground">
              {t("review.draftsPublish", { count: draftCount })}
            </p>
          )}
          <VoteRow label="Code-Review" value={cr} onChange={setCr} options={[-2, -1, 0, 1, 2]} />
          <VoteRow label="Verified" value={verified} onChange={setVerified} options={[-1, 0, 1]} />
          <Textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            placeholder={t("placeholder.coverMessage")}
            rows={3}
          />
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => setOpen(false)}>
            {t("common:action.cancel")}
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? t("review.sending") : t("review.send")}
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
