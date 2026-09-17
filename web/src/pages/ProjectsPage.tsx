import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ChevronRight, FolderGit2, FolderOpen, Plus, Tag, Terminal, X } from "lucide-react";
import { api, type ProjectInfo, type NamespaceNode } from "@/lib/api";
import { useAuth } from "@/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";

export default function ProjectsPage() {
  const { user } = useAuth();
  const { t } = useTranslation("projects");
  const [projects, setProjects] = useState<ProjectInfo[] | null>(null);
  const [namespaces, setNamespaces] = useState<NamespaceNode[]>([]);
  const [allLabels, setAllLabels] = useState<Record<string, string[]>>({});
  const [selectedNs, setSelectedNs] = useState<string>("");
  const [selectedLabel, setSelectedLabel] = useState<string>("");
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [copyFrom, setCopyFrom] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () => {
    const params: { namespace?: string; label?: string } = {};
    if (selectedNs) params.namespace = selectedNs;
    if (selectedLabel) params.label = selectedLabel;
    api
      .listProjects(params)
      .then((map) => setProjects(Object.values(map ?? {}).sort((a, b) => a.name.localeCompare(b.name))))
      .catch((err) => {
        setError((err as Error).message);
        setProjects([]);
      });
  };

  useEffect(() => {
    api.listNamespaces().then(setNamespaces).catch(() => {});
    api.listLabels().then(setAllLabels).catch(() => {});
  }, []);

  useEffect(() => {
    load();
  }, [selectedNs, selectedLabel]);

  const onCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.createProject(name.trim(), description.trim(), copyFrom.trim() || undefined);
      setOpen(false);
      setName("");
      setDescription("");
      setCopyFrom("");
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  // Group projects by their immediate namespace for display.
  const grouped = useMemo(() => {
    if (!projects) return null;
    const groups = new Map<string, ProjectInfo[]>();
    for (const p of projects) {
      const ns = p.namespace || "";
      if (!groups.has(ns)) groups.set(ns, []);
      groups.get(ns)!.push(p);
    }
    return Array.from(groups.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [projects]);

  return (
    <div className="flex gap-4">
      {/* Left sidebar: namespace tree + label filter */}
      <aside className="hidden w-56 shrink-0 flex-col gap-4 md:flex">
        {/* Namespace tree */}
        <div className="rounded-lg border p-3">
          <h3 className="mb-2 text-sm font-medium text-muted-foreground">{t("namespaces")}</h3>
          <div className="flex flex-col gap-0.5">
            <button
              onClick={() => setSelectedNs("")}
              className={`rounded px-2 py-1 text-left text-sm hover:bg-accent ${!selectedNs ? "bg-accent font-medium" : ""}`}
            >
              {t("allProjects")}
            </button>
            {namespaces.map((ns) => (
              <NsNode key={ns.path} node={ns} depth={0} selected={selectedNs} onSelect={setSelectedNs} />
            ))}
          </div>
        </div>

        {/* Label filter */}
        {Object.keys(allLabels).length > 0 && (
          <div className="rounded-lg border p-3">
            <h3 className="mb-2 text-sm font-medium text-muted-foreground">{t("labels")}</h3>
            <div className="flex flex-col gap-1">
              {Object.entries(allLabels).map(([key, values]) => (
                <div key={key}>
                  <span className="text-xs text-muted-foreground">{key}</span>
                  <div className="mt-0.5 flex flex-wrap gap-1">
                    {values.map((v) => {
                      const filter = v ? `${key}:${v}` : key;
                      const active = selectedLabel === filter;
                      return (
                        <Badge
                          key={v || "(any)"}
                          variant={active ? "default" : "outline"}
                          className="cursor-pointer text-xs"
                          onClick={() => setSelectedLabel(active ? "" : filter)}
                        >
                          {v || "(any)"}
                        </Badge>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </aside>

      {/* Main content */}
      <div className="flex min-w-0 flex-1 flex-col gap-4">
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-semibold">{t("common:nav.projects")}</h1>
          {(selectedNs || selectedLabel) && (
            <div className="flex items-center gap-1.5">
              {selectedNs && (
                <Badge variant="secondary" className="gap-1">
                  {selectedNs}
                  <button onClick={() => setSelectedNs("")}><X className="size-3" /></button>
                </Badge>
              )}
              {selectedLabel && (
                <Badge variant="secondary" className="gap-1">
                  <Tag className="size-3" />{selectedLabel}
                  <button onClick={() => setSelectedLabel("")}><X className="size-3" /></button>
                </Badge>
              )}
            </div>
          )}
          {user && (
            <Dialog open={open} onOpenChange={setOpen}>
              <DialogTrigger asChild>
                <Button size="sm" className="ml-auto">
                  <Plus className="size-4" />
                  {t("newProject")}
                </Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>{t("createProject")}</DialogTitle>
                  <DialogDescription>{t("createDescription")}</DialogDescription>
                </DialogHeader>
                <form onSubmit={onCreate} className="flex flex-col gap-4">
                  <div className="flex flex-col gap-2">
                    <Label htmlFor="proj-name">{t("common:common.name")}</Label>
                    <Input
                      id="proj-name"
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      placeholder="my/project"
                      required
                    />
                  </div>
                  <div className="flex flex-col gap-2">
                    <Label htmlFor="proj-desc">{t("common:common.description")}</Label>
                    <Textarea
                      id="proj-desc"
                      value={description}
                      onChange={(e) => setDescription(e.target.value)}
                      rows={3}
                    />
                  </div>
                  <div className="flex flex-col gap-2">
                    <Label htmlFor="proj-copy">{t("copyFrom")}</Label>
                    <Input
                      id="proj-copy"
                      value={copyFrom}
                      onChange={(e) => setCopyFrom(e.target.value)}
                      placeholder={t("copyFromPlaceholder")}
                    />
                  </div>
                  {error && <p className="text-sm text-destructive">{error}</p>}
                  <DialogFooter>
                    <Button type="submit" disabled={busy}>
                      {busy ? t("common:action.creating") : t("common:action.create")}
                    </Button>
                  </DialogFooter>
                </form>
              </DialogContent>
            </Dialog>
          )}
        </div>

        {projects === null ? (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-28 w-full" />
            ))}
          </div>
        ) : projects.length === 0 ? (
          <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
            {t("empty")}
          </div>
        ) : (
          <div className="flex flex-col gap-4">
            {grouped?.map(([ns, projs]) => (
              <div key={ns || "(root)"}>
                {ns && (
                  <h2 className="mb-2 flex items-center gap-1.5 text-sm font-medium text-muted-foreground">
                    <FolderOpen className="size-4" />
                    {ns}
                    <span className="text-xs">({projs.length})</span>
                  </h2>
                )}
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                  {projs.map((p) => (
                    <Card key={p.name} className="min-w-0 gap-3 py-4 transition-shadow hover:shadow-md">
                      <CardHeader className="px-4">
                        <CardTitle className="flex min-w-0 items-center gap-2 text-base">
                          <FolderGit2 className="size-4 shrink-0 text-muted-foreground" />
                          <Link to={`/projects/${encodeURIComponent(p.name)}`} className="min-w-0 break-all hover:underline">
                            {p.name}
                          </Link>
                        </CardTitle>
                        <CardDescription className="line-clamp-2">
                          {p.description || t("noDescription")}
                        </CardDescription>
                      </CardHeader>
                      <CardContent className="flex flex-col gap-2 px-4">
                        <div className="flex items-center gap-1.5 rounded-md bg-muted px-2 py-1 font-mono text-xs text-muted-foreground">
                          <Terminal className="size-3 shrink-0" />
                          <span className="min-w-0 truncate">git clone {window.location.origin}/git/{p.name}.git</span>
                        </div>
                        {p.labels && Object.keys(p.labels).length > 0 && (
                          <div className="flex flex-wrap gap-1">
                            {Object.entries(p.labels).map(([k, v]) => (
                              <Badge key={k} variant="outline" className="text-xs">
                                {k}{v ? `:${v}` : ""}
                              </Badge>
                            ))}
                          </div>
                        )}
                      </CardContent>
                    </Card>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
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
