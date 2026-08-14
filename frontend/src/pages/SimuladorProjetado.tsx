import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Pencil, Check, X, RotateCcw } from 'lucide-react';
import { toast } from 'sonner';

interface Aliquota {
  ano: number;
  perc_ibs_uf: number;
  perc_ibs_mun: number;
  perc_cbs: number;
  perc_reduc_icms: number;
  perc_reduc_piscofins: number;
  editado: boolean;
}

interface ProjecaoAno {
  ano: number;
  perc_ibs: number;
  perc_cbs: number;
  debito_ibs_projetado: number;
  debito_cbs_projetado: number;
  credito_ibs_projetado: number;
  credito_cbs_projetado: number;
  saldo_ibs: number;
  saldo_cbs: number;
}

const MESES = [
  { v: '01', l: 'Janeiro' }, { v: '02', l: 'Fevereiro' }, { v: '03', l: 'Março' },
  { v: '04', l: 'Abril' },   { v: '05', l: 'Maio' },      { v: '06', l: 'Junho' },
  { v: '07', l: 'Julho' },   { v: '08', l: 'Agosto' },    { v: '09', l: 'Setembro' },
  { v: '10', l: 'Outubro' }, { v: '11', l: 'Novembro' },  { v: '12', l: 'Dezembro' },
];

function fmtSaldo(v: number): string {
  const abs = fmtBRL(Math.abs(v));
  return v >= 0 ? `${abs} a pagar` : `${abs} a recuperar`;
}

interface ProjecaoResponse {
  base_debito_ibs_cbs: number;
  base_credito_ibs_cbs: number;
  projecao: ProjecaoAno[];
}

function fmtPct(v: number): string {
  return `${v.toFixed(2)}%`;
}

function fmtBRL(v: number): string {
  return new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' }).format(v);
}

// ── Linha editável da tabela de alíquotas ──────────────────────────────────

function LinhaAliquota({ aliquota, onSaved }: { aliquota: Aliquota; onSaved: () => void }) {
  const [editing, setEditing] = useState(false);
  const [form, setForm] = useState(aliquota);
  const [saving, setSaving] = useState(false);

  function startEdit() {
    setForm(aliquota);
    setEditing(true);
  }

  async function save() {
    setSaving(true);
    try {
      const res = await fetch('/api/rfb/simulador/aliquotas', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      });
      if (!res.ok) {
        const text = await res.text();
        toast.error(text || 'Erro ao salvar alíquota');
        return;
      }
      toast.success(`Alíquotas de ${aliquota.ano} atualizadas`);
      setEditing(false);
      onSaved();
    } catch {
      toast.error('Erro de conexão');
    } finally {
      setSaving(false);
    }
  }

  async function resetar() {
    if (!confirm(`Voltar ${aliquota.ano} para o valor oficial?`)) return;
    try {
      const res = await fetch(`/api/rfb/simulador/aliquotas?ano=${aliquota.ano}`, { method: 'DELETE' });
      if (!res.ok) {
        toast.error('Erro ao resetar');
        return;
      }
      toast.success(`${aliquota.ano} voltou ao valor oficial`);
      onSaved();
    } catch {
      toast.error('Erro de conexão');
    }
  }

  function numInput(key: keyof Aliquota) {
    return (
      <Input
        type="number" step="0.01" min={0} max={100}
        value={form[key] as number}
        onChange={e => setForm(f => ({ ...f, [key]: parseFloat(e.target.value) || 0 }))}
        className="h-7 text-xs text-right w-20 ml-auto"
      />
    );
  }

  return (
    <tr className={`border-t ${aliquota.editado ? 'bg-blue-50/40' : ''}`}>
      <td className="py-1 px-2 text-xs font-medium">
        {aliquota.ano}
        {aliquota.editado && <span className="ml-1.5 text-[9px] text-blue-600 font-normal">(editado)</span>}
      </td>
      <td className="py-1 px-2 text-right text-xs">{editing ? numInput('perc_ibs_uf') : fmtPct(aliquota.perc_ibs_uf)}</td>
      <td className="py-1 px-2 text-right text-xs">{editing ? numInput('perc_ibs_mun') : fmtPct(aliquota.perc_ibs_mun)}</td>
      <td className="py-1 px-2 text-right text-xs font-semibold text-blue-600">
        {fmtPct((editing ? form.perc_ibs_uf + form.perc_ibs_mun : aliquota.perc_ibs_uf + aliquota.perc_ibs_mun))}
      </td>
      <td className="py-1 px-2 text-right text-xs">{editing ? numInput('perc_cbs') : fmtPct(aliquota.perc_cbs)}</td>
      <td className="py-1 px-2 text-right text-xs text-red-500">{editing ? numInput('perc_reduc_icms') : `-${fmtPct(aliquota.perc_reduc_icms)}`}</td>
      <td className="py-1 px-2 text-right text-xs text-red-500">{editing ? numInput('perc_reduc_piscofins') : `-${fmtPct(aliquota.perc_reduc_piscofins)}`}</td>
      <td className="py-1 px-2 text-right">
        {editing ? (
          <div className="flex justify-end gap-1">
            <Button size="sm" variant="ghost" className="h-6 w-6 p-0" disabled={saving} onClick={save}>
              <Check className="h-3.5 w-3.5 text-green-600" />
            </Button>
            <Button size="sm" variant="ghost" className="h-6 w-6 p-0" disabled={saving} onClick={() => setEditing(false)}>
              <X className="h-3.5 w-3.5 text-muted-foreground" />
            </Button>
          </div>
        ) : (
          <div className="flex justify-end gap-1">
            <Button size="sm" variant="ghost" className="h-6 w-6 p-0" onClick={startEdit}>
              <Pencil className="h-3.5 w-3.5 text-muted-foreground" />
            </Button>
            {aliquota.editado && (
              <Button size="sm" variant="ghost" className="h-6 w-6 p-0" onClick={resetar} title="Voltar ao valor oficial">
                <RotateCcw className="h-3.5 w-3.5 text-muted-foreground" />
              </Button>
            )}
          </div>
        )}
      </td>
    </tr>
  );
}

// ── Página principal ────────────────────────────────────────────────────────

export default function SimuladorProjetado() {
  const queryClient = useQueryClient();
  const [anoFiltro, setAnoFiltro] = useState<string>('todos');
  const [mesFiltro, setMesFiltro] = useState<string>('todos');

  const { data: aliquotasData, isLoading: loadingAliquotas } = useQuery<{ aliquotas: Aliquota[] }>({
    queryKey: ['simulador-aliquotas'],
    queryFn: async () => {
      const res = await fetch('/api/rfb/simulador/aliquotas');
      if (!res.ok) throw new Error('Erro ao carregar alíquotas');
      return res.json();
    },
  });

  const { data: projecaoData, isLoading: loadingProjecao } = useQuery<ProjecaoResponse>({
    queryKey: ['simulador-projecao', anoFiltro, mesFiltro],
    queryFn: async () => {
      const params = new URLSearchParams();
      if (anoFiltro !== 'todos' && mesFiltro !== 'todos') {
        params.set('periodo', `${mesFiltro}/${anoFiltro}`);
      } else if (anoFiltro !== 'todos') {
        params.set('ano', anoFiltro);
      }
      const res = await fetch(`/api/rfb/simulador/projecao?${params}`);
      if (!res.ok) throw new Error('Erro ao carregar projeção');
      return res.json();
    },
  });

  const aliquotas = aliquotasData?.aliquotas ?? [];
  const projecao = projecaoData?.projecao ?? [];

  function refetchAliquotas() {
    queryClient.invalidateQueries({ queryKey: ['simulador-aliquotas'] });
    queryClient.invalidateQueries({ queryKey: ['simulador-projecao'] });
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Simulador Projetado</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Alíquotas de IBS/CBS editáveis por empresa (2027–2033) e projeção de débitos e créditos
          com base nos valores atuais de IBS/CBS importados.
        </p>
      </div>

      <Card>
        <CardHeader className="py-3 px-4">
          <CardTitle className="text-sm">Alíquotas da Transição (editáveis)</CardTitle>
          <CardDescription className="text-xs">
            Valores oficiais (EC 132/2023) por padrão — edite uma linha pra testar um cenário diferente,
            sem afetar mais nada no sistema. "Resetar" volta ao valor oficial.
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {loadingAliquotas ? (
            <p className="text-xs text-muted-foreground text-center py-8">Carregando...</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="bg-muted/40">
                    <th className="py-1.5 px-2 text-left font-medium text-muted-foreground">Ano</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">IBS UF %</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">IBS Mun %</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">IBS Total %</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">CBS %</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Redução ICMS %</th>
                    <th className="py-1.5 px-2 text-right font-medium text-muted-foreground">Redução PIS/COFINS %</th>
                    <th className="py-1.5 px-2 w-16"></th>
                  </tr>
                </thead>
                <tbody>
                  {aliquotas.map(a => (
                    <LinhaAliquota key={a.ano} aliquota={a} onSaved={refetchAliquotas} />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="py-3 px-4 flex flex-row items-center justify-between">
          <div>
            <CardTitle className="text-sm">Apuração Projetada (Débito − Crédito por imposto)</CardTitle>
            <CardDescription className="text-xs">
              Base = soma de v_bc_ibs_cbs das notas importadas no período (débito: NF-e saídas; crédito: NF-e/CT-e entradas).
            </CardDescription>
          </div>
          <div className="flex gap-2">
            <Select
              value={mesFiltro}
              onValueChange={setMesFiltro}
              disabled={anoFiltro === 'todos'}
            >
              <SelectTrigger className="h-8 text-xs w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="todos" className="text-xs">Ano todo</SelectItem>
                {MESES.map(m => (
                  <SelectItem key={m.v} value={m.v} className="text-xs">{m.l}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={anoFiltro}
              onValueChange={v => { setAnoFiltro(v); if (v === 'todos') setMesFiltro('todos'); }}
            >
              <SelectTrigger className="h-8 text-xs w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="todos" className="text-xs">Todos os períodos</SelectItem>
                {Array.from({ length: 6 }, (_, i) => 2026 - i).map(a => (
                  <SelectItem key={a} value={String(a)} className="text-xs">{a}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </CardHeader>
        <CardContent className="p-0">
          {loadingProjecao ? (
            <p className="text-xs text-muted-foreground text-center py-8">Carregando...</p>
          ) : (
            <>
              <div className="flex gap-4 px-4 py-2 text-xs border-b bg-muted/20">
                <span>Base Débito (v_bc_ibs_cbs): <strong>{fmtBRL(projecaoData?.base_debito_ibs_cbs ?? 0)}</strong></span>
                <span>Base Crédito (v_bc_ibs_cbs): <strong>{fmtBRL(projecaoData?.base_credito_ibs_cbs ?? 0)}</strong></span>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="bg-muted/40">
                      <th className="py-1.5 px-2 text-left font-medium text-muted-foreground" rowSpan={2}>Ano</th>
                      <th className="py-1.5 px-2 text-center font-medium text-muted-foreground border-l" colSpan={3}>IBS</th>
                      <th className="py-1.5 px-2 text-center font-medium text-muted-foreground border-l" colSpan={3}>CBS</th>
                    </tr>
                    <tr className="bg-muted/40">
                      <th className="py-1 px-2 text-right font-medium text-muted-foreground border-l">Débito</th>
                      <th className="py-1 px-2 text-right font-medium text-muted-foreground">Crédito</th>
                      <th className="py-1 px-2 text-right font-medium text-muted-foreground">Saldo</th>
                      <th className="py-1 px-2 text-right font-medium text-muted-foreground border-l">Débito</th>
                      <th className="py-1 px-2 text-right font-medium text-muted-foreground">Crédito</th>
                      <th className="py-1 px-2 text-right font-medium text-muted-foreground">Saldo</th>
                    </tr>
                  </thead>
                  <tbody>
                    {projecao.map(p => (
                      <tr key={p.ano} className="border-t">
                        <td className="py-1 px-2 font-medium">
                          {p.ano}
                          <div className="text-[9px] text-muted-foreground">IBS {fmtPct(p.perc_ibs)} · CBS {fmtPct(p.perc_cbs)}</div>
                        </td>
                        <td className="py-1 px-2 text-right border-l">{fmtBRL(p.debito_ibs_projetado)}</td>
                        <td className="py-1 px-2 text-right">{fmtBRL(p.credito_ibs_projetado)}</td>
                        <td className={`py-1 px-2 text-right font-semibold ${p.saldo_ibs >= 0 ? 'text-red-600' : 'text-green-600'}`}>
                          {fmtSaldo(p.saldo_ibs)}
                        </td>
                        <td className="py-1 px-2 text-right border-l">{fmtBRL(p.debito_cbs_projetado)}</td>
                        <td className="py-1 px-2 text-right">{fmtBRL(p.credito_cbs_projetado)}</td>
                        <td className={`py-1 px-2 text-right font-semibold ${p.saldo_cbs >= 0 ? 'text-red-600' : 'text-green-600'}`}>
                          {fmtSaldo(p.saldo_cbs)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
