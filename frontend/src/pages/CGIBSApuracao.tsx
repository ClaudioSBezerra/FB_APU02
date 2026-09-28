import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { toast } from 'sonner'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import {
  Globe, RefreshCw, AlertTriangle, Trash2, CheckCircle2, Info,
  ExternalLink, CalendarClock, Send, FileBarChart, X,
} from 'lucide-react'

interface CGIBSSolicitacao {
  id: string
  cnpj_base: string
  id_solicitacao_externo: number | null
  tipo_solicitacao: string
  situacao_solicitacao: string
  data_solicitacao: string | null
  data_transacao_ini: string | null
  data_transacao_fim: string | null
  qtd_operacoes: number
  qtd_arq_vinculados: number
  error_message: string
  created_at: string
  updated_at: string
}

// Situações em andamento não podem ser removidas do histórico (ainda não há
// resultado nem erro definitivo) — o botão de excluir só aparece nas demais.
const SITUACOES_EM_ANDAMENTO = ['solicitada', 'enviada']

const situacaoConfig: Record<string, { label: string; color: string }> = {
  solicitada: { label: 'Em andamento', color: 'bg-yellow-100 text-yellow-700' },
  enviada:    { label: 'Em andamento', color: 'bg-yellow-100 text-yellow-700' },
  gerada:     { label: 'Arquivo pronto', color: 'bg-blue-100 text-blue-700' },
  cancelada:  { label: 'Cancelada',    color: 'bg-gray-100 text-gray-700' },
  expirada:   { label: 'Expirada',     color: 'bg-gray-100 text-gray-700' },
}

function situacaoBadge(s: CGIBSSolicitacao): { label: string; color: string } {
  // "Erro" é um estado visual derivado no frontend — não existe como valor de
  // situacao_solicitacao, é sinalizado só pela presença de error_message.
  if (s.error_message) return { label: 'Erro', color: 'bg-red-100 text-red-700' }
  return situacaoConfig[s.situacao_solicitacao] || { label: s.situacao_solicitacao, color: 'bg-gray-100 text-gray-700' }
}

function fmtCNPJBase(cnpj: string): string {
  if (cnpj && cnpj.length === 8) return `${cnpj.slice(0, 2)}.${cnpj.slice(2, 5)}.${cnpj.slice(5)}`
  return cnpj || '—'
}

function fmtDate(s: string | null): string {
  if (!s) return '—'
  return new Date(s).toLocaleDateString('pt-BR')
}

function fmtDateTime(s: string | null): string {
  if (!s) return '—'
  return new Date(s).toLocaleString('pt-BR')
}

async function extractError(res: Response, fallback: string): Promise<string> {
  try {
    const data = await res.json()
    return data?.error || fallback
  } catch {
    return fallback
  }
}

export default function CGIBSApuracao() {
  const [solicitacoes, setSolicitacoes] = useState<CGIBSSolicitacao[]>([])
  const [loading, setLoading] = useState(true)
  const [soliciting, setSoliciting] = useState(false)
  const [showForm, setShowForm] = useState(false)
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  const fetchStatus = useCallback(async () => {
    try {
      const res = await fetch('/api/cgibs/apuracao/status')
      if (res.ok) {
        const data = await res.json()
        setSolicitacoes(data.solicitacoes || [])
      } else {
        toast.error(await extractError(res, 'Erro ao carregar histórico de solicitações'))
      }
    } catch (error: any) {
      toast.error(error?.message || 'Erro de conexão')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchStatus() }, [fetchStatus])

  const handleSolicitar = async () => {
    if (!dataIni || !dataFim) {
      toast.error('Selecione a data de início e a data de fim do período')
      return
    }
    if (dataFim < dataIni) {
      toast.error('A data de fim deve ser igual ou posterior à data de início')
      return
    }
    setSoliciting(true)
    try {
      const res = await fetch('/api/cgibs/apuracao/solicitar', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ data_ini: dataIni, data_fim: dataFim }),
      })
      if (res.status === 201) {
        const data = await res.json()
        toast.success(data.message || 'Solicitação enviada!')
        setShowForm(false)
        setDataIni('')
        setDataFim('')
        fetchStatus()
      } else if (res.status === 503) {
        toast.info(await extractError(res, 'API CGIBS em fase piloto — integração ainda não configurada.'))
      } else {
        toast.error(await extractError(res, 'Erro ao solicitar apuração IBS'))
      }
    } catch (error: any) {
      toast.error(error?.message || 'Erro de conexão')
    } finally {
      setSoliciting(false)
    }
  }

  const handleDelete = async (id: string) => {
    if (!confirm('Remover este registro do histórico?')) return
    try {
      const res = await fetch(`/api/cgibs/apuracao/${id}`, { method: 'DELETE' })
      if (res.status === 204) {
        setSolicitacoes(prev => prev.filter(s => s.id !== id))
      } else {
        toast.error(await extractError(res, 'Erro ao remover registro'))
      }
    } catch (error: any) {
      toast.error(error?.message || 'Erro de conexão')
    }
  }

  const handleClearErrors = async () => {
    if (!confirm('Limpar todos os registros com erro?')) return
    try {
      const res = await fetch('/api/cgibs/apuracao/clear-errors', { method: 'DELETE' })
      if (res.ok) {
        const data = await res.json().catch(() => null)
        toast.success(data?.message || 'Registros com erro removidos.')
        setSolicitacoes(prev => prev.filter(s => !s.error_message))
      } else {
        toast.error(await extractError(res, 'Erro ao limpar registros com erro'))
      }
    } catch (error: any) {
      toast.error(error?.message || 'Erro de conexão')
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary" />
      </div>
    )
  }

  return (
    <div className="max-w-5xl mx-auto px-4 py-6">
      {/* ── Cabeçalho ── */}
      <div className="md:flex md:items-center md:justify-between mb-6">
        <div>
          <h2 className="text-2xl font-bold flex items-center gap-2">
            <Globe className="h-6 w-6" />
            Importação dos Movimentos IBS
          </h2>
          <p className="mt-1 text-sm text-gray-600">
            Solicite e acompanhe a importação da conta corrente fiscal IBS diretamente do CGIBS — Comitê Gestor do IBS.
          </p>
        </div>
        <div className="mt-4 md:mt-0 flex gap-2">
          {solicitacoes.some(s => s.error_message) && (
            <Button variant="outline" className="text-red-600 hover:text-red-700 hover:bg-red-50" onClick={handleClearErrors}>
              <Trash2 className="mr-2 h-4 w-4" /> Limpar erros
            </Button>
          )}
          <Button variant="outline" onClick={fetchStatus}>
            <RefreshCw className="mr-2 h-4 w-4" /> Atualizar
          </Button>
          <Button onClick={() => setShowForm(v => !v)} variant="default">
            <Send className="mr-2 h-4 w-4" />
            Solicitar Apuração IBS
          </Button>
        </div>
      </div>

      {/* ── Banner: API em fase piloto ── */}
      <div className="mb-4 flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-amber-900">
        <Info className="h-5 w-5 shrink-0 mt-0.5 text-amber-600" />
        <div className="text-sm space-y-1">
          <p className="font-semibold">API CGIBS em fase piloto — acesso restrito</p>
          <p>
            A apuração assistida do IBS está em fase piloto com
            <strong> 123 empresas selecionadas</strong> (jan–mar/2026), desenvolvida pelo CGIBS em parceria com a SVRS.
            A integração completa será disponibilizada assim que o CGIBS liberar o acesso irrestrito.
          </p>
          <a
            href="https://www.servicos.cgibs.gov.br/"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 font-medium underline underline-offset-2 hover:text-amber-700"
          >
            <ExternalLink className="h-3.5 w-3.5" />
            Acessar Portal CGIBS
          </a>
        </div>
      </div>

      {/* ── Formulário de período (abre antes de confirmar a solicitação) ── */}
      {showForm && (
        <Card className="mb-4 border-primary/30">
          <CardContent className="pt-4">
            <div className="flex flex-wrap items-end gap-3">
              <div className="flex flex-col gap-1">
                <label className="text-xs text-muted-foreground">Data início</label>
                <Input
                  type="date"
                  value={dataIni}
                  onChange={e => setDataIni(e.target.value)}
                  className="h-9 w-40"
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs text-muted-foreground">Data fim</label>
                <Input
                  type="date"
                  value={dataFim}
                  onChange={e => setDataFim(e.target.value)}
                  className="h-9 w-40"
                />
              </div>
              <Button onClick={handleSolicitar} disabled={soliciting}>
                {soliciting ? 'Enviando...' : 'Confirmar'}
              </Button>
              <Button
                variant="ghost"
                onClick={() => { setShowForm(false); setDataIni(''); setDataFim('') }}
              >
                <X className="mr-2 h-4 w-4" /> Cancelar
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {/* ── Histórico de Solicitações ── */}
      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Histórico de Solicitações</CardTitle>
          <CardDescription>
            Registro de todas as importações da conta corrente fiscal IBS do CGIBS.
            Para ver o resultado detalhado (operações e lançamentos), acesse{' '}
            <Link to="/cgibs/extrato" className="font-medium underline underline-offset-2 inline-flex items-center gap-1">
              <FileBarChart className="h-3.5 w-3.5" /> Extrato IBS
            </Link>.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {solicitacoes.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">
              <Globe className="mx-auto h-12 w-12 mb-3 opacity-30" />
              <p>Nenhuma solicitação realizada.</p>
              <p className="text-xs mt-1">
                Clique em "Solicitar Apuração IBS" e escolha o período para começar.
              </p>
            </div>
          ) : (
            <div className="space-y-4">
              {solicitacoes.map((s) => {
                const badge = situacaoBadge(s)
                const isPending = SITUACOES_EM_ANDAMENTO.includes(s.situacao_solicitacao) && !s.error_message
                const canDelete = !isPending
                return (
                  <div key={s.id} className="rounded-lg border overflow-hidden">
                    <div className="flex items-center justify-between p-4">
                      <div className="flex items-center gap-3">
                        {isPending && <div className="animate-spin rounded-full h-5 w-5 border-b-2 border-primary shrink-0" />}
                        {s.situacao_solicitacao === 'gerada' && !s.error_message && <CheckCircle2 className="h-5 w-5 text-blue-600 shrink-0" />}
                        {s.error_message && <AlertTriangle className="h-5 w-5 text-red-500 shrink-0" />}
                        <div>
                          <div className="flex items-center gap-2 flex-wrap">
                            <span className="font-medium text-sm">CNPJ: {fmtCNPJBase(s.cnpj_base)}</span>
                            <Badge className={badge.color}>{badge.label}</Badge>
                            <Badge variant="outline" className="text-xs">{s.tipo_solicitacao}</Badge>
                            {s.id_solicitacao_externo !== null && (
                              <span className="text-xs text-muted-foreground font-mono">
                                Nº {s.id_solicitacao_externo}
                              </span>
                            )}
                          </div>
                          <p className="text-xs text-muted-foreground mt-0.5">
                            Período: {fmtDate(s.data_transacao_ini)} – {fmtDate(s.data_transacao_fim)}
                            <span className="mx-1">·</span>
                            Solicitado em {fmtDateTime(s.data_solicitacao || s.created_at)}
                          </p>
                          {s.error_message && (
                            <p className="text-xs text-red-600 mt-0.5">{s.error_message}</p>
                          )}
                        </div>
                      </div>
                      <div className="flex items-center gap-3">
                        <div className="text-right text-xs text-muted-foreground hidden sm:block">
                          <div>{s.qtd_operacoes} operações</div>
                          <div>{s.qtd_arq_vinculados} arquivo(s) vinculado(s)</div>
                        </div>
                        {canDelete && (
                          <Button size="sm" variant="ghost" className="text-red-500 hover:bg-red-50 px-2"
                            onClick={() => handleDelete(s.id)}>
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        )}
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </CardContent>
      </Card>

      {/* ── Roadmap ── */}
      <Card className="mt-6 border-dashed">
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-semibold flex items-center gap-2">
            <CalendarClock className="h-4 w-4 text-muted-foreground" />
            Cronograma da API CGIBS
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-2 text-sm">
            <div className="flex items-start gap-3">
              <div className="mt-0.5 h-2 w-2 rounded-full bg-green-500 shrink-0" />
              <div><strong>Jan–Mar 2026</strong> — Fase piloto com 123 empresas selecionadas (NF-e)</div>
            </div>
            <div className="flex items-start gap-3">
              <div className="mt-0.5 h-2 w-2 rounded-full bg-yellow-400 shrink-0" />
              <div><strong>Abr 2026</strong> — Fase 2: expansão para novos contribuintes e documentos fiscais</div>
            </div>
            <div className="flex items-start gap-3">
              <div className="mt-0.5 h-2 w-2 rounded-full bg-gray-300 shrink-0" />
              <div><strong>Previsão 2026</strong> — API pública irrestrita para todos os contribuintes</div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
