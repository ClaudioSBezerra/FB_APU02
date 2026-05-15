import { useState, useEffect, useCallback } from 'react';
import * as XLSX from 'xlsx';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { ChevronDown, ChevronRight, Download, RefreshCw } from 'lucide-react';
import { useAuth } from '@/contexts/AuthContext';

// ─── Interfaces ───────────────────────────────────────────────────────────────

interface ConciliacaoSumario {
  total_pago: number;
  total_notas: number;
  cbs_extinto: number;
  notas_extinto: number;
  cbs_pendente: number;
  notas_pendente: number;
  notas_sem_dados: number;
}

interface ConciliacaoItem {
  chave_doc: string;
  tipo_doc: string;
  forn_cnpj: string;
  forn_nome: string | null;
  num_parcelas: number;
  total_pago: number;
  primeira_parcela: string;
  ultima_parcela: string;
  valor_nota: number;
  valor_cbs_nota: number;
  valor_ibs_nota: number;
  valor_cbs_nao_extinto: number;
  situacao_credito: string | null;
  status_conciliacao: string;
}

interface Parcela {
  id: number;
  data_pagamento: string;
  valor_pagamento: number;
  num_doc_pagamento: string | null;
  descricao: string | null;
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function formatCurrency(value: number): string {
  return new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' }).format(value);
}

function formatDate(d: string | null | undefined): string {
  if (!d) return '—';
  return new Date(d + 'T00:00:00').toLocaleDateString('pt-BR');
}

function formatNI(ni: string): string {
  if (!ni) return '—';
  const d = ni.replace(/\D/g, '');
  if (d.length === 14) return d.replace(/(\d{2})(\d{3})(\d{3})(\d{4})(\d{2})/, '$1.$2.$3/$4-$5');
  if (d.length === 11) return d.replace(/(\d{3})(\d{3})(\d{3})(\d{2})/, '$1.$2.$3-$4');
  return ni;
}

function formatChaveTruncated(c: string): string {
  if (!c || c.length <= 8) return c;
  return c.slice(0, 4) + '…' + c.slice(-4);
}

function todayStr(): string {
  return new Date().toISOString().slice(0, 10).replace(/-/g, '');
}

// ─── StatusBadge ─────────────────────────────────────────────────────────────

const STATUS_CLASSES: Record<string, string> = {
  pendente:  'bg-red-100 text-red-800 border-red-300',
  extinto:   'bg-green-100 text-green-800 border-green-300',
  sem_dados: 'bg-gray-100 text-gray-600 border-gray-300',
};

const STATUS_LABELS: Record<string, string> = {
  pendente:  'Pendente',
  extinto:   'Extinto',
  sem_dados: 'Sem dados RFB',
};

function StatusBadge({ status }: { status: string }) {
  const cls = STATUS_CLASSES[status] ?? 'bg-gray-100 text-gray-600 border-gray-300';
  const label = STATUS_LABELS[status] ?? status;
  return (
    <span className={`inline-block px-2 py-0.5 rounded border text-xs font-medium ${cls}`}>
      {label}
    </span>
  );
}

// ─── Page component ───────────────────────────────────────────────────────────

export default function RFBPagamentosFornecedores() {
  const { companyId } = useAuth();

  // State
  const [items, setItems] = useState<ConciliacaoItem[]>([]);
  const [sumario, setSumario] = useState<ConciliacaoSumario | null>(null);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const pageSize = 50;

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Filters
  const [filterMesAno, setFilterMesAno] = useState('');
  const [filterStatus, setFilterStatus] = useState('');
  const [filterForn, setFilterForn] = useState('');

  // Drill-down state
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [parcelas, setParcelas] = useState<Record<string, Parcela[]>>({});
  const [parcelasLoading, setParcelasLoading] = useState<Set<string>>(new Set());

  // Export
  const [exporting, setExporting] = useState(false);

  // ── Fetch main data ─────────────────────────────────────────────────────────
  const fetchConciliacao = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
      if (filterMesAno) params.set('mes_ano', filterMesAno);
      if (filterStatus) params.set('status', filterStatus);
      if (filterForn)   params.set('forn_cnpj', filterForn);

      const response = await fetch(`/api/rfb/pagamentos-fornecedores?${params}`);
      if (!response.ok) {
        const msg = await response.text();
        throw new Error(msg || 'Erro ao buscar dados');
      }
      const data = await response.json();
      setItems(data.items ?? []);
      setSumario(data.sumario ?? null);
      setTotal(data.total ?? 0);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Erro desconhecido');
    } finally {
      setLoading(false);
    }
  }, [page, filterMesAno, filterStatus, filterForn, companyId]);

  useEffect(() => {
    fetchConciliacao();
  }, [fetchConciliacao]);

  // ── Row expansion ───────────────────────────────────────────────────────────
  const toggleExpand = useCallback(async (chaveDoc: string) => {
    setExpanded(prev => {
      const next = new Set(prev);
      if (next.has(chaveDoc)) {
        next.delete(chaveDoc);
      } else {
        next.add(chaveDoc);
      }
      return next;
    });

    // Fetch parcelas if not cached
    if (!parcelas[chaveDoc]) {
      setParcelasLoading(prev => new Set(prev).add(chaveDoc));
      try {
        const params = new URLSearchParams({ chave_doc: chaveDoc, page_size: '200' });
        const res = await fetch(`/api/pagamentos-fornecedores?${params}`);
        if (res.ok) {
          const data = await res.json();
          setParcelas(prev => ({ ...prev, [chaveDoc]: data.items ?? data ?? [] }));
        }
      } catch {
        // silent — user can re-click
      } finally {
        setParcelasLoading(prev => {
          const next = new Set(prev);
          next.delete(chaveDoc);
          return next;
        });
      }
    }
  }, [parcelas]);

  // ── Apply filters ────────────────────────────────────────────────────────────
  const applyFilters = () => {
    setPage(1);
    // useEffect will re-run because filterMesAno/filterStatus/filterForn are in deps
  };

  // ── Excel export ─────────────────────────────────────────────────────────────
  const exportExcel = async () => {
    setExporting(true);
    try {
      const params = new URLSearchParams({ page: '1', page_size: '9999' });
      if (filterMesAno) params.set('mes_ano', filterMesAno);
      if (filterStatus) params.set('status', filterStatus);
      if (filterForn)   params.set('forn_cnpj', filterForn);

      const res = await fetch(`/api/rfb/pagamentos-fornecedores?${params}`);
      if (!res.ok) throw new Error('Erro ao exportar');
      const data = await res.json();
      const exportItems: ConciliacaoItem[] = data.items ?? [];

      const rows = exportItems.map(it => ({
        'Chave Doc':          it.chave_doc,
        'Tipo':               it.tipo_doc,
        'CNPJ Fornecedor':    formatNI(it.forn_cnpj),
        'Nome Fornecedor':    it.forn_nome ?? '',
        'Parcelas':           it.num_parcelas,
        'Total Pago':         it.total_pago,
        'Primeira Parcela':   it.primeira_parcela,
        'Última Parcela':     it.ultima_parcela,
        'Valor Nota':         it.valor_nota,
        'Valor CBS Nota':     it.valor_cbs_nota,
        'Valor IBS Nota':     it.valor_ibs_nota,
        'CBS Não Extinto':    it.valor_cbs_nao_extinto,
        'Situação RFB':       it.situacao_credito ?? '',
        'Status Conciliação': STATUS_LABELS[it.status_conciliacao] ?? it.status_conciliacao,
      }));

      const ws = XLSX.utils.json_to_sheet(rows);
      const wb = XLSX.utils.book_new();
      XLSX.utils.book_append_sheet(wb, ws, 'Conciliação');
      XLSX.writeFile(wb, `conciliacao-cbs-${todayStr()}.xlsx`);
    } catch (err: unknown) {
      console.error('Erro ao exportar Excel:', err);
    } finally {
      setExporting(false);
    }
  };

  // ── Computed ─────────────────────────────────────────────────────────────────
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  // ── Render ────────────────────────────────────────────────────────────────────
  return (
    <div className="max-w-7xl mx-auto px-4 py-6">
      {/* Header */}
      <div className="md:flex md:items-start md:justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold">Pgtos Fornecedores — Conciliação CBS</h1>
          <p className="mt-1 text-sm text-gray-600 max-w-2xl">
            Cruzamento entre pagamentos efetuados a fornecedores e a situação da CBS na Receita Federal.
            Linhas em <strong>vermelho</strong> indicam pagamentos cuja CBS ainda{' '}
            <strong>não foi extinta</strong> pelo fornecedor — potencial cobrança / risco de crédito.
          </p>
        </div>
        <div className="flex gap-2 mt-3 md:mt-0 shrink-0">
          <Button variant="outline" onClick={fetchConciliacao} disabled={loading}>
            <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
            Atualizar
          </Button>
          <Button variant="outline" onClick={exportExcel} disabled={exporting || loading}>
            <Download className="mr-2 h-4 w-4" />
            {exporting ? 'Exportando…' : 'Exportar Excel'}
          </Button>
        </div>
      </div>

      {/* Filter panel */}
      <div className="flex flex-wrap items-end gap-3 mb-5">
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-500 font-medium">Mês/Ano</label>
          <input
            type="month"
            value={filterMesAno}
            onChange={e => setFilterMesAno(e.target.value)}
            className="border rounded px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-primary"
          />
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-500 font-medium">Status</label>
          <select
            value={filterStatus}
            onChange={e => setFilterStatus(e.target.value)}
            className="border rounded px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-primary"
          >
            <option value="">Todos</option>
            <option value="pendente">Pendente</option>
            <option value="extinto">Extinto</option>
            <option value="sem_dados">Sem dados RFB</option>
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-500 font-medium">CNPJ Fornecedor</label>
          <input
            type="text"
            placeholder="Somente dígitos"
            value={filterForn}
            onChange={e => setFilterForn(e.target.value)}
            className="border rounded px-2 py-1.5 text-sm w-44 focus:outline-none focus:ring-1 focus:ring-primary"
          />
        </div>
        <Button size="sm" onClick={applyFilters}>
          Aplicar
        </Button>
        {(filterMesAno || filterStatus || filterForn) && (
          <button
            className="text-xs text-gray-500 hover:text-gray-800 underline"
            onClick={() => { setFilterMesAno(''); setFilterStatus(''); setFilterForn(''); setPage(1); }}
          >
            Limpar filtros
          </button>
        )}
        <span className="ml-auto text-sm text-muted-foreground">
          {total} registro{total !== 1 ? 's' : ''}
        </span>
      </div>

      {/* Summary cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4 mb-6">
        {/* Total Pago */}
        <div className="rounded-lg border border-blue-200 bg-blue-50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wide text-blue-800 mb-1">
            Total Pago
          </div>
          <div className="text-xl font-bold text-blue-900">
            {sumario ? formatCurrency(sumario.total_pago) : 'R$ 0,00'}
          </div>
          <div className="text-xs mt-1 text-blue-700">
            {sumario ? sumario.total_notas : 0} nota{sumario?.total_notas !== 1 ? 's' : ''}
          </div>
        </div>

        {/* CBS Extinto */}
        <div className="rounded-lg border border-green-200 bg-green-50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wide text-green-800 mb-1">
            CBS Extinto
          </div>
          <div className="text-xl font-bold text-green-900">
            {sumario ? formatCurrency(sumario.cbs_extinto) : 'R$ 0,00'}
          </div>
          <div className="text-xs mt-1 text-green-700">
            {sumario ? sumario.notas_extinto : 0} nota{sumario?.notas_extinto !== 1 ? 's' : ''}
          </div>
        </div>

        {/* CBS Pendente — card de risco, destaque em vermelho */}
        <div className="rounded-lg border-2 border-red-300 bg-red-50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wide text-red-800 mb-1">
            CBS Pendente
          </div>
          <div className="text-xl font-bold text-red-900">
            {sumario ? formatCurrency(sumario.cbs_pendente) : 'R$ 0,00'}
          </div>
          <div className="text-xs mt-1 text-red-700">
            {sumario ? sumario.notas_pendente : 0} nota{sumario?.notas_pendente !== 1 ? 's' : ''} em risco
          </div>
        </div>

        {/* Sem dados RFB */}
        <div className="rounded-lg border border-gray-200 bg-gray-50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wide text-gray-700 mb-1">
            Sem dados RFB
          </div>
          <div className="text-xl font-bold text-gray-800">
            {sumario ? sumario.notas_sem_dados : 0}
          </div>
          <div className="text-xs mt-1 text-gray-600">
            nota{sumario?.notas_sem_dados !== 1 ? 's' : ''} sem retorno RFB
          </div>
        </div>
      </div>

      {/* Table */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-base">Pagamentos x CBS</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {loading ? (
            <div className="flex items-center justify-center h-40">
              <div className="animate-spin rounded-full h-10 w-10 border-b-2 border-primary" />
            </div>
          ) : error ? (
            <div className="py-10 text-center text-red-600 text-sm">{error}</div>
          ) : items.length === 0 ? (
            <div className="py-12 text-center text-muted-foreground text-sm">
              Nenhum pagamento encontrado para os filtros aplicados.
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b bg-gray-50 text-xs text-muted-foreground">
                    <th className="px-3 py-2 text-left font-medium w-6" />
                    <th className="px-3 py-2 text-left font-medium">Fornecedor</th>
                    <th className="px-3 py-2 text-left font-medium">Tipo</th>
                    <th className="px-3 py-2 text-left font-medium">Chave Doc</th>
                    <th className="px-3 py-2 text-right font-medium">Parcelas</th>
                    <th className="px-3 py-2 text-right font-medium">Total Pago</th>
                    <th className="px-3 py-2 text-right font-medium">Valor CBS</th>
                    <th className="px-3 py-2 text-center font-medium">Status</th>
                    <th className="px-3 py-2 text-right font-medium">CBS Pendente</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {items.map(item => {
                    const isExpanded = expanded.has(item.chave_doc);
                    const rowParcelas = parcelas[item.chave_doc];
                    const isLoadingParcelas = parcelasLoading.has(item.chave_doc);

                    return (
                      <>
                        <tr
                          key={item.chave_doc}
                          className={`hover:bg-gray-50 transition-colors cursor-pointer ${
                            item.status_conciliacao === 'pendente' ? 'bg-red-50/40' : ''
                          }`}
                          onClick={() => toggleExpand(item.chave_doc)}
                        >
                          {/* Chevron */}
                          <td className="px-3 py-2 text-muted-foreground">
                            {isExpanded
                              ? <ChevronDown className="h-4 w-4" />
                              : <ChevronRight className="h-4 w-4" />
                            }
                          </td>

                          {/* Fornecedor */}
                          <td className="px-3 py-2">
                            <div className="font-medium text-sm">
                              {item.forn_nome ?? '—'}
                            </div>
                            <div className="text-xs text-muted-foreground font-mono">
                              {formatNI(item.forn_cnpj)}
                            </div>
                          </td>

                          {/* Tipo */}
                          <td className="px-3 py-2">
                            <span className="font-mono text-xs bg-gray-100 px-1.5 py-0.5 rounded">
                              {item.tipo_doc}
                            </span>
                          </td>

                          {/* Chave Doc */}
                          <td className="px-3 py-2">
                            <span
                              className="font-mono text-xs text-muted-foreground"
                              title={item.chave_doc}
                            >
                              {formatChaveTruncated(item.chave_doc)}
                            </span>
                          </td>

                          {/* Parcelas */}
                          <td className="px-3 py-2 text-right text-muted-foreground">
                            {item.num_parcelas}
                          </td>

                          {/* Total Pago */}
                          <td className="px-3 py-2 text-right font-medium">
                            {formatCurrency(item.total_pago)}
                          </td>

                          {/* Valor CBS Nota */}
                          <td className="px-3 py-2 text-right text-muted-foreground">
                            {formatCurrency(item.valor_cbs_nota)}
                          </td>

                          {/* Status */}
                          <td className="px-3 py-2 text-center">
                            <StatusBadge status={item.status_conciliacao} />
                          </td>

                          {/* CBS Pendente */}
                          <td className="px-3 py-2 text-right">
                            {item.valor_cbs_nao_extinto > 0
                              ? <span className="text-red-700 font-semibold">
                                  {formatCurrency(item.valor_cbs_nao_extinto)}
                                </span>
                              : <span className="text-muted-foreground">—</span>
                            }
                          </td>
                        </tr>

                        {/* Expanded parcelas row */}
                        {isExpanded && (
                          <tr key={`${item.chave_doc}-parcelas`} className="bg-gray-50">
                            <td colSpan={9} className="px-6 py-3">
                              {isLoadingParcelas ? (
                                <p className="text-xs text-muted-foreground">Carregando parcelas…</p>
                              ) : rowParcelas && rowParcelas.length > 0 ? (
                                <div>
                                  <p className="text-xs font-semibold text-muted-foreground mb-2">
                                    Parcelas ({rowParcelas.length})
                                  </p>
                                  <table className="w-full text-xs border rounded overflow-hidden">
                                    <thead>
                                      <tr className="bg-white border-b text-muted-foreground">
                                        <th className="px-3 py-1.5 text-left font-medium">Data Pagamento</th>
                                        <th className="px-3 py-1.5 text-right font-medium">Valor</th>
                                        <th className="px-3 py-1.5 text-left font-medium">Nº Doc Pagamento</th>
                                        <th className="px-3 py-1.5 text-left font-medium">Descrição</th>
                                      </tr>
                                    </thead>
                                    <tbody className="divide-y">
                                      {rowParcelas.map((p, idx) => (
                                        <tr key={idx} className="bg-white">
                                          <td className="px-3 py-1.5">{formatDate(p.data_pagamento)}</td>
                                          <td className="px-3 py-1.5 text-right font-medium">
                                            {formatCurrency(p.valor_pagamento)}
                                          </td>
                                          <td className="px-3 py-1.5 font-mono">
                                            {p.num_doc_pagamento ?? '—'}
                                          </td>
                                          <td className="px-3 py-1.5 text-muted-foreground">
                                            {p.descricao ?? '—'}
                                          </td>
                                        </tr>
                                      ))}
                                    </tbody>
                                  </table>
                                </div>
                              ) : (
                                <p className="text-xs text-muted-foreground">
                                  Nenhuma parcela encontrada.
                                </p>
                              )}
                            </td>
                          </tr>
                        )}
                      </>
                    );
                  })}
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
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
