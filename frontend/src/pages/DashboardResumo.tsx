import { useState, useEffect } from 'react'
import { BarChart, Bar, XAxis, YAxis, Tooltip, Legend, ResponsiveContainer } from 'recharts'
import { useAuth } from '@/contexts/AuthContext'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatCurrency } from '@/lib/utils'

interface BlocoDoc {
  count: number
  v_nf?: number
  v_prest?: number
  v_ibs: number
  v_cbs: number
  v_bc_ibs_cbs: number
}

interface DashboardData {
  mes_ano: string
  nfe_entradas: BlocoDoc
  nfe_saidas: BlocoDoc
  cte_entradas: BlocoDoc
  total_creditos_ibs: number
  total_creditos_cbs: number
  total_debitos_ibs: number
  total_debitos_cbs: number
  saldo_ibs: number
  saldo_cbs: number
  creditos_apropriar: number
  aliquota_efetiva_ibs: number | null
}

export default function DashboardResumo() {
  const [mes, setMes] = useState<string>(new Date().toISOString().slice(0, 7))
  const [data, setData] = useState<DashboardData | null>(null)
  const [loading, setLoading] = useState(false)
  const [erro, setErro] = useState<string | null>(null)
  const [chartMode, setChartMode] = useState<'quantidade' | 'valores'>('quantidade')
  const { companyId } = useAuth()

  useEffect(() => {
    if (!companyId) return
    setLoading(true)
    setErro(null)
    fetch(`/api/dashboard/resumo?mes=${mes}`)
      .then(async res => {
        if (!res.ok) {
          const body = await res.json().catch(() => ({ error: 'Erro desconhecido' }))
          throw new Error(body.error ?? 'Erro ao carregar resumo fiscal')
        }
        return res.json() as Promise<DashboardData>
      })
      .then(json => setData(json))
      .catch((e: Error) => setErro(e.message))
      .finally(() => setLoading(false))
  }, [mes, companyId])

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Resumo Fiscal</h1>
          <p className="text-muted-foreground text-sm">Visão consolidada dos documentos fiscais e apuração IBS/CBS</p>
        </div>
        <input
          type="month"
          value={mes}
          onChange={e => setMes(e.target.value)}
          className="border rounded px-3 py-2 text-sm"
        />
      </div>

      {loading && <p className="text-muted-foreground">Carregando...</p>}
      {erro && <p className="text-red-600">{erro}</p>}

      {data && (
        <>
          {/* KPI Cards */}
          <div className="grid grid-cols-2 lg:grid-cols-5 gap-4">
            {/* Card 1: Total de Documentos */}
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">Total de Documentos</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold">
                  {(data.nfe_entradas.count + data.nfe_saidas.count + data.cte_entradas.count).toLocaleString('pt-BR')}
                </p>
                <p className="text-xs text-muted-foreground mt-1">
                  Entrada: {(data.nfe_entradas.count + data.cte_entradas.count).toLocaleString('pt-BR')} | Saída: {data.nfe_saidas.count.toLocaleString('pt-BR')}
                </p>
              </CardContent>
            </Card>

            {/* Card 2: Total de Créditos */}
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">Total de Créditos</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold">{formatCurrency(data.total_creditos_ibs + data.total_creditos_cbs)}</p>
                <p className="text-xs text-muted-foreground mt-1">
                  IBS: {formatCurrency(data.total_creditos_ibs)} | CBS: {formatCurrency(data.total_creditos_cbs)}
                </p>
              </CardContent>
            </Card>

            {/* Card 3: Total de Débitos */}
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">Total de Débitos</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold">{formatCurrency(data.total_debitos_ibs + data.total_debitos_cbs)}</p>
                <p className="text-xs text-muted-foreground mt-1">
                  IBS: {formatCurrency(data.total_debitos_ibs)} | CBS: {formatCurrency(data.total_debitos_cbs)}
                </p>
              </CardContent>
            </Card>

            {/* Card 4: Créditos a Apropriar */}
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">Créditos a Apropriar</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold">{formatCurrency(data.creditos_apropriar)}</p>
              </CardContent>
            </Card>

            {/* Card 5: Alíquota Efetiva IBS */}
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">Alíquota Efetiva IBS</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold">
                  {data.aliquota_efetiva_ibs !== null ? `${data.aliquota_efetiva_ibs.toFixed(2)}%` : '--'}
                </p>
              </CardContent>
            </Card>
          </div>

          {/* Bottom section: Chart + Summary */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {/* Gráfico de Documentos por Tipo */}
            <Card>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <CardTitle className="text-base">Documentos por Tipo</CardTitle>
                  <div className="flex gap-2">
                    <button
                      onClick={() => setChartMode('quantidade')}
                      className={`text-xs px-3 py-1 rounded ${chartMode === 'quantidade' ? 'bg-primary text-primary-foreground' : 'border'}`}
                    >Quantidade</button>
                    <button
                      onClick={() => setChartMode('valores')}
                      className={`text-xs px-3 py-1 rounded ${chartMode === 'valores' ? 'bg-primary text-primary-foreground' : 'border'}`}
                    >Valores</button>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <ResponsiveContainer width="100%" height={200}>
                  <BarChart
                    layout="vertical"
                    data={[
                      {
                        tipo: 'NF-e Entradas',
                        entrada: chartMode === 'quantidade' ? data.nfe_entradas.count : (data.nfe_entradas.v_nf ?? 0),
                        saida: 0,
                      },
                      {
                        tipo: 'NF-e Saídas',
                        entrada: 0,
                        saida: chartMode === 'quantidade' ? data.nfe_saidas.count : (data.nfe_saidas.v_nf ?? 0),
                      },
                      {
                        tipo: 'CT-e Entradas',
                        entrada: chartMode === 'quantidade' ? data.cte_entradas.count : (data.cte_entradas.v_prest ?? 0),
                        saida: 0,
                      },
                    ]}
                    margin={{ top: 0, right: 20, left: 80, bottom: 0 }}
                  >
                    <XAxis type="number" tick={{ fontSize: 11 }} />
                    <YAxis type="category" dataKey="tipo" tick={{ fontSize: 11 }} width={80} />
                    <Tooltip
                      formatter={(value: number | undefined) => {
                        const v = value ?? 0
                        return chartMode === 'valores' ? formatCurrency(v) : v.toLocaleString('pt-BR')
                      }}
                    />
                    <Legend wrapperStyle={{ fontSize: 11 }} />
                    <Bar dataKey="entrada" name="Entrada" fill="#22c55e" radius={[0, 4, 4, 0]} />
                    <Bar dataKey="saida" name="Saída" fill="#ef4444" radius={[0, 4, 4, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </CardContent>
            </Card>

            {/* Resumo de Créditos e Débitos */}
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Resumo de Créditos e Débitos</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="grid grid-cols-3 text-xs font-medium text-muted-foreground border-b pb-2">
                  <span>Item</span><span className="text-right">IBS</span><span className="text-right">CBS</span>
                </div>
                {[
                  { label: 'Débitos', ibs: data.total_debitos_ibs, cbs: data.total_debitos_cbs },
                  { label: 'Créditos', ibs: data.total_creditos_ibs, cbs: data.total_creditos_cbs },
                  { label: 'Saldo a Recolher', ibs: data.saldo_ibs, cbs: data.saldo_cbs },
                ].map(row => (
                  <div key={row.label} className="grid grid-cols-3 text-sm py-1 border-b last:border-0">
                    <span className="font-medium">{row.label}</span>
                    <span className="text-right">{formatCurrency(row.ibs)}</span>
                    <span className="text-right">{formatCurrency(row.cbs)}</span>
                  </div>
                ))}
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </div>
  )
}
