import { useState, useEffect, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Badge } from "@/components/ui/badge"
import { AlertCircle, TrendingDown, TrendingUp, Scale, Building2, MapPin, Info, ExternalLink } from "lucide-react"

// ---------------------------------------------------------------------------
// Tipos
// ---------------------------------------------------------------------------
interface IBSResult {
  debito_uf: number
  debito_mun: number
  debito_total: number
  qtd_saidas: number
  credito_nfe_uf: number
  credito_nfe_mun: number
  credito_nfe_total: number
  qtd_entradas: number
  credito_cte: number
  qtd_ctes: number
  saldo_uf: number
  saldo_mun: number
  saldo_total: number
}

interface PainelData {
  meses_disponiveis: string[]
  mes_selecionado: string
  ibs: IBSResult
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function fmt(v: number) {
  return v.toLocaleString("pt-BR", { style: "currency", currency: "BRL" })
}
function fmtNum(v: number) {
  return v.toLocaleString("pt-BR", { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}
function fmtParen(v: number) {
  if (v === 0) return "0,00"
  return `(${fmtNum(v)})`
}
function saldoCor(v: number) {
  return v > 0 ? "text-red-600" : v < 0 ? "text-green-600" : "text-muted-foreground"
}
function saldoLabel(v: number) {
  return v > 0 ? "A Recolher" : v < 0 ? "Crédito a Favor" : "Zerado"
}
function saldoBg(v: number) {
  return v > 0 ? "border-red-200 bg-red-50/40" : v < 0 ? "border-green-200 bg-green-50/40" : ""
}

// ---------------------------------------------------------------------------
// Componente
// ---------------------------------------------------------------------------
export default function CGIBSPainel() {
  const [data, setData] = useState<PainelData | null>(null)
  const [mesSelecionado, setMesSelecionado] = useState<string>("")
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async (mes?: string) => {
    setLoading(true)
    setError(null)
    try {
      const params = mes ? `?mes_ano=${encodeURIComponent(mes)}` : ""
      const res = await fetch(`/api/apuracao/painel${params}`)
      if (!res.ok) throw new Error("Erro ao carregar dados")
      const json: PainelData = await res.json()
      setData(json)
      if (!mes) setMesSelecionado(json.mes_selecionado)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  function handleMesChange(mes: string) {
    setMesSelecionado(mes)
    fetchData(mes)
  }

  const ibs = data?.ibs

  return (
    <div className="space-y-6">

      {/* ── Cabeçalho ── */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <h1 className="text-xl font-bold">Painel de Apuração IBS</h1>
            <Badge variant="outline" className="text-xs font-normal text-amber-700 border-amber-300 bg-amber-50">
              Estimativa interna
            </Badge>
          </div>
          <p className="text-sm text-muted-foreground">
            Imposto sobre Bens e Serviços — Comitê Gestor do IBS &nbsp;·&nbsp;
            <a href="https://www.servicos.cgibs.gov.br/" target="_blank" rel="noopener noreferrer"
              className="inline-flex items-center gap-0.5 hover:underline text-blue-600">
              servicos.cgibs.gov.br <ExternalLink className="h-3 w-3" />
            </a>
          </p>
        </div>
        <div className="w-44 shrink-0">
          <Select value={mesSelecionado} onValueChange={handleMesChange} disabled={loading}>
            <SelectTrigger>
              <SelectValue placeholder="Selecione o mês" />
            </SelectTrigger>
            <SelectContent>
              {(data?.meses_disponiveis ?? []).map((m) => (
                <SelectItem key={m} value={m}>{m}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {/* ── Banner fonte ── */}
      <div className="flex items-start gap-3 rounded-lg border border-blue-200 bg-blue-50 px-4 py-3 text-blue-900 text-sm">
        <Info className="h-4 w-4 shrink-0 mt-0.5 text-blue-600" />
        <p>
          Valores calculados internamente a partir das tags <code className="bg-blue-100 px-1 rounded">vIBSUF</code> e{" "}
          <code className="bg-blue-100 px-1 rounded">vIBSMun</code> das NF-e e CT-e importados via ERP Bridge.
          A apuração oficial pelo CGIBS estará disponível quando a API for liberada para todos os contribuintes.
        </p>
      </div>

      {/* ── Erro ── */}
      {error && (
        <div className="flex items-center gap-2 text-sm text-red-600 bg-red-50 border border-red-200 rounded p-3">
          <AlertCircle className="h-4 w-4 shrink-0" />
          {error}
        </div>
      )}

      {/* ── KPI Cards principais ── */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
              <TrendingUp className="h-4 w-4 text-red-500" />
              Débito IBS Total
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold text-red-600">
              {loading ? "..." : fmt(ibs?.debito_total ?? 0)}
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              {ibs?.qtd_saidas ?? 0} NF-e de saída
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
              <TrendingDown className="h-4 w-4 text-green-500" />
              Crédito IBS Total
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold text-green-600">
              {loading ? "..." : fmt((ibs?.credito_nfe_total ?? 0) + (ibs?.credito_cte ?? 0))}
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              {ibs?.qtd_entradas ?? 0} NF-e + {ibs?.qtd_ctes ?? 0} CT-e de entrada
            </p>
          </CardContent>
        </Card>

        <Card className={`border-2 ${saldoBg(ibs?.saldo_total ?? 0)}`}>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
              <Scale className="h-4 w-4" />
              Saldo IBS — {mesSelecionado || "—"}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className={`text-2xl font-bold ${saldoCor(ibs?.saldo_total ?? 0)}`}>
              {loading ? "..." : fmt(ibs?.saldo_total ?? 0)}
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              {saldoLabel(ibs?.saldo_total ?? 0)}
            </p>
          </CardContent>
        </Card>
      </div>

      {/* ── Cards UF vs Municipal ── */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <Building2 className="h-4 w-4 text-muted-foreground" />
              IBS Estadual (UF)
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex justify-between items-center text-sm">
              <span className="text-muted-foreground">Débito</span>
              <span className="font-mono font-medium text-red-600">{loading ? "..." : fmtNum(ibs?.debito_uf ?? 0)}</span>
            </div>
            <div className="flex justify-between items-center text-sm">
              <span className="text-muted-foreground">Crédito NF-e</span>
              <span className="font-mono font-medium text-green-700">{loading ? "..." : fmtParen(ibs?.credito_nfe_uf ?? 0)}</span>
            </div>
            <div className="border-t pt-2 flex justify-between items-center">
              <span className="text-sm font-semibold">Saldo UF</span>
              <span className={`font-mono font-bold ${saldoCor(ibs?.saldo_uf ?? 0)}`}>
                {loading ? "..." : fmtNum(ibs?.saldo_uf ?? 0)}
              </span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <MapPin className="h-4 w-4 text-muted-foreground" />
              IBS Municipal
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex justify-between items-center text-sm">
              <span className="text-muted-foreground">Débito</span>
              <span className="font-mono font-medium text-red-600">{loading ? "..." : fmtNum(ibs?.debito_mun ?? 0)}</span>
            </div>
            <div className="flex justify-between items-center text-sm">
              <span className="text-muted-foreground">Crédito NF-e</span>
              <span className="font-mono font-medium text-green-700">{loading ? "..." : fmtParen(ibs?.credito_nfe_mun ?? 0)}</span>
            </div>
            <div className="border-t pt-2 flex justify-between items-center">
              <span className="text-sm font-semibold">Saldo Municipal</span>
              <span className={`font-mono font-bold ${saldoCor(ibs?.saldo_mun ?? 0)}`}>
                {loading ? "..." : fmtNum(ibs?.saldo_mun ?? 0)}
              </span>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* ── Tabela detalhada ── */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Detalhamento por Origem</CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <p className="text-sm text-muted-foreground py-6 text-center">Carregando...</p>
          ) : !ibs ? (
            <p className="text-sm text-muted-foreground py-6 text-center">
              Nenhum dado disponível para o período selecionado.
            </p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-muted-foreground text-xs uppercase tracking-wide">
                  <th className="text-left py-2 pr-4 font-medium">Origem</th>
                  <th className="text-right py-2 px-4 font-medium">IBS UF/Est. (R$)</th>
                  <th className="text-right py-2 px-4 font-medium">IBS Mun. (R$)</th>
                  <th className="text-right py-2 pl-4 font-medium">Total IBS (R$)</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                <tr>
                  <td className="py-2.5 pr-4 text-muted-foreground">Débito — NF-e Saídas</td>
                  <td className="py-2.5 px-4 text-right font-mono">{fmtNum(ibs.debito_uf)}</td>
                  <td className="py-2.5 px-4 text-right font-mono">{fmtNum(ibs.debito_mun)}</td>
                  <td className="py-2.5 pl-4 text-right font-mono font-medium">{fmtNum(ibs.debito_total)}</td>
                </tr>
                <tr>
                  <td className="py-2.5 pr-4 text-muted-foreground">Crédito — NF-e Entradas</td>
                  <td className="py-2.5 px-4 text-right font-mono text-green-700">{fmtParen(ibs.credito_nfe_uf)}</td>
                  <td className="py-2.5 px-4 text-right font-mono text-green-700">{fmtParen(ibs.credito_nfe_mun)}</td>
                  <td className="py-2.5 pl-4 text-right font-mono font-medium text-green-700">{fmtParen(ibs.credito_nfe_total)}</td>
                </tr>
                <tr>
                  <td className="py-2.5 pr-4 text-muted-foreground">Crédito — CT-e Entradas</td>
                  <td className="py-2.5 px-4 text-right text-muted-foreground/50">—</td>
                  <td className="py-2.5 px-4 text-right text-muted-foreground/50">—</td>
                  <td className="py-2.5 pl-4 text-right font-mono font-medium text-green-700">{fmtParen(ibs.credito_cte)}</td>
                </tr>
              </tbody>
              <tfoot>
                <tr className="border-t-2">
                  <td className="py-3 pr-4 font-bold">Saldo a Recolher / Favor</td>
                  <td className={`py-3 px-4 text-right font-mono font-bold ${saldoCor(ibs.saldo_uf)}`}>
                    {fmtNum(ibs.saldo_uf)}
                  </td>
                  <td className={`py-3 px-4 text-right font-mono font-bold ${saldoCor(ibs.saldo_mun)}`}>
                    {fmtNum(ibs.saldo_mun)}
                  </td>
                  <td className={`py-3 pl-4 text-right font-mono font-bold text-base ${saldoCor(ibs.saldo_total)}`}>
                    {fmtNum(ibs.saldo_total)}
                  </td>
                </tr>
              </tfoot>
            </table>
          )}
        </CardContent>
      </Card>

      {/* ── Nota metodológica ── */}
      <p className="text-xs text-muted-foreground border-t pt-3">
        <strong>IBS UF</strong> = parcela estadual — tag <code>vIBSUF</code> nas NF-e/CT-e. &nbsp;
        <strong>IBS Mun</strong> = parcela municipal — tag <code>vIBSMun</code>. &nbsp;
        CT-e não desagrega UF/Mun — apenas o total IBS é considerado no crédito. &nbsp;
        Saldo <span className="text-green-600 font-medium">verde</span> = crédito a favor. &nbsp;
        Saldo <span className="text-red-600 font-medium">vermelho</span> = IBS a recolher ao CGIBS.
      </p>
    </div>
  )
}
