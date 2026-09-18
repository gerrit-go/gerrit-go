import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "lucide-react";
import { api, type AuditEntry } from "@/lib/api";
import { useAuth } from "@/auth";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const PAGE_SIZE = 50;

export default function AuditPage() {
  const { user } = useAuth();
  const { t } = useTranslation("audit");
  const [entries, setEntries] = useState<AuditEntry[] | null>(null);
  const [actions, setActions] = useState<string[]>([]);
  const [total, setTotal] = useState(0);
  const [start, setStart] = useState(0);
  const [action, setAction] = useState("");
  const [actor, setActor] = useState("");
  const [error, setError] = useState("");
  const [permModel, setPermModel] = useState("");

  useEffect(() => {
    api.getConfig().then((c) => setPermModel(c.permission_model ?? "")).catch(() => {});
  }, []);

  const load = useCallback(() => {
    if (!user?.admin) return;
    setEntries(null);
    api
      .listAudit({ n: PAGE_SIZE, start, action: action || undefined, actor: actor || undefined })
      .then((res) => {
        setEntries(res.entries || []);
        setTotal(res.total || 0);
        setActions(res.actions || []);
      })
      .catch((err) => {
        setError((err as Error).message);
        setEntries([]);
      });
  }, [user?.admin, start, action, actor]);

  useEffect(() => { load(); }, [load]);

  if (!user?.admin) {
    return <div className="p-6 text-muted-foreground">{t("adminOnly")}</div>;
  }

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4 p-4 sm:p-6">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold">{t("title")}</h1>
        {permModel && (
          <Badge variant={permModel === "default-deny-v1" ? "secondary" : "destructive"} className="text-xs">
            {permModel === "default-deny-v1" ? t("modelDefaultDeny") : t("modelLegacy")}
          </Badge>
        )}
        <span className="text-sm text-muted-foreground">{t("total", { count: total })}</span>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <select
            value={action}
            onChange={(e) => { setAction(e.target.value); setStart(0); }}
            className="rounded border bg-background px-2 py-1 text-sm"
          >
            <option value="">{t("allActions")}</option>
            {actions.map((a) => <option key={a} value={a}>{a}</option>)}
          </select>
          <input
            value={actor}
            onChange={(e) => { setActor(e.target.value); setStart(0); }}
            placeholder={t("actorPlaceholder")}
            className="w-36 rounded border bg-background px-2 py-1 text-sm"
          />
          <Button size="sm" variant="outline" onClick={load}>
            <RefreshCw className="size-3.5" />
          </Button>
        </div>
      </div>

      {error && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">{error}</div>}

      {entries === null ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}
        </div>
      ) : entries.length === 0 ? (
        <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">{t("empty")}</div>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-44">{t("col.time")}</TableHead>
                <TableHead className="w-32">{t("col.actor")}</TableHead>
                <TableHead className="w-40">{t("col.action")}</TableHead>
                <TableHead className="w-28">{t("col.target")}</TableHead>
                <TableHead>{t("col.detail")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {entries.map((e) => (
                <TableRow key={e.id}>
                  <TableCell className="whitespace-nowrap text-xs text-muted-foreground">{formatTime(e.created)}</TableCell>
                  <TableCell className="font-mono text-sm">{e.username || `#${e.account_id}`}</TableCell>
                  <TableCell><Badge variant="secondary" className="text-xs">{e.action}</Badge></TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {e.target_type ? `${e.target_type}${e.target_id ? " " + e.target_id : ""}` : "-"}
                  </TableCell>
                  <TableCell className="max-w-md break-words text-sm">{e.detail || "-"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {entries && entries.length > 0 && (
        <div className="flex items-center justify-end gap-2 text-sm">
          <Button size="sm" variant="outline" disabled={start === 0} onClick={() => setStart(Math.max(0, start - PAGE_SIZE))}>
            {t("prev")}
          </Button>
          <span className="text-muted-foreground">{start + 1}–{start + entries.length}</span>
          <Button size="sm" variant="outline" disabled={start + PAGE_SIZE >= total} onClick={() => setStart(start + PAGE_SIZE)}>
            {t("next")}
          </Button>
        </div>
      )}
    </div>
  );
}

function formatTime(iso: string) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}
