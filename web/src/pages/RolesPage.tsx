import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, ShieldCheck, Trash2, Users } from "lucide-react";
import { api, type Role, type RoleBinding } from "@/lib/api";
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

const KNOWN_PERMISSIONS = [
  "read", "push", "submit", "abandon", "comment",
  "editTopicName", "addReviewer", "createProject", "editAccess", "admin",
];

export default function RolesPage() {
  const { user } = useAuth();
  const { t } = useTranslation("roles");
  const [roles, setRoles] = useState<Role[] | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);
  const [editRole, setEditRole] = useState<Role | null>(null);
  const [name, setName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [permissions, setPermissions] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);

  const load = () =>
    api.listRoles().then(setRoles).catch((err) => {
      setError((err as Error).message);
      setRoles([]);
    });

  useEffect(() => { load(); }, []);

  const resetForm = () => {
    setName(""); setDisplayName(""); setDescription(""); setPermissions([]);
    setEditRole(null);
  };

  const openEdit = (role: Role) => {
    setEditRole(role);
    setName(role.name);
    setDisplayName(role.display_name);
    setDescription(role.description);
    setPermissions(role.permissions || []);
    setOpen(true);
  };

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (editRole) {
        await api.updateRole(editRole.id, { display_name: displayName, description, permissions });
      } else {
        await api.createRole({ name: name.trim(), display_name: displayName, description, permissions });
      }
      setOpen(false);
      resetForm();
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const onDelete = async (id: number) => {
    if (!confirm(t("confirmDelete"))) return;
    try {
      await api.deleteRole(id);
      await load();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const togglePerm = (perm: string) => {
    setPermissions((prev) =>
      prev.includes(perm) ? prev.filter((p) => p !== perm) : [...prev, perm]
    );
  };

  if (!user?.admin) {
    return <div className="p-8 text-center text-muted-foreground">{t("adminOnly")}</div>;
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">{t("title")}</h1>
        <Dialog open={open} onOpenChange={(v) => { setOpen(v); if (!v) resetForm(); }}>
          <DialogTrigger asChild>
            <Button size="sm" className="ml-auto">
              <Plus className="size-4" />
              {t("newRole")}
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-lg">
            <DialogHeader>
              <DialogTitle>{editRole ? t("editRole") : t("createRole")}</DialogTitle>
              <DialogDescription>{t("createDescription")}</DialogDescription>
            </DialogHeader>
            <form onSubmit={onSubmit} className="flex flex-col gap-4">
              {!editRole && (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="role-name">{t("name")}</Label>
                  <Input id="role-name" value={name} onChange={(e) => setName(e.target.value)}
                    placeholder="bsp-lead" required />
                </div>
              )}
              <div className="flex flex-col gap-2">
                <Label htmlFor="role-display">{t("displayName")}</Label>
                <Input id="role-display" value={displayName} onChange={(e) => setDisplayName(e.target.value)}
                  placeholder="BSP Lead" />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="role-desc">{t("common:common.description")}</Label>
                <Textarea id="role-desc" value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
              </div>
              <div className="flex flex-col gap-2">
                <Label>{t("permissions")}</Label>
                <div className="flex flex-wrap gap-1.5">
                  {KNOWN_PERMISSIONS.map((perm) => (
                    <Badge
                      key={perm}
                      variant={permissions.includes(perm) ? "default" : "outline"}
                      className="cursor-pointer"
                      onClick={() => togglePerm(perm)}
                    >
                      {perm}
                    </Badge>
                  ))}
                </div>
              </div>
              {error && <p className="text-sm text-destructive">{error}</p>}
              <DialogFooter>
                <Button type="submit" disabled={busy}>
                  {busy ? t("common:action.saving") : editRole ? t("common:action.save") : t("common:action.create")}
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      {roles === null ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-32 w-full" />)}
        </div>
      ) : roles.length === 0 ? (
        <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
          {t("empty")}
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {roles.map((role) => (
            <RoleCard key={role.id} role={role} onEdit={openEdit} onDelete={onDelete} onChanged={load} />
          ))}
        </div>
      )}
    </div>
  );
}

function RoleCard({ role, onEdit, onDelete, onChanged }: {
  role: Role;
  onEdit: (r: Role) => void;
  onDelete: (id: number) => void;
  onChanged: () => void;
}) {
  const { t } = useTranslation("roles");
  const [bindings, setBindings] = useState<RoleBinding[] | null>(null);
  const [showBindings, setShowBindings] = useState(false);
  const [newSubjectType, setNewSubjectType] = useState<"account" | "group">("group");
  const [newSubjectID, setNewSubjectID] = useState("");
  const [newScope, setNewScope] = useState("*");

  const loadBindings = () =>
    api.listRoleBindings(role.id).then(setBindings).catch(() => setBindings([]));

  useEffect(() => {
    if (showBindings) loadBindings();
  }, [showBindings]);

  const addBinding = async () => {
    const sid = parseInt(newSubjectID, 10);
    if (!sid) return;
    try {
      await api.createRoleBinding(role.id, { subject_type: newSubjectType, subject_id: sid, scope: newScope });
      setNewSubjectID("");
      setNewScope("*");
      await loadBindings();
      onChanged();
    } catch { /* ignore */ }
  };

  const removeBinding = async (id: number) => {
    try {
      await api.deleteRoleBinding(id);
      await loadBindings();
      onChanged();
    } catch { /* ignore */ }
  };

  return (
    <Card className="min-w-0 gap-3 py-4">
      <CardHeader className="px-4">
        <CardTitle className="flex items-center gap-2 text-base">
          <ShieldCheck className="size-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 truncate">{role.display_name || role.name}</span>
          <code className="ml-auto text-xs text-muted-foreground">{role.name}</code>
        </CardTitle>
        <CardDescription className="line-clamp-2">{role.description || t("noDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 px-4">
        <div className="flex flex-wrap gap-1">
          {(role.permissions || []).map((p) => (
            <Badge key={p} variant="secondary" className="text-xs">{p}</Badge>
          ))}
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => onEdit(role)}>{t("common:action.edit")}</Button>
          <Button size="sm" variant="outline" onClick={() => setShowBindings(!showBindings)}>
            <Users className="size-3.5" />
            {t("bindings")}
          </Button>
          <Button size="sm" variant="destructive" onClick={() => onDelete(role.id)}>
            <Trash2 className="size-3.5" />
          </Button>
        </div>
        {showBindings && (
          <div className="rounded-md border p-3">
            <h4 className="mb-2 text-sm font-medium">{t("bindings")}</h4>
            {bindings === null ? (
              <Skeleton className="h-8 w-full" />
            ) : bindings.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("noBindings")}</p>
            ) : (
              <div className="flex flex-col gap-1">
                {bindings.map((b) => (
                  <div key={b.id} className="flex items-center gap-2 text-sm">
                    <Badge variant="outline" className="text-xs">{b.subject_type}</Badge>
                    <span>#{b.subject_id}</span>
                    <span className="text-muted-foreground">{t("scope")}: {b.scope}</span>
                    <button onClick={() => removeBinding(b.id)} className="ml-auto text-destructive hover:underline">
                      <Trash2 className="size-3" />
                    </button>
                  </div>
                ))}
              </div>
            )}
            <div className="mt-2 flex items-end gap-2">
              <div className="flex flex-col gap-1">
                <Label className="text-xs">{t("subjectType")}</Label>
                <select
                  value={newSubjectType}
                  onChange={(e) => setNewSubjectType(e.target.value as "account" | "group")}
                  className="rounded border bg-background px-2 py-1 text-sm"
                >
                  <option value="group">{t("group")}</option>
                  <option value="account">{t("account")}</option>
                </select>
              </div>
              <div className="flex flex-col gap-1">
                <Label className="text-xs">{t("subjectId")}</Label>
                <Input value={newSubjectID} onChange={(e) => setNewSubjectID(e.target.value)}
                  placeholder="ID" className="w-20" />
              </div>
              <div className="flex flex-col gap-1">
                <Label className="text-xs">{t("scope")}</Label>
                <Input value={newScope} onChange={(e) => setNewScope(e.target.value)}
                  placeholder="*" className="w-32" />
              </div>
              <Button size="sm" onClick={addBinding} disabled={!newSubjectID}>{t("add")}</Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
