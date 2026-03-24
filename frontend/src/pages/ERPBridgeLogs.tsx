import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  ChevronDown, ChevronRight, Loader2, CheckCircle2, XCircle, AlertTriangle,
  Clock, RefreshCw,
} from 'lucide-react';

interface BridgeRunItem {
  id: string;
  run_id: string;
  servidor: string;
  tipo: string;
  enviados: number;
  ignorados: number;
  erros: number;
  status: string;
  erro_msg: string | null;
}

interface BridgeRun {
  id: string;
  iniciado_em: string;
  finalizado_em: string | null;
  status: string;
  data_ini: string | null;
  data_fim: string | null;
  total_enviados: number;
  total_ignorados: number;
  total_erros: number;
  erro_msg: string | null;
  origem: string;
  items?: BridgeRunItem[];
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function fmtDateTime(iso: string | null): string {
  if (!iso) return '—';
  return new Date(iso).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' });
}

function fmtDuration(ini: string, fim: string | null): string {
  if (!fim) return '—';
  const secs = Math.round((new Date(fim).getTime() - new Date(ini).getTime()) / 1000);
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60), s = secs % 60;
  if (m < 60) return `${m}m ${s}s`;
  const h = Math.floor(m / 60), rem = m % 60;
  return `${h}h ${rem}m`;
}

const TIPO_LABELS: Record<string, string> = {
  nfe_saidas:   'NF-e Saídas',
  nfe_entradas: 'NF-e Entradas',
  cte_entradas: 'CT-e Entradas',
};

function StatusBadge({ status }: { status: string }) {
  const map: Record<string, { label: string; className: string }> = {
    running:  { label: 'Em andamento', className: 'bg-blue-100 text-blue-700 border-blue-200' },
    success:  { label: 'Sucesso',      className: 'bg-green-100 text-green-700 border-green-200' },
    partial:  { label: 'Parcial',      className: 'bg-yellow-100 text-yellow-700 border-yellow-200' },
    error:    { label: 'Erro',         className: 'bg-red-100 text-red-700 border-red-200' },
    ok:               { label: 'OK',          className: 'bg-green-50 text-green-600 border-green-200' },
    erro_conexao:     { label: 'Erro conexão', className: 'bg-red-100 text-red-600 border-red-200' },
    erro_query:       { label: 'Erro query',  className: 'bg-red-100 text-red-600 border-red-200' },
    erro_parcial:     { label: 'Parcial',     className: 'bg-yellow-100 text-yellow-600 border-yellow-200' },
  };
  const s = map[status] ?? { label: status, className: 'bg-gray-100 text-gray-600' };
  return (
    <Badge variant="outline" className={`text-[10px] px-1.5 py-0 ${s.className}`}>{s.label}</Badge>
  );
}

function StatusIcon({ status }: { status: string }) {
  if (status === 'running') return <Loader2 className="h-3.5 w-3.5 text-blue-500 animate-spin" />;
  if (status === 'success') return <CheckCircle2 className="h-3.5 w-3.5 text-green-500" />;
  if (status === 'error')   return <XCircle className="h-3.5 w-3.5 text-red-500" />;
  return <AlertTriangle className="h-3.5 w-3.5 text-yellow-500" />;
}

// ── Row expandível ─────────────────────────────────────────────────────────────

function RunRow({ run, authHeaders }: { run: BridgeRun; authHeaders: Record<string, string> }) {
  const [expanded, setExpanded] = useState(false);

  const { data: detail, isFetching } = useQuery<BridgeRun>({
    queryKey: ['erp-bridge-run-detail', run.id],
    queryFn: async () => {
      const res = await fetch(`/api/erp-bridge/runs/${run.id}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    enabled: expanded,
    staleTime: 30_000,
  });

  const items = detail?.items ?? [];

  // Agrupa items por servidor
  const byServer: Record<string, BridgeRunItem[]> = {};
  items.forEach(item => {
    if (!byServer[item.servidor]) byServer[item.servidor] = [];
    byServer[item.servidor].push(item);
  });

  return (
    <>
      <tr
        className="border-b hover:bg-muted/30 cursor-pointer select-none"
        onClick={() => setExpanded(v => !v)}
      >
        <td className="py-1.5 px-3">
          {expanded
            ? <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
            : <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
          }
        </td>
        <td className="py-1.5 px-2">
          <div className="flex items-center gap-1.5">
            <StatusIcon status={run.status} />
            <span className="text-[11px] whitespace-nowrap">{fmtDateTime(run.iniciado_em)}</span>
          </div>
        </td>
        <td className="py-1.5 px-2 text-[11px] capitalize text-muted-foreground">{run.origem}</td>
        <td className="py-1.5 px-2"><StatusBadge status={run.status} /></td>
        <td className="py-1.5 px-2 text-[11px] text-muted-foreground whitespace-nowrap">
          {run.data_ini ?? '—'}
        </td>
        <td className="py-1.5 px-2 text-[11px] text-right text-green-600 font-medium">
          {run.total_enviados.toLocaleString('pt-BR')}
        </td>
        <td className="py-1.5 px-2 text-[11px] text-right text-muted-foreground">
          {run.total_ignorados.toLocaleString('pt-BR')}
        </td>
        <td className="py-1.5 px-2 text-[11px] text-right text-red-500">
          {run.total_erros > 0 ? run.total_erros : '—'}
        </td>
        <td className="py-1.5 px-2 text-[11px] text-right text-muted-foreground whitespace-nowrap">
          <span className="flex items-center gap-1 justify-end">
            <Clock className="h-3 w-3" />
            {fmtDuration(run.iniciado_em, run.finalizado_em)}
          </span>
        </td>
      </tr>

      {/* Detalhe expandido */}
      {expanded && (
        <tr className="border-b bg-muted/20">
          <td colSpan={9} className="px-6 py-3">
            {isFetching && items.length === 0 ? (
              <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
                <Loader2 className="h-3 w-3 animate-spin" /> Carregando detalhes...
              </div>
            ) : items.length === 0 ? (
              <p className="text-xs text-muted-foreground py-1">Nenhum detalhe disponível.</p>
            ) : (
              <div className="space-y-3">
                {Object.entries(byServer).map(([servidor, serverItems]) => (
                  <div key={servidor}>
                    <p className="text-[11px] font-semibold text-foreground mb-1">{servidor}</p>
                    <table className="w-full text-[11px] border rounded overflow-hidden">
                      <thead>
                        <tr className="bg-muted/50">
                          <th className="py-1 px-2 text-left font-medium text-muted-foreground">Tipo</th>
                          <th className="py-1 px-2 text-right font-medium text-muted-foreground">Enviados</th>
                          <th className="py-1 px-2 text-right font-medium text-muted-foreground">Ignorados</th>
                          <th className="py-1 px-2 text-right font-medium text-muted-foreground">Erros</th>
                          <th className="py-1 px-2 text-left font-medium text-muted-foreground">Status</th>
                          <th className="py-1 px-2 text-left font-medium text-muted-foreground">Mensagem</th>
                        </tr>
                      </thead>
                      <tbody>
                        {serverItems.map(item => (
                          <tr key={item.id} className="border-t">
                            <td className="py-0.5 px-2">{TIPO_LABELS[item.tipo] ?? item.tipo}</td>
                            <td className="py-0.5 px-2 text-right text-green-600 font-medium">{item.enviados.toLocaleString('pt-BR')}</td>
                            <td className="py-0.5 px-2 text-right text-muted-foreground">{item.ignorados.toLocaleString('pt-BR')}</td>
                            <td className="py-0.5 px-2 text-right text-red-500">{item.erros > 0 ? item.erros : '—'}</td>
                            <td className="py-0.5 px-2"><StatusBadge status={item.status} /></td>
                            <td className="py-0.5 px-2 text-muted-foreground max-w-[300px] truncate">
                              {item.erro_msg ?? '—'}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ))}

                {run.erro_msg && (
                  <div className="flex items-start gap-1.5 text-[11px] text-red-600 bg-red-50 border border-red-200 rounded px-2 py-1.5">
                    <XCircle className="h-3.5 w-3.5 mt-0.5 shrink-0" />
                    <span>{run.erro_msg}</span>
                  </div>
                )}
              </div>
            )}
          </td>
        </tr>
      )}
    </>
  );
}

// ── Página principal ───────────────────────────────────────────────────────────

export default function ERPBridgeLogs() {
  const { token, companyId } = useAuth();
  const authHeaders = { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId || '' };

  const { data, isLoading, isFetching, refetch } = useQuery<{ items: BridgeRun[]; total: number }>({
    queryKey: ['erp-bridge-runs', companyId],
    queryFn: async () => {
      const res = await fetch('/api/erp-bridge/runs', { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    enabled: !!token && !!companyId,
    refetchInterval: 15_000,
  });

  const runs = data?.items ?? [];

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">ERP Bridge — Histórico</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Execuções do bridge Oracle → FBTax. Clique em uma linha para ver o detalhe por servidor/tipo.
          </p>
        </div>
        <Button size="sm" variant="outline" onClick={() => refetch()} disabled={isFetching} className="mt-1">
          <RefreshCw className={`h-3.5 w-3.5 mr-1.5 ${isFetching ? 'animate-spin' : ''}`} />
          Atualizar
        </Button>
      </div>

      <Card>
        <CardHeader className="py-2 px-4">
          <CardTitle className="text-[11px] text-muted-foreground font-normal">
            {isLoading ? 'Carregando...' : `${runs.length} execuç${runs.length !== 1 ? 'ões' : 'ão'} · Atualiza automaticamente a cada 15s`}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="flex items-center gap-2 text-sm text-muted-foreground py-8 justify-center">
              <Loader2 className="h-4 w-4 animate-spin" /> Carregando...
            </div>
          ) : runs.length === 0 ? (
            <p className="text-xs text-muted-foreground text-center py-8">
              Nenhuma execução registrada ainda.
            </p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-[11px]">
                <thead>
                  <tr className="border-b bg-muted/30">
                    <th className="py-1.5 px-3 w-6"></th>
                    <th className="py-1.5 px-2 text-left font-medium text-muted-foreground">Data/Hora</th>
                    <th className="py-1.5 px-2 text-left font-medium text-muted-foreground">Origem</th>
                    <th className="py-1.5 px-2 text-left font-medium text-muted-foreground">Status</th>
                    <th className="py-1.5 px-2 text-left font-medium text-muted-foreground">Período</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Enviados</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Ignorados</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Erros</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Duração</th>
                  </tr>
                </thead>
                <tbody>
                  {runs.map(run => (
                    <RunRow key={run.id} run={run} authHeaders={authHeaders} />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
