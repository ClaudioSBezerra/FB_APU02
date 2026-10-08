import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { toast } from 'sonner';
import { Link } from 'react-router-dom';
import { Info, Wallet, FileCheck2, Users } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { formatCurrency } from '@/lib/utils';
import { formatCNPJ } from '@/lib/formatFilial';

// ─── Tipos ───────────────────────────────────────────────────────────────────

// RFBResumoMensal é a resposta de GET /api/rfb/resumo-mensal — camada de apuração mensal
// sobre o schema raso (binário extinto/não-extinto) do CBS v1, ver spec-apuracao-mensal-
// cbs-ibs.md e spec-apuracao-cbs-dashboard-real.md (campos total_debitos/total_creditos/
// valor_cbs_total_debito/valor_cbs_total_credito adicionados por esta última).
interface RFBResumoMensal {
  periodo: string;
  valor_cbs_nao_extinto: number;
  valor_cbs_extinto_credito: number;
  saldo_a_pagar_aproximado: number;
  aproximado: boolean;
  status_periodo: string;
  data_fechamento: string;
  total_debitos: number;
  total_creditos: number;
  valor_cbs_total_debito: number;
  valor_cbs_total_credito: number;
}

// Campos em comum entre RFBDebitoRow (GET /api/rfb/debitos) e CreditoItem (GET
// /api/rfb/creditos/lista) — "tipo" e "situacao" são adicionados nesta tela no merge, não
// existem com esse nome em nenhum dos dois payloads (situacao_debito/situacao_credito).
interface LancamentoCBS {
  id: string;
  tipo: 'debito' | 'credito';
  tipo_apuracao: string;
  numero_dfe: string;
  chave_dfe: string;
  data_dfe_emissao: string | null;
  data_apuracao: string;
  ni_emitente: string;
  ni_adquirente: string;
  valor_cbs_total: number;
  valor_cbs_extinto: number;
  valor_cbs_nao_extinto: number;
  situacao: string;
}

interface RFBDebitoRow {
  id: string;
  tipo_apuracao: string;
  numero_dfe: string;
  chave_dfe: string;
  data_dfe_emissao: string | null;
  data_apuracao: string;
  ni_emitente: string;
  ni_adquirente: string;
  valor_cbs_total: number;
  valor_cbs_extinto: number;
  valor_cbs_nao_extinto: number;
  situacao_debito: string;
}

interface CreditoItem {
  id: string;
  tipo_apuracao: string;
  numero_dfe: string;
  chave_dfe: string;
  data_dfe_emissao: string | null;
  data_apuracao: string;
  ni_emitente: string;
  ni_adquirente: string;
  valor_cbs_total: number;
  valor_cbs_extinto: number;
  valor_cbs_nao_extinto: number;
  situacao_credito: string;
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

const mesesLabel = [
  '01 - Janeiro', '02 - Fevereiro', '03 - Março', '04 - Abril', '05 - Maio', '06 - Junho',
  '07 - Julho', '08 - Agosto', '09 - Setembro', '10 - Outubro', '11 - Novembro', '12 - Dezembro',
];

function anosDisponiveis(): number[] {
  const atual = new Date().getFullYear();
  return [atual - 1, atual, atual + 1];
}

function statusPeriodoLabelCBS(status: string, dataFechamento: string): string {
  if (status === 'em_ajuste') {
    const d = new Date(dataFechamento);
    if (!isNaN(d.getTime())) {
      const dd = String(d.getDate()).padStart(2, '0');
      const mm = String(d.getMonth() + 1).padStart(2, '0');
      return `Em ajuste — prévia até ${dd}/${mm}`;
    }
    return 'Em ajuste — prévia';
  }
  if (status === 'fechado') return 'Fechado';
  return 'Em andamento';
}
function statusPeriodoBadgeClassCBS(status: string): string {
  if (status === 'em_ajuste') return 'bg-orange-100 text-orange-700 border-orange-200';
  if (status === 'fechado') return 'bg-green-100 text-green-700 border-green-200';
  return 'bg-blue-100 text-blue-700 border-blue-200';
}

async function extractErrorCBS(res: Response, fallback: string): Promise<string> {
  try {
    const data = await res.json();
    return data?.error || fallback;
  } catch {
    return fallback;
  }
}

function formatDocumento(l: LancamentoCBS): string {
  if (l.numero_dfe) return `NF-e ${l.numero_dfe}`;
  if (l.chave_dfe) return `${l.chave_dfe.slice(0, 8)}…${l.chave_dfe.slice(-6)}`;
  return '—';
}

// Contraparte a exibir: a própria empresa é sempre um dos dois lados do documento — para
// crédito, a própria empresa é a adquirente (mostra o emitente); para débito, a própria
// empresa é a emitente (mostra o adquirente). Ver Boundaries da spec apuracao-cbs-
// dashboard-real.md.
function formatContraparte(l: LancamentoCBS): string {
  const doc = l.tipo === 'credito' ? l.ni_emitente : l.ni_adquirente;
  return doc ? formatCNPJ(doc) : '—';
}

function formatDateBR(s: string | null): string {
  if (!s) return '—';
  try {
    const d = new Date(s);
    if (isNaN(d.getTime())) return s;
    return d.toLocaleDateString('pt-BR');
  } catch {
    return s;
  }
}

const TIPO_APURACAO_LABELS: Record<string, string> = {
  corrente: 'Corrente',
  ajuste: 'Ajuste',
  extemporaneo: 'Extemporâneo',
};

// ─── Componente Principal ─────────────────────────────────────────────────────

export default function GestaoCredIBSCBS() {
  const [activeTab, setActiveTab] = useState('todos');

  // ─── Período único da tela (reaproveitado por KPIs + tabela de lançamentos) ──
  const agora = new Date();
  const [mesCBS, setMesCBS] = useState(String(agora.getMonth() + 1).padStart(2, '0'));
  const [anoCBS, setAnoCBS] = useState(String(agora.getFullYear()));
  const periodoAAAAMM = `${anoCBS}${mesCBS}`; // formato esperado por /api/rfb/debitos e /api/rfb/creditos/lista

  const [resumoCBS, setResumoCBS] = useState<RFBResumoMensal | null>(null);
  const [loadingResumoCBS, setLoadingResumoCBS] = useState(true);

  const [lancamentos, setLancamentos] = useState<LancamentoCBS[]>([]);
  const [loadingLancamentos, setLoadingLancamentos] = useState(true);

  // Guardam o último período pedido por cada fetch — respostas de um período que o usuário já
  // trocou (ex.: cliques rápidos no seletor de mês) são descartadas em vez de sobrescrever a
  // tela com dado de um período que não é mais o selecionado.
  const ultimoPeriodoResumoRef = useRef('');
  const ultimoPeriodoLancamentosRef = useRef('');

  const fetchResumoCBS = useCallback(async (mes: string, ano: string) => {
    const chave = `${mes}/${ano}`;
    ultimoPeriodoResumoRef.current = chave;
    setLoadingResumoCBS(true);
    try {
      const params = new URLSearchParams({ periodo: chave });
      const res = await fetch(`/api/rfb/resumo-mensal?${params}`);
      if (ultimoPeriodoResumoRef.current !== chave) return
      if (res.ok) {
        setResumoCBS(await res.json());
      } else {
        toast.error(await extractErrorCBS(res, 'Erro ao carregar apuração mensal CBS'));
        setResumoCBS(null);
      }
    } catch (error: any) {
      if (ultimoPeriodoResumoRef.current !== chave) return
      toast.error(error?.message || 'Erro de conexão');
      setResumoCBS(null);
    } finally {
      if (ultimoPeriodoResumoRef.current === chave) setLoadingResumoCBS(false);
    }
  }, []);

  const fetchLancamentos = useCallback(async (mes: string, ano: string) => {
    const periodo = `${ano}${mes}`;
    ultimoPeriodoLancamentosRef.current = periodo;
    setLoadingLancamentos(true);
    try {
      const [debitosRes, creditosRes] = await Promise.all([
        fetch(`/api/rfb/debitos?periodo=${periodo}&page_size=50`),
        fetch(`/api/rfb/creditos/lista?periodo=${periodo}&page_size=50`),
      ]);

      let debitos: RFBDebitoRow[] = [];
      if (debitosRes.ok) {
        const data = await debitosRes.json();
        debitos = data.debitos || [];
      } else {
        toast.error(await extractErrorCBS(debitosRes, 'Erro ao carregar débitos CBS'));
      }

      let creditos: CreditoItem[] = [];
      if (creditosRes.ok) {
        const data = await creditosRes.json();
        creditos = data.creditos || [];
      } else {
        toast.error(await extractErrorCBS(creditosRes, 'Erro ao carregar créditos CBS'));
      }

      const merged: LancamentoCBS[] = [
        ...debitos.map((d): LancamentoCBS => ({
          id: d.id,
          tipo: 'debito',
          tipo_apuracao: d.tipo_apuracao,
          numero_dfe: d.numero_dfe,
          chave_dfe: d.chave_dfe,
          data_dfe_emissao: d.data_dfe_emissao,
          data_apuracao: d.data_apuracao,
          ni_emitente: d.ni_emitente,
          ni_adquirente: d.ni_adquirente,
          valor_cbs_total: d.valor_cbs_total,
          valor_cbs_extinto: d.valor_cbs_extinto,
          valor_cbs_nao_extinto: d.valor_cbs_nao_extinto,
          situacao: d.situacao_debito,
        })),
        ...creditos.map((c): LancamentoCBS => ({
          id: c.id,
          tipo: 'credito',
          tipo_apuracao: c.tipo_apuracao,
          numero_dfe: c.numero_dfe,
          chave_dfe: c.chave_dfe,
          data_dfe_emissao: c.data_dfe_emissao,
          data_apuracao: c.data_apuracao,
          ni_emitente: c.ni_emitente,
          ni_adquirente: c.ni_adquirente,
          valor_cbs_total: c.valor_cbs_total,
          valor_cbs_extinto: c.valor_cbs_extinto,
          valor_cbs_nao_extinto: c.valor_cbs_nao_extinto,
          situacao: c.situacao_credito,
        })),
      ].sort((a, b) => {
        const da = a.data_dfe_emissao ? new Date(a.data_dfe_emissao).getTime() : 0;
        const db_ = b.data_dfe_emissao ? new Date(b.data_dfe_emissao).getTime() : 0;
        return db_ - da;
      });

      if (ultimoPeriodoLancamentosRef.current !== periodo) return
      setLancamentos(merged);
    } catch (error: any) {
      if (ultimoPeriodoLancamentosRef.current !== periodo) return
      toast.error(error?.message || 'Erro de conexão ao carregar lançamentos');
      setLancamentos([]);
    } finally {
      if (ultimoPeriodoLancamentosRef.current === periodo) setLoadingLancamentos(false);
    }
  }, []);

  useEffect(() => {
    fetchResumoCBS(mesCBS, anoCBS);
    fetchLancamentos(mesCBS, anoCBS);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function handlePeriodoCBSChange(mes: string, ano: string) {
    setMesCBS(mes);
    setAnoCBS(ano);
    fetchResumoCBS(mes, ano);
    fetchLancamentos(mes, ano);
  }

  // ─── Tabs — filtram o array já carregado, sem nova chamada ───────────────────
  // "Pendências" usa o sinal numérico já existente no schema (valor_cbs_nao_extinto > 0,
  // comum aos dois endpoints) em vez de tentar mapear situacao_debito (texto livre da RFB,
  // sem enum fechado) ou duplicar o enum fechado de situacao_credito (A_APROPRIAR/
  // APROPRIADO/COMPENSADO, de RFBCreditosCBS.tsx) — ver decisão registrada na spec.
  const filteredLancamentos = useMemo(() => lancamentos.filter((l) => {
    if (activeTab === 'creditos') return l.tipo === 'credito';
    if (activeTab === 'debitos') return l.tipo === 'debito';
    if (activeTab === 'pendencias') return l.valor_cbs_nao_extinto > 0;
    return true;
  }), [lancamentos, activeTab]);

  const totalCBSFiltrado = filteredLancamentos.reduce((s, l) => s + l.valor_cbs_total, 0);

  // A tabela busca no máximo 50 débitos + 50 créditos (sem paginação) — quando o período tem
  // mais documentos que isso (total_debitos/total_creditos vêm do resumo mensal, que cobre o
  // período inteiro), avisa que a lista está truncada em vez de deixar parecer completa.
  const totalDocsPeriodo = (resumoCBS?.total_debitos ?? 0) + (resumoCBS?.total_creditos ?? 0);
  const listaTruncada = totalDocsPeriodo > lancamentos.length;

  return (
    <div className="max-w-7xl mx-auto space-y-4 pb-8">

      {/* ── Cabeçalho ── */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Apuração CBS — Receita Federal</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Monitoramento de créditos e débitos de CBS apurados junto à Receita Federal
          </p>
        </div>

        {/* Seletor de período único da tela */}
        <div className="flex items-center gap-2">
          <Select value={mesCBS} onValueChange={(v) => handlePeriodoCBSChange(v, anoCBS)}>
            <SelectTrigger className="h-8 w-36 text-[11px]">
              <SelectValue placeholder="Mês" />
            </SelectTrigger>
            <SelectContent>
              {mesesLabel.map((label, idx) => {
                const valor = String(idx + 1).padStart(2, '0');
                return <SelectItem key={valor} value={valor} className="text-[11px]">{label}</SelectItem>;
              })}
            </SelectContent>
          </Select>
          <Select value={anoCBS} onValueChange={(v) => handlePeriodoCBSChange(mesCBS, v)}>
            <SelectTrigger className="h-8 w-24 text-[11px]">
              <SelectValue placeholder="Ano" />
            </SelectTrigger>
            <SelectContent>
              {anosDisponiveis().map((ano) => (
                <SelectItem key={ano} value={String(ano)} className="text-[11px]">{ano}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {/* ── KPI Cards ── */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        {loadingResumoCBS ? (
          <div className="col-span-2 md:col-span-4 text-center text-xs text-muted-foreground py-4">Carregando KPIs...</div>
        ) : !resumoCBS ? (
          <div className="col-span-2 md:col-span-4 text-center text-xs text-muted-foreground py-4">
            Não foi possível carregar os KPIs deste período.
          </div>
        ) : (
          [
            { label: 'Crédito Potencial',   value: resumoCBS.valor_cbs_total_credito,     color: 'bg-blue-700' },
            { label: 'Crédito Confirmado',  value: resumoCBS.valor_cbs_extinto_credito,    color: 'bg-blue-600' },
            { label: 'Débitos a Pagar',     value: resumoCBS.valor_cbs_nao_extinto,         color: 'bg-blue-700' },
            { label: 'Saldo Líquido Atual', value: resumoCBS.saldo_a_pagar_aproximado,       color: 'bg-blue-600' },
          ].map((kpi) => (
            <Card key={kpi.label} className={`${kpi.color} text-white border-0 shadow-md`}>
              <CardContent className="pt-3 pb-2 px-3">
                <p className="text-[10px] text-blue-100 font-medium">{kpi.label}</p>
                <p className={`text-base font-bold mt-0.5 ${kpi.value < 0 ? 'text-red-200' : ''}`}>
                  {formatCurrency(kpi.value)}
                </p>
              </CardContent>
            </Card>
          ))
        )}
      </div>

      {/* ── Apuração Mensal CBS ── */}
      <Card className="border-2 border-primary/20 bg-primary/5">
        <CardHeader className="pb-2 pt-3 px-4">
          <CardTitle className="text-sm font-semibold">Apuração Mensal CBS</CardTitle>
        </CardHeader>
        <CardContent className="px-4 pb-4">
          {loadingResumoCBS ? (
            <p className="text-xs text-center text-muted-foreground py-4">Carregando...</p>
          ) : !resumoCBS ? (
            <p className="text-xs text-center text-muted-foreground py-4">Não foi possível carregar a apuração mensal CBS.</p>
          ) : (
            <div className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  <Wallet className="h-5 w-5 text-primary shrink-0" />
                  <div>
                    <p className="text-[10px] text-muted-foreground">Saldo a pagar (aproximado)</p>
                    <p className="text-lg font-bold">{formatCurrency(resumoCBS.saldo_a_pagar_aproximado)}</p>
                  </div>
                </div>
                <Badge variant="outline" className={`${statusPeriodoBadgeClassCBS(resumoCBS.status_periodo)} text-[11px] px-2 py-0.5`}>
                  {statusPeriodoLabelCBS(resumoCBS.status_periodo, resumoCBS.data_fechamento)}
                </Badge>
              </div>
              <div className="flex items-start gap-1.5 rounded-md bg-amber-50 border border-amber-200 px-2.5 py-2 text-[11px] text-amber-800">
                <Info className="h-3.5 w-3.5 mt-0.5 shrink-0" />
                <span>
                  Valor aproximado — o schema atual da RFB (v1) só informa o corte binário extinto/não-extinto,
                  sem o detalhamento completo de apropriação de crédito. O detalhamento completo chega com a API v2 da RFB.
                </span>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* ── Card Principal: tabela de lançamentos ── */}
      <Card>
        <CardContent className="pt-3 space-y-3">

          {/* Tabs */}
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="h-7">
              <TabsTrigger value="todos"      className="text-[11px] h-6">Todos</TabsTrigger>
              <TabsTrigger value="creditos"   className="text-[11px] h-6">Créditos</TabsTrigger>
              <TabsTrigger value="debitos"    className="text-[11px] h-6">Débitos</TabsTrigger>
              <TabsTrigger value="pendencias" className="text-[11px] h-6">Pendências</TabsTrigger>
            </TabsList>

            <TabsContent value={activeTab} className="mt-3">
              <p className="text-[10px] font-semibold text-muted-foreground mb-2">
                Lançamentos de Créditos e Débitos CBS — {mesesLabel[parseInt(mesCBS, 10) - 1]?.slice(5)}/{anoCBS}
              </p>
              {loadingLancamentos ? (
                <div className="flex justify-center py-10">
                  <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
                </div>
              ) : (
                <div className="overflow-x-auto rounded-md border">
                  <table className="min-w-full divide-y divide-gray-200">
                    <thead className="bg-gray-50">
                      <tr>
                        {['Tipo', 'Documento', 'Emitente/Adquirente', 'Valor CBS (R$)', 'Situação', 'Tipo Apuração'].map((h) => (
                          <th
                            key={h}
                            className={`px-2 py-1.5 text-[11px] font-semibold text-gray-600 ${
                              h === 'Valor CBS (R$)' ? 'text-right' : 'text-left'
                            }`}
                          >
                            {h}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-gray-100 bg-white">
                      {filteredLancamentos.length === 0 && (
                        <tr>
                          <td colSpan={6} className="px-2 py-4 text-center text-[11px] text-muted-foreground">
                            Nenhum lançamento encontrado para este período.
                          </td>
                        </tr>
                      )}
                      {filteredLancamentos.map((item) => (
                        <tr key={`${item.tipo}-${item.id}`} className="hover:bg-gray-50 transition-colors h-8">
                          <td className="px-2 py-1">
                            <Badge className={`${item.tipo === 'credito' ? 'bg-green-600' : 'bg-red-600'} text-white text-[10px] px-1.5 py-0 font-medium`}>
                              {item.tipo === 'credito' ? 'Crédito' : 'Débito'}
                            </Badge>
                          </td>
                          <td className="px-2 py-1 text-[11px] font-mono" title={item.chave_dfe}>
                            {formatDocumento(item)}
                          </td>
                          <td className="px-2 py-1 text-[11px] font-mono">{formatContraparte(item)}</td>
                          <td className="px-2 py-1 text-right text-[11px] font-medium">{formatCurrency(item.valor_cbs_total)}</td>
                          <td className="px-2 py-1 text-[11px] text-muted-foreground">{item.situacao || '—'}</td>
                          <td className="px-2 py-1 text-[11px] text-muted-foreground">
                            {TIPO_APURACAO_LABELS[item.tipo_apuracao] || item.tipo_apuracao || '—'}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                    {filteredLancamentos.length > 0 && (
                      <tfoot className="bg-gray-50 border-t-2 border-gray-300">
                        <tr>
                          <td colSpan={3} className="px-2 py-1.5 text-[11px] font-semibold text-right text-gray-700">
                            Total:
                          </td>
                          <td className="px-2 py-1.5 text-right text-[11px] font-bold">{formatCurrency(totalCBSFiltrado)}</td>
                          <td colSpan={2} />
                        </tr>
                      </tfoot>
                    )}
                  </table>
                  {listaTruncada && (
                    <p className="text-[10px] text-amber-700 bg-amber-50 px-2 py-1.5 border-t border-amber-200">
                      Mostrando os {lancamentos.length} lançamentos mais recentes de {totalDocsPeriodo} no período — os KPIs acima consideram o total completo.
                    </p>
                  )}
                  <p className="text-[10px] text-muted-foreground px-2 py-1.5 border-t">
                    Data de emissão não exibida nesta coluna é usada apenas para ordenação — ver chave eletrônica completa no título ao passar o mouse.
                  </p>
                </div>
              )}
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>

      {/* ── Resumo da Apuração + atalhos reais ── */}
      <Card className="border-2 border-blue-100 bg-blue-50/40">
        <CardHeader className="pb-2 pt-3 px-4">
          <CardTitle className="text-[11px] font-semibold text-blue-900 leading-tight">
            Resumo da Apuração — {mesesLabel[parseInt(mesCBS, 10) - 1]?.slice(5)}/{anoCBS}
          </CardTitle>
        </CardHeader>
        <CardContent className="px-4 pb-4 space-y-4">
          {loadingResumoCBS ? (
            <p className="text-xs text-center text-muted-foreground py-4">Carregando...</p>
          ) : !resumoCBS ? (
            <p className="text-xs text-center text-muted-foreground py-4">Sem dados para este período.</p>
          ) : (
            <div className="space-y-2">
              <div className="flex justify-between items-center text-[11px]">
                <span className="text-muted-foreground">· Créditos Confirmados ({resumoCBS.total_creditos} docs)</span>
                <span className="font-semibold text-green-700">{formatCurrency(resumoCBS.valor_cbs_extinto_credito)}</span>
              </div>
              <div className="flex justify-between items-center text-[11px]">
                <span className="text-muted-foreground">· Débitos a Pagar ({resumoCBS.total_debitos} docs)</span>
                <span className="font-semibold text-red-700">{formatCurrency(resumoCBS.valor_cbs_nao_extinto)}</span>
              </div>
              <div className="flex justify-between items-center text-[11px] font-bold border-t pt-2 mt-1">
                <span>· Saldo a Pagar (aproximado)</span>
                <span className={resumoCBS.saldo_a_pagar_aproximado < 0 ? 'text-green-600' : 'text-orange-600'}>
                  {formatCurrency(resumoCBS.saldo_a_pagar_aproximado)}
                </span>
              </div>
            </div>
          )}

          <div className="space-y-2 pt-1">
            <Button asChild variant="outline" className="w-full h-8 text-[11px] justify-start gap-2">
              <Link to="/rfb/apuracao">
                <FileCheck2 className="h-3.5 w-3.5" />
                Importar Movimento (solicitar apuração à RFB)
              </Link>
            </Button>
            <Button asChild variant="outline" className="w-full h-8 text-[11px] justify-start gap-2">
              <Link to="/rfb/pagamentos-fornecedores">
                <Users className="h-3.5 w-3.5" />
                Pagamentos a Fornecedores
              </Link>
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
