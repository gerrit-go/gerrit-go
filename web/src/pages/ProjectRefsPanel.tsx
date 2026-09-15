import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
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
    if (!confirm(`Delete branch "${branch}"? This cannot be undone.`)) return;
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
            <Label htmlFor="new-branch" className="text-xs">New branch</Label>
            <Input
              id="new-branch"
              className="h-8 w-56 text-sm"
              placeholder="feature/my-work"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="from-ref" className="text-xs">From (optional)</Label>
            <Input
              id="from-ref"
              className="h-8 w-56 text-sm"
              placeholder="master / commit SHA"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
            />
          </div>
          <Button size="sm" onClick={create} disabled={busy || !name.trim()}>
            <Plus className="size-4" />
            {busy ? "Creating…" : "Create"}
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
          No branches yet.
        </p>
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Branch</TableHead>
                <TableHead className="w-40">Revision</TableHead>
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
                        aria-label={`Delete ${b.name}`}
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
  const [tags, setTags] = useState<TagInfo[] | null>(null);
  const [name, setName] = useState("");
  const [from, setFrom] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = () =>
    api.tags(project).then((t) => setTags(t ?? [])).catch((e) => setError((e as Error).message));

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
    if (!confirm(`Delete tag "${tag}"?`)) return;
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
            <Label htmlFor="new-tag" className="text-xs">New tag</Label>
            <Input
              id="new-tag"
              className="h-8 w-44 text-sm"
              placeholder="v1.0.0"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="tag-from" className="text-xs">At (optional)</Label>
            <Input
              id="tag-from"
              className="h-8 w-44 text-sm"
              placeholder="master / SHA"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="tag-msg" className="text-xs">Message (annotated)</Label>
            <Input
              id="tag-msg"
              className="h-8 w-64 text-sm"
              placeholder="Release notes…"
              value={message}
              onChange={(e) => setMessage(e.target.value)}
            />
          </div>
          <Button size="sm" onClick={create} disabled={busy || !name.trim()}>
            <TagIcon className="size-4" />
            {busy ? "Creating…" : "Create"}
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
          No tags yet.
        </p>
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Tag</TableHead>
                <TableHead className="w-40">Revision</TableHead>
                <TableHead>Message</TableHead>
                {canEdit && <TableHead className="w-12" />}
              </TableRow>
            </TableHeader>
            <TableBody>
              {tags.map((t) => (
                <TableRow key={t.name}>
                  <TableCell className="font-medium">{t.name}</TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {t.sha.slice(0, 10)}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{t.message ?? ""}</TableCell>
                  {canEdit && (
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-8"
                        aria-label={`Delete ${t.name}`}
                        onClick={() => remove(t.name)}
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
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [confirmText, setConfirmText] = useState("");

  if (!isAdmin) {
    return (
      <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
        Only administrators can archive or delete a project.
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
        <h2 className="text-sm font-semibold">Project state</h2>
        <p className="text-xs text-muted-foreground">
          Current state: <span className="font-medium text-foreground">{state}</span>. Archiving makes
          the project read-only; hiding removes it from listings.
        </p>
        <div className="flex gap-2">
          {archived ? (
            <Button size="sm" variant="outline" onClick={() => setState("ACTIVE")} disabled={busy}>
              <ArchiveRestore className="size-4" />
              Restore (Active)
            </Button>
          ) : (
            <Button size="sm" variant="outline" onClick={() => setState("READ_ONLY")} disabled={busy}>
              <Archive className="size-4" />
              Archive (Read-only)
            </Button>
          )}
          {state !== "HIDDEN" && (
            <Button size="sm" variant="outline" onClick={() => setState("HIDDEN")} disabled={busy}>
              Hide
            </Button>
          )}
        </div>
      </section>

      <section className="flex flex-col gap-2 rounded-lg border border-destructive/40 p-4">
        <h2 className="text-sm font-semibold text-destructive">Delete project</h2>
        <p className="text-xs text-muted-foreground">
          Permanently deletes the repository, all of its changes, comments and votes. This cannot be
          undone.
        </p>
        <Button size="sm" variant="destructive" className="self-start" onClick={() => setConfirmOpen(true)}>
          <Trash2 className="size-4" />
          Delete project…
        </Button>
      </section>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete {project}?</DialogTitle>
            <DialogDescription>
              Type the project name to confirm permanent deletion.
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
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={confirmText !== project || busy}
              onClick={doDelete}
            >
              {busy ? "Deleting…" : "Delete forever"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export function WebhooksPanel({ project, canEdit }: { project: string; canEdit: boolean }) {
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
    if (!confirm("Delete this webhook?")) return;
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
        Webhooks POST a JSON event payload to the URL whenever a matching change event occurs. Set a
        secret to receive an <code>X-GerritGo-Signature</code> HMAC-SHA256 header. Use{" "}
        <code>*</code> to subscribe to every event.
      </p>
      {error && <p className="text-sm text-destructive">{error}</p>}
      {canEdit && (
        <div className="flex flex-wrap items-end gap-3 rounded-lg border p-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="hook-url" className="text-xs">URL</Label>
            <Input
              id="hook-url"
              className="h-8 w-72 text-sm"
              placeholder="https://example.com/hook"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="hook-events" className="text-xs">Events (comma-separated)</Label>
            <Input
              id="hook-events"
              className="h-8 w-56 text-sm"
              placeholder="* or created,submitted"
              value={events}
              onChange={(e) => setEvents(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="hook-secret" className="text-xs">Secret (optional)</Label>
            <Input
              id="hook-secret"
              className="h-8 w-44 text-sm"
              placeholder="shared secret"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
            />
          </div>
          <Button size="sm" onClick={create} disabled={busy || !url.trim()}>
            <Plus className="size-4" />
            {busy ? "Adding…" : "Add"}
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
          No webhooks configured.
        </p>
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>URL</TableHead>
                <TableHead className="w-48">Events</TableHead>
                <TableHead className="w-20">Active</TableHead>
                {canEdit && <TableHead className="w-12" />}
              </TableRow>
            </TableHeader>
            <TableBody>
              {hooks.map((h) => (
                <TableRow key={h.id}>
                  <TableCell className="max-w-[24rem] truncate font-mono text-xs">{h.url}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{h.events.join(", ")}</TableCell>
                  <TableCell className="text-xs">{h.active ? "yes" : "no"}</TableCell>
                  {canEdit && (
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-8"
                        aria-label="Delete webhook"
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
          Only project owners and administrators can manage webhooks.
        </p>
      )}
    </div>
  );
}
