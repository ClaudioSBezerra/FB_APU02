import { useState, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Loader2, KeyRound, Eye, EyeOff, Copy } from 'lucide-react';
import { toast } from 'sonner';

interface BridgeConfig {
  erp_type: string;
  fbtax_email: string;
  fbtax_password_set: boolean;
  oracle_dsn: string;
  oracle_usuario: string;
  oracle_senha_set: boolean;
  api_key: string;
}

export default function ERPBridgeCredenciais() {
  const { token, companyId } = useAuth();
  const qc = useQueryClient();
  const authHeaders = { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId || '' };

  const { data: cfg, isLoading } = useQuery<BridgeConfig>({
    queryKey: ['erp-bridge-config', companyId],
    queryFn: async () => {
      const res = await fetch('/api/erp-bridge/config', { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    enabled: !!token && !!companyId,
  });

  const [erpType, setErpType]             = useState('oracle_xml');
  const [fbtaxEmail, setFbtaxEmail]       = useState('');
  const [fbtaxPassword, setFbtaxPassword] = useState('');
  const [oracleDsn, setOracleDsn]         = useState('');
  const [oracleUsuario, setOracleUsuario] = useState('');
  const [oracleSenha, setOracleSenha]     = useState('');
  const [apiKey, setApiKey]               = useState('');
  const [showFbtaxPwd, setShowFbtaxPwd]   = useState(false);
  const [showOraclePwd, setShowOraclePwd] = useState(false);
  const [showApiKey, setShowApiKey]       = useState(false);

  useEffect(() => {
    if (cfg) {
      setErpType(cfg.erp_type || 'oracle_xml');
      setFbtaxEmail(cfg.fbtax_email || '');
      setOracleDsn(cfg.oracle_dsn || '');
      setOracleUsuario(cfg.oracle_usuario || '');
      setApiKey(cfg.api_key || '');
    }
  }, [cfg]);

  const saveCredentialsMutation = useMutation({
    mutationFn: async () => {
      const body: Record<string, string> = {
        erp_type: erpType,
        fbtax_email: fbtaxEmail,
        oracle_dsn: oracleDsn,
        oracle_usuario: oracleUsuario,
      };
      if (fbtaxPassword)  body.fbtax_password = fbtaxPassword;
      if (oracleSenha)    body.oracle_senha   = oracleSenha;
      const res = await fetch('/api/erp-bridge/config', {
        method: 'PATCH',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(await res.text());
    },
    onSuccess: () => {
      setFbtaxPassword('');
      setOracleSenha('');
      toast.success('Credenciais salvas com segurança.');
      qc.invalidateQueries({ queryKey: ['erp-bridge-config', companyId] });
    },
    onError: (e: Error) => toast.error(`Erro ao salvar credenciais: ${e.message}`),
  });

  const generateApiKeyMutation = useMutation({
    mutationFn: async () => {
      const res = await fetch('/api/erp-bridge/config/generate-api-key', {
        method: 'POST',
        headers: authHeaders,
      });
      if (!res.ok) throw new Error(await res.text());
      return res.json() as Promise<{ api_key: string }>;
    },
    onSuccess: (data) => {
      setApiKey(data.api_key);
      setShowApiKey(true);
      toast.success('Nova API key gerada. Copie e atualize o config.yaml no servidor.');
      qc.invalidateQueries({ queryKey: ['erp-bridge-config', companyId] });
    },
    onError: (e: Error) => toast.error(`Erro ao gerar API key: ${e.message}`),
  });

  if (isLoading) {
    return <div className="flex items-center gap-2 text-sm text-muted-foreground py-8 justify-center"><Loader2 className="h-4 w-4 animate-spin" />Carregando...</div>;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">ERP Bridge — Credenciais</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Credenciais armazenadas criptografadas (AES-256). Visível apenas para administradores.
        </p>
      </div>

      <Card>
        <CardHeader className="py-3 px-4">
          <CardTitle className="text-sm flex items-center gap-2">
            <KeyRound className="h-4 w-4" /> Credenciais do Daemon Bridge
          </CardTitle>
        </CardHeader>
        <CardContent className="px-4 pb-4 space-y-5">
          <p className="text-[11px] text-muted-foreground bg-muted/40 rounded px-3 py-2">
            As senhas são criptografadas com AES-256 antes de serem salvas no banco.
            O daemon Bridge busca as credenciais automaticamente via API key —
            remova <code className="font-mono text-[10px]">email</code>,{' '}
            <code className="font-mono text-[10px]">password</code> e{' '}
            <code className="font-mono text-[10px]">senha</code> do{' '}
            <code className="font-mono text-[10px]">config.yaml</code> após configurar.
          </p>

          {/* Tipo de ERP */}
          <div className="space-y-2">
            <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Tipo de ERP</p>
            <div className="flex flex-wrap gap-3">
              <div className="flex flex-col gap-1 flex-1 min-w-48">
                <label className="text-xs text-muted-foreground">Modo de integração</label>
                <Select value={erpType} onValueChange={setErpType}>
                  <SelectTrigger className="h-8 text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="oracle_xml" className="text-xs">Oracle XML (legado)</SelectItem>
                    <SelectItem value="sap_s4hana" className="text-xs">SAP S/4HANA (FCCORP)</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              {erpType === 'sap_s4hana' && (
                <div className="flex flex-col gap-1 flex-1 min-w-64">
                  <label className="text-xs text-muted-foreground">Oracle DSN (ex: host:1521/FCCORP)</label>
                  <Input value={oracleDsn} onChange={e => setOracleDsn(e.target.value)}
                    className="h-8 text-sm font-mono" placeholder="hostname:1521/FCCORP" />
                </div>
              )}
            </div>
          </div>

          {/* FBTax */}
          <div className="space-y-2">
            <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">FBTax</p>
            <div className="flex flex-wrap gap-3">
              <div className="flex flex-col gap-1 flex-1 min-w-48">
                <label className="text-xs text-muted-foreground">E-mail</label>
                <Input value={fbtaxEmail} onChange={e => setFbtaxEmail(e.target.value)}
                  className="h-8 text-sm" placeholder="usuario@empresa.com.br" />
              </div>
              <div className="flex flex-col gap-1 flex-1 min-w-48">
                <label className="text-xs text-muted-foreground">
                  Senha {cfg?.fbtax_password_set && <span className="text-green-600 ml-1">✓ configurada</span>}
                </label>
                <div className="relative">
                  <Input type={showFbtaxPwd ? 'text' : 'password'}
                    value={fbtaxPassword} onChange={e => setFbtaxPassword(e.target.value)}
                    className="h-8 text-sm pr-8"
                    placeholder={cfg?.fbtax_password_set ? '••••••••' : 'Nova senha'} />
                  <button type="button" onClick={() => setShowFbtaxPwd(v => !v)}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                    {showFbtaxPwd ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                  </button>
                </div>
              </div>
            </div>
          </div>

          {/* Oracle */}
          <div className="space-y-2">
            <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Oracle ERP</p>
            <div className="flex flex-wrap gap-3">
              <div className="flex flex-col gap-1 flex-1 min-w-36">
                <label className="text-xs text-muted-foreground">Usuário</label>
                <Input value={oracleUsuario} onChange={e => setOracleUsuario(e.target.value)}
                  className="h-8 text-sm" placeholder="fcosta" />
              </div>
              <div className="flex flex-col gap-1 flex-1 min-w-36">
                <label className="text-xs text-muted-foreground">
                  Senha {cfg?.oracle_senha_set && <span className="text-green-600 ml-1">✓ configurada</span>}
                </label>
                <div className="relative">
                  <Input type={showOraclePwd ? 'text' : 'password'}
                    value={oracleSenha} onChange={e => setOracleSenha(e.target.value)}
                    className="h-8 text-sm pr-8"
                    placeholder={cfg?.oracle_senha_set ? '••••••••' : 'Nova senha'} />
                  <button type="button" onClick={() => setShowOraclePwd(v => !v)}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                    {showOraclePwd ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                  </button>
                </div>
              </div>
            </div>
          </div>

          {/* API Key */}
          <div className="space-y-2">
            <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">API Key do Daemon</p>
            <p className="text-[11px] text-muted-foreground">
              Adicione ao <code className="font-mono text-[10px]">config.yaml</code> como{' '}
              <code className="font-mono text-[10px]">api_key: "..."</code> na seção <code className="font-mono text-[10px]">fbtax</code>.
            </p>
            <div className="flex gap-2 items-end">
              <div className="flex flex-col gap-1 flex-1">
                <div className="relative">
                  <Input readOnly
                    type={showApiKey ? 'text' : 'password'}
                    value={apiKey || ''}
                    className="h-8 text-sm font-mono pr-16"
                    placeholder={apiKey ? undefined : 'Clique em Gerar para criar a chave'} />
                  <div className="absolute right-1 top-1/2 -translate-y-1/2 flex gap-1">
                    <button type="button" onClick={() => setShowApiKey(v => !v)}
                      className="text-muted-foreground hover:text-foreground p-1">
                      {showApiKey ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                    </button>
                    {apiKey && (
                      <button type="button"
                        onClick={() => { navigator.clipboard.writeText(apiKey); toast.success('Chave copiada!'); }}
                        className="text-muted-foreground hover:text-foreground p-1" title="Copiar">
                        <Copy className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                </div>
              </div>
              <Button size="sm" variant="outline" className="h-8 text-xs shrink-0"
                onClick={() => generateApiKeyMutation.mutate()}
                disabled={generateApiKeyMutation.isPending}>
                {generateApiKeyMutation.isPending
                  ? <Loader2 className="h-3 w-3 animate-spin" />
                  : <><KeyRound className="h-3 w-3 mr-1" />{apiKey ? 'Regenerar' : 'Gerar chave'}</>
                }
              </Button>
            </div>
          </div>

          <div className="flex justify-end pt-1 border-t">
            <Button size="sm" onClick={() => saveCredentialsMutation.mutate()}
              disabled={saveCredentialsMutation.isPending}>
              {saveCredentialsMutation.isPending && <Loader2 className="h-3 w-3 mr-1.5 animate-spin" />}
              Salvar credenciais
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
