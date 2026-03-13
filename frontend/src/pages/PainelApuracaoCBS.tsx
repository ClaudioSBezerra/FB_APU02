import { useState, useEffect, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { AlertCircle, TrendingDown, TrendingUp, Scale } from "lucide-react"

// ---------------------------------------------------------------------------
// Tipos
// ---------------------------------------------------------------------------
interface CBSResult {
  debito_total: number
  qtd_saidas: number
  credito_nfe_total: number
  qtd_entradas: number
  credito_cte: number
  qtd_ctes: number
  saldo_total: number
}

interface PainelData {
  meses_disponiveis: string[]
  mes_selecionado: string
  cbs: CBSResult
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
export default function PainelApuracaoCBS() {
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

  const cbs = data?.cbs

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
          <h1 className="text-xl font-bold">Apuração CBS</h1>
          <p className="text-sm text-muted-foreground">
            Contribuição sobre Bens e Serviços — tributo federal (substitui PIS/Cofins)
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
              Débito CBS Total
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold text-red-600">
              {loading ? "..." : fmt(cbs?.debito_total ?? 0)}
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              {cbs?.qtd_saidas ?? 0} NF-e de saída
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
              <TrendingDown className="h-4 w-4 text-green-500" />
              Crédito CBS Total
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold text-green-600">
              {loading ? "..." : fmt((cbs?.credito_nfe_total ?? 0) + (cbs?.credito_cte ?? 0))}
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              {cbs?.qtd_entradas ?? 0} NF-e + {cbs?.qtd_ctes ?? 0} CT-e de entrada
            </p>
          </CardContent>
        </Card>

        <Card className="border-2">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
              <Scale className="h-4 w-4" />
              Saldo CBS — {mesSelecionado || "—"}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className={`text-2xl font-bold ${saldoCor(cbs?.saldo_total ?? 0)}`}>
              {loading ? "..." : fmt(cbs?.saldo_total ?? 0)}
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              {saldoLabel(cbs?.saldo_total ?? 0)}
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
          ) : !cbs ? (
            <p className="text-sm text-muted-foreground py-6 text-center">
              Nenhum dado disponível para o período selecionado.
            </p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-muted-foreground text-xs uppercase tracking-wide">
                  <th className="text-left py-2 pr-4 font-medium">Origem</th>
                  <th className="text-right py-2 pl-4 font-medium">CBS</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                <tr>
                  <td className="py-2.5 pr-4 text-muted-foreground">Débito — NF-e Saídas</td>
                  <td className="py-2.5 pl-4 text-right font-mono font-medium">{fmt(cbs.debito_total)}</td>
                </tr>
                <tr>
                  <td className="py-2.5 pr-4 text-muted-foreground">Crédito — NF-e Entradas</td>
                  <td className="py-2.5 pl-4 text-right font-mono font-medium text-green-700">{fmtParen(cbs.credito_nfe_total)}</td>
                </tr>
                <tr>
                  <td className="py-2.5 pr-4 text-muted-foreground">Crédito — CT-e Entradas</td>
                  <td className="py-2.5 pl-4 text-right font-mono font-medium text-green-700">{fmtParen(cbs.credito_cte)}</td>
                </tr>
              </tbody>
              <tfoot>
                <tr className="border-t-2">
                  <td className="py-3 pr-4 font-bold">Saldo a Recolher / Favor</td>
                  <td className={`py-3 pl-4 text-right font-mono font-bold text-base ${saldoCor(cbs.saldo_total)}`}>
                    {fmt(cbs.saldo_total)}
                  </td>
                </tr>
              </tfoot>
            </table>
          )}
        </CardContent>
      </Card>

      {/* ── Nota metodológica ── */}
      <p className="text-xs text-muted-foreground border-t pt-3">
        <strong>CBS</strong> = Contribuição sobre Bens e Serviços, tributo federal que substitui PIS e Cofins na Reforma Tributária. &nbsp;
        Saldo <span className="text-green-600 font-medium">verde</span> = crédito acumulado a favor da empresa. &nbsp;
        Saldo <span className="text-red-600 font-medium">vermelho</span> = CBS a recolher à Receita Federal. &nbsp;
        Valores extraídos das tags <code>vCBS</code> nos XMLs das NF-e e CT-e importados.
      </p>
    </div>
  )
}
