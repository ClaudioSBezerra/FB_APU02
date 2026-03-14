import { useState, useEffect, useCallback, useMemo } from 'react';
import { Card, CardContent } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ChevronLeft, ChevronRight, Filter, X } from 'lucide-react';

// ── Interfaces ───────────────────────────────────────────────────────────────

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
  serie?: string;
  numero_dfe: string;
  chave_dfe: string;
  data_dfe_emissao?: string;
  data_apuracao: string;
  ni_emitente: string;
  ni_adquirente: string;
  valor_documento?: number;
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

interface Filters {
  modelo: string;
  dataInicio: string;
  dataFim: string;
  numInicio: string;
  numFim: string;
  chave: string;
  cliente: string;
  valorMin: string;
}

const EMPTY_FILTERS: Filters = {
  modelo: '', dataInicio: '', dataFim: '',
  numInicio: '', numFim: '', chave: '', cliente: '', valorMin: '',
};

// ── Helpers ──────────────────────────────────────────────────────────────────

function formatCNPJBase(cnpj: string): string {
  if (!cnpj) return '—';
  const d = cnpj.replace(/\D/g, '');
  if (d.length === 8)  return `${d.slice(0,2)}.${d.slice(2,5)}.${d.slice(5)}`;
  if (d.length === 14) return `${d.slice(0,2)}.${d.slice(2,5)}.${d.slice(5,8)}/${d.slice(8,12)}-${d.slice(12)}`;
  if (d.length === 11) return `${d.slice(0,3)}.${d.slice(3,6)}.${d.slice(6,9)}-${d.slice(9)}`;
  return cnpj;
}

function formatPeriodo(p: string): string {
  if (p && p.length === 6) return `${p.slice(4,6)}/${p.slice(0,4)}`;
  return p || '—';
}

function formatDate(s?: string): string {
  if (!s) return '—';
  try { return new Date(s).toLocaleDateString('pt-BR'); } catch { return s; }
}

function formatCurrency(v: number): string {
  return new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' }).format(v);
}

function formatNumber(n: number): string {
  return new Intl.NumberFormat('pt-BR').format(n);
}

function chaveShort(chave: string): string {
  if (!chave || chave.length < 10) return chave || '—';
  return `${chave.slice(0, 9)}…${chave.slice(-4)}`;
}

// ── Componente principal ─────────────────────────────────────────────────────

export default function RFBDebitos() {
  const [requests,      setRequests]      = useState<RFBRequest[]>([]);
  const [loadingList,   setLoadingList]   = useState(true);
  const [selectedId,    setSelectedId]    = useState<string | null>(null);
  const [resumo,        setResumo]        = useState<RFBResumo | null>(null);
  const [debitos,       setDebitos]       = useState<RFBDebito[]>([]);
  const [pagination,    setPagination]    = useState<Pagination>({ page:1, page_size:500, total:0, total_pages:1 });
  const [detailLoading, setDetailLoading] = useState(false);
  const [filters,       setFilters]       = useState<Filters>(EMPTY_FILTERS);
  const [showFilters,   setShowFilters]   = useState(false);

  const getHeaders = () => ({
    'Authorization': `Bearer ${localStorage.getItem('token')}`,
    'X-Company-ID':  localStorage.getItem('companyId') || '',
  });

  // ── Carrega lista de requests ─────────────────────────────────────────────
  const fetchRequests = useCallback(async () => {
    try {
      const res = await fetch('/api/rfb/apuracao/status', { headers: getHeaders() });
      if (res.ok) {
        const data = await res.json();
        const completed = (data.requests || []).filter((r: RFBRequest) => r.status === 'completed');
        setRequests(completed);
        return completed as RFBRequest[];
      }
    } catch { /* silent */ }
    finally { setLoadingList(false); }
    return [];
  }, []);

  // ── Carrega detalhes de um request ───────────────────────────────────────
  const fetchDetail = useCallback(async (requestId: string, page = 1) => {
    setSelectedId(requestId);
    setDetailLoading(true);
    try {
      const res = await fetch(`/api/rfb/apuracao/${requestId}?page=${page}&page_size=500`, { headers: getHeaders() });
      if (res.ok) {
        const data = await res.json();
        setResumo(data.resumo || null);
        setDebitos(data.debitos || []);
        setPagination(data.pagination || { page:1, page_size:500, total:0, total_pages:1 });
      }
    } catch { /* silent */ }
    finally { setDetailLoading(false); }
  }, []);

  // ── Auto-carrega ao abrir a aba ──────────────────────────────────────────
  useEffect(() => {
    fetchRequests().then(list => {
      if (list.length > 0) fetchDetail(list[0].id);
    });
  }, [fetchRequests, fetchDetail]);

  // ── Filtragem client-side ────────────────────────────────────────────────
  const filtered = useMemo(() => {
    return debitos.filter(d => {
      if (filters.modelo    && d.modelo_dfe !== filters.modelo) return false;
      if (filters.chave     && !d.chave_dfe?.includes(filters.chave)) return false;
      if (filters.cliente   && !d.ni_adquirente?.includes(filters.cliente.replace(/\D/g, ''))) return false;
      if (filters.numInicio && Number(d.numero_dfe) < Number(filters.numInicio)) return false;
      if (filters.numFim    && Number(d.numero_dfe) > Number(filters.numFim)) return false;
      if (filters.valorMin  && d.valor_cbs_total < Number(filters.valorMin.replace(',','.'))) return false;
      if (filters.dataInicio && d.data_dfe_emissao) {
        if (new Date(d.data_dfe_emissao) < new Date(filters.dataInicio)) return false;
      }
      if (filters.dataFim && d.data_dfe_emissao) {
        if (new Date(d.data_dfe_emissao) > new Date(filters.dataFim)) return false;
      }
      return true;
    });
  }, [debitos, filters]);

  const hasActiveFilters = Object.values(filters).some(v => v !== '');
  const modelosUnicos = useMemo(() => [...new Set(debitos.map(d => d.modelo_dfe).filter(Boolean))].sort(), [debitos]);

  function clearFilters() { setFilters(EMPTY_FILTERS); }
  function setFilter(key: keyof Filters, value: string) {
    setFilters(prev => ({ ...prev, [key]: value }));
  }

  const selectedRequest = requests.find(r => r.id === selectedId);

  // ── Loading inicial ──────────────────────────────────────────────────────
  if (loadingList || (detailLoading && !resumo)) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-10 w-10 border-b-2 border-primary" />
      </div>
    );
  }

  if (requests.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-64 text-muted-foreground text-sm gap-2">
        <p className="font-medium">Nenhuma importação concluída.</p>
        <p className="text-xs">Acesse <strong>Importar Débitos</strong> para carregar os dados da RFB.</p>
      </div>
    );
  }

  // ── Layout principal ─────────────────────────────────────────────────────
  return (
    <div className="space-y-3">

      {/* ── Seletor de período + resumo compacto ── */}
      <div className="flex flex-wrap items-start gap-3">

        {/* Período */}
        <div className="shrink-0">
          <Label className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1 block">Período</Label>
          <Select value={selectedId ?? ''} onValueChange={id => fetchDetail(id)}>
            <SelectTrigger className="h-8 text-xs w-44">
              <SelectValue placeholder="Selecione..." />
            </SelectTrigger>
            <SelectContent>
              {requests.map(r => (
                <SelectItem key={r.id} value={r.id} className="text-xs">
                  {r.resumo ? formatPeriodo(r.resumo.data_apuracao) : new Date(r.created_at).toLocaleDateString('pt-BR')}
                  {' — '}{formatCNPJBase(r.cnpj_base)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* Cards resumo compactos */}
        {resumo && (
          <div className="flex flex-wrap gap-2 flex-1">
            {[
              { label: 'Total Débitos',   value: formatNumber(resumo.total_debitos),              color: 'text-foreground' },
              { label: 'CBS Total',       value: formatCurrency(resumo.valor_cbs_total),           color: 'text-red-600' },
              { label: 'CBS Não Extinto', value: formatCurrency(resumo.valor_cbs_nao_extinto),     color: 'text-orange-600' },
              { label: 'CBS Extinto',     value: formatCurrency(resumo.valor_cbs_extinto),         color: 'text-green-600' },
              { label: 'Corrente',        value: formatNumber(resumo.total_corrente),              color: 'text-foreground' },
              { label: 'Ajuste',          value: formatNumber(resumo.total_ajuste),                color: 'text-foreground' },
              { label: 'Extemporâneo',    value: formatNumber(resumo.total_extemporaneo),          color: 'text-foreground' },
            ].map(c => (
              <Card key={c.label} className="shrink-0">
                <CardContent className="px-3 py-1.5">
                  <p className="text-[9px] text-muted-foreground uppercase tracking-wide leading-tight">{c.label}</p>
                  <p className={`text-sm font-bold leading-tight ${c.color}`}>{c.value}</p>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </div>

      {/* ── Barra de filtros ── */}
      <div className="border rounded-lg bg-white">
        <div
          className="flex items-center justify-between px-3 py-2 cursor-pointer select-none"
          onClick={() => setShowFilters(v => !v)}
        >
          <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
            <Filter className="h-3.5 w-3.5" />
            Filtros
            {hasActiveFilters && (
              <Badge className="ml-1 text-[9px] px-1.5 py-0 h-4 bg-primary/10 text-primary border-0">
                ativos
              </Badge>
            )}
          </div>
          <div className="flex items-center gap-2">
            {hasActiveFilters && (
              <button
                onClick={e => { e.stopPropagation(); clearFilters(); }}
                className="flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground"
              >
                <X className="h-3 w-3" /> Limpar
              </button>
            )}
            <span className="text-[10px] text-muted-foreground">{showFilters ? '▲' : '▼'}</span>
          </div>
        </div>

        {showFilters && (
          <div className="border-t px-3 py-3 grid grid-cols-2 md:grid-cols-4 gap-3">
            <div>
              <Label className="text-[10px] text-muted-foreground mb-1 block">Modelo Doc.</Label>
              <Select value={filters.modelo || '_all'} onValueChange={v => setFilter('modelo', v === '_all' ? '' : v)}>
                <SelectTrigger className="h-7 text-xs">
                  <SelectValue placeholder="Todos" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="_all" className="text-xs">Todos</SelectItem>
                  {modelosUnicos.map(m => (
                    <SelectItem key={m} value={m} className="text-xs">{m}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label className="text-[10px] text-muted-foreground mb-1 block">Data Emissão Início</Label>
              <Input type="date" className="h-7 text-xs"
                value={filters.dataInicio} onChange={e => setFilter('dataInicio', e.target.value)} />
            </div>
            <div>
              <Label className="text-[10px] text-muted-foreground mb-1 block">Data Emissão Fim</Label>
              <Input type="date" className="h-7 text-xs"
                value={filters.dataFim} onChange={e => setFilter('dataFim', e.target.value)} />
            </div>
            <div>
              <Label className="text-[10px] text-muted-foreground mb-1 block">Nº NF Início</Label>
              <Input placeholder="0" className="h-7 text-xs"
                value={filters.numInicio} onChange={e => setFilter('numInicio', e.target.value)} />
            </div>
            <div>
              <Label className="text-[10px] text-muted-foreground mb-1 block">Nº NF Fim</Label>
              <Input placeholder="999999" className="h-7 text-xs"
                value={filters.numFim} onChange={e => setFilter('numFim', e.target.value)} />
            </div>
            <div>
              <Label className="text-[10px] text-muted-foreground mb-1 block">Chave Eletrônica</Label>
              <Input placeholder="Parte da chave..." className="h-7 text-xs"
                value={filters.chave} onChange={e => setFilter('chave', e.target.value)} />
            </div>
            <div>
              <Label className="text-[10px] text-muted-foreground mb-1 block">Cliente (CNPJ/CPF)</Label>
              <Input placeholder="Somente números" className="h-7 text-xs"
                value={filters.cliente} onChange={e => setFilter('cliente', e.target.value)} />
            </div>
            <div>
              <Label className="text-[10px] text-muted-foreground mb-1 block">CBS Mín. (R$)</Label>
              <Input placeholder="0,00" className="h-7 text-xs"
                value={filters.valorMin} onChange={e => setFilter('valorMin', e.target.value)} />
            </div>
          </div>
        )}
      </div>

      {/* ── Tabela ── */}
      <Card>
        <div className="flex items-center justify-between px-4 py-2 border-b">
          <span className="text-xs text-muted-foreground">
            {hasActiveFilters
              ? <>{formatNumber(filtered.length)} <span className="text-primary font-medium">filtrados</span> de {formatNumber(debitos.length)} registros</>
              : <>{formatNumber(pagination.total)} registros · pág. {pagination.page}/{pagination.total_pages}</>
            }
          </span>
          <div className="flex items-center gap-1">
            <Button size="sm" variant="outline" className="h-6 w-6 p-0"
              disabled={pagination.page <= 1 || detailLoading || hasActiveFilters}
              onClick={() => selectedId && fetchDetail(selectedId, pagination.page - 1)}>
              <ChevronLeft className="h-3 w-3" />
            </Button>
            <Button size="sm" variant="outline" className="h-6 w-6 p-0"
              disabled={pagination.page >= pagination.total_pages || detailLoading || hasActiveFilters}
              onClick={() => selectedId && fetchDetail(selectedId, pagination.page + 1)}>
              <ChevronRight className="h-3 w-3" />
            </Button>
          </div>
        </div>

        <CardContent className="p-0">
          {detailLoading ? (
            <div className="flex justify-center py-10">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
            </div>
          ) : filtered.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-gray-100 text-[11px]">
                <thead className="bg-gray-50 sticky top-0">
                  <tr>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Tipo</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Mod.</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Série</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Nº NF</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">CNPJ Emitente</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Cliente</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Data Emissão</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Chave Eletrônica</th>
                    <th className="px-2 py-2 text-right font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Valor Doc.</th>
                    <th className="px-2 py-2 text-right font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">CBS Total</th>
                    <th className="px-2 py-2 text-right font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Extinto</th>
                    <th className="px-2 py-2 text-right font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Não Extinto</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Situação</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-50">
                  {filtered.map(d => (
                    <tr key={d.id} className="hover:bg-gray-50/60">
                      <td className="px-2 py-1">
                        <Badge variant="outline" className="text-[9px] px-1 py-0 h-4">
                          {d.tipo_apuracao === 'corrente' ? 'Corr.' : d.tipo_apuracao === 'ajuste' ? 'Ajuste' : 'Extem.'}
                        </Badge>
                      </td>
                      <td className="px-2 py-1 font-mono">{d.modelo_dfe || '—'}</td>
                      <td className="px-2 py-1 font-mono">{d.serie || '—'}</td>
                      <td className="px-2 py-1 font-mono">{d.numero_dfe || '—'}</td>
                      <td className="px-2 py-1 font-mono text-[10px]">{formatCNPJBase(d.ni_emitente)}</td>
                      <td className="px-2 py-1 font-mono text-[10px]">{formatCNPJBase(d.ni_adquirente)}</td>
                      <td className="px-2 py-1">{formatDate(d.data_dfe_emissao)}</td>
                      <td className="px-2 py-1 font-mono text-[10px] text-muted-foreground" title={d.chave_dfe}>
                        {chaveShort(d.chave_dfe)}
                      </td>
                      <td className="px-2 py-1 text-right">{d.valor_documento != null ? formatCurrency(d.valor_documento) : '—'}</td>
                      <td className="px-2 py-1 text-right font-medium text-red-600">{formatCurrency(d.valor_cbs_total)}</td>
                      <td className="px-2 py-1 text-right text-green-600">{formatCurrency(d.valor_cbs_extinto)}</td>
                      <td className="px-2 py-1 text-right text-orange-600">{formatCurrency(d.valor_cbs_nao_extinto)}</td>
                      <td className="px-2 py-1 text-muted-foreground">{d.situacao_debito || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="py-10 text-center text-muted-foreground text-xs">
              {hasActiveFilters ? 'Nenhum resultado para os filtros aplicados.' : 'Nenhum débito CBS encontrado.'}
            </div>
          )}
        </CardContent>
      </Card>

      {selectedRequest && (
        <p className="text-[10px] text-muted-foreground text-right">
          CNPJ Base: {formatCNPJBase(selectedRequest.cnpj_base)} · Importado em: {new Date(selectedRequest.created_at).toLocaleString('pt-BR')}
        </p>
      )}
    </div>
  );
}
