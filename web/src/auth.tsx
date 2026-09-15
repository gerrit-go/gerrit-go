import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { api, type AccountInfo } from "@/lib/api";

interface AuthState {
  user: AccountInfo | null;
  loading: boolean;
  signIn: (username: string, password: string) => Promise<void>;
  signUp: (body: {
    username: string;
    password: string;
    full_name?: string;
    email?: string;
  }) => Promise<void>;
  signOut: () => Promise<void>;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AccountInfo | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .self()
      .then(setUser)
      .catch(() => setUser(null))
      .finally(() => setLoading(false));
  }, []);

  const signIn = async (username: string, password: string) => {
    const acct = await api.login(username, password);
    setUser(acct);
  };

  const signUp = async (body: {
    username: string;
    password: string;
    full_name?: string;
    email?: string;
  }) => {
    const acct = await api.register(body);
    setUser(acct);
  };

  const signOut = async () => {
    await api.logout();
    setUser(null);
  };

  return (
    <AuthContext.Provider value={{ user, loading, signIn, signUp, signOut }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
