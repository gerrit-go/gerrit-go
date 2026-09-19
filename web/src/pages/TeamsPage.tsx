import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { FolderTree, Layers, Pencil, Plus, Search, ShieldCheck, Tag, Trash2, UserPlus, Users, X } from "lucide-react";
import {
  api,
  type AccountInfo,
  type NamespaceNode,
  type ProjectInfo,
  type Role,
  type RoleBinding,
  type Team,
  type TeamMemberView,
} from "@/lib/api";
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
import { useToast } from "@/components/ui/toast";

const TEAM_NAME_RE = /^[A-Za-z0-9_.-]+$/;

export default function TeamsPage() {
  const { user } = useAuth();
  const { t } = useTranslation("teams");
  const toast = useToast();
  const [teams, setTeams] = useState<Team[] | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);
  const [editTeam, setEditTeam] = useState<Team | null>(null);
  const [detail, setDetail] = useState<Team | null>(null);
  const [accounts, setAccounts] = useState<AccountInfo[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);

  const admin = !!user?.admin;

  const load = () =>
    api.listTeams().then(setTeams).catch((err) => {
      setError((err as Error).message);
      setTeams([]);
    });

  useEffect(() => {
    load();
    // Pickers degrade to empty lists / raw inputs on failure.
    api.listRoles().then(setRoles).catch(() => setRoles([]));
    if (admin) {
      api.listAccounts().then(setAccounts).catch(() => setAccounts([]));
    }
  }, [admin]);

  const resetForm = () => setEditTeam(null);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">{t("title")}</h1>
        {!admin && <span className="text-sm text-muted-foreground">{t("memberHint")}</span>}
        {admin && (
          <Dialog open={open} onOpenChange={(v) => { setOpen(v); if (!v) resetForm(); }}>
            <DialogTrigger asChild>
              <Button size="sm" className="ml-auto">
                <Plus className="size-4" />
                {t("newTeam")}
              </Button>
            </DialogTrigger>
            <DialogContent>
              <TeamForm
                team={editTeam}
                accounts={accounts}
                admin
                onDone={() => { setOpen(false); resetForm(); load(); }}
                onError={setError}
              />
            </DialogContent>
          </Dialog>
        )}
      </div>
      {error && <p className="text-sm text-destructive">{error}</p>}

      {teams === null ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-36 w-full" />)}
        </div>
      ) : teams.length === 0 ? (
        <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
          {admin ? t("empty") : t("emptyMine")}
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {teams.map((team) => (
            <Card key={team.id} className="min-w-0 gap-3 py-4 transition-shadow hover:shadow-md">
              <CardHeader className="px-4">
                <CardTitle className="flex items-center gap-2 text-base">
                  <Users className="size-4 shrink-0 text-muted-foreground" />
                  <button className="min-w-0 truncate text-left hover:underline" onClick={() => setDetail(team)}>
                    {team.display_name || team.name}
                  </button>
                  <code className="ml-auto text-xs text-muted-foreground">{team.name}</code>
                </CardTitle>
                <CardDescription className="line-clamp-2">{team.description || t("noDescription")}</CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-2 px-4">
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
                  <span>{t("leader")}: {team.leader_name || t("none")}</span>
                  <span>{t("members")}: {team.member_count}</span>
                  {!!team.can_manage && !admin && (
                    <Badge variant="secondary" className="text-xs">{t("leaderBadge")}</Badge>
                  )}
                </div>
                {!!team.scopes?.length && (
                  <div className="flex flex-wrap gap-1">
                    {team.scopes.map((s, i) => (
                      <Badge key={`${s}-${i}`} variant="outline" className="max-w-full truncate text-xs">{s}</Badge>
                    ))}
                  </div>
                )}
                <div className="flex items-center gap-2">
                  <Button size="sm" variant="outline" onClick={() => setDetail(team)}>{t("manage")}</Button>
                  {team.can_manage && (
                    <Button size="sm" variant="outline" onClick={() => { setEditTeam(team); setOpen(true); }}>
                      <Pencil className="size-3.5" />
                    </Button>
                  )}
                  {admin && (
                    <Button size="sm" variant="destructive" onClick={async () => {
                      if (!confirm(t("confirmDelete"))) return;
                      try { await api.deleteTeam(team.id); await load(); toast(t("toastDeleted"), "success"); }
                      catch (err) { setError((err as Error).message); toast((err as Error).message, "error"); }
                    }}>
                      <Trash2 className="size-3.5" />
                    </Button>
                  )}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {/* Leader edit dialog (non-admins cannot open the admin dialog above). */}
      {!admin && (
        <Dialog open={open} onOpenChange={(v) => { setOpen(v); if (!v) resetForm(); }}>
          <DialogContent>
            {editTeam && (
              <TeamForm
                team={editTeam}
                accounts={accounts}
                admin={false}
                onDone={() => { setOpen(false); resetForm(); load(); }}
                onError={setError}
              />
            )}
          </DialogContent>
        </Dialog>
      )}

      {detail && (
        <TeamDetailDialog
          teamId={detail.id}
          isAdmin={admin}
          accounts={accounts}
          roles={roles}
          onClose={() => setDetail(null)}
          onChanged={load}
        />
      )}
    </div>
  );
}

function TeamForm({ team, accounts, admin, onDone, onError }: {
  team: Team | null;
  accounts: AccountInfo[];
  admin: boolean;
  onDone: () => void;
  onError: (msg: string) => void;
}) {
  const { t } = useTranslation("teams");
  const toast = useToast();
  const [name, setName] = useState(team?.name ?? "");
  const [displayName, setDisplayName] = useState(team?.display_name ?? "");
  const [description, setDescription] = useState(team?.description ?? "");
  const [leader, setLeader] = useState(team?.leader_name ?? "");
  const [busy, setBusy] = useState(false);
  const [touched, setTouched] = useState(false);

  // Create-only identity field; validate live for the submit gate, but only
  // surface the red hint after blur so it doesn't flash mid-typing.
  const nameRequired = admin && !team;
  const nameInvalid = nameRequired && !TEAM_NAME_RE.test(name.trim());
  const showNameError = nameInvalid && touched;

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (nameRequired && !TEAM_NAME_RE.test(name.trim())) {
      setTouched(true);
      onError(t("nameRule"));
      return;
    }
    setBusy(true);
    onError("");
    try {
      if (team) {
        if (admin) {
          await api.updateTeam(team.id, { name, display_name: displayName, description, leader });
        } else {
          await api.updateTeam(team.id, { display_name: displayName, description });
        }
        toast(t("toastSaved"), "success");
      } else {
        await api.createTeam({ name: name.trim(), display_name: displayName, description, leader });
        toast(t("toastCreated"), "success");
      }
      onDone();
    } catch (err) {
      onError((err as Error).message);
      toast((err as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{team ? t("editTeam") : t("createTeam")}</DialogTitle>
        <DialogDescription>{admin ? t("createDescription") : t("leaderHint")}</DialogDescription>
      </DialogHeader>
      {admin ? (
        <div className="flex flex-col gap-2">
          <Label htmlFor="team-name">{t("teamName")}</Label>
          <Input id="team-name" value={name} onChange={(e) => setName(e.target.value)}
            onBlur={() => setTouched(true)}
            placeholder="bsp-team" required aria-invalid={showNameError} />
          <p className={`text-xs ${showNameError ? "text-destructive" : "text-muted-foreground"}`}>{t("nameRule")}</p>
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{t("identityLocked")}</p>
      )}
      <div className="flex flex-col gap-2">
        <Label htmlFor="team-display">{t("displayName")}</Label>
        <Input id="team-display" value={displayName} onChange={(e) => setDisplayName(e.target.value)}
          placeholder={t("displayNamePlaceholder")} />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="team-desc">{t("common:common.description")}</Label>
        <Textarea id="team-desc" value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
      </div>
      {admin && (
        <div className="flex flex-col gap-2">
          <Label htmlFor="team-leader">{t("leader")}</Label>
          {accounts.length ? (
            <select id="team-leader" value={leader} onChange={(e) => setLeader(e.target.value)}
              className="rounded border bg-background px-2 py-1.5 text-sm">
              <option value="">{t("noLeader")}</option>
              {accounts.map((a) => (
                <option key={a._account_id} value={a.username}>
                  {a.username}{a.name ? ` — ${a.name}` : ""}
                </option>
              ))}
            </select>
          ) : (
            <Input value={leader} onChange={(e) => setLeader(e.target.value)} placeholder={t("username")} />
          )}
        </div>
      )}
      <DialogFooter>
        <Button type="submit" disabled={busy || nameInvalid || (nameRequired && !name.trim())}>
          {busy ? t("common:action.saving") : team ? t("common:action.save") : t("common:action.create")}
        </Button>
      </DialogFooter>
    </form>
  );
}

function TeamDetailDialog({ teamId, isAdmin, accounts, roles, onClose, onChanged }: {
  teamId: number;
  isAdmin: boolean;
  accounts: AccountInfo[];
  roles: Role[];
  onClose: () => void;
  onChanged: () => void;
}) {
  const { t } = useTranslation("teams");
  const toast = useToast();
  const [team, setTeam] = useState<Team | null>(null);
  const [members, setMembers] = useState<TeamMemberView[]>([]);
  const [bindings, setBindings] = useState<RoleBinding[]>([]);
  const [error, setError] = useState("");
  const [newMember, setNewMember] = useState("");
  const [bindRole, setBindRole] = useState("");
  const [scopes, setScopes] = useState<string[]>([]);
  const [scopeDialog, setScopeDialog] = useState(false);
  const [busy, setBusy] = useState(false);

  const canManage = !!team?.can_manage;

  const memberExclude = useMemo(() => new Set(members.map((m) => m.account_id)), [members]);

  const loadDetail = () =>
    api.getTeam(teamId).then((d) => {
      setTeam(d.team);
      setMembers(d.members ?? []);
      setBindings(d.bindings ?? []);
    }).catch((err) => setError((err as Error).message));

  useEffect(() => { loadDetail(); }, [teamId]);

  const assignableView = roles.filter((r) => (isAdmin ? true : r.team_assignable));

  const addMember = async () => {
    if (!newMember || busy) return;
    setBusy(true);
    setError("");
    try {
      await api.addTeamMember(teamId, newMember);
      toast(t("toastMemberAdded", { name: newMember }), "success");
      setNewMember("");
      await loadDetail();
      onChanged();
    } catch (err) { setError((err as Error).message); toast((err as Error).message, "error"); }
    finally { setBusy(false); }
  };

  const removeMember = async (username?: string, accountID?: number) => {
    if (!username || busy || !confirm(t("confirmRemoveMember"))) return;
    setBusy(true);
    setError("");
    try {
      await api.removeTeamMember(teamId, username);
      toast(t("toastMemberRemoved", { name: username }), "success");
      await loadDetail();
      onChanged();
      if (accountID && team?.leader_id === accountID) setTeam({ ...team, leader_id: 0, leader_name: undefined });
    } catch (err) { setError((err as Error).message); toast((err as Error).message, "error"); }
    finally { setBusy(false); }
  };

  const addBindings = async () => {
    const roleId = parseInt(bindRole, 10);
    if (!roleId || !scopes.length || busy) return;
    setBusy(true);
    setError("");
    try {
      for (const scope of scopes) {
        await api.createRoleBinding(roleId, { subject_type: "team", subject_id: teamId, scope });
      }
      toast(t("toastBound", { count: scopes.length }), "success");
      setScopes([]);
      await loadDetail();
      onChanged();
    } catch (err) { setError((err as Error).message); toast((err as Error).message, "error"); }
    finally { setBusy(false); }
  };

  const removeBinding = async (id: number) => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api.deleteRoleBinding(id);
      toast(t("toastUnbound"), "success");
      await loadDetail();
      onChanged();
    } catch (err) { setError((err as Error).message); toast((err as Error).message, "error"); }
    finally { setBusy(false); }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {team ? (team.display_name || team.name) : t("common:common.loading")}
            {team && <code className="text-xs text-muted-foreground">{team.name}</code>}
          </DialogTitle>
          <DialogDescription>{team?.description || t("noDescription")}</DialogDescription>
        </DialogHeader>
        {error && <p className="text-sm text-destructive">{error}</p>}
        {!team ? (
          <Skeleton className="h-32 w-full" />
        ) : (
          <div className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
              <span>{t("leader")}: {team.leader_name || t("none")}</span>
              {canManage && !isAdmin && <Badge variant="secondary" className="text-xs">{t("leaderBadge")}</Badge>}
            </div>
            {!!team.group_id && (
              <p className="text-xs text-muted-foreground">{t("teamGroupHint", { name: `team:${team.name}` })}</p>
            )}

            <section className="flex flex-col gap-2">
              <h3 className="text-sm font-medium">{t("members")} ({members.length})</h3>
              <div className="flex flex-col gap-1">
                {members.map((m) => (
                  <div key={m.account_id} className="flex items-center gap-2 text-sm">
                    <span>{m.username || `#${m.account_id}`}</span>
                    {m.account_id === team.leader_id && <Badge variant="secondary" className="text-xs">{t("leader")}</Badge>}
                    {canManage && (isAdmin || m.account_id !== team.leader_id) && (
                      <button className="ml-auto text-destructive hover:underline disabled:opacity-50" disabled={busy}
                        onClick={() => removeMember(m.username, m.account_id)}>
                        <X className="size-3.5" />
                      </button>
                    )}
                  </div>
                ))}
              </div>
              {canManage && (
                <div className="flex items-end gap-2">
                  <div className="flex flex-1 flex-col gap-1">
                    <Label className="text-xs">{t("addMember")}</Label>
                    {isAdmin ? (
                      accounts.length ? (
                        <select value={newMember} onChange={(e) => setNewMember(e.target.value)}
                          className="rounded border bg-background px-2 py-1 text-sm">
                          <option value="">{t("selectAccount")}</option>
                          {accounts
                            .filter((a) => !members.some((m) => m.account_id === a._account_id))
                            .map((a) => (
                              <option key={a._account_id} value={a.username}>
                                {a.username}{a.name ? ` — ${a.name}` : ""}
                              </option>
                            ))}
                        </select>
                      ) : (
                        <Input value={newMember} onChange={(e) => setNewMember(e.target.value)}
                          placeholder={t("username")} />
                      )
                    ) : (
                      <CandidatePicker teamId={teamId} exclude={memberExclude} onPick={setNewMember} />
                    )}
                  </div>
                  <Button size="sm" onClick={addMember} disabled={!newMember || busy}>
                    <UserPlus className="size-3.5" />
                    {t("common:action.add")}
                  </Button>
                </div>
              )}
            </section>

            <section className="flex flex-col gap-2">
              <h3 className="flex items-center gap-1.5 text-sm font-medium">
                <ShieldCheck className="size-4" />
                {t("bindings")}
              </h3>
              {bindings.length === 0 ? (
                <p className="text-sm text-muted-foreground">{t("noBindings")}</p>
              ) : (
                <div className="flex flex-col gap-1">
                  {bindings.map((b) => (
                    <div key={b.id} className="flex items-center gap-2 text-sm">
                      <Badge variant="outline" className="text-xs">{b.role_name || `role#${b.role_id}`}</Badge>
                      <span className="text-muted-foreground">{t("scope")}: <code className="text-xs">{b.scope}</code></span>
                      {canManage && (
                        <button className="ml-auto text-destructive hover:underline disabled:opacity-50" disabled={busy} onClick={() => removeBinding(b.id)}>
                          <Trash2 className="size-3" />
                        </button>
                      )}
                    </div>
                  ))}
                </div>
              )}
              {canManage && (
                <div className="flex flex-col gap-2 rounded-md border p-3">
                  <div className="flex flex-wrap items-end gap-2">
                    <div className="flex flex-col gap-1">
                      <Label className="text-xs">{t("role")}</Label>
                      <select value={bindRole} onChange={(e) => setBindRole(e.target.value)}
                        className="rounded border bg-background px-2 py-1 text-sm">
                        <option value="">{t("selectRole")}</option>
                        {assignableView.map((r) => (
                          <option key={r.id} value={r.id}>{r.display_name || r.name}</option>
                        ))}
                      </select>
                    </div>
                    <Button size="sm" variant="outline" onClick={() => setScopeDialog(true)}>
                      <FolderTree className="size-3.5" />
                      {t("selectScope")}
                    </Button>
                    {isAdmin && (
                      <Input
                        className="w-44"
                        placeholder={t("manualScopePlaceholder")}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") {
                            e.preventDefault();
                            const v = (e.target as HTMLInputElement).value.trim();
                            if (v && !scopes.includes(v)) { setScopes([...scopes, v]); (e.target as HTMLInputElement).value = ""; }
                          }
                        }}
                      />
                    )}
                  </div>
                  {!!scopes.length && (
                    <div className="flex flex-wrap items-center gap-1">
                      {scopes.map((s) => (
                        <Badge key={s} variant="outline" className="gap-1 text-xs">
                          {s}
                          <button onClick={() => setScopes(scopes.filter((x) => x !== s))}>
                            <X className="size-3" />
                          </button>
                        </Badge>
                      ))}
                      <span className="text-xs text-muted-foreground">{t("selectedScopes", { count: scopes.length })}</span>
                    </div>
                  )}
                  <div className="flex items-center gap-2">
                    <Button size="sm" onClick={addBindings} disabled={!bindRole || !scopes.length || busy}>
                      {busy ? t("common:action.saving") : t("addBindings", { count: scopes.length })}
                    </Button>
                    {!isAdmin && <span className="text-xs text-muted-foreground">{t("assignableOnly")}</span>}
                  </div>
                </div>
              )}
            </section>
          </div>
        )}
        {scopeDialog && (
          <ScopePickerDialog
            onPick={(picked) => {
              setScopes(Array.from(new Set([...scopes, ...picked])));
              setScopeDialog(false);
            }}
            onClose={() => setScopeDialog(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function CandidatePicker({ teamId, exclude, onPick }: {
  teamId: number;
  exclude: Set<number>;
  onPick: (username: string) => void;
}) {
  const { t } = useTranslation("teams");
  const [q, setQ] = useState("");
  const [sel, setSel] = useState("");
  const [searching, setSearching] = useState(false);
  const [candidates, setCandidates] = useState<AccountInfo[]>([]);

  useEffect(() => {
    setSel("");
    const term = q.trim();
    if (term.length < 2) { setCandidates([]); setSearching(false); return; }
    setSearching(true);
    const timer = setTimeout(() => {
      api.teamAccountCandidates(teamId, term)
        .then((list) => setCandidates((list ?? []).filter((a) => !exclude.has(a._account_id))))
        .catch(() => setCandidates([]))
        .finally(() => setSearching(false));
    }, 300);
    return () => clearTimeout(timer);
  }, [q, teamId, exclude]);

  return (
    <div className="flex flex-col gap-1">
      <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("searchAccountPlaceholder")} />
      {q.trim().length >= 2 && (
        <select value={sel} onChange={(e) => { setSel(e.target.value); onPick(e.target.value); }} disabled={searching}
          className="rounded border bg-background px-2 py-1 text-sm disabled:opacity-60">
          <option value="">{searching ? t("searching") : candidates.length ? t("selectedCandidate") : t("noCandidates")}</option>
          {candidates.map((a) => (
            <option key={a._account_id} value={a.username}>{a.username}{a.name ? ` — ${a.name}` : ""}</option>
          ))}
        </select>
      )}
    </div>
  );
}

function flattenNamespaces(nodes: NamespaceNode[], depth = 0): { node: NamespaceNode; depth: number }[] {
  return nodes.flatMap((n) => [{ node: n, depth }, ...flattenNamespaces(n.children ?? [], depth + 1)]);
}

function ScopePickerDialog({ onPick, onClose }: {
  onPick: (scopes: string[]) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("teams");
  const [tab, setTab] = useState<"ns" | "proj" | "label">("ns");
  const [namespaces, setNamespaces] = useState<NamespaceNode[]>([]);
  const [labels, setLabels] = useState<Record<string, string[]>>({});
  const [nsFilter, setNsFilter] = useState("");
  const [projectQuery, setProjectQuery] = useState("");
  const [projects, setProjects] = useState<ProjectInfo[]>([]);
  const [labelKey, setLabelKey] = useState("");
  const [picked, setPicked] = useState<Set<string>>(new Set());

  useEffect(() => {
    api.listNamespaces().then(setNamespaces).catch(() => {});
    api.listLabels().then(setLabels).catch(() => {});
  }, []);

  useEffect(() => {
    if (tab !== "proj") return;
    api.listProjects(nsFilter ? { namespace: nsFilter } : undefined)
      .then((map) => setProjects(Object.values(map ?? {}).sort((a, b) => a.name.localeCompare(b.name))))
      .catch(() => setProjects([]));
  }, [tab, nsFilter]);

  const flatNs = useMemo(() => flattenNamespaces(namespaces), [namespaces]);
  const nsPaths = useMemo(() => flatNs.map((f) => f.node.path), [flatNs]);
  const shownProjects = useMemo(() => {
    const q = projectQuery.trim().toLowerCase();
    const list = q ? projects.filter((p) => p.name.toLowerCase().includes(q)) : projects;
    return list.slice(0, 60);
  }, [projects, projectQuery]);
  const labelEntries = useMemo<[string, string[]][]>(() => {
    const keys = Object.keys(labels).sort();
    if (labelKey && labels[labelKey]) return [[labelKey, labels[labelKey]]];
    return keys.map((k) => [k, labels[k]]);
  }, [labels, labelKey]);

  const toggle = (scope: string) => {
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(scope)) next.delete(scope);
      else next.add(scope);
      return next;
    });
  };

  const tabBtn = (id: typeof tab, label: string, icon: React.ReactNode) => (
    <button
      className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm ${tab === id ? "bg-secondary font-medium" : "text-muted-foreground hover:bg-accent"}`}
      onClick={() => setTab(id)}
    >
      {icon}{label}
    </button>
  );

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("scopePickerTitle")}</DialogTitle>
          <DialogDescription>{t("scopePickerDescription")}</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-1">
          {tabBtn("ns", t("tabNamespace"), <FolderTree className="size-3.5" />)}
          {tabBtn("proj", t("tabProject"), <Layers className="size-3.5" />)}
          {tabBtn("label", t("tabLabel"), <Tag className="size-3.5" />)}
        </div>
        <div className="max-h-96 overflow-y-auto rounded-md border">
          {tab === "ns" && (
            <div className="flex flex-col">
              {flatNs.length === 0 && <p className="p-4 text-sm text-muted-foreground">{t("common:common.loading")}</p>}
              {flatNs.map(({ node, depth }) => {
                const scope = `${node.path}/*`;
                return (
                  <label key={node.path} className="flex cursor-pointer items-center gap-2 border-b px-3 py-1.5 text-sm last:border-0"
                    style={{ paddingLeft: 12 + depth * 18 }}>
                    <input type="checkbox" checked={picked.has(scope)} onChange={() => toggle(scope)} />
                    <span className="font-medium">{node.name}</span>
                    <span className="text-xs text-muted-foreground">{node.count}</span>
                    <code className="ml-auto text-xs text-muted-foreground">{scope}</code>
                  </label>
                );
              })}
            </div>
          )}
          {tab === "proj" && (
            <div className="flex flex-col gap-2 p-3">
              <div className="flex items-center gap-2">
                <select value={nsFilter} onChange={(e) => setNsFilter(e.target.value)}
                  className="rounded border bg-background px-2 py-1 text-sm">
                  <option value="">{t("allNamespaces")}</option>
                  {nsPaths.map((p) => <option key={p} value={p}>{p}</option>)}
                </select>
                <div className="relative flex-1">
                  <Search className="absolute left-2 top-2 size-3.5 text-muted-foreground" />
                  <Input value={projectQuery} onChange={(e) => setProjectQuery(e.target.value)}
                    placeholder={t("searchProjectPlaceholder")} className="pl-7" />
                </div>
              </div>
              {shownProjects.map((p) => (
                <label key={p.name} className="flex cursor-pointer items-center gap-2 py-1 text-sm">
                  <input type="checkbox" checked={picked.has(p.name)} onChange={() => toggle(p.name)} />
                  <code className="truncate">{p.name}</code>
                </label>
              ))}
              {shownProjects.length === 0 && <p className="py-2 text-sm text-muted-foreground">{t("noProjectsMatch")}</p>}
              {projects.length > shownProjects.length && (
                <p className="pt-1 text-xs text-muted-foreground">{t("moreProjects", { count: projects.length - shownProjects.length })}</p>
              )}
            </div>
          )}
          {tab === "label" && (
            <div className="flex flex-col gap-3 p-3">
              <Input value={labelKey} onChange={(e) => setLabelKey(e.target.value)}
                placeholder={t("labelKeyPlaceholder")} className="max-w-56" />
              {labelEntries.map(([k, values]) => (
                <div key={k} className="flex flex-col gap-1">
                  <p className="text-xs font-medium text-muted-foreground">{k}</p>
                  <div className="flex flex-wrap gap-1">
                    {values.map((v) => {
                      const scope = `label:${k}=${v}`;
                      return (
                        <Badge key={scope} variant={picked.has(scope) ? "default" : "outline"} className="cursor-pointer text-xs"
                          onClick={() => toggle(scope)}>
                          {k}={v}
                        </Badge>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
        <DialogFooter className="items-center gap-2">
          <span className="mr-auto text-xs text-muted-foreground">{t("selectedScopes", { count: picked.size })}</span>
          <Button size="sm" onClick={() => onPick(Array.from(picked))} disabled={!picked.size}>
            {t("addBindings", { count: picked.size })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
