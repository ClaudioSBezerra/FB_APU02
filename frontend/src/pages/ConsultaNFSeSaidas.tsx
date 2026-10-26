import { useState, useEffect } from 'react';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
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
import { X, ChevronLeft, ChevronRight, Copy, Check, ChevronUp, ChevronDown as ChevronDownIcon, ChevronsUpDown } from 'lucide-react';

const PAGE_SIZE = 100;

// ── Types ─────────────────────────────────────────────────────────────────────
interface NFSeRow {
  id: string;
  identificador_nfse: string;
  cnpj_prestador: string;
  cnpj_tomador: string;
  data_emissao: string;
  mes_ano: string;
  valor_servicos: number;
  base_iss: number; iss: number; aliquota_iss: number; iss_retido: number;
  base_pis: number; pis: number; base_cofins: number; cofins: number;
  inss: number; irrf: number; csll: number;
  // IBS/CBS (leiaute 1.01) — null = grupo IBSCBS ausente no XML (distinto de 0).
  ibscbs_cod_localidade_incid: string | null;
  ibscbs_base: number | null;
  ibs_uf_aliquota: number | null; ibs_uf_aliquota_efetiva: number | null;
  ibs_mun_aliquota: number | null; ibs_mun_aliquota_efetiva: number | null;
  cbs_aliquota: number | null; cbs_aliquota_efetiva: number | null;
  ibscbs_valor_total_nf: number | null;
  ibs_total: number | null; ibs_uf: number | null; ibs_mun: number | null; cbs: number | null;
  ibscbs_fin_nfse: string | null; ibscbs_ind_op: string | null; ibscbs_ind_dest: string | null;
  ibscbs_cst: string | null; ibscbs_cclass_trib: string | null;
}

interface NFSeResponse {
  total: number; page: number; page_size: number; total_pages: number;
  totals: {
    valor_servicos: number; iss: number; pis: number; cofins: number;
    inss: number; irrf: number; csll: number;
    total_ibs: number; total_cbs: number;
  };
  items: NFSeRow[];
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function fmtBRL(v: number | null | undefined, dash = '—'): string {
  if (v == null) return dash;
  return v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });
}

function fmtNum(v: number | null | undefined, dash = '—'): string {
  if (v == null) return dash;
  return v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function fmtCNPJ(v: string): string {
  if (!v) return '—';
  const d = v.replace(/\D/g, '');
  if (d.length === 14) return `${d.slice(0,2)}.${d.slice(2,5)}.${d.slice(5,8)}/${d.slice(8,12)}-${d.slice(12)}`;
  return v || '—';
}

// ── Identificador copiável ──────────────────────────────────────────────────
function CopyIdentificador({ identificador }: { identificador: string }) {
  const [copied, setCopied] = useState(false);
  if (!identificador) return <span className="text-[10px] text-muted-foreground">—</span>;
  const handle = () => {
    if (!navigator.clipboard) return;
    navigator.clipboard.writeText(identificador).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }).catch(() => {});
  };
  return (
    <button onClick={handle}
      title={identificador}
      className="flex items-center gap-1 text-[10px] font-mono text-muted-foreground hover:text-foreground transition-colors">
      <span className="truncate max-w-[80px]">{identificador.slice(0, 8)}…</span>
      {copied
        ? <Check className="h-3 w-3 text-green-500 shrink-0" />
        : <Copy className="h-3 w-3 shrink-0" />}
    </button>
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
function DetalheNFSe({ nfse, onClose }: { nfse: NFSeRow; onClose: () => void }) {
  const Linha = ({ label, value }: { label: string; value: string | number | null | undefined }) => (
    <div className="flex justify-between py-0.5 border-b border-dashed last:border-0">
      <span className="text-[11px] text-muted-foreground w-36 shrink-0">{label}</span>
      <span className="text-[11px] font-medium text-right">{value ?? '—'}</span>
    </div>
  );
  const LinhaBRL = ({ label, value }: { label: string; value: number | null | undefined }) => (
    <div className="flex justify-between py-0.5 border-b border-dashed last:border-0">
      <span className="text-[11px] text-muted-foreground w-36 shrink-0">{label}</span>
      <span className="text-[11px] font-medium text-right">{fmtBRL(value, '—')}</span>
    </div>
  );
  const Secao = ({ title, children }: { title: string; children: React.ReactNode }) => (
    <div className="mb-2">
      <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-1 pb-0.5 border-b">{title}</h3>
      {children}
    </div>
  );
  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="text-xs">
            NFS-e Prestada
            <div className="text-[11px] font-normal text-muted-foreground mt-0.5 break-all">Identificador: {nfse.identificador_nfse}</div>
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-1 mt-1">
          <Secao title="Identificação">
            <Linha label="Data Emissão" value={nfse.data_emissao || '—'} />
            <Linha label="Mês/Ano" value={nfse.mes_ano} />
          </Secao>
          <Secao title="Prestador (Filial)">
            <Linha label="CNPJ" value={fmtCNPJ(nfse.cnpj_prestador)} />
          </Secao>
          <Secao title="Tomador (Cliente)">
            <Linha label="CNPJ" value={fmtCNPJ(nfse.cnpj_tomador)} />
          </Secao>
          <Secao title="Valor do Serviço">
            <LinhaBRL label="Valor dos Serviços" value={nfse.valor_servicos} />
          </Secao>
          <Secao title="Tributos">
            <LinhaBRL label="Base ISS" value={nfse.base_iss} />
            <LinhaBRL label="ISS" value={nfse.iss} />
            <Linha label="Alíquota ISS (%)" value={fmtNum(nfse.aliquota_iss)} />
            <LinhaBRL label="ISS Retido" value={nfse.iss_retido} />
            <LinhaBRL label="Base PIS" value={nfse.base_pis} />
            <LinhaBRL label="PIS" value={nfse.pis} />
            <LinhaBRL label="Base COFINS" value={nfse.base_cofins} />
            <LinhaBRL label="COFINS" value={nfse.cofins} />
            <LinhaBRL label="INSS" value={nfse.inss} />
            <LinhaBRL label="IRRF" value={nfse.irrf} />
            <LinhaBRL label="CSLL" value={nfse.csll} />
          </Secao>
          <Secao title="IBS / CBS">
            <LinhaBRL label="Base IBS/CBS" value={nfse.ibscbs_base} />
            <Linha label="Alíq. IBS UF (%)" value={fmtNum(nfse.ibs_uf_aliquota)} />
            <Linha label="Alíq. Efetiva IBS UF (%)" value={fmtNum(nfse.ibs_uf_aliquota_efetiva)} />
            <Linha label="Alíq. IBS Mun (%)" value={fmtNum(nfse.ibs_mun_aliquota)} />
            <Linha label="Alíq. Efetiva IBS Mun (%)" value={fmtNum(nfse.ibs_mun_aliquota_efetiva)} />
            <Linha label="Alíq. CBS (%)" value={fmtNum(nfse.cbs_aliquota)} />
            <Linha label="Alíq. Efetiva CBS (%)" value={fmtNum(nfse.cbs_aliquota_efetiva)} />
            <LinhaBRL label="IBS UF" value={nfse.ibs_uf} />
            <LinhaBRL label="IBS Municipal" value={nfse.ibs_mun} />
            <LinhaBRL label="IBS Total" value={nfse.ibs_total} />
            <LinhaBRL label="CBS" value={nfse.cbs} />
            <LinhaBRL label="Valor Total NF" value={nfse.ibscbs_valor_total_nf} />
            <Linha label="CST" value={nfse.ibscbs_cst ?? '—'} />
            <Linha label="cClassTrib" value={nfse.ibscbs_cclass_trib ?? '—'} />
            <Linha label="Indicador Operação" value={nfse.ibscbs_ind_op ?? '—'} />
            <Linha label="Local Incidência (IBGE)" value={nfse.ibscbs_cod_localidade_incid ?? '—'} />
          </Secao>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ── Página principal ──────────────────────────────────────────────────────────
export default function ConsultaNFSeSaidas() {
  const { token, companyId } = useAuth();

  const [mesAnoOptions, setMesAnoOptions] = useState<string[]>([]);
  const [mesAno,        setMesAno]        = useState('');
  const [mesAnoLoaded,  setMesAnoLoaded]  = useState(false);
  const [filterTomador, setFilterTomador] = useState('');
  const [filterDataDe,  setFilterDataDe]  = useState('');
  const [filterDataAte, setFilterDataAte] = useState('');
  const [sortCol, setSortCol] = useState('data_emissao');
  const [sortDir, setSortDir] = useState<'asc'|'desc'>('desc');
  const [page, setPage] = useState(1);

  const [tomadorDebounced, setTomadorDebounced] = useState('');
  useEffect(() => {
    const t = setTimeout(() => setTomadorDebounced(filterTomador), 400);
    return () => clearTimeout(t);
  }, [filterTomador]);

  useEffect(() => { setPage(1); }, [companyId, mesAno, tomadorDebounced, filterDataDe, filterDataAte, sortCol, sortDir]);

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

  const [selected, setSelected] = useState<NFSeRow | null>(null);

  const authHeaders = { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId || '' };

  // Reseta o filtro de mês/ano ao trocar de empresa, antes da nova busca de competências —
  // evita manter ativo brevemente (ou indefinidamente, se a nova empresa não tiver aquele mês)
  // o mes_ano da empresa anterior.
  useEffect(() => {
    setMesAno('');
    setMesAnoLoaded(false);
  }, [companyId]);

  useEffect(() => {
    if (!token || !companyId) return;
    let ignore = false;
    fetch('/api/nfse-saidas/competencias', { headers: authHeaders })
      .then(r => r.ok ? r.json() : [])
      .then((meses: string[]) => {
        if (ignore) return;
        setMesAnoOptions(meses);
        setMesAno(prev => prev || meses[0] || '');
        setMesAnoLoaded(true);
      })
      .catch(() => { if (!ignore) setMesAnoLoaded(true); });
    return () => { ignore = true; };
  }, [token, companyId]); // eslint-disable-line react-hooks/exhaustive-deps

  const { data, isFetching, isError } = useQuery<NFSeResponse>({
    queryKey: ['nfse-saidas', companyId, { page, mesAno, tomadorDebounced, filterDataDe, filterDataAte, sortCol, sortDir }],
    queryFn: async () => {
      const params = new URLSearchParams();
      params.set('page', String(page));
      params.set('page_size', String(PAGE_SIZE));
      params.set('sort_by', sortCol);
      params.set('sort_dir', sortDir);
      if (mesAno)        params.set('mes_ano', mesAno);
      if (filterDataDe)  params.set('data_de', filterDataDe);
      if (filterDataAte) params.set('data_ate', filterDataAte);
      if (tomadorDebounced) {
        const digits = tomadorDebounced.replace(/\D/g, '');
        if (digits) params.set('cnpj_tomador', digits);
      }
      const res = await fetch(`/api/nfse-saidas?${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    placeholderData: keepPreviousData,
    enabled: !!token && !!companyId && mesAnoLoaded,
  });

  const items      = data?.items      ?? [];
  const total      = data?.total      ?? 0;
  const totalPages = data?.total_pages ?? 1;
  const totals     = data?.totals     ?? { valor_servicos: 0, iss: 0, pis: 0, cofins: 0, inss: 0, irrf: 0, csll: 0, total_ibs: 0, total_cbs: 0 };

  const hasFilters = !!(filterTomador || filterDataDe || filterDataAte);

  function clearFilters() {
    setFilterTomador('');
    setFilterDataDe(''); setFilterDataAte('');
    setPage(1);
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">NFS-e Saídas (Prestadas)</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Consulta de NFS-e de serviços prestados, capturadas via Portal Nacional ADN. Clique em uma linha para ver todos os dados.
        </p>
      </div>

      <Card>
        <CardContent className="pt-4 space-y-3">
          <div className="flex flex-wrap gap-3 items-end">
            {/* Mês/Ano */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Mês/Ano</label>
              <Select value={mesAno} onValueChange={v => { setMesAno(v); setPage(1); }}>
                <SelectTrigger className="h-8 w-32 text-[11px]">
                  <SelectValue placeholder={!mesAnoLoaded ? 'Carregando...' : mesAnoOptions.length === 0 ? 'Sem períodos' : 'Selecione...'} />
                </SelectTrigger>
                <SelectContent>
                  {mesAnoOptions.map(m => <SelectItem key={m} value={m} className="text-xs">{m}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>

            {/* Tomador */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Tomador (CNPJ)</label>
              <Input placeholder="Digite o CNPJ..." value={filterTomador}
                onChange={e => setFilterTomador(e.target.value)}
                className="h-8 w-44" />
            </div>

            {/* Data De */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Emissão De</label>
              <Input type="date" value={filterDataDe}
                onChange={e => { setFilterDataDe(e.target.value); setPage(1); }}
                className="h-8 w-36" />
            </div>

            {/* Data Até */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Emissão Até</label>
              <Input type="date" value={filterDataAte}
                onChange={e => { setFilterDataAte(e.target.value); setPage(1); }}
                className="h-8 w-36" />
            </div>

            {hasFilters && (
              <Button size="sm" variant="ghost" onClick={clearFilters} className="self-end">
                <X className="h-3 w-3 mr-1" /> Limpar filtros
              </Button>
            )}

            <span className="text-xs text-muted-foreground ml-auto self-end">
              {isFetching ? 'Carregando...' : isError ? '—' : `${total.toLocaleString('pt-BR')} nota(s)`}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* Totalizador */}
      {!isError && total > 0 && (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
          {[
            { label: 'Valor Serviços', value: totals.valor_servicos },
            { label: 'Total ISS',      value: totals.iss },
            { label: 'Total PIS',      value: totals.pis },
            { label: 'Total COFINS',   value: totals.cofins },
            { label: 'Total INSS',     value: totals.inss },
            { label: 'Total IRRF',     value: totals.irrf },
            { label: 'Total CSLL',     value: totals.csll },
            { label: 'Total IBS',      value: totals.total_ibs },
            { label: 'Total CBS',      value: totals.total_cbs },
          ].map(c => (
            <Card key={c.label} className="p-2">
              <p className="text-[10px] text-muted-foreground">{c.label}</p>
              <p className="text-xs font-bold mt-0.5">{fmtBRL(c.value)}</p>
            </Card>
          ))}
        </div>
      )}

      <Card>
        <CardHeader className="py-2 px-4">
          <CardTitle className="text-[11px] text-muted-foreground font-normal">
            Clique em uma linha para ver detalhes
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isError ? (
            <p className="text-xs text-red-500 text-center py-8">Erro ao carregar dados.</p>
          ) : items.length === 0 ? (
            <p className="text-xs text-muted-foreground text-center py-8">
              {isFetching ? 'Carregando...' : 'Nenhum documento encontrado para o período/filtros selecionados.'}
            </p>
          ) : (
            <>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="py-1.5 px-2 text-[11px]">Identificador</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] whitespace-nowrap cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('data_emissao')}>
                        Data <SortIcon col="data_emissao" />
                      </TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Prestador (Filial)</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Tomador</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('valor_servicos')}>
                        Valor Serviços (R$) <SortIcon col="valor_servicos" />
                      </TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('iss')}>
                        ISS (R$) <SortIcon col="iss" />
                      </TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap">ISS Retido (R$)</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('pis')}>
                        PIS (R$) <SortIcon col="pis" />
                      </TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('cofins')}>
                        COFINS (R$) <SortIcon col="cofins" />
                      </TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap">IBS (R$)</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap">CBS (R$)</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap">INSS (R$)</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap">IRRF (R$)</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap">CSLL (R$)</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {items.map(row => (
                      <TableRow key={row.id}
                        className="cursor-pointer hover:bg-muted/50"
                        onClick={() => setSelected(row)}>
                        <TableCell className="py-0.5 px-2" onClick={e => e.stopPropagation()}>
                          <CopyIdentificador identificador={row.identificador_nfse} />
                        </TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] whitespace-nowrap">{row.data_emissao || '—'}</TableCell>
                        <TableCell className="py-0.5 px-2 max-w-[160px]">
                          <div className="truncate text-[11px] font-mono">{fmtCNPJ(row.cnpj_prestador)}</div>
                        </TableCell>
                        <TableCell className="py-0.5 px-2 max-w-[160px]">
                          <div className="truncate text-[11px] font-mono">{fmtCNPJ(row.cnpj_tomador)}</div>
                        </TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right font-semibold whitespace-nowrap">{fmtNum(row.valor_servicos)}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.iss)}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.iss_retido)}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.pis)}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.cofins)}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.ibs_total)}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.cbs)}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.inss)}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.irrf)}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.csll)}</TableCell>
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

      {selected && <DetalheNFSe nfse={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}
