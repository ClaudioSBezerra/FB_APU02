import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useAuth } from '@/contexts/AuthContext'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'

interface UserActivityItem {
  user_id: string
  user_name: string
  email: string
  company_name: string
  modules: string[]
  visit_count: number
  total_duration_seconds: number
  last_visited_at: string
}

interface ModuleActivityItem {
  module: string
  user_count: number
  visit_count: number
  total_duration_seconds: number
  last_visited_at: string
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  if (h > 0) return `${h}h ${m}m`
  if (m > 0 && s > 0) return `${m}m ${s}s`
  return `${m}m`
}

export default function UserActivity() {
  const { token } = useAuth()
  const [view, setView] = useState<'users' | 'modules'>('users')

  const { data: userData, isLoading: loadingUsers } = useQuery({
    queryKey: ['user-activity'],
    queryFn: async () => {
      const res = await fetch('/api/admin/user-activity')
      if (!res.ok) throw new Error('Erro ao buscar atividade')
      return res.json() as Promise<{ items: UserActivityItem[] }>
    },
    enabled: !!token,
  })

  const { data: moduleData, isLoading: loadingModules } = useQuery({
    queryKey: ['module-activity'],
    queryFn: async () => {
      const res = await fetch('/api/admin/user-activity/modules')
      if (!res.ok) throw new Error('Erro ao buscar atividade')
      return res.json() as Promise<{ items: ModuleActivityItem[] }>
    },
    enabled: !!token,
  })

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Button
          variant={view === 'users' ? 'default' : 'outline'}
          size="sm"
          onClick={() => setView('users')}
        >
          Por usuário
        </Button>
        <Button
          variant={view === 'modules' ? 'default' : 'outline'}
          size="sm"
          onClick={() => setView('modules')}
        >
          Por módulo
        </Button>
      </div>

      {view === 'users' && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Usuário</TableHead>
              <TableHead>Empresa</TableHead>
              <TableHead>Módulos acessados</TableHead>
              <TableHead className="text-right w-20">Visitas</TableHead>
              <TableHead className="text-right w-28">Tempo total</TableHead>
              <TableHead className="w-40">Última visita</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loadingUsers ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground py-10">
                  Carregando...
                </TableCell>
              </TableRow>
            ) : !userData?.items?.length ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground py-10">
                  Nenhuma atividade registrada ainda.
                </TableCell>
              </TableRow>
            ) : userData.items.map(item => (
              <TableRow key={item.user_id}>
                <TableCell>
                  <div className="font-medium text-sm">{item.user_name}</div>
                  <div className="text-xs text-muted-foreground">{item.email}</div>
                </TableCell>
                <TableCell className="text-sm">{item.company_name || '—'}</TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    {item.modules.filter(Boolean).map(m => (
                      <Badge key={m} variant="secondary" className="text-xs font-normal">
                        {m}
                      </Badge>
                    ))}
                  </div>
                </TableCell>
                <TableCell className="text-right font-mono text-sm">{item.visit_count}</TableCell>
                <TableCell className="text-right font-mono text-sm">
                  {formatDuration(item.total_duration_seconds)}
                </TableCell>
                <TableCell className="text-sm text-muted-foreground">{item.last_visited_at}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {view === 'modules' && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Módulo</TableHead>
              <TableHead className="text-right w-24">Usuários</TableHead>
              <TableHead className="text-right w-20">Visitas</TableHead>
              <TableHead className="text-right w-28">Tempo total</TableHead>
              <TableHead className="w-40">Último acesso</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loadingModules ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground py-10">
                  Carregando...
                </TableCell>
              </TableRow>
            ) : !moduleData?.items?.length ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground py-10">
                  Nenhuma atividade registrada ainda.
                </TableCell>
              </TableRow>
            ) : moduleData.items.map(item => (
              <TableRow key={item.module}>
                <TableCell className="font-medium text-sm">{item.module}</TableCell>
                <TableCell className="text-right text-sm">{item.user_count}</TableCell>
                <TableCell className="text-right font-mono text-sm">{item.visit_count}</TableCell>
                <TableCell className="text-right font-mono text-sm">
                  {formatDuration(item.total_duration_seconds)}
                </TableCell>
                <TableCell className="text-sm text-muted-foreground">{item.last_visited_at}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}
