import React, { createContext, useContext, useState, useEffect, useRef } from 'react';
import {
  SESSION_KEY_AUTH_VIA_SSO,
  SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT,
  fetchIAMConfig,
} from '@/lib/keycloak/buildLoginUrl';

interface User {
  id: string;
  email: string;
  full_name: string;
  trial_ends_at: string;
  role?: string;
}

interface AuthContextType {
  user: User | null;
  token: string | null;
  environment: string | null;
  group: string | null;
  company: string | null;
  companyId: string | null;
  cnpj: string | null;
  loading: boolean;
  login: (data: any, opts?: { viaSSO?: boolean }) => void;
  logout: () => void;
  switchCompany: (id: string, name: string, cnpj: string) => void;
  isAuthenticated: boolean;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider = ({ children }: { children: React.ReactNode }) => {
  const [user, setUser] = useState<User | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [environment, setEnvironment] = useState<string | null>(null);
  const [group, setGroup] = useState<string | null>(null);
  const [company, setCompany] = useState<string | null>(null);
  const [companyId, setCompanyId] = useState<string | null>(null);
  const [cnpj, setCnpj] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  // Refs para o interceptor de fetch (sem stale closure)
  const tokenRef = useRef<string | null>(null);
  const companyIdRef = useRef<string | null>(null);
  // Guarda contra dupla-invocação de logout() (duplo clique, 2 componentes de navegação)
  const loggingOutRef = useRef(false);
  // Dedup de refresh: garante no máximo 1 chamada a /api/auth/refresh em voo por aba
  const refreshPromiseRef = useRef<Promise<string | null> | null>(null);

  // Mantém refs atualizados com o estado mais recente
  useEffect(() => { tokenRef.current = token; }, [token]);
  useEffect(() => { companyIdRef.current = companyId; }, [companyId]);

  // Renova o access token via refresh cookie httpOnly. Compartilhada (dedup) pelo
  // interceptor de fetch e pela restauração de sessão no mount — nunca duas chamadas
  // concorrentes a /api/auth/refresh na mesma aba.
  const refreshAccessToken = (): Promise<string | null> => {
    if (!refreshPromiseRef.current) {
      refreshPromiseRef.current = (async () => {
        // Logout em andamento vence qualquer refresh pendente — nunca reautentica
        // silenciosamente uma sessão que o usuário acabou de encerrar.
        if (loggingOutRef.current) return null;

        const controller = new AbortController();
        const timeoutId = window.setTimeout(() => controller.abort(), 8000);
        try {
          const res = await fetch('/api/auth/refresh', { method: 'POST', signal: controller.signal });
          if (!res.ok) return null;
          const data = await res.json();
          if (typeof data.token !== 'string' || !data.token) return null;
          if (loggingOutRef.current) return null;
          tokenRef.current = data.token;
          sessionStorage.setItem('token', data.token);
          setToken(data.token);
          return data.token;
        } catch {
          return null;
        } finally {
          window.clearTimeout(timeoutId);
          refreshPromiseRef.current = null;
        }
      })();
    }
    return refreshPromiseRef.current;
  };

  // Interceptor global de fetch: injeta Authorization e X-Company-ID e trata 401 (token expirado)
  useEffect(() => {
    const originalFetch = window.fetch.bind(window);
    window.fetch = async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const headers = new Headers(init.headers || {});
      if (!headers.has('Authorization') && tokenRef.current) {
        headers.set('Authorization', `Bearer ${tokenRef.current}`);
      }
      if (companyIdRef.current) {
        headers.set('X-Company-ID', companyIdRef.current);
      }
      const response = await originalFetch(input, { ...init, headers });

      // Token expirado: redireciona para login com aviso
      const url = typeof input === 'string' ? input : (input as Request).url ?? '';
      const isApiCall  = url.includes('/api/');
      const isAuthCall = url.includes('/api/auth/');
      if (response.status === 401 && isApiCall && !isAuthCall && tokenRef.current && !loggingOutRef.current) {
        const newToken = await refreshAccessToken();
        if (newToken && !loggingOutRef.current) {
          const retryHeaders = new Headers(init.headers || {});
          retryHeaders.set('Authorization', `Bearer ${newToken}`);
          if (companyIdRef.current) {
            retryHeaders.set('X-Company-ID', companyIdRef.current);
          }
          try {
            return await originalFetch(input, { ...init, headers: retryHeaders });
          } catch (retryErr) {
            console.error('[Auth] Falha ao repetir requisição após refresh:', retryErr);
          }
        }
        console.error('[Auth] Refresh de token falhou — sessão encerrada.');
        sessionStorage.clear();
        sessionStorage.setItem('session_expired', '1');
        window.location.href = '/login';
      }

      return response;
    };
    return () => { window.fetch = originalFetch; };
  }, []);

  useEffect(() => {
    // Restore session from sessionStorage
    const storedToken = sessionStorage.getItem('token');
    const storedUser = sessionStorage.getItem('user');
    const storedEnv = sessionStorage.getItem('environment');
    const storedGroup = sessionStorage.getItem('group');
    const storedCompany = sessionStorage.getItem('company');
    const storedCompanyId = sessionStorage.getItem('companyId');
    const storedCnpj = sessionStorage.getItem('cnpj');

    if (storedToken && storedUser) {
      setToken(storedToken);
      tokenRef.current = storedToken;
      setUser(JSON.parse(storedUser));
      setEnvironment(storedEnv);
      setGroup(storedGroup);
      setCompany(storedCompany);
      setCompanyId(storedCompanyId);
      companyIdRef.current = storedCompanyId;
      setCnpj(storedCnpj);

      // Refresh user profile from server to ensure role and trial status are up to date
      fetch('/api/auth/me', {
        headers: { Authorization: `Bearer ${storedToken}` }
      })
      .then(async (res) => {
        if (res.ok) return res.json();
        if (res.status === 401) {
          const newToken = await refreshAccessToken();
          if (newToken) {
            const retryRes = await fetch('/api/auth/me', {
              headers: { Authorization: `Bearer ${newToken}` }
            });
            if (retryRes.ok) return retryRes.json();
          }
          sessionStorage.clear();
          window.location.href = '/login';
          throw new Error('Session expired');
        }
        throw new Error('Failed to refresh user data');
      })
      .then(userData => {
        // Se um logout aconteceu enquanto o refresh estava em voo, não repopula a
        // sessão que acabou de ser encerrada.
        if (loggingOutRef.current) return;
        setUser(userData);
        sessionStorage.setItem('user', JSON.stringify(userData));
      })
      .catch(err => console.error("Session refresh error:", err))
      .finally(() => setLoading(false));
    } else {
      setLoading(false);
    }
  }, []);

  const login = (data: any, opts?: { viaSSO?: boolean }) => {
    setToken(data.token);
    setUser(data.user);
    setEnvironment(data.environment_name);
    setGroup(data.group_name);

    // Restaura preferência de empresa salva para este usuário (persiste após logout)
    let companyName = data.company_name;
    let companyIdVal = data.company_id;
    let cnpjVal = data.cnpj;
    if (data.user?.id) {
      const saved = localStorage.getItem(`pref_company_${data.user.id}`);
      if (saved) {
        try {
          const pref = JSON.parse(saved);
          if (pref.id) {
            companyName = pref.name;
            companyIdVal = pref.id;
            cnpjVal = pref.cnpj || '';
          }
        } catch {}
      }
    }

    setCompany(companyName);
    setCompanyId(companyIdVal);
    setCnpj(cnpjVal);

    sessionStorage.setItem('token', data.token);
    sessionStorage.setItem('user', JSON.stringify(data.user));
    sessionStorage.setItem('environment', data.environment_name || '');
    sessionStorage.setItem('group', data.group_name || '');
    sessionStorage.setItem('company', companyName || '');
    sessionStorage.setItem('companyId', companyIdVal || '');
    sessionStorage.setItem('cnpj', cnpjVal || '');
    // Sempre reescrita — nunca deixa resíduo de uma sessão anterior (ex: aba duplicada
    // com sessão SSO que depois loga por senha) marcando um logout futuro como SSO.
    sessionStorage.setItem(SESSION_KEY_AUTH_VIA_SSO, opts?.viaSSO ? '1' : '0');
  };

  const logout = () => {
    // Evita que uma segunda chamada concorrente (duplo clique, 2 componentes de
    // navegação) vença a navegação já em curso da primeira.
    if (loggingOutRef.current) return;
    loggingOutRef.current = true;

    // Captura a marca de origem SSO antes do .clear() — decide o destino do redirect.
    const wasSSO = sessionStorage.getItem(SESSION_KEY_AUTH_VIA_SSO) === '1';

    // Limpeza local sempre acontece de imediato, antes de qualquer chamada de rede ao
    // Keycloak — o app nunca fica "preso" esperando o IdP responder. Preferências de
    // empresa (pref_company_*) ficam intactas no localStorage.
    sessionStorage.clear();

    setUser(null);
    setToken(null);
    setEnvironment(null);
    setGroup(null);
    setCompany(null);
    setCompanyId(null);
    setCnpj(null);

    if (!wasSSO) {
      window.location.href = '/login';
      return;
    }

    // Sessão via SSO: suprime o auto-redirect de login que Login.tsx dispararia caso
    // monte (via ProtectedRoute) antes de fetchIAMConfig() resolver.
    sessionStorage.setItem(SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT, '1');

    let settled = false;
    const timeoutId = window.setTimeout(() => {
      if (settled) return;
      settled = true;
      window.location.href = '/login';
    }, 5000);

    fetchIAMConfig().then((config) => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timeoutId);

      if (config.enabled && config.base_url && config.client_id) {
        // URI exata cadastrada pelo time do Marlos em "Valid Post Logout Redirect URIs"
        // no client fb-apu02 do Keycloak — sem query string. O loop de auto-redirect que
        // motivaria um "?password" já é evitado pela flag SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT
        // acima, que sobrevive à ida-e-volta pelo Keycloak (outro domínio) no sessionStorage
        // desta origem.
        const params = new URLSearchParams({
          client_id: config.client_id,
          post_logout_redirect_uri: `${window.location.origin}/login`,
        });
        window.location.href = `${config.base_url}/protocol/openid-connect/logout?${params.toString()}`;
      } else {
        window.location.href = '/login';
      }
    }).catch(() => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timeoutId);
      window.location.href = '/login';
    });
  };

  const switchCompany = (id: string, name: string, newCnpj: string) => {
    setCompany(name);
    setCompanyId(id);
    setCnpj(newCnpj);
    sessionStorage.setItem('company', name);
    sessionStorage.setItem('companyId', id);
    sessionStorage.setItem('cnpj', newCnpj);
    // Salva preferência persistente para este usuário (localStorage + banco)
    if (user?.id) {
      localStorage.setItem(`pref_company_${user.id}`, JSON.stringify({ id, name, cnpj: newCnpj }));
    }
    const tok = sessionStorage.getItem('token');
    if (tok) {
      fetch('/api/user/preferred-company', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${tok}` },
        body: JSON.stringify({ company_id: id }),
      }).catch(() => {}); // fire-and-forget
    }
    window.location.reload();
  };

  return (
    <AuthContext.Provider value={{
      user,
      token,
      environment,
      group,
      company,
      companyId,
      cnpj,
      loading,
      login,
      logout,
      switchCompany,
      isAuthenticated: !!user
    }}>
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};
