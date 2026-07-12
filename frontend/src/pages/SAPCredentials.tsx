import { useState, useEffect } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Globe, Save, CheckCircle2, XCircle, PlugZap } from 'lucide-react';

interface SAPCredential {
  id: string;
  company_id: string;
  client_id: string;
  client_secret_set: boolean;
  base_url: string;
  bukrs_list: string[];
  ativo: boolean;
  created_at: string;
  updated_at: string;
}

export default function SAPCredentials() {
  const [credential, setCredential] = useState<SAPCredential | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [editing, setEditing] = useState(false);
  const [testingConnection, setTestingConnection] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const [formData, setFormData] = useState({
    client_id: '',
    client_secret: '',
    base_url: '',
    bukrs_text: '', // um código BUKRS por linha
  });

  const fetchCredential = async () => {
    try {
      const response = await fetch('/api/sap/credentials');
      if (response.ok) {
        const data = await response.json();
        if (data.credential) {
          setCredential(data.credential);
          setFormData({
            client_id: data.credential.client_id,
            client_secret: '',
            base_url: data.credential.base_url || '',
            bukrs_text: (data.credential.bukrs_list || []).join('\n'),
          });
          setEditing(false);
        } else {
          setCredential(null);
          setEditing(true);
        }
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro ao carregar credenciais' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchCredential();
  }, []);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setMessage(null);
    setSaving(true);

    // Aceita um BUKRS por linha ou separados por vírgula (ou ambos misturados)
    const bukrsList = formData.bukrs_text
      .split(/[\n,]/)
      .map((b) => b.trim())
      .filter((b) => b.length > 0);

    try {
      const response = await fetch('/api/sap/credentials', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          client_id: formData.client_id,
          client_secret: formData.client_secret,
          base_url: formData.base_url,
          bukrs_list: bukrsList,
        }),
      });

      if (response.ok) {
        setMessage({ type: 'success', text: 'Credenciais SAP salvas com sucesso!' });
        fetchCredential();
      } else {
        const text = await response.text();
        let errorMessage = text || 'Erro ao salvar credenciais';
        try {
          const parsed = JSON.parse(text);
          if (parsed?.error) errorMessage = parsed.error;
        } catch {
          // resposta não era JSON (ex.: 401/405 em texto plano) — usa o texto bruto
        }
        setMessage({ type: 'error', text: errorMessage });
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' });
    } finally {
      setSaving(false);
    }
  };

  const handleTestConnection = async () => {
    if (testingConnection) return; // evita disparo duplicado por clique duplo antes do re-render
    setMessage(null);
    setTestingConnection(true);
    try {
      const response = await fetch('/api/sap/credentials/test', { method: 'POST' });
      const text = await response.text();
      let parsed: { message?: string; error?: string } = {};
      try {
        parsed = JSON.parse(text);
      } catch {
        // resposta não era JSON — ignora, cai no fallback abaixo
      }
      if (response.ok) {
        setMessage({ type: 'success', text: parsed.message || 'Conexão bem-sucedida' });
      } else {
        setMessage({ type: 'error', text: parsed.error || text || 'Falha ao testar conexão' });
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' });
    } finally {
      setTestingConnection(false);
    }
  };

  const handleEdit = () => {
    setEditing(true);
    setFormData({
      client_id: credential?.client_id || '',
      client_secret: '',
      base_url: credential?.base_url || '',
      bukrs_text: (credential?.bukrs_list || []).join('\n'),
    });
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary"></div>
      </div>
    );
  }

  return (
    <div className="max-w-2xl mx-auto px-4 py-8">
      <div className="mb-6">
        <h2 className="text-2xl font-bold leading-7 text-gray-900 flex items-center gap-2">
          <Globe className="h-6 w-6" />
          Credenciais API - SAP S/4HANA
        </h2>
        <p className="mt-2 text-sm text-gray-600">
          Configure as credenciais OAuth2 e os códigos de empresa (BUKRS) para a sincronização automática de pagamentos SAP
        </p>
      </div>

      {message && (
        <div className={`mb-4 rounded-md p-4 ${
          message.type === 'success' ? 'bg-green-50 text-green-800' : 'bg-red-50 text-red-800'
        }`}>
          <p className="text-sm font-medium">{message.text}</p>
        </div>
      )}

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="text-lg">Credenciais SAP</CardTitle>
              <CardDescription>
                Client ID/Secret OAuth2 e códigos de empresa SAP (BUKRS)
              </CardDescription>
            </div>
            <div className="flex items-center gap-2">
              {credential ? (
                <span className="inline-flex items-center gap-1 rounded-full bg-green-50 px-3 py-1 text-xs font-medium text-green-700 ring-1 ring-inset ring-green-600/20">
                  <CheckCircle2 className="h-3 w-3" /> Configurado
                </span>
              ) : (
                <span className="inline-flex items-center gap-1 rounded-full bg-gray-50 px-3 py-1 text-xs font-medium text-gray-600 ring-1 ring-inset ring-gray-500/10">
                  <XCircle className="h-3 w-3" /> Não configurado
                </span>
              )}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {editing ? (
            <form onSubmit={handleSave} className="space-y-4">
              <div>
                <Label htmlFor="client_id">Client ID *</Label>
                <Input
                  id="client_id"
                  placeholder="Informe o Client ID"
                  required
                  value={formData.client_id}
                  onChange={(e) => setFormData({ ...formData, client_id: e.target.value })}
                />
              </div>
              <div>
                <Label htmlFor="client_secret">Client Secret {credential ? '' : '*'}</Label>
                <Input
                  id="client_secret"
                  type="password"
                  placeholder={credential ? 'Deixe em branco para manter o atual' : 'Informe o Client Secret'}
                  required={!credential}
                  value={formData.client_secret}
                  onChange={(e) => setFormData({ ...formData, client_secret: e.target.value })}
                />
                {credential?.client_secret_set && (
                  <p className="text-xs text-muted-foreground mt-1">
                    Já existe um Client Secret configurado. Por segurança, ele nunca é exibido — deixe o campo em branco para mantê-lo.
                  </p>
                )}
              </div>
              <div>
                <Label htmlFor="base_url">Base URL</Label>
                <Input
                  id="base_url"
                  placeholder="https://api.exemplo-sap.com"
                  value={formData.base_url}
                  onChange={(e) => setFormData({ ...formData, base_url: e.target.value })}
                />
              </div>
              <div>
                <Label htmlFor="bukrs_text">Códigos de Empresa SAP (BUKRS) *</Label>
                <textarea
                  id="bukrs_text"
                  className="flex min-h-20 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-sm"
                  placeholder={'Um código por linha, ex.:\n1000\n2000'}
                  required
                  value={formData.bukrs_text}
                  onChange={(e) => setFormData({ ...formData, bukrs_text: e.target.value })}
                />
                <p className="text-xs text-muted-foreground mt-1">Um código BUKRS por linha (ou separados por vírgula). Ao menos um é obrigatório.</p>
              </div>
              <div className="flex gap-2">
                <Button type="submit" disabled={saving}>
                  <Save className="h-4 w-4 mr-2" />
                  {saving ? 'Salvando...' : 'Salvar'}
                </Button>
                {credential && (
                  <Button type="button" variant="outline" onClick={() => setEditing(false)}>
                    Cancelar
                  </Button>
                )}
                <Button type="button" variant="outline" onClick={handleTestConnection} disabled={testingConnection}>
                  <PlugZap className="h-4 w-4 mr-2" />
                  {testingConnection ? 'Testando...' : 'Testar Conexão'}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                "Testar Conexão" verifica a credencial já salva no servidor — se você ainda não salvou nenhuma, o teste vai informar isso.
              </p>
            </form>
          ) : (
            <div className="space-y-3">
              <div>
                <span className="text-sm font-medium text-gray-500">Client ID</span>
                <p className="text-sm">{credential?.client_id}</p>
              </div>
              <div>
                <span className="text-sm font-medium text-gray-500">Base URL</span>
                <p className="text-sm">{credential?.base_url || '—'}</p>
              </div>
              <div>
                <span className="text-sm font-medium text-gray-500">Códigos BUKRS</span>
                <p className="text-sm">{(credential?.bukrs_list || []).join(', ') || '—'}</p>
              </div>
              <div className="flex gap-2">
                <Button onClick={handleEdit} variant="outline">
                  Editar
                </Button>
                <Button onClick={handleTestConnection} variant="outline" disabled={testingConnection}>
                  <PlugZap className="h-4 w-4 mr-2" />
                  {testingConnection ? 'Testando...' : 'Testar Conexão'}
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
