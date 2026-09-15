import { useEffect, useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { ChevronRight, File, Folder, GitCommitHorizontal, Terminal } from "lucide-react";
import { api, type BranchInfo, type CommitInfo, type FileEntry } from "@/lib/api";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Skeleton } from "@/components/ui/skeleton";
import { timeAgo } from "@/lib/utils";
import ProjectAccessPanel from "@/pages/ProjectAccessPanel";
import ProjectSubmitPanel from "@/pages/ProjectSubmitPanel";
import {
  Table,
  TableBody,
  TableCell,
  TableRow,
} from "@/components/ui/table";

export default function ProjectDetailPage() {
  const params = useParams();
  const project = decodeURIComponent(params["*"] ?? "");
  const [searchParams, setSearchParams] = useSearchParams();
  const revision = searchParams.get("revision") ?? "";
  const dirPath = searchParams.get("path") ?? "";
  const tab = searchParams.get("tab") ?? "files";
  const viewFile = searchParams.get("file") ?? "";

  const [branches, setBranches] = useState<BranchInfo[]>([]);
  const [entries, setEntries] = useState<FileEntry[] | null>(null);
  const [commits, setCommits] = useState<CommitInfo[] | null>(null);
  const [fileText, setFileText] = useState<string | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api.branches(project).then((b) => setBranches(b ?? [])).catch(() => setBranches([]));
  }, [project]);

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
      .catch(() => setFileText("// failed to load file"));
  }, [project, viewFile, revision, rev]);

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

  if (!project) return null;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <div>
          <h1 className="text-xl font-semibold">{project}</h1>
          <div className="mt-1 flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
            <Terminal className="size-3" />
            git clone {window.location.origin}/git/{project}.git
          </div>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <select
            value={revision || branches[0]?.name || ""}
            onChange={(e) => setParam("revision", e.target.value === (branches[0]?.name ?? "") ? "" : e.target.value)}
            className="h-9 rounded-md border border-input bg-transparent px-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            {branches.length === 0 && <option value="">(empty repository)</option>}
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
          <TabsTrigger value="files">Files</TabsTrigger>
          <TabsTrigger value="commits">Commits</TabsTrigger>
          <TabsTrigger value="submit">Submit</TabsTrigger>
          <TabsTrigger value="access">Access</TabsTrigger>
        </TabsList>

        <TabsContent value="files" className="mt-3">
          {viewFile ? (
            <div className="overflow-hidden rounded-lg border">
              <div className="flex items-center gap-2 border-b bg-muted/40 px-4 py-2">
                <button
                  className="text-sm text-muted-foreground hover:text-foreground"
                  onClick={() => setParam("file", "")}
                >
                  ← back to files
                </button>
                <span className="ml-auto font-mono text-sm">{viewFile}</span>
              </div>
              {fileText === null ? (
                <Skeleton className="m-4 h-64 w-auto" />
              ) : (
                <pre className="max-h-[70vh] overflow-auto bg-zinc-950 p-4 text-xs leading-5 text-zinc-100">
                  <code>{fileText}</code>
                </pre>
              )}
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
                  This branch is empty. Push some commits to get started.
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
            <p className="p-8 text-center text-sm text-muted-foreground">No commits yet.</p>
          ) : (
            <div className="rounded-lg border">
              <Table>
                <TableBody>
                  {commits.map((c) => (
                    <TableRow key={c.sha}>
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
                            navigator.clipboard.writeText(c.sha);
                          }}
                          title="Copy full SHA"
                          className="hover:text-foreground"
                        >
                          {c.sha.slice(0, 8)}
                        </Link>
                      </TableCell>
                    </TableRow>
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
      </Tabs>
    </div>
  );
}
