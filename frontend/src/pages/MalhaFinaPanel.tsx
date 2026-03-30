import { useState, useEffect, useMemo } from 'react'; // useMemo: emitenteOptions
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { AlertTriangle, Telescope, ChevronLeft, ChevronRight, Copy, FileText, X, ChevronUp, ChevronDown as ChevronDownIcon, ChevronsUpDown } from 'lucide-react';
import { toast } from 'sonner';
import { formatCnpjComApelido } from '@/lib/formatFilial';

// ── Types ──────────────────────────────────────────────────────────────────────
export interface MalhaFinaRow {
  id: string;
  chave_dfe: string;
  modelo_dfe: string;
  numero_dfe: string;
  data_dfe_emissao: string;
  data_apuracao: string;
  ni_emitente: string;
  ni_adquirente: string;
  valor_cbs_total: number;
  valor_cbs_extinto: number;
  valor_cbs_nao_extinto: number;
  situacao_debito: string;
  tipo_apuracao: string;
  status_nota: string; // 'AUSENTE' | 'CANCELADA'
}

interface MalhaFinaApiResponse {
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
  totals: { valor_cbs_total: number; valor_cbs_nao_extinto: number; canceladas_count: number };
  items: MalhaFinaRow[];
}

interface MalhaFinaResumoRow {
  ni_emitente: string;
  data_emissao: string; // YYYY-MM-DD
  quantidade: number;
}

interface MalhaFinaResumoResponse {
  items: MalhaFinaResumoRow[];
}

export type MalhaFinaTipo = 'nfe-entradas' | 'nfe-saidas' | 'cte';

interface Props {
  tipo: MalhaFinaTipo;
  title: string;
  description: string;
  rfbDisponivel?: boolean;
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function extractNumero(chave: string): string {
  if (chave.length !== 44) return '—';
  return String(parseInt(chave.slice(25, 34), 10));
}

function fmtBRL(v: number | null | undefined): string {
  if (v == null) return '—';
  return v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });
}
function fmtNum(v: number | null | undefined): string {
  if (v == null) return '—';
  return v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}
function fmtCNPJ(v: string): string {
  if (!v) return '—';
  const d = v.replace(/\D/g, '');
  if (d.length === 14) return `${d.slice(0, 2)}.${d.slice(2, 5)}.${d.slice(5, 8)}/${d.slice(8, 12)}-${d.slice(12)}`;
  if (d.length === 11) return `${d.slice(0, 3)}.${d.slice(3, 6)}.${d.slice(6, 9)}-${d.slice(9)}`;
  return v;
}


const SITUACAO_COLOR: Record<string, string> = {
  'EXTINTO': 'bg-green-100 text-green-700 border-green-200',
  'EM_ABERTO': 'bg-red-100 text-red-700 border-red-200',
  'PARCIALMENTE_EXTINTO': 'bg-orange-100 text-orange-700 border-orange-200',
};

function SituacaoBadge({ s }: { s: string }) {
  const cls = SITUACAO_COLOR[s] || 'bg-gray-100 text-gray-600 border-gray-200';
  return (
    <Badge variant="outline" className={`text-[10px] px-1 py-0 ${cls}`}>
      {s.replace(/_/g, ' ') || '—'}
    </Badge>
  );
}

function StatusNotaBadge({ status }: { status: string }) {
  if (status === 'CANCELADA') {
    return (
      <Badge variant="outline" className="text-[10px] px-1.5 py-0 bg-yellow-50 text-yellow-700 border-yellow-300">
        Cancelada
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="text-[10px] px-1.5 py-0 bg-red-50 text-red-700 border-red-200">
      Ausente
    </Badge>
  );
}

// ── Paginação ─────────────────────────────────────────────────────────────────
function Pagination({ page, pageCount, onChange }: { page: number; pageCount: number; onChange: (p: number) => void }) {
  const [inputVal, setInputVal] = useState(String(page));
  useEffect(() => { setInputVal(String(page)); }, [page]);
  if (pageCount <= 1) return null;
  const go = (raw: string) => {
    const n = parseInt(raw, 10);
    if (!isNaN(n) && n >= 1 && n <= pageCount) onChange(n);
    else setInputVal(String(page));
  };
  return (
    <div className="flex items-center justify-center gap-2 py-3">
      <Button size="sm" variant="outline" className="h-7 w-7 p-0" disabled={page === 1} onClick={() => onChange(page - 1)}>
        <ChevronLeft className="h-3 w-3" />
      </Button>
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <span>Pág.</span>
        <input type="number" min={1} max={pageCount} value={inputVal}
          onChange={e => setInputVal(e.target.value)}
          onBlur={e => go(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') go(inputVal); }}
          className="w-20 h-7 rounded border border-input bg-background px-2 text-center text-xs focus:outline-none focus:ring-1 focus:ring-ring"
        />
        <span>de {pageCount}</span>
      </div>
      <Button size="sm" variant="outline" className="h-7 w-7 p-0" disabled={page === pageCount} onClick={() => onChange(page + 1)}>
        <ChevronRight className="h-3 w-3" />
      </Button>
    </div>
  );
}

// ── Detalhe ───────────────────────────────────────────────────────────────────
function DR({ label, value }: { label: string; value: string | number | null | undefined }) {
  return (
    <div className="flex justify-between py-0.5 border-b border-dashed last:border-0">
      <span className="text-[11px] text-muted-foreground w-40 shrink-0">{label}</span>
      <span className="text-[11px] font-medium text-right">{value ?? '—'}</span>
    </div>
  );
}
function DRBRL({ label, value }: { label: string; value: number | null | undefined }) {
  return (
    <div className="flex justify-between py-0.5 border-b border-dashed last:border-0">
      <span className="text-[11px] text-muted-foreground w-40 shrink-0">{label}</span>
      <span className="text-[11px] font-medium text-right">{fmtBRL(value)}</span>
    </div>
  );
}
function DS({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-2">
      <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-1 pb-0.5 border-b">{title}</h3>
      {children}
    </div>
  );
}

function DetalheMalhaFina({ row, onClose, token, companyId }: {
  row: MalhaFinaRow; onClose: () => void;
  token: string | null; companyId: string | null;
}) {
  const [danfeLoading, setDanfeLoading] = useState(false);

  async function openDanfe() {
    setDanfeLoading(true);
    try {
      const res = await fetch(`/api/danfe/${row.chave_dfe}`, {
        headers: { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId || '' },
      });
      if (!res.ok) {
        if (res.status === 404) {
          toast.warning('XML não disponível. Importe este documento para gerar o DANFE.');
        } else {
          toast.error('Erro ao gerar DANFE');
        }
        return;
      }
      const blob = await res.blob();
      window.open(URL.createObjectURL(blob), '_blank');
    } catch {
      toast.error('Erro de conexão ao gerar DANFE');
    } finally {
      setDanfeLoading(false);
    }
  }

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="text-xs">
            Documento {row.modelo_dfe} · Nº {row.numero_dfe || '—'}
            <div className="text-[11px] font-normal text-muted-foreground mt-0.5 break-all">
              Chave: {row.chave_dfe}
            </div>
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-1 mt-1">
          <DS title="Identificação">
            <DR label="Modelo DFe" value={row.modelo_dfe} />
            <DR label="Número" value={row.numero_dfe} />
            <DR label="Data Emissão" value={row.data_dfe_emissao} />
            <DR label="Período Apuração" value={row.data_apuracao} />
          </DS>
          <DS title="Receita Federal">
            <DR label="Tipo Apuração" value={row.tipo_apuracao} />
            <div className="flex justify-between py-0.5 border-b border-dashed last:border-0">
              <span className="text-[11px] text-muted-foreground w-40 shrink-0">Situação Débito</span>
              <SituacaoBadge s={row.situacao_debito} />
            </div>
          </DS>
          <DS title="Partes">
            <DR label="CNPJ Emitente" value={fmtCNPJ(row.ni_emitente)} />
            <DR label="CNPJ Adquirente" value={fmtCNPJ(row.ni_adquirente)} />
          </DS>
          <DS title="Valores CBS">
            <DRBRL label="CBS Total" value={row.valor_cbs_total} />
            <DRBRL label="CBS Extinto" value={row.valor_cbs_extinto} />
            <DRBRL label="CBS Não Extinto" value={row.valor_cbs_nao_extinto} />
          </DS>
        </div>
        <div className="flex justify-end pt-2 border-t">
          <Button size="sm" variant="outline" onClick={openDanfe} disabled={danfeLoading} className="text-xs gap-1.5">
            <FileText className="h-3.5 w-3.5" />
            {danfeLoading ? 'Gerando...' : 'Ver DANFE'}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ── Painel principal ───────────────────────────────────────────────────────────
export default function MalhaFinaPanel({ tipo, title, description, rfbDisponivel = true }: Props) {
  const { token, companyId } = useAuth();

  const hoje = new Date();
  const defaultDataDe = `${hoje.getFullYear()}-${String(hoje.getMonth() + 1).padStart(2, '0')}-01`;

  const [dataDe,     setDataDe]     = useState(defaultDataDe);
  const [dataAte,    setDataAte]    = useState('');
  const [statusFilt, setStatusFilt] = useState('');
  const [filterCNPJ, setFilterCNPJ] = useState('');
  const [sortCol,    setSortCol]    = useState('data_dfe_emissao');
  const [sortDir,    setSortDir]    = useState<'asc'|'desc'>('desc');
  const [page,       setPage]       = useState(1);
  const [cnpjDeb,    setCnpjDeb]    = useState('');
  const [selected,   setSelected]   = useState<MalhaFinaRow | null>(null);

  // Apelidos de filiais
  const [apelidos, setApelidos] = useState<Record<string, string>>({});

  const authHeaders = { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId || '' };

  useEffect(() => {
    if (!token) return;
    fetch('/api/config/filial-apelidos', { headers: authHeaders })
      .then(r => r.ok ? r.json() : [])
      .then((list: { cnpj: string; apelido: string }[]) => {
        const map: Record<string, string> = {};
        (list || []).forEach(fa => { map[fa.cnpj.replace(/\D/g, '')] = fa.apelido; });
        setApelidos(map);
      })
      .catch(() => {});
  }, [token, companyId]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const t = setTimeout(() => setCnpjDeb(filterCNPJ), 400);
    return () => clearTimeout(t);
  }, [filterCNPJ]);

  useEffect(() => { setPage(1); }, [dataDe, dataAte, statusFilt, cnpjDeb, sortCol, sortDir]);

  function handleSort(col: string) {
    if (sortCol === col) setSortDir(d => d === 'desc' ? 'asc' : 'desc');
    else { setSortCol(col); setSortDir('desc'); }
    setPage(1);
  }
  function SortIcon({ col }: { col: string }) {
    if (sortCol !== col) return <ChevronsUpDown className="inline h-3 w-3 ml-0.5 opacity-40" />;
    return sortDir === 'desc'
      ? <ChevronDownIcon className="inline h-3 w-3 ml-0.5" />
      : <ChevronUp className="inline h-3 w-3 ml-0.5" />;
  }

  const { data, isFetching, isError } = useQuery<MalhaFinaApiResponse>({
    queryKey: ['malha-fina', tipo, companyId, { page, dataDe, dataAte, statusFilt, cnpjDeb, sortCol, sortDir }],
    queryFn: async () => {
      const params = new URLSearchParams();
      params.set('page', String(page));
      params.set('sort_by', sortCol);
      params.set('sort_dir', sortDir);
      if (dataDe) params.set('data_de', dataDe);
      if (dataAte) params.set('data_ate', dataAte);
      if (statusFilt) params.set('status', statusFilt);
      if (cnpjDeb) params.set('emit_cnpj', cnpjDeb.replace(/\D/g, ''));
      const res = await fetch(`/api/malha-fina/${tipo}?${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    placeholderData: keepPreviousData,
    enabled: !!token && !!companyId && rfbDisponivel,
  });

  // Resumo por tipo (da MV) — apenas data_de, sem filtro de emitente para sempre mostrar todas as opções
  const { data: resumoData } = useQuery<MalhaFinaResumoResponse>({
    queryKey: ['malha-fina-resumo', tipo, companyId, dataDe, dataAte],
    queryFn: async () => {
      const params = new URLSearchParams();
      if (dataDe) params.set('data_de', dataDe);
      if (dataAte) params.set('data_ate', dataAte);
      const res = await fetch(`/api/malha-fina/${tipo}/resumo?${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    placeholderData: keepPreviousData,
    enabled: !!token && !!companyId && rfbDisponivel,
  });

  const items      = data?.items      ?? [];
  const total      = data?.total      ?? 0;
  const totalPages = data?.total_pages ?? 1;
  const totals     = data?.totals     ?? { valor_cbs_total: 0, valor_cbs_nao_extinto: 0, canceladas_count: 0 };
  const resumoItems = resumoData?.items ?? [];

  // Opções do Select derivadas dos emitentes presentes no resumo
  const emitenteOptions = useMemo(() => {
    const seen = new Set<string>();
    const opts: { cnpj: string; label: string }[] = [];
    for (const row of resumoItems) {
      const cnpj = row.ni_emitente.replace(/\D/g, '');
      if (!cnpj || seen.has(cnpj)) continue;
      seen.add(cnpj);
      opts.push({ cnpj, label: formatCnpjComApelido(cnpj, apelidos) });
    }
    return opts;
  }, [resumoItems, apelidos]);

  const hasFilters = !!filterCNPJ;
  function clearFilters() { setFilterCNPJ(''); setPage(1); }

  function copyChave(chave: string, e: React.MouseEvent) {
    e.stopPropagation();
    navigator.clipboard.writeText(chave).then(() => toast.success('Chave copiada'));
  }

  if (!rfbDisponivel) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{title}</h1>
          <p className="text-sm text-muted-foreground mt-1">{description}</p>
        </div>
        <div className="flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 px-5 py-4 text-sm text-amber-800">
          <Telescope className="h-5 w-5 mt-0.5 shrink-0" />
          <div>
            <p className="font-semibold">A Receita Federal ainda não disponibilizou este tipo de movimento</p>
            <p className="text-xs mt-1 text-amber-700">
              Quando a RFB liberar a consulta para este tipo de documento, basta ativar o painel
              definindo <code className="bg-amber-100 px-1 rounded">rfbDisponivel&#123;true&#125;</code> na página correspondente.
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Cabeçalho */}
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{title}</h1>
        <p className="text-sm text-muted-foreground mt-1">{description}</p>
      </div>

      {/* Aviso */}
      <div className="flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
        <AlertTriangle className="h-4 w-4 mt-0.5 shrink-0" />
        <span>
          Documentos identificados pela Receita Federal que <strong>não constam nos seus registros importados</strong>.
          Importe os XMLs ausentes para manter a apuração completa.
        </span>
      </div>

      {/* Filtros */}
      <Card>
        <CardContent className="pt-4">
          <div className="flex flex-wrap gap-3 items-end">
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Data Início</label>
              <Input
                type="date"
                value={dataDe}
                onChange={e => { setDataDe(e.target.value); setPage(1); }}
                className="h-8 w-40"
              />
            </div>

            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Data Fim</label>
              <Input
                type="date"
                value={dataAte}
                onChange={e => { setDataAte(e.target.value); setPage(1); }}
                className="h-8 w-40"
              />
            </div>

            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Status</label>
              <Select value={statusFilt || 'todas'} onValueChange={v => { setStatusFilt(v === 'todas' ? '' : v); setPage(1); }}>
                <SelectTrigger className="h-8 w-36 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="todas" className="text-xs">Todas</SelectItem>
                  <SelectItem value="ausente" className="text-xs">Ausentes</SelectItem>
                  <SelectItem value="cancelada" className="text-xs">Canceladas</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">CNPJ Emitente</label>
              <Select
                value={filterCNPJ || 'all'}
                onValueChange={v => { setFilterCNPJ(v === 'all' ? '' : v); setPage(1); }}
              >
                <SelectTrigger className="h-8 w-64 text-xs">
                  <SelectValue placeholder="Todas as filiais..." />
                </SelectTrigger>
                <SelectContent position="popper" side="bottom" className="max-h-72 overflow-y-auto">
                  <SelectItem value="all" className="text-xs">Todas as filiais</SelectItem>
                  {emitenteOptions.map(opt => (
                    <SelectItem key={opt.cnpj} value={opt.cnpj} className="text-xs font-mono">
                      {opt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {hasFilters && (
              <Button size="sm" variant="ghost" onClick={clearFilters} className="self-end">
                <X className="h-3 w-3 mr-1" /> Limpar filtros
              </Button>
            )}

            <span className="text-xs text-muted-foreground ml-auto self-end">
              {isFetching ? 'Carregando...' : `${total.toLocaleString('pt-BR')} documento(s) na malha fina`}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* Totalizadores */}
      {total > 0 && (
        <div className="grid grid-cols-3 gap-2">
          <Card className="p-3 border-red-100">
            <p className="text-[10px] text-muted-foreground">CBS Total (RFB)</p>
            <p className="text-sm font-bold mt-0.5 text-red-700">{fmtBRL(totals.valor_cbs_total)}</p>
          </Card>
          <Card className="p-3 border-red-200 bg-red-50/50">
            <p className="text-[10px] text-muted-foreground">CBS Não Extinto (em aberto)</p>
            <p className="text-sm font-bold mt-0.5 text-red-800">{fmtBRL(totals.valor_cbs_nao_extinto)}</p>
          </Card>
          <Card className="p-3 border-yellow-200 bg-yellow-50/50">
            <p className="text-[10px] text-muted-foreground">Canceladas (SAP)</p>
            <p className="text-sm font-bold mt-0.5 text-yellow-700">
              {totals.canceladas_count.toLocaleString('pt-BR')} nota{totals.canceladas_count !== 1 ? 's' : ''}
            </p>
          </Card>
        </div>
      )}


      {/* Tabela principal */}
      <Card>
        <CardHeader className="py-2 px-4">
          <CardTitle className="flex items-center gap-2 text-[11px] text-muted-foreground font-normal">
            <AlertTriangle className="h-3.5 w-3.5 text-red-500" />
            Clique em uma linha para ver todos os dados e gerar o DANFE
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isError ? (
            <p className="text-xs text-red-500 text-center py-8">Erro ao carregar dados.</p>
          ) : items.length === 0 ? (
            <div className="text-center py-12 space-y-2">
              {isFetching ? (
                <p className="text-xs text-muted-foreground">Carregando...</p>
              ) : (
                <>
                  <p className="text-sm font-medium text-green-700">Nenhum documento na malha fina</p>
                  <p className="text-xs text-muted-foreground">
                    Todos os documentos da RFB estão presentes nos seus registros importados para o período selecionado.
                  </p>
                </>
              )}
            </div>
          ) : (
            <>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="py-1.5 px-2 text-[11px]">Status</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Chave DFe</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-center">Mod.</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-center">Número</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('data_dfe_emissao')}>
                        Emissão <SortIcon col="data_dfe_emissao" />
                      </TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">CNPJ Emitente</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">CNPJ Adquirente</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('valor_cbs_total')}>
                        CBS Total (R$) <SortIcon col="valor_cbs_total" />
                      </TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('valor_cbs_nao_extinto')}>
                        CBS Não Extinto (R$) <SortIcon col="valor_cbs_nao_extinto" />
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {items.map(row => (
                      <TableRow
                        key={row.id}
                        className={`cursor-pointer hover:bg-muted/50 h-8 ${row.status_nota === 'CANCELADA' ? 'bg-yellow-50/40 dark:bg-yellow-950/10' : 'bg-red-50/30 dark:bg-red-950/10'}`}
                        onClick={() => setSelected(row)}
                      >
                        <TableCell className="py-1 px-2">
                          <StatusNotaBadge status={row.status_nota} />
                        </TableCell>
                        <TableCell className="py-1 px-2">
                          <div className="flex items-center gap-1">
                            <span className="font-mono text-[10px] text-muted-foreground">{row.chave_dfe}</span>
                            <button
                              onClick={e => copyChave(row.chave_dfe, e)}
                              className="text-muted-foreground hover:text-foreground transition-colors shrink-0"
                              title="Copiar chave"
                            >
                              <Copy className="h-2.5 w-2.5" />
                            </button>
                          </div>
                        </TableCell>
                        <TableCell className="py-1 px-2 text-[11px] text-center font-mono">{row.modelo_dfe}</TableCell>
                        <TableCell className="py-1 px-2 text-[11px] text-center font-mono">{extractNumero(row.chave_dfe)}</TableCell>
                        <TableCell className="py-1 px-2 text-[11px] whitespace-nowrap">{row.data_dfe_emissao || '—'}</TableCell>
                        <TableCell className="py-1 px-2 text-[11px]">
                          {formatCnpjComApelido(row.ni_emitente, apelidos)}
                        </TableCell>
                        <TableCell className="py-1 px-2 font-mono text-[11px]">{fmtCNPJ(row.ni_adquirente)}</TableCell>
                        <TableCell className="py-1 px-2 text-[11px] text-right font-semibold text-red-700">
                          {fmtNum(row.valor_cbs_total)}
                        </TableCell>
                        <TableCell className="py-1 px-2 text-[11px] text-right font-semibold text-red-800">
                          {fmtNum(row.valor_cbs_nao_extinto)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
              <Pagination page={page} pageCount={totalPages} onChange={setPage} />
            </>
          )}
        </CardContent>
      </Card>

      {selected && (
        <DetalheMalhaFina
          row={selected}
          onClose={() => setSelected(null)}
          token={token}
          companyId={companyId}
        />
      )}
    </div>
  );
}
