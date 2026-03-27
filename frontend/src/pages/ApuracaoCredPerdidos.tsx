import { useState, useEffect } from 'react';
import { useAuth } from '@/contexts/AuthContext';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
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
import { AlertTriangle, RefreshCw, ShieldAlert, TrendingDown, Info, Loader2 } from 'lucide-react';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------
interface Aliquotas { ano: number; ibs: number; cbs: number; }

interface FornNFe {
  forn_cnpj: string;
  forn_nome: string;
  qtd_notas: number;
  valor_total: number;
  ibs_estimado: number;
  cbs_estimado: number;
  total_estimado: number;
}

interface FornSimples {
  forn_cnpj: string;
  forn_nome: string;
  valor_total: number;
  ibs_perdido: number;
  cbs_perdido: number;
  total_perdido: number;
}

interface NFeSemCredito {
  total_notas: number;
  total_universe: number;
  perc_sem_credito: number;
  valor_total: number;
  ibs_estimado: number;
  cbs_estimado: number;
  total_estimado: number;
  por_fornecedor: FornNFe[];
}

interface SimplesNacional {
  total_fornecedores: number;
  valor_total: number;
  ibs_perdido: number;
  cbs_perdido: number;
  total_perdido: number;
  por_fornecedor: FornSimples[];
}

interface FornCTe {
  emit_cnpj: string;
  emit_nome: string;
  qtd_ctes: number;
  valor_total: number;
  ibs_estimado: number;
  cbs_estimado: number;
  total_estimado: number;
}

interface CTeSemCredito {
  total_ctes: number;
  total_universe: number;
  perc_sem_credito: number;
  valor_total: number;
  ibs_estimado: number;
  cbs_estimado: number;
  total_estimado: number;
  por_transportadora: FornCTe[];
}

interface CreditosPerdidosData {
  meses_disponiveis: string[];
  mes_selecionado: string;
  aliquotas: Aliquotas;
  nfe_sem_credito: NFeSemCredito;
  simples_nacional: SimplesNacional;
  cte_sem_credito: CTeSemCredito;
  total_credito_em_risco: number;
}

interface NotaDrillDown {
  filial: string;
  filial_cnpj: string;
  chave: string;
  data_emissao: string;
  serie: string;
  numero: string;
  valor: number;
}

interface DrillDownState {
  open: boolean;
  titulo: string;
  forn_cnpj: string;
  tipo: 'nfe' | 'cte';
  notas: NotaDrillDown[];
  loading: boolean;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
const fmtBRL = (v: number) =>
  v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });

const fmtNum = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 });

const fmtPct = (v: number) =>
  `${v.toLocaleString('pt-BR', { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%`;

function fmtCNPJ(v: string) {
  const d = (v || '').replace(/\D/g, '');
  if (d.length === 14)
    return `${d.slice(0,2)}.${d.slice(2,5)}.${d.slice(5,8)}/${d.slice(8,12)}-${d.slice(12)}`;
  return v || '—';
}

// ---------------------------------------------------------------------------
// Componente principal
// ---------------------------------------------------------------------------
export default function ApuracaoCredPerdidos() {
  const { token, companyId } = useAuth();
  const [data, setData] = useState<CreditosPerdidosData | null>(null);
  const [loading, setLoading] = useState(false);
  const [mesSelecionado, setMesSelecionado] = useState('');
  const [drill, setDrill] = useState<DrillDownState>({
    open: false, titulo: '', forn_cnpj: '', tipo: 'nfe', notas: [], loading: false,
  });

  const authHeaders = {
    Authorization: `Bearer ${token}`,
    'X-Company-ID': companyId || '',
  };

  async function openDrill(titulo: string, forn_cnpj: string, tipo: 'nfe' | 'cte') {
    setDrill({ open: true, titulo, forn_cnpj, tipo, notas: [], loading: true });
    try {
      const params = new URLSearchParams({ forn_cnpj, tipo });
      if (mesSelecionado) params.set('mes_ano', mesSelecionado);
      const res = await fetch(`/api/apuracao/creditos-perdidos/notas?${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      const notas: NotaDrillDown[] = await res.json();
      setDrill(d => ({ ...d, notas, loading: false }));
    } catch (err) {
      toast.error('Erro ao carregar notas: ' + String(err));
      setDrill(d => ({ ...d, loading: false }));
    }
  }

  const fetchData = async (mes?: string) => {
    setLoading(true);
    try {
      const params = mes ? `?mes_ano=${encodeURIComponent(mes)}` : '';
      const res = await fetch(`/api/apuracao/creditos-perdidos${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      const json: CreditosPerdidosData = await res.json();
      setData(json);
      if (!mes && json.mes_selecionado) setMesSelecionado(json.mes_selecionado);
    } catch (err) {
      toast.error('Erro ao carregar dados: ' + String(err));
    } finally {
      setLoading(false);
    }
  };

  function handleMesChange(mes: string) {
    setMesSelecionado(mes);
    fetchData(mes);
  }

  useEffect(() => { fetchData(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const nfe = data?.nfe_sem_credito;
  const simples = data?.simples_nacional;
  const cte = data?.cte_sem_credito;
  const aliq = data?.aliquotas;
  const totalRisco = data?.total_credito_em_risco ?? 0;

  const temDados = (nfe?.total_notas ?? 0) > 0 ||
                   (simples?.total_fornecedores ?? 0) > 0 ||
                   (cte?.total_ctes ?? 0) > 0;

  return (
    <div className="space-y-6">
      {/* Cabeçalho */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <ShieldAlert className="h-5 w-5 text-red-600" />
            <h1 className="text-2xl font-bold tracking-tight">Créditos IBS/CBS em Risco</h1>
          </div>
          <p className="text-sm text-muted-foreground mt-1">
            Estimativa de créditos IBS+CBS que sua empresa não aproveitará na Reforma Tributária —
            projeção com alíquotas de 2033.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-36">
            <Select value={mesSelecionado} onValueChange={handleMesChange} disabled={loading}>
              <SelectTrigger className="h-8 text-xs">
                <SelectValue placeholder="Período" />
              </SelectTrigger>
              <SelectContent>
                {(data?.meses_disponiveis ?? []).map(m => (
                  <SelectItem key={m} value={m} className="text-xs">{m}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button size="sm" variant="outline" onClick={() => fetchData(mesSelecionado || undefined)} disabled={loading}>
            <RefreshCw className={`h-3.5 w-3.5 mr-1.5 ${loading ? 'animate-spin' : ''}`} />
            Atualizar
          </Button>
        </div>
      </div>

      {/* ── Card de Impacto Total ─────────────────────────────────────────── */}
      {temDados && (
        <Card className="border-red-200 bg-red-50 dark:bg-red-950/20">
          <CardContent className="pt-5 pb-4">
            <div className="flex flex-col md:flex-row md:items-center gap-4">
              <div className="flex items-center gap-3">
                <div className="flex items-center justify-center w-12 h-12 rounded-full bg-red-100 dark:bg-red-900/40 shrink-0">
                  <TrendingDown className="h-6 w-6 text-red-600" />
                </div>
                <div>
                  <p className="text-xs text-red-700 dark:text-red-400 font-medium uppercase tracking-wide">
                    Total de Créditos em Risco (IBS + CBS)
                  </p>
                  <p className="text-3xl font-bold text-red-700 dark:text-red-300 leading-tight">
                    {fmtBRL(totalRisco)}
                  </p>
                  <p className="text-[11px] text-red-600/70 mt-0.5">
                    Estimativa baseada nas alíquotas de {aliq?.ano} — IBS {fmtPct(aliq?.ibs ?? 0)} + CBS {fmtPct(aliq?.cbs ?? 0)}
                  </p>
                </div>
              </div>

              <div className="flex gap-4 md:ml-auto flex-wrap">
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">NF-e sem IBS/CBS</p>
                  <p className="text-lg font-bold text-orange-600">{fmtBRL(nfe?.total_estimado ?? 0)}</p>
                </div>
                <div className="w-px bg-border hidden md:block" />
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">Simples Nacional (EFD)</p>
                  <p className="text-lg font-bold text-amber-600">{fmtBRL(simples?.total_perdido ?? 0)}</p>
                </div>
                {(cte?.total_ctes ?? 0) > 0 && (
                  <>
                    <div className="w-px bg-border hidden md:block" />
                    <div className="text-center">
                      <p className="text-[10px] text-muted-foreground">CT-e sem IBS/CBS</p>
                      <p className="text-lg font-bold text-violet-600">{fmtBRL(cte?.total_estimado ?? 0)}</p>
                    </div>
                  </>
                )}
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {!temDados && !loading && (
        <Card className="border-dashed">
          <CardContent className="py-12 text-center">
            <Info className="h-8 w-8 text-muted-foreground mx-auto mb-3" />
            <p className="text-sm text-muted-foreground">
              Nenhum dado encontrado. Importe XMLs de entrada e/ou SPEDs para visualizar os créditos em risco.
            </p>
          </CardContent>
        </Card>
      )}

      {loading && (
        <Card>
          <CardContent className="py-12 text-center text-sm text-muted-foreground">
            Calculando créditos em risco...
          </CardContent>
        </Card>
      )}

      {/* ── Seção 1: NF-e sem IBS+CBS ────────────────────────────────────── */}
      {(nfe?.total_notas ?? 0) > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between flex-wrap gap-2">
              <div>
                <CardTitle className="text-base flex items-center gap-2">
                  <AlertTriangle className="h-4 w-4 text-orange-500" />
                  NF-e de Entrada sem IBS+CBS declarado
                </CardTitle>
                <CardDescription className="text-[11px] mt-0.5">
                  Fornecedores que emitiram notas sem as tags de IBS/CBS —
                  provavelmente ainda não se adaptaram à Reforma Tributária.
                </CardDescription>
              </div>
              <div className="flex gap-3 flex-wrap">
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">Notas sem crédito</p>
                  <p className="font-bold text-orange-600">
                    {nfe?.total_notas} <span className="text-[10px] font-normal text-muted-foreground">/ {nfe?.total_universe}</span>
                  </p>
                </div>
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">% do total</p>
                  <p className="font-bold text-orange-600">{fmtPct(nfe?.perc_sem_credito ?? 0)}</p>
                </div>
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">Valor total das notas</p>
                  <p className="font-bold">{fmtBRL(nfe?.valor_total ?? 0)}</p>
                </div>
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">IBS + CBS estimado</p>
                  <p className="font-bold text-orange-600">{fmtBRL(nfe?.total_estimado ?? 0)}</p>
                </div>
              </div>
            </div>
          </CardHeader>
          <CardContent className="p-0">
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="py-1.5 px-3 text-[11px]">Fornecedor</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px]">CNPJ</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-center">Notas</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right">Valor Total (R$)</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right">IBS Est. (R$)</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right">CBS Est. (R$)</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right font-semibold text-orange-600">Total em Risco (R$)</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(nfe?.por_fornecedor ?? []).map((f, i) => (
                    <TableRow key={i} className="h-8">
                      <TableCell className="py-1 px-3 text-[11px] font-medium">{f.forn_nome || fmtCNPJ(f.forn_cnpj)}</TableCell>
                      <TableCell className="py-1 px-3 text-[10px] font-mono text-muted-foreground">{fmtCNPJ(f.forn_cnpj)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-center">
                        <Badge
                          variant="secondary"
                          className="text-[10px] px-1.5 py-0 cursor-pointer hover:bg-orange-100 hover:text-orange-700 transition-colors"
                          onClick={() => openDrill(f.forn_nome || fmtCNPJ(f.forn_cnpj), f.forn_cnpj, 'nfe')}
                          title="Ver notas"
                        >
                          {f.qtd_notas}
                        </Badge>
                      </TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right">{fmtNum(f.valor_total)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right text-blue-600">{fmtNum(f.ibs_estimado)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right text-purple-600">{fmtNum(f.cbs_estimado)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right font-semibold text-orange-600">{fmtNum(f.total_estimado)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </CardContent>
        </Card>
      )}

      {/* ── Seção 2: Simples Nacional ────────────────────────────────────── */}
      {(simples?.total_fornecedores ?? 0) > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between flex-wrap gap-2">
              <div>
                <CardTitle className="text-base flex items-center gap-2">
                  <AlertTriangle className="h-4 w-4 text-amber-500" />
                  Fornecedores do Simples Nacional — Crédito Não Aproveitado
                </CardTitle>
                <CardDescription className="text-[11px] mt-0.5">
                  Compras de fornecedores optantes pelo Simples Nacional (dados dos SPEDs importados).
                  Esses fornecedores não destacam IBS/CBS integralmente, impedindo o crédito pleno.
                </CardDescription>
              </div>
              <div className="flex gap-3 flex-wrap">
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">Fornecedores</p>
                  <p className="font-bold text-amber-600">{simples?.total_fornecedores}</p>
                </div>
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">Valor total compras</p>
                  <p className="font-bold">{fmtBRL(simples?.valor_total ?? 0)}</p>
                </div>
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">IBS + CBS perdido</p>
                  <p className="font-bold text-amber-600">{fmtBRL(simples?.total_perdido ?? 0)}</p>
                </div>
              </div>
            </div>
          </CardHeader>
          <CardContent className="p-0">
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="py-1.5 px-3 text-[11px]">Fornecedor</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px]">CNPJ</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right">Valor Compras (R$)</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right">IBS Perdido (R$)</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right">CBS Perdido (R$)</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right font-semibold text-amber-600">Total Perdido (R$)</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(simples?.por_fornecedor ?? []).map((f, i) => (
                    <TableRow key={i} className="h-8">
                      <TableCell className="py-1 px-3 text-[11px] font-medium">{f.forn_nome || fmtCNPJ(f.forn_cnpj)}</TableCell>
                      <TableCell className="py-1 px-3 text-[10px] font-mono text-muted-foreground">{fmtCNPJ(f.forn_cnpj)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right">{fmtNum(f.valor_total)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right text-blue-600">{fmtNum(f.ibs_perdido)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right text-purple-600">{fmtNum(f.cbs_perdido)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right font-semibold text-amber-600">{fmtNum(f.total_perdido)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </CardContent>
        </Card>
      )}

      {/* ── Seção 3: CT-e sem IBS+CBS ────────────────────────────────────── */}
      {(cte?.total_ctes ?? 0) > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between flex-wrap gap-2">
              <div>
                <CardTitle className="text-base flex items-center gap-2">
                  <AlertTriangle className="h-4 w-4 text-violet-500" />
                  CT-e de Entrada sem IBS+CBS declarado
                </CardTitle>
                <CardDescription className="text-[11px] mt-0.5">
                  Transportadoras que emitiram CT-e sem as tags de IBS/CBS —
                  provavelmente ainda não se adaptaram à Reforma Tributária.
                </CardDescription>
              </div>
              <div className="flex gap-3 flex-wrap">
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">CT-es sem crédito</p>
                  <p className="font-bold text-violet-600">
                    {cte?.total_ctes} <span className="text-[10px] font-normal text-muted-foreground">/ {cte?.total_universe}</span>
                  </p>
                </div>
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">% do total</p>
                  <p className="font-bold text-violet-600">{fmtPct(cte?.perc_sem_credito ?? 0)}</p>
                </div>
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">Valor total do frete</p>
                  <p className="font-bold">{fmtBRL(cte?.valor_total ?? 0)}</p>
                </div>
                <div className="text-center">
                  <p className="text-[10px] text-muted-foreground">IBS + CBS estimado</p>
                  <p className="font-bold text-violet-600">{fmtBRL(cte?.total_estimado ?? 0)}</p>
                </div>
              </div>
            </div>
          </CardHeader>
          <CardContent className="p-0">
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="py-1.5 px-3 text-[11px]">Transportadora</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px]">CNPJ</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-center">CT-es</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right">Valor Total Frete (R$)</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right">IBS Est. (R$)</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right">CBS Est. (R$)</TableHead>
                    <TableHead className="py-1.5 px-3 text-[11px] text-right font-semibold text-violet-600">Total em Risco (R$)</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(cte?.por_transportadora ?? []).map((t, i) => (
                    <TableRow key={i} className="h-8">
                      <TableCell className="py-1 px-3 text-[11px] font-medium">{t.emit_nome || fmtCNPJ(t.emit_cnpj)}</TableCell>
                      <TableCell className="py-1 px-3 text-[10px] font-mono text-muted-foreground">{fmtCNPJ(t.emit_cnpj)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-center">
                        <Badge
                          variant="secondary"
                          className="text-[10px] px-1.5 py-0 cursor-pointer hover:bg-violet-100 hover:text-violet-700 transition-colors"
                          onClick={() => openDrill(t.emit_nome || fmtCNPJ(t.emit_cnpj), t.emit_cnpj, 'cte')}
                          title="Ver CT-es"
                        >
                          {t.qtd_ctes}
                        </Badge>
                      </TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right">{fmtNum(t.valor_total)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right text-blue-600">{fmtNum(t.ibs_estimado)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right text-purple-600">{fmtNum(t.cbs_estimado)}</TableCell>
                      <TableCell className="py-1 px-3 text-[11px] text-right font-semibold text-violet-600">{fmtNum(t.total_estimado)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </CardContent>
        </Card>
      )}

      {/* ── Nota metodológica ────────────────────────────────────────────── */}
      {temDados && (
        <Card className="bg-muted/30 border-dashed">
          <CardContent className="py-3 px-4">
            <p className="text-[11px] text-muted-foreground leading-relaxed">
              <strong>Metodologia:</strong> Os valores de IBS e CBS são estimativas calculadas sobre o valor total das notas/fretes
              aplicando as alíquotas de {aliq?.ano} (IBS {fmtPct(aliq?.ibs ?? 0)} + CBS {fmtPct(aliq?.cbs ?? 0)}).
              NF-e sem IBS/CBS: fornecedores que não preencheram as tags IBSCBSTot no XML.
              Simples Nacional: fornecedores cadastrados no módulo de Simples Nacional, cujos dados vêm dos SPEDs importados.
              CT-e sem IBS/CBS: transportadoras que não preencheram as tags IBSCBSTot no XML do Conhecimento de Transporte.
              Esses valores representam o crédito que <strong>não será aproveitado</strong> na apuração de IBS/CBS.
            </p>
          </CardContent>
        </Card>
      )}

      {/* ── Dialog de drill-down: notas por fornecedor ───────────────────── */}
      <Dialog open={drill.open} onOpenChange={open => setDrill(d => ({ ...d, open }))}>
        <DialogContent className="max-w-5xl max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="text-sm font-semibold">
              {drill.tipo === 'cte' ? 'CT-es' : 'NF-es'} sem IBS/CBS —{' '}
              <span className="text-muted-foreground font-normal">{drill.titulo}</span>
              {mesSelecionado && (
                <span className="ml-2 text-[11px] text-muted-foreground font-normal">({mesSelecionado})</span>
              )}
            </DialogTitle>
          </DialogHeader>

          {drill.loading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : drill.notas.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-8">Nenhuma nota encontrada.</p>
          ) : (
            <>
              <p className="text-[11px] text-muted-foreground px-1">
                {drill.notas.length} {drill.tipo === 'cte' ? 'CT-e(s)' : 'NF-e(s)'} — clique na chave para copiar
              </p>
              <div className="overflow-auto flex-1 rounded border">
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="py-1.5 px-2 text-[11px] whitespace-nowrap">Filial</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-center whitespace-nowrap">Série</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-center whitespace-nowrap">
                        Nº {drill.tipo === 'cte' ? 'CT-e' : 'NF'}
                      </TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] whitespace-nowrap">Data Emissão</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px]">Chave Eletrônica</TableHead>
                      <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap">
                        {drill.tipo === 'cte' ? 'vPrest (R$)' : 'Valor NF (R$)'}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {drill.notas.map((n, i) => (
                      <TableRow key={i} className="h-7">
                        <TableCell className="py-0.5 px-2 text-[11px] font-medium whitespace-nowrap">
                          {n.filial || fmtCNPJ(n.filial_cnpj)}
                        </TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-center font-mono">{n.serie || '—'}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-center font-mono">{n.numero || '—'}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] whitespace-nowrap">{n.data_emissao}</TableCell>
                        <TableCell className="py-0.5 px-2 text-[10px] font-mono text-muted-foreground">
                          <span
                            className="cursor-pointer hover:text-foreground transition-colors"
                            title="Clique para copiar"
                            onClick={() => {
                              navigator.clipboard.writeText(n.chave);
                              toast.success('Chave copiada!');
                            }}
                          >
                            {n.chave}
                          </span>
                        </TableCell>
                        <TableCell className="py-0.5 px-2 text-[11px] text-right font-semibold whitespace-nowrap tabular-nums">
                          {fmtNum(n.valor)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
