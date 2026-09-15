import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, type ChangeInfo, type LabelInfo } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Skeleton } from "@/components/ui/skeleton";
import { timeAgo } from "@/lib/utils";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function StatusBadge({ status }: { status: ChangeInfo["status"] }) {
  switch (status) {
    case "NEW":
      return <Badge variant="outline" className="border-emerald-600/40 text-emerald-700 dark:text-emerald-400">Open</Badge>;
    case "MERGED":
      return <Badge variant="success">Merged</Badge>;
    case "ABANDONED":
      return <Badge variant="muted" className="line-through">Abandoned</Badge>;
  }
}

export function VoteChips({ labels }: { labels?: Record<string, LabelInfo> }) {
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
          title={`${v.label} ${v.value > 0 ? "+" : ""}${v.value} by ${v.name} (PS ${v.patch_set})`}
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

export default function ChangesPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const q = searchParams.get("q") ?? "";
  const [changes, setChanges] = useState<ChangeInfo[] | null>(null);
  const [error, setError] = useState("");

  const statusFilter = useMemo(() => {
    const m = q.match(/status:(open|merged|abandoned)/);
    return m ? m[1] : "open";
  }, [q]);
  const freeText = useMemo(() => q.replace(/status:\w+/g, "").trim(), [q]);

  const load = () => {
    setChanges(null);
    api
      .listChanges(q)
      .then((data) => setChanges(data ?? []))
      .catch((err) => {
        setError((err as Error).message);
        setChanges([]);
      });
  };

  useEffect(load, [q]);

  const setTab = (tab: string) => {
    const next = new URLSearchParams();
    const parts: string[] = [];
    if (TAB_QUERY[tab]) parts.push(TAB_QUERY[tab]);
    if (freeText) parts.push(freeText);
    if (parts.length) next.set("q", parts.join(" "));
    setSearchParams(next);
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
        <h1 className="text-xl font-semibold">Changes</h1>
        <Tabs value={statusFilter} onValueChange={setTab} className="ml-auto">
          <TabsList>
            <TabsTrigger value="open">Open</TabsTrigger>
            <TabsTrigger value="merged">Merged</TabsTrigger>
            <TabsTrigger value="abandoned">Abandoned</TabsTrigger>
            <TabsTrigger value="all">All</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      <form onSubmit={onSearch} className="flex gap-2">
        <SearchBar defaultValue={freeText} />
      </form>

      {error && <p className="text-sm text-destructive">{error}</p>}

      {changes === null ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      ) : changes.length === 0 ? (
        <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
          No changes found.
          <div className="mt-2 text-sm">
            Push to <code className="rounded bg-muted px-1.5 py-0.5 text-foreground">refs/for/&lt;branch&gt;</code> to create a change.
          </div>
        </div>
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-16">Number</TableHead>
                <TableHead>Subject</TableHead>
                <TableHead className="w-44">Project / Branch</TableHead>
                <TableHead className="w-32">Owner</TableHead>
                <TableHead className="w-32">Updated</TableHead>
                <TableHead className="w-40">Votes</TableHead>
                <TableHead className="w-24">Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {changes.map((c) => (
                <TableRow key={c._number} className="cursor-pointer" onClick={() => (window.location.href = `/c/${c._number}`)}>
                  <TableCell className="font-mono text-xs text-muted-foreground">{c._number}</TableCell>
                  <TableCell className="max-w-md truncate font-medium">
                    <Link to={`/c/${c._number}`} className="hover:underline" onClick={(e) => e.stopPropagation()}>
                      {c.subject}
                    </Link>
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
    </div>
  );
}

function SearchBar({ defaultValue }: { defaultValue: string }) {
  return (
    <input
      name="q"
      defaultValue={defaultValue}
      placeholder="Filter: free text, project:foo…"
      className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs transition-[color,box-shadow] outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
    />
  );
}
