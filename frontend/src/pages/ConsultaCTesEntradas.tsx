import { useState, useEffect } from 'react';
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
import { X, AlertTriangle, Truck, ChevronLeft, ChevronRight } from 'lucide-react';

const PAGE_SIZE = 100;

function buildMesAnoOptions() {
  const opts: { value: string; label: string }[] = [];
  const now = new Date();
  for (let i = 0; i < 24; i++) {
    const d = new Date(now.getFullYear(), now.getMonth() - i, 1);
    const mm = String(d.getMonth() + 1).padStart(2, '0');
    const yyyy = String(d.getFullYear());
    opts.push({ value: `${mm}/${yyyy}`, label: `${mm}/${yyyy}` });
  }
  return opts;
}
const MES_ANO_OPTIONS = buildMesAnoOptions();

// ── Types ─────────────────────────────────────────────────────────────────────
interface CteEntradaRow {
  id: string; chave_cte: string; modelo: number; serie: string; numero_cte: string;
  data_emissao: string; mes_ano: string; nat_op: string; cfop: string; modal: string;
  emit_cnpj: string; emit_nome: string; emit_uf: string;
  rem_cnpj_cpf: string; rem_nome: string; rem_uf: string;
  dest_cnpj_cpf: string; dest_nome: string; dest_uf: string;
  v_prest: number; v_rec: number; v_carga: number; v_bc_icms: number; v_icms: number;
  v_bc_ibs_cbs: number | null; v_ibs: number | null; v_cbs: number | null;
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
function fmtCNPJ(v: string): string {
  if (!v) return '—';
  const d = v.replace(/\D/g, '');
  if (d.length === 14) return `${d.slice(0,2)}.${d.slice(2,5)}.${d.slice(5,8)}/${d.slice(8,12)}-${d.slice(12)}`;
  if (d.length === 11) return `${d.slice(0,3)}.${d.slice(3,6)}.${d.slice(6,9)}-${d.slice(9)}`;
  return v;
}
const MODAL_LABELS: Record<string, string> = { '01':'Rodoviário','02':'Aéreo','03':'Aquaviário','04':'Ferroviário','05':'Dutoviário','06':'Multimodal' };
function fmtModal(m: string): string { return MODAL_LABELS[m] || m || '—'; }

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
          className="w-14 h-7 rounded border border-input bg-background px-2 text-center text-xs focus:outline-none focus:ring-1 focus:ring-ring"
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
            <Linha label="Mês/Ano" value={cte.mes_ano} /><Linha label="Natureza Operação" value={cte.nat_op} />
            <Linha label="CFOP" value={cte.cfop} /><Linha label="Modal" value={fmtModal(cte.modal)} />
          </Secao>
          <Secao title="Transportadora (Emitente)">
            <Linha label="CNPJ" value={fmtCNPJ(cte.emit_cnpj)} /><Linha label="Razão Social" value={cte.emit_nome} />
            <Linha label="UF" value={cte.emit_uf} />
          </Secao>
          <Secao title="Remetente">
            <Linha label="CNPJ/CPF" value={fmtCNPJ(cte.rem_cnpj_cpf)} /><Linha label="Nome/Razão Social" value={cte.rem_nome} />
            <Linha label="UF" value={cte.rem_uf} />
          </Secao>
          <Secao title="Destinatário">
            <Linha label="CNPJ/CPF" value={fmtCNPJ(cte.dest_cnpj_cpf)} /><Linha label="Nome/Razão Social" value={cte.dest_nome} />
            <Linha label="UF" value={cte.dest_uf} />
          </Secao>
          <Secao title="Prestação e Carga">
            <LinhaBRL label="vTPrest (Total)" value={cte.v_prest} /><LinhaBRL label="vRec (A Receber)" value={cte.v_rec} />
            <LinhaBRL label="vCarga" value={cte.v_carga} /><LinhaBRL label="vBC ICMS" value={cte.v_bc_icms} />
            <LinhaBRL label="vICMS" value={cte.v_icms} />
          </Secao>
          <Secao title="IBSCBSTot — Reforma Tributária">
            <LinhaBRL label="vBCIBSCBS (Base)" value={cte.v_bc_ibs_cbs} />
            <LinhaBRL label="vIBS" value={cte.v_ibs} /><LinhaBRL label="vCBS" value={cte.v_cbs} />
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

  const defaultMes = MES_ANO_OPTIONS[0]?.value ?? '';
  const [mesAno,       setMesAno]       = useState(defaultMes);
  const [filterTransp, setFilterTransp] = useState('');
  const [filterDataDe, setFilterDataDe] = useState('');
  const [filterDataAte, setFilterDataAte] = useState('');
  const [filterSemIBS, setFilterSemIBS] = useState(false);
  const [page, setPage] = useState(1);

  const [transpDebounced, setTranspDebounced] = useState('');
  useEffect(() => {
    const t = setTimeout(() => setTranspDebounced(filterTransp), 400);
    return () => clearTimeout(t);
  }, [filterTransp]);

  useEffect(() => { setPage(1); }, [mesAno, transpDebounced, filterDataDe, filterDataAte, filterSemIBS]);

  const [selected, setSelected] = useState<CteEntradaRow | null>(null);

  const authHeaders = { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId || '' };

  const { data, isFetching, isError } = useQuery<CteEntradaResponse>({
    queryKey: ['cte-entradas', companyId, { page, mesAno, transpDebounced, filterDataDe, filterDataAte, filterSemIBS }],
    queryFn: async () => {
      const params = new URLSearchParams();
      params.set('page', String(page));
      params.set('page_size', String(PAGE_SIZE));
      if (mesAno)       params.set('mes_ano',    mesAno);
      if (filterDataDe) params.set('data_de',    filterDataDe);
      if (filterDataAte) params.set('data_ate',  filterDataAte);
      if (filterSemIBS)  params.set('sem_ibs_cbs', 'true');
      if (transpDebounced) {
        const digits = transpDebounced.replace(/\D/g, '');
        if (digits && digits === transpDebounced.replace(/[.\-/]/g, '').replace(/\s/g, '')) {
          params.set('emit_cnpj_search', digits);
        } else {
          params.set('emit_nome', transpDebounced);
        }
      }
      const res = await fetch(`/api/cte-entradas?${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    placeholderData: keepPreviousData,
    enabled: !!token && !!companyId,
  });

  const items      = data?.items      ?? [];
  const total      = data?.total      ?? 0;
  const totalPages = data?.total_pages ?? 1;
  const totals     = data?.totals     ?? { v_prest: 0, v_ibs: 0, v_cbs: 0 };

  const hasFilters = !!(filterTransp || filterDataDe || filterDataAte || filterSemIBS);

  function clearFilters() {
    setFilterTransp(''); setFilterDataDe(''); setFilterDataAte(''); setFilterSemIBS(false);
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
                <SelectTrigger className="h-8 w-32 text-[11px]"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {MES_ANO_OPTIONS.map(o => <SelectItem key={o.value} value={o.value} className="text-xs">{o.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>

            {/* Transportadora */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Transportadora (nome ou CNPJ)</label>
              <Input placeholder="Digite nome ou documento..." value={filterTransp}
                onChange={e => setFilterTransp(e.target.value)}
                className="h-8 w-60" />
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
            Clique em uma linha para ver todos os dados do CT-e
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
                      <TableHead className="py-1.5 px-2 text-[11px]">CNPJ Transportadora</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Transportadora / UF</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Remetente</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Destinatário</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Data</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-center">Série</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-center">Nº CT-e</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Modal</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right">vPrest</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right">vIBS</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right">vCBS</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {items.map(row => {
                      const semCredito = (row.v_ibs == null || row.v_ibs === 0) && (row.v_cbs == null || row.v_cbs === 0);
                      return (
                        <TableRow key={row.id}
                          className={`cursor-pointer hover:bg-muted/50 h-8 ${semCredito ? 'bg-orange-50/50 dark:bg-orange-950/10' : ''}`}
                          onClick={() => setSelected(row)}>
                          <TableCell className="py-1 px-2 font-mono text-[11px]">{fmtCNPJ(row.emit_cnpj)}</TableCell>
                          <TableCell className="py-1 px-2">
                            <div className="text-[11px] font-medium leading-tight">{row.emit_nome || '—'}</div>
                            <div className="text-[10px] text-muted-foreground leading-tight">{row.emit_uf}</div>
                          </TableCell>
                          <TableCell className="py-1 px-2">
                            <div className="text-[11px] leading-tight">{row.rem_nome || '—'}</div>
                            <div className="text-[10px] text-muted-foreground font-mono leading-tight">{row.rem_uf}</div>
                          </TableCell>
                          <TableCell className="py-1 px-2">
                            <div className="text-[11px] leading-tight">{row.dest_nome || '—'}</div>
                            <div className="text-[10px] text-muted-foreground font-mono leading-tight">{row.dest_uf}</div>
                          </TableCell>
                          <TableCell className="py-1 px-2 text-[11px] whitespace-nowrap">{row.data_emissao}</TableCell>
                          <TableCell className="py-1 px-2 text-[11px] text-center">{row.serie}</TableCell>
                          <TableCell className="py-1 px-2 text-[11px] text-center font-mono">{row.numero_cte}</TableCell>
                          <TableCell className="py-1 px-2">
                            <Badge variant="outline" className="text-[10px] px-1 py-0">{fmtModal(row.modal)}</Badge>
                          </TableCell>
                          <TableCell className="py-1 px-2 text-[11px] text-right font-semibold">{fmtBRL(row.v_prest)}</TableCell>
                          <TableCell className="py-1 px-2 text-[11px] text-right">
                            {row.v_ibs != null ? fmtBRL(row.v_ibs) : <span className="text-orange-500 font-medium">—</span>}
                          </TableCell>
                          <TableCell className="py-1 px-2 text-[11px] text-right">
                            {row.v_cbs != null ? fmtBRL(row.v_cbs) : <span className="text-orange-500 font-medium">—</span>}
                          </TableCell>
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
