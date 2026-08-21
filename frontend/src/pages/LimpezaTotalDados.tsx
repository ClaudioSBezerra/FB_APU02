import { useState, useEffect } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { AlertTriangle, Loader2 } from 'lucide-react'

interface PreviewResponse {
  contagens: Record<string, number>
  total: number
  tabelas: string[]
  frase: string
}

export default function LimpezaTotalDados() {
  const [preview, setPreview] = useState<PreviewResponse | null>(null)
  const [loadingPreview, setLoadingPreview] = useState(true)
  const [confirmando, setConfirmando] = useState(false)
  const [textoDigitado, setTextoDigitado] = useState('')
  const [executando, setExecutando] = useState(false)
  const [resultado, setResultado] = useState<string | null>(null)
  const [erro, setErro] = useState<string | null>(null)

  function carregarPreview() {
    setLoadingPreview(true)
    setErro(null)
    fetch('/api/admin/limpeza-total')
      .then(async r => {
        if (!r.ok) throw new Error('Erro ao consultar contagens')
        return r.json()
      })
      .then((d: PreviewResponse) => setPreview(d))
      .catch((e: Error) => setErro(e.message))
      .finally(() => setLoadingPreview(false))
  }

  useEffect(() => {
    carregarPreview()
  }, [])

  function handleExecutar() {
    if (!preview || textoDigitado !== preview.frase) return
    setExecutando(true)
    setErro(null)
    fetch('/api/admin/limpeza-total', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ confirmacao: textoDigitado }),
    })
      .then(async r => {
        const d = await r.json()
        if (!r.ok) throw new Error(d.error ?? 'Erro ao executar limpeza')
        setResultado(d.message ?? 'Limpeza concluída.')
        setConfirmando(false)
        setTextoDigitado('')
        setPreview(null)
      })
      .catch((e: Error) => setErro(e.message))
      .finally(() => setExecutando(false))
  }

  const fraseCorreta = preview?.frase ?? ''

  return (
    <div className="p-6 space-y-6 max-w-2xl">
      <div>
        <h1 className="text-2xl font-bold text-red-700 flex items-center gap-2">
          <AlertTriangle className="h-6 w-6" /> Limpeza Total de Dados de Teste
        </h1>
        <p className="text-muted-foreground text-sm mt-1">
          Remove TODOS os registros de movimento (NF-e, CT-e, RFB, CGIBS, ERP Bridge, SPED, parceiros)
          de TODAS as empresas do sistema. Ação irreversível — use apenas antes da operação real
          começar (jan/2027), conforme decisão do gestor. Faça backup do banco antes de continuar.
        </p>
      </div>

      {loadingPreview && (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> Consultando contagens...
        </div>
      )}

      {erro && <p className="text-sm text-red-600">{erro}</p>}

      {preview && (
        <Card className="border-red-300 bg-red-50">
          <CardHeader className="pb-3">
            <CardTitle className="text-base text-red-800">
              {preview.total.toLocaleString('pt-BR')} registro(s) serão removidos, em {preview.tabelas.length} tabelas
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-1 max-h-64 overflow-y-auto">
            {preview.tabelas
              .filter(t => (preview.contagens[t] ?? 0) > 0)
              .map(t => (
                <div key={t} className="flex justify-between text-sm">
                  <span className="text-red-900 font-mono">{t}</span>
                  <span className="font-bold text-red-900">
                    {(preview.contagens[t] ?? 0).toLocaleString('pt-BR')}
                  </span>
                </div>
              ))}
          </CardContent>
        </Card>
      )}

      {preview && !resultado && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Confirmação</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {!confirmando ? (
              <Button variant="destructive" onClick={() => setConfirmando(true)} disabled={preview.total === 0}>
                Quero limpar tudo
              </Button>
            ) : (
              <div className="space-y-3">
                <p className="text-sm text-red-700 font-medium">
                  Digite exatamente a frase abaixo para confirmar. Não pode ser desfeito.
                </p>
                <p className="text-sm font-mono bg-muted px-3 py-2 rounded select-all">{fraseCorreta}</p>
                <Input
                  value={textoDigitado}
                  onChange={e => setTextoDigitado(e.target.value)}
                  placeholder="Digite a frase de confirmação"
                  className="font-mono"
                />
                <div className="flex gap-2">
                  <Button
                    variant="destructive"
                    onClick={handleExecutar}
                    disabled={executando || textoDigitado !== fraseCorreta}
                  >
                    {executando ? 'Removendo...' : 'Confirmar e executar TRUNCATE'}
                  </Button>
                  <Button variant="outline" onClick={() => { setConfirmando(false); setTextoDigitado('') }}>
                    Cancelar
                  </Button>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {resultado && (
        <Card className="border-green-300 bg-green-50">
          <CardHeader className="pb-3">
            <CardTitle className="text-base text-green-800">Concluído</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-green-900">{resultado}</p>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
