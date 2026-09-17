import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Bookmark, BookmarkPlus, Star, Trash2 } from "lucide-react";
import { api, type ChangeInfo, type LabelInfo, type SavedQuery } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn, timeAgo } from "@/lib/utils";
import { useAuth } from "@/auth";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function StatusBadge({ status }: { status: ChangeInfo["status"] }) {
  const { t } = useTranslation("changes");
  switch (status) {
    case "NEW":
      return <Badge variant="outline" className="border-emerald-600/40 text-emerald-700 dark:text-emerald-400">{t("common:status.new")}</Badge>;
    case "MERGED":
      return <Badge variant="success">{t("common:status.merged")}</Badge>;
    case "ABANDONED":
      return <Badge variant="muted" className="line-through">{t("common:status.abandoned")}</Badge>;
  }
}

export function VoteChips({ labels }: { labels?: Record<string, LabelInfo> }) {
  const { t } = useTranslation("changes");
  if (!labels) return null;
  const entries = Object.entries(labels).flatMap(([label, info]) =>
    info.all.map((v) => ({ label, ...v })),
  );
  if (entries.length === 0) return <span className="text-muted-foreground">—</span>;
  return (
    <div className="flex flex-wrap gap-1">
      {entries.map((v, i) => (
        <Badge
          key={`${v.label}-${v._account_id}-${i}`}
          variant="secondary"
          className={
            v.label === "Code-Review" && v.value === 2
              ? "bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300"
              : v.value < 0
                ? "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300"
                : undefined
          }
          title={t("voteTooltip", {
            label: v.label,
            value: `${v.value > 0 ? "+" : ""}${v.value}`,
            name: v.name,
            ps: v.patch_set,
          })}
        >
          {v.label === "Code-Review" ? "CR" : v.label} {v.value > 0 ? `+${v.value}` : v.value}
        </Badge>
      ))}
    </div>
  );
}

const TAB_QUERY: Record<string, string> = {
  open: "status:open",
  merged: "status:merged",
  abandoned: "status:abandoned",
  all: "",
};

const PAGE_SIZE = 25;

export default function ChangesPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useAuth();
  const { t } = useTranslation("changes");
  const q = searchParams.get("q") ?? "";
  const start = Number(searchParams.get("start") ?? "0") || 0;
  const [changes, setChanges] = useState<ChangeInfo[] | null>(null);
  const [total, setTotal] = useState(0);
  const [error, setError] = useState("");
  const [savedQueries, setSavedQueries] = useState<SavedQuery[]>([]);
  const [saveOpen, setSaveOpen] = useState(false);

  useEffect(() => {
    if (!user) {
      setSavedQueries([]);
      return;
    }
    api
      .listSavedQueries()
      .then((s) => setSavedQueries(s ?? []))
      .catch(() => setSavedQueries([]));
  }, [user]);

  const statusFilter = useMemo(() => {
    const m = q.match(/status:(open|merged|abandoned)/);
    return m ? m[1] : "open";
  }, [q]);
  const freeText = useMemo(() => q.replace(/status:\w+/g, "").trim(), [q]);

  const load = () => {
    setChanges(null);
    api
      .listChangesPaged(q, PAGE_SIZE, start)
      .then(({ items, total }) => {
        setChanges(items ?? []);
        setTotal(total);
      })
      .catch((err) => {
        setError((err as Error).message);
        setChanges([]);
      });
  };

  useEffect(load, [q, start]);

  const gotoStart = (next: number) => {
    const params = new URLSearchParams(searchParams);
    if (next > 0) params.set("start", String(next));
    else params.delete("start");
    setSearchParams(params);
  };

  const toggleStar = async (num: number) => {
    setChanges((xs) =>
      (xs ?? []).map((c) => (c._number === num ? { ...c, starred: !c.starred } : c)),
    );
    const target = changes?.find((c) => c._number === num);
    try {
      if (target?.starred) await api.unstar(num);
      else await api.star(num);
    } catch {
      setChanges((xs) =>
        (xs ?? []).map((c) => (c._number === num ? { ...c, starred: !c.starred } : c)),
      );
    }
  };

  const setTab = (tab: string) => {
    const next = new URLSearchParams();
    const parts: string[] = [];
    if (TAB_QUERY[tab]) parts.push(TAB_QUERY[tab]);
    if (freeText) parts.push(freeText);
    if (parts.length) next.set("q", parts.join(" "));
    setSearchParams(next);
  };

  const applySavedQuery = (sq: SavedQuery) => {
    const next = new URLSearchParams();
    next.set("q", sq.query);
    setSearchParams(next);
  };

  const removeSavedQuery = async (id: number) => {
    try {
      await api.deleteSavedQuery(id);
      setSavedQueries((s) => s.filter((x) => x.id !== id));
    } catch {
      /* ignore */
    }
  };

  const onSearch = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const input = new FormData(e.currentTarget).get("q") as string;
    const next = new URLSearchParams();
    const parts: string[] = [];
    if (TAB_QUERY[statusFilter]) parts.push(TAB_QUERY[statusFilter]);
    if (input.trim()) parts.push(input.trim());
    if (parts.length) next.set("q", parts.join(" "));
    setSearchParams(next);
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold">{t("common:nav.changes")}</h1>
        <Tabs value={statusFilter} onValueChange={setTab} className="ml-auto">
          <TabsList>
            <TabsTrigger value="open">{t("common:status.new")}</TabsTrigger>
            <TabsTrigger value="merged">{t("common:status.merged")}</TabsTrigger>
            <TabsTrigger value="abandoned">{t("common:status.abandoned")}</TabsTrigger>
            <TabsTrigger value="all">{t("common:common.all")}</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      <form onSubmit={onSearch} className="flex gap-2">
        <SearchBar defaultValue={freeText} />
        {user && (
          <>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button type="button" size="sm" variant="outline">
                  <Bookmark className="size-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-64">
                {savedQueries.length === 0 ? (
                  <div className="px-2 py-1.5 text-xs text-muted-foreground">{t("savedQueries.empty")}</div>
                ) : (
                  savedQueries.map((sq) => (
                    <DropdownMenuItem key={sq.id} onSelect={() => applySavedQuery(sq)} className="flex items-center gap-2">
                      <span className="min-w-0 flex-1 truncate">
                        {sq.name}
                        {sq.shared && sq.owner && <span className="text-muted-foreground"> · {sq.owner}</span>}
                      </span>
                      <button
                        className="shrink-0 text-muted-foreground hover:text-destructive"
                        onClick={(e) => {
                          e.stopPropagation();
                          removeSavedQuery(sq.id);
                        }}
                      >
                        <Trash2 className="size-3.5" />
                      </button>
                    </DropdownMenuItem>
                  ))
                )}
              </DropdownMenuContent>
            </DropdownMenu>
            <Button type="button" size="sm" variant="outline" onClick={() => setSaveOpen(true)} title={t("savedQueries.save")}>
              <BookmarkPlus className="size-4" />
            </Button>
          </>
        )}
      </form>
      <SaveQueryDialog
        open={saveOpen}
        onOpenChange={setSaveOpen}
        currentQuery={q}
        onSaved={(sq) => setSavedQueries((s) => [...s, sq])}
      />

      {error && <p className="text-sm text-destructive">{error}</p>}

      {changes === null ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      ) : changes.length === 0 ? (
        <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
          {t("empty")}
          <div className="mt-2 text-sm">
            {t("emptyHintBefore")}<code className="rounded bg-muted px-1.5 py-0.5 text-foreground">refs/for/&lt;branch&gt;</code>{t("emptyHintAfter")}
          </div>
        </div>
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                {user && <TableHead className="w-8" />}
                <TableHead className="w-16">{t("number")}</TableHead>
                <TableHead>{t("common:common.subject")}</TableHead>
                <TableHead className="w-44">{t("common:common.project")} / {t("common:common.branch")}</TableHead>
                <TableHead className="w-32">{t("common:common.owner")}</TableHead>
                <TableHead className="w-32">{t("common:common.updated")}</TableHead>
                <TableHead className="w-40">{t("votes")}</TableHead>
                <TableHead className="w-24">{t("status")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {changes.map((c) => (
                <TableRow key={c._number} className="cursor-pointer" onClick={() => (window.location.href = `/c/${c._number}`)}>
                  {user && (
                    <TableCell className="pr-0">
                      <button
                        className="text-muted-foreground transition-colors hover:text-foreground"
                        title={c.starred ? t("unstar") : t("star")}
                        aria-label={c.starred ? t("unstar") : t("star")}
                        onClick={(e) => {
                          e.stopPropagation();
                          toggleStar(c._number);
                        }}
                      >
                        <Star className={cn("size-4", c.starred && "fill-amber-400 text-amber-400")} />
                      </button>
                    </TableCell>
                  )}
                  <TableCell className="font-mono text-xs text-muted-foreground">{c._number}</TableCell>
                  <TableCell className="max-w-md font-medium">
                    <div className="flex items-center gap-2">
                      <Link to={`/c/${c._number}`} className="truncate hover:underline" onClick={(e) => e.stopPropagation()}>
                        {c.subject}
                      </Link>
                      {c.work_in_progress && (
                        <Badge variant="muted" className="shrink-0 text-[10px]">WIP</Badge>
                      )}
                      {c.topic && (
                        <Badge variant="outline" className="shrink-0 text-[10px]">#{c.topic}</Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="text-sm">
                    <Link
                      to={`/projects/${encodeURIComponent(c.project)}`}
                      className="text-muted-foreground hover:underline"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {c.project}
                    </Link>
                    <span className="text-muted-foreground"> : </span>
                    <span className="font-mono text-xs">{c.branch}</span>
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">{c.owner.name}</TableCell>
                  <TableCell className="text-sm text-muted-foreground">{timeAgo(c.updated)}</TableCell>
                  <TableCell><VoteChips labels={c.labels} /></TableCell>
                  <TableCell><StatusBadge status={c.status} /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {changes !== null && total > PAGE_SIZE && (
        <div className="flex items-center justify-between text-sm text-muted-foreground">
          <span>
            {t("range", {
              from: start + 1,
              to: Math.min(start + PAGE_SIZE, total),
              total,
            })}
          </span>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={start === 0}
              onClick={() => gotoStart(start - PAGE_SIZE)}
            >
              {t("prev")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={start + PAGE_SIZE >= total}
              onClick={() => gotoStart(start + PAGE_SIZE)}
            >
              {t("next")}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

function SearchBar({ defaultValue }: { defaultValue: string }) {
  const { t } = useTranslation("changes");
  return (
    <input
      name="q"
      defaultValue={defaultValue}
      placeholder={t("filterPlaceholder")}
      className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs transition-[color,box-shadow] outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
    />
  );
}

function SaveQueryDialog({
  open,
  onOpenChange,
  currentQuery,
  onSaved,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  currentQuery: string;
  onSaved: (sq: SavedQuery) => void;
}) {
  const { t } = useTranslation("changes");
  const [name, setName] = useState("");
  const [shared, setShared] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (open) {
      setName("");
      setShared(false);
      setErr("");
    }
  }, [open]);

  const save = async () => {
    if (!name.trim() || !currentQuery.trim()) return;
    setBusy(true);
    setErr("");
    try {
      const sq = await api.createSavedQuery(name.trim(), currentQuery, shared);
      onSaved(sq);
      onOpenChange(false);
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("savedQueries.saveTitle")}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <code className="rounded bg-muted/40 px-2 py-1 font-mono text-xs">{currentQuery}</code>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("savedQueries.namePlaceholder")} />
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={shared} onChange={(e) => setShared(e.target.checked)} />
            {t("savedQueries.share")}
          </label>
          {err && <p className="text-xs text-destructive">{err}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            {t("common:cancel")}
          </Button>
          <Button onClick={save} disabled={busy || !name.trim() || !currentQuery.trim()}>
            {t("savedQueries.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
