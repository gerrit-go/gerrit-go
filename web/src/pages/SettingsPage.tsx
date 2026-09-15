import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Copy, KeyRound, Plus, ShieldCheck, Trash2, User } from "lucide-react";
import { api, type AccountInfo, type SSHKeyInfo } from "@/lib/api";
import { useAuth } from "@/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { timeAgo } from "@/lib/utils";

export default function SettingsPage() {
  const { user, loading } = useAuth();
  const navigate = useNavigate();

  useEffect(() => {
    if (!loading && !user) navigate("/login", { replace: true });
  }, [loading, user, navigate]);

  if (loading || !user) return null;

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Settings</h1>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ProfileCard user={user} />
        <PasswordCard />
        <HTTPPasswordCard />
        <SSHKeysCard />
      </div>
      {user.admin && <AdminAccountsCard />}
    </div>
  );
}

function ProfileCard({ user }: { user: AccountInfo }) {
  const { refresh } = useAuth();
  const [name, setName] = useState(user.name ?? "");
  const [email, setEmail] = useState(user.email ?? "");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setMsg("");
    setError("");
    try {
      await api.updateSelf({ name, email });
      await refresh();
      setMsg("Profile updated");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <User className="size-4 text-muted-foreground" />
          Profile
        </CardTitle>
        <CardDescription>
          Signed in as <span className="font-mono">@{user.username}</span>
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={save} className="flex flex-col gap-3">
          <div className="flex flex-col gap-2">
            <Label htmlFor="profile-name">Full name</Label>
            <Input id="profile-name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="profile-email">Email</Label>
            <Input
              id="profile-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          {msg && <p className="text-sm text-emerald-600 dark:text-emerald-400">{msg}</p>}
          <Button type="submit" disabled={busy} className="self-start">
            {busy ? "Saving…" : "Save profile"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function PasswordCard() {
  const [oldPass, setOldPass] = useState("");
  const [newPass, setNewPass] = useState("");
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setMsg("");
    setError("");
    if (newPass !== confirm) {
      setError("New passwords do not match");
      return;
    }
    setBusy(true);
    try {
      await api.setPassword(oldPass, newPass);
      setOldPass("");
      setNewPass("");
      setConfirm("");
      setMsg("Password changed");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <ShieldCheck className="size-4 text-muted-foreground" />
          Login password
        </CardTitle>
        <CardDescription>Used to sign in to the web UI.</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={save} className="flex flex-col gap-3">
          <div className="flex flex-col gap-2">
            <Label htmlFor="cur-pass">Current password</Label>
            <Input
              id="cur-pass"
              type="password"
              value={oldPass}
              onChange={(e) => setOldPass(e.target.value)}
              autoComplete="current-password"
              required
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="new-pass">New password</Label>
            <Input
              id="new-pass"
              type="password"
              value={newPass}
              onChange={(e) => setNewPass(e.target.value)}
              autoComplete="new-password"
              required
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="confirm-pass">Confirm new password</Label>
            <Input
              id="confirm-pass"
              type="password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              autoComplete="new-password"
              required
            />
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          {msg && <p className="text-sm text-emerald-600 dark:text-emerald-400">{msg}</p>}
          <Button type="submit" disabled={busy} className="self-start">
            {busy ? "Updating…" : "Change password"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function HTTPPasswordCard() {
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    api
      .httpPasswordStatus()
      .then((s) => setEnabled(s.enabled))
      .catch(() => setEnabled(false));
  }, []);

  const generate = async () => {
    setBusy(true);
    setError("");
    setCopied(false);
    try {
      const res = await api.generateHTTPPassword();
      setSecret(res.http_password);
      setEnabled(true);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const clear = async () => {
    setBusy(true);
    setError("");
    try {
      await api.clearHTTPPassword();
      setSecret("");
      setEnabled(false);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
    } catch {
      /* clipboard unavailable */
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <KeyRound className="size-4 text-muted-foreground" />
          HTTP password
        </CardTitle>
        <CardDescription>
          Credential for git over HTTP and the <code className="font-mono">/a/</code> REST API
          (HTTP basic auth).
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">Status:</span>
          {enabled === null ? (
            <span className="text-muted-foreground">loading…</span>
          ) : enabled ? (
            <Badge variant="success">Enabled</Badge>
          ) : (
            <Badge variant="muted">Not set</Badge>
          )}
        </div>
        {secret && (
          <div className="flex flex-col gap-2 rounded-md border bg-muted/40 p-3">
            <p className="text-xs text-muted-foreground">
              Copy this now — it is shown only once and stored hashed.
            </p>
            <div className="flex items-center gap-2">
              <code className="flex-1 break-all font-mono text-sm">{secret}</code>
              <Button variant="outline" size="sm" onClick={copy}>
                <Copy className="size-4" />
                {copied ? "Copied" : "Copy"}
              </Button>
            </div>
          </div>
        )}
        {error && <p className="text-sm text-destructive">{error}</p>}
        <div className="flex gap-2">
          <Button onClick={generate} disabled={busy} variant={enabled ? "outline" : "default"}>
            {enabled ? "Regenerate" : "Generate"}
          </Button>
          {enabled && (
            <Button onClick={clear} disabled={busy} variant="ghost">
              <Trash2 className="size-4" />
              Clear
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function SSHKeysCard() {
  const [keys, setKeys] = useState<SSHKeyInfo[] | null>(null);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = () => {
    api
      .listSSHKeys()
      .then((k) => setKeys(k ?? []))
      .catch(() => setKeys([]));
  };
  useEffect(load, []);

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.addSSHKey(input.trim());
      setInput("");
      load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: number) => {
    setKeys((ks) => (ks ?? []).filter((k) => k.id !== id));
    try {
      await api.deleteSSHKey(id);
    } catch {
      load();
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <KeyRound className="size-4 text-muted-foreground" />
          SSH public keys
        </CardTitle>
        <CardDescription>Register the public keys you use to identify yourself.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {keys === null ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : keys.length === 0 ? (
          <p className="text-sm text-muted-foreground">No keys registered.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {keys.map((k) => (
              <li key={k.id} className="flex items-center gap-2 rounded-md border p-2">
                <div className="min-w-0 flex-1">
                  <code className="block truncate font-mono text-xs">{k.public_key}</code>
                  <span className="text-xs text-muted-foreground">
                    {k.comment || "no comment"} · added {timeAgo(k.created)}
                  </span>
                </div>
                <Button variant="ghost" size="icon" onClick={() => remove(k.id)} aria-label="Delete key">
                  <Trash2 className="size-4" />
                </Button>
              </li>
            ))}
          </ul>
        )}
        <form onSubmit={add} className="flex flex-col gap-2">
          <Label htmlFor="ssh-key">Add a public key</Label>
          <Textarea
            id="ssh-key"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="ssh-ed25519 AAAAC3Nz… you@example.com"
            rows={2}
            className="font-mono text-xs"
          />
          {error && <p className="text-sm text-destructive">{error}</p>}
          <Button type="submit" disabled={busy || !input.trim()} className="self-start">
            <Plus className="size-4" />
            Add key
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function AdminAccountsCard() {
  const [accounts, setAccounts] = useState<AccountInfo[] | null>(null);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = () => {
    api
      .listAccounts()
      .then((a) => setAccounts(a ?? []))
      .catch(() => setAccounts([]));
  };
  useEffect(load, []);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.createAccount({
        username,
        password,
        name: name || undefined,
        email: email || undefined,
      });
      setUsername("");
      setPassword("");
      setName("");
      setEmail("");
      load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <ShieldCheck className="size-4 text-muted-foreground" />
          Accounts <Badge variant="outline">admin</Badge>
        </CardTitle>
        <CardDescription>Create and inspect server accounts.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-16">ID</TableHead>
                <TableHead>Username</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Email</TableHead>
                <TableHead className="w-24">Role</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(accounts ?? []).map((a) => (
                <TableRow key={a._account_id}>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {a._account_id}
                  </TableCell>
                  <TableCell className="font-mono text-sm">{a.username}</TableCell>
                  <TableCell className="text-sm">{a.name}</TableCell>
                  <TableCell className="text-sm text-muted-foreground">{a.email}</TableCell>
                  <TableCell>
                    {a.admin ? <Badge variant="secondary">admin</Badge> : <Badge variant="muted">user</Badge>}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <form onSubmit={create} className="flex flex-col gap-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="acc-user">Username</Label>
              <Input id="acc-user" value={username} onChange={(e) => setUsername(e.target.value)} required />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="acc-pass">Password</Label>
              <Input
                id="acc-pass"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
                required
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="acc-name">Full name</Label>
              <Input id="acc-name" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="acc-email">Email</Label>
              <Input id="acc-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
            </div>
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <Button type="submit" disabled={busy} className="self-start">
            <Plus className="size-4" />
            Create account
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
