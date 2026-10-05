import { useState, useEffect, useCallback } from 'react';
import { toast } from 'sonner';
import { Search, User, Filter, MoreVertical, Info, Wallet } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { Separator } from '@/components/ui/separator';
import { formatCurrency } from '@/lib/utils';

// ─── Tipos ───────────────────────────────────────────────────────────────────

interface Lancamento {
  id: string;
  tipo: 'credito' | 'debito';
  documento: string;
  fornecedorCliente: string;
  descricao: string;
  valorIBS: number;
  valorCBS: number;
  status: 'confirmado' | 'pendente' | 'apurado' | 'em_analise';
  vinculado: string;
}

interface ConciliacaoItem {
  id: string;
  notaFiscal: string;
  pedidoSAP: string;
  statusCredito: 'confirmado' | 'pendente';
}

// ─── Mock Data ────────────────────────────────────────────────────────────────

const lancamentosMock: Lancamento[] = [
  { id: '1', tipo: 'credito', documento: 'NF-e 123456',  fornecedorCliente: 'Fornecedor ABC',       descricao: 'Compras de Insumos', valorIBS: 15600, valorCBS: 1500, status: 'confirmado', vinculado: 'Aguardando Nfps' },
  { id: '2', tipo: 'credito', documento: 'NF-e 210431',  fornecedorCliente: 'Fornecedor Emprego',    descricao: 'Mão de Obra',        valorIBS: 25000, valorCBS: 1500, status: 'pendente',   vinculado: 'Aguardando Nfps' },
  { id: '3', tipo: 'debito',  documento: 'NF-e 220432',  fornecedorCliente: 'Fornecedor Provisório', descricao: 'Pedido de Carga',    valorIBS: 38000, valorCBS: 1000, status: 'apurado',    vinculado: 'Aguardando Nfps' },
  { id: '4', tipo: 'credito', documento: 'NF-e 156/708', fornecedorCliente: 'Fornecedor Subtenso',   descricao: 'Pedido de Tempo',    valorIBS: 36000, valorCBS: 1500, status: 'pendente',   vinculado: 'Aguardando Nfps' },
  { id: '5', tipo: 'credito', documento: 'NF-e 252/026', fornecedorCliente: 'Fornecedor Transpo',    descricao: 'Pedido de Meios',    valorIBS: 20000, valorCBS: 1500, status: 'confirmado', vinculado: 'Aguardando Nfps' },
];

const conciliacaoMock: ConciliacaoItem[] = [
  { id: '1', notaFiscal: 'NF-e 123456 – Fornecedor ABC',    pedidoSAP: 'PO-2024-0891 / PAG-0345', statusCredito: 'confirmado' },
  { id: '2', notaFiscal: 'NF-e 252/026 – Fornecedor Transpo', pedidoSAP: 'PO-2024-0892 / PAG-0346', statusCredito: 'confirmado' },
  { id: '3', notaFiscal: 'NF-e 210431 – Fornecedor Emprego',  pedidoSAP: 'PO-2024-0893 / —',        statusCredito: 'pendente'   },
];

// ─── Helpers ──────────────────────────────────────────────────────────────────

function fmt(value: number): string {
  return new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' }).format(value);
}

function fmtNum(value: number): string {
  return new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value);
}

const statusCfg: Record<Lancamento['status'], { label: string; cls: string }> = {
  confirmado: { label: 'Confirmado', cls: 'bg-green-100 text-green-700 border-green-200' },
  pendente:   { label: 'Pendente',   cls: 'bg-orange-100 text-orange-700 border-orange-200' },
  apurado:    { label: 'Apurado',    cls: 'bg-blue-100 text-blue-700 border-blue-200' },
  em_analise: { label: 'Em Análise', cls: 'bg-yellow-100 text-yellow-700 border-yellow-200' },
};

const tipoCfg: Record<Lancamento['tipo'], { label: string; cls: string }> = {
  credito: { label: 'Crédito', cls: 'bg-green-600 text-white' },
  debito:  { label: 'Débito',  cls: 'bg-red-600 text-white' },
};

const concStatusCfg: Record<ConciliacaoItem['statusCredito'], { label: string; cls: string }> = {
  confirmado: { label: 'Confirmado', cls: 'bg-green-100 text-green-700 border-green-200' },
  pendente:   { label: 'Pendente',   cls: 'bg-orange-100 text-orange-700 border-orange-200' },
};

// ─── Apuração Mensal CBS (spec-apuracao-mensal-cbs-ibs.md) ────────────────────
// Camada aproximada sobre o schema raso (binário extinto/não-extinto) do CBS v1 —
// contraste com o IBS acima (ainda mock nesta tela), que já é granular.

interface RFBResumoMensal {
  periodo: string;
  valor_cbs_nao_extinto: number;
  valor_cbs_extinto_credito: number;
  saldo_a_pagar_aproximado: number;
  aproximado: boolean;
  status_periodo: string;
  data_fechamento: string;
}

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

// ─── Componente Principal ─────────────────────────────────────────────────────

export default function GestaoCredIBSCBS() {
  const [activeTab, setActiveTab] = useState('todos');
  const [periodoFiltro, setPeriodoFiltro] = useState('maio2024');
  const [statusFiltro, setStatusFiltro] = useState('todas');
  const [concDocFiltro, setConcDocFiltro] = useState('todos');
  const [concStatusFiltro, setConcStatusFiltro] = useState('todos');

  // ─── Apuração Mensal CBS ──────────────────────────────────────────────────
  const agora = new Date();
  const [mesCBS, setMesCBS] = useState(String(agora.getMonth() + 1).padStart(2, '0'));
  const [anoCBS, setAnoCBS] = useState(String(agora.getFullYear()));
  const [resumoCBS, setResumoCBS] = useState<RFBResumoMensal | null>(null);
  const [loadingResumoCBS, setLoadingResumoCBS] = useState(true);

  const fetchResumoCBS = useCallback(async (mes: string, ano: string) => {
    setLoadingResumoCBS(true);
    try {
      const params = new URLSearchParams({ periodo: `${mes}/${ano}` });
      const res = await fetch(`/api/rfb/resumo-mensal?${params}`);
      if (res.ok) {
        setResumoCBS(await res.json());
      } else {
        toast.error(await extractErrorCBS(res, 'Erro ao carregar apuração mensal CBS'));
        setResumoCBS(null);
      }
    } catch (error: any) {
      toast.error(error?.message || 'Erro de conexão');
      setResumoCBS(null);
    } finally {
      setLoadingResumoCBS(false);
    }
  }, []);

  useEffect(() => {
    fetchResumoCBS(mesCBS, anoCBS);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function handlePeriodoCBSChange(mes: string, ano: string) {
    setMesCBS(mes);
    setAnoCBS(ano);
    fetchResumoCBS(mes, ano);
  }

  const filteredLancamentos = lancamentosMock.filter((l) => {
    const tabOk =
      activeTab === 'todos'      ? true :
      activeTab === 'creditos'   ? l.tipo === 'credito' :
      activeTab === 'debitos'    ? l.tipo === 'debito'  :
      activeTab === 'pendencias' ? l.status === 'pendente' : true;
    const statusOk = statusFiltro === 'todas' || l.status === statusFiltro;
    return tabOk && statusOk;
  });

  const totalIBS = filteredLancamentos.reduce((s, l) => s + l.valorIBS, 0);
  const totalCBS = filteredLancamentos.reduce((s, l) => s + l.valorCBS, 0);

  const filteredConc = conciliacaoMock.filter((c) => {
    const docOk    = concDocFiltro    === 'todos' || c.id === concDocFiltro;
    const statusOk = concStatusFiltro === 'todos' || c.statusCredito === concStatusFiltro;
    return docOk && statusOk;
  });

  return (
    <div className="max-w-7xl mx-auto space-y-4 pb-8">

      {/* ── Cabeçalho ── */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Gestão de Créditos e Débitos IBS / CBS</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Monitoramento e conciliação de obrigações tributárias da Reforma Tributária
          </p>
        </div>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon"><Search className="h-4 w-4" /></Button>
          <Button variant="ghost" size="icon"><User className="h-4 w-4" /></Button>
        </div>
      </div>

      {/* ── KPI Cards ── */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        {[
          { label: 'Crédito Potencial',   value: 1_280_500, color: 'bg-blue-700' },
          { label: 'Crédito Confirmado',  value:   850_300, color: 'bg-blue-600' },
          { label: 'Débitos a Pagar',     value:   940_700, color: 'bg-blue-700' },
          { label: 'Saldo Líquido Atual', value:   -90_400, color: 'bg-blue-600' },
        ].map((kpi) => (
          <Card key={kpi.label} className={`${kpi.color} text-white border-0 shadow-md`}>
            <CardContent className="pt-3 pb-2 px-3">
              <p className="text-[10px] text-blue-100 font-medium">{kpi.label}</p>
              <p className={`text-base font-bold mt-0.5 ${kpi.value < 0 ? 'text-red-200' : ''}`}>
                {fmt(kpi.value)}
              </p>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* ── Apuração Mensal CBS ── */}
      <Card className="border-2 border-primary/20 bg-primary/5">
        <CardHeader className="pb-2 pt-3 px-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle className="text-sm font-semibold">Apuração Mensal CBS</CardTitle>
            <div className="flex items-center gap-2">
              <Select value={mesCBS} onValueChange={(v) => handlePeriodoCBSChange(v, anoCBS)}>
                <SelectTrigger className="h-7 w-36 text-[11px]">
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
                <SelectTrigger className="h-7 w-24 text-[11px]">
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

      {/* ── Card Principal ── */}
      <Card>
        <CardContent className="pt-3 space-y-3">

          {/* Filtros */}
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[11px] font-medium text-muted-foreground">Filtro:</span>

            <Select value={periodoFiltro} onValueChange={setPeriodoFiltro}>
              <SelectTrigger className="h-7 w-32 text-[11px]">
                <SelectValue placeholder="Período" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="maio2024" className="text-[11px]">Maio 2024</SelectItem>
                <SelectItem value="abril2024" className="text-[11px]">Abril 2024</SelectItem>
                <SelectItem value="marco2024" className="text-[11px]">Março 2024</SelectItem>
              </SelectContent>
            </Select>

            <Select defaultValue="matrizDF">
              <SelectTrigger className="h-7 w-32 text-[11px]">
                <SelectValue placeholder="Empresa" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="matrizDF" className="text-[11px]">Matriz DF</SelectItem>
                <SelectItem value="filialSP" className="text-[11px]">Filial SP</SelectItem>
              </SelectContent>
            </Select>

            <Select value={statusFiltro} onValueChange={setStatusFiltro}>
              <SelectTrigger className="h-7 w-32 text-[11px]">
                <SelectValue placeholder="Status" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="todas" className="text-[11px]">Todas</SelectItem>
                <SelectItem value="confirmado" className="text-[11px]">Confirmado</SelectItem>
                <SelectItem value="pendente" className="text-[11px]">Pendente</SelectItem>
                <SelectItem value="apurado" className="text-[11px]">Apurado</SelectItem>
              </SelectContent>
            </Select>

            <Button size="sm" className="h-7 text-[11px] px-2">
              <Filter className="mr-1 h-3 w-3" /> Filtrar
            </Button>
          </div>

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
                Monitoramento de Créditos e Débitos
              </p>
              <div className="overflow-x-auto rounded-md border">
                <table className="min-w-full divide-y divide-gray-200">
                  <thead className="bg-gray-50">
                    <tr>
                      {['Tipo', 'Documento', 'Fornecedor / Cliente', 'Descrição',
                        'Valor IBS (R$)', 'Valor CBS (R$)', 'Status', 'Vínculado', ''].map((h) => (
                        <th
                          key={h}
                          className={`px-2 py-1.5 text-[11px] font-semibold text-gray-600 ${
                            ['Valor IBS (R$)', 'Valor CBS (R$)'].includes(h) ? 'text-right' : 'text-left'
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
                        <td colSpan={9} className="px-2 py-4 text-center text-[11px] text-muted-foreground">
                          Nenhum lançamento encontrado para os filtros selecionados.
                        </td>
                      </tr>
                    )}
                    {filteredLancamentos.map((item) => {
                      const tipo = tipoCfg[item.tipo];
                      const st   = statusCfg[item.status];
                      return (
                        <tr key={item.id} className="hover:bg-gray-50 transition-colors h-8">
                          <td className="px-2 py-1">
                            <Badge className={`${tipo.cls} text-[10px] px-1.5 py-0 font-medium`}>
                              {tipo.label}
                            </Badge>
                          </td>
                          <td className="px-2 py-1 text-[11px] font-mono">{item.documento}</td>
                          <td className="px-2 py-1 text-[11px]">{item.fornecedorCliente}</td>
                          <td className="px-2 py-1 text-[11px] text-muted-foreground">{item.descricao}</td>
                          <td className="px-2 py-1 text-right text-[11px] font-medium">{fmtNum(item.valorIBS)}</td>
                          <td className="px-2 py-1 text-right text-[11px] font-medium">{fmtNum(item.valorCBS)}</td>
                          <td className="px-2 py-1">
                            <Badge variant="outline" className={`${st.cls} text-[10px] px-1.5 py-0`}>
                              {st.label}
                            </Badge>
                          </td>
                          <td className="px-2 py-1 text-[11px] text-muted-foreground">{item.vinculado}</td>
                          <td className="px-2 py-1">
                            <DropdownMenu>
                              <DropdownMenuTrigger asChild>
                                <Button variant="ghost" size="icon" className="h-5 w-5">
                                  <MoreVertical className="h-3 w-3" />
                                </Button>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end">
                                <DropdownMenuItem className="text-[11px]">Ver detalhes</DropdownMenuItem>
                                <DropdownMenuItem className="text-[11px]">Confirmar</DropdownMenuItem>
                                <DropdownMenuItem className="text-[11px]">Vincular</DropdownMenuItem>
                              </DropdownMenuContent>
                            </DropdownMenu>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                  <tfoot className="bg-gray-50 border-t-2 border-gray-300">
                    <tr>
                      <td colSpan={4} className="px-2 py-1.5 text-[11px] font-semibold text-right text-gray-700">
                        Total:
                      </td>
                      <td className="px-2 py-1.5 text-right text-[11px] font-bold">{fmtNum(totalIBS)}</td>
                      <td className="px-2 py-1.5 text-right text-[11px] font-bold">{fmtNum(totalCBS)}</td>
                      <td colSpan={3} />
                    </tr>
                  </tfoot>
                </table>
              </div>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>

      {/* ── Seção inferior: Conciliação + Painel Apuração ── */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">

        {/* Conciliação de Pagamentos (2/3) */}
        <Card className="md:col-span-2">
          <CardHeader className="pb-2 pt-3 px-4">
            <CardTitle className="text-[11px] font-semibold">Conciliação de Pagamentos</CardTitle>
          </CardHeader>
          <CardContent className="px-4 pb-4 space-y-3">

            <div className="flex items-center gap-2">
              <Select value={concDocFiltro} onValueChange={setConcDocFiltro}>
                <SelectTrigger className="h-7 w-48 text-[11px]">
                  <SelectValue placeholder="Documento" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="todos" className="text-[11px]">Todos os documentos</SelectItem>
                  <SelectItem value="1" className="text-[11px]">NF-e 123456</SelectItem>
                  <SelectItem value="2" className="text-[11px]">NF-e 252/026</SelectItem>
                  <SelectItem value="3" className="text-[11px]">NF-e 210431</SelectItem>
                </SelectContent>
              </Select>

              <Select value={concStatusFiltro} onValueChange={setConcStatusFiltro}>
                <SelectTrigger className="h-7 w-32 text-[11px]">
                  <SelectValue placeholder="Status" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="todos" className="text-[11px]">Todos</SelectItem>
                  <SelectItem value="confirmado" className="text-[11px]">Confirmado</SelectItem>
                  <SelectItem value="pendente" className="text-[11px]">Pendente</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="overflow-x-auto rounded-md border">
              <table className="min-w-full divide-y divide-gray-200">
                <thead className="bg-gray-50">
                  <tr>
                    <th className="px-2 py-1.5 text-left text-[11px] font-semibold text-gray-600">Nota Fiscal de Entrada</th>
                    <th className="px-2 py-1.5 text-left text-[11px] font-semibold text-gray-600">Pedido SAP / Pagamento</th>
                    <th className="px-2 py-1.5 text-left text-[11px] font-semibold text-gray-600">Status do Crédito</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100 bg-white">
                  {filteredConc.length === 0 && (
                    <tr>
                      <td colSpan={3} className="px-2 py-4 text-center text-[11px] text-muted-foreground">
                        Nenhum registro encontrado.
                      </td>
                    </tr>
                  )}
                  {filteredConc.map((c) => {
                    const sc = concStatusCfg[c.statusCredito];
                    return (
                      <tr key={c.id} className="hover:bg-gray-50 transition-colors h-8">
                        <td className="px-2 py-1 text-[11px]">{c.notaFiscal}</td>
                        <td className="px-2 py-1 text-[11px] font-mono">{c.pedidoSAP}</td>
                        <td className="px-2 py-1">
                          <Badge variant="outline" className={`${sc.cls} text-[10px] px-1.5 py-0`}>
                            {sc.label}
                          </Badge>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>

        {/* Painel Apuração Assistida (1/3) */}
        <Card className="border-2 border-blue-100 bg-blue-50/40">
          <CardHeader className="pb-2 pt-3 px-4">
            <div className="flex items-start justify-between">
              <div>
                <CardTitle className="text-[11px] font-semibold text-blue-900 leading-tight">
                  Apuração Assistida – IBS / CBS
                </CardTitle>
                <p className="text-[10px] text-blue-600 mt-0.5">Maio 2024</p>
              </div>
              <div className="flex gap-1 shrink-0">
                <Button variant="ghost" size="icon" className="h-6 w-6 text-blue-600">
                  <Search className="h-3 w-3" />
                </Button>
                <Button variant="ghost" size="icon" className="h-6 w-6 text-blue-600">
                  <User className="h-3 w-3" />
                </Button>
              </div>
            </div>
          </CardHeader>
          <CardContent className="px-4 pb-4 space-y-4">
            <div>
              <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wide mb-2">
                Resumo da Apuração
              </p>
              <div className="space-y-2">
                <div className="flex justify-between items-center text-[11px]">
                  <span className="text-muted-foreground">· Créditos Confirmados</span>
                  <span className="font-semibold text-green-700">{fmt(850_300)}</span>
                </div>
                <div className="flex justify-between items-center text-[11px]">
                  <span className="text-muted-foreground">· Débitos Confirmados</span>
                  <span className="font-semibold text-red-700">{fmt(940_700)}</span>
                </div>
                <Separator className="my-1" />
                <div className="flex justify-between items-center text-[11px] font-bold">
                  <span>· Saldo a Receber</span>
                  <span className="text-orange-600">{fmt(90_400)}</span>
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <Button className="w-full h-7 text-[11px] bg-green-600 hover:bg-green-700 text-white font-semibold">
                Sugerir Composição
              </Button>
              <p className="text-center text-[10px] text-blue-600 underline cursor-pointer hover:text-blue-800 transition-colors">
                Utilizar Créditos Disponíveis
              </p>
              <Button className="w-full h-8 text-xs bg-orange-500 hover:bg-orange-600 text-white font-bold tracking-wide">
                APURAR
              </Button>
            </div>
          </CardContent>
        </Card>

      </div>
    </div>
  );
}
