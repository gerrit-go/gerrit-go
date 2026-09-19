import { Fragment, useEffect, useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Bell, BellOff, ChevronRight, File, Folder, GitCommitHorizontal, Pencil, Terminal } from "lucide-react";
import { api, type BlameLine, type BranchInfo, type CommitInfo, type FileDiff, type FileEntry, type FileLogEntry } from "@/lib/api";
import { highlightBlock, highlightLine, langForPath } from "@/lib/highlight";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { timeAgo } from "@/lib/utils";
import { useAuth } from "@/auth";
import ProjectAccessPanel from "@/pages/ProjectAccessPanel";
import ProjectSubmitPanel from "@/pages/ProjectSubmitPanel";
import ProjectPipelinesPanel from "@/pages/ProjectPipelinesPanel";
import { BranchesPanel, ManagePanel, TagsPanel, WebhooksPanel } from "@/pages/ProjectRefsPanel";
import {
  Table,
  TableBody,
  TableCell,
  TableRow,
} from "@/components/ui/table";

export default function ProjectDetailPage() {
  const params = useParams();
  const project = decodeURIComponent(params["*"] ?? "");
  const { user } = useAuth();
  const { t } = useTranslation("projectDetail");
  const [searchParams, setSearchParams] = useSearchParams();
  const revision = searchParams.get("revision") ?? "";
  const dirPath = searchParams.get("path") ?? "";
  const tab = searchParams.get("tab") ?? "files";
  const viewFile = searchParams.get("file") ?? "";

  const [branches, setBranches] = useState<BranchInfo[]>([]);
  const [entries, setEntries] = useState<FileEntry[] | null>(null);
  const [commits, setCommits] = useState<CommitInfo[] | null>(null);
  const [expandedSha, setExpandedSha] = useState<string | null>(null);
  const [commitDiff, setCommitDiff] = useState<FileDiff[] | null>(null);
  const [fileText, setFileText] = useState<string | null>(null);
  const [fileView, setFileView] = useState<"code" | "blame" | "history">("code");
  const [blameLines, setBlameLines] = useState<BlameLine[] | null>(null);
  const [fileHistory, setFileHistory] = useState<FileLogEntry[] | null>(null);
  const [error, setError] = useState("");
  const [projectState, setProjectState] = useState("ACTIVE");
  const [canEdit, setCanEdit] = useState(false);
  const [editOpen, setEditOpen] = useState(false);

  useEffect(() => {
    api.branches(project).then((b) => setBranches(b ?? [])).catch(() => setBranches([]));
    api.projectConfig(project).then((c) => setProjectState(c.state || "ACTIVE")).catch(() => {});
    api.projectAccess(project).then((a) => setCanEdit(!!a.can_edit)).catch(() => setCanEdit(false));
  }, [project]);

  const isAdmin = !!user?.admin;


  const rev = revision || branches[0]?.name || "HEAD";

  useEffect(() => {
    if (!project) return;
    setEntries(null);
    api
      .tree(project, revision, dirPath)
      .then((data) => setEntries(data ?? []))
      .catch((err) => {
        setError((err as Error).message);
        setEntries([]);
      });
  }, [project, rev, dirPath, revision]);

  useEffect(() => {
    if (!project) return;
    setCommits(null);
    api
      .commits(project, revision || undefined)
      .then((data) => setCommits(data ?? []))
      .catch(() => setCommits([]));
  }, [project, rev, revision]);

  useEffect(() => {
    if (!viewFile) {
      setFileText(null);
      return;
    }
    setFileText(null);
    api
      .fileText(project, revision || rev, viewFile)
      .then(setFileText)
      .catch(() => setFileText(t("fileLoadFailed")));
  }, [project, viewFile, revision, rev, t]);

  useEffect(() => {
    setBlameLines(null);
    setFileHistory(null);
    if (!viewFile) return;
    if (fileView === "blame") {
      api
        .blame(project, revision || rev, viewFile)
        .then(setBlameLines)
        .catch(() => setBlameLines([]));
    } else if (fileView === "history") {
      api
        .fileLog(project, revision || rev, viewFile)
        .then(setFileHistory)
        .catch(() => setFileHistory([]));
    }
  }, [project, viewFile, revision, rev, fileView]);

  const crumbs = useMemo(() => {
    if (!dirPath) return [];
    const parts = dirPath.split("/");
    return parts.map((p, i) => ({ name: p, path: parts.slice(0, i + 1).join("/") }));
  }, [dirPath]);

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(key, value);
    else next.delete(key);
    setSearchParams(next);
  };

  const toggleCommit = (sha: string) => {
    if (expandedSha === sha) {
      setExpandedSha(null);
      setCommitDiff(null);
      return;
    }
    setExpandedSha(sha);
    setCommitDiff(null);
    api
      .commitDiff(project, sha)
      .then((d) => setCommitDiff(d ?? []))
      .catch(() => setCommitDiff([]));
  };

  if (!project) return null;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-0">
          <h1 className="flex items-center gap-2 break-all text-xl font-semibold">
            {project}
            {projectState !== "ACTIVE" && (
              <span className="rounded-md bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
                {projectState}
              </span>
            )}
          </h1>
          <div className="mt-1 flex items-center gap-1.5 break-all font-mono text-xs text-muted-foreground">
            <Terminal className="size-3 shrink-0" />
            <span className="min-w-0">git clone {window.location.origin}/git/{project}.git</span>
          </div>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <WatchButton project={project} />
          <select
            value={revision || branches[0]?.name || ""}
            onChange={(e) => setParam("revision", e.target.value === (branches[0]?.name ?? "") ? "" : e.target.value)}
            className="h-9 rounded-md border border-input bg-transparent px-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            {branches.length === 0 && <option value="">{t("emptyRepo")}</option>}
            {branches.map((b) => (
              <option key={b.name} value={b.name}>
                {b.name}
              </option>
            ))}
          </select>
        </div>
      </div>

      {error && <p className="text-sm text-destructive">{error}</p>}

      <Tabs value={tab} onValueChange={(v) => setParam("tab", v === "files" ? "" : v)}>
        <TabsList>
          <TabsTrigger value="files">{t("tabs.files")}</TabsTrigger>
          <TabsTrigger value="commits">{t("tabs.commits")}</TabsTrigger>
          <TabsTrigger value="branches">{t("tabs.branches")}</TabsTrigger>
          <TabsTrigger value="tags">{t("tabs.tags")}</TabsTrigger>
          <TabsTrigger value="submit">{t("tabs.submit")}</TabsTrigger>
          <TabsTrigger value="access">{t("tabs.access")}</TabsTrigger>
          <TabsTrigger value="manage">{t("tabs.manage")}</TabsTrigger>
          <TabsTrigger value="webhooks">{t("tabs.webhooks")}</TabsTrigger>
          <TabsTrigger value="pipelines">{t("tabs.pipelines")}</TabsTrigger>
        </TabsList>

        <TabsContent value="files" className="mt-3">
          {viewFile ? (
            <div className="overflow-hidden rounded-lg border">
              <div className="flex items-center gap-2 border-b bg-muted/40 px-4 py-2">
                <button
                  className="text-sm text-muted-foreground hover:text-foreground"
                  onClick={() => setParam("file", "")}
                >
                  {t("backToFiles")}
                </button>
                <span className="ml-auto font-mono text-sm">{viewFile}</span>
                <div className="flex items-center gap-0.5 rounded-md border p-0.5">
                  {(["code", "blame", "history"] as const).map((m) => (
                    <Button
                      key={m}
                      size="sm"
                      variant={fileView === m ? "secondary" : "ghost"}
                      className="h-7 px-2 text-xs"
                      onClick={() => setFileView(m)}
                    >
                      {m === "code" ? t("viewCode") : m === "blame" ? t("viewBlame") : t("viewHistory")}
                    </Button>
                  ))}
                </div>
                {canEdit && projectState === "ACTIVE" && fileText !== null && fileView === "code" && (
                  <Button size="sm" variant="outline" onClick={() => setEditOpen(true)}>
                    <Pencil className="size-3.5" />
                    {t("common:action.edit")}
                  </Button>
                )}
              </div>
              {fileView === "code" &&
                (fileText === null ? (
                  <Skeleton className="m-4 h-64 w-auto" />
                ) : (
                  <pre className="max-h-[70vh] overflow-auto bg-muted/30 p-4 text-xs leading-5 text-foreground">
                    <code
                      dangerouslySetInnerHTML={{
                        __html: highlightBlock(fileText, langForPath(viewFile)),
                      }}
                    />
                  </pre>
                ))}
              {fileView === "blame" &&
                (blameLines === null ? (
                  <Skeleton className="m-4 h-64 w-auto" />
                ) : (
                  <div className="max-h-[70vh] overflow-auto bg-muted/30 font-mono text-xs leading-5">
                    {blameLines.map((l) => (
                      <div key={l.line} className="flex hover:bg-accent/40">
                        <span className="w-14 shrink-0 select-none border-r px-2 text-right text-muted-foreground/70">
                          {l.line}
                        </span>
                        <span
                          className="w-40 shrink-0 select-none truncate border-r px-2 text-muted-foreground"
                          title={`${l.author} · ${l.summary}`}
                        >
                          {l.author || l.sha.slice(0, 8)}
                        </span>
                        <span className="w-20 shrink-0 select-none border-r px-2 text-muted-foreground/70">
                          {timeAgo(new Date(l.when * 1000).toISOString())}
                        </span>
                        <span
                          className="whitespace-pre px-2 text-foreground"
                          dangerouslySetInnerHTML={{ __html: highlightLine(l.text, langForPath(viewFile)) }}
                        />
                      </div>
                    ))}
                  </div>
                ))}
              {fileView === "history" &&
                (fileHistory === null ? (
                  <Skeleton className="m-4 h-64 w-auto" />
                ) : fileHistory.length === 0 ? (
                  <p className="p-6 text-center text-sm text-muted-foreground">{t("historyEmpty")}</p>
                ) : (
                  <div className="max-h-[70vh] divide-y overflow-auto">
                    {fileHistory.map((e) => (
                      <div key={e.sha} className="flex items-center gap-3 px-4 py-2 text-sm">
                        <code className="shrink-0 font-mono text-xs text-muted-foreground">{e.sha.slice(0, 10)}</code>
                        <span className="min-w-0 flex-1 truncate">{e.subject}</span>
                        <span className="shrink-0 text-muted-foreground">{e.author}</span>
                        <span className="shrink-0 text-xs text-muted-foreground/70">{timeAgo(new Date(e.when * 1000).toISOString())}</span>
                      </div>
                    ))}
                  </div>
                ))}
            </div>
          ) : (
            <div className="rounded-lg border">
              <div className="flex items-center gap-1 border-b bg-muted/40 px-4 py-2 text-sm">
                <button
                  className="font-medium hover:underline"
                  onClick={() => setParam("path", "")}
                >
                  {project}
                </button>
                {crumbs.map((c) => (
                  <span key={c.path} className="flex items-center gap-1">
                    <ChevronRight className="size-3 text-muted-foreground" />
                    <button
                      className="font-medium hover:underline"
                      onClick={() => setParam("path", c.path)}
                    >
                      {c.name}
                    </button>
                  </span>
                ))}
              </div>
              {entries === null ? (
                <div className="flex flex-col gap-1 p-4">
                  {Array.from({ length: 5 }).map((_, i) => (
                    <Skeleton key={i} className="h-8 w-full" />
                  ))}
                </div>
              ) : entries.length === 0 ? (
                <p className="p-8 text-center text-sm text-muted-foreground">
                  {t("emptyBranch")}
                </p>
              ) : (
                <Table>
                  <TableBody>
                    {entries.map((e) => (
                      <TableRow
                        key={e.name}
                        className="cursor-pointer"
                        onClick={() =>
                          e.type === "tree"
                            ? setParam("path", dirPath ? `${dirPath}/${e.name}` : e.name)
                            : setParam("file", dirPath ? `${dirPath}/${e.name}` : e.name)
                        }
                      >
                        <TableCell className="w-8">
                          {e.type === "tree" ? (
                            <Folder className="size-4 text-amber-500" />
                          ) : (
                            <File className="size-4 text-muted-foreground" />
                          )}
                        </TableCell>
                        <TableCell className="font-medium">{e.name}</TableCell>
                        <TableCell className="text-right text-xs text-muted-foreground">
                          {e.type === "blob" && e.size !== undefined ? `${e.size} B` : ""}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </div>
          )}
        </TabsContent>

        <TabsContent value="commits" className="mt-3">
          {commits === null ? (
            <div className="flex flex-col gap-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : commits.length === 0 ? (
            <p className="p-8 text-center text-sm text-muted-foreground">{t("noCommits")}</p>
          ) : (
            <div className="rounded-lg border">
              <Table>
                <TableBody>
                  {commits.map((c) => (
                    <Fragment key={c.sha}>
                      <TableRow className="cursor-pointer hover:bg-accent/50" onClick={() => toggleCommit(c.sha)}>
                        <TableCell className="w-8">
                          <GitCommitHorizontal className="size-4 text-muted-foreground" />
                        </TableCell>
                        <TableCell>
                          <div className="font-medium">{c.subject}</div>
                          <div className="text-xs text-muted-foreground">
                            {c.author} · {timeAgo(c.date)}
                          </div>
                        </TableCell>
                        <TableCell className="text-right font-mono text-xs text-muted-foreground">
                          <Link
                            to={`#`}
                            onClick={(e) => {
                              e.preventDefault();
                              e.stopPropagation();
                              navigator.clipboard.writeText(c.sha);
                            }}
                            title={t("copyFullSha")}
                            className="hover:text-foreground"
                          >
                            {c.sha.slice(0, 8)}
                          </Link>
                        </TableCell>
                      </TableRow>
                      {expandedSha === c.sha && (
                        <TableRow key={c.sha + "-diff"}>
                          <TableCell colSpan={3} className="bg-muted/20 p-0">
                          <CommitDiffView diffs={commitDiff} />
                          </TableCell>
                        </TableRow>
                      )}
                    </Fragment>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </TabsContent>

        <TabsContent value="submit" className="mt-3">
          <ProjectSubmitPanel project={project} />
        </TabsContent>

        <TabsContent value="access" className="mt-3">
          <ProjectAccessPanel project={project} />
        </TabsContent>

        <TabsContent value="branches" className="mt-3">
          <BranchesPanel project={project} canEdit={canEdit && projectState === "ACTIVE"} />
        </TabsContent>

        <TabsContent value="tags" className="mt-3">
          <TagsPanel project={project} canEdit={canEdit && projectState === "ACTIVE"} />
        </TabsContent>

        <TabsContent value="manage" className="mt-3">
          <ManagePanel
            project={project}
            state={projectState}
            isAdmin={isAdmin}
            onStateChanged={setProjectState}
          />
        </TabsContent>

        <TabsContent value="webhooks" className="mt-3">
          <WebhooksPanel project={project} canEdit={canEdit} />
        </TabsContent>

        <TabsContent value="pipelines" className="mt-3">
          <ProjectPipelinesPanel project={project} canEdit={canEdit} />
        </TabsContent>
      </Tabs>

      <FileEditDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        project={project}
        branch={revision || branches[0]?.name || "master"}
        path={viewFile}
        initial={fileText ?? ""}
        onSaved={() => {
          setEditOpen(false);
          api.fileText(project, revision || branches[0]?.name || "HEAD", viewFile).then(setFileText).catch(() => {});
          api.commits(project, revision || undefined).then((d) => setCommits(d ?? [])).catch(() => {});
        }}
      />
    </div>
  );
}

function FileEditDialog({
  open,
  onOpenChange,
  project,
  branch,
  path,
  initial,
  onSaved,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  project: string;
  branch: string;
  path: string;
  initial: string;
  onSaved: () => void;
}) {
  const { t } = useTranslation("projectDetail");
  const [content, setContent] = useState(initial);
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) {
      setContent(initial);
      setMessage(t("defaultCommitMessage", { path }));
      setError("");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, initial, path]);

  const save = async () => {
    setBusy(true);
    setError("");
    try {
      await api.editFile(project, { branch, path, content, message: message.trim() || undefined });
      onSaved();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("editTitle", { path })}</DialogTitle>
          <DialogDescription>
            {t("editDescBefore")} <span className="font-mono">{branch}</span>
            {t("editDescAfter")}
          </DialogDescription>
        </DialogHeader>
        <Textarea
          className="h-72 font-mono text-xs"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          spellCheck={false}
        />
        <div className="flex flex-col gap-1">
          <Label htmlFor="commit-msg" className="text-xs">{t("commitMessageLabel")}</Label>
          <Input
            id="commit-msg"
            className="h-8 text-sm"
            value={message}
            onChange={(e) => setMessage(e.target.value)}
          />
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common:action.cancel")}
          </Button>
          <Button onClick={save} disabled={busy}>
            {busy ? t("committing") : t("commitChange")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function WatchButton({ project }: { project: string }) {
  const { user } = useAuth();
  const { t } = useTranslation("projectDetail");
  const [watched, setWatched] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!user) return;
    api
      .listWatched()
      .then((list) => setWatched((list ?? []).some((w) => w.project === project)))
      .catch(() => setWatched(false));
  }, [user, project]);

  if (!user) return null;

  const toggle = async () => {
    const next = !watched;
    setWatched(next);
    setBusy(true);
    try {
      if (next) await api.watchProject(project);
      else await api.unwatchProject(project);
    } catch {
      setWatched(!next);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Button size="sm" variant="outline" onClick={toggle} disabled={busy}>
      {watched ? <Bell className="size-4" /> : <BellOff className="size-4" />}
      {watched ? t("watching") : t("watch")}
    </Button>
  );
}

function CommitDiffView({ diffs }: { diffs: FileDiff[] | null }) {
  const { t } = useTranslation("projectDetail");
  if (diffs === null) {
    return <Skeleton className="m-4 h-24 w-auto" />;
  }
  if (diffs.length === 0) {
    return <p className="p-4 text-center text-sm text-muted-foreground">{t("noDiff")}</p>;
  }
  return (
    <div className="divide-y font-mono text-xs leading-5">
      {diffs.map((f) => (
        <div key={f.path}>
          <div className="flex items-center gap-2 bg-muted/40 px-4 py-1.5">
            <FileStatusBadge status={f.status} />
            <span className="font-medium">{f.path}</span>
            <span className="ml-auto">
              <span className="text-emerald-600">+{f.add_count}</span>{" "}
              <span className="text-red-600">−{f.del_count}</span>
            </span>
          </div>
          {f.binary ? (
            <p className="px-4 py-2 text-muted-foreground">{t("binaryFile")}</p>
          ) : (
            f.hunks.map((h, hi) => (
              <div key={hi}>
                <div className="bg-blue-50 px-4 py-0.5 text-[11px] text-blue-700 dark:bg-blue-950/40 dark:text-blue-300">
                  {h.header}
                </div>
                {h.lines.map((l, li) => (
                  <div
                    key={li}
                    className={
                      l.type === "add"
                        ? "bg-emerald-50 dark:bg-emerald-950/30"
                        : l.type === "del"
                          ? "bg-red-50 dark:bg-red-950/30"
                          : ""
                    }
                  >
                    <span className="inline-block w-10 select-none border-r px-1 text-right text-muted-foreground/60">
                      {l.old_no ?? ""}
                    </span>
                    <span className="inline-block w-10 select-none border-r px-1 text-right text-muted-foreground/60">
                      {l.new_no ?? ""}
                    </span>
                    <span
                      className="whitespace-pre-wrap break-all px-2"
                      dangerouslySetInnerHTML={{ __html: highlightLine(l.text, langForPath(f.path)) }}
                    />
                  </div>
                ))}
              </div>
            ))
          )}
        </div>
      ))}
    </div>
  );
}

function FileStatusBadge({ status }: { status: string }) {
  const cls =
    status === "A"
      ? "bg-emerald-100 text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-400"
      : status === "D"
        ? "bg-red-100 text-red-700 dark:bg-red-950/50 dark:text-red-400"
        : "bg-blue-100 text-blue-700 dark:bg-blue-950/50 dark:text-blue-400";
  return <span className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${cls}`}>{status}</span>;
}
