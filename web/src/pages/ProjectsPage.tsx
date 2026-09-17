import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { FolderGit2, Plus, Terminal } from "lucide-react";
import { api, type ProjectInfo } from "@/lib/api";
import { useAuth } from "@/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
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
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [copyFrom, setCopyFrom] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () =>
    api
      .listProjects()
      .then((map) => setProjects(Object.values(map ?? {}).sort((a, b) => a.name.localeCompare(b.name))))
      .catch((err) => {
        setError((err as Error).message);
        setProjects([]);
      });

  useEffect(() => {
    load();
  }, []);

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

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">{t("common:nav.projects")}</h1>
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
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {projects.map((p) => (
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
              <CardContent className="px-4">
                <div className="flex items-center gap-1.5 rounded-md bg-muted px-2 py-1 font-mono text-xs text-muted-foreground">
                  <Terminal className="size-3 shrink-0" />
                  <span className="min-w-0 truncate">git clone {window.location.origin}/git/{p.name}.git</span>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
