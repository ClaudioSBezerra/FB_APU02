import { useState, useEffect, useRef } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import {
  ShieldCheck,
  Save,
  Trash2,
  Pencil,
  CheckCircle2,
  XCircle,
  AlertTriangle,
  Upload,
  FileKey,
} from 'lucide-react'

// NFSeCertificado reflete os metadados devolvidos por GET /api/nfse/certificado — nunca inclui
// o certificado nem a senha (backend/handlers/nfse_credentials.go nunca os expõe).
interface NFSeCertificado {
  id?: string
  company_id?: string
  tem_certificado: boolean
  nome_arquivo?: string
  validade?: string | null
  status?: string
  created_at?: string
  updated_at?: string
}

// readFileAsBase64 lê o arquivo .pfx selecionado e devolve só o conteúdo em base64 (sem o
// prefixo "data:...;base64," que o FileReader inclui em readAsDataURL).
function readFileAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const result = reader.result as string
      const base64 = result.includes(',') ? result.split(',')[1] : result
      resolve(base64)
    }
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}

// diasParaVencer calcula a diferença em dias de calendário entre hoje e a validade
// (string AAAA-MM-DD). Pode retornar um número negativo quando já venceu — quem exibe o
// resultado NUNCA deve mostrar esse número negativo diretamente (ver render abaixo), só usá-lo
// para decidir que o certificado já está vencido.
function diasParaVencer(validade: string): number {
  const hoje = new Date()
  hoje.setHours(0, 0, 0, 0)
  const venc = new Date(`${validade}T00:00:00`)
  return Math.round((venc.getTime() - hoje.getTime()) / 86400000)
}

async function extractError(res: Response, fallback: string): Promise<string> {
  try {
    const text = await res.text()
    try {
      const parsed = JSON.parse(text)
      return parsed?.error || text || fallback
    } catch {
      return text || fallback
    }
  } catch {
    return fallback
  }
}

export default function NFSeCertificado() {
  const [certificado, setCertificado] = useState<NFSeCertificado | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [editing, setEditing] = useState(false)
  // fetchError é distinto de "certificado === null" — um GET que falhou (rede, 401, 500...)
  // nunca pode ser renderizado como "empresa sem certificado cadastrado".
  const [fetchError, setFetchError] = useState<string | null>(null)
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [formData, setFormData] = useState({
    certificado_pfx: '',
    nome_arquivo_selecionado: '',
    senha: '',
    validade: '',
  })
  const fileInputRef = useRef<HTMLInputElement>(null)

  const resetFormFromCertificado = (cert: NFSeCertificado | null) => {
    setFormData({
      certificado_pfx: '',
      nome_arquivo_selecionado: '',
      // senha nunca vem do GET (a API nunca devolve o valor salvo) — fica sempre em branco até
      // o usuário digitar uma nova.
      senha: '',
      validade: cert?.validade || '',
    })
  }

  const fetchCertificado = async () => {
    setLoading(true)
    setFetchError(null)
    try {
      const res = await fetch('/api/nfse/certificado')
      if (!res.ok) {
        setFetchError(await extractError(res, 'Erro ao carregar certificado NFS-e'))
        setCertificado(null)
        return
      }
      const data = await res.json()
      const cert: NFSeCertificado = data.certificado || { tem_certificado: false }
      setCertificado(cert)
      resetFormFromCertificado(cert)
      setEditing(!cert.tem_certificado)
    } catch {
      setFetchError('Erro de conexão ao carregar certificado NFS-e')
      setCertificado(null)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchCertificado()
  }, [])

  const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    // Arquivo de 0 bytes vira uma string base64 vazia, que o backend trata como "campo não
    // enviado" e preserva silenciosamente o certificado anterior — rejeita aqui para avisar o
    // usuário em vez de deixar isso passar batido.
    if (file.size === 0) {
      setMessage({ type: 'error', text: 'O arquivo selecionado está vazio (0 bytes). Selecione o arquivo .pfx correto.' })
      if (fileInputRef.current) fileInputRef.current.value = ''
      return
    }
    try {
      const base64 = await readFileAsBase64(file)
      setFormData((fd) => ({ ...fd, certificado_pfx: base64, nome_arquivo_selecionado: file.name }))
    } catch {
      setMessage({ type: 'error', text: 'Erro ao ler o arquivo selecionado' })
    }
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setMessage(null)
    setSaving(true)
    try {
      const res = await fetch('/api/nfse/certificado', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          certificado_pfx: formData.certificado_pfx,
          certificado_senha: formData.senha,
          nome_arquivo: formData.nome_arquivo_selecionado,
          validade: formData.validade,
        }),
      })
      if (res.ok) {
        setMessage({ type: 'success', text: 'Certificado NFS-e salvo com sucesso!' })
        await fetchCertificado()
      } else {
        setMessage({ type: 'error', text: await extractError(res, 'Erro ao salvar certificado NFS-e') })
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' })
    } finally {
      setSaving(false)
    }
  }

  // handleCancel limpa senha/validade (e o arquivo selecionado) do estado — não deixa uma senha
  // digitada e não enviada persistindo em memória (Spec Change Log, achado "patch").
  const handleCancel = () => {
    setFormData({ certificado_pfx: '', nome_arquivo_selecionado: '', senha: '', validade: '' })
    if (fileInputRef.current) fileInputRef.current.value = ''
    setEditing(false)
  }

  const handleDelete = async () => {
    if (!confirm('Excluir o certificado NFS-e desta empresa?')) return
    setDeleting(true)
    setMessage(null)
    try {
      const res = await fetch('/api/nfse/certificado', { method: 'DELETE' })
      if (res.ok) {
        setMessage({ type: 'success', text: 'Certificado excluído com sucesso!' })
        await fetchCertificado()
      } else {
        setMessage({ type: 'error', text: await extractError(res, 'Erro ao excluir certificado') })
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' })
    } finally {
      setDeleting(false)
    }
  }

  const handleEdit = () => {
    resetFormFromCertificado(certificado)
    setEditing(true)
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary" />
      </div>
    )
  }

  const dias = certificado?.validade ? diasParaVencer(certificado.validade) : null
  // certificado.status é a fonte da verdade (calculado no backend via SQL com CURRENT_DATE do
  // servidor) — nunca recalcular "vencido" a partir de dias/new Date() no cliente, já que o fuso
  // horário do navegador pode divergir do servidor exatamente no dia do vencimento. dias só serve
  // para o texto complementar ("vence em N dias"), nunca para decidir a cor do badge.
  const vencido = certificado?.status === 'vencido'

  return (
    <div className="max-w-2xl mx-auto px-4 py-8">
      <div className="mb-6">
        <h2 className="text-2xl font-bold leading-7 flex items-center gap-2">
          <FileKey className="h-6 w-6" />
          Certificado Digital A1 — NFS-e
        </h2>
        <p className="mt-2 text-sm text-gray-600">
          Certificado exigido pelo Portal Nacional ADN (mTLS) para a captura automática de NFS-e.
          O arquivo e a senha são armazenados criptografados e nunca são exibidos novamente.
        </p>
      </div>

      {fetchError ? (
        <div className="mb-6 flex items-start gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-red-900 text-sm">
          <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5 text-red-600" />
          <div>
            <p className="font-semibold mb-1">Não foi possível carregar o certificado</p>
            <p>{fetchError}</p>
            <Button variant="outline" size="sm" className="mt-2" onClick={fetchCertificado}>
              Tentar novamente
            </Button>
          </div>
        </div>
      ) : (
        <>
          {message && (
            <div className={`mb-4 rounded-md p-4 ${message.type === 'success' ? 'bg-green-50 text-green-800' : 'bg-red-50 text-red-800'}`}>
              <p className="text-sm font-medium">{message.text}</p>
            </div>
          )}

          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle className="text-lg">Certificado A1</CardTitle>
                  <CardDescription>Upload do arquivo .pfx e senha de proteção</CardDescription>
                </div>
                <div className="flex items-center gap-2">
                  {certificado?.tem_certificado ? (
                    <span className="inline-flex items-center gap-1 rounded-full bg-green-50 px-3 py-1 text-xs font-medium text-green-700 ring-1 ring-inset ring-green-600/20">
                      <CheckCircle2 className="h-3 w-3" /> Configurado
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 rounded-full bg-gray-50 px-3 py-1 text-xs font-medium text-gray-600 ring-1 ring-inset ring-gray-500/10">
                      <XCircle className="h-3 w-3" /> Não configurado
                    </span>
                  )}
                </div>
              </div>
            </CardHeader>
            <CardContent>
              {editing ? (
                <form onSubmit={handleSave} className="space-y-4">
                  <div>
                    <Label htmlFor="certificado_pfx">Arquivo do Certificado (.pfx) {certificado?.tem_certificado ? '' : '*'}</Label>
                    <div className="mt-1 flex items-center gap-2">
                      <input
                        ref={fileInputRef}
                        id="certificado_pfx"
                        type="file"
                        accept=".pfx,.p12"
                        className="hidden"
                        onChange={handleFileChange}
                      />
                      <Button type="button" variant="outline" onClick={() => fileInputRef.current?.click()}>
                        <Upload className="mr-2 h-4 w-4" />
                        Selecionar arquivo
                      </Button>
                      <span className="text-sm text-muted-foreground truncate">
                        {formData.nome_arquivo_selecionado || 'Nenhum arquivo selecionado'}
                      </span>
                    </div>
                    {certificado?.tem_certificado ? (
                      <p className="text-xs text-muted-foreground mt-1">
                        Já existe um certificado salvo. Deixe sem selecionar arquivo para mantê-lo.
                      </p>
                    ) : (
                      !formData.certificado_pfx && (
                        <p className="text-xs text-red-600 mt-1">
                          Selecione o arquivo .pfx — obrigatório no primeiro cadastro.
                        </p>
                      )
                    )}
                  </div>
                  <div>
                    <Label htmlFor="senha">Senha do Certificado {certificado?.tem_certificado ? '' : '*'}</Label>
                    <Input
                      id="senha"
                      type="password"
                      placeholder={certificado?.tem_certificado ? 'Deixe em branco para manter a atual' : 'Informe a senha do .pfx'}
                      required={!certificado?.tem_certificado}
                      value={formData.senha}
                      onChange={(e) => setFormData({ ...formData, senha: e.target.value })}
                    />
                    {certificado?.tem_certificado && (
                      <p className="text-xs text-muted-foreground mt-1">
                        Por segurança, a senha salva nunca é exibida — deixe o campo em branco para mantê-la.
                      </p>
                    )}
                  </div>
                  <div>
                    <Label htmlFor="validade">Validade</Label>
                    <Input
                      id="validade"
                      type="date"
                      value={formData.validade}
                      onChange={(e) => setFormData({ ...formData, validade: e.target.value })}
                    />
                    <p className="text-xs text-muted-foreground mt-1">
                      Informe a data de validade impressa no certificado. Deixe em branco para manter a validade já cadastrada.
                    </p>
                  </div>
                  <div className="flex gap-2 pt-2">
                    <Button
                      type="submit"
                      disabled={saving || (!certificado?.tem_certificado && !formData.certificado_pfx)}
                    >
                      <Save className="mr-2 h-4 w-4" />
                      {saving ? 'Salvando...' : 'Salvar Certificado'}
                    </Button>
                    {certificado?.tem_certificado && (
                      <Button type="button" variant="outline" onClick={handleCancel}>
                        Cancelar
                      </Button>
                    )}
                  </div>
                </form>
              ) : (
                <div className="space-y-4">
                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                    <div>
                      <Label className="text-muted-foreground">Arquivo</Label>
                      <p className="text-sm font-medium mt-1">{certificado?.nome_arquivo || '—'}</p>
                    </div>
                    <div>
                      <Label className="text-muted-foreground">Senha</Label>
                      <p className="text-sm font-medium mt-1">{'•'.repeat(12)}</p>
                    </div>
                    <div>
                      <Label className="text-muted-foreground">Validade</Label>
                      <p className="text-sm font-medium mt-1">
                        {certificado?.validade ? new Date(`${certificado.validade}T00:00:00`).toLocaleDateString('pt-BR') : '—'}
                      </p>
                    </div>
                    <div>
                      <Label className="text-muted-foreground">Status</Label>
                      <p className="text-sm font-medium mt-1">
                        {vencido ? (
                          <Badge className="bg-red-100 text-red-700">
                            <AlertTriangle className="mr-1 h-3 w-3" /> Vencido
                          </Badge>
                        ) : dias !== null && dias <= 30 ? (
                          <Badge className="bg-amber-100 text-amber-700">
                            <AlertTriangle className="mr-1 h-3 w-3" /> Vence em {dias} dia{dias === 1 ? '' : 's'}
                          </Badge>
                        ) : (
                          <Badge className="bg-green-100 text-green-700">
                            <CheckCircle2 className="mr-1 h-3 w-3" /> Ativo
                          </Badge>
                        )}
                      </p>
                    </div>
                    <div>
                      <Label className="text-muted-foreground">Última atualização</Label>
                      <p className="text-sm font-medium mt-1">
                        {certificado?.updated_at ? new Date(certificado.updated_at).toLocaleString('pt-BR') : '—'}
                      </p>
                    </div>
                  </div>
                  <div className="flex flex-wrap gap-2 pt-2">
                    <Button variant="outline" onClick={handleEdit}>
                      <Pencil className="mr-2 h-4 w-4" /> Trocar Certificado
                    </Button>
                    <Button variant="destructive" onClick={handleDelete} disabled={deleting}>
                      <Trash2 className="mr-2 h-4 w-4" /> {deleting ? 'Excluindo...' : 'Excluir'}
                    </Button>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>

          <div className="mt-6 flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-amber-900 text-sm">
            <ShieldCheck className="h-4 w-4 shrink-0 mt-0.5 text-amber-600" />
            <p>
              Esta tela só cadastra o certificado — a captura automática de NFS-e via Portal
              Nacional ADN ainda não está implementada (próxima etapa).
            </p>
          </div>
        </>
      )}
    </div>
  )
}
