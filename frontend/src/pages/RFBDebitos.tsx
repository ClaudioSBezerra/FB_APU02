import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { FileText, ChevronLeft, ChevronRight, AlertTriangle, CheckCircle2 } from 'lucide-react';

interface RFBResumo {
  id: string;
  request_id: string;
  data_apuracao: string;
  total_debitos: number;
  valor_cbs_total: number;
  valor_cbs_extinto: number;
  valor_cbs_nao_extinto: number;
  total_corrente: number;
  total_ajuste: number;
  total_extemporaneo: number;
}

interface RFBRequest {
  id: string;
  cnpj_base: string;
  status: string;
  created_at: string;
  resumo?: RFBResumo;
}

interface RFBDebito {
  id: string;
  tipo_apuracao: string;
  modelo_dfe: string;
  numero_dfe: string;
  chave_dfe: string;
  data_dfe_emissao?: string;
  data_apuracao: string;
  ni_emitente: string;
  ni_adquirente: string;
  valor_cbs_total: number;
  valor_cbs_extinto: number;
  valor_cbs_nao_extinto: number;
  situacao_debito: string;
}

interface Pagination {
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
}

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

export default function RFBDebitos() {
  const [requests, setRequests] = useState<RFBRequest[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedRequest, setSelectedRequest] = useState<string | null>(null);
  const [detail, setDetail] = useState<{
    request: RFBRequest;
    resumo: RFBResumo | null;
    debitos: RFBDebito[];
    pagination: Pagination;
  } | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

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
        // Exibe apenas requests concluídos
        setRequests((data.requests || []).filter((r: RFBRequest) => r.status === 'completed'));
      }
    } catch {
      // silent
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchRequests();
  }, [fetchRequests]);

  const fetchDetail = async (requestId: string, page = 1) => {
    setSelectedRequest(requestId);
    setDetailLoading(true);
    try {
      const response = await fetch(`/api/rfb/apuracao/${requestId}?page=${page}`, { headers: getHeaders() });
      if (response.ok) {
        const data = await response.json();
        setDetail({
          request: data.request,
          resumo: data.resumo || null,
          debitos: data.debitos || [],
          pagination: data.pagination || { page: 1, page_size: 500, total: 0, total_pages: 1 },
        });
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro ao carregar débitos' });
    } finally {
      setDetailLoading(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600" />
      </div>
    );
  }

  // ── Tela de detalhe (tabela paginada) ──
  if (selectedRequest && detail) {
    const { request: req, resumo, debitos, pagination } = detail;

    return (
      <div className="max-w-7xl mx-auto px-4 py-6">
        <Button variant="ghost" className="mb-4" onClick={() => { setSelectedRequest(null); setDetail(null); }}>
          <ChevronLeft className="mr-1 h-4 w-4" /> Voltar
        </Button>

        <div className="mb-4">
          <h2 className="text-2xl font-bold flex items-center gap-2">
            Débitos CBS — Detalhamento
            <Badge className="bg-green-100 text-green-700">Concluído</Badge>
          </h2>
          <p className="text-sm text-muted-foreground mt-1">
            CNPJ Base: {formatCNPJBase(req.cnpj_base)} · Importado em: {new Date(req.created_at).toLocaleString('pt-BR')}
          </p>
        </div>

        {/* Cards de resumo */}
        {resumo && (
          <>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-3">
              <Card><CardContent className="pt-4">
                <p className="text-xs text-muted-foreground">Período</p>
                <p className="text-xl font-bold">{formatPeriodo(resumo.data_apuracao)}</p>
              </CardContent></Card>
              <Card><CardContent className="pt-4">
                <p className="text-xs text-muted-foreground">Total de Débitos</p>
                <p className="text-xl font-bold">{formatNumber(resumo.total_debitos)}</p>
              </CardContent></Card>
              <Card><CardContent className="pt-4">
                <p className="text-xs text-muted-foreground">CBS Total</p>
                <p className="text-xl font-bold text-red-600">{formatCurrency(resumo.valor_cbs_total)}</p>
              </CardContent></Card>
              <Card><CardContent className="pt-4">
                <p className="text-xs text-muted-foreground">CBS Não Extinto</p>
                <p className="text-xl font-bold text-orange-600">{formatCurrency(resumo.valor_cbs_nao_extinto)}</p>
              </CardContent></Card>
            </div>
            <div className="grid grid-cols-3 gap-3 mb-4">
              <Card><CardContent className="pt-4 text-center">
                <p className="text-xs text-muted-foreground">Corrente</p>
                <p className="text-lg font-bold">{formatNumber(resumo.total_corrente)}</p>
              </CardContent></Card>
              <Card><CardContent className="pt-4 text-center">
                <p className="text-xs text-muted-foreground">Ajuste</p>
                <p className="text-lg font-bold">{formatNumber(resumo.total_ajuste)}</p>
              </CardContent></Card>
              <Card><CardContent className="pt-4 text-center">
                <p className="text-xs text-muted-foreground">Extemporâneo</p>
                <p className="text-lg font-bold">{formatNumber(resumo.total_extemporaneo)}</p>
              </CardContent></Card>
            </div>
          </>
        )}

        {/* Tabela paginada */}
        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="text-base">
                Débitos CBS — página {pagination.page} de {pagination.total_pages}
                <span className="ml-2 text-sm font-normal text-muted-foreground">
                  ({formatNumber(pagination.total)} registros no total)
                </span>
              </CardTitle>
              <div className="flex items-center gap-2">
                <Button size="sm" variant="outline"
                  disabled={pagination.page <= 1 || detailLoading}
                  onClick={() => fetchDetail(req.id, pagination.page - 1)}>
                  <ChevronLeft className="h-4 w-4" />
                </Button>
                <span className="text-xs text-muted-foreground w-16 text-center">
                  {pagination.page} / {pagination.total_pages}
                </span>
                <Button size="sm" variant="outline"
                  disabled={pagination.page >= pagination.total_pages || detailLoading}
                  onClick={() => fetchDetail(req.id, pagination.page + 1)}>
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            {detailLoading ? (
              <div className="flex justify-center py-8">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600" />
              </div>
            ) : debitos.length > 0 ? (
              <div className="overflow-x-auto">
                <table className="min-w-full divide-y divide-gray-200 text-xs">
                  <thead className="bg-gray-50">
                    <tr>
                      <th className="px-3 py-2 text-left font-semibold">Tipo</th>
                      <th className="px-3 py-2 text-left font-semibold">Mod.</th>
                      <th className="px-3 py-2 text-left font-semibold">Número NF</th>
                      <th className="px-3 py-2 text-left font-semibold">Emitente (NI)</th>
                      <th className="px-3 py-2 text-left font-semibold">Período</th>
                      <th className="px-3 py-2 text-right font-semibold">CBS Total</th>
                      <th className="px-3 py-2 text-right font-semibold">Extinto</th>
                      <th className="px-3 py-2 text-right font-semibold">Não Extinto</th>
                      <th className="px-3 py-2 text-left font-semibold">Situação</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-100">
                    {debitos.map((d) => (
                      <tr key={d.id} className="hover:bg-gray-50">
                        <td className="px-3 py-1.5">
                          <Badge variant="outline" className="text-[10px]">
                            {d.tipo_apuracao === 'corrente' ? 'Corrente' : d.tipo_apuracao === 'ajuste' ? 'Ajuste' : 'Extemp.'}
                          </Badge>
                        </td>
                        <td className="px-3 py-1.5 font-mono">{d.modelo_dfe || '—'}</td>
                        <td className="px-3 py-1.5 font-mono">{d.numero_dfe || '—'}</td>
                        <td className="px-3 py-1.5 font-mono">{d.ni_emitente}</td>
                        <td className="px-3 py-1.5">{formatPeriodo(d.data_apuracao?.replace('-', '').slice(0, 6))}</td>
                        <td className="px-3 py-1.5 text-right">{formatCurrency(d.valor_cbs_total)}</td>
                        <td className="px-3 py-1.5 text-right text-green-600">{formatCurrency(d.valor_cbs_extinto)}</td>
                        <td className="px-3 py-1.5 text-right text-orange-600">{formatCurrency(d.valor_cbs_nao_extinto)}</td>
                        <td className="px-3 py-1.5">{d.situacao_debito}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="py-8 text-center text-muted-foreground text-sm">
                Nenhum débito CBS encontrado para este período.
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    );
  }

  // ── Tela de lista (apenas requests concluídos) ──
  return (
    <div className="max-w-5xl mx-auto px-4 py-6">
      <div className="mb-6">
        <h2 className="text-2xl font-bold flex items-center gap-2">
          <FileText className="h-6 w-6" />
          Débitos CBS — Mês Corrente
        </h2>
        <p className="mt-1 text-sm text-gray-600">
          Visualize os débitos CBS importados da Receita Federal. Clique em uma importação para ver os detalhes.
        </p>
      </div>

      {message && (
        <div className={`mb-4 rounded-md p-4 ${message.type === 'success' ? 'bg-green-50 text-green-800' : 'bg-red-50 text-red-800'}`}>
          <p className="text-sm font-medium">{message.text}</p>
        </div>
      )}

      {requests.length === 0 ? (
        <Card>
          <CardContent className="py-12 text-center text-muted-foreground">
            <FileText className="mx-auto h-12 w-12 mb-3 opacity-30" />
            <p>Nenhuma importação concluída.</p>
            <p className="text-xs mt-1">
              Para importar débitos, acesse <strong>Importação dos débitos CBS</strong>.
            </p>
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-4">
          {requests.map((req) => {
            const { resumo } = req;
            return (
              <div
                key={req.id}
                className="rounded-lg border overflow-hidden cursor-pointer hover:border-blue-300 transition-colors"
                onClick={() => fetchDetail(req.id)}
              >
                <div className="flex items-center justify-between p-4">
                  <div className="flex items-center gap-3">
                    <CheckCircle2 className="h-5 w-5 text-green-600 shrink-0" />
                    <div>
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-sm">CNPJ: {formatCNPJBase(req.cnpj_base)}</span>
                        <Badge className="bg-green-100 text-green-700">Concluído</Badge>
                      </div>
                      <p className="text-xs text-muted-foreground mt-0.5">
                        {new Date(req.created_at).toLocaleString('pt-BR')}
                      </p>
                    </div>
                  </div>
                  <span className="text-xs text-muted-foreground">Ver débitos →</span>
                </div>

                {resumo && (
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
    </div>
  );
}
