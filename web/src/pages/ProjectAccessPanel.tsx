import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Save, Trash2 } from "lucide-react";
import {
  api,
  type AccessRuleInfo,
  type GroupInfo,
  type ProjectAccess,
} from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";

const selectCls =
  "h-9 rounded-md border border-input bg-transparent px-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50";

const PERMISSIONS = [
  "read",
  "push",
  "submit",
  "abandon",
  "comment",
  "editTopicName",
  "addReviewer",
  "editAccess",
  "label-Code-Review",
  "label-Verified",
];

function isLabel(perm: string) {
  return perm.startsWith("label-");
}

export default function ProjectAccessPanel({ project }: { project: string }) {
  const { t } = useTranslation("projectAccess");
  const [data, setData] = useState<ProjectAccess | null>(null);
  const [groups, setGroups] = useState<GroupInfo[]>([]);
  const [rules, setRules] = useState<AccessRuleInfo[]>([]);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    setData(null);
    setDirty(false);
    setError("");
    api
      .projectAccess(project)
      .then((acc) => {
        setData(acc);
        setRules((acc.local ?? []).filter((r) => r.project !== "*"));
      })
      .catch((err) => setError((err as Error).message));
    api
      .listGroups()
      .then((map) =>
        setGroups(Object.values(map ?? {}).sort((a, b) => a.name.localeCompare(b.name))),
      )
      .catch(() => setGroups([]));
  }, [project]);

  const inherited = useMemo(
    () => (data?.local ?? []).filter((r) => r.project === "*"),
    [data],
  );
  const canEdit = !!data?.can_edit;

  const addRule = () => {
    const firstGroup = groups[0];
    setRules((rs) => [
      ...rs,
      {
        ref_pattern: "refs/heads/*",
        permission: "read",
        group_id: firstGroup ? Number(firstGroup.id) : 0,
        action: "ALLOW",
      },
    ]);
    setDirty(true);
  };

  const update = (i: number, patch: Partial<AccessRuleInfo>) => {
    setRules((rs) => rs.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
    setDirty(true);
  };

  const remove = (i: number) => {
    setRules((rs) => rs.filter((_, idx) => idx !== i));
    setDirty(true);
  };

  const save = async () => {
    setBusy(true);
    setError("");
    try {
      const acc = await api.setProjectAccess(project, rules);
      setData(acc);
      setRules((acc.local ?? []).filter((r) => r.project !== "*"));
      setDirty(false);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  if (!data) {
    return (
      <div className="flex flex-col gap-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  const groupName = (id: number) => groups.find((g) => Number(g.id) === id)?.name ?? `#${id}`;

  return (
    <div className="flex flex-col gap-6">
      {error && <p className="text-sm text-destructive">{error}</p>}

      <section className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold">{t("projectRules")}</h2>
          {canEdit && (
            <div className="ml-auto flex items-center gap-2">
              <Button size="sm" variant="outline" onClick={addRule}>
                <Plus className="size-4" />
                {t("addRule")}
              </Button>
              <Button size="sm" onClick={save} disabled={!dirty || busy}>
                <Save className="size-4" />
                {busy ? t("common:action.saving") : t("common:action.save")}
              </Button>
            </div>
          )}
        </div>

        {rules.length === 0 ? (
          <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
            {t("noRules")}
          </p>
        ) : (
          <div className="rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("refPattern")}</TableHead>
                  <TableHead>{t("permission")}</TableHead>
                  <TableHead>{t("group")}</TableHead>
                  <TableHead>{t("action")}</TableHead>
                  <TableHead>{t("range")}</TableHead>
                  {canEdit && <TableHead className="w-12" />}
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.map((r, i) => (
                  <TableRow key={i}>
                    <TableCell>
                      {canEdit ? (
                        <Input
                          className="h-8 font-mono text-xs"
                          value={r.ref_pattern}
                          onChange={(e) => update(i, { ref_pattern: e.target.value })}
                        />
                      ) : (
                        <span className="font-mono text-xs">{r.ref_pattern}</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {canEdit ? (
                        <select
                          className={selectCls + " h-8 text-xs"}
                          value={r.permission}
                          onChange={(e) =>
                            update(i, {
                              permission: e.target.value,
                              ...(isLabel(e.target.value) ? { min: -1, max: 1 } : {}),
                            })
                          }
                        >
                          {!PERMISSIONS.includes(r.permission) && (
                            <option value={r.permission}>{r.permission}</option>
                          )}
                          {PERMISSIONS.map((p) => (
                            <option key={p} value={p}>
                              {p}
                            </option>
                          ))}
                        </select>
                      ) : (
                        <span className="text-xs">{r.permission}</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {canEdit ? (
                        <select
                          className={selectCls + " h-8 text-xs"}
                          value={r.group_id}
                          onChange={(e) => update(i, { group_id: Number(e.target.value) })}
                        >
                          {!groups.some((g) => Number(g.id) === r.group_id) && (
                            <option value={r.group_id}>{r.group_name ?? `#${r.group_id}`}</option>
                          )}
                          {groups.map((g) => (
                            <option key={g.id} value={g.id}>
                              {g.name}
                            </option>
                          ))}
                        </select>
                      ) : (
                        <span className="text-xs">{r.group_name ?? groupName(r.group_id)}</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {canEdit ? (
                        <select
                          className={selectCls + " h-8 text-xs"}
                          value={r.action}
                          onChange={(e) =>
                            update(i, { action: e.target.value as AccessRuleInfo["action"] })
                          }
                        >
                          <option value="ALLOW">ALLOW</option>
                          <option value="DENY">DENY</option>
                          <option value="BLOCK">BLOCK</option>
                        </select>
                      ) : (
                        <Badge
                          variant={r.action === "ALLOW" ? "secondary" : "destructive"}
                          className="text-xs"
                        >
                          {r.action}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      {isLabel(r.permission) ? (
                        canEdit ? (
                          <span className="flex items-center gap-1">
                            <Input
                              type="number"
                              className="h-8 w-16 text-xs"
                              value={r.min ?? 0}
                              onChange={(e) => update(i, { min: Number(e.target.value) })}
                            />
                            …
                            <Input
                              type="number"
                              className="h-8 w-16 text-xs"
                              value={r.max ?? 0}
                              onChange={(e) => update(i, { max: Number(e.target.value) })}
                            />
                          </span>
                        ) : (
                          <span className="font-mono text-xs">
                            {r.min ?? 0}…{r.max ?? 0}
                          </span>
                        )
                      ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    {canEdit && (
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-8"
                          onClick={() => remove(i)}
                          aria-label={t("removeRule")}
                        >
                          <Trash2 className="size-4 text-destructive" />
                        </Button>
                      </TableCell>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </section>

      <section className="flex flex-col gap-2">
        <h2 className="text-sm font-semibold text-muted-foreground">
          {t("globalDefaults")}
        </h2>
        {inherited.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("none")}</p>
        ) : (
          <div className="rounded-lg border">
            <Table>
              <TableBody>
                {inherited.map((r, i) => (
                  <TableRow key={i}>
                    <TableCell className="font-mono text-xs">{r.ref_pattern}</TableCell>
                    <TableCell className="text-xs">{r.permission}</TableCell>
                    <TableCell className="text-xs">{r.group_name}</TableCell>
                    <TableCell>
                      <Badge
                        variant={r.action === "ALLOW" ? "secondary" : "destructive"}
                        className="text-xs"
                      >
                        {r.action}
                      </Badge>
                    </TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">
                      {isLabel(r.permission) ? `${r.min ?? 0}…${r.max ?? 0}` : ""}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </section>
    </div>
  );
}
