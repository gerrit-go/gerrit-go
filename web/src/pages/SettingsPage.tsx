import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Copy, KeyRound, Plus, ShieldCheck, Smartphone, Trash2, User } from "lucide-react";
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
  const { t } = useTranslation("settings");
  const navigate = useNavigate();

  useEffect(() => {
    if (!loading && !user) navigate("/login", { replace: true });
  }, [loading, user, navigate]);

  if (loading || !user) return null;

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">{t("common:action.settings")}</h1>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ProfileCard user={user} />
        <PasswordCard />
        <TwoFactorCard />
        <HTTPPasswordCard />
        <SSHKeysCard />
      </div>
      {user.admin && <AdminAccountsCard />}
    </div>
  );
}

function ProfileCard({ user }: { user: AccountInfo }) {
  const { refresh } = useAuth();
  const { t } = useTranslation("settings");
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
      setMsg(t("profile.updated"));
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
          {t("profile.title")}
        </CardTitle>
        <CardDescription>
          {t("profile.signedInAs")} <span className="font-mono">@{user.username}</span>
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={save} className="flex flex-col gap-3">
          <div className="flex flex-col gap-2">
            <Label htmlFor="profile-name">{t("profile.fullName")}</Label>
            <Input id="profile-name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="profile-email">{t("profile.email")}</Label>
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
            {busy ? t("common:action.saving") : t("profile.save")}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function PasswordCard() {
  const { t } = useTranslation("settings");
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
      setError(t("password.mismatch"));
      return;
    }
    setBusy(true);
    try {
      await api.setPassword(oldPass, newPass);
      setOldPass("");
      setNewPass("");
      setConfirm("");
      setMsg(t("password.changed"));
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
          {t("password.title")}
        </CardTitle>
        <CardDescription>{t("password.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={save} className="flex flex-col gap-3">
          <div className="flex flex-col gap-2">
            <Label htmlFor="cur-pass">{t("password.current")}</Label>
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
            <Label htmlFor="new-pass">{t("password.new")}</Label>
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
            <Label htmlFor="confirm-pass">{t("password.confirm")}</Label>
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
            {busy ? t("password.updating") : t("password.change")}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function TwoFactorCard() {
  const { t } = useTranslation("settings");
  const [state, setState] = useState<{ enabled: boolean; enrolled: boolean } | null>(null);
  const [secret, setSecret] = useState("");
  const [uri, setUri] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);

  const load = () => {
    api
      .get2FA()
      .then(setState)
      .catch(() => setState({ enabled: false, enrolled: false }));
  };
  useEffect(load, []);

  const enroll = async () => {
    setBusy(true);
    setError("");
    setCopied(false);
    try {
      const res = await api.enroll2FA();
      setSecret(res.secret);
      setUri(res.otpauth_uri);
      setCode("");
      setState({ enabled: false, enrolled: true });
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const enable = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.enable2FA(code);
      setSecret("");
      setUri("");
      setCode("");
      setState({ enabled: true, enrolled: true });
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const disable = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.disable2FA(code);
      setCode("");
      setState({ enabled: false, enrolled: false });
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

  const enabled = state?.enabled ?? false;
  const enrolled = state?.enrolled ?? false;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <Smartphone className="size-4 text-muted-foreground" />
          {t("twoFactor.title")}
        </CardTitle>
        <CardDescription>{t("twoFactor.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">{t("twoFactor.status")}</span>
          {state === null ? (
            <span className="text-muted-foreground">{t("common:common.loading")}</span>
          ) : enabled ? (
            <Badge variant="success">{t("twoFactor.enabled")}</Badge>
          ) : (
            <Badge variant="muted">{t("twoFactor.disabled")}</Badge>
          )}
        </div>

        {secret && (
          <div className="flex flex-col gap-2 rounded-md border bg-muted/40 p-3">
            <p className="text-xs text-muted-foreground">{t("twoFactor.scanHint")}</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 break-all font-mono text-sm">{secret}</code>
              <Button variant="outline" size="sm" onClick={copy}>
                <Copy className="size-4" />
                {copied ? t("common:action.copied") : t("common:action.copy")}
              </Button>
            </div>
            <code className="break-all font-mono text-[10px] text-muted-foreground">{uri}</code>
          </div>
        )}

        {!enabled && (
          <form onSubmit={enable} className="flex flex-col gap-2">
            {!enrolled ? (
              <Button type="button" onClick={enroll} disabled={busy} className="self-start">
                <Plus className="size-4" />
                {t("twoFactor.enroll")}
              </Button>
            ) : (
              <>
                <Label htmlFor="2fa-code">{t("twoFactor.codeLabel")}</Label>
                <div className="flex gap-2">
                  <Input
                    id="2fa-code"
                    value={code}
                    onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                    inputMode="numeric"
                    placeholder="000000"
                    className="max-w-40 font-mono"
                    required
                  />
                  <Button type="submit" disabled={busy || code.length !== 6}>
                    {t("twoFactor.enable")}
                  </Button>
                </div>
              </>
            )}
          </form>
        )}

        {enabled && (
          <form onSubmit={disable} className="flex flex-col gap-2">
            <Label htmlFor="2fa-disable-code">{t("twoFactor.disableHint")}</Label>
            <div className="flex gap-2">
              <Input
                id="2fa-disable-code"
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                inputMode="numeric"
                placeholder="000000"
                className="max-w-40 font-mono"
                required
              />
              <Button type="submit" variant="outline" disabled={busy || code.length !== 6}>
                <Trash2 className="size-4" />
                {t("twoFactor.disable")}
              </Button>
            </div>
          </form>
        )}

        {error && <p className="text-sm text-destructive">{error}</p>}
      </CardContent>
    </Card>
  );
}

function HTTPPasswordCard() {
  const { t } = useTranslation("settings");
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
          {t("httpPassword.title")}
        </CardTitle>
        <CardDescription>
          {t("httpPassword.descriptionPrefix")} <code className="font-mono">/a/</code>{" "}
          {t("httpPassword.descriptionSuffix")}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">{t("httpPassword.status")}</span>
          {enabled === null ? (
            <span className="text-muted-foreground">{t("common:common.loading")}</span>
          ) : enabled ? (
            <Badge variant="success">{t("httpPassword.enabled")}</Badge>
          ) : (
            <Badge variant="muted">{t("httpPassword.notSet")}</Badge>
          )}
        </div>
        {secret && (
          <div className="flex flex-col gap-2 rounded-md border bg-muted/40 p-3">
            <p className="text-xs text-muted-foreground">{t("httpPassword.copyWarning")}</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 break-all font-mono text-sm">{secret}</code>
              <Button variant="outline" size="sm" onClick={copy}>
                <Copy className="size-4" />
                {copied ? t("common:action.copied") : t("common:action.copy")}
              </Button>
            </div>
          </div>
        )}
        {error && <p className="text-sm text-destructive">{error}</p>}
        <div className="flex gap-2">
          <Button onClick={generate} disabled={busy} variant={enabled ? "outline" : "default"}>
            {enabled ? t("httpPassword.regenerate") : t("httpPassword.generate")}
          </Button>
          {enabled && (
            <Button onClick={clear} disabled={busy} variant="ghost">
              <Trash2 className="size-4" />
              {t("httpPassword.clear")}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function SSHKeysCard() {
  const { t } = useTranslation("settings");
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
          {t("sshKeys.title")}
        </CardTitle>
        <CardDescription>{t("sshKeys.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {keys === null ? (
          <p className="text-sm text-muted-foreground">{t("common:common.loading")}</p>
        ) : keys.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("sshKeys.empty")}</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {keys.map((k) => (
              <li key={k.id} className="flex items-center gap-2 rounded-md border p-2">
                <div className="min-w-0 flex-1">
                  <code className="block truncate font-mono text-xs">{k.public_key}</code>
                  <span className="text-xs text-muted-foreground">
                    {k.comment || t("sshKeys.noComment")} ·{" "}
                    {t("sshKeys.added", { time: timeAgo(k.created) })}
                  </span>
                </div>
                <Button variant="ghost" size="icon" onClick={() => remove(k.id)} aria-label={t("sshKeys.deleteKey")}>
                  <Trash2 className="size-4" />
                </Button>
              </li>
            ))}
          </ul>
        )}
        <form onSubmit={add} className="flex flex-col gap-2">
          <Label htmlFor="ssh-key">{t("sshKeys.addLabel")}</Label>
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
            {t("sshKeys.add")}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function AdminAccountsCard() {
  const { t } = useTranslation("settings");
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
          {t("accounts.title")} <Badge variant="outline">{t("accounts.admin")}</Badge>
        </CardTitle>
        <CardDescription>{t("accounts.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-16">{t("accounts.id")}</TableHead>
                <TableHead>{t("accounts.username")}</TableHead>
                <TableHead>{t("common:common.name")}</TableHead>
                <TableHead>{t("profile.email")}</TableHead>
                <TableHead className="w-24">{t("accounts.role")}</TableHead>
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
                    {a.admin ? <Badge variant="secondary">{t("accounts.admin")}</Badge> : <Badge variant="muted">{t("accounts.user")}</Badge>}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <form onSubmit={create} className="flex flex-col gap-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="acc-user">{t("accounts.username")}</Label>
              <Input id="acc-user" value={username} onChange={(e) => setUsername(e.target.value)} required />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="acc-pass">{t("accounts.password")}</Label>
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
              <Label htmlFor="acc-name">{t("profile.fullName")}</Label>
              <Input id="acc-name" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="acc-email">{t("profile.email")}</Label>
              <Input id="acc-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
            </div>
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <Button type="submit" disabled={busy} className="self-start">
            <Plus className="size-4" />
            {t("accounts.create")}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
