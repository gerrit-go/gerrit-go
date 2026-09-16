import { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
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
  const { t } = useTranslation("login");
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
    if (err) setError(t("ssoFailed", { code: err }));
  }, [searchParams, t]);

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
        <h1 className="text-2xl font-bold">{t("common:brand")}</h1>
      </div>
      <Card className="w-full">
        <CardHeader>
          <CardTitle>{t("welcome")}</CardTitle>
          <CardDescription>{t("subtitle")}</CardDescription>
        </CardHeader>
        <CardContent>
          <Tabs defaultValue="login">
            <TabsList className="w-full">
              <TabsTrigger value="login">{t("signInTab")}</TabsTrigger>
              <TabsTrigger value="register">{t("registerTab")}</TabsTrigger>
            </TabsList>
            <TabsContent value="login">
              <form onSubmit={onLogin} className="flex flex-col gap-4 pt-2">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="username">{t("username")}</Label>
                  <Input
                    id="username"
                    value={loginUser}
                    onChange={(e) => setLoginUser(e.target.value)}
                    autoComplete="username"
                    required
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="password">{t("password")}</Label>
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
                  {busy ? t("signingIn") : t("signInTab")}
                </Button>
              </form>
            </TabsContent>
            <TabsContent value="register">
              <form onSubmit={onRegister} className="flex flex-col gap-4 pt-2">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="reg-username">{t("username")}</Label>
                  <Input
                    id="reg-username"
                    value={regUser}
                    onChange={(e) => setRegUser(e.target.value)}
                    required
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="reg-name">{t("fullName")}</Label>
                  <Input
                    id="reg-name"
                    value={regName}
                    onChange={(e) => setRegName(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="reg-email">{t("email")}</Label>
                  <Input
                    id="reg-email"
                    type="email"
                    value={regEmail}
                    onChange={(e) => setRegEmail(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="reg-password">{t("password")}</Label>
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
                  {busy ? t("creatingAccount") : t("createAccount")}
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
            {t("or")}
            <span className="h-px flex-1 bg-border" />
          </div>
          <Button asChild variant="outline" className="w-full">
            <a href="/login/oauth">{t("ssoButton")}</a>
          </Button>
        </div>
      )}
      <p className="mt-4 text-sm text-muted-foreground">
        <Link to="/" className="underline underline-offset-4 hover:text-foreground">
          {t("continueAnon")}
        </Link>
      </p>
    </div>
  );
}
