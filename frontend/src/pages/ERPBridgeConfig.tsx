import { useState, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { Settings2, Clock, CalendarDays, CheckCircle2, XCircle, Loader2, AlertTriangle, RefreshCw, Zap, Ban, Trash2 } from 'lucide-react';
import { toast } from 'sonner';

interface BridgeConfig {
  company_id: string;
  ativo: boolean;
  horario: string;
  dias_retroativos: number;
  ultimo_run_em: string | null;
  updated_at: string;
  erp_type: string; // 'sap_s4hana' | 'oracle_xml'
  fbtax_email: string;
  fbtax_password_set: boolean;
  oracle_usuario: string;
  oracle_senha_set: boolean;
  api_key: string;
  daemon_last_seen: string | null;
  daemon_online: boolean;
}

interface BridgeRunItem {
  id: string;
  servidor: string;
  tipo: string;
  enviados: number;
  ignorados: number;
  erros: number;
  status: string;
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
  items?: BridgeRunItem[];
}

const TIPO_LABELS: Record<string, string> = {
  nfe_saidas:   'NF-e Saídas',
  nfe_entradas: 'NF-e Entradas',
  cte_entradas: 'CT-e Entradas',
};

function StatusBadge({ status }: { status: string }) {
  const map: Record<string, { label: string; className: string }> = {
    pending:   { label: 'Aguardando',   className: 'bg-amber-100 text-amber-700 border-amber-200' },
    running:   { label: 'Em andamento', className: 'bg-blue-100 text-blue-700 border-blue-200' },
    success:   { label: 'Sucesso',      className: 'bg-green-100 text-green-700 border-green-200' },
    partial:   { label: 'Parcial',      className: 'bg-yellow-100 text-yellow-700 border-yellow-200' },
    error:     { label: 'Erro',         className: 'bg-red-100 text-red-700 border-red-200' },
    cancelled: { label: 'Cancelado',    className: 'bg-gray-100 text-gray-500 border-gray-200' },
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

function ServidorBadge({ erp_type }: { erp_type: string }) {
  if (erp_type === 'sap_s4hana') {
    return (
      <span className="inline-flex items-center gap-1 text-[11px] font-semibold text-blue-700 bg-blue-50 border border-blue-200 rounded px-1.5 py-0.5">
        <span className="font-mono">FCCORP</span>
        <span className="font-normal text-blue-500">SAP S/4HANA</span>
      </span>
    );
  }
  return null;
}

// ── Helpers de data ───────────────────────────────────────────────────────────
function firstDayOfPrevMonth(): string {
  const d = new Date();
  d.setDate(1);
  d.setMonth(d.getMonth() - 1);
  return d.toISOString().slice(0, 10);
}
function today(): string {
  return new Date().toISOString().slice(0, 10);
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

  const { data: runs, dataUpdatedAt } = useQuery<{ items: BridgeRun[] }>({
    queryKey: ['erp-bridge-runs', companyId],
    queryFn: async () => {
      const res = await fetch('/api/erp-bridge/runs', { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    enabled: !!token && !!companyId,
    refetchInterval: 30_000,
  });

  const runningRun = runs?.items?.find(r => r.status === 'running') ?? null;
  const pendingRun = runs?.items?.find(r => r.status === 'pending')  ?? null;
  const activeRun  = runningRun ?? pendingRun;

  // Detalhe do run ativo — atualiza a cada 60s para mostrar progresso por filial
  const { data: runDetail, dataUpdatedAt: detailUpdatedAt, refetch: refetchDetail } =
    useQuery<BridgeRun>({
      queryKey: ['erp-bridge-run-detail', runningRun?.id],
      queryFn: async () => {
        const res = await fetch(`/api/erp-bridge/runs/${runningRun!.id}`, { headers: authHeaders });
        if (!res.ok) throw new Error(res.statusText);
        return res.json();
      },
      enabled: !!runningRun,
      refetchInterval: 60_000,
    });

  // ── Estado: agendamento ───────────────────────────────────────────────────
  const [ativo, setAtivo] = useState(false);
  const [horario, setHorario] = useState('02:00');
  const [diasRetro, setDiasRetro] = useState(1);


  // ── Estado: trigger manual ────────────────────────────────────────────────
  const [triggerIni, setTriggerIni]               = useState(firstDayOfPrevMonth);
  const [triggerFim, setTriggerFim]               = useState(today);
  const [triggerQueued, setTriggerQueued]         = useState(false);
  const [onlyParceiros, setOnlyParceiros]         = useState(false);

  useEffect(() => {
    if (cfg) {
      setAtivo(cfg.ativo);
      setHorario(cfg.horario);
      setDiasRetro(cfg.dias_retroativos);
    }
  }, [cfg]);

const abortMutation = useMutation({
    mutationFn: async (runId: string) => {
      const res = await fetch(`/api/erp-bridge/runs/${runId}`, {
        method: 'PATCH',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: 'cancelled' }),
      });
      if (!res.ok) throw new Error(await res.text());
    },
    onSuccess: () => {
      setTriggerQueued(false);
      toast.success('Importação cancelada.');
      qc.invalidateQueries({ queryKey: ['erp-bridge-runs', companyId] });
    },
    onError: (e: Error) => toast.error(`Erro ao cancelar: ${e.message}`),
  });

  const triggerMutation = useMutation({
    mutationFn: async () => {
      const res = await fetch('/api/erp-bridge/trigger', {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({
          data_ini: triggerIni,
          data_fim: triggerFim,
          filiais_filter: [],
          only_parceiros: onlyParceiros,
        }),
      });
      if (res.status === 409) throw new Error('Já existe uma importação em andamento. Aguarde a conclusão.');
      if (!res.ok) throw new Error(await res.text());
    },
    onSuccess: () => {
      setTriggerQueued(true);
      toast.success('Importação agendada! O daemon Bridge executará em até 1 minuto.');
      qc.invalidateQueries({ queryKey: ['erp-bridge-runs', companyId] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

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

  const resetTrackerMutation = useMutation({
    mutationFn: async () => {
      const res = await fetch('/api/erp-bridge/config', {
        method: 'PATCH',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ reset_tracker: true }),
      });
      if (!res.ok) throw new Error(await res.text());
    },
    onSuccess: () => toast.success('Tracker sinalizado para limpeza. O daemon Bridge limpará o tracker.db na próxima varredura.'),
    onError: (e: Error) => toast.error(`Erro: ${e.message}`),
  });

  const lastRun = runs?.items?.[0] ?? null;

  if (isLoading) {
    return <div className="flex items-center gap-2 text-sm text-muted-foreground py-8 justify-center"><Loader2 className="h-4 w-4 animate-spin" />Carregando...</div>;
  }

  const daemonOnline = cfg?.daemon_online ?? false;
  const daemonLastSeen = cfg?.daemon_last_seen
    ? new Date(cfg.daemon_last_seen).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
    : null;

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">ERP Bridge — Agendamento</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Configure o horário de importação automática dos XMLs do Oracle ERP para o FBTax.
          </p>
        </div>
        <div className="flex items-center gap-1.5 mt-1">
          {daemonOnline ? (
            <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-green-700 bg-green-50 border border-green-200 rounded-full px-2.5 py-1">
              <span className="w-1.5 h-1.5 rounded-full bg-green-500 animate-pulse" />
              Daemon online
            </span>
          ) : (
            <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-red-700 bg-red-50 border border-red-200 rounded-full px-2.5 py-1" title={daemonLastSeen ? `Último contato: ${daemonLastSeen}` : 'Nunca conectou'}>
              <span className="w-1.5 h-1.5 rounded-full bg-red-500" />
              Daemon offline{daemonLastSeen ? ` · ${daemonLastSeen}` : ''}
            </span>
          )}
        </div>
      </div>

      {/* Run pendente aguardando daemon */}
      {pendingRun && !runningRun && (
        <Card className={`border ${daemonOnline ? 'border-amber-200 bg-amber-50/40' : 'border-red-200 bg-red-50/40'}`}>
          <CardHeader className="py-3 px-4">
            <CardTitle className="text-sm flex items-center justify-between">
              <span className="flex items-center gap-2">
                {daemonOnline
                  ? <Loader2 className="h-4 w-4 animate-spin text-amber-500" />
                  : <AlertTriangle className="h-4 w-4 text-red-500" />
                }
                {daemonOnline
                  ? 'Aguardando o daemon Bridge...'
                  : 'Daemon offline — importação não iniciará até o daemon ser reiniciado'
                }
              </span>
              <Button
                size="sm" variant="ghost"
                className="h-7 px-2 text-red-600 hover:bg-red-50 hover:text-red-700"
                onClick={() => abortMutation.mutate(pendingRun.id)}
                disabled={abortMutation.isPending}
                title="Cancelar importação antes de o daemon iniciar"
              >
                {abortMutation.isPending
                  ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  : <><Ban className="h-3.5 w-3.5 mr-1" />Cancelar</>
                }
              </Button>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-4 pb-3">
            <div className="flex flex-wrap gap-4 text-xs items-center">
              {cfg?.erp_type && <ServidorBadge erp_type={cfg.erp_type} />}
              <div>
                <span className="text-muted-foreground">Criado em: </span>
                <span className="font-medium">{fmtDateTime(pendingRun.iniciado_em)}</span>
              </div>
              {pendingRun.data_ini && (
                <div>
                  <span className="text-muted-foreground">Período: </span>
                  <span className="font-medium">{pendingRun.data_ini} → {pendingRun.data_fim}</span>
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Status do run ativo — com progresso por filial */}
      {runningRun && (
        <Card className="border border-blue-200 bg-blue-50/40">
          <CardHeader className="py-3 px-4">
            <CardTitle className="text-sm flex items-center justify-between">
              <span className="flex items-center gap-2">
                <Loader2 className="h-4 w-4 animate-spin text-blue-500" />
                Importação em andamento...
              </span>
              <span className="flex items-center gap-2 text-[11px] font-normal text-muted-foreground">
                <Button
                  size="sm" variant="ghost"
                  className="h-7 px-2 text-red-600 hover:bg-red-50 hover:text-red-700"
                  onClick={() => abortMutation.mutate(runningRun.id)}
                  disabled={abortMutation.isPending}
                  title="Interrompe após concluir o servidor atual"
                >
                  {abortMutation.isPending
                    ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    : <><Ban className="h-3.5 w-3.5 mr-1" />Abortar</>
                  }
                </Button>
                <span className="border-l pl-2">Atualiza a cada 60s</span>
                <button
                  onClick={() => refetchDetail()}
                  className="text-muted-foreground hover:text-foreground transition-colors"
                  title="Atualizar agora"
                >
                  <RefreshCw className="h-3.5 w-3.5" />
                </button>
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-4 pb-3 space-y-3">
            <div className="flex flex-wrap gap-4 text-xs items-center">
              {cfg?.erp_type && <ServidorBadge erp_type={cfg.erp_type} />}
              <div>
                <span className="text-muted-foreground">Início: </span>
                <span className="font-medium">{fmtDateTime(runningRun.iniciado_em)}</span>
              </div>
              {runningRun.data_ini && (
                <div>
                  <span className="text-muted-foreground">Período: </span>
                  <span className="font-medium">{runningRun.data_ini} → {runningRun.data_fim}</span>
                </div>
              )}
              {detailUpdatedAt > 0 && (
                <div className="text-muted-foreground ml-auto">
                  Atualizado: {new Date(detailUpdatedAt).toLocaleTimeString('pt-BR')}
                </div>
              )}
            </div>

            {/* Tabela de progresso por filial */}
            {(runDetail?.items?.length ?? 0) > 0 ? (
              <div className="overflow-x-auto rounded border bg-white">
                <table className="w-full text-[11px]">
                  <thead>
                    <tr className="border-b bg-muted/40">
                      <th className="py-1 px-2 text-left font-medium text-muted-foreground">Filial</th>
                      <th className="py-1 px-2 text-left font-medium text-muted-foreground">Tipo</th>
                      <th className="py-1 px-2 text-right font-medium text-green-700">Enviados</th>
                      <th className="py-1 px-2 text-right font-medium text-muted-foreground">Ignorados</th>
                      <th className="py-1 px-2 text-right font-medium text-red-600">Erros</th>
                      <th className="py-1 px-2 text-left font-medium text-muted-foreground">Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {runDetail!.items!.map(item => (
                      <tr key={item.id} className="border-t hover:bg-muted/20">
                        <td className="py-0.5 px-2 font-medium">{item.servidor}</td>
                        <td className="py-0.5 px-2 text-muted-foreground">{TIPO_LABELS[item.tipo] ?? item.tipo}</td>
                        <td className="py-0.5 px-2 text-right text-green-600 font-medium">{item.enviados.toLocaleString('pt-BR')}</td>
                        <td className="py-0.5 px-2 text-right text-muted-foreground">{item.ignorados.toLocaleString('pt-BR')}</td>
                        <td className="py-0.5 px-2 text-right text-red-500">{item.erros > 0 ? item.erros : '—'}</td>
                        <td className="py-0.5 px-2"><StatusBadge status={item.status} /></td>
                      </tr>
                    ))}
                  </tbody>
                  <tfoot className="border-t bg-muted/20 font-semibold">
                    <tr>
                      <td className="py-1 px-2 text-[10px] text-muted-foreground" colSpan={2}>
                        Total parcial ({runDetail!.items!.length} filial/tipo processado{runDetail!.items!.length !== 1 ? 's' : ''})
                      </td>
                      <td className="py-1 px-2 text-right text-green-600 text-[11px]">
                        {runDetail!.items!.reduce((s, i) => s + i.enviados, 0).toLocaleString('pt-BR')}
                      </td>
                      <td className="py-1 px-2 text-right text-muted-foreground text-[11px]">
                        {runDetail!.items!.reduce((s, i) => s + i.ignorados, 0).toLocaleString('pt-BR')}
                      </td>
                      <td className="py-1 px-2 text-right text-red-500 text-[11px]">
                        {runDetail!.items!.reduce((s, i) => s + i.erros, 0) || '—'}
                      </td>
                      <td />
                    </tr>
                  </tfoot>
                </table>
              </div>
            ) : (
              <p className="text-[11px] text-muted-foreground italic">
                {cfg?.erp_type === 'sap_s4hana'
                  ? 'FCCORP (SAP S/4HANA) — Aguardando início da importação...'
                  : 'Aguardando conclusão da primeira filial...'}
              </p>
            )}
          </CardContent>
        </Card>
      )}

      {/* Status do último run finalizado */}
      {lastRun && lastRun.status !== 'running' && (
        <Card>
          <CardHeader className="py-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              {lastRun.status === 'success'
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
                <span className="text-green-600 font-medium">↑ {lastRun.total_enviados.toLocaleString('pt-BR')} enviados</span>
                <span className="text-muted-foreground">/ {lastRun.total_ignorados.toLocaleString('pt-BR')} ignorados</span>
                {lastRun.total_erros > 0 && <span className="text-red-500 font-medium">{lastRun.total_erros} erros</span>}
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* ── Importação Manual ── */}
      <Card className={triggerQueued ? 'border-green-200 bg-green-50/30' : ''}>
        <CardHeader className="py-3 px-4">
          <CardTitle className="text-sm flex items-center gap-2">
            <Zap className="h-4 w-4 text-amber-500" /> Importação Manual
          </CardTitle>
        </CardHeader>
        <CardContent className="px-4 pb-4 space-y-4">
          <div className="flex items-center gap-2 text-[11px] text-muted-foreground flex-wrap">
            <span>Dispara uma importação imediatamente. O daemon Bridge a executará na próxima varredura (em até 1 minuto).</span>
            {cfg?.erp_type && <ServidorBadge erp_type={cfg.erp_type} />}
          </div>

          {/* Período */}
          <div className="flex flex-wrap gap-4 items-end">
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">De</label>
              <Input type="date" value={triggerIni}
                onChange={e => { setTriggerIni(e.target.value); setTriggerQueued(false); }}
                className="h-8 w-36 text-sm"
                disabled={triggerMutation.isPending || !!activeRun} />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Até</label>
              <Input type="date" value={triggerFim}
                onChange={e => { setTriggerFim(e.target.value); setTriggerQueued(false); }}
                className="h-8 w-36 text-sm"
                disabled={triggerMutation.isPending || !!activeRun} />
            </div>
          </div>

          {/* Apenas Parceiros — visível somente para SAP S/4HANA */}
          {cfg?.erp_type === 'sap_s4hana' && (
            <label className="flex items-center gap-2 cursor-pointer w-fit">
              <input
                type="checkbox"
                checked={onlyParceiros}
                onChange={e => { setOnlyParceiros(e.target.checked); setTriggerQueued(false); }}
                disabled={triggerMutation.isPending || !!activeRun}
                className="h-3.5 w-3.5 accent-amber-500"
              />
              <span className="text-xs text-muted-foreground">
                Apenas Parceiros (FORN/CLIE) — sem importar movimentos
              </span>
            </label>
          )}

{/* Botão + status */}
          <div className="flex items-center gap-3 pt-1">
            <Button
              size="sm"
              onClick={() => triggerMutation.mutate()}
              disabled={triggerMutation.isPending || !!activeRun || triggerQueued || !triggerIni || !triggerFim}
            >
              {triggerMutation.isPending
                ? <><Loader2 className="h-3 w-3 mr-1.5 animate-spin" />Agendando...</>
                : <><Zap className="h-3 w-3 mr-1.5" />Importar agora</>
              }
            </Button>
            {activeRun && (
              <span className="text-[11px] text-blue-600">
                {activeRun.status === 'pending'
                  ? 'Aguardando daemon — cancele acima se precisar alterar.'
                  : 'Aguarde: há uma importação em andamento.'}
              </span>
            )}
            {triggerQueued && !runningRun && (
              <span className="text-[11px] text-green-600 font-medium">
                ✓ Importação agendada — aguardando o daemon Bridge...
              </span>
            )}
          </div>
        </CardContent>
      </Card>

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

          <div className="flex justify-between items-center pt-1">
            <Button
              size="sm"
              variant="ghost"
              className="text-xs text-muted-foreground hover:text-red-600"
              onClick={() => resetTrackerMutation.mutate()}
              disabled={resetTrackerMutation.isPending || !!activeRun}
              title="Sinaliza ao daemon Bridge para limpar o histórico de notas já enviadas (tracker.db), permitindo reimportação completa."
            >
              {resetTrackerMutation.isPending
                ? <Loader2 className="h-3 w-3 mr-1.5 animate-spin" />
                : <Trash2 className="h-3 w-3 mr-1.5" />
              }
              Limpar tracker
            </Button>
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
