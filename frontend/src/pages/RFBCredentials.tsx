import { useState, useEffect } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Globe, Save, Trash2, Pencil, CheckCircle2, XCircle, Clock } from 'lucide-react';

interface RFBCredential {
  id: string;
  company_id: string;
  cnpj_matriz: string;
  client_id: string;
  client_secret: string;
  ambiente: string;
  ativo: boolean;
  agendamento_ativo: boolean;
  horario_agendamento: string; // HH:MM
  created_at: string;
  updated_at: string;
}

function formatCNPJ(value: string): string {
  const digits = value.replace(/\D/g, '').slice(0, 14);
  if (digits.length <= 2) return digits;
  if (digits.length <= 5) return `${digits.slice(0, 2)}.${digits.slice(2)}`;
  if (digits.length <= 8) return `${digits.slice(0, 2)}.${digits.slice(2, 5)}.${digits.slice(5)}`;
  if (digits.length <= 12) return `${digits.slice(0, 2)}.${digits.slice(2, 5)}.${digits.slice(5, 8)}/${digits.slice(8)}`;
  return `${digits.slice(0, 2)}.${digits.slice(2, 5)}.${digits.slice(5, 8)}/${digits.slice(8, 12)}-${digits.slice(12)}`;
}

export default function RFBCredentials() {
  const [credential, setCredential] = useState<RFBCredential | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [editing, setEditing] = useState(false);
  const [savingSchedule, setSavingSchedule] = useState(false);
  const [scheduleData, setScheduleData] = useState({ agendamento_ativo: false, horario_agendamento: '06:00' });
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const [formData, setFormData] = useState({
    cnpj_matriz: '',
    client_id: '',
    client_secret: '',
    ambiente: 'producao',
  });

  const fetchCredential = async () => {
    try {
      const token = localStorage.getItem('token');
      const companyId = localStorage.getItem('companyId');
      const response = await fetch('/api/rfb/credentials', {
        headers: {
          'Authorization': `Bearer ${token}`,
          'X-Company-ID': companyId || '',
        },
      });
      if (response.ok) {
        const data = await response.json();
        if (data.credential) {
          setCredential(data.credential);
          setFormData({
            cnpj_matriz: formatCNPJ(data.credential.cnpj_matriz),
            client_id: data.credential.client_id,
            client_secret: '',
            ambiente: data.credential.ambiente || 'producao_restrita',
          });
          setScheduleData({
            agendamento_ativo: data.credential.agendamento_ativo ?? false,
            horario_agendamento: data.credential.horario_agendamento || '06:00',
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

    try {
      const token = localStorage.getItem('token');
      const companyId = localStorage.getItem('companyId');
      const response = await fetch('/api/rfb/credentials', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`,
          'X-Company-ID': companyId || '',
        },
        body: JSON.stringify({
          cnpj_matriz: formData.cnpj_matriz.replace(/\D/g, ''),
          client_id: formData.client_id,
          client_secret: formData.client_secret,
          ambiente: formData.ambiente,
        }),
      });

      if (response.ok) {
        setMessage({ type: 'success', text: 'Credenciais salvas com sucesso!' });
        fetchCredential();
      } else {
        const text = await response.text();
        setMessage({ type: 'error', text: text || 'Erro ao salvar credenciais' });
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' });
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!confirm('Tem certeza que deseja excluir as credenciais da Receita Federal?')) return;
    setMessage(null);

    try {
      const token = localStorage.getItem('token');
      const companyId = localStorage.getItem('companyId');
      const response = await fetch('/api/rfb/credentials', {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${token}`,
          'X-Company-ID': companyId || '',
        },
      });

      if (response.ok) {
        setMessage({ type: 'success', text: 'Credenciais excluídas com sucesso!' });
        setCredential(null);
        setFormData({ cnpj_matriz: '', client_id: '', client_secret: '', ambiente: 'producao_restrita' });
        setEditing(true);
      } else {
        setMessage({ type: 'error', text: 'Erro ao excluir credenciais' });
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' });
    }
  };

  const handleEdit = () => {
    setEditing(true);
    setFormData({
      cnpj_matriz: credential ? formatCNPJ(credential.cnpj_matriz) : '',
      client_id: credential?.client_id || '',
      client_secret: '',
      ambiente: credential?.ambiente || 'producao_restrita',
    });
  };

  const handleSaveSchedule = async () => {
    setSavingSchedule(true);
    setMessage(null);
    try {
      const token = localStorage.getItem('token');
      const companyId = localStorage.getItem('companyId');
      const response = await fetch('/api/rfb/credentials/agendamento', {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`,
          'X-Company-ID': companyId || '',
        },
        body: JSON.stringify(scheduleData),
      });
      if (response.ok) {
        setMessage({ type: 'success', text: 'Agendamento salvo com sucesso!' });
      } else {
        const text = await response.text();
        setMessage({ type: 'error', text: text || 'Erro ao salvar agendamento' });
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' });
    } finally {
      setSavingSchedule(false);
    }
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
          Credenciais API - Receita Federal
        </h2>
        <p className="mt-2 text-sm text-gray-600">
          Configure as credenciais de acesso à API de Apuração Assistida do portal consumo.tributos.gov.br
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
              <CardTitle className="text-lg">Credenciais CBS/IBS</CardTitle>
              <CardDescription>
                Obtenha suas credenciais no portal da Receita Federal
              </CardDescription>
            </div>
            <div className="flex items-center gap-2">
              {credential ? (
                <span className="inline-flex items-center gap-1 rounded-full bg-green-50 px-3 py-1 text-xs font-medium text-green-700 ring-1 ring-inset ring-green-600/20">
                  <CheckCircle2 className="h-3 w-3" /> Conectado
                </span>
              ) : (
                <span className="inline-flex items-center gap-1 rounded-full bg-gray-50 px-3 py-1 text-xs font-medium text-gray-600 ring-1 ring-inset ring-gray-500/10">
                  <XCircle className="h-3 w-3" /> Não conectado
                </span>
              )}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {editing ? (
            <form onSubmit={handleSave} className="space-y-4">
              <div>
                <Label htmlFor="cnpj_matriz">CNPJ Matriz *</Label>
                <Input
                  id="cnpj_matriz"
                  placeholder="00.000.000/0000-00"
                  required
                  value={formData.cnpj_matriz}
                  onChange={(e) => setFormData({ ...formData, cnpj_matriz: formatCNPJ(e.target.value) })}
                  maxLength={18}
                />
                <p className="text-xs text-muted-foreground mt-1">CNPJ da matriz da empresa (14 dígitos)</p>
              </div>
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
                <Label htmlFor="client_secret">Client Secret *</Label>
                <Input
                  id="client_secret"
                  type="password"
                  placeholder={credential ? 'Informe o novo Client Secret' : 'Informe o Client Secret'}
                  required
                  value={formData.client_secret}
                  onChange={(e) => setFormData({ ...formData, client_secret: e.target.value })}
                />
                {credential && (
                  <p className="text-xs text-muted-foreground mt-1">
                    Atual: {credential.client_secret}
                  </p>
                )}
              </div>
              <div>
                <Label>Ambiente *</Label>
                <div className="mt-2 flex flex-col gap-2">
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="radio"
                      name="ambiente"
                      value="producao_restrita"
                      checked={formData.ambiente === 'producao_restrita'}
                      onChange={() => setFormData({ ...formData, ambiente: 'producao_restrita' })}
                      className="h-4 w-4"
                    />
                    <span className="text-sm font-medium">Produção Restrita</span>
                    <span className="text-xs text-muted-foreground">(credenciais via credencial-api-beta — <code className="bg-muted px-1 rounded">prr-rtc</code>)</span>
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="radio"
                      name="ambiente"
                      value="producao"
                      checked={formData.ambiente === 'producao'}
                      onChange={() => setFormData({ ...formData, ambiente: 'producao' })}
                      className="h-4 w-4"
                    />
                    <span className="text-sm font-medium">Produção</span>
                    <span className="text-xs text-muted-foreground">(acesso irrestrito — <code className="bg-muted px-1 rounded">rtc</code>)</span>
                  </label>
                </div>
              </div>
              <div className="flex gap-2 pt-2">
                <Button type="submit" disabled={saving}>
                  <Save className="mr-2 h-4 w-4" />
                  {saving ? 'Salvando...' : 'Salvar Credenciais'}
                </Button>
                {credential && (
                  <Button type="button" variant="outline" onClick={() => setEditing(false)}>
                    Cancelar
                  </Button>
                )}
              </div>
            </form>
          ) : (
            <div className="space-y-4">
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <Label className="text-muted-foreground">CNPJ Matriz</Label>
                  <p className="text-sm font-medium mt-1">{formatCNPJ(credential?.cnpj_matriz || '')}</p>
                </div>
                <div>
                  <Label className="text-muted-foreground">Client ID</Label>
                  <p className="text-sm font-medium mt-1">{credential?.client_id}</p>
                </div>
                <div>
                  <Label className="text-muted-foreground">Client Secret</Label>
                  <p className="text-sm font-medium mt-1">{credential?.client_secret}</p>
                </div>
                <div>
                  <Label className="text-muted-foreground">Ambiente</Label>
                  <p className="text-sm font-medium mt-1">
                    {credential?.ambiente === 'producao_restrita'
                      ? 'Produção Restrita (prr-rtc)'
                      : 'Produção (rtc)'}
                  </p>
                </div>
                <div>
                  <Label className="text-muted-foreground">Última atualização</Label>
                  <p className="text-sm font-medium mt-1">
                    {credential ? new Date(credential.updated_at).toLocaleString('pt-BR') : '-'}
                  </p>
                </div>
              </div>
              <div className="flex gap-2 pt-2">
                <Button variant="outline" onClick={handleEdit}>
                  <Pencil className="mr-2 h-4 w-4" />
                  Editar
                </Button>
                <Button variant="destructive" onClick={handleDelete}>
                  <Trash2 className="mr-2 h-4 w-4" />
                  Excluir
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {credential && (
        <Card className="mt-6">
          <CardHeader>
            <div className="flex items-center gap-2">
              <Clock className="h-5 w-5 text-muted-foreground" />
              <div>
                <CardTitle className="text-lg">Agendamento Automático</CardTitle>
                <CardDescription>
                  Solicitação diária automática no horário configurado (fuso Brasília)
                </CardDescription>
              </div>
            </div>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <Label>Ativar agendamento</Label>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Usa 1 slot/dia — preserva 1 slot para solicitação manual
                </p>
              </div>
              <Switch
                checked={scheduleData.agendamento_ativo}
                onCheckedChange={(v) => setScheduleData({ ...scheduleData, agendamento_ativo: v })}
              />
            </div>
            <div>
              <Label htmlFor="horario_agendamento">Horário (Brasília)</Label>
              <Input
                id="horario_agendamento"
                type="time"
                value={scheduleData.horario_agendamento}
                onChange={(e) => setScheduleData({ ...scheduleData, horario_agendamento: e.target.value })}
                className="mt-1 w-36"
                disabled={!scheduleData.agendamento_ativo}
              />
            </div>
            <Button onClick={handleSaveSchedule} disabled={savingSchedule} variant="outline">
              <Save className="mr-2 h-4 w-4" />
              {savingSchedule ? 'Salvando...' : 'Salvar Agendamento'}
            </Button>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
