import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { TrendingUp, RefreshCw, Info, ChevronLeft, ChevronRight, AlertTriangle, AlertCircle, CheckCircle2, RotateCcw } from 'lucide-react';
import { toast } from 'sonner';
import { Link } from 'react-router-dom';

interface SituacaoTotais {
  situacao: string;
  quantidade: number;
  valor_total: number;
}

interface CreditoRequestItem {
  id: string;
  cnpj_base: string;
  status: string;
  error_code?: string;
  error_message?: string;
  created_at: string;
  has_raw_json: boolean;
}

const REQUEST_STATUS_LABELS: Record<string, string> = {
  pending: 'Pendente',
  requested: 'Solicitado',
  webhook_received: 'Processando',
  downloading: 'Baixando',
  reprocessing: 'Reprocessando',
  completed: 'Concluído',
};

interface CreditoItem {
  id: string;
  request_id: string;
  tipo_apuracao: string;
  modelo_dfe: string;
  numero_dfe: string;
  chave_dfe: string;
  data_dfe_emissao: string | null;
  data_apuracao: string;
  ni_emitente: string;
  ni_adquirente: string;
  valor_cbs_total: number;
  valor_cbs_extinto: number;
  valor_cbs_nao_extinto: number;
  situacao_credito: string;
  formas_extincao: string;
  created_at: string;
}

function formatCurrency(value: number): string {
  return new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' }).format(value);
}

function formatCNPJBase(cnpj: string): string {
  if (cnpj.length === 8) return `${cnpj.slice(0, 2)}.${cnpj.slice(2, 5)}.${cnpj.slice(5)}`;
  return cnpj;
}

function formatDate(d: string | null): string {
  if (!d) return '—';
  return new Date(d + 'T00:00:00').toLocaleDateString('pt-BR');
}

function formatPeriodo(p: string): string {
  if (p && p.length === 6) return `${p.slice(4, 6)}/${p.slice(0, 4)}`;
  return p || '—';
}

function formatNI(ni: string): string {
  if (!ni) return '—';
  const d = ni.replace(/\D/g, '');
  if (d.length === 14) return d.replace(/(\d{2})(\d{3})(\d{3})(\d{4})(\d{2})/, '$1.$2.$3/$4-$5');
  if (d.length === 11) return d.replace(/(\d{3})(\d{3})(\d{3})(\d{2})/, '$1.$2.$3-$4');
  return ni;
}

const SITUACAO_LABELS: Record<string, string> = {
  A_APROPRIAR: 'A Apropriar',
  APROPRIADO: 'Apropriado',
  COMPENSADO: 'Compensado',
};

const SITUACAO_COLORS: Record<string, string> = {
  A_APROPRIAR: 'bg-yellow-100 text-yellow-800 border-yellow-300',
  APROPRIADO: 'bg-green-100 text-green-800 border-green-300',
  COMPENSADO: 'bg-blue-100 text-blue-800 border-blue-300',
};

const CARD_COLORS: Record<string, { bg: string; title: string; value: string; count: string }> = {
  A_APROPRIAR: { bg: 'border-yellow-200 bg-yellow-50', title: 'text-yellow-800', value: 'text-yellow-900', count: 'text-yellow-700' },
  APROPRIADO:  { bg: 'border-green-200 bg-green-50',  title: 'text-green-800',  value: 'text-green-900',  count: 'text-green-700'  },
  COMPENSADO:  { bg: 'border-blue-200 bg-blue-50',    title: 'text-blue-800',   value: 'text-blue-900',   count: 'text-blue-700'   },
};

function SituacaoBadge({ situacao }: { situacao: string }) {
  const classes = SITUACAO_COLORS[situacao] ?? 'bg-gray-100 text-gray-700 border-gray-300';
  const label = SITUACAO_LABELS[situacao] ?? situacao;
  return (
    <span className={`inline-block px-2 py-0.5 rounded border text-xs font-medium ${classes}`}>
      {label}
    </span>
  );
}

export default function RFBCreditosCBS() {
  const [creditos, setCreditos] = useState<CreditoItem[]>([]);
  const [totais, setTotais] = useState<SituacaoTotais[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [filterSituacao, setFilterSituacao] = useState('');
  const [filterPeriodo, setFilterPeriodo] = useState('');
  const [requests, setRequests] = useState<CreditoRequestItem[]>([]);
  const [requestsLoading, setRequestsLoading] = useState(true);
  const [resoliciting, setResoliciting] = useState<string | null>(null);
  const [reprocessing, setReprocessing] = useState<string | null>(null);

  const fetchRequests = useCallback(async () => {
    setRequestsLoading(true);
    try {
      const response = await fetch('/api/rfb/creditos/status');
      if (response.ok) {
        const data = await response.json();
        setRequests(data.requests || []);
      }
    } catch {
      // silent
    } finally {
      setRequestsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchRequests();
  }, [fetchRequests]);

  const handleResolicitar = async (requestId: string) => {
    setResoliciting(requestId);
    try {
      const response = await fetch('/api/rfb/apuracao/resolicitar', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ request_id: requestId }),
      });
      if (!response.ok) {
        const text = await response.text();
        toast.error(text || 'Erro ao resolicitar');
        return;
      }
      toast.success('Solicitação de créditos reenviada à Receita Federal.');
      fetchRequests();
    } catch {
      toast.error('Erro de conexão');
    } finally {
      setResoliciting(null);
    }
  };

  const handleReprocess = async (requestId: string) => {
    setReprocessing(requestId);
    try {
      const response = await fetch('/api/rfb/apuracao/reprocess', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ request_id: requestId }),
      });
      if (!response.ok) {
        const text = await response.text();
        toast.error(text || 'Erro ao reprocessar');
        return;
      }
      toast.success('Reprocessamento iniciado.');
      fetchRequests();
    } catch {
      toast.error('Erro de conexão');
    } finally {
      setReprocessing(null);
    }
  };

  const fetchCreditos = useCallback(async (p: number, sit: string, per: string) => {
    setLoading(true);
    try {
      const params = new URLSearchParams({ page: String(p) });
      if (sit) params.set('situacao', sit);
      if (per) params.set('periodo', per);

      const response = await fetch(`/api/rfb/creditos/lista?${params}`);
      if (response.ok) {
        const data = await response.json();
        setCreditos(data.creditos || []);
        setTotal(data.total || 0);
        setTotais(data.totais || []);
      }
    } catch {
      // silent
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchCreditos(page, filterSituacao, filterPeriodo);
  }, [fetchCreditos, page, filterSituacao, filterPeriodo]);

  const handleSituacaoFilter = (sit: string) => {
    setFilterSituacao(prev => prev === sit ? '' : sit);
    setPage(1);
  };

  const totalPages = Math.ceil(total / 50);

  const totalGeral = totais.reduce((acc, t) => ({ qty: acc.qty + t.quantidade, val: acc.val + t.valor_total }), { qty: 0, val: 0 });

  return (
    <div className="max-w-6xl mx-auto px-4 py-6">
      {/* Header */}
      <div className="md:flex md:items-center md:justify-between mb-6">
        <div>
          <h2 className="text-2xl font-bold flex items-center gap-2">
            <TrendingUp className="h-6 w-6" />
            Créditos CBS
          </h2>
          <p className="mt-1 text-sm text-gray-600">
            Créditos CBS extraídos das importações de débitos — apuração assistida RFB.
          </p>
        </div>
        <Button variant="outline" onClick={() => fetchCreditos(page, filterSituacao, filterPeriodo)}>
          <RefreshCw className="mr-2 h-4 w-4" /> Atualizar
        </Button>
      </div>

      {/* Art. 48 notice */}
      <div className="mb-5 flex items-start gap-3 rounded-md border border-blue-200 bg-blue-50 px-4 py-3 text-blue-800">
        <Info className="h-4 w-4 shrink-0 mt-0.5" />
        <p className="text-sm">
          <strong>Art. 48 — LC 214/2025:</strong> Em 2026, o split payment ainda não é obrigatório.
          Os créditos são registrados a partir do destaque na NF-e sem aguardar a extinção do débito do fornecedor.
          Para solicitar uma nova importação, acesse{' '}
          <Link to="/rfb/apuracao" className="font-semibold underline hover:no-underline">
            Importar Movimento
          </Link>.
        </p>
      </div>

      {/* Solicitações de Créditos CBS */}
      <Card className="mb-6">
        <CardHeader className="pb-2">
          <CardTitle className="text-base">Solicitações de Créditos CBS</CardTitle>
          <CardDescription>Status das tentativas de obter créditos junto à Receita Federal.</CardDescription>
        </CardHeader>
        <CardContent>
          {requestsLoading ? (
            <div className="flex items-center justify-center h-16">
              <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-primary"></div>
            </div>
          ) : requests.length === 0 ? (
            <p className="text-sm text-muted-foreground py-2">Nenhuma solicitação de crédito ainda.</p>
          ) : (
            <div className="space-y-2">
              {requests.map(req => {
                const isAlerta = req.status === 'error' && req.error_code === 'ENDPOINT_INDISPONIVEL';
                const isErro = req.status === 'error' && !isAlerta;
                return (
                  <div key={req.id} className="flex items-center justify-between gap-3 rounded-md border p-3">
                    <div className="flex items-center gap-2 min-w-0">
                      {req.status === 'completed' && <CheckCircle2 className="h-4 w-4 text-green-600 shrink-0" />}
                      {isErro && <AlertTriangle className="h-4 w-4 text-red-500 shrink-0" />}
                      {isAlerta && <AlertCircle className="h-4 w-4 text-amber-500 shrink-0" />}
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-medium">CNPJ: {formatCNPJBase(req.cnpj_base)}</span>
                          {isAlerta
                            ? <span className="inline-block px-2 py-0.5 rounded border text-xs font-medium bg-amber-100 text-amber-800 border-amber-300">Alerta</span>
                            : isErro
                              ? <span className="inline-block px-2 py-0.5 rounded border text-xs font-medium bg-red-100 text-red-700 border-red-300">Erro</span>
                              : req.status === 'completed'
                                ? <span className="inline-block px-2 py-0.5 rounded border text-xs font-medium bg-green-100 text-green-700 border-green-300">Concluído</span>
                                : <span className="inline-block px-2 py-0.5 rounded border text-xs font-medium bg-gray-100 text-gray-700 border-gray-300">
                                    {REQUEST_STATUS_LABELS[req.status] || req.status}
                                  </span>}
                        </div>
                        {req.error_message && (
                          <p className={`text-xs mt-0.5 truncate ${isAlerta ? 'text-amber-700' : 'text-red-600'}`}>
                            {req.error_message}
                          </p>
                        )}
                      </div>
                    </div>
                    {req.status === 'error' && !req.has_raw_json && (
                      <button
                        onClick={() => handleResolicitar(req.id)}
                        disabled={resoliciting === req.id}
                        className="shrink-0 inline-flex items-center px-2 py-1 text-xs font-medium rounded bg-blue-100 text-blue-700 hover:bg-blue-200 disabled:opacity-50"
                      >
                        <RefreshCw className="mr-1 h-3 w-3" />
                        {resoliciting === req.id ? 'Reenviando...' : 'Re-solicitar'}
                      </button>
                    )}
                    {req.status === 'error' && req.has_raw_json && (
                      <button
                        onClick={() => handleReprocess(req.id)}
                        disabled={reprocessing === req.id}
                        className="shrink-0 inline-flex items-center px-2 py-1 text-xs font-medium rounded bg-purple-100 text-purple-700 hover:bg-purple-200 disabled:opacity-50"
                      >
                        <RotateCcw className="mr-1 h-3 w-3" />
                        {reprocessing === req.id ? 'Reprocessando...' : 'Reprocessar'}
                      </button>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Summary cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-6">
        {['A_APROPRIAR', 'APROPRIADO', 'COMPENSADO'].map((sit) => {
          const t = totais.find(x => x.situacao === sit);
          const c = CARD_COLORS[sit];
          const active = filterSituacao === sit;
          return (
            <button
              key={sit}
              onClick={() => handleSituacaoFilter(sit)}
              className={`text-left rounded-lg border p-4 transition-all ${c.bg} ${active ? 'ring-2 ring-offset-1 ring-gray-400' : 'hover:shadow-sm'}`}
            >
              <div className={`text-xs font-semibold uppercase tracking-wide mb-1 ${c.title}`}>
                {SITUACAO_LABELS[sit]}
              </div>
              <div className={`text-xl font-bold ${c.value}`}>
                {t ? formatCurrency(t.valor_total) : 'R$ 0,00'}
              </div>
              <div className={`text-xs mt-1 ${c.count}`}>
                {t ? t.quantidade : 0} {t?.quantidade === 1 ? 'crédito' : 'créditos'}
              </div>
            </button>
          );
        })}
      </div>

      {/* Filter bar */}
      <div className="flex flex-wrap items-center gap-3 mb-4">
        <div className="flex items-center gap-2">
          <label className="text-sm text-gray-600">Período:</label>
          <input
            type="text"
            placeholder="AAAAMM"
            maxLength={6}
            value={filterPeriodo}
            onChange={e => { setFilterPeriodo(e.target.value); setPage(1); }}
            className="border rounded px-2 py-1 text-sm w-28 focus:outline-none focus:ring-1 focus:ring-primary"
          />
        </div>
        {filterSituacao && (
          <button
            onClick={() => { setFilterSituacao(''); setPage(1); }}
            className="text-xs text-gray-500 hover:text-gray-800 underline"
          >
            Limpar filtro: {SITUACAO_LABELS[filterSituacao]}
          </button>
        )}
        <span className="ml-auto text-sm text-muted-foreground">
          {total} registro{total !== 1 ? 's' : ''}
          {totalGeral.qty > 0 && ` · Total: ${formatCurrency(totalGeral.val)}`}
        </span>
      </div>

      {/* Table */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-base">Créditos Individuais</CardTitle>
          <CardDescription>Clique em uma situação acima para filtrar.</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {loading ? (
            <div className="flex items-center justify-center h-40">
              <div className="animate-spin rounded-full h-10 w-10 border-b-2 border-primary"></div>
            </div>
          ) : creditos.length === 0 ? (
            <div className="py-12 text-center text-muted-foreground">
              <TrendingUp className="mx-auto h-10 w-10 mb-3 opacity-30" />
              <p className="text-sm">Nenhum crédito CBS encontrado.</p>
              <p className="text-xs mt-1">
                Os créditos aparecem após concluir uma{' '}
                <Link to="/rfb/apuracao" className="text-primary hover:underline">
                  importação de débitos
                </Link>.
              </p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b bg-gray-50 text-xs text-muted-foreground">
                    <th className="px-4 py-2 text-left font-medium">Emissão</th>
                    <th className="px-4 py-2 text-left font-medium">Período</th>
                    <th className="px-4 py-2 text-left font-medium">Modelo / Nº</th>
                    <th className="px-4 py-2 text-left font-medium">Emitente</th>
                    <th className="px-4 py-2 text-right font-medium">CBS Total</th>
                    <th className="px-4 py-2 text-right font-medium">Não Extinto</th>
                    <th className="px-4 py-2 text-center font-medium">Situação</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {creditos.map(c => (
                    <tr key={c.id} className="hover:bg-gray-50 transition-colors">
                      <td className="px-4 py-2 whitespace-nowrap">{formatDate(c.data_dfe_emissao)}</td>
                      <td className="px-4 py-2 whitespace-nowrap">{formatPeriodo(c.data_apuracao)}</td>
                      <td className="px-4 py-2">
                        <span className="font-mono text-xs">
                          {c.modelo_dfe || '—'}
                          {c.numero_dfe ? ` · ${c.numero_dfe}` : ''}
                        </span>
                      </td>
                      <td className="px-4 py-2 whitespace-nowrap">
                        <span title={c.ni_emitente} className="font-mono text-xs">
                          {formatNI(c.ni_emitente)}
                        </span>
                      </td>
                      <td className="px-4 py-2 text-right font-medium text-green-700">
                        {formatCurrency(c.valor_cbs_total)}
                      </td>
                      <td className="px-4 py-2 text-right text-blue-700">
                        {formatCurrency(c.valor_cbs_nao_extinto)}
                      </td>
                      <td className="px-4 py-2 text-center">
                        <SituacaoBadge situacao={c.situacao_credito} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between px-4 py-3 border-t">
              <Button
                variant="outline"
                size="sm"
                disabled={page <= 1}
                onClick={() => setPage(p => p - 1)}
              >
                <ChevronLeft className="h-4 w-4" />
                Anterior
              </Button>
              <span className="text-xs text-muted-foreground">
                Página {page} de {totalPages}
              </span>
              <Button
                variant="outline"
                size="sm"
                disabled={page >= totalPages}
                onClick={() => setPage(p => p + 1)}
              >
                Próxima
                <ChevronRight className="h-4 w-4" />
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
