import { useState, useEffect, useCallback, useRef, Fragment } from 'react'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { formatCurrency } from '@/lib/utils'
import {
  FileBarChart, Search, ChevronLeft, ChevronRight, ChevronDown, ChevronUp, Inbox, AlertTriangle, Wallet,
} from 'lucide-react'

interface CGIBSAlertaDecadencia {
  chave_acesso: string
  operacao_id: string
  ja_decaido?: boolean
  valor: number
  dth_lancto: string
  dias_restantes: number
}

interface CGIBSResumoPeriodo {
  data_ini: string
  data_fim: string
  total_operacoes: number
  recurso_financeiro_disponivel: number
  recurso_financeiro_a_transferir: number
  credito_a_apropriar: number
  credito_nao_utilizado: number
  credito_utilizado: number
  debito_em_aberto: number
  debito_extinto: number
  status_periodo: string
  data_fechamento: string
  saldo_a_pagar: number
  alertas_decadencia: CGIBSAlertaDecadencia[]
}

// ─── Status do período de apuração (spec-apuracao-mensal-cbs-ibs.md) ───────────
function statusPeriodoLabel(status: string, dataFechamento: string): string {
  if (status === 'em_ajuste') {
    const d = new Date(dataFechamento)
    if (!isNaN(d.getTime())) {
      const dd = String(d.getDate()).padStart(2, '0')
      const mm = String(d.getMonth() + 1).padStart(2, '0')
      return `Em ajuste — prévia até ${dd}/${mm}`
    }
    return 'Em ajuste — prévia'
  }
  if (status === 'fechado') return 'Fechado'
  return 'Em andamento'
}
function statusPeriodoBadgeClass(status: string): string {
  if (status === 'em_ajuste') return 'bg-orange-100 text-orange-700 border-orange-200'
  if (status === 'fechado') return 'bg-green-100 text-green-700 border-green-200'
  return 'bg-blue-100 text-blue-700 border-blue-200'
}

interface CGIBSOperacao {
  id: string
  operacao_id_externo: number | null
  chave_acesso: string
  dth_emissao: string | null
  dth_autorizacao: string | null
  cnpj_fornecedor: string
  cnpj_adquirente: string
  created_at: string
  updated_at: string
}

interface CGIBSLancamento {
  id: string
  lancamento_id_externo: number
  dth_lancto: string
  mov_codigo: number | null
  mov_descricao: string
  recurso_financeiro_disponivel: number
  recurso_financeiro_a_transferir: number
  credito_a_apropriar: number
  credito_nao_utilizado: number
  credito_utilizado: number
  debito_em_aberto: number
  debito_extinto: number
  created_at: string
}

interface Pagination {
  page: number
  page_size: number
  total: number
  total_pages: number
}

function toISODate(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
function defaultDataIni(): string {
  const d = new Date()
  return toISODate(new Date(d.getFullYear(), d.getMonth(), 1))
}
function defaultDataFim(): string {
  return toISODate(new Date())
}
function fmtCNPJBase(cnpj: string): string {
  if (cnpj && cnpj.length === 8) return `${cnpj.slice(0, 2)}.${cnpj.slice(2, 5)}.${cnpj.slice(5)}`
  return cnpj || '—'
}
function fmtDateTime(s: string | null): string {
  if (!s) return '—'
  return new Date(s).toLocaleString('pt-BR')
}
function fmtChave(chave: string): string {
  if (!chave) return '—'
  return `${chave.slice(0, 4)}…${chave.slice(-6)}`
}

async function extractError(res: Response, fallback: string): Promise<string> {
  try {
    const data = await res.json()
    return data?.error || fallback
  } catch {
    return fallback
  }
}

export default function CGIBSExtrato() {
  const [dataIni, setDataIni] = useState(defaultDataIni())
  const [dataFim, setDataFim] = useState(defaultDataFim())
  const [resumo, setResumo] = useState<CGIBSResumoPeriodo | null>(null)
  const [loadingResumo, setLoadingResumo] = useState(true)

  const [operacoes, setOperacoes] = useState<CGIBSOperacao[]>([])
  const [pagination, setPagination] = useState<Pagination | null>(null)
  const [page, setPage] = useState(1)
  const [loadingOperacoes, setLoadingOperacoes] = useState(true)

  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [loadingLancamentos, setLoadingLancamentos] = useState<string | null>(null)
  const [lancamentosById, setLancamentosById] = useState<Record<string, CGIBSLancamento[]>>({})

  const fetchResumo = useCallback(async (ini: string, fim: string) => {
    setLoadingResumo(true)
    try {
      const params = new URLSearchParams({ data_ini: ini, data_fim: fim })
      const res = await fetch(`/api/cgibs/resumo?${params}`)
      if (res.ok) {
        setResumo(await res.json())
      } else {
        toast.error(await extractError(res, 'Erro ao carregar resumo do período'))
        setResumo(null)
      }
    } catch (error: any) {
      toast.error(error?.message || 'Erro de conexão')
      setResumo(null)
    } finally {
      setLoadingResumo(false)
    }
  }, [])

  // Guarda a página pedida por último — respostas fora de ordem (ex.: cliques rápidos em
  // "próxima"/"anterior") não podem sobrescrever a tabela com dados de uma página que o
  // usuário já não está mais vendo.
  const ultimaPaginaPedidaRef = useRef(0)

  const fetchOperacoes = useCallback(async (p: number) => {
    ultimaPaginaPedidaRef.current = p
    setLoadingOperacoes(true)
    try {
      const params = new URLSearchParams({ page: String(p), page_size: '100' })
      const res = await fetch(`/api/cgibs/operacoes?${params}`)
      if (ultimaPaginaPedidaRef.current !== p) return // resposta obsoleta — outra página já foi pedida
      if (res.ok) {
        const data = await res.json()
        setOperacoes(data.operacoes || [])
        setPagination(data.pagination || null)
      } else {
        toast.error(await extractError(res, 'Erro ao carregar operações'))
      }
    } catch (error: any) {
      if (ultimaPaginaPedidaRef.current !== p) return
      toast.error(error?.message || 'Erro de conexão')
    } finally {
      if (ultimaPaginaPedidaRef.current === p) setLoadingOperacoes(false)
    }
  }, [])

  useEffect(() => {
    fetchResumo(dataIni, dataFim)
    fetchOperacoes(1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function handleConsultar() {
    if (!dataIni || !dataFim) {
      toast.error('Selecione a data de início e a data de fim do período')
      return
    }
    if (dataFim < dataIni) {
      toast.error('A data de fim deve ser igual ou posterior à data de início')
      return
    }
    fetchResumo(dataIni, dataFim)
  }

  function handlePage(p: number) {
    setPage(p)
    fetchOperacoes(p)
  }

  async function toggleLancamentos(id: string) {
    if (expandedId === id) {
      setExpandedId(null)
      return
    }
    setExpandedId(id)
    if (lancamentosById[id]) return // já carregado — só expande (lazy só na 1ª vez)
    setLoadingLancamentos(id)
    try {
      const res = await fetch(`/api/cgibs/operacoes/${id}/lancamentos`)
      if (res.ok) {
        const data = await res.json()
        setLancamentosById(prev => ({ ...prev, [id]: data.lancamentos || [] }))
      } else {
        toast.error(await extractError(res, 'Erro ao carregar lançamentos'))
        setExpandedId(null)
      }
    } catch (error: any) {
      toast.error(error?.message || 'Erro de conexão')
      setExpandedId(null)
    } finally {
      setLoadingLancamentos(null)
    }
  }

  return (
    <div className="max-w-6xl mx-auto px-4 py-6 space-y-4">
      {/* ── Cabeçalho ── */}
      <div>
        <h2 className="text-2xl font-bold flex items-center gap-2">
          <FileBarChart className="h-6 w-6" />
          Extrato IBS — Conta Corrente Fiscal
        </h2>
        <p className="mt-1 text-sm text-gray-600">
          Resumo agregado por período e lista de operações (conta corrente fiscal por documento) importadas do CGIBS.
        </p>
      </div>

      {/* ── Seletor de período ── */}
      <Card>
        <CardContent className="pt-4">
          <div className="flex flex-wrap items-end gap-3">
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Data início</label>
              <Input type="date" value={dataIni} onChange={e => setDataIni(e.target.value)} className="h-9 w-40" />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Data fim</label>
              <Input type="date" value={dataFim} onChange={e => setDataFim(e.target.value)} className="h-9 w-40" />
            </div>
            <Button onClick={handleConsultar} disabled={loadingResumo}>
              <Search className="mr-2 h-4 w-4" /> Consultar
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* ── Cards de resumo do período ── */}
      {loadingResumo ? (
        <p className="text-sm text-center text-muted-foreground py-4">Carregando resumo...</p>
      ) : !resumo ? (
        <p className="text-sm text-center text-muted-foreground py-4">Não foi possível carregar o resumo do período.</p>
      ) : (
        <div className="space-y-4">
          {/* ── Saldo a pagar + status do período ── */}
          <Card className="border-2 border-primary/20 bg-primary/5">
            <CardContent className="pt-4 pb-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-3">
                  <Wallet className="h-6 w-6 text-primary shrink-0" />
                  <div>
                    <p className="text-xs text-muted-foreground">Saldo a pagar (débito em aberto do período)</p>
                    <p className="text-2xl font-bold">{formatCurrency(resumo.saldo_a_pagar)}</p>
                  </div>
                </div>
                <Badge variant="outline" className={`${statusPeriodoBadgeClass(resumo.status_periodo)} text-xs px-2.5 py-1`}>
                  {statusPeriodoLabel(resumo.status_periodo, resumo.data_fechamento)}
                </Badge>
              </div>
            </CardContent>
          </Card>

          {/* ── Alertas de decadência de crédito (5 anos) ── */}
          {resumo.alertas_decadencia && resumo.alertas_decadencia.length > 0 && (
            <Card className="border-2 border-amber-200 bg-amber-50/60">
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-semibold flex items-center gap-2 text-amber-800">
                  <AlertTriangle className="h-4 w-4" />
                  Alerta de decadência de crédito ({resumo.alertas_decadencia.length})
                </CardTitle>
                <p className="text-xs text-amber-700">
                  Operações com crédito IBS não utilizado há mais de 4 anos — o prazo legal de decadência é de 5 anos.
                  Esta lista é da empresa inteira, independente do período selecionado acima.
                </p>
              </CardHeader>
              <CardContent className="pt-0">
                <div className="overflow-x-auto">
                  <table className="w-full text-xs">
                    <thead>
                      <tr className="border-b text-muted-foreground uppercase tracking-wide">
                        <th className="text-left px-2 py-1 font-medium">Chave de acesso</th>
                        <th className="text-right px-2 py-1 font-medium">Valor</th>
                        <th className="text-right px-2 py-1 font-medium">Dias restantes</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y">
                      {resumo.alertas_decadencia.map(a => (
                        <tr key={a.operacao_id}>
                          <td className="px-2 py-1 font-mono" title={a.chave_acesso}>{fmtChave(a.chave_acesso)}</td>
                          <td className="px-2 py-1 text-right font-mono">{formatCurrency(a.valor)}</td>
                          <td className={`px-2 py-1 text-right font-mono ${a.ja_decaido || a.dias_restantes <= 90 ? 'text-red-600 font-semibold' : 'text-amber-700'}`}>
                            {a.ja_decaido ? `Decaído há ${Math.abs(a.dias_restantes)}d` : `${a.dias_restantes}d`}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
          )}

          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <Card className="p-3">
              <p className="text-xs text-muted-foreground">Total de operações</p>
              <p className="text-xl font-bold">{resumo.total_operacoes.toLocaleString('pt-BR')}</p>
            </Card>
          </div>

          <div className="grid gap-4 md:grid-cols-3">
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-semibold">Recursos financeiros</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Disponível p/ transferência</span>
                  <span className="font-semibold">{formatCurrency(resumo.recurso_financeiro_disponivel)}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">A transferir</span>
                  <span className="font-semibold">{formatCurrency(resumo.recurso_financeiro_a_transferir)}</span>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-semibold">Créditos</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">A apropriar</span>
                  <span className="font-semibold">{formatCurrency(resumo.credito_a_apropriar)}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Não utilizado</span>
                  <span className="font-semibold">{formatCurrency(resumo.credito_nao_utilizado)}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Utilizado</span>
                  <span className="font-semibold">{formatCurrency(resumo.credito_utilizado)}</span>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-semibold">Débitos</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Em aberto</span>
                  <span className="font-semibold text-orange-600">{formatCurrency(resumo.debito_em_aberto)}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Extinto</span>
                  <span className="font-semibold text-green-700">{formatCurrency(resumo.debito_extinto)}</span>
                </div>
              </CardContent>
            </Card>
          </div>
        </div>
      )}

      {/* ── Lista de operações ── */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">
            {pagination ? `${pagination.total.toLocaleString('pt-BR')} operação(ões)` : 'Operações'}
          </CardTitle>
          <p className="text-xs text-muted-foreground">
            Lista completa de operações da empresa, ordenada pela movimentação mais recente — independente do período selecionado acima (que filtra apenas os cards de resumo).
          </p>
        </CardHeader>
        <CardContent className="p-0">
          {loadingOperacoes ? (
            <p className="text-sm text-center text-muted-foreground py-8">Carregando...</p>
          ) : operacoes.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">
              <Inbox className="mx-auto h-12 w-12 mb-3 opacity-30" />
              <p>Nenhuma operação encontrada.</p>
              <p className="text-xs mt-1">As operações aparecem aqui assim que o CGIBS enviar a conta corrente fiscal.</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Chave de acesso</TableHead>
                  <TableHead>CNPJ fornecedor</TableHead>
                  <TableHead>CNPJ adquirente</TableHead>
                  <TableHead>Emissão</TableHead>
                  <TableHead>Atualizado em</TableHead>
                  <TableHead className="text-right">Ações</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {operacoes.map(op => (
                  <Fragment key={op.id}>
                    <TableRow>
                      <TableCell className="font-mono text-xs" title={op.chave_acesso}>{fmtChave(op.chave_acesso)}</TableCell>
                      <TableCell className="font-mono text-xs">{fmtCNPJBase(op.cnpj_fornecedor)}</TableCell>
                      <TableCell className="font-mono text-xs">{fmtCNPJBase(op.cnpj_adquirente)}</TableCell>
                      <TableCell className="text-xs">{fmtDateTime(op.dth_emissao)}</TableCell>
                      <TableCell className="text-xs">{fmtDateTime(op.updated_at)}</TableCell>
                      <TableCell className="text-right">
                        <Button size="sm" variant="ghost" onClick={() => toggleLancamentos(op.id)}>
                          {expandedId === op.id ? <ChevronUp className="h-3.5 w-3.5 mr-1" /> : <ChevronDown className="h-3.5 w-3.5 mr-1" />}
                          Ver lançamentos
                        </Button>
                      </TableCell>
                    </TableRow>
                    {expandedId === op.id && (
                      <TableRow>
                        <TableCell colSpan={6} className="bg-muted/30 p-3">
                          {loadingLancamentos === op.id ? (
                            <p className="text-xs text-center text-muted-foreground py-4">Carregando lançamentos...</p>
                          ) : (lancamentosById[op.id]?.length ?? 0) === 0 ? (
                            <p className="text-xs text-center text-muted-foreground py-4">Nenhum lançamento para esta operação.</p>
                          ) : (
                            <div className="overflow-x-auto">
                              <table className="w-full text-xs">
                                <thead>
                                  <tr className="border-b text-muted-foreground uppercase tracking-wide">
                                    <th className="text-left px-2 py-1 font-medium">Data</th>
                                    <th className="text-left px-2 py-1 font-medium">Mov.</th>
                                    <th className="text-left px-2 py-1 font-medium">Descrição</th>
                                    <th className="text-right px-2 py-1 font-medium">Rec. disponível</th>
                                    <th className="text-right px-2 py-1 font-medium">Rec. a transferir</th>
                                    <th className="text-right px-2 py-1 font-medium">Créd. a apropriar</th>
                                    <th className="text-right px-2 py-1 font-medium">Créd. não utiliz.</th>
                                    <th className="text-right px-2 py-1 font-medium">Créd. utilizado</th>
                                    <th className="text-right px-2 py-1 font-medium">Déb. em aberto</th>
                                    <th className="text-right px-2 py-1 font-medium">Déb. extinto</th>
                                  </tr>
                                </thead>
                                <tbody className="divide-y">
                                  {lancamentosById[op.id]?.map(l => (
                                    <tr key={l.id}>
                                      <td className="px-2 py-1">{fmtDateTime(l.dth_lancto)}</td>
                                      <td className="px-2 py-1 font-mono">{l.mov_codigo !== null ? `MOV ${l.mov_codigo}` : '—'}</td>
                                      <td className="px-2 py-1">{l.mov_descricao || '—'}</td>
                                      <td className="px-2 py-1 text-right font-mono">{formatCurrency(l.recurso_financeiro_disponivel)}</td>
                                      <td className="px-2 py-1 text-right font-mono">{formatCurrency(l.recurso_financeiro_a_transferir)}</td>
                                      <td className="px-2 py-1 text-right font-mono">{formatCurrency(l.credito_a_apropriar)}</td>
                                      <td className="px-2 py-1 text-right font-mono">{formatCurrency(l.credito_nao_utilizado)}</td>
                                      <td className="px-2 py-1 text-right font-mono">{formatCurrency(l.credito_utilizado)}</td>
                                      <td className="px-2 py-1 text-right font-mono text-orange-600">{formatCurrency(l.debito_em_aberto)}</td>
                                      <td className="px-2 py-1 text-right font-mono text-green-700">{formatCurrency(l.debito_extinto)}</td>
                                    </tr>
                                  ))}
                                </tbody>
                              </table>
                            </div>
                          )}
                        </TableCell>
                      </TableRow>
                    )}
                  </Fragment>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* ── Paginação ── */}
      {pagination && pagination.total_pages > 1 && (
        <div className="flex items-center justify-between text-sm text-muted-foreground">
          <span>Página {pagination.page} de {pagination.total_pages}</span>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => handlePage(page - 1)} disabled={page <= 1}>
              <ChevronLeft className="h-4 w-4" />
            </Button>
            <Button variant="outline" size="sm" onClick={() => handlePage(page + 1)} disabled={page >= pagination.total_pages}>
              <ChevronRight className="h-4 w-4" />
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
