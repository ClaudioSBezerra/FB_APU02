import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Globe, Send, RefreshCw, AlertTriangle, Download, Trash2, RotateCcw, CheckCircle2, CalendarClock } from 'lucide-react';

interface RFBResumo {
  total_debitos: number;
  valor_cbs_total: number;
  valor_cbs_nao_extinto: number;
  total_corrente: number;
  total_ajuste: number;
  data_apuracao: string;
}

interface RFBRequest {
  id: string;
  cnpj_base: string;
  tiquete: string;
  status: string;
  ambiente: string;
  error_code?: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
  resumo?: RFBResumo;
}

const statusConfig: Record<string, { label: string; color: string }> = {
  pending:          { label: 'Pendente',      color: 'bg-gray-100 text-gray-700' },
  requested:        { label: 'Solicitado',    color: 'bg-yellow-100 text-yellow-700' },
  webhook_received: { label: 'Processando',   color: 'bg-blue-100 text-blue-700' },
  downloading:      { label: 'Baixando',      color: 'bg-blue-100 text-blue-700' },
  reprocessing:     { label: 'Reprocessando', color: 'bg-purple-100 text-purple-700' },
  completed:        { label: 'Concluído',     color: 'bg-green-100 text-green-700' },
  error:            { label: 'Erro',          color: 'bg-red-100 text-red-700' },
};

function formatCNPJBase(cnpj: string): string {
  if (cnpj.length === 8) return `${cnpj.slice(0, 2)}.${cnpj.slice(2, 5)}.${cnpj.slice(5)}`;
  return cnpj;
}

function formatPeriodo(p: string): string {
  if (p && p.length === 6) return `${p.slice(4, 6)}/${p.slice(0, 4)}`;
  return p || '—';
}

function formatNumber(n: number): string {
  return new Intl.NumberFormat('pt-BR').format(n);
}

function formatCurrency(value: number): string {
  return new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' }).format(value);
}

export default function RFBApuracao() {
  const [requests, setRequests] = useState<RFBRequest[]>([]);
  const [loading, setLoading] = useState(true);
  const [soliciting, setSoliciting] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const [scheduleInfo, setScheduleInfo] = useState<{ agendamento_ativo: boolean; horario_agendamento: string } | null>(null);

  const getHeaders = () => {
    const token = localStorage.getItem('token');
    const companyId = localStorage.getItem('companyId');
    return {
      'Authorization': `Bearer ${token}`,
      'X-Company-ID': companyId || '',
    };
  };

  const fetchRequests = useCallback(async () => {
    try {
      const response = await fetch('/api/rfb/apuracao/status', { headers: getHeaders() });
      if (response.ok) {
        const data = await response.json();
        setRequests(data.requests || []);
      }
    } catch {
      // silent
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchRequests();
    // Buscar info de agendamento para banner informativo
    const token = localStorage.getItem('token');
    const companyId = localStorage.getItem('companyId');
    fetch('/api/rfb/credentials', {
      headers: { 'Authorization': `Bearer ${token}`, 'X-Company-ID': companyId || '' },
    })
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (data?.credential) {
          setScheduleInfo({
            agendamento_ativo: data.credential.agendamento_ativo ?? false,
            horario_agendamento: data.credential.horario_agendamento || '06:00',
          });
        }
      })
      .catch(() => {});
  }, [fetchRequests]);

  // Poll para requests em andamento
  useEffect(() => {
    const hasPending = requests.some(r =>
      ['pending', 'requested', 'webhook_received', 'downloading', 'reprocessing'].includes(r.status)
    );
    if (!hasPending) return;
    const interval = setInterval(fetchRequests, 10000);
    return () => clearInterval(interval);
  }, [requests, fetchRequests]);

  const handleSolicitar = async () => {
    if (!confirm('Solicitar apuração de débitos CBS à Receita Federal?\n\nLimite: 2 solicitações por dia.')) return;
    setMessage(null);
    setSoliciting(true);
    try {
      const response = await fetch('/api/rfb/apuracao/solicitar', {
        method: 'POST',
        headers: { ...getHeaders(), 'Content-Type': 'application/json' },
      });
      if (response.ok) {
        const data = await response.json();
        setMessage({ type: 'success', text: data.message || 'Solicitação enviada!' });
        fetchRequests();
      } else {
        const text = await response.text();
        setMessage({ type: 'error', text: text || 'Erro ao solicitar apuração' });
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' });
    } finally {
      setSoliciting(false);
    }
  };

  const handleDelete = async (requestId: string) => {
    if (!confirm('Remover este registro do histórico?')) return;
    try {
      await fetch(`/api/rfb/apuracao/${requestId}`, { method: 'DELETE', headers: getHeaders() });
      setRequests(prev => prev.filter(r => r.id !== requestId));
    } catch {
      setMessage({ type: 'error', text: 'Erro ao remover registro' });
    }
  };

  const handleClearErrors = async () => {
    if (!confirm('Limpar todos os registros com erro do histórico?')) return;
    try {
      await fetch('/api/rfb/apuracao/clear-errors', { method: 'DELETE', headers: getHeaders() });
      setRequests(prev => prev.filter(r => r.status !== 'error'));
    } catch {
      setMessage({ type: 'error', text: 'Erro ao limpar logs' });
    }
  };

  const handleReprocess = async (requestId: string) => {
    setMessage(null);
    try {
      const response = await fetch('/api/rfb/apuracao/reprocess', {
        method: 'POST',
        headers: { ...getHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ request_id: requestId }),
      });
      if (response.ok) {
        setMessage({ type: 'success', text: 'Reprocessamento iniciado!' });
        fetchRequests();
      } else {
        const text = await response.text();
        setMessage({ type: 'error', text: text || 'Erro ao reprocessar' });
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' });
    }
  };

  const handleDownloadManual = async (requestId: string) => {
    setMessage(null);
    try {
      const response = await fetch('/api/rfb/apuracao/download', {
        method: 'POST',
        headers: { ...getHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ request_id: requestId }),
      });
      if (response.ok) {
        setMessage({ type: 'success', text: 'Download iniciado! Acompanhe o status.' });
        fetchRequests();
      } else {
        const text = await response.text();
        setMessage({ type: 'error', text: text || 'Erro ao iniciar download' });
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' });
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
    <div className="max-w-5xl mx-auto px-4 py-6">
      <div className="md:flex md:items-center md:justify-between mb-6">
        <div>
          <h2 className="text-2xl font-bold flex items-center gap-2">
            <Globe className="h-6 w-6" />
            Importação dos Débitos CBS
          </h2>
          <p className="mt-1 text-sm text-gray-600">
            Solicite e acompanhe a importação de débitos CBS diretamente da Receita Federal.
          </p>
        </div>
        <div className="mt-4 md:mt-0 flex gap-2">
          {requests.some(r => r.status === 'error') && (
            <Button variant="outline" className="text-red-600 hover:text-red-700 hover:bg-red-50" onClick={handleClearErrors}>
              <Trash2 className="mr-2 h-4 w-4" /> Limpar erros
            </Button>
          )}
          <Button variant="outline" onClick={fetchRequests}>
            <RefreshCw className="mr-2 h-4 w-4" /> Atualizar
          </Button>
          <Button onClick={handleSolicitar} disabled={soliciting}>
            <Send className="mr-2 h-4 w-4" />
            {soliciting ? 'Solicitando...' : 'Solicitar Apuração CBS'}
          </Button>
        </div>
      </div>

      {scheduleInfo?.agendamento_ativo && (
        <div className="mb-4 flex items-center gap-3 rounded-md border border-blue-200 bg-blue-50 px-4 py-3 text-blue-800">
          <CalendarClock className="h-4 w-4 shrink-0" />
          <p className="text-sm">
            <strong>Importação automática ativa</strong> — a solicitação de débitos CBS é enviada automaticamente à Receita Federal todos os dias às <strong>{scheduleInfo.horario_agendamento}</strong> (horário de Brasília).
          </p>
        </div>
      )}

      {message && (
        <div className={`mb-4 rounded-md p-4 ${message.type === 'success' ? 'bg-green-50 text-green-800' : 'bg-red-50 text-red-800'}`}>
          <p className="text-sm font-medium">{message.text}</p>
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Histórico de Solicitações</CardTitle>
          <CardDescription>
            Registro de todas as importações de débitos CBS da RFB (limite: 2 solicitações por dia).
            Para visualizar os débitos importados, acesse <strong>Débitos mês corrente</strong>.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {requests.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">
              <Globe className="mx-auto h-12 w-12 mb-3 opacity-30" />
              <p>Nenhuma solicitação realizada.</p>
              <p className="text-xs mt-1">Clique em "Solicitar Apuração CBS" para começar.</p>
            </div>
          ) : (
            <div className="space-y-4">
              {requests.map((req) => {
                const sc = statusConfig[req.status] || statusConfig.pending;
                const isPending = ['pending', 'requested', 'webhook_received', 'downloading', 'reprocessing'].includes(req.status);
                const { resumo } = req;

                return (
                  <div key={req.id} className="rounded-lg border overflow-hidden">
                    <div className="flex items-center justify-between p-4">
                      <div className="flex items-center gap-3">
                        {isPending && <div className="animate-spin rounded-full h-5 w-5 border-b-2 border-primary shrink-0" />}
                        {req.status === 'completed' && <CheckCircle2 className="h-5 w-5 text-green-600 shrink-0" />}
                        {req.status === 'error' && <AlertTriangle className="h-5 w-5 text-red-500 shrink-0" />}
                        <div>
                          <div className="flex items-center gap-2">
                            <span className="font-medium text-sm">CNPJ: {formatCNPJBase(req.cnpj_base)}</span>
                            <Badge className={sc.color}>{sc.label}</Badge>
                          </div>
                          <p className="text-xs text-muted-foreground mt-0.5">
                            {new Date(req.created_at).toLocaleString('pt-BR')}
                            {req.error_message && (
                              <span className="text-red-600 ml-2">{req.error_message}</span>
                            )}
                          </p>
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        {req.status === 'webhook_received' && (
                          <Button size="sm" variant="outline"
                            onClick={() => handleDownloadManual(req.id)}>
                            <Download className="mr-1 h-3 w-3" /> Download Manual
                          </Button>
                        )}
                        {req.status === 'error' && (
                          <>
                            <Badge variant="destructive" className="text-xs">{req.error_code}</Badge>
                            <Button size="sm" variant="outline" className="text-purple-600 hover:bg-purple-50"
                              onClick={() => handleReprocess(req.id)}>
                              <RotateCcw className="mr-1 h-3 w-3" /> Reprocessar
                            </Button>
                            <Button size="sm" variant="ghost" className="text-red-500 hover:text-red-700 hover:bg-red-50 px-2"
                              onClick={() => handleDelete(req.id)}>
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                          </>
                        )}
                      </div>
                    </div>

                    {/* Resumo inline para requests concluídos */}
                    {req.status === 'completed' && resumo && (
                      <div className="border-t bg-gray-50 px-4 py-3 grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
                        <div>
                          <span className="text-xs text-muted-foreground block">Período</span>
                          <span className="font-semibold">{formatPeriodo(resumo.data_apuracao)}</span>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground block">Total de débitos</span>
                          <span className="font-semibold">{formatNumber(resumo.total_debitos)}</span>
                          <span className="text-xs text-muted-foreground ml-1">
                            ({formatNumber(resumo.total_corrente)} corr. + {formatNumber(resumo.total_ajuste)} ajuste)
                          </span>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground block">CBS Total</span>
                          <span className="font-semibold text-red-600">{formatCurrency(resumo.valor_cbs_total)}</span>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground block">CBS Não Extinto</span>
                          <span className="font-semibold text-orange-600">{formatCurrency(resumo.valor_cbs_nao_extinto)}</span>
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
