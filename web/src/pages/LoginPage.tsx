import { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { GitPullRequestArrow } from "lucide-react";
import { useAuth } from "@/auth";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

export default function LoginPage() {
  const { signIn, signUp } = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [oauthEnabled, setOauthEnabled] = useState(false);

  useEffect(() => {
    api
      .getConfig()
      .then((c) => setOauthEnabled(!!c?.auth?.oauth))
      .catch(() => setOauthEnabled(false));
    const err = searchParams.get("error");
    if (err) setError(`Single sign-on failed (${err}).`);
  }, [searchParams]);

  const [loginUser, setLoginUser] = useState("");
  const [loginPass, setLoginPass] = useState("");
  const [regUser, setRegUser] = useState("");
  const [regPass, setRegPass] = useState("");
  const [regName, setRegName] = useState("");
  const [regEmail, setRegEmail] = useState("");

  const onLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await signIn(loginUser, loginPass);
      navigate("/");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const onRegister = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await signUp({
        username: regUser,
        password: regPass,
        full_name: regName || undefined,
        email: regEmail || undefined,
      });
      navigate("/");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto flex max-w-md flex-col items-center py-12">
      <div className="mb-6 flex items-center gap-2">
        <span className="flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
          <GitPullRequestArrow className="size-5" />
        </span>
        <h1 className="text-2xl font-bold">Gerrit Go</h1>
      </div>
      <Card className="w-full">
        <CardHeader>
          <CardTitle>Welcome back</CardTitle>
          <CardDescription>Sign in to review code, or create a new account.</CardDescription>
        </CardHeader>
        <CardContent>
          <Tabs defaultValue="login">
            <TabsList className="w-full">
              <TabsTrigger value="login">Sign in</TabsTrigger>
              <TabsTrigger value="register">Register</TabsTrigger>
            </TabsList>
            <TabsContent value="login">
              <form onSubmit={onLogin} className="flex flex-col gap-4 pt-2">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="username">Username</Label>
                  <Input
                    id="username"
                    value={loginUser}
                    onChange={(e) => setLoginUser(e.target.value)}
                    autoComplete="username"
                    required
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="password">Password</Label>
                  <Input
                    id="password"
                    type="password"
                    value={loginPass}
                    onChange={(e) => setLoginPass(e.target.value)}
                    autoComplete="current-password"
                    required
                  />
                </div>
                {error && <p className="text-sm text-destructive">{error}</p>}
                <Button type="submit" disabled={busy} className="w-full">
                  {busy ? "Signing in…" : "Sign in"}
                </Button>
              </form>
            </TabsContent>
            <TabsContent value="register">
              <form onSubmit={onRegister} className="flex flex-col gap-4 pt-2">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="reg-username">Username</Label>
                  <Input
                    id="reg-username"
                    value={regUser}
                    onChange={(e) => setRegUser(e.target.value)}
                    required
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="reg-name">Full name</Label>
                  <Input
                    id="reg-name"
                    value={regName}
                    onChange={(e) => setRegName(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="reg-email">Email</Label>
                  <Input
                    id="reg-email"
                    type="email"
                    value={regEmail}
                    onChange={(e) => setRegEmail(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="reg-password">Password</Label>
                  <Input
                    id="reg-password"
                    type="password"
                    value={regPass}
                    onChange={(e) => setRegPass(e.target.value)}
                    autoComplete="new-password"
                    required
                  />
                </div>
                {error && <p className="text-sm text-destructive">{error}</p>}
                <Button type="submit" disabled={busy} className="w-full">
                  {busy ? "Creating account…" : "Create account"}
                </Button>
              </form>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
      {oauthEnabled && (
        <div className="mt-4 flex w-full max-w-md flex-col items-center gap-3">
          <div className="flex w-full items-center gap-3 text-xs text-muted-foreground">
            <span className="h-px flex-1 bg-border" />
            or
            <span className="h-px flex-1 bg-border" />
          </div>
          <Button asChild variant="outline" className="w-full">
            <a href="/login/oauth">Sign in with single sign-on</a>
          </Button>
        </div>
      )}
      <p className="mt-4 text-sm text-muted-foreground">
        <Link to="/" className="underline underline-offset-4 hover:text-foreground">
          Continue browsing without signing in
        </Link>
      </p>
    </div>
  );
}
