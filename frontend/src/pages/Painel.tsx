import { useEffect, useState } from 'react'
import { Upload, FileText, ShieldAlert, TrendingUp, AlertCircle, CheckCircle2, Clock } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useAuth } from '@/contexts/AuthContext'

interface KpiCard {
  label: string
  value: string | number
  sub?: string
  color: string
  icon: React.ElementType
  href?: string
}

const kpis: KpiCard[] = [
  { label: 'XMLs Importados',    value: '—', sub: 'hoje',          color: 'text-blue-600',  icon: Upload,       href: '/apuracao/saida' },
  { label: 'Notas Processadas',  value: '—', sub: 'mês corrente',  color: 'text-green-600', icon: CheckCircle2, href: '/apuracao/saida/notas' },
  { label: 'Créditos em Risco',  value: '—', sub: 'pendentes',     color: 'text-red-600',   icon: ShieldAlert,  href: '/apuracao/creditos-perdidos' },
  { label: 'Saldo IBS/CBS',      value: '—', sub: 'estimado mês',  color: 'text-primary',   icon: TrendingUp,   href: '/rfb/gestao-creditos' },
]

interface StatusRow {
  label: string
  status: 'ok' | 'pending' | 'error' | 'empty'
  count?: number
  href: string
}

const baseStatusItems: StatusRow[] = [
  { label: 'NF-e Saídas',    status: 'empty', href: '/apuracao/saida' },
  { label: 'NF-e Entradas',  status: 'empty', href: '/apuracao/entrada' },
  { label: 'CT-e Entradas',  status: 'empty', href: '/apuracao/cte-entrada' },
]

const statusConfig = {
  ok:      { icon: CheckCircle2, cls: 'text-green-600', label: 'OK' },
  pending: { icon: Clock,        cls: 'text-yellow-600', label: 'Pendente' },
  error:   { icon: AlertCircle,  cls: 'text-red-600',    label: 'Erro' },
  empty:   { icon: FileText,     cls: 'text-gray-400',   label: 'Sem dados' },
}

export default function Painel() {
  const { token, companyId } = useAuth()

  // NFS-e ainda não tem contagem agregada num único endpoint — soma o `total` de
  // entradas + saídas com page_size=1 (só queremos o total, não os itens). 'empty'
  // enquanto a soma não chega; 'error' se qualquer uma das 2 chamadas falhar (nunca mostra
  // contagem incerta como 'ok', e nunca confunde falha de rede com "zero documentos").
  const [nfseStatus, setNfseStatus] = useState<StatusRow['status']>('empty')
  const [nfseCount, setNfseCount] = useState<number | undefined>(undefined)

  useEffect(() => {
    if (!token || !companyId) return
    let ignore = false
    const authHeaders = { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId }
    Promise.all([
      fetch('/api/nfse-entradas?page_size=1', { headers: authHeaders }).then(r => {
        if (!r.ok) throw new Error('Erro ao consultar NFS-e entradas')
        return r.json()
      }),
      fetch('/api/nfse-saidas?page_size=1', { headers: authHeaders }).then(r => {
        if (!r.ok) throw new Error('Erro ao consultar NFS-e saídas')
        return r.json()
      }),
    ])
      .then(([entradas, saidas]) => {
        if (ignore) return
        const total = (entradas?.total ?? 0) + (saidas?.total ?? 0)
        setNfseCount(total)
        setNfseStatus(total > 0 ? 'ok' : 'empty')
      })
      .catch(() => {
        if (ignore) return
        setNfseStatus('error')
        setNfseCount(undefined)
      })
    return () => { ignore = true }
  }, [token, companyId])

  const statusItems: StatusRow[] = [
    ...baseStatusItems,
    { label: 'NFS-e', status: nfseStatus, count: nfseCount, href: '/apuracao/nfse-entrada/notas' },
  ]

  return (
    <div className="space-y-6">
      {/* KPIs */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {kpis.map(kpi => {
          const Icon = kpi.icon
          const inner = (
            <Card className="hover:shadow-sm transition-shadow cursor-pointer">
              <CardContent className="flex items-start gap-3 p-4">
                <div className={cn('mt-0.5', kpi.color)}>
                  <Icon className="h-5 w-5" />
                </div>
                <div className="min-w-0">
                  <p className="text-[11px] text-muted-foreground leading-tight">{kpi.label}</p>
                  <p className={cn('text-2xl font-bold leading-tight mt-0.5', kpi.color)}>{kpi.value}</p>
                  {kpi.sub && <p className="text-[10px] text-muted-foreground">{kpi.sub}</p>}
                </div>
              </CardContent>
            </Card>
          )
          return kpi.href && kpi.href !== '#'
            ? <Link key={kpi.label} to={kpi.href}>{inner}</Link>
            : <div key={kpi.label}>{inner}</div>
        })}
      </div>

      {/* Status importações */}
      <div>
        <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
          Status Importações — hoje
        </h2>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          {statusItems.map(item => {
            const cfg  = statusConfig[item.status]
            const Icon = cfg.icon
            const inner = (
              <Card className={cn('hover:shadow-sm transition-shadow', item.href !== '#' && 'cursor-pointer')}>
                <CardContent className="flex items-center gap-3 p-4">
                  <Icon className={cn('h-5 w-5 shrink-0', cfg.cls)} />
                  <div className="min-w-0">
                    <p className="text-xs font-medium truncate">{item.label}</p>
                    <p className={cn('text-[10px]', cfg.cls)}>
                      {cfg.label}{item.count != null && item.count > 0 ? ` — ${item.count.toLocaleString('pt-BR')}` : ''}
                    </p>
                  </div>
                </CardContent>
              </Card>
            )
            return item.href && item.href !== '#'
              ? <Link key={item.label} to={item.href}>{inner}</Link>
              : <div key={item.label}>{inner}</div>
          })}
        </div>
      </div>

      {/* Placeholder — próximos dados reais */}
      <div className="rounded-lg border border-dashed border-muted-foreground/25 p-8 text-center">
        <TrendingUp className="h-8 w-8 mx-auto text-muted-foreground/40 mb-3" />
        <p className="text-sm text-muted-foreground font-medium">Painel de Apuração</p>
        <p className="text-xs text-muted-foreground mt-1">
          Gráficos e consolidações IBS/CBS por filial e período serão exibidos aqui.
        </p>
      </div>
    </div>
  )
}
