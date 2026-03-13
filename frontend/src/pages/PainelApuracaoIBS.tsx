import { useState, useEffect, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { AlertCircle, TrendingDown, TrendingUp, Scale } from "lucide-react"

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

function fmtParen(v: number) {
  if (v === 0) return "R$ 0,00"
  return `(${fmt(v)})`
}

// ---------------------------------------------------------------------------
// Componente
// ---------------------------------------------------------------------------
export default function PainelApuracaoIBS() {
  const [data, setData] = useState<PainelData | null>(null)
  const [mesSelecionado, setMesSelecionado] = useState<string>("")
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const token = localStorage.getItem("token") || ""
  const companyID = localStorage.getItem("company_id") || ""

  const fetchData = useCallback(async (mes?: string) => {
    setLoading(true)
    setError(null)
    try {
      const params = mes ? `?mes_ano=${encodeURIComponent(mes)}` : ""
      const res = await fetch(`/api/apuracao/painel${params}`, {
        headers: {
          Authorization: `Bearer ${token}`,
          "X-Company-ID": companyID,
        },
      })
      if (!res.ok) throw new Error("Erro ao carregar dados")
      const json: PainelData = await res.json()
      setData(json)
      if (!mes) setMesSelecionado(json.mes_selecionado)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [token, companyID])

  useEffect(() => { fetchData() }, [fetchData])

  function handleMesChange(mes: string) {
    setMesSelecionado(mes)
    fetchData(mes)
  }

  const ibs = data?.ibs

  // Cor do saldo: verde = crédito a favor (≤0), vermelho = a recolher (>0)
  function saldoCor(v: number) {
    return v > 0 ? "text-red-600" : "text-green-600"
  }
  function saldoLabel(v: number) {
    return v > 0 ? "A Recolher" : v < 0 ? "Crédito a Favor" : "Zerado"
  }

  return (
    <div className="space-y-6">
      {/* ── Cabeçalho ── */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold">Apuração IBS</h1>
          <p className="text-sm text-muted-foreground">
            Imposto sobre Bens e Serviços — parcelas estadual (UF) e municipal
          </p>
        </div>
        <div className="w-44">
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

      {/* ── Erro ── */}
      {error && (
        <div className="flex items-center gap-2 text-sm text-red-600 bg-red-50 border border-red-200 rounded p-3">
          <AlertCircle className="h-4 w-4 shrink-0" />
          {error}
        </div>
      )}

      {/* ── KPI Cards ── */}
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

        <Card className="border-2">
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
                  <th className="text-right py-2 px-4 font-medium">IBS UF (Estadual)</th>
                  <th className="text-right py-2 px-4 font-medium">IBS Mun (Municipal)</th>
                  <th className="text-right py-2 pl-4 font-medium">Total IBS</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                <tr>
                  <td className="py-2.5 pr-4 text-muted-foreground">Débito — NF-e Saídas</td>
                  <td className="py-2.5 px-4 text-right font-mono">{fmt(ibs.debito_uf)}</td>
                  <td className="py-2.5 px-4 text-right font-mono">{fmt(ibs.debito_mun)}</td>
                  <td className="py-2.5 pl-4 text-right font-mono font-medium">{fmt(ibs.debito_total)}</td>
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
                    {fmt(ibs.saldo_uf)}
                  </td>
                  <td className={`py-3 px-4 text-right font-mono font-bold ${saldoCor(ibs.saldo_mun)}`}>
                    {fmt(ibs.saldo_mun)}
                  </td>
                  <td className={`py-3 pl-4 text-right font-mono font-bold text-base ${saldoCor(ibs.saldo_total)}`}>
                    {fmt(ibs.saldo_total)}
                  </td>
                </tr>
              </tfoot>
            </table>
          )}
        </CardContent>
      </Card>

      {/* ── Nota metodológica ── */}
      <p className="text-xs text-muted-foreground border-t pt-3">
        <strong>IBS UF</strong> = parcela estadual do IBS (tag <code>vIBSUF</code>). &nbsp;
        <strong>IBS Mun</strong> = parcela municipal (tag <code>vIBSMun</code>). &nbsp;
        Saldo <span className="text-green-600 font-medium">verde</span> = crédito acumulado a favor da empresa. &nbsp;
        Saldo <span className="text-red-600 font-medium">vermelho</span> = imposto a recolher. &nbsp;
        CT-e não desagrega UF/Mun — apenas o total IBS é considerado.
      </p>
    </div>
  )
}
