import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  ArrowRightLeft,
  ChevronRight,
  ShieldCheck,
  Tags,
  X,
} from "lucide-react";
import {
  api,
  type AccountInfo,
  type GroupInfo,
  type NamespaceNode,
  type ProjectInfo,
  type RoleBinding,
} from "@/lib/api";
import { useAuth } from "@/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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

export default function OrgPage() {
  const { user } = useAuth();
  const { t } = useTranslation("organization");
  const [namespaces, setNamespaces] = useState<NamespaceNode[]>([]);
  const [projects, setProjects] = useState<ProjectInfo[] | null>(null);
  const [allLabels, setAllLabels] = useState<Record<string, string[]>>({});
  const [bindings, setBindings] = useState<RoleBinding[]>([]);
  const [accounts, setAccounts] = useState<AccountInfo[]>([]);
  const [groups, setGroups] = useState<GroupInfo[]>([]);
  const [selectedNs, setSelectedNs] = useState("");
  const [checked, setChecked] = useState<Set<string>>(new Set());
  const [error, setError] = useState("");
  const [labelOpen, setLabelOpen] = useState(false);
  const [moveOpen, setMoveOpen] = useState(false);

  const isAdmin = !!user?.admin;

  useEffect(() => {
    if (!isAdmin) return;
    api.listNamespaces().then(setNamespaces).catch(() => {});
    api.listLabels().then(setAllLabels).catch(() => {});
    api.listAllRoleBindings().then(setBindings).catch(() => {});
    api.listAccounts().then(setAccounts).catch(() => {});
    api.listGroups()
      .then((g) => setGroups(Object.values(g)))
      .catch(() => {});
  }, [isAdmin]);

  useEffect(() => {
    if (!isAdmin) return;
    setChecked(new Set());
    api
      .listProjects(selectedNs ? { namespace: selectedNs } : undefined)
      .then((map) => setProjects(Object.values(map ?? {}).sort((a, b) => a.name.localeCompare(b.name))))
      .catch((err) => {
        setError((err as Error).message);
        setProjects([]);
      });
  }, [selectedNs, isAdmin]);

  const toggleCheck = (name: string) => {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const checkedNames = useMemo(() => Array.from(checked), [checked]);

  const visibleBindings = useMemo(() => {
    if (!selectedNs) return bindings;
    return bindings.filter(
      (b) =>
        b.scope === "*" ||
        b.scope === selectedNs ||
        b.scope === `${selectedNs}/*` ||
        b.scope.startsWith(`${selectedNs}/`),
    );
  }, [bindings, selectedNs]);

  const subjectLabel = (b: RoleBinding) => {
    if (b.subject_type === "group") {
      const g = groups.find((x) => x.id === String(b.subject_id));
      if (g) return g.name;
    } else {
      const a = accounts.find((x) => x._account_id === b.subject_id);
      if (a) return a.username;
    }
    return `#${b.subject_id}`;
  };

  if (!isAdmin) {
    return <div className="p-8 text-center text-muted-foreground">{t("adminOnly")}</div>;
  }

  return (
    <div className="flex gap-4">
      <aside className="hidden w-60 shrink-0 flex-col gap-4 md:flex">
        <div className="rounded-lg border p-3">
          <h3 className="mb-2 text-sm font-medium text-muted-foreground">{t("namespaces")}</h3>
          <div className="flex flex-col gap-0.5">
            <button
              onClick={() => setSelectedNs("")}
              className={`rounded px-2 py-1 text-left text-sm hover:bg-accent ${!selectedNs ? "bg-accent font-medium" : ""}`}
            >
              {t("allNamespaces")}
            </button>
            {namespaces.map((ns) => (
              <NsNode key={ns.path} node={ns} depth={0} selected={selectedNs} onSelect={setSelectedNs} />
            ))}
          </div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col gap-4">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="text-xl font-semibold">{t("title")}</h1>
          {selectedNs && (
            <Badge variant="secondary" className="gap-1">
              {selectedNs}
              <button onClick={() => setSelectedNs("")}><X className="size-3" /></button>
            </Badge>
          )}
          <span className="text-sm text-muted-foreground">
            {t("selected", { count: checkedNames.length })}
          </span>
          <div className="ml-auto flex gap-2">
            <Button size="sm" variant="outline" disabled={checkedNames.length === 0} onClick={() => setLabelOpen(true)}>
              <Tags className="size-4" />
              {t("bulkLabel")}
            </Button>
            <Button size="sm" variant="outline" disabled={checkedNames.length !== 1} onClick={() => setMoveOpen(true)}>
              <ArrowRightLeft className="size-4" />
              {t("moveProject")}
            </Button>
          </div>
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}

        {projects === null ? (
          <div className="flex flex-col gap-2">
            {Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}
          </div>
        ) : projects.length === 0 ? (
          <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
            {t("empty")}
          </div>
        ) : (
          <div className="rounded-lg border">
            <div className="flex items-center gap-3 border-b bg-muted/40 px-3 py-2 text-xs font-medium text-muted-foreground">
              <span className="w-4" />
              <span className="flex-1">{t("colProject")}</span>
              <span className="hidden w-64 lg:block">{t("colLabels")}</span>
              <span className="hidden w-40 md:block">{t("colNamespace")}</span>
            </div>
            {projects.slice(0, 300).map((p) => (
              <div key={p.name} className="flex items-center gap-3 border-b px-3 py-2 text-sm last:border-b-0">
                <input
                  type="checkbox"
                  className="size-4"
                  checked={checked.has(p.name)}
                  onChange={() => toggleCheck(p.name)}
                  aria-label={p.name}
                />
                <Link to={`/projects/${encodeURIComponent(p.name)}`} className="min-w-0 flex-1 truncate hover:underline">
                  {p.name}
                </Link>
                <div className="hidden w-64 flex-wrap gap-1 lg:flex">
                  {Object.entries(p.labels ?? {}).map(([k, v]) => (
                    <Badge key={k} variant="outline" className="text-xs">
                      {k}{v ? `:${v}` : ""}
                    </Badge>
                  ))}
                </div>
                <span className="hidden w-40 truncate text-xs text-muted-foreground md:block">
                  {p.namespace || "-"}
                </span>
              </div>
            ))}
            {projects.length > 300 && (
              <p className="px-3 py-2 text-xs text-muted-foreground">
                {t("truncated", { shown: 300, total: projects.length })}
              </p>
            )}
          </div>
        )}

        <Card className="min-w-0 gap-3 py-4">
          <CardHeader className="px-4">
            <CardTitle className="flex items-center gap-2 text-base">
              <ShieldCheck className="size-4 text-muted-foreground" />
              {t("bindingsTitle")}
            </CardTitle>
            <CardDescription>{t("bindingsDescription")}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2 px-4">
            {visibleBindings.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("noBindings")}</p>
            ) : (
              visibleBindings.map((b) => (
                <div key={b.id} className="flex flex-wrap items-center gap-2 text-sm">
                  <Link to="/roles" className="hover:underline">
                    <Badge variant="secondary" className="text-xs">{b.role_name}</Badge>
                  </Link>
                  <span>{subjectLabel(b)}</span>
                  <span className="text-muted-foreground">@ {b.scope}</span>
                </div>
              ))
            )}
            <p className="pt-1 text-xs text-muted-foreground">
              {t("manageBindings")}{" "}
              <Link to="/roles" className="text-foreground underline hover:no-underline">{t("rolesPage")}</Link>
            </p>
          </CardContent>
        </Card>
      </div>

      <BulkLabelDialog
        open={labelOpen}
        onOpenChange={setLabelOpen}
        projects={checkedNames}
        allLabels={allLabels}
        onError={setError}
        onDone={() => {
          setLabelOpen(false);
          api.listProjects(selectedNs ? { namespace: selectedNs } : undefined)
            .then((m) => setProjects(Object.values(m ?? {}).sort((a, b) => a.name.localeCompare(b.name))))
            .catch(() => {});
          api.listLabels().then(setAllLabels).catch(() => {});
        }}
      />
      <MoveDialog
        open={moveOpen}
        onOpenChange={setMoveOpen}
        project={checkedNames.length === 1 ? checkedNames[0] : ""}
        namespaces={namespaces}
        onError={setError}
        onDone={(newName) => {
          setMoveOpen(false);
          setSelectedNs(newName.includes("/") ? newName.slice(0, newName.lastIndexOf("/")) : "");
          setChecked(new Set());
        }}
      />
    </div>
  );
}

function NsNode({ node, depth, selected, onSelect }: {
  node: NamespaceNode;
  depth: number;
  selected: string;
  onSelect: (path: string) => void;
}) {
  const [expanded, setExpanded] = useState(depth < 1);
  const hasChildren = node.children && node.children.length > 0;
  const isSelected = selected === node.path;
  return (
    <div>
      <div className="flex items-center">
        {hasChildren ? (
          <button onClick={() => setExpanded(!expanded)} className="p-0.5">
            <ChevronRight className={`size-3.5 transition-transform ${expanded ? "rotate-90" : ""}`} />
          </button>
        ) : (
          <span className="w-[18px]" />
        )}
        <button
          onClick={() => onSelect(isSelected ? "" : node.path)}
          className={`flex-1 rounded px-1.5 py-1 text-left text-sm hover:bg-accent ${isSelected ? "bg-accent font-medium" : ""}`}
        >
          {node.name}
          {node.count > 0 && <span className="ml-1 text-xs text-muted-foreground">({node.count})</span>}
        </button>
      </div>
      {expanded && hasChildren && (
        <div className="ml-3">
          {node.children!.map((child) => (
            <NsNode key={child.path} node={child} depth={depth + 1} selected={selected} onSelect={onSelect} />
          ))}
        </div>
      )}
    </div>
  );
}

function flattenNamespaces(nodes: NamespaceNode[], out: string[] = []): string[] {
  for (const n of nodes) {
    out.push(n.path);
    if (n.children) flattenNamespaces(n.children, out);
  }
  return out;
}

function BulkLabelDialog({ open, onOpenChange, projects, allLabels, onDone, onError }: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  projects: string[];
  allLabels: Record<string, string[]>;
  onDone: () => void;
  onError: (msg: string) => void;
}) {
  const { t } = useTranslation("organization");
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [removing, setRemoving] = useState(false);
  const [busy, setBusy] = useState(false);

  const apply = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    onError("");
    try {
      const labels = removing ? { [key.trim()]: null } : { [key.trim()]: value };
      await api.bulkSetLabels(projects, labels);
      setKey("");
      setValue("");
      setRemoving(false);
      onDone();
    } catch (err) {
      onError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("bulkLabel")}</DialogTitle>
          <DialogDescription>{t("bulkLabelDesc", { count: projects.length })}</DialogDescription>
        </DialogHeader>
        <form onSubmit={apply} className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="bl-key">{t("labelKey")}</Label>
            <Input
              id="bl-key"
              list="bl-key-options"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder="os"
              required
            />
            <datalist id="bl-key-options">
              {Object.keys(allLabels).map((k) => <option key={k} value={k} />)}
            </datalist>
          </div>
          {!removing && (
            <div className="flex flex-col gap-2">
              <Label htmlFor="bl-value">{t("labelValue")}</Label>
              <Input
                id="bl-value"
                list="bl-value-options"
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder="linux"
                disabled={!key.trim()}
              />
              <datalist id="bl-value-options">
                {(allLabels[key.trim()] ?? []).map((v) => <option key={v} value={v} />)}
              </datalist>
            </div>
          )}
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              className="size-4"
              checked={removing}
              onChange={(e) => setRemoving(e.target.checked)}
            />
            {t("removeLabel")}
          </label>
          <div className="flex flex-wrap gap-1">
            {projects.map((p) => (
              <Badge key={p} variant="outline" className="text-xs">{p}</Badge>
            ))}
          </div>
          <DialogFooter>
            <Button type="submit" disabled={busy || !key.trim()}>
              {busy ? t("common:action.saving") : t("apply")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function MoveDialog({ open, onOpenChange, project, namespaces, onDone, onError }: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  project: string;
  namespaces: NamespaceNode[];
  onDone: (newName: string) => void;
  onError: (msg: string) => void;
}) {
  const { t } = useTranslation("organization");
  const currentNs = project.includes("/") ? project.slice(0, project.lastIndexOf("/")) : "";
  const baseName = project.includes("/") ? project.slice(project.lastIndexOf("/") + 1) : project;
  const [targetNs, setTargetNs] = useState(currentNs);
  const [customNs, setCustomNs] = useState("");
  const [name, setName] = useState(baseName);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (open) {
      setTargetNs(currentNs);
      setCustomNs("");
      setName(baseName);
    }
  }, [open, currentNs, baseName]);

  const nsOptions = flattenNamespaces(namespaces);
  const chosenNs = targetNs === "__custom" ? customNs.trim().replace(/\/+$/, "") : targetNs;
  const newName = chosenNs ? `${chosenNs}/${name.trim()}` : name.trim();
  const valid = !!project && !!name.trim() && !name.trim().includes("..") && newName !== project;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    onError("");
    try {
      const res = await api.renameProject(project, newName);
      onDone(res.name);
    } catch (err) {
      onError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("moveProject")}</DialogTitle>
          <DialogDescription>{t("moveDesc", { project })}</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="mv-ns">{t("targetNamespace")}</Label>
            <select
              id="mv-ns"
              value={targetNs}
              onChange={(e) => setTargetNs(e.target.value)}
              className="rounded border bg-background px-2 py-1.5 text-sm"
            >
              <option value="">{t("rootNamespace")}</option>
              {nsOptions.map((p) => <option key={p} value={p}>{p}</option>)}
              <option value="__custom">{t("customNamespace")}</option>
            </select>
          </div>
          {targetNs === "__custom" && (
            <div className="flex flex-col gap-2">
              <Label htmlFor="mv-custom">{t("customNamespace")}</Label>
              <Input
                id="mv-custom"
                value={customNs}
                onChange={(e) => setCustomNs(e.target.value)}
                placeholder="rk/Linux/bsp"
              />
            </div>
          )}
          <div className="flex flex-col gap-2">
            <Label htmlFor="mv-name">{t("projectName")}</Label>
            <Input id="mv-name" value={name} onChange={(e) => setName(e.target.value)} required />
          </div>
          <div className="rounded-md bg-muted px-3 py-2 font-mono text-xs">
            {project} <span className="text-muted-foreground">→</span> {newName || "?"}
          </div>
          <p className="text-sm text-destructive">{t("moveWarning")}</p>
          <DialogFooter>
            <Button type="submit" disabled={busy || !valid}>
              {busy ? t("common:action.saving") : t("confirmMove")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
