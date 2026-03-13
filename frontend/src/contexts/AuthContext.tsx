import React, { createContext, useContext, useState, useEffect, useRef } from 'react';

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
  login: (data: any) => void;
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

  // Mantém refs atualizados com o estado mais recente
  useEffect(() => { tokenRef.current = token; }, [token]);
  useEffect(() => { companyIdRef.current = companyId; }, [companyId]);

  // Interceptor global de fetch: injeta Authorization e X-Company-ID em todas as chamadas
  useEffect(() => {
    const originalFetch = window.fetch.bind(window);
    window.fetch = (input: RequestInfo | URL, init: RequestInit = {}) => {
      const headers = new Headers(init.headers || {});
      if (!headers.has('Authorization') && tokenRef.current) {
        headers.set('Authorization', `Bearer ${tokenRef.current}`);
      }
      if (companyIdRef.current) {
        headers.set('X-Company-ID', companyIdRef.current);
      }
      return originalFetch(input, { ...init, headers });
    };
    return () => { window.fetch = originalFetch; };
  }, []);

  useEffect(() => {
    // Restore session from localStorage
    const storedToken = localStorage.getItem('token');
    const storedUser = localStorage.getItem('user');
    const storedEnv = localStorage.getItem('environment');
    const storedGroup = localStorage.getItem('group');
    const storedCompany = localStorage.getItem('company');
    const storedCompanyId = localStorage.getItem('companyId');
    const storedCnpj = localStorage.getItem('cnpj');

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
      .then(res => {
        if (res.ok) return res.json();
        if (res.status === 401) {
          localStorage.clear();
          window.location.href = '/login';
          throw new Error('Session expired');
        }
        throw new Error('Failed to refresh user data');
      })
      .then(userData => {
        setUser(userData);
        localStorage.setItem('user', JSON.stringify(userData));
      })
      .catch(err => console.error("Session refresh error:", err))
      .finally(() => setLoading(false));
    } else {
      setLoading(false);
    }
  }, []);

  const login = (data: any) => {
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

    localStorage.setItem('token', data.token);
    localStorage.setItem('user', JSON.stringify(data.user));
    localStorage.setItem('environment', data.environment_name || '');
    localStorage.setItem('group', data.group_name || '');
    localStorage.setItem('company', companyName || '');
    localStorage.setItem('companyId', companyIdVal || '');
    localStorage.setItem('cnpj', cnpjVal || '');
  };

  const logout = () => {
    // Preserva preferências de empresa antes de limpar o storage
    const prefs: Record<string, string> = {};
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key?.startsWith('pref_company_')) {
        prefs[key] = localStorage.getItem(key) || '';
      }
    }
    localStorage.clear();
    Object.entries(prefs).forEach(([k, v]) => localStorage.setItem(k, v));

    setUser(null);
    setToken(null);
    setEnvironment(null);
    setGroup(null);
    setCompany(null);
    setCompanyId(null);
    setCnpj(null);
    window.location.href = '/login';
  };

  const switchCompany = (id: string, name: string, newCnpj: string) => {
    setCompany(name);
    setCompanyId(id);
    setCnpj(newCnpj);
    localStorage.setItem('company', name);
    localStorage.setItem('companyId', id);
    localStorage.setItem('cnpj', newCnpj);
    // Salva preferência persistente para este usuário
    if (user?.id) {
      localStorage.setItem(`pref_company_${user.id}`, JSON.stringify({ id, name, cnpj: newCnpj }));
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
