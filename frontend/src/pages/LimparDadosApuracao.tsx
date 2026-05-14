import { useState, useEffect } from 'react'
import { useAuth } from '@/contexts/AuthContext'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Button } from '@/components/ui/button'

const TABELAS = [
  { id: 'nfe_entradas', label: 'NF-e Entradas' },
  { id: 'nfe_saidas',   label: 'NF-e Saídas' },
  { id: 'cte_entradas', label: 'CT-e Entradas' },
]

export default function LimparDadosApuracao() {
  const { companyId } = useAuth()
  const [periodos, setPeriodos] = useState<string[]>([])
  const [mesSelecionado, setMesSelecionado] = useState<string>('__all__')
  const [tabelasSelecionadas, setTabelasSelecionadas] = useState<string[]>(
    TABELAS.map(t => t.id)
  )
  const [preview, setPreview] = useState<Record<string, number> | null>(null)
  const [loadingPreview, setLoadingPreview] = useState(false)
  const [executing, setExecuting] = useState(false)
  const [resultado, setResultado] = useState<Record<string, number> | null>(null)
  const [erro, setErro] = useState<string | null>(null)
  const [confirmando, setConfirmando] = useState(false)

  useEffect(() => {
    if (!companyId) return
    fetch('/api/admin/limpeza-base')
      .then(r => r.ok ? r.json() : Promise.reject())
      .then(d => setPeriodos(d.periodos ?? []))
      .catch(() => {})
  }, [companyId])

  const mesParaAPI = mesSelecionado === '__all__' ? '' : mesSelecionado

  function toggleTabela(id: string) {
    setTabelasSelecionadas(prev =>
      prev.includes(id) ? prev.filter(x => x !== id) : [...prev, id]
    )
    setPreview(null)
    setResultado(null)
  }

  function handlePreview() {
    if (tabelasSelecionadas.length === 0) return
    setLoadingPreview(true)
    setErro(null)
    const params = mesParaAPI ? `?mes_ano=${encodeURIComponent(mesParaAPI)}` : ''
    fetch(`/api/admin/limpeza-base${params}`)
      .then(async r => {
        if (!r.ok) throw new Error('Erro ao consultar contagens')
        return r.json()
      })
      .then(d => setPreview(d.contagens ?? {}))
      .catch((e: Error) => setErro(e.message))
      .finally(() => setLoadingPreview(false))
  }

  function handleExecutar() {
    setExecuting(true)
    setErro(null)
    fetch('/api/admin/limpeza-base', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tabelas: tabelasSelecionadas, mes_ano: mesParaAPI }),
    })
      .then(async r => {
        const d = await r.json()
        if (!r.ok) throw new Error(d.error ?? 'Erro ao executar limpeza')
        setResultado(d.totais ?? {})
        setPreview(null)
        setConfirmando(false)
      })
      .catch((e: Error) => setErro(e.message))
      .finally(() => setExecuting(false))
  }

  const totalPreview = tabelasSelecionadas.reduce(
    (s, t) => s + ((preview?.[t] ?? 0) as number), 0
  )

  return (
    <div className="p-6 space-y-6 max-w-2xl">
      <div>
        <h1 className="text-2xl font-bold">Limpeza de Base de Dados</h1>
        <p className="text-muted-foreground text-sm mt-1">
          Remova documentos fiscais por tabela e período. A operação é restrita à empresa selecionada e não pode ser desfeita.
        </p>
      </div>

      {/* Tabelas */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Tabelas</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {TABELAS.map(t => (
            <label key={t.id} className="flex items-center gap-3 cursor-pointer select-none">
              <input
                type="checkbox"
                checked={tabelasSelecionadas.includes(t.id)}
                onChange={() => toggleTabela(t.id)}
                className="h-4 w-4 rounded border border-input"
              />
              <span className="text-sm font-medium">{t.label}</span>
            </label>
          ))}
          <button
            type="button"
            className="text-xs text-primary underline mt-1"
            onClick={() => { setTabelasSelecionadas(TABELAS.map(t => t.id)); setPreview(null) }}
          >
            Selecionar todas
          </button>
        </CardContent>
      </Card>

      {/* Período */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Período</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          <Select
            value={mesSelecionado}
            onValueChange={v => { setMesSelecionado(v); setPreview(null); setResultado(null) }}
          >
            <SelectTrigger className="w-52">
              <SelectValue placeholder="Todos os períodos" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">Todos os períodos</SelectItem>
              {periodos.map(m => (
                <SelectItem key={m} value={m}>{m}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            Sem seleção: remove todos os períodos das tabelas escolhidas.
          </p>
        </CardContent>
      </Card>

      {/* Pré-visualizar */}
      <Button
        variant="outline"
        onClick={handlePreview}
        disabled={tabelasSelecionadas.length === 0 || loadingPreview}
      >
        {loadingPreview ? 'Consultando…' : 'Pré-visualizar'}
      </Button>

      {/* Preview */}
      {preview !== null && (
        <Card className="border-amber-300 bg-amber-50">
          <CardHeader className="pb-3">
            <CardTitle className="text-base text-amber-800">Registros que serão removidos</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {TABELAS.filter(t => tabelasSelecionadas.includes(t.id)).map(t => (
              <div key={t.id} className="flex justify-between text-sm">
                <span className="text-amber-900">{t.label}</span>
                <span className="font-bold text-amber-900">
                  {((preview[t.id] ?? 0) as number).toLocaleString('pt-BR')}
                </span>
              </div>
            ))}
            <div className="flex justify-between text-sm font-bold border-t border-amber-300 pt-2">
              <span className="text-amber-900">Total</span>
              <span className="text-amber-900">{totalPreview.toLocaleString('pt-BR')}</span>
            </div>

            <div className="pt-3">
              {!confirmando ? (
                <Button
                  variant="destructive"
                  onClick={() => setConfirmando(true)}
                  disabled={totalPreview === 0}
                >
                  Confirmar Exclusão
                </Button>
              ) : (
                <div className="space-y-2">
                  <p className="text-sm text-red-700 font-medium">
                    Esta ação removerá {totalPreview.toLocaleString('pt-BR')} registros e não pode ser desfeita. Confirma?
                  </p>
                  <div className="flex gap-2">
                    <Button variant="destructive" onClick={handleExecutar} disabled={executing}>
                      {executing ? 'Removendo…' : 'Sim, remover'}
                    </Button>
                    <Button variant="outline" onClick={() => setConfirmando(false)}>
                      Cancelar
                    </Button>
                  </div>
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Resultado */}
      {resultado !== null && (
        <Card className="border-green-300 bg-green-50">
          <CardHeader className="pb-3">
            <CardTitle className="text-base text-green-800">Limpeza concluída</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {TABELAS.filter(t => resultado[t.id] !== undefined).map(t => (
              <div key={t.id} className="flex justify-between text-sm">
                <span className="text-green-900">{t.label}</span>
                <span className="font-bold text-green-900">
                  {(resultado[t.id] as number).toLocaleString('pt-BR')} registros removidos
                </span>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      {erro && <p className="text-sm text-red-600">{erro}</p>}
    </div>
  )
}
