import { useState, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { Settings2, Clock, CalendarDays, CheckCircle2, XCircle, Loader2, AlertTriangle } from 'lucide-react';
import { toast } from 'sonner';

interface BridgeConfig {
  company_id: string;
  ativo: boolean;
  horario: string;
  dias_retroativos: number;
  ultimo_run_em: string | null;
  updated_at: string;
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
  origem: string;
}

function StatusBadge({ status }: { status: string }) {
  const map: Record<string, { label: string; className: string }> = {
    running:  { label: 'Em andamento', className: 'bg-blue-100 text-blue-700 border-blue-200' },
    success:  { label: 'Sucesso',      className: 'bg-green-100 text-green-700 border-green-200' },
    partial:  { label: 'Parcial',      className: 'bg-yellow-100 text-yellow-700 border-yellow-200' },
    error:    { label: 'Erro',         className: 'bg-red-100 text-red-700 border-red-200' },
  };
  const s = map[status] ?? { label: status, className: 'bg-gray-100 text-gray-600' };
  return (
    <Badge variant="outline" className={`text-[10px] px-1.5 py-0 ${s.className}`}>{s.label}</Badge>
  );
}

function fmtDateTime(iso: string | null): string {
  if (!iso) return '—';
  return new Date(iso).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' });
}

export default function ERPBridgeConfig() {
  const { token, companyId } = useAuth();
  const qc = useQueryClient();
  const authHeaders = { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId || '' };

  const { data: cfg, isLoading } = useQuery<BridgeConfig>({
    queryKey: ['erp-bridge-config', companyId],
    queryFn: async () => {
      const res = await fetch('/api/erp-bridge/config', { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    enabled: !!token && !!companyId,
  });

  const { data: runs } = useQuery<{ items: BridgeRun[] }>({
    queryKey: ['erp-bridge-runs', companyId],
    queryFn: async () => {
      const res = await fetch('/api/erp-bridge/runs', { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    enabled: !!token && !!companyId,
    refetchInterval: 15_000,
  });

  const [ativo, setAtivo] = useState(false);
  const [horario, setHorario] = useState('02:00');
  const [diasRetro, setDiasRetro] = useState(1);

  useEffect(() => {
    if (cfg) {
      setAtivo(cfg.ativo);
      setHorario(cfg.horario);
      setDiasRetro(cfg.dias_retroativos);
    }
  }, [cfg]);

  const saveMutation = useMutation({
    mutationFn: async () => {
      const res = await fetch('/api/erp-bridge/config', {
        method: 'PATCH',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ ativo, horario, dias_retroativos: diasRetro }),
      });
      if (!res.ok) throw new Error(await res.text());
    },
    onSuccess: () => {
      toast.success('Configuração salva.');
      qc.invalidateQueries({ queryKey: ['erp-bridge-config', companyId] });
    },
    onError: (e: Error) => toast.error(`Erro ao salvar: ${e.message}`),
  });

  const lastRun = runs?.items?.[0] ?? null;
  const runningRun = runs?.items?.find(r => r.status === 'running') ?? null;

  if (isLoading) {
    return <div className="flex items-center gap-2 text-sm text-muted-foreground py-8 justify-center"><Loader2 className="h-4 w-4 animate-spin" />Carregando...</div>;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">ERP Bridge — Agendamento</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Configure o horário de importação automática dos XMLs do Oracle ERP para o FBTax.
        </p>
      </div>

      {/* Status do último run */}
      {lastRun && (
        <Card className={`border ${runningRun ? 'border-blue-200 bg-blue-50/40' : ''}`}>
          <CardHeader className="py-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              {runningRun
                ? <><Loader2 className="h-4 w-4 animate-spin text-blue-500" /> Importação em andamento...</>
                : lastRun.status === 'success'
                  ? <><CheckCircle2 className="h-4 w-4 text-green-500" /> Última execução</>
                  : lastRun.status === 'error'
                    ? <><XCircle className="h-4 w-4 text-red-500" /> Última execução (com erros)</>
                    : <><AlertTriangle className="h-4 w-4 text-yellow-500" /> Última execução (parcial)</>
              }
            </CardTitle>
          </CardHeader>
          <CardContent className="px-4 pb-3">
            <div className="flex flex-wrap gap-4 text-xs">
              <div>
                <span className="text-muted-foreground">Status: </span>
                <StatusBadge status={lastRun.status} />
              </div>
              <div>
                <span className="text-muted-foreground">Início: </span>
                <span className="font-medium">{fmtDateTime(lastRun.iniciado_em)}</span>
              </div>
              {lastRun.finalizado_em && (
                <div>
                  <span className="text-muted-foreground">Fim: </span>
                  <span className="font-medium">{fmtDateTime(lastRun.finalizado_em)}</span>
                </div>
              )}
              {lastRun.data_ini && (
                <div>
                  <span className="text-muted-foreground">Período: </span>
                  <span className="font-medium">{lastRun.data_ini} → {lastRun.data_fim}</span>
                </div>
              )}
              <div className="flex gap-3">
                <span className="text-green-600 font-medium">↑ {lastRun.total_enviados} enviados</span>
                <span className="text-muted-foreground">/ {lastRun.total_ignorados} ignorados</span>
                {lastRun.total_erros > 0 && <span className="text-red-500 font-medium">{lastRun.total_erros} erros</span>}
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Configuração */}
      <Card>
        <CardHeader className="py-3 px-4">
          <CardTitle className="text-sm flex items-center gap-2">
            <Settings2 className="h-4 w-4" /> Configuração do Agendamento
          </CardTitle>
        </CardHeader>
        <CardContent className="px-4 pb-4 space-y-5">

          {/* Ativo */}
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium">Agendamento ativo</p>
              <p className="text-[11px] text-muted-foreground mt-0.5">
                O bridge verificará o horário configurado e executará automaticamente.
              </p>
            </div>
            <Switch checked={ativo} onCheckedChange={setAtivo} />
          </div>

          {/* Horário */}
          <div className="flex items-center gap-4">
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground flex items-center gap-1">
                <Clock className="h-3 w-3" /> Horário de execução (Brasília)
              </label>
              <Input
                type="time"
                value={horario}
                onChange={e => setHorario(e.target.value)}
                className="h-8 w-32 text-sm"
                disabled={!ativo}
              />
            </div>

            {/* Dias retroativos */}
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground flex items-center gap-1">
                <CalendarDays className="h-3 w-3" /> Dias retroativos
              </label>
              <Input
                type="number"
                min={1}
                max={90}
                value={diasRetro}
                onChange={e => setDiasRetro(Math.max(1, Math.min(90, parseInt(e.target.value) || 1)))}
                className="h-8 w-24 text-sm"
                disabled={!ativo}
              />
            </div>
          </div>

          {ativo && (
            <p className="text-[11px] text-muted-foreground bg-muted/40 rounded px-3 py-2">
              O bridge importará os últimos <strong>{diasRetro}</strong> dia{diasRetro !== 1 ? 's' : ''} todo{diasRetro !== 1 ? 's' : ''} os dias às <strong>{horario}</strong> (horário de Brasília).
              O processo de daemon deve estar em execução no servidor ({' '}
              <code className="font-mono text-[10px]">venv/bin/python bridge.py --daemon</code>).
            </p>
          )}

          <div className="flex justify-end pt-1">
            <Button
              size="sm"
              onClick={() => saveMutation.mutate()}
              disabled={saveMutation.isPending}
            >
              {saveMutation.isPending && <Loader2 className="h-3 w-3 mr-1.5 animate-spin" />}
              Salvar configuração
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Resumo dos últimos runs */}
      {(runs?.items?.length ?? 0) > 0 && (
        <Card>
          <CardHeader className="py-3 px-4">
            <CardTitle className="text-sm text-muted-foreground font-normal">
              Últimas 5 execuções — <a href="/importacoes/erp-bridge/logs" className="text-primary underline-offset-2 hover:underline">ver histórico completo</a>
            </CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            <table className="w-full text-[11px]">
              <thead>
                <tr className="border-b bg-muted/30">
                  <th className="py-1.5 px-3 text-left font-medium text-muted-foreground">Data/Hora</th>
                  <th className="py-1.5 px-3 text-left font-medium text-muted-foreground">Origem</th>
                  <th className="py-1.5 px-3 text-left font-medium text-muted-foreground">Status</th>
                  <th className="py-1.5 px-3 text-right font-medium text-muted-foreground">Enviados</th>
                  <th className="py-1.5 px-3 text-right font-medium text-muted-foreground">Ignorados</th>
                  <th className="py-1.5 px-3 text-right font-medium text-muted-foreground">Erros</th>
                </tr>
              </thead>
              <tbody>
                {runs!.items.slice(0, 5).map(run => (
                  <tr key={run.id} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="py-1.5 px-3 whitespace-nowrap">{fmtDateTime(run.iniciado_em)}</td>
                    <td className="py-1.5 px-3 capitalize">{run.origem}</td>
                    <td className="py-1.5 px-3"><StatusBadge status={run.status} /></td>
                    <td className="py-1.5 px-3 text-right text-green-600 font-medium">{run.total_enviados.toLocaleString('pt-BR')}</td>
                    <td className="py-1.5 px-3 text-right text-muted-foreground">{run.total_ignorados.toLocaleString('pt-BR')}</td>
                    <td className="py-1.5 px-3 text-right text-red-500">{run.total_erros > 0 ? run.total_erros : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
