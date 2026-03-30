import { useState, useEffect } from 'react';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
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
import { X, AlertTriangle, Truck, ChevronLeft, ChevronRight, Copy, Check, ChevronUp, ChevronDown as ChevronDownIcon, ChevronsUpDown } from 'lucide-react';
import { formatCnpjComApelido, formatCNPJMasked } from '@/lib/formatFilial';

const PAGE_SIZE = 100;


// ── Types ─────────────────────────────────────────────────────────────────────
interface FilialOption { cnpj: string; nome: string; apelido: string }

interface CteEntradaRow {
  id: string; chave_cte: string; modelo: number; serie: string; numero_cte: string;
  data_emissao: string; data_autorizacao: string; mes_ano: string;
  emit_cnpj: string;
  emit_nome: string;
  dest_cnpj_cpf: string;
  v_prest: number;
  v_bc_ibs_cbs: number; v_ibs_uf: number; v_ibs_mun: number;
  v_ibs: number; v_cbs: number;
  cancelado: string; // "S" | "N"
}

interface CteEntradaResponse {
  total: number; page: number; page_size: number; total_pages: number;
  totals: { v_prest: number; v_ibs: number; v_cbs: number };
  items: CteEntradaRow[];
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
  if (d.length === 11) return `${d.slice(0,3)}.${d.slice(3,6)}.${d.slice(6,9)}-${d.slice(9)}`;
  return v;
}


// ── Chave eletrônica copiável ─────────────────────────────────────────────────
function CopyChave({ chave }: { chave: string }) {
  const [copied, setCopied] = useState(false);
  if (!chave) return <span className="text-[10px] text-muted-foreground">—</span>;
  const handle = () => {
    navigator.clipboard.writeText(chave).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };
  return (
    <button onClick={handle}
      title={chave}
      className="flex items-center gap-1 text-[10px] font-mono text-muted-foreground hover:text-foreground transition-colors">
      <span className="truncate max-w-[80px]">{chave.slice(0, 8)}…</span>
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
function DetalheCTe({ cte, onClose }: { cte: CteEntradaRow; onClose: () => void }) {
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
            CT-e {cte.modelo} · Série {cte.serie} · Nº {cte.numero_cte}
            <div className="text-[11px] font-normal text-muted-foreground mt-0.5 break-all">Chave: {cte.chave_cte}</div>
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-1 mt-1">
          <Secao title="Identificação">
            <Linha label="Modelo" value={cte.modelo} /><Linha label="Série" value={cte.serie} />
            <Linha label="Número" value={cte.numero_cte} /><Linha label="Data Emissão" value={cte.data_emissao} />
            <Linha label="Data Autorização" value={cte.data_autorizacao || '—'} />
            <Linha label="Mês/Ano" value={cte.mes_ano} />
          </Secao>
          <Secao title="Transportadora (Emitente)">
            <Linha label="CNPJ" value={fmtCNPJ(cte.emit_cnpj)} />
          </Secao>
          <Secao title="Destinatário (Filial)">
            <Linha label="CNPJ/CPF" value={fmtCNPJ(cte.dest_cnpj_cpf)} />
          </Secao>
          <Secao title="Valores — Reforma Tributária">
            <LinhaBRL label="vTPrest (Total Prestação)" value={cte.v_prest} />
            <LinhaBRL label="vBCIBSCBS (Base)" value={cte.v_bc_ibs_cbs} />
            <LinhaBRL label="vIBSUF" value={cte.v_ibs_uf} /><LinhaBRL label="vIBSMun" value={cte.v_ibs_mun} />
            <LinhaBRL label="vIBS (Total)" value={cte.v_ibs} />
            <LinhaBRL label="vCBS" value={cte.v_cbs} />
            {(cte.v_ibs == null || cte.v_ibs === 0) && (cte.v_cbs == null || cte.v_cbs === 0) && (
              <div className="flex items-center gap-1 mt-1 text-orange-600">
                <AlertTriangle className="h-3 w-3" />
                <span className="text-[11px]">Transportadora sem IBS/CBS declarado</span>
              </div>
            )}
          </Secao>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ── Página principal ──────────────────────────────────────────────────────────
export default function ConsultaCTesEntradas() {
  const { token, companyId } = useAuth();

  const [mesAnoOptions, setMesAnoOptions] = useState<string[]>([]);
  const [mesAno,        setMesAno]        = useState('');
  const [mesAnoLoaded,  setMesAnoLoaded]  = useState(false);
  const [filterFilial,  setFilterFilial]  = useState('');
  const [filterTransp,  setFilterTransp]  = useState('');
  const [filterDataDe,  setFilterDataDe]  = useState('');
  const [filterDataAte, setFilterDataAte] = useState('');
  const [filterSemIBS,  setFilterSemIBS]  = useState(false);
  const [sortCol, setSortCol] = useState('data_emissao');
  const [sortDir, setSortDir] = useState<'asc'|'desc'>('desc');
  const [page,          setPage]          = useState(1);
  const [filiaisOptions, setFiliaisOptions] = useState<FilialOption[]>([]);

  const [transpDebounced, setTranspDebounced] = useState('');
  useEffect(() => {
    const t = setTimeout(() => setTranspDebounced(filterTransp), 400);
    return () => clearTimeout(t);
  }, [filterTransp]);

  useEffect(() => { setPage(1); }, [mesAno, filterFilial, transpDebounced, filterDataDe, filterDataAte, filterSemIBS, sortCol, sortDir]);

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

  const [selected, setSelected] = useState<CteEntradaRow | null>(null);
  const [apelidos, setApelidos] = useState<Record<string, string>>({});

  const authHeaders = { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId || '' };

  useEffect(() => {
    if (!token || !companyId) return;
    fetch('/api/config/filial-apelidos', { headers: authHeaders })
      .then(r => r.ok ? r.json() : [])
      .then((list: { cnpj: string; apelido: string }[]) => {
        const map: Record<string, string> = {};
        (list || []).forEach(fa => { map[fa.cnpj] = fa.apelido; });
        setApelidos(map);
      })
      .catch(() => {});
    fetch('/api/cte-entradas/competencias', { headers: authHeaders })
      .then(r => r.ok ? r.json() : [])
      .then((meses: string[]) => {
        setMesAnoOptions(meses);
        setMesAno(prev => prev || meses[0] || '');
        setMesAnoLoaded(true);
      })
      .catch(() => { setMesAnoLoaded(true); });
    fetch('/api/cte-entradas/filiais', { headers: authHeaders })
      .then(r => r.ok ? r.json() : [])
      .then((list: FilialOption[]) => setFiliaisOptions(list || []))
      .catch(() => {});
  }, [token, companyId]); // eslint-disable-line react-hooks/exhaustive-deps

  const { data, isFetching, isError } = useQuery<CteEntradaResponse>({
    queryKey: ['cte-entradas', companyId, { page, mesAno, filterFilial, transpDebounced, filterDataDe, filterDataAte, filterSemIBS, sortCol, sortDir }],
    queryFn: async () => {
      const params = new URLSearchParams();
      params.set('page', String(page));
      params.set('page_size', String(PAGE_SIZE));
      params.set('sort_by', sortCol);
      params.set('sort_dir', sortDir);
      if (mesAno)        params.set('mes_ano',    mesAno);
      if (filterFilial)  params.set('dest_cnpj',  filterFilial);
      if (filterDataDe)  params.set('data_de',    filterDataDe);
      if (filterDataAte) params.set('data_ate',   filterDataAte);
      if (filterSemIBS)  params.set('sem_ibs_cbs', 'true');
      if (transpDebounced) {
        const digits = transpDebounced.replace(/\D/g, '');
        if (digits) params.set('emit_cnpj', digits);
      }
      const res = await fetch(`/api/cte-entradas?${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    placeholderData: keepPreviousData,
    enabled: !!token && !!companyId && mesAnoLoaded,
  });

  const items      = data?.items      ?? [];
  const total      = data?.total      ?? 0;
  const totalPages = data?.total_pages ?? 1;
  const totals     = data?.totals     ?? { v_prest: 0, v_ibs: 0, v_cbs: 0 };

  const hasFilters = !!(filterFilial || filterTransp || filterDataDe || filterDataAte || filterSemIBS);

  function clearFilters() {
    setFilterFilial(''); setFilterTransp(''); setTranspDebounced('');
    setFilterDataDe(''); setFilterDataAte(''); setFilterSemIBS(false);
    setPage(1);
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">CT-e de Entrada</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Consulta de Conhecimentos de Transporte Eletrônico de entrada. Clique em uma linha para ver todos os dados.
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

            {/* Filial (destinatário) */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Filial</label>
              <Select value={filterFilial || 'all'} onValueChange={v => { setFilterFilial(v === 'all' ? '' : v); setPage(1); }}>
                <SelectTrigger className="h-8 w-52 text-[11px]">
                  <SelectValue placeholder="Todas as filiais" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all" className="text-xs">Todas as filiais</SelectItem>
                  {filiaisOptions.map(f => (
                    <SelectItem key={f.cnpj} value={f.cnpj} className="text-xs">
                      {formatCNPJMasked(f.cnpj)}{(f.apelido || f.nome) ? ` — ${f.apelido || f.nome}` : ''}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Transportadora */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Transportadora (CNPJ)</label>
              <Input placeholder="Digite o CNPJ..." value={filterTransp}
                onChange={e => setFilterTransp(e.target.value)}
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

            {/* Sem IBS+CBS */}
            <Button size="sm" variant={filterSemIBS ? 'default' : 'outline'}
              onClick={() => setFilterSemIBS(v => !v)}
              className={filterSemIBS ? 'bg-orange-600 hover:bg-orange-700 text-white self-end' : 'text-orange-600 border-orange-300 hover:bg-orange-50 self-end'}>
              <AlertTriangle className="h-3 w-3 mr-1" />
              Sem IBS+CBS
            </Button>

            {hasFilters && (
              <Button size="sm" variant="ghost" onClick={clearFilters} className="self-end">
                <X className="h-3 w-3 mr-1" /> Limpar filtros
              </Button>
            )}

            <span className="text-xs text-muted-foreground ml-auto self-end">
              {isFetching ? 'Carregando...' : `${total.toLocaleString('pt-BR')} CT-e(s)`}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* Totalizador */}
      {total > 0 && (
        <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
          {[
            { label: 'Total vPrest', value: totals.v_prest },
            { label: 'Total vIBS',  value: totals.v_ibs },
            { label: 'Total vCBS',  value: totals.v_cbs },
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
          <CardTitle className="flex items-center gap-2 text-[11px] text-muted-foreground font-normal">
            <Truck className="h-3.5 w-3.5" />
            Clique em uma linha para ver detalhes
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isError ? (
            <p className="text-xs text-red-500 text-center py-8">Erro ao carregar dados.</p>
          ) : items.length === 0 ? (
            <p className="text-xs text-muted-foreground text-center py-8">
              {isFetching ? 'Carregando...' : 'Nenhum CT-e encontrado para o período/filtros selecionados.'}
            </p>
          ) : (
            <>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="py-1.5 px-2 text-[11px] whitespace-nowrap">Série/Nº</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] whitespace-nowrap cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('data_emissao')}>
                        Data <SortIcon col="data_emissao" />
                      </TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Transportadora</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Destinatário (Filial)</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Chave</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('v_prest')}>
                        vPrest (R$) <SortIcon col="v_prest" />
                      </TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('v_ibs')}>
                        IBS (R$) <SortIcon col="v_ibs" />
                      </TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap">IBS Mun. (R$)</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap cursor-pointer select-none hover:text-foreground" onClick={() => handleSort('v_cbs')}>
                        CBS (R$) <SortIcon col="v_cbs" />
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {items.map(row => {
                      const semCredito = (row.v_ibs == null || row.v_ibs === 0) && (row.v_cbs == null || row.v_cbs === 0);
                      return (
                        <TableRow key={row.id}
                          className={`cursor-pointer hover:bg-muted/50 ${semCredito ? 'bg-orange-50/50 dark:bg-orange-950/10' : ''} ${row.cancelado === 'S' ? 'opacity-60' : ''}`}
                          onClick={() => setSelected(row)}>
                          <TableCell className="py-0.5 px-2 text-[11px] font-mono whitespace-nowrap">
                            {row.serie}/{row.numero_cte}
                            {row.cancelado === 'S' && (
                              <Badge variant="outline" className="ml-1.5 text-[9px] px-1 py-0 bg-red-50 text-red-600 border-red-200 align-middle">Cancelada</Badge>
                            )}
                          </TableCell>
                          <TableCell className="py-0.5 px-2 text-[11px] whitespace-nowrap">{row.data_emissao}</TableCell>
                          <TableCell className="py-0.5 px-2 max-w-[180px]">
                            <div className="truncate text-[11px] font-medium">
                              {row.emit_nome || fmtCNPJ(row.emit_cnpj)}
                            </div>
                            {row.emit_nome && (
                              <div className="truncate text-[10px] font-mono text-muted-foreground">
                                {fmtCNPJ(row.emit_cnpj)}
                              </div>
                            )}
                          </TableCell>
                          <TableCell className="py-0.5 px-2 max-w-[180px]">
                            <div className="truncate text-[11px]">
                              {formatCnpjComApelido(row.dest_cnpj_cpf, apelidos)}
                            </div>
                          </TableCell>
                          <TableCell className="py-0.5 px-2" onClick={e => e.stopPropagation()}>
                            <CopyChave chave={row.chave_cte} />
                          </TableCell>
                          <TableCell className="py-0.5 px-2 text-[11px] text-right font-semibold whitespace-nowrap">{fmtNum(row.v_prest)}</TableCell>
                          <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.v_ibs_uf)}</TableCell>
                          <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.v_ibs_mun)}</TableCell>
                          <TableCell className="py-0.5 px-2 text-[11px] text-right whitespace-nowrap tabular-nums">{fmtNum(row.v_cbs)}</TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
              <Pagination page={page} pageCount={totalPages} onChange={setPage} />
            </>
          )}
        </CardContent>
      </Card>

      {selected && <DetalheCTe cte={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}
