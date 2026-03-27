import { useState, useEffect, useMemo } from 'react';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { AlertTriangle, RefreshCw, BarChart2, X } from 'lucide-react';
import { toast } from 'sonner';
import { formatCnpjComApelido } from '@/lib/formatFilial';

// ── Types ─────────────────────────────────────────────────────────────────────
interface ResumoGeralRow {
  tipo: 'nfe-saidas' | 'nfe-entradas' | 'cte';
  ni_emitente: string;
  data_emissao: string; // YYYY-MM-DD
  quantidade: number;
  valor_cbs_nao_extinto: number;
}

interface ResumoGeralResponse {
  items: ResumoGeralRow[];
}

// ── Helpers ───────────────────────────────────────────────────────────────────
const TIPO_LABEL: Record<string, string> = {
  'nfe-saidas':   'NF-e Saídas',
  'nfe-entradas': 'NF-e Entradas',
  'cte':          'CT-e',
};
const TIPO_COLOR: Record<string, string> = {
  'nfe-saidas':   'bg-red-100 text-red-700 border-red-200',
  'nfe-entradas': 'bg-orange-100 text-orange-700 border-orange-200',
  'cte':          'bg-purple-100 text-purple-700 border-purple-200',
};

function fmtBRL(v: number): string {
  return v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });
}
function fmtNum(v: number): string {
  return v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}
function fmtCNPJ(v: string): string {
  const d = v.replace(/\D/g, '');
  if (d.length === 14) return `${d.slice(0,2)}.${d.slice(2,5)}.${d.slice(5,8)}/${d.slice(8,12)}-${d.slice(12)}`;
  return v;
}
function fmtData(iso: string): string {
  if (!iso) return '—';
  const [y, m, d] = iso.split('-');
  return `${d}/${m}/${y}`;
}

// ── Painel principal ───────────────────────────────────────────────────────────
export default function MalhaFinaResumoGeral() {
  const { token, companyId } = useAuth();

  const hoje = new Date();
  const defaultDataDe = `${hoje.getFullYear()}-${String(hoje.getMonth() + 1).padStart(2, '0')}-01`;
  const [dataDe, setDataDe] = useState(defaultDataDe);
  const [refreshing, setRefreshing] = useState(false);

  const authHeaders = { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId || '' };

  // Carrega apelidos de filiais
  const [apelidosMap, setApelidosMap] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!token || !companyId) return;
    fetch('/api/config/filial-apelidos', {
      headers: { Authorization: `Bearer ${token}`, 'X-Company-ID': companyId },
    })
      .then(r => r.ok ? r.json() : [])
      .then((list: { cnpj: string; apelido: string }[]) => {
        const map: Record<string, string> = {};
        (list || []).forEach(fa => { map[fa.cnpj.replace(/\D/g, '')] = fa.apelido; });
        setApelidosMap(map);
      })
      .catch(() => {});
  }, [token, companyId]);

  const { data, isFetching, isError, refetch } = useQuery<ResumoGeralResponse>({
    queryKey: ['malha-fina-resumo-geral', companyId, dataDe],
    queryFn: async () => {
      const params = new URLSearchParams();
      if (dataDe) params.set('data_de', dataDe);
      const res = await fetch(`/api/malha-fina/resumo-geral?${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    },
    placeholderData: keepPreviousData,
    enabled: !!token && !!companyId,
  });

  const items = data?.items ?? [];

  // Agrupar: tipo → emitente → datas[]
  const grupos = useMemo(() => {
    type DiaItem = { data: string; qtd: number; valor: number };
    type EmitenteMap = Map<string, DiaItem[]>;
    const map = new Map<string, EmitenteMap>();

    for (const row of items) {
      if (!map.has(row.tipo)) map.set(row.tipo, new Map());
      const emiMap = map.get(row.tipo)!;
      const cnpj = row.ni_emitente.replace(/\D/g, '');
      if (!emiMap.has(cnpj)) emiMap.set(cnpj, []);
      emiMap.get(cnpj)!.push({ data: row.data_emissao, qtd: row.quantidade, valor: row.valor_cbs_nao_extinto });
    }
    return map;
  }, [items]);

  // Totais por tipo
  const totais = useMemo(() => {
    const t: Record<string, { qtd: number; valor: number }> = {};
    for (const row of items) {
      if (!t[row.tipo]) t[row.tipo] = { qtd: 0, valor: 0 };
      t[row.tipo].qtd   += row.quantidade;
      t[row.tipo].valor += row.valor_cbs_nao_extinto;
    }
    return t;
  }, [items]);

  const totalGeral = useMemo(() =>
    items.reduce((s, r) => ({ qtd: s.qtd + r.quantidade, valor: s.valor + r.valor_cbs_nao_extinto }), { qtd: 0, valor: 0 }),
    [items]
  );

  async function handleRefresh() {
    setRefreshing(true);
    try {
      const res = await fetch('/api/malha-fina/resumo-geral/refresh', {
        method: 'POST',
        headers: authHeaders,
      });
      if (!res.ok) throw new Error();
      toast.success('Atualização iniciada — aguarde alguns instantes e recarregue');
      setTimeout(() => { refetch(); }, 4000);
    } catch {
      toast.error('Erro ao iniciar atualização');
    } finally {
      setRefreshing(false);
    }
  }

  return (
    <div className="space-y-6">
      {/* Cabeçalho */}
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Resumo Malha Fina</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Visão consolidada de todos os documentos na RFB não importados — NF-e Saídas, NF-e Entradas e CT-e.
        </p>
      </div>

      {/* Aviso */}
      <div className="flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
        <AlertTriangle className="h-4 w-4 mt-0.5 shrink-0" />
        <span>
          Documentos identificados pela Receita Federal que <strong>não constam nos seus registros importados</strong>.
          Os dados são pré-calculados — use <strong>Atualizar</strong> após novos downloads da RFB.
        </span>
      </div>

      {/* Filtros + Atualizar */}
      <Card>
        <CardContent className="pt-4">
          <div className="flex flex-wrap gap-3 items-end">
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Data Início</label>
              <Input
                type="date"
                value={dataDe}
                onChange={e => setDataDe(e.target.value)}
                className="h-8 w-40"
              />
            </div>
            {dataDe && (
              <Button size="sm" variant="ghost" onClick={() => setDataDe('')} className="self-end">
                <X className="h-3 w-3 mr-1" /> Limpar data
              </Button>
            )}
            <Button
              size="sm"
              variant="outline"
              onClick={handleRefresh}
              disabled={refreshing}
              className="self-end gap-1.5"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`} />
              {refreshing ? 'Atualizando...' : 'Atualizar Resumo'}
            </Button>
            <span className="text-xs text-muted-foreground ml-auto self-end">
              {isFetching ? 'Carregando...' : `${totalGeral.qtd.toLocaleString('pt-BR')} doc(s) — ${fmtBRL(totalGeral.valor)}`}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* Cards de totais por tipo */}
      {Object.keys(totais).length > 0 && (
        <div className="grid grid-cols-3 gap-2">
          {(['nfe-saidas', 'nfe-entradas', 'cte'] as const).map(tipo => {
            const t = totais[tipo];
            if (!t) return null;
            return (
              <Card key={tipo} className="p-3">
                <div className="flex items-center gap-1.5 mb-1">
                  <Badge variant="outline" className={`text-[10px] px-1.5 py-0 ${TIPO_COLOR[tipo]}`}>
                    {TIPO_LABEL[tipo]}
                  </Badge>
                </div>
                <p className="text-xs text-muted-foreground">{t.qtd.toLocaleString('pt-BR')} documentos</p>
                <p className="text-sm font-bold text-red-700 mt-0.5">{fmtBRL(t.valor)}</p>
              </Card>
            );
          })}
        </div>
      )}

      {/* Tabela de resumo */}
      {isError ? (
        <p className="text-xs text-red-500 text-center py-8">Erro ao carregar resumo. Verifique se a MV foi criada (migration 072).</p>
      ) : items.length === 0 && !isFetching ? (
        <div className="text-center py-12 space-y-2">
          <p className="text-sm font-medium text-green-700">Nenhum documento na malha fina</p>
          <p className="text-xs text-muted-foreground">
            Todos os documentos da RFB estão presentes nos registros importados para o período selecionado,
            ou o resumo ainda não foi calculado — clique em <strong>Atualizar Resumo</strong>.
          </p>
        </div>
      ) : (
        Array.from(grupos.entries()).map(([tipo, emiMap]) => (
          <Card key={tipo}>
            <CardHeader className="py-2 px-4">
              <CardTitle className="flex items-center gap-2 text-xs font-medium">
                <BarChart2 className="h-3.5 w-3.5" />
                <Badge variant="outline" className={`text-[10px] px-1.5 py-0 ${TIPO_COLOR[tipo]}`}>
                  {TIPO_LABEL[tipo]}
                </Badge>
                <span className="text-muted-foreground font-normal">
                  {Array.from(emiMap.values()).flat().reduce((s, d) => s + d.qtd, 0).toLocaleString('pt-BR')} documentos
                </span>
              </CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="py-1.5 px-3 text-[11px]">CNPJ Emitente</TableHead>
                      <TableHead className="py-1.5 px-3 text-[11px]">Apelido Filial</TableHead>
                      <TableHead className="py-1.5 px-3 text-[11px]">Data Emissão</TableHead>
                      <TableHead className="py-1.5 px-3 text-[11px] text-right">Qtd Docs</TableHead>
                      <TableHead className="py-1.5 px-3 text-[11px] text-right">CBS Não Extinto (R$)</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {Array.from(emiMap.entries()).flatMap(([cnpj, datas]) => {
                      const apelido = apelidosMap[cnpj] || '';
                      const totalGrupo = datas.reduce((s, d) => s + d.qtd, 0);
                      const valorGrupo = datas.reduce((s, d) => s + d.valor, 0);
                      return datas.map((d, idx) => (
                        <TableRow
                          key={`${cnpj}-${d.data}`}
                          className={idx === 0 ? 'border-t-2 border-t-muted' : ''}
                        >
                          <TableCell className="py-1 px-3 font-mono text-[11px]">
                            {idx === 0 ? fmtCNPJ(cnpj) : ''}
                          </TableCell>
                          <TableCell className="py-1 px-3 text-[11px]">
                            {idx === 0
                              ? (apelido
                                ? <Badge variant="outline" className="text-[10px] px-1.5 py-0">{apelido}</Badge>
                                : <span className="text-muted-foreground text-[10px]">{formatCnpjComApelido(cnpj, apelidosMap)}</span>)
                              : ''}
                          </TableCell>
                          <TableCell className="py-1 px-3 text-[11px] whitespace-nowrap">
                            {fmtData(d.data)}
                          </TableCell>
                          <TableCell className="py-1 px-3 text-[11px] text-right font-semibold">
                            {d.qtd.toLocaleString('pt-BR')}
                            {idx === 0 && datas.length > 1 && (
                              <span className="ml-1 text-[10px] text-muted-foreground font-normal">
                                ({totalGrupo.toLocaleString('pt-BR')} total)
                              </span>
                            )}
                          </TableCell>
                          <TableCell className="py-1 px-3 text-[11px] text-right text-red-700 font-semibold">
                            {idx === 0 ? fmtNum(valorGrupo) : fmtNum(d.valor)}
                          </TableCell>
                        </TableRow>
                      ));
                    })}
                  </TableBody>
                </Table>
              </div>
            </CardContent>
          </Card>
        ))
      )}
    </div>
  );
}
