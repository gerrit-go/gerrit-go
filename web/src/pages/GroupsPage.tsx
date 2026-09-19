import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Trash2, Users, X } from "lucide-react";
import { api, type GroupInfo, type GroupMember } from "@/lib/api";
import { useAuth } from "@/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { useToast } from "@/components/ui/toast";

export default function GroupsPage() {
  const { user } = useAuth();
  const { t } = useTranslation("groups");
  const toast = useToast();
  const isAdmin = !!user?.admin;
  const [groups, setGroups] = useState<GroupInfo[] | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [membersFor, setMembersFor] = useState<GroupInfo | null>(null);

  const load = () =>
    api
      .listGroups()
      .then((map) => setGroups(Object.values(map ?? {}).sort((a, b) => a.name.localeCompare(b.name))))
      .catch((err) => {
        setError((err as Error).message);
        setGroups([]);
      });

  useEffect(() => {
    load().catch(() => {
      /* anonymous: listGroups returns 401, page shows sign-in hint */
    });
  }, []);

  const onCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.createGroup(name.trim(), description.trim());
      setOpen(false);
      setName("");
      setDescription("");
      await load();
      toast(t("toastGroupCreated"), "success");
    } catch (err) {
      setError((err as Error).message);
      toast((err as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };

  const onDelete = async (g: GroupInfo) => {
    if (busy || !confirm(t("deleteConfirm", { name: g.name }))) return;
    setBusy(true);
    setError("");
    try {
      await api.deleteGroup(g.id);
      await load();
      toast(t("toastGroupDeleted"), "success");
    } catch (err) {
      setError((err as Error).message);
      toast((err as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };

  if (!user) {
    return (
      <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
        {t("signInToView")}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">{t("common:nav.groups")}</h1>
        {isAdmin && (
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
              <Button size="sm" className="ml-auto">
                <Plus className="size-4" />
                {t("newGroup")}
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>{t("createGroup")}</DialogTitle>
                <DialogDescription>
                  {t("createGroupDesc")}
                </DialogDescription>
              </DialogHeader>
              <form onSubmit={onCreate} className="flex flex-col gap-4">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="grp-name">{t("common:common.name")}</Label>
                  <Input
                    id="grp-name"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder={t("namePlaceholder")}
                    required
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="grp-desc">{t("common:common.description")}</Label>
                  <Input
                    id="grp-desc"
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
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

      {error && <p className="text-sm text-destructive">{error}</p>}

      {groups === null ? (
        <div className="flex flex-col gap-1">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : groups.length === 0 ? (
        <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
          {t("noGroups")}
        </div>
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("common:common.name")}</TableHead>
                <TableHead>{t("common:common.description")}</TableHead>
                <TableHead className="w-32 text-right">{t("actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {groups.map((g) => (
                <TableRow key={g.id}>
                  <TableCell className="font-medium">
                    <span className="flex items-center gap-2">
                      {g.name}
                      {g.system && <Badge variant="secondary">system</Badge>}
                    </span>
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {g.description || "—"}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setMembersFor(g)}
                    >
                      <Users className="size-4" />
                      {t("members")}
                    </Button>
                    {isAdmin && !g.system && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => onDelete(g)}
                        aria-label={t("deleteGroupAria", { name: g.name })}
                      >
                        <Trash2 className="size-4 text-destructive" />
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <MembersDialog
        group={membersFor}
        onClose={() => setMembersFor(null)}
        canEdit={isAdmin}
      />
    </div>
  );
}

function MembersDialog({
  group,
  onClose,
  canEdit,
}: {
  group: GroupInfo | null;
  onClose: () => void;
  canEdit: boolean;
}) {
  const { t } = useTranslation("groups");
  const toast = useToast();
  const [members, setMembers] = useState<GroupMember[]>([]);
  const [account, setAccount] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const load = (g: GroupInfo) =>
    api
      .groupMembers(g.id)
      .then((map) => setMembers(Object.values(map ?? {})))
      .catch((err) => setError((err as Error).message));

  useEffect(() => {
    if (group) {
      setMembers([]);
      setError("");
      setAccount("");
      load(group);
    }
  }, [group]);

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!group || busy) return;
    setBusy(true);
    setError("");
    try {
      await api.addGroupMember(group.id, account.trim());
      toast(t("toastMemberAdded", { name: account.trim() }), "success");
      setAccount("");
      await load(group);
    } catch (err) {
      setError((err as Error).message);
      toast((err as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };

  const remove = async (m: GroupMember) => {
    if (!group || busy) return;
    setBusy(true);
    setError("");
    try {
      await api.removeGroupMember(group.id, m.username);
      toast(t("toastMemberRemoved", { name: m.username }), "success");
      await load(group);
    } catch (err) {
      setError((err as Error).message);
      toast((err as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={!!group} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("membersOf", { name: group?.name })}</DialogTitle>
          <DialogDescription>
            {canEdit ? t("membersDescEdit") : t("membersDescReadOnly")}
          </DialogDescription>
        </DialogHeader>
        {canEdit && (
          <form onSubmit={add} className="flex items-end gap-2">
            <div className="flex flex-1 flex-col gap-2">
              <Label htmlFor="member-acct">{t("username")}</Label>
              <Input
                id="member-acct"
                value={account}
                onChange={(e) => setAccount(e.target.value)}
                placeholder={t("memberPlaceholder")}
              />
            </div>
            <Button type="submit" disabled={!account.trim() || busy}>
              <Plus className="size-4" />
              {busy ? t("common:action.saving") : t("common:action.add")}
            </Button>
          </form>
        )}
        {error && <p className="text-sm text-destructive">{error}</p>}
        <div className="max-h-72 overflow-auto rounded-md border">
          {members.length === 0 ? (
            <p className="p-6 text-center text-sm text-muted-foreground">{t("noMembers")}</p>
          ) : (
            <Table>
              <TableBody>
                {members.map((m) => (
                  <TableRow key={m._account_id}>
                    <TableCell>
                      <div className="font-medium">{m.name || m.username}</div>
                      <div className="text-xs text-muted-foreground">@{m.username}</div>
                    </TableCell>
                    <TableCell className="text-right">
                      {canEdit && (
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => remove(m)}
                          disabled={busy}
                          aria-label={t("removeMemberAria", { name: m.username })}
                        >
                          <X className="size-4" />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
