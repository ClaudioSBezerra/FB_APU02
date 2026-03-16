import { useState, useEffect } from 'react';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';
import { toast } from 'sonner';
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
import { X, Copy, Check, FileText, ChevronLeft, ChevronRight } from 'lucide-react';
import { formatCnpjComApelido } from '@/lib/formatFilial';

const PAGE_SIZE = 100;

// ── Meses disponíveis ─────────────────────────────────────────────────────────
function buildMesAnoOptions(): { value: string; label: string }[] {
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
interface NfeSaidaRow {
  id: string;
  chave_nfe: string;
  modelo: number;
  serie: string;
  numero_nfe: string;
  data_emissao: string;
  mes_ano: string;
  nat_op: string;
  emit_cnpj: string;
  emit_nome: string;
  emit_uf: string;
  emit_municipio: string;
  dest_cnpj_cpf: string;
  dest_nome: string;
  dest_uf: string;
  dest_c_mun: string;
  v_bc: number; v_icms: number; v_icms_deson: number; v_fcp: number;
  v_bc_st: number; v_st: number; v_fcp_st: number; v_fcp_st_ret: number;
  v_prod: number; v_frete: number; v_seg: number; v_desc: number;
  v_ii: number; v_ipi: number; v_ipi_devol: number;
  v_pis: number; v_cofins: number; v_outro: number; v_nf: number;
  v_bc_ibs_cbs: number | null; v_ibs_uf: number | null; v_ibs_mun: number | null;
  v_ibs: number | null; v_cred_pres_ibs: number | null;
  v_cbs: number | null; v_cred_pres_cbs: number | null;
}

interface NfeSaidaResponse {
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
  totals: { v_nf: number; v_icms: number; v_ibs: number; v_cbs: number };
  items: NfeSaidaRow[];
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

async function openDanfe(chave: string, token: string | null, companyId: string | null) {
  const res = await fetch(`/api/danfe/${chave}`, {
    headers: { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId || '' },
  });
  if (res.status === 404) { toast.error('XML desta NF-e não encontrado. Importe o XML primeiro.'); return; }
  if (!res.ok) { toast.error('Erro ao gerar DANFE.'); return; }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const win = window.open(url, '_blank');
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
  if (!win) toast.warning('Permita popups para visualizar o DANFE.');
}

function CopyChaveButton({ chave }: { chave: string }) {
  const [copied, setCopied] = useState(false);
  const copy = (e: React.MouseEvent) => {
    e.stopPropagation();
    navigator.clipboard.writeText(chave).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };
  return (
    <button onClick={copy} title="Copiar chave"
      className="text-muted-foreground hover:text-foreground transition-colors shrink-0">
      {copied ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
    </button>
  );
}

// ── Paginação com input de página ─────────────────────────────────────────────
function Pagination({
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
    <div className="flex items-center justify-center gap-2 py-3">
      <Button size="sm" variant="outline" className="h-7 w-7 p-0"
        disabled={page === 1} onClick={() => onChange(page - 1)}>
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
      <Button size="sm" variant="outline" className="h-7 w-7 p-0"
        disabled={page === pageCount} onClick={() => onChange(page + 1)}>
        <ChevronRight className="h-3 w-3" />
      </Button>
    </div>
  );
}

// ── Detalhe (Dialog) ──────────────────────────────────────────────────────────
function DetalheNFe({ nfe, onClose }: { nfe: NfeSaidaRow; onClose: () => void }) {
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
            NF-e {nfe.modelo} · Série {nfe.serie} · Nº {nfe.numero_nfe}
            <div className="text-[11px] font-normal text-muted-foreground mt-0.5 break-all">Chave: {nfe.chave_nfe}</div>
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-1 mt-1">
          <Secao title="Identificação">
            <Linha label="Modelo" value={nfe.modelo} />
            <Linha label="Série" value={nfe.serie} />
            <Linha label="Número" value={nfe.numero_nfe} />
            <Linha label="Data Emissão" value={nfe.data_emissao} />
            <Linha label="Mês/Ano" value={nfe.mes_ano} />
            <Linha label="Natureza Operação" value={nfe.nat_op} />
          </Secao>
          <Secao title="Emitente (Filial)">
            <Linha label="CNPJ" value={fmtCNPJ(nfe.emit_cnpj)} />
            <Linha label="Razão Social" value={nfe.emit_nome} />
            <Linha label="Município" value={nfe.emit_municipio} />
            <Linha label="UF" value={nfe.emit_uf} />
          </Secao>
          <Secao title="Destinatário (Cliente)">
            <Linha label="CNPJ/CPF" value={fmtCNPJ(nfe.dest_cnpj_cpf)} />
            <Linha label="Nome/Razão Social" value={nfe.dest_nome} />
            <Linha label="UF" value={nfe.dest_uf} />
            <Linha label="Município (IBGE)" value={nfe.dest_c_mun} />
          </Secao>
          <Secao title="ICMSTot — Totais da Nota">
            <LinhaBRL label="vProd" value={nfe.v_prod} />
            <LinhaBRL label="vFrete" value={nfe.v_frete} />
            <LinhaBRL label="vSeg" value={nfe.v_seg} />
            <LinhaBRL label="vDesc" value={nfe.v_desc} />
            <LinhaBRL label="vII" value={nfe.v_ii} />
            <LinhaBRL label="vIPI" value={nfe.v_ipi} />
            <LinhaBRL label="vIPIDevol" value={nfe.v_ipi_devol} />
            <LinhaBRL label="vPIS" value={nfe.v_pis} />
            <LinhaBRL label="vCOFINS" value={nfe.v_cofins} />
            <LinhaBRL label="vOutro" value={nfe.v_outro} />
            <LinhaBRL label="vNF (Valor Total)" value={nfe.v_nf} />
            <LinhaBRL label="vBC (Base ICMS)" value={nfe.v_bc} />
            <LinhaBRL label="vICMS" value={nfe.v_icms} />
            <LinhaBRL label="vICMSDeson" value={nfe.v_icms_deson} />
            <LinhaBRL label="vFCP" value={nfe.v_fcp} />
            <LinhaBRL label="vBCST" value={nfe.v_bc_st} />
            <LinhaBRL label="vST" value={nfe.v_st} />
            <LinhaBRL label="vFCPST" value={nfe.v_fcp_st} />
            <LinhaBRL label="vFCPSTRet" value={nfe.v_fcp_st_ret} />
          </Secao>
          <Secao title="IBSCBSTot — Reforma Tributária">
            <LinhaBRL label="vBCIBSCBS (Base)" value={nfe.v_bc_ibs_cbs} />
            <LinhaBRL label="vIBSUF" value={nfe.v_ibs_uf} />
            <LinhaBRL label="vIBSMun" value={nfe.v_ibs_mun} />
            <LinhaBRL label="vIBS (Total)" value={nfe.v_ibs} />
            <LinhaBRL label="vCredPres IBS" value={nfe.v_cred_pres_ibs} />
            <LinhaBRL label="vCBS" value={nfe.v_cbs} />
            <LinhaBRL label="vCredPres CBS" value={nfe.v_cred_pres_cbs} />
          </Secao>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ── Página principal ──────────────────────────────────────────────────────────
export default function ConsultaNFeSaidas() {
  const { token, companyId } = useAuth();

  // Mês obrigatório — sem seleção não carrega dados
  const defaultMes = MES_ANO_OPTIONS[0]?.value ?? '';
  const [mesAno,       setMesAno]       = useState(defaultMes);
  const [filterFilial, setFilterFilial] = useState('');
  const [filterModelo, setFilterModelo] = useState('');
  const [filterCliente, setFilterCliente] = useState('');
  const [filterDataDe, setFilterDataDe] = useState('');
  const [filterDataAte, setFilterDataAte] = useState('');
  const [page, setPage] = useState(1);

  // Debounce do campo de cliente para não disparar query a cada tecla
  const [clienteDebounced, setClienteDebounced] = useState('');
  useEffect(() => {
    const t = setTimeout(() => setClienteDebounced(filterCliente), 400);
    return () => clearTimeout(t);
  }, [filterCliente]);

  // Reset página quando qualquer filtro muda
  useEffect(() => { setPage(1); }, [mesAno, filterFilial, filterModelo, clienteDebounced, filterDataDe, filterDataAte]);

  const [selected, setSelected] = useState<NfeSaidaRow | null>(null);
  const [apelidos, setApelidos] = useState<Record<string, string>>({});

  const authHeaders = {
    Authorization: `Bearer ${token}`,
    'X-Company-ID': companyId || '',
  };

  // Carrega apelidos de filiais
  useEffect(() => {
    if (!token) return;
    fetch('/api/config/filial-apelidos', { headers: authHeaders })
      .then(r => r.ok ? r.json() : [])
      .then((list: { cnpj: string; apelido: string }[]) => {
        const map: Record<string, string> = {};
        (list || []).forEach(fa => { map[fa.cnpj] = fa.apelido; });
        setApelidos(map);
      })
      .catch(() => {});
  }, [token, companyId]); // eslint-disable-line react-hooks/exhaustive-deps

  // ── React Query: busca server-side ───────────────────────────────────────
  const { data, isFetching, isError } = useQuery<NfeSaidaResponse>({
    queryKey: ['nfe-saidas', companyId, {
      page, mesAno, filterFilial, filterModelo,
      clienteDebounced, filterDataDe, filterDataAte,
    }],
    queryFn: async () => {
      const params = new URLSearchParams();
      params.set('page', String(page));
      params.set('page_size', String(PAGE_SIZE));
      if (mesAno)             params.set('mes_ano',   mesAno);
      if (filterFilial)       params.set('emit_cnpj', filterFilial);
      if (filterModelo)       params.set('modelo',    filterModelo);
      if (filterDataDe)       params.set('data_de',   filterDataDe);
      if (filterDataAte)      params.set('data_ate',  filterDataAte);
      if (clienteDebounced) {
        const digits = clienteDebounced.replace(/\D/g, '');
        // Se for apenas dígitos → busca por CNPJ/CPF; senão → busca por nome
        if (digits && digits === clienteDebounced.replace(/[.\-/]/g, '').replace(/\s/g, '')) {
          params.set('dest_cnpj', digits);
        } else {
          params.set('dest_nome', clienteDebounced);
        }
      }
      const res = await fetch(`/api/nfe-saidas?${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    placeholderData: keepPreviousData,
    enabled: !!token && !!companyId,
  });

  const items      = data?.items      ?? [];
  const total      = data?.total      ?? 0;
  const totalPages = data?.total_pages ?? 1;
  const totals     = data?.totals     ?? { v_nf: 0, v_icms: 0, v_ibs: 0, v_cbs: 0 };

  const hasFilters = !!(filterFilial || filterModelo || filterCliente || filterDataDe || filterDataAte);

  function clearFilters() {
    setFilterFilial(''); setFilterModelo('');
    setFilterCliente(''); setFilterDataDe(''); setFilterDataAte('');
    setPage(1);
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Notas de Saída</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Consulta de NF-e e NFC-e de saída importadas via XML. Clique em uma linha para ver todos os dados.
        </p>
      </div>

      {/* ── Filtros ── */}
      <Card>
        <CardContent className="pt-4 space-y-3">
          <div className="flex flex-wrap gap-3 items-end">

            {/* Mês/Ano — obrigatório */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Mês/Ano</label>
              <Select value={mesAno} onValueChange={v => { setMesAno(v); setPage(1); }}>
                <SelectTrigger className="h-8 w-32 text-[11px]">
                  <SelectValue placeholder="Selecione..." />
                </SelectTrigger>
                <SelectContent>
                  {MES_ANO_OPTIONS.map(o => (
                    <SelectItem key={o.value} value={o.value} className="text-xs">{o.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Filial */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Filial (CNPJ)</label>
              <Input placeholder="CNPJ da filial..."
                value={filterFilial}
                onChange={e => { setFilterFilial(e.target.value.replace(/\D/g, '')); setPage(1); }}
                className="h-8 w-40 font-mono text-xs" />
            </div>

            {/* Modelo */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Modelo</label>
              <Select value={filterModelo || 'all'} onValueChange={v => { setFilterModelo(v === 'all' ? '' : v); setPage(1); }}>
                <SelectTrigger className="h-8 w-36 text-[11px]">
                  <SelectValue placeholder="Todos" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todos</SelectItem>
                  <SelectItem value="55">55 — NF-e</SelectItem>
                  <SelectItem value="65">65 — NFC-e</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {/* Cliente */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Cliente (nome ou CNPJ/CPF)</label>
              <Input placeholder="Digite nome ou documento..."
                value={filterCliente}
                onChange={e => setFilterCliente(e.target.value)}
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

            {hasFilters && (
              <Button size="sm" variant="ghost" onClick={clearFilters} className="self-end">
                <X className="h-3 w-3 mr-1" />
                Limpar filtros
              </Button>
            )}

            <span className="text-xs text-muted-foreground ml-auto self-end">
              {isFetching ? 'Carregando...' : `${total.toLocaleString('pt-BR')} nota(s)`}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* ── Totalizador ── */}
      {total > 0 && (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
          {[
            { label: 'Total vNF',   value: totals.v_nf },
            { label: 'Total vICMS', value: totals.v_icms },
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

      {/* ── Tabela ── */}
      <Card>
        <CardHeader className="py-2 px-4">
          <CardTitle className="text-[11px] text-muted-foreground font-normal">
            Clique em uma linha para ver detalhes · Botão DANFE abre o documento fiscal
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isError ? (
            <p className="text-xs text-red-500 text-center py-8">Erro ao carregar dados. Tente recarregar a página.</p>
          ) : items.length === 0 ? (
            <p className="text-xs text-muted-foreground text-center py-8">
              {isFetching ? 'Carregando...' : 'Nenhuma nota encontrada para o período/filtros selecionados.'}
            </p>
          ) : (
            <>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="py-1.5 px-2 text-[11px]">Filial / UF</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Cliente</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Data</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-center">Série</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-center">Nº Nota</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-center">Mod</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Chave Eletrônica</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right">Valor Total</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-center">DANFE</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {items.map(row => (
                      <TableRow key={row.id}
                        className="cursor-pointer hover:bg-muted/50"
                        onClick={() => setSelected(row)}>
                        <TableCell className="py-0.5 px-2">
                          <div className="flex items-baseline gap-1">
                            <span className="text-[10px] font-semibold text-muted-foreground w-6 shrink-0">{row.emit_uf}</span>
                            <span className="text-[11px] font-medium leading-none truncate max-w-[140px]">
                              {formatCnpjComApelido(row.emit_cnpj, apelidos)}
                            </span>
                          </div>
                        </TableCell>
                        <TableCell className="py-0.5 px-2">
                          <div className="flex items-baseline gap-1">
                            <span className="font-mono text-[10px] text-muted-foreground shrink-0 whitespace-nowrap">
                              {fmtCNPJ(row.dest_cnpj_cpf)}
                            </span>
                            <span className="text-[11px] font-medium leading-none truncate max-w-[160px]">
                              {row.dest_nome || '—'}
                            </span>
                          </div>
                        </TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] whitespace-nowrap">{row.data_emissao}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-center">{row.serie}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-center font-mono">{row.numero_nfe}</TableCell>
                        <TableCell className="py-0.5 px-2 text-center">
                          <Badge variant="outline" className="text-[10px] px-1 py-0">{row.modelo}</Badge>
                        </TableCell>
                        <TableCell className="py-0.5 px-2" onClick={e => e.stopPropagation()}>
                          <div className="flex items-center gap-1">
                            <span className="font-mono text-[10px] text-muted-foreground select-all">{row.chave_nfe}</span>
                            <CopyChaveButton chave={row.chave_nfe} />
                          </div>
                        </TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right font-semibold">{fmtBRL(row.v_nf)}</TableCell>
                        <TableCell className="py-0.5 px-2 text-center" onClick={e => e.stopPropagation()}>
                          <button title="Gerar DANFE"
                            onClick={() => openDanfe(row.chave_nfe, token, companyId)}
                            className="text-muted-foreground hover:text-foreground transition-colors">
                            <FileText className="h-3.5 w-3.5" />
                          </button>
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

      {selected && <DetalheNFe nfe={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}
