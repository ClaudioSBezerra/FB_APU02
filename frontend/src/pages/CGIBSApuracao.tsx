import { useState, useEffect, useCallback } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Globe, RefreshCw, AlertTriangle, Trash2, CheckCircle2, Info, ExternalLink, CalendarClock, Send } from 'lucide-react'

interface CGIBSResumo {
  data_apuracao: string
  total_debitos: number
  valor_ibs_total: number
  valor_ibs_uf: number
  valor_ibs_mun: number
  valor_ibs_nao_extinto: number
  total_corrente: number
  total_ajuste: number
}

interface CGIBSRequest {
  id: string
  cnpj_base: string
  tiquete: string
  status: string
  ambiente: string
  error_code?: string
  error_message?: string
  created_at: string
  updated_at: string
  resumo?: CGIBSResumo
}

const statusConfig: Record<string, { label: string; color: string }> = {
  pending:      { label: 'Pendente',    color: 'bg-gray-100 text-gray-700' },
  requested:    { label: 'Solicitado',  color: 'bg-yellow-100 text-yellow-700' },
  downloading:  { label: 'Baixando',   color: 'bg-blue-100 text-blue-700' },
  completed:    { label: 'Concluído',  color: 'bg-green-100 text-green-700' },
  error:        { label: 'Erro',       color: 'bg-red-100 text-red-700' },
}

function fmt(v: number) {
  return v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' })
}
function fmtCNPJ(cnpj: string) {
  if (cnpj.length === 8) return `${cnpj.slice(0,2)}.${cnpj.slice(2,5)}.${cnpj.slice(5)}`
  return cnpj
}
function fmtPeriodo(p: string) {
  if (p && p.length === 6) return `${p.slice(4,6)}/${p.slice(0,4)}`
  return p || '—'
}

export default function CGIBSApuracao() {
  const [requests, setRequests] = useState<CGIBSRequest[]>([])
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState<{ type: 'success' | 'error' | 'info'; text: string } | null>(null)

  const fetchStatus = useCallback(async () => {
    try {
      const res = await fetch('/api/cgibs/apuracao/status')
      if (res.ok) {
        const data = await res.json()
        setRequests(data.requests || [])
      }
    } catch { /* silent */ }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { fetchStatus() }, [fetchStatus])

  const handleSolicitar = async () => {
    setMessage(null)
    try {
      const res = await fetch('/api/cgibs/apuracao/solicitar', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      })
      const data = await res.json()
      if (res.ok) {
        setMessage({ type: 'success', text: data.message || 'Solicitação enviada!' })
        fetchStatus()
      } else {
        setMessage({ type: 'info', text: data.detail || data.error || 'API CGIBS indisponível no momento.' })
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' })
    }
  }

  const handleDelete = async (id: string) => {
    if (!confirm('Remover este registro do histórico?')) return
    await fetch(`/api/cgibs/apuracao/${id}`, { method: 'DELETE' })
    setRequests(prev => prev.filter(r => r.id !== id))
  }

  const handleClearErrors = async () => {
    if (!confirm('Limpar todos os registros com erro?')) return
    await fetch('/api/cgibs/apuracao/clear-errors', { method: 'DELETE' })
    setRequests(prev => prev.filter(r => r.status !== 'error'))
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
            Importação dos Débitos IBS
          </h2>
          <p className="mt-1 text-sm text-gray-600">
            Solicite e acompanhe a importação de débitos IBS diretamente do CGIBS — Comitê Gestor do IBS.
          </p>
        </div>
        <div className="mt-4 md:mt-0 flex gap-2">
          {requests.some(r => r.status === 'error') && (
            <Button variant="outline" className="text-red-600 hover:text-red-700 hover:bg-red-50" onClick={handleClearErrors}>
              <Trash2 className="mr-2 h-4 w-4" /> Limpar erros
            </Button>
          )}
          <Button variant="outline" onClick={fetchStatus}>
            <RefreshCw className="mr-2 h-4 w-4" /> Atualizar
          </Button>
          <Button onClick={handleSolicitar} variant="default">
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

      {/* ── Mensagem de feedback ── */}
      {message && (
        <div className={`mb-4 rounded-md p-4 text-sm font-medium ${
          message.type === 'success' ? 'bg-green-50 text-green-800' :
          message.type === 'info'    ? 'bg-blue-50 text-blue-800' :
                                       'bg-red-50 text-red-800'
        }`}>
          {message.text}
        </div>
      )}

      {/* ── Histórico de Solicitações ── */}
      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Histórico de Solicitações</CardTitle>
          <CardDescription>
            Registro de todas as importações de débitos IBS do CGIBS.
            Para visualizar os débitos calculados internamente, acesse <strong>Débitos IBS</strong>.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {requests.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">
              <Globe className="mx-auto h-12 w-12 mb-3 opacity-30" />
              <p>Nenhuma solicitação realizada.</p>
              <p className="text-xs mt-1">
                A integração com o CGIBS será habilitada assim que a API for disponibilizada.
              </p>
            </div>
          ) : (
            <div className="space-y-4">
              {requests.map((req) => {
                const sc = statusConfig[req.status] || statusConfig.pending
                const isPending = ['pending', 'requested', 'downloading'].includes(req.status)
                return (
                  <div key={req.id} className="rounded-lg border overflow-hidden">
                    <div className="flex items-center justify-between p-4">
                      <div className="flex items-center gap-3">
                        {isPending && <div className="animate-spin rounded-full h-5 w-5 border-b-2 border-primary shrink-0" />}
                        {req.status === 'completed' && <CheckCircle2 className="h-5 w-5 text-green-600 shrink-0" />}
                        {req.status === 'error' && <AlertTriangle className="h-5 w-5 text-red-500 shrink-0" />}
                        <div>
                          <div className="flex items-center gap-2">
                            <span className="font-medium text-sm">CNPJ: {fmtCNPJ(req.cnpj_base)}</span>
                            <Badge className={sc.color}>{sc.label}</Badge>
                            <Badge variant="outline" className="text-xs">{req.ambiente}</Badge>
                          </div>
                          <p className="text-xs text-muted-foreground mt-0.5">
                            {new Date(req.created_at).toLocaleString('pt-BR')}
                            {req.error_message && (
                              <span className="text-red-600 ml-2">{req.error_message}</span>
                            )}
                          </p>
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        {req.status === 'error' && (
                          <Button size="sm" variant="ghost" className="text-red-500 hover:bg-red-50 px-2"
                            onClick={() => handleDelete(req.id)}>
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        )}
                      </div>
                    </div>

                    {req.status === 'completed' && req.resumo && (
                      <div className="border-t bg-gray-50 px-4 py-3 grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
                        <div>
                          <span className="text-xs text-muted-foreground block">Período</span>
                          <span className="font-semibold">{fmtPeriodo(req.resumo.data_apuracao)}</span>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground block">Total débitos</span>
                          <span className="font-semibold">{req.resumo.total_debitos.toLocaleString('pt-BR')}</span>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground block">IBS Total</span>
                          <span className="font-semibold text-red-600">{fmt(req.resumo.valor_ibs_total)}</span>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground block">IBS Não Extinto</span>
                          <span className="font-semibold text-orange-600">{fmt(req.resumo.valor_ibs_nao_extinto)}</span>
                        </div>
                      </div>
                    )}
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
