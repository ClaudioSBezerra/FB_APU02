import { useState, useEffect, useCallback } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { RefreshCw, Search, Info, ChevronLeft, ChevronRight } from 'lucide-react'

interface Debito {
  id: string
  chave_nfe: string
  modelo: number
  serie: string
  numero_nfe: string
  data_emissao?: string
  mes_ano: string
  emit_cnpj: string
  dest_cnpj_cpf: string
  valor_nf: number
  valor_ibs_uf: number
  valor_ibs_mun: number
  valor_ibs_total: number
  cancelado: string
}

interface Resumo {
  total_documentos: number
  valor_ibs_total: number
  valor_ibs_uf: number
  valor_ibs_mun: number
}

interface Pagination {
  page: number
  page_size: number
  total: number
  total_pages: number
}

function fmt(v: number) {
  return v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}
function fmtCur(v: number) {
  return v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' })
}
function fmtCNPJ(c: string) {
  const d = c.replace(/\D/g, '')
  if (d.length === 14) return `${d.slice(0,2)}.${d.slice(2,5)}.${d.slice(5,8)}/${d.slice(8,12)}-${d.slice(12)}`
  return c
}
function fmtDate(s?: string) {
  if (!s) return '—'
  return new Date(s).toLocaleDateString('pt-BR')
}

export default function CGIBSDebitos() {
  const [debitos, setDebitos] = useState<Debito[]>([])
  const [resumo, setResumo] = useState<Resumo | null>(null)
  const [pagination, setPagination] = useState<Pagination | null>(null)
  const [periodos, setPeriodos] = useState<string[]>([])
  const [mesSelecionado, setMesSelecionado] = useState('')
  const [filterChave, setFilterChave] = useState('')
  const [filterEmit, setFilterEmit] = useState('')
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/cgibs/debitos/periodos')
      .then(r => r.ok ? r.json() : { periodos: [] })
      .then(data => setPeriodos(data.periodos || []))
  }, [])

  const fetchDebitos = useCallback(async (p = 1) => {
    setLoading(true)
    const params = new URLSearchParams()
    if (mesSelecionado) params.set('mes_ano', mesSelecionado)
    if (filterChave)    params.set('chave', filterChave)
    if (filterEmit)     params.set('emit_cnpj', filterEmit)
    params.set('page', String(p))
    params.set('page_size', '100')

    try {
      const res = await fetch(`/api/cgibs/debitos?${params}`)
      if (res.ok) {
        const data = await res.json()
        setDebitos(data.debitos || [])
        setResumo(data.resumo || null)
        setPagination(data.pagination || null)
      }
    } finally {
      setLoading(false)
    }
  }, [mesSelecionado, filterChave, filterEmit])

  useEffect(() => {
    setPage(1)
    fetchDebitos(1)
  }, [mesSelecionado])

  function handleSearch(e: React.FormEvent) {
    e.preventDefault()
    setPage(1)
    fetchDebitos(1)
  }

  function handlePage(p: number) {
    setPage(p)
    fetchDebitos(p)
  }

  return (
    <div className="space-y-4">
      {/* ── Cabeçalho ── */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold">Débitos IBS — mês corrente</h1>
          <p className="text-sm text-muted-foreground">
            Documentos fiscais de saída com valores IBS calculados internamente
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => fetchDebitos(page)}>
          <RefreshCw className="h-4 w-4 mr-2" /> Atualizar
        </Button>
      </div>

      {/* ── Banner fonte interna ── */}
      <div className="flex items-start gap-3 rounded-lg border border-blue-200 bg-blue-50 px-4 py-3 text-blue-900 text-sm">
        <Info className="h-4 w-4 shrink-0 mt-0.5 text-blue-600" />
        <p>
          <strong>Fonte interna</strong> — valores extraídos das tags <code>vIBSUF</code> e <code>vIBSMun</code>
          das NF-e de saída importadas via ERP Bridge. Quando a API CGIBS estiver disponível, esses dados
          serão substituídos pela apuração oficial do Comitê Gestor.
        </p>
      </div>

      {/* ── KPI Cards ── */}
      {resumo && (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <Card className="p-3">
            <p className="text-xs text-muted-foreground">Total de documentos</p>
            <p className="text-xl font-bold">{resumo.total_documentos.toLocaleString('pt-BR')}</p>
          </Card>
          <Card className="p-3">
            <p className="text-xs text-muted-foreground">IBS Total</p>
            <p className="text-xl font-bold text-red-600">{fmtCur(resumo.valor_ibs_total)}</p>
          </Card>
          <Card className="p-3">
            <p className="text-xs text-muted-foreground">IBS UF (Estadual)</p>
            <p className="text-xl font-bold">{fmtCur(resumo.valor_ibs_uf)}</p>
          </Card>
          <Card className="p-3">
            <p className="text-xs text-muted-foreground">IBS Municipal</p>
            <p className="text-xl font-bold">{fmtCur(resumo.valor_ibs_mun)}</p>
          </Card>
        </div>
      )}

      {/* ── Filtros ── */}
      <Card>
        <CardContent className="pt-4">
          <form onSubmit={handleSearch} className="flex flex-wrap gap-3 items-end">
            <div className="w-40">
              <p className="text-xs text-muted-foreground mb-1">Competência</p>
              <Select value={mesSelecionado || '_all'} onValueChange={v => setMesSelecionado(v === '_all' ? '' : v)}>
                <SelectTrigger>
                  <SelectValue placeholder="Todos" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="_all">Todos</SelectItem>
                  {periodos.map(p => (
                    <SelectItem key={p} value={p}>{p}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex-1 min-w-40">
              <p className="text-xs text-muted-foreground mb-1">Chave NF-e</p>
              <Input placeholder="Buscar chave..." value={filterChave}
                onChange={e => setFilterChave(e.target.value)} />
            </div>
            <div className="w-44">
              <p className="text-xs text-muted-foreground mb-1">CNPJ Emitente</p>
              <Input placeholder="CNPJ emitente" value={filterEmit}
                onChange={e => setFilterEmit(e.target.value.replace(/\D/g, ''))} maxLength={14} />
            </div>
            <Button type="submit" size="sm">
              <Search className="h-4 w-4 mr-2" /> Buscar
            </Button>
          </form>
        </CardContent>
      </Card>

      {/* ── Tabela ── */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">
            {pagination ? `${pagination.total.toLocaleString('pt-BR')} documento(s)` : 'Carregando...'}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {loading ? (
            <p className="text-sm text-center text-muted-foreground py-8">Carregando...</p>
          ) : debitos.length === 0 ? (
            <p className="text-sm text-center text-muted-foreground py-8">
              Nenhum documento com IBS encontrado para os filtros selecionados.
            </p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b bg-muted/40 text-muted-foreground uppercase tracking-wide">
                    <th className="text-left px-3 py-2 font-medium">Competência</th>
                    <th className="text-left px-3 py-2 font-medium">Série / Nº</th>
                    <th className="text-left px-3 py-2 font-medium">Emissão</th>
                    <th className="text-left px-3 py-2 font-medium">Emitente</th>
                    <th className="text-right px-3 py-2 font-medium">Valor NF</th>
                    <th className="text-right px-3 py-2 font-medium">IBS UF</th>
                    <th className="text-right px-3 py-2 font-medium">IBS Mun</th>
                    <th className="text-right px-3 py-2 font-medium">IBS Total</th>
                    <th className="text-center px-3 py-2 font-medium">Status</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {debitos.map(d => (
                    <tr key={d.id} className="hover:bg-muted/20 transition-colors">
                      <td className="px-3 py-2 font-mono">{d.mes_ano}</td>
                      <td className="px-3 py-2 font-mono">
                        {d.serie && d.numero_nfe ? `${d.serie}/${d.numero_nfe}` : d.chave_nfe.slice(0, 8) + '...'}
                      </td>
                      <td className="px-3 py-2">{fmtDate(d.data_emissao)}</td>
                      <td className="px-3 py-2 font-mono">{fmtCNPJ(d.emit_cnpj)}</td>
                      <td className="px-3 py-2 text-right font-mono">{fmt(d.valor_nf)}</td>
                      <td className="px-3 py-2 text-right font-mono text-red-600">{fmt(d.valor_ibs_uf)}</td>
                      <td className="px-3 py-2 text-right font-mono text-red-600">{fmt(d.valor_ibs_mun)}</td>
                      <td className="px-3 py-2 text-right font-mono font-semibold text-red-700">{fmt(d.valor_ibs_total)}</td>
                      <td className="px-3 py-2 text-center">
                        {d.cancelado === 'S'
                          ? <Badge variant="destructive" className="text-xs">Cancelada</Badge>
                          : <Badge variant="outline" className="text-xs text-green-700 border-green-300">Normal</Badge>
                        }
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      {/* ── Paginação ── */}
      {pagination && pagination.total_pages > 1 && (
        <div className="flex items-center justify-between text-sm text-muted-foreground">
          <span>
            Página {pagination.page} de {pagination.total_pages}
          </span>
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
