import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Trans, useTranslation } from "react-i18next";
import { Archive, ArchiveRestore, Plus, Tag as TagIcon, Trash2, Webhook as WebhookIcon } from "lucide-react";
import { api, type BranchInfo, type TagInfo, type Webhook } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function BranchesPanel({ project, canEdit }: { project: string; canEdit: boolean }) {
  const { t } = useTranslation("projectRefs");
  const [branches, setBranches] = useState<BranchInfo[] | null>(null);
  const [name, setName] = useState("");
  const [from, setFrom] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = () =>
    api.branches(project).then((b) => setBranches(b ?? [])).catch((e) => setError((e as Error).message));

  useEffect(() => {
    setBranches(null);
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project]);

  const create = async () => {
    if (!name.trim()) return;
    setBusy(true);
    setError("");
    try {
      await api.createBranch(project, name.trim(), from.trim() || undefined);
      setName("");
      setFrom("");
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (branch: string) => {
    if (!confirm(t("branches.deleteConfirm", { branch }))) return;
    setError("");
    try {
      await api.deleteBranch(project, branch);
      await load();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      {error && <p className="text-sm text-destructive">{error}</p>}
      {canEdit && (
        <div className="flex flex-wrap items-end gap-3 rounded-lg border p-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="new-branch" className="text-xs">{t("branches.newBranch")}</Label>
            <Input
              id="new-branch"
              className="h-8 w-56 text-sm"
              placeholder={t("branches.namePlaceholder")}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="from-ref" className="text-xs">{t("branches.from")}</Label>
            <Input
              id="from-ref"
              className="h-8 w-56 text-sm"
              placeholder={t("branches.fromPlaceholder")}
              value={from}
              onChange={(e) => setFrom(e.target.value)}
            />
          </div>
          <Button size="sm" onClick={create} disabled={busy || !name.trim()}>
            <Plus className="size-4" />
            {busy ? t("common:action.creating") : t("common:action.create")}
          </Button>
        </div>
      )}
      {branches === null ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : branches.length === 0 ? (
        <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
          {t("branches.empty")}
        </p>
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("common:common.branch")}</TableHead>
                <TableHead className="w-40">{t("branches.revision")}</TableHead>
                {canEdit && <TableHead className="w-12" />}
              </TableRow>
            </TableHeader>
            <TableBody>
              {branches.map((b) => (
                <TableRow key={b.name}>
                  <TableCell className="font-medium">{b.name}</TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {b.sha.slice(0, 10)}
                  </TableCell>
                  {canEdit && (
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-8"
                        aria-label={t("branches.deleteAria", { name: b.name })}
                        onClick={() => remove(b.name)}
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
    </div>
  );
}

export function TagsPanel({ project, canEdit }: { project: string; canEdit: boolean }) {
  const { t } = useTranslation("projectRefs");
  const [tags, setTags] = useState<TagInfo[] | null>(null);
  const [name, setName] = useState("");
  const [from, setFrom] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = () =>
    api.tags(project).then((list) => setTags(list ?? [])).catch((e) => setError((e as Error).message));

  useEffect(() => {
    setTags(null);
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project]);

  const create = async () => {
    if (!name.trim()) return;
    setBusy(true);
    setError("");
    try {
      await api.createTag(project, name.trim(), from.trim() || undefined, message.trim() || undefined);
      setName("");
      setFrom("");
      setMessage("");
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (tag: string) => {
    if (!confirm(t("tags.deleteConfirm", { tag }))) return;
    setError("");
    try {
      await api.deleteTag(project, tag);
      await load();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      {error && <p className="text-sm text-destructive">{error}</p>}
      {canEdit && (
        <div className="flex flex-wrap items-end gap-3 rounded-lg border p-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="new-tag" className="text-xs">{t("tags.newTag")}</Label>
            <Input
              id="new-tag"
              className="h-8 w-44 text-sm"
              placeholder={t("tags.namePlaceholder")}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="tag-from" className="text-xs">{t("tags.at")}</Label>
            <Input
              id="tag-from"
              className="h-8 w-44 text-sm"
              placeholder={t("tags.atPlaceholder")}
              value={from}
              onChange={(e) => setFrom(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="tag-msg" className="text-xs">{t("tags.message")}</Label>
            <Input
              id="tag-msg"
              className="h-8 w-64 text-sm"
              placeholder={t("tags.messagePlaceholder")}
              value={message}
              onChange={(e) => setMessage(e.target.value)}
            />
          </div>
          <Button size="sm" onClick={create} disabled={busy || !name.trim()}>
            <TagIcon className="size-4" />
            {busy ? t("common:action.creating") : t("common:action.create")}
          </Button>
        </div>
      )}
      {tags === null ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : tags.length === 0 ? (
        <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
          {t("tags.empty")}
        </p>
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("tags.tag")}</TableHead>
                <TableHead className="w-40">{t("tags.revision")}</TableHead>
                <TableHead>{t("common:common.message")}</TableHead>
                {canEdit && <TableHead className="w-12" />}
              </TableRow>
            </TableHeader>
            <TableBody>
              {tags.map((tag) => (
                <TableRow key={tag.name}>
                  <TableCell className="font-medium">{tag.name}</TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {tag.sha.slice(0, 10)}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{tag.message ?? ""}</TableCell>
                  {canEdit && (
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-8"
                        aria-label={t("tags.deleteAria", { name: tag.name })}
                        onClick={() => remove(tag.name)}
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
    </div>
  );
}

export function ManagePanel({
  project,
  state,
  isAdmin,
  onStateChanged,
}: {
  project: string;
  state: string;
  isAdmin: boolean;
  onStateChanged: (state: string) => void;
}) {
  const navigate = useNavigate();
  const { t } = useTranslation("projectRefs");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [confirmText, setConfirmText] = useState("");

  if (!isAdmin) {
    return (
      <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
        {t("manage.onlyAdmins")}
      </p>
    );
  }

  const setState = async (next: string) => {
    setBusy(true);
    setError("");
    try {
      const res = await api.setProjectState(project, next);
      onStateChanged(res.state);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const doDelete = async () => {
    setBusy(true);
    setError("");
    try {
      await api.deleteProject(project);
      navigate("/projects");
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  const archived = state !== "ACTIVE";

  return (
    <div className="flex max-w-xl flex-col gap-6">
      {error && <p className="text-sm text-destructive">{error}</p>}

      <section className="flex flex-col gap-2">
        <h2 className="text-sm font-semibold">{t("manage.projectState")}</h2>
        <p className="text-xs text-muted-foreground">
          <Trans
            i18nKey="manage.stateDescription"
            t={t}
            values={{ state }}
            components={{ span: <span className="font-medium text-foreground" /> }}
          />
        </p>
        <div className="flex gap-2">
          {archived ? (
            <Button size="sm" variant="outline" onClick={() => setState("ACTIVE")} disabled={busy}>
              <ArchiveRestore className="size-4" />
              {t("manage.restore")}
            </Button>
          ) : (
            <Button size="sm" variant="outline" onClick={() => setState("READ_ONLY")} disabled={busy}>
              <Archive className="size-4" />
              {t("manage.archive")}
            </Button>
          )}
          {state !== "HIDDEN" && (
            <Button size="sm" variant="outline" onClick={() => setState("HIDDEN")} disabled={busy}>
              {t("manage.hide")}
            </Button>
          )}
        </div>
      </section>

      <section className="flex flex-col gap-2 rounded-lg border border-destructive/40 p-4">
        <h2 className="text-sm font-semibold text-destructive">{t("manage.deleteProject")}</h2>
        <p className="text-xs text-muted-foreground">
          {t("manage.deleteDescription")}
        </p>
        <Button size="sm" variant="destructive" className="self-start" onClick={() => setConfirmOpen(true)}>
          <Trash2 className="size-4" />
          {t("manage.deleteButton")}
        </Button>
      </section>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("manage.confirmTitle", { project })}</DialogTitle>
            <DialogDescription>
              {t("manage.confirmDescription")}
            </DialogDescription>
          </DialogHeader>
          <Input
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder={project}
            autoFocus
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmOpen(false)}>
              {t("common:action.cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={confirmText !== project || busy}
              onClick={doDelete}
            >
              {busy ? t("common:action.deleting") : t("manage.deleteForever")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export function WebhooksPanel({ project, canEdit }: { project: string; canEdit: boolean }) {
  const { t } = useTranslation("projectRefs");
  const [hooks, setHooks] = useState<Webhook[] | null>(null);
  const [url, setUrl] = useState("");
  const [events, setEvents] = useState("*");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = () =>
    api
      .listWebhooks(project)
      .then((h) => setHooks(h ?? []))
      .catch((e) => setError((e as Error).message));

  useEffect(() => {
    setHooks(null);
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project]);

  const create = async () => {
    if (!url.trim()) return;
    setBusy(true);
    setError("");
    try {
      const ev = events
        .split(",")
        .map((e) => e.trim())
        .filter(Boolean);
      await api.createWebhook(project, {
        url: url.trim(),
        events: ev.length ? ev : ["*"],
        secret: secret.trim() || undefined,
      });
      setUrl("");
      setEvents("*");
      setSecret("");
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: number) => {
    if (!confirm(t("webhooks.deleteConfirm"))) return;
    setError("");
    try {
      await api.deleteWebhook(project, id);
      await load();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <p className="text-xs text-muted-foreground">
        <Trans i18nKey="webhooks.explainer" t={t} components={{ code: <code /> }} />
      </p>
      {error && <p className="text-sm text-destructive">{error}</p>}
      {canEdit && (
        <div className="flex flex-wrap items-end gap-3 rounded-lg border p-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="hook-url" className="text-xs">{t("common:common.url")}</Label>
            <Input
              id="hook-url"
              className="h-8 w-72 text-sm"
              placeholder={t("webhooks.urlPlaceholder")}
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="hook-events" className="text-xs">{t("webhooks.events")}</Label>
            <Input
              id="hook-events"
              className="h-8 w-56 text-sm"
              placeholder={t("webhooks.eventsPlaceholder")}
              value={events}
              onChange={(e) => setEvents(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="hook-secret" className="text-xs">{t("webhooks.secret")}</Label>
            <Input
              id="hook-secret"
              className="h-8 w-44 text-sm"
              placeholder={t("webhooks.secretPlaceholder")}
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
            />
          </div>
          <Button size="sm" onClick={create} disabled={busy || !url.trim()}>
            <Plus className="size-4" />
            {busy ? t("common:action.adding") : t("common:action.add")}
          </Button>
        </div>
      )}
      {hooks === null ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : hooks.length === 0 ? (
        <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
          {t("webhooks.empty")}
        </p>
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("common:common.url")}</TableHead>
                <TableHead className="w-48">{t("common:common.events")}</TableHead>
                <TableHead className="w-20">{t("common:common.active")}</TableHead>
                {canEdit && <TableHead className="w-12" />}
              </TableRow>
            </TableHeader>
            <TableBody>
              {hooks.map((h) => (
                <TableRow key={h.id}>
                  <TableCell className="max-w-[24rem] truncate font-mono text-xs">{h.url}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{h.events.join(", ")}</TableCell>
                  <TableCell className="text-xs">{h.active ? t("common:common.yes") : t("common:common.no")}</TableCell>
                  {canEdit && (
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-8"
                        aria-label={t("webhooks.deleteAria")}
                        onClick={() => remove(h.id)}
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
      {!canEdit && (
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <WebhookIcon className="size-3.5" />
          {t("webhooks.onlyOwners")}
        </p>
      )}
    </div>
  );
}
