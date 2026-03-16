import { useState, useEffect, useCallback } from 'react';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import * as XLSX from 'xlsx';
import { useFiliais } from '@/contexts/FilialContext';
import { Card, CardContent } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ChevronLeft, ChevronRight, Filter, X, Download, Copy, Check, FileText } from 'lucide-react';
import { toast } from 'sonner';

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

interface DebitPage {
  debitos: RFBDebito[];
  resumo: RFBResumo | null;
  pagination: { page: number; page_size: number; total: number; total_pages: number };
}

// ── Paginação ─────────────────────────────────────────────────────────────────

function PaginationBar({
  page, pageCount, onChange,
}: { page: number; pageCount: number; onChange: (p: number) => void }) {
  const [inputVal, setInputVal] = useState(String(page));
  useEffect(() => { setInputVal(String(page)); }, [page]);
  if (pageCount <= 1) return null;
  const go = (raw: string) => {
    const n = parseInt(raw, 10);
    if (!isNaN(n) && n >= 1 && n <= pageCount) onChange(n);
    else setInputVal(String(page));
  };
  return (
    <div className="flex items-center justify-center gap-2 py-3 border-t">
      <Button size="sm" variant="outline" className="h-7 w-7 p-0"
        disabled={page === 1} onClick={() => onChange(page - 1)}>
        <ChevronLeft className="h-3 w-3" />
      </Button>
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <span>Pág.</span>
        <input
          type="number" min={1} max={pageCount}
          value={inputVal}
          onChange={e => setInputVal(e.target.value)}
          onBlur={e => go(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') go(inputVal); }}
          className="w-14 h-7 rounded border border-input bg-background px-2 text-center text-xs focus:outline-none focus:ring-1 focus:ring-ring"
        />
        <span>de {pageCount}</span>
      </div>
      <Button size="sm" variant="outline" className="h-7 w-7 p-0"
        disabled={page === pageCount} onClick={() => onChange(page + 1)}>
        <ChevronRight className="h-3 w-3" />
      </Button>
    </div>
  );
}

// ── Filtros ──────────────────────────────────────────────────────────────────

interface Filters {
  modelo: string;
  dataInicio: string;
  dataFim: string;
  chave: string;
  cliente: string;
}

const EMPTY_FILTERS: Filters = { modelo: '', dataInicio: '', dataFim: '', chave: '', cliente: '' };

const MODELOS_DFE = ['55', '65', '57', '67', '58', '63'];

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
  if (!p) return '—';
  // YYYYMM → MM/YYYY
  if (/^\d{6}$/.test(p)) return `${p.slice(4, 6)}/${p.slice(0, 4)}`;
  // YYYY-MM ou YYYY-MM-DD → MM/YYYY
  const m = p.match(/^(\d{4})-(\d{2})/);
  if (m) return `${m[2]}/${m[1]}`;
  return p;
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

// ── DANFE via backend (/api/danfe/{chave}) ────────────────────────────────────

async function openDanfe(chave: string) {
  const res = await fetch(`/api/danfe/${chave}`, {
    headers: {
      'Authorization': `Bearer ${localStorage.getItem('token')}`,
      'X-Company-ID':  localStorage.getItem('companyId') || '',
    },
  });
  if (res.status === 404) { toast.error('XML desta NF-e não encontrado. Importe o XML de saída primeiro.'); return; }
  if (!res.ok) { toast.error('Erro ao gerar DANFE. Tente novamente.'); return; }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const win = window.open(url, '_blank');
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
  if (!win) toast.warning('Permita popups para visualizar o DANFE.');
}

// ── Botão copiar chave ────────────────────────────────────────────────────────

function CopyChaveButton({ chave }: { chave: string }) {
  const [copied, setCopied] = useState(false);
  function handleCopy(e: React.MouseEvent) {
    e.stopPropagation();
    navigator.clipboard.writeText(chave).then(() => {
      setCopied(true);
      toast.success('Chave copiada!');
      setTimeout(() => setCopied(false), 2000);
    });
  }
  return (
    <button onClick={handleCopy} title="Copiar chave de acesso"
      className="ml-1 inline-flex items-center text-muted-foreground hover:text-primary transition-colors">
      {copied ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
    </button>
  );
}

// ── Componente principal ─────────────────────────────────────────────────────

export default function RFBDebitos() {
  const [requests,    setRequests]    = useState<RFBRequest[]>([]);
  const [loadingList, setLoadingList] = useState(true);
  const [selectedId,  setSelectedId]  = useState<string | null>(null);
  const [page,        setPage]        = useState(1);
  const [filters,     setFilters]     = useState<Filters>(EMPTY_FILTERS);
  const [showFilters, setShowFilters] = useState(false);

  // Debounced text filters
  const [chaveDebounced,   setChaveDebounced]   = useState('');
  const [clienteDebounced, setClienteDebounced] = useState('');

  useEffect(() => {
    const t = setTimeout(() => setChaveDebounced(filters.chave), 400);
    return () => clearTimeout(t);
  }, [filters.chave]);

  useEffect(() => {
    const t = setTimeout(() => setClienteDebounced(filters.cliente), 400);
    return () => clearTimeout(t);
  }, [filters.cliente]);

  const { selectedFiliais } = useFiliais();
  const niEmitente = selectedFiliais.length === 1 ? selectedFiliais[0].replace(/\D/g, '') : '';

  const getHeaders = useCallback(() => ({
    'Authorization': `Bearer ${localStorage.getItem('token')}`,
    'X-Company-ID':  localStorage.getItem('companyId') || '',
  }), []);

  // Reset page when filters or filial change
  useEffect(() => { setPage(1); }, [filters, niEmitente]);

  // ── Carrega lista de requests concluídos ──────────────────────────────────
  const fetchRequests = useCallback(async () => {
    try {
      const res = await fetch('/api/rfb/apuracao/status', { headers: getHeaders() });
      if (res.ok) {
        const data = await res.json();
        // Ordena mais recente primeiro e de-duplica por período — mantém 1 entrada por MM/AAAA
        const completed = (data.requests || [] as RFBRequest[])
          .filter((r: RFBRequest) => r.status === 'completed')
          .sort((a: RFBRequest, b: RFBRequest) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());

        // Normaliza qualquer formato de período para YYYY-MM para de-duplicação
        const toPeriodKey = (r: RFBRequest): string => {
          const raw = r.resumo?.data_apuracao;
          if (raw) {
            if (/^\d{6}$/.test(raw)) return `${raw.slice(0, 4)}-${raw.slice(4, 6)}`; // 202603 → 2026-03
            const m = raw.match(/^(\d{4})-(\d{2})/);
            if (m) return `${m[1]}-${m[2]}`; // 2026-03-xx → 2026-03
          }
          return r.created_at.slice(0, 7); // fallback: YYYY-MM do created_at
        };

        const seen = new Set<string>();
        const deduped = completed.filter((r: RFBRequest) => {
          const key = toPeriodKey(r);
          if (seen.has(key)) return false;
          seen.add(key);
          return true;
        });

        setRequests(deduped);
        return deduped as RFBRequest[];
      }
    } catch { /* silent */ } finally { setLoadingList(false); }
    return [];
  }, [getHeaders]);

  useEffect(() => {
    fetchRequests().then(list => {
      if (list.length > 0) setSelectedId(list[0].id);
    });
  }, [fetchRequests]);

  // ── React Query para débitos ──────────────────────────────────────────────
  const { data: debitData, isLoading: detailLoading } = useQuery<DebitPage>({
    queryKey: ['rfb-debitos', selectedId, {
      page, modelo: filters.modelo, dataInicio: filters.dataInicio,
      dataFim: filters.dataFim, chave: chaveDebounced,
      cliente: clienteDebounced, niEmitente,
    }],
    queryFn: async () => {
      if (!selectedId) return { debitos: [], resumo: null, pagination: { page: 1, page_size: 100, total: 0, total_pages: 1 } };
      const params = new URLSearchParams({ page: String(page), page_size: '100' });
      if (filters.modelo)     params.set('modelo',       filters.modelo);
      if (filters.dataInicio) params.set('data_de',      filters.dataInicio);
      if (filters.dataFim)    params.set('data_ate',     filters.dataFim);
      if (chaveDebounced)     params.set('chave',        chaveDebounced);
      if (clienteDebounced)   params.set('ni_adquirente', clienteDebounced.replace(/\D/g, ''));
      if (niEmitente)         params.set('ni_emitente',  niEmitente);
      const res = await fetch(`/api/rfb/apuracao/${selectedId}?${params}`, { headers: getHeaders() });
      if (!res.ok) throw new Error('Erro ao carregar débitos');
      return res.json();
    },
    enabled: !!selectedId,
    placeholderData: keepPreviousData,
  });

  const debitos    = debitData?.debitos    || [];
  const resumo     = debitData?.resumo     || null;
  const pagination = debitData?.pagination || { page: 1, page_size: 100, total: 0, total_pages: 1 };
  const pageCount  = pagination.total_pages;

  const hasActiveFilters = Object.values(filters).some(v => v !== '');

  function clearFilters() { setFilters(EMPTY_FILTERS); }
  function setFilter(key: keyof Filters, value: string) {
    setFilters(prev => ({ ...prev, [key]: value }));
  }

  // ── Export Excel (busca todos os registros filtrados) ──────────────────────
  async function exportExcel() {
    if (!selectedId) return;
    const params = new URLSearchParams({ page: '1', page_size: '500' });
    if (filters.modelo)     params.set('modelo',       filters.modelo);
    if (filters.dataInicio) params.set('data_de',      filters.dataInicio);
    if (filters.dataFim)    params.set('data_ate',     filters.dataFim);
    if (chaveDebounced)     params.set('chave',        chaveDebounced);
    if (clienteDebounced)   params.set('ni_adquirente', clienteDebounced.replace(/\D/g, ''));
    if (niEmitente)         params.set('ni_emitente',  niEmitente);

    let allRows: RFBDebito[] = [];
    try {
      const res = await fetch(`/api/rfb/apuracao/${selectedId}?${params}`, { headers: getHeaders() });
      if (res.ok) {
        const data = await res.json();
        allRows = data.debitos || [];
        if (data.pagination?.total > 500) {
          toast.warning(`Exportando primeiros 500 de ${formatNumber(data.pagination.total)} registros`);
        }
      }
    } catch {
      toast.error('Erro ao buscar dados para exportação');
      return;
    }

    const periodo = resumo ? formatPeriodo(resumo.data_apuracao) : 'export';
    const rows = allRows.map(d => ({
      'Modelo':           d.modelo_dfe || '—',
      'Série':            d.serie || '—',
      'Nº NF':            d.numero_dfe || '—',
      'CNPJ Emitente':    d.ni_emitente,
      'Cliente':          d.ni_adquirente,
      'Data Emissão':     d.data_dfe_emissao ? d.data_dfe_emissao.slice(0, 10) : '—',
      'Chave Eletrônica': d.chave_dfe,
      'CBS Total':        d.valor_cbs_total,
      'CBS Extinto':      d.valor_cbs_extinto,
      'CBS Não Extinto':  d.valor_cbs_nao_extinto,
      'Situação':         d.situacao_debito || '—',
    }));
    const ws = XLSX.utils.json_to_sheet(rows);
    ws['!cols'] = [
      { wch: 8 }, { wch: 6 }, { wch: 12 }, { wch: 20 }, { wch: 20 },
      { wch: 12 }, { wch: 46 }, { wch: 14 }, { wch: 14 }, { wch: 14 }, { wch: 20 },
    ];
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, ws, 'Débitos CBS');
    XLSX.writeFile(wb, `debitos_cbs_${periodo.replace('/', '-')}.xlsx`);
    toast.success(`${formatNumber(allRows.length)} registros exportados`);
  }

  const selectedRequest = requests.find(r => r.id === selectedId);

  // ── Loading inicial ──────────────────────────────────────────────────────
  if (loadingList || (detailLoading && !resumo && debitos.length === 0)) {
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
          <Select value={selectedId ?? ''} onValueChange={id => { setSelectedId(id); setPage(1); }}>
            <SelectTrigger className="h-8 text-xs w-44">
              <SelectValue placeholder="Selecione..." />
            </SelectTrigger>
            <SelectContent>
              {requests.map(r => {
                const periodo = r.resumo?.data_apuracao
                  ? formatPeriodo(r.resumo.data_apuracao)
                  : formatPeriodo(r.created_at.slice(0, 7)); // YYYY-MM
                return (
                  <SelectItem key={r.id} value={r.id} className="text-xs">
                    {periodo}{' — '}{formatCNPJBase(r.cnpj_base)}
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>
        </div>

        {/* Cards resumo compactos */}
        {resumo && (
          <div className="flex flex-wrap gap-2 flex-1">
            {/* Valores CBS */}
            {[
              { label: 'CBS Total',       value: formatCurrency(resumo.valor_cbs_total),       color: 'text-red-600' },
              { label: 'CBS Não Extinto', value: formatCurrency(resumo.valor_cbs_nao_extinto), color: 'text-orange-600' },
              { label: 'CBS Extinto',     value: formatCurrency(resumo.valor_cbs_extinto),     color: 'text-green-600' },
            ].map(c => (
              <Card key={c.label} className="shrink-0">
                <CardContent className="px-3 py-1.5">
                  <p className="text-[9px] text-muted-foreground uppercase tracking-wide leading-tight">{c.label}</p>
                  <p className={`text-sm font-bold leading-tight ${c.color}`}>{c.value}</p>
                </CardContent>
              </Card>
            ))}

            {/* Separador visual */}
            <div className="self-stretch w-px bg-border mx-1" />

            {/* Contagens de documentos */}
            {[
              { label: 'Total Docs',   value: resumo.total_debitos,       color: 'text-foreground' },
              { label: 'Corrente',     value: resumo.total_corrente,      color: 'text-blue-600' },
              { label: 'Ajuste',       value: resumo.total_ajuste,        color: 'text-purple-600' },
              { label: 'Extemporâneo', value: resumo.total_extemporaneo,  color: 'text-amber-600' },
            ].map(c => (
              <Card key={c.label} className="shrink-0">
                <CardContent className="px-3 py-1.5">
                  <p className="text-[9px] text-muted-foreground uppercase tracking-wide leading-tight">{c.label}</p>
                  <p className={`text-sm font-bold leading-tight ${c.color}`}>
                    {formatNumber(c.value)}
                    <span className="text-[9px] font-normal text-muted-foreground ml-1">docs</span>
                  </p>
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
                  {MODELOS_DFE.map(m => (
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
              <Label className="text-[10px] text-muted-foreground mb-1 block">Chave Eletrônica</Label>
              <Input placeholder="Parte da chave..." className="h-7 text-xs"
                value={filters.chave} onChange={e => setFilter('chave', e.target.value)} />
            </div>
            <div>
              <Label className="text-[10px] text-muted-foreground mb-1 block">Cliente (CNPJ/CPF)</Label>
              <Input placeholder="Somente números" className="h-7 text-xs"
                value={filters.cliente} onChange={e => setFilter('cliente', e.target.value)} />
            </div>
          </div>
        )}
      </div>

      {/* ── Tabela ── */}
      <Card>
        <div className="flex items-center justify-between px-4 py-2 border-b">
          <span className="text-xs text-muted-foreground">
            {detailLoading
              ? 'Carregando...'
              : <>{formatNumber(pagination.total)} registros{hasActiveFilters && <span className="text-primary font-medium"> filtrados</span>}</>
            }
          </span>
          <div className="flex items-center gap-2">
            <Button
              size="sm" variant="outline" className="h-7 text-xs gap-1.5"
              onClick={exportExcel}
              disabled={pagination.total === 0 || detailLoading}
            >
              <Download className="h-3.5 w-3.5" />
              Excel ({formatNumber(Math.min(pagination.total, 500))})
            </Button>
          </div>
        </div>

        <CardContent className="p-0">
          {detailLoading ? (
            <div className="flex justify-center py-10">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
            </div>
          ) : debitos.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-gray-100 text-[11px]">
                <thead className="bg-gray-50 sticky top-0">
                  <tr>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Mod.</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Série</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Nº NF</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">CNPJ Emitente</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Cliente</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Data Emissão</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Chave Eletrônica</th>
                    <th className="px-2 py-2 text-right font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">CBS Total</th>
                    <th className="px-2 py-2 text-right font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Extinto</th>
                    <th className="px-2 py-2 text-right font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Não Extinto</th>
                    <th className="px-2 py-2 text-left font-semibold text-[10px] uppercase tracking-wide text-muted-foreground">Situação</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-50">
                  {debitos.map(d => (
                    <tr key={d.id} className="hover:bg-gray-50/60">
                      <td className="px-2 py-1 font-mono">{d.modelo_dfe || '—'}</td>
                      <td className="px-2 py-1 font-mono">{d.serie || '—'}</td>
                      <td className="px-2 py-1 font-mono">{d.numero_dfe || '—'}</td>
                      <td className="px-2 py-1 font-mono text-[10px]">{formatCNPJBase(d.ni_emitente)}</td>
                      <td className="px-2 py-1 font-mono text-[10px]">{formatCNPJBase(d.ni_adquirente)}</td>
                      <td className="px-2 py-1">{formatDate(d.data_dfe_emissao)}</td>
                      <td className="px-2 py-1 font-mono text-[10px] text-muted-foreground">
                        <span className="select-all">{d.chave_dfe || '—'}</span>
                        {d.chave_dfe && <CopyChaveButton chave={d.chave_dfe} />}
                        {d.chave_dfe && (
                          <button
                            onClick={() => openDanfe(d.chave_dfe)}
                            title="Ver DANFE"
                            className="ml-1 inline-flex items-center text-muted-foreground hover:text-primary transition-colors"
                          >
                            <FileText className="h-3 w-3" />
                          </button>
                        )}
                      </td>
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
        <PaginationBar page={page} pageCount={pageCount} onChange={setPage} />
      </Card>

      {selectedRequest && (
        <p className="text-[10px] text-muted-foreground text-right">
          CNPJ Base: {formatCNPJBase(selectedRequest.cnpj_base)} · Importado em: {new Date(selectedRequest.created_at).toLocaleString('pt-BR')}
        </p>
      )}
    </div>
  );
}
