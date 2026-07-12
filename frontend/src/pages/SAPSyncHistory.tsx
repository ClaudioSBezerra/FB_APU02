import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Loader2, RefreshCw, Clock, ChevronLeft, ChevronRight } from 'lucide-react';

interface SAPSyncRun {
  id: string;
  bukrs: string;
  status: string;
  iniciado_em: string;
  concluido_em: string | null;
  duracao_segundos: number | null;
  chaves_enviadas: number;
  chaves_pago_total: number;
  chaves_pago_parcial: number;
  chaves_em_aberto: number;
  chaves_nao_localizado: number;
  erro_detalhe: string;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function fmtDateTime(iso: string): string {
  return new Date(iso).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' });
}

function fmtDuration(secs: number | null): string {
  if (secs === null) return '—';
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60), s = secs % 60;
  if (m < 60) return `${m}m ${s}s`;
  const h = Math.floor(m / 60), rem = m % 60;
  return `${h}h ${rem}m`;
}

const STATUS_MAP: Record<string, { label: string; className: string }> = {
  em_andamento: { label: 'Em andamento', className: 'bg-blue-100 text-blue-700 border-blue-200' },
  concluido: { label: 'Concluído', className: 'bg-green-100 text-green-700 border-green-200' },
  falha: { label: 'Falha', className: 'bg-red-100 text-red-700 border-red-200' },
  falha_credencial: { label: 'Falha de credencial', className: 'bg-orange-100 text-orange-700 border-orange-200' },
};

function StatusBadge({ status }: { status: string }) {
  const s = STATUS_MAP[status] ?? { label: status, className: 'bg-gray-100 text-gray-600' };
  return <Badge variant="outline" className={`text-[10px] px-1.5 py-0 ${s.className}`}>{s.label}</Badge>;
}

// ── Página principal ───────────────────────────────────────────────────────────

export default function SAPSyncHistory() {
  const { token, companyId } = useAuth();
  const authHeaders = { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId || '' };

  const [bukrsFilter, setBukrsFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [page, setPage] = useState(1);
  const pageSize = 50;

  const { data, isLoading, isFetching, isError, error, refetch } = useQuery<{ items: SAPSyncRun[]; total: number }>({
    queryKey: ['sap-sync-runs', companyId, bukrsFilter, statusFilter, page],
    queryFn: async () => {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
      if (bukrsFilter) params.set('bukrs', bukrsFilter);
      if (statusFilter) params.set('status', statusFilter);
      const res = await fetch(`/api/sap/sync-runs?${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    enabled: !!token && !!companyId,
    refetchInterval: 15_000,
  });

  const runs = data?.items ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Sincronizações SAP — Histórico</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Execuções de sincronização de pagamentos com o SAP, uma linha por código de empresa (BUKRS).
          </p>
        </div>
        <Button size="sm" variant="outline" onClick={() => refetch()} disabled={isFetching}>
          <RefreshCw className={`h-3.5 w-3.5 mr-1.5 ${isFetching ? 'animate-spin' : ''}`} />
          Atualizar
        </Button>
      </div>

      <div className="flex gap-3 items-end">
        <div className="w-40">
          <label className="text-xs text-muted-foreground">BUKRS</label>
          <Input
            value={bukrsFilter}
            onChange={(e) => { setBukrsFilter(e.target.value); setPage(1); }}
            placeholder="ex.: 1000"
          />
        </div>
        <div className="w-56">
          <label className="text-xs text-muted-foreground">Status</label>
          <select
            className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm"
            value={statusFilter}
            onChange={(e) => { setStatusFilter(e.target.value); setPage(1); }}
          >
            <option value="">Todos</option>
            <option value="em_andamento">Em andamento</option>
            <option value="concluido">Concluído</option>
            <option value="falha">Falha</option>
            <option value="falha_credencial">Falha de credencial</option>
          </select>
        </div>
      </div>

      <Card>
        <CardHeader className="py-2 px-4">
          <CardTitle className="text-[11px] text-muted-foreground font-normal">
            {isLoading ? 'Carregando...' : `${total} execuç${total !== 1 ? 'ões' : 'ão'} · Atualiza automaticamente a cada 15s`}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="flex items-center gap-2 text-sm text-muted-foreground py-8 justify-center">
              <Loader2 className="h-4 w-4 animate-spin" /> Carregando...
            </div>
          ) : isError ? (
            <p className="text-xs text-red-600 text-center py-8">
              Erro ao carregar o histórico{error instanceof Error ? `: ${error.message}` : ''}. Tente atualizar novamente.
            </p>
          ) : runs.length === 0 ? (
            <p className="text-xs text-muted-foreground text-center py-8">
              Nenhuma execução registrada ainda.
            </p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-[11px]">
                <thead>
                  <tr className="border-b bg-muted/30">
                    <th className="py-1.5 px-3 text-left font-medium text-muted-foreground">Data/Hora</th>
                    <th className="py-1.5 px-2 text-left font-medium text-muted-foreground">BUKRS</th>
                    <th className="py-1.5 px-2 text-left font-medium text-muted-foreground">Status</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Enviadas</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Pago Total</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Pago Parcial</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Em Aberto</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Não Localizado</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Duração</th>
                    <th className="py-1.5 px-2 text-left font-medium text-muted-foreground">Detalhe</th>
                  </tr>
                </thead>
                <tbody>
                  {runs.map(run => (
                    <tr key={run.id} className="border-b hover:bg-muted/20">
                      <td className="py-1.5 px-3 whitespace-nowrap">{fmtDateTime(run.iniciado_em)}</td>
                      <td className="py-1.5 px-2 font-medium">{run.bukrs}</td>
                      <td className="py-1.5 px-2"><StatusBadge status={run.status} /></td>
                      <td className="py-1.5 px-2 text-right text-muted-foreground">{run.chaves_enviadas}</td>
                      <td className="py-1.5 px-2 text-right text-green-600 font-medium">{run.chaves_pago_total || '—'}</td>
                      <td className="py-1.5 px-2 text-right text-yellow-600">{run.chaves_pago_parcial || '—'}</td>
                      <td className="py-1.5 px-2 text-right text-muted-foreground">{run.chaves_em_aberto || '—'}</td>
                      <td className="py-1.5 px-2 text-right text-muted-foreground">{run.chaves_nao_localizado || '—'}</td>
                      <td className="py-1.5 px-2 text-right text-muted-foreground whitespace-nowrap">
                        <span className="flex items-center gap-1 justify-end">
                          <Clock className="h-3 w-3" />
                          {fmtDuration(run.duracao_segundos)}
                        </span>
                      </td>
                      <td className="py-1.5 px-2 text-muted-foreground max-w-[300px] truncate" title={run.erro_detalhe || undefined}>
                        {run.erro_detalhe || '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-3">
          <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>
            <ChevronLeft className="h-3.5 w-3.5" />
          </Button>
          <span className="text-xs text-muted-foreground">Página {page} de {totalPages}</span>
          <Button size="sm" variant="outline" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>
            <ChevronRight className="h-3.5 w-3.5" />
          </Button>
        </div>
      )}
    </div>
  );
}
