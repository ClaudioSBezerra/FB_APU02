import { useState, useEffect } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Globe, Save, Trash2, Pencil, CheckCircle2, XCircle, Clock, Info, ExternalLink } from 'lucide-react'

interface CGIBSCredential {
  id: string
  cnpj_matriz: string
  client_id: string
  client_secret: string
  ambiente: string
  ativo: boolean
  agendamento_ativo: boolean
  horario_agendamento: string
  created_at: string
  updated_at: string
}

function formatCNPJ(value: string): string {
  const d = value.replace(/\D/g, '').slice(0, 14)
  if (d.length <= 2) return d
  if (d.length <= 5) return `${d.slice(0,2)}.${d.slice(2)}`
  if (d.length <= 8) return `${d.slice(0,2)}.${d.slice(2,5)}.${d.slice(5)}`
  if (d.length <= 12) return `${d.slice(0,2)}.${d.slice(2,5)}.${d.slice(5,8)}/${d.slice(8)}`
  return `${d.slice(0,2)}.${d.slice(2,5)}.${d.slice(5,8)}/${d.slice(8,12)}-${d.slice(12)}`
}

export default function CGIBSCredentials() {
  const [credential, setCredential] = useState<CGIBSCredential | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [editing, setEditing] = useState(false)
  const [savingSchedule, setSavingSchedule] = useState(false)
  const [scheduleData, setScheduleData] = useState({ agendamento_ativo: false, horario_agendamento: '06:00' })
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [formData, setFormData] = useState({
    cnpj_matriz: '',
    client_id: '',
    client_secret: '',
    ambiente: 'piloto',
  })

  const fetchCredential = async () => {
    try {
      const res = await fetch('/api/cgibs/credentials')
      if (res.ok) {
        const data = await res.json()
        if (data.credential) {
          setCredential(data.credential)
          setFormData({
            cnpj_matriz: formatCNPJ(data.credential.cnpj_matriz),
            client_id: data.credential.client_id,
            client_secret: '',
            ambiente: data.credential.ambiente || 'piloto',
          })
          setScheduleData({
            agendamento_ativo: data.credential.agendamento_ativo ?? false,
            horario_agendamento: data.credential.horario_agendamento || '06:00',
          })
          setEditing(false)
        } else {
          setCredential(null)
          setEditing(true)
        }
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro ao carregar credenciais' })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchCredential() }, [])

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setMessage(null)
    setSaving(true)
    try {
      const res = await fetch('/api/cgibs/credentials', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          cnpj_matriz: formData.cnpj_matriz.replace(/\D/g, ''),
          client_id: formData.client_id,
          client_secret: formData.client_secret,
          ambiente: formData.ambiente,
        }),
      })
      if (res.ok) {
        setMessage({ type: 'success', text: 'Credenciais CGIBS salvas com sucesso!' })
        fetchCredential()
      } else {
        const text = await res.text()
        setMessage({ type: 'error', text: text || 'Erro ao salvar credenciais' })
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' })
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!confirm('Excluir as credenciais CGIBS?')) return
    try {
      const res = await fetch('/api/cgibs/credentials', { method: 'DELETE' })
      if (res.ok) {
        setMessage({ type: 'success', text: 'Credenciais excluídas com sucesso!' })
        setCredential(null)
        setFormData({ cnpj_matriz: '', client_id: '', client_secret: '', ambiente: 'piloto' })
        setEditing(true)
      } else {
        setMessage({ type: 'error', text: 'Erro ao excluir credenciais' })
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' })
    }
  }

  const handleSaveSchedule = async () => {
    setSavingSchedule(true)
    setMessage(null)
    try {
      const res = await fetch('/api/cgibs/credentials/agendamento', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(scheduleData),
      })
      if (res.ok) {
        setMessage({ type: 'success', text: 'Agendamento salvo com sucesso!' })
      } else {
        setMessage({ type: 'error', text: 'Erro ao salvar agendamento' })
      }
    } catch {
      setMessage({ type: 'error', text: 'Erro de conexão' })
    } finally {
      setSavingSchedule(false)
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
    <div className="max-w-2xl mx-auto px-4 py-8">
      <div className="mb-6">
        <h2 className="text-2xl font-bold leading-7 flex items-center gap-2">
          <Globe className="h-6 w-6" />
          Credenciais API — CGIBS
        </h2>
        <p className="mt-2 text-sm text-gray-600">
          Configure as credenciais de acesso à API do Comitê Gestor do IBS (servicos.cgibs.gov.br)
        </p>
      </div>

      {/* ── Banner piloto ── */}
      <div className="mb-6 flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-amber-900 text-sm">
        <Info className="h-4 w-4 shrink-0 mt-0.5 text-amber-600" />
        <div>
          <p className="font-semibold mb-1">API em fase piloto</p>
          <p>
            A API do CGIBS está restrita a empresas participantes do piloto (jan/2026).
            Cadastre suas credenciais para ficar pronto quando o acesso for liberado.
          </p>
          <a href="https://www.servicos.cgibs.gov.br/" target="_blank" rel="noopener noreferrer"
            className="inline-flex items-center gap-1 mt-1.5 font-medium underline underline-offset-2 hover:text-amber-700">
            <ExternalLink className="h-3.5 w-3.5" />
            Acessar portal CGIBS
          </a>
        </div>
      </div>

      {message && (
        <div className={`mb-4 rounded-md p-4 ${message.type === 'success' ? 'bg-green-50 text-green-800' : 'bg-red-50 text-red-800'}`}>
          <p className="text-sm font-medium">{message.text}</p>
        </div>
      )}

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="text-lg">Credenciais IBS</CardTitle>
              <CardDescription>Obtenha suas credenciais no portal CGIBS</CardDescription>
            </div>
            <div className="flex items-center gap-2">
              {credential ? (
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
                <Label htmlFor="cnpj_matriz">CNPJ Matriz *</Label>
                <Input
                  id="cnpj_matriz"
                  placeholder="00.000.000/0000-00"
                  required
                  value={formData.cnpj_matriz}
                  onChange={(e) => setFormData({ ...formData, cnpj_matriz: formatCNPJ(e.target.value) })}
                  maxLength={18}
                />
              </div>
              <div>
                <Label htmlFor="client_id">Client ID *</Label>
                <Input
                  id="client_id"
                  placeholder="Informe o Client ID"
                  required
                  value={formData.client_id}
                  onChange={(e) => setFormData({ ...formData, client_id: e.target.value })}
                />
              </div>
              <div>
                <Label htmlFor="client_secret">Client Secret *</Label>
                <Input
                  id="client_secret"
                  type="password"
                  placeholder={credential ? 'Informe o novo Client Secret' : 'Informe o Client Secret'}
                  required
                  value={formData.client_secret}
                  onChange={(e) => setFormData({ ...formData, client_secret: e.target.value })}
                />
              </div>
              <div>
                <Label>Ambiente *</Label>
                <div className="mt-2 flex flex-col gap-2">
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input type="radio" name="ambiente" value="piloto"
                      checked={formData.ambiente === 'piloto'}
                      onChange={() => setFormData({ ...formData, ambiente: 'piloto' })}
                      className="h-4 w-4" />
                    <span className="text-sm font-medium">Piloto</span>
                    <span className="text-xs text-muted-foreground">(fase piloto restrita — jan–mar/2026)</span>
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input type="radio" name="ambiente" value="producao"
                      checked={formData.ambiente === 'producao'}
                      onChange={() => setFormData({ ...formData, ambiente: 'producao' })}
                      className="h-4 w-4" />
                    <span className="text-sm font-medium">Produção</span>
                    <span className="text-xs text-muted-foreground">(acesso irrestrito — previsão 2026)</span>
                  </label>
                </div>
              </div>
              <div className="flex gap-2 pt-2">
                <Button type="submit" disabled={saving}>
                  <Save className="mr-2 h-4 w-4" />
                  {saving ? 'Salvando...' : 'Salvar Credenciais'}
                </Button>
                {credential && (
                  <Button type="button" variant="outline" onClick={() => setEditing(false)}>
                    Cancelar
                  </Button>
                )}
              </div>
            </form>
          ) : (
            <div className="space-y-4">
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <Label className="text-muted-foreground">CNPJ Matriz</Label>
                  <p className="text-sm font-medium mt-1">{formatCNPJ(credential?.cnpj_matriz || '')}</p>
                </div>
                <div>
                  <Label className="text-muted-foreground">Client ID</Label>
                  <p className="text-sm font-medium mt-1">{credential?.client_id}</p>
                </div>
                <div>
                  <Label className="text-muted-foreground">Client Secret</Label>
                  <p className="text-sm font-medium mt-1">{credential?.client_secret}</p>
                </div>
                <div>
                  <Label className="text-muted-foreground">Ambiente</Label>
                  <p className="text-sm font-medium mt-1">
                    {credential?.ambiente === 'piloto' ? 'Piloto (fase restrita)' : 'Produção'}
                  </p>
                </div>
                <div>
                  <Label className="text-muted-foreground">Última atualização</Label>
                  <p className="text-sm font-medium mt-1">
                    {credential ? new Date(credential.updated_at).toLocaleString('pt-BR') : '-'}
                  </p>
                </div>
              </div>
              <div className="flex gap-2 pt-2">
                <Button variant="outline" onClick={() => setEditing(true)}>
                  <Pencil className="mr-2 h-4 w-4" /> Editar
                </Button>
                <Button variant="destructive" onClick={handleDelete}>
                  <Trash2 className="mr-2 h-4 w-4" /> Excluir
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {credential && (
        <Card className="mt-6">
          <CardHeader>
            <div className="flex items-center gap-2">
              <Clock className="h-5 w-5 text-muted-foreground" />
              <div>
                <CardTitle className="text-lg">Agendamento Automático</CardTitle>
                <CardDescription>
                  Solicitação diária automática quando a API estiver disponível (fuso Brasília)
                </CardDescription>
              </div>
            </div>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <Label>Ativar agendamento</Label>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Será ativado automaticamente quando a API CGIBS estiver disponível
                </p>
              </div>
              <Switch
                checked={scheduleData.agendamento_ativo}
                onCheckedChange={(v) => setScheduleData({ ...scheduleData, agendamento_ativo: v })}
              />
            </div>
            <div>
              <Label htmlFor="horario_agendamento">Horário (Brasília)</Label>
              <Input
                id="horario_agendamento"
                type="time"
                value={scheduleData.horario_agendamento}
                onChange={(e) => setScheduleData({ ...scheduleData, horario_agendamento: e.target.value })}
                className="mt-1 w-36"
                disabled={!scheduleData.agendamento_ativo}
              />
            </div>
            <Button onClick={handleSaveSchedule} disabled={savingSchedule} variant="outline">
              <Save className="mr-2 h-4 w-4" />
              {savingSchedule ? 'Salvando...' : 'Salvar Agendamento'}
            </Button>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
