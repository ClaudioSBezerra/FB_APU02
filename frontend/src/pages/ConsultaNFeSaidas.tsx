import { useState, useEffect, useMemo } from 'react';
import { useAuth } from '@/contexts/AuthContext';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Search, X } from 'lucide-react';
import { formatCnpjComApelido } from '@/lib/formatFilial';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------
interface NfeSaidaRow {
  id: string;
  chave_nfe: string;
  modelo: number;
  serie: string;
  numero_nfe: string;
  data_emissao: string;
  mes_ano: string;
  nat_op: string;
  emit_cnpj: string;
  emit_nome: string;
  emit_uf: string;
  emit_municipio: string;
  dest_cnpj_cpf: string;
  dest_nome: string;
  dest_uf: string;
  dest_c_mun: string;
  // ICMSTot
  v_bc: number;
  v_icms: number;
  v_icms_deson: number;
  v_fcp: number;
  v_bc_st: number;
  v_st: number;
  v_fcp_st: number;
  v_fcp_st_ret: number;
  v_prod: number;
  v_frete: number;
  v_seg: number;
  v_desc: number;
  v_ii: number;
  v_ipi: number;
  v_ipi_devol: number;
  v_pis: number;
  v_cofins: number;
  v_outro: number;
  v_nf: number;
  // IBSCBSTot
  v_bc_ibs_cbs: number | null;
  v_ibs_uf: number | null;
  v_ibs_mun: number | null;
  v_ibs: number | null;
  v_cred_pres_ibs: number | null;
  v_cbs: number | null;
  v_cred_pres_cbs: number | null;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function fmtBRL(v: number | null | undefined, dash = '—'): string {
  if (v == null) return dash;
  return v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });
}

function fmtCNPJ(v: string): string {
  if (!v) return '—';
  const d = v.replace(/\D/g, '');
  if (d.length === 14)
    return `${d.slice(0,2)}.${d.slice(2,5)}.${d.slice(5,8)}/${d.slice(8,12)}-${d.slice(12)}`;
  if (d.length === 11)
    return `${d.slice(0,3)}.${d.slice(3,6)}.${d.slice(6,9)}-${d.slice(9)}`;
  return v;
}

/** Converte "DD/MM/YYYY" → Date (para comparação de range) */
function parseDMY(s: string): Date | null {
  const m = s?.match(/^(\d{2})\/(\d{2})\/(\d{4})$/);
  if (!m) return null;
  return new Date(+m[3], +m[2] - 1, +m[1]);
}

// ---------------------------------------------------------------------------
// Detalhe da Nota (Dialog)
// ---------------------------------------------------------------------------
function DetalheNFe({ nfe, onClose }: { nfe: NfeSaidaRow; onClose: () => void }) {
  const Linha = ({ label, value }: { label: string; value: string | number | null | undefined }) => (
    <div className="flex justify-between py-0.5 border-b border-dashed last:border-0">
      <span className="text-[11px] text-muted-foreground w-36 shrink-0">{label}</span>
      <span className="text-[11px] font-medium text-right">{value ?? '—'}</span>
    </div>
  );

  const LinhaBRL = ({ label, value }: { label: string; value: number | null | undefined }) => (
    <div className="flex justify-between py-0.5 border-b border-dashed last:border-0">
      <span className="text-[11px] text-muted-foreground w-36 shrink-0">{label}</span>
      <span className="text-[11px] font-medium text-right">{fmtBRL(value, '—')}</span>
    </div>
  );

  const Secao = ({ title, children }: { title: string; children: React.ReactNode }) => (
    <div className="mb-2">
      <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-1 pb-0.5 border-b">
        {title}
      </h3>
      {children}
    </div>
  );

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="text-xs">
            NF-e {nfe.modelo} · Série {nfe.serie} · Nº {nfe.numero_nfe}
            <div className="text-[11px] font-normal text-muted-foreground mt-0.5 break-all">
              Chave: {nfe.chave_nfe}
            </div>
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-1 mt-1">
          <Secao title="Identificação">
            <Linha label="Modelo" value={nfe.modelo} />
            <Linha label="Série" value={nfe.serie} />
            <Linha label="Número" value={nfe.numero_nfe} />
            <Linha label="Data Emissão" value={nfe.data_emissao} />
            <Linha label="Mês/Ano" value={nfe.mes_ano} />
            <Linha label="Natureza Operação" value={nfe.nat_op} />
          </Secao>

          <Secao title="Emitente (Filial)">
            <Linha label="CNPJ" value={fmtCNPJ(nfe.emit_cnpj)} />
            <Linha label="Razão Social" value={nfe.emit_nome} />
            <Linha label="Município" value={nfe.emit_municipio} />
            <Linha label="UF" value={nfe.emit_uf} />
          </Secao>

          <Secao title="Destinatário (Cliente)">
            <Linha label="CNPJ/CPF" value={fmtCNPJ(nfe.dest_cnpj_cpf)} />
            <Linha label="Nome/Razão Social" value={nfe.dest_nome} />
            <Linha label="UF" value={nfe.dest_uf} />
            <Linha label="Município (IBGE)" value={nfe.dest_c_mun} />
          </Secao>

          <Secao title="ICMSTot — Totais da Nota">
            <LinhaBRL label="vProd" value={nfe.v_prod} />
            <LinhaBRL label="vFrete" value={nfe.v_frete} />
            <LinhaBRL label="vSeg" value={nfe.v_seg} />
            <LinhaBRL label="vDesc" value={nfe.v_desc} />
            <LinhaBRL label="vII" value={nfe.v_ii} />
            <LinhaBRL label="vIPI" value={nfe.v_ipi} />
            <LinhaBRL label="vIPIDevol" value={nfe.v_ipi_devol} />
            <LinhaBRL label="vPIS" value={nfe.v_pis} />
            <LinhaBRL label="vCOFINS" value={nfe.v_cofins} />
            <LinhaBRL label="vOutro" value={nfe.v_outro} />
            <LinhaBRL label="vNF (Valor Total)" value={nfe.v_nf} />
            <LinhaBRL label="vBC (Base ICMS)" value={nfe.v_bc} />
            <LinhaBRL label="vICMS" value={nfe.v_icms} />
            <LinhaBRL label="vICMSDeson" value={nfe.v_icms_deson} />
            <LinhaBRL label="vFCP" value={nfe.v_fcp} />
            <LinhaBRL label="vBCST" value={nfe.v_bc_st} />
            <LinhaBRL label="vST" value={nfe.v_st} />
            <LinhaBRL label="vFCPST" value={nfe.v_fcp_st} />
            <LinhaBRL label="vFCPSTRet" value={nfe.v_fcp_st_ret} />
          </Secao>

          <Secao title="IBSCBSTot — Reforma Tributária">
            <LinhaBRL label="vBCIBSCBS (Base)" value={nfe.v_bc_ibs_cbs} />
            <LinhaBRL label="vIBSUF" value={nfe.v_ibs_uf} />
            <LinhaBRL label="vIBSMun" value={nfe.v_ibs_mun} />
            <LinhaBRL label="vIBS (Total)" value={nfe.v_ibs} />
            <LinhaBRL label="vCredPres IBS" value={nfe.v_cred_pres_ibs} />
            <LinhaBRL label="vCBS" value={nfe.v_cbs} />
            <LinhaBRL label="vCredPres CBS" value={nfe.v_cred_pres_cbs} />
          </Secao>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Página principal
// ---------------------------------------------------------------------------
export default function ConsultaNFeSaidas() {
  const { token, companyId } = useAuth();

  const [items, setItems] = useState<NfeSaidaRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<NfeSaidaRow | null>(null);
  const [apelidos, setApelidos] = useState<Record<string, string>>({});

  // Filtros client-side
  const [filterFilial, setFilterFilial] = useState('all');
  const [filterCliente, setFilterCliente] = useState('');
  const [filterDataDe, setFilterDataDe] = useState('');
  const [filterDataAte, setFilterDataAte] = useState('');

  const authHeaders = {
    Authorization: `Bearer ${token}`,
    'X-Company-ID': companyId || '',
  };

  // Carrega apelidos de filiais
  useEffect(() => {
    if (!token) return;
    fetch('/api/config/filial-apelidos', { headers: authHeaders })
      .then(r => r.ok ? r.json() : [])
      .then((list: { cnpj: string; apelido: string }[]) => {
        const map: Record<string, string> = {};
        (list || []).forEach(fa => { map[fa.cnpj] = fa.apelido; });
        setApelidos(map);
      })
      .catch(() => {});
  }, [token, companyId]); // eslint-disable-line react-hooks/exhaustive-deps

  const fetchData = async () => {
    setLoading(true);
    try {
      const res = await fetch('/api/nfe-saidas', { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      const data = await res.json();
      setItems(data.items || []);
      setFilterFilial('all');
      setFilterCliente('');
      setFilterDataDe('');
      setFilterDataAte('');
    } catch (err: unknown) {
      toast.error('Erro ao buscar notas: ' + String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const clearFilters = () => {
    setFilterFilial('all');
    setFilterCliente('');
    setFilterDataDe('');
    setFilterDataAte('');
  };

  // Filiais únicas derivadas dos dados carregados
  const uniqueFiliais = useMemo(() => {
    const seen = new Map<string, string>();
    items.forEach(r => { if (r.emit_cnpj) seen.set(r.emit_cnpj, r.emit_nome); });
    return Array.from(seen.entries())
      .map(([cnpj, nome]) => ({ cnpj, nome }))
      .sort((a, b) => a.nome.localeCompare(b.nome));
  }, [items]);

  // Filtros client-side aplicados
  const displayItems = useMemo(() => {
    const dataDe  = filterDataDe  ? new Date(filterDataDe)  : null;
    const dataAte = filterDataAte ? new Date(filterDataAte) : null;

    return items.filter(r => {
      if (filterFilial !== 'all' && r.emit_cnpj !== filterFilial) return false;

      if (filterCliente) {
        const nomeOk = r.dest_nome?.toLowerCase().includes(filterCliente.toLowerCase());
        const cnpjOk = r.dest_cnpj_cpf?.replace(/\D/g, '').includes(filterCliente.replace(/\D/g, ''));
        if (!nomeOk && !cnpjOk) return false;
      }

      if (dataDe || dataAte) {
        const d = parseDMY(r.data_emissao);
        if (!d) return false;
        if (dataDe && d < dataDe) return false;
        if (dataAte && d > dataAte) return false;
      }

      return true;
    });
  }, [items, filterFilial, filterCliente, filterDataDe, filterDataAte]);

  const hasClientFilters = filterFilial !== 'all' || filterCliente || filterDataDe || filterDataAte;

  const totalVNF  = displayItems.reduce((s, r) => s + r.v_nf,           0);
  const totalICMS = displayItems.reduce((s, r) => s + r.v_icms,          0);
  const totalIBS  = displayItems.reduce((s, r) => s + (r.v_ibs  ?? 0),  0);
  const totalCBS  = displayItems.reduce((s, r) => s + (r.v_cbs  ?? 0),  0);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Notas de Saída</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Consulta de NF-e e NFC-e de saída importadas via XML. Clique em uma linha para ver todos os dados.
        </p>
      </div>

      {/* ── Filtros ── */}
      <Card>
        <CardContent className="pt-4 space-y-3">

          {/* Linha 1: Recarregar */}
          <div className="flex flex-wrap gap-3 items-end">
            <Button size="sm" onClick={fetchData} disabled={loading}>
              <Search className="h-3 w-3 mr-1" />
              {loading ? 'Carregando...' : 'Recarregar'}
            </Button>
            {hasClientFilters && (
              <Button size="sm" variant="ghost" onClick={clearFilters}>
                <X className="h-3 w-3 mr-1" />
                Limpar filtros
              </Button>
            )}
            <span className="text-xs text-muted-foreground ml-auto self-end">
              {displayItems.length} de {items.length} nota(s)
            </span>
          </div>

          {/* Linha 2: Filial + Cliente + Datas — só aparecem após dados carregados */}
          {items.length > 0 && (
            <div className="flex flex-wrap gap-3 items-end border-t pt-3">

              {/* Filial */}
              <div className="flex flex-col gap-1">
                <label className="text-xs text-muted-foreground">Filial</label>
                <Select value={filterFilial} onValueChange={setFilterFilial}>
                  <SelectTrigger className="h-8 w-64 text-[11px]">
                    <SelectValue placeholder="Todas as filiais" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">Todas as filiais</SelectItem>
                    {uniqueFiliais.map(f => (
                      <SelectItem key={f.cnpj} value={f.cnpj}>
                        {formatCnpjComApelido(f.cnpj, apelidos)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              {/* Cliente */}
              <div className="flex flex-col gap-1">
                <label className="text-xs text-muted-foreground">Cliente (nome ou CNPJ/CPF)</label>
                <Input
                  placeholder="Digite nome ou documento..."
                  value={filterCliente}
                  onChange={e => setFilterCliente(e.target.value)}
                  className="h-8 w-60"
                />
              </div>

              {/* Data De */}
              <div className="flex flex-col gap-1">
                <label className="text-xs text-muted-foreground">Emissão De</label>
                <Input
                  type="date"
                  value={filterDataDe}
                  onChange={e => setFilterDataDe(e.target.value)}
                  className="h-8 w-36"
                />
              </div>

              {/* Data Até */}
              <div className="flex flex-col gap-1">
                <label className="text-xs text-muted-foreground">Emissão Até</label>
                <Input
                  type="date"
                  value={filterDataAte}
                  onChange={e => setFilterDataAte(e.target.value)}
                  className="h-8 w-36"
                />
              </div>

            </div>
          )}
        </CardContent>
      </Card>

      {/* ── Totalizador ── */}
      {displayItems.length > 0 && (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
          {[
            { label: 'Total vNF',   value: totalVNF },
            { label: 'Total vICMS', value: totalICMS },
            { label: 'Total vIBS',  value: totalIBS },
            { label: 'Total vCBS',  value: totalCBS },
          ].map(c => (
            <Card key={c.label} className="p-2">
              <p className="text-[10px] text-muted-foreground">{c.label}</p>
              <p className="text-xs font-bold mt-0.5">{fmtBRL(c.value)}</p>
            </Card>
          ))}
        </div>
      )}

      {/* ── Tabela ── */}
      <Card>
        <CardHeader className="py-2 px-4">
          <CardTitle className="text-[11px] text-muted-foreground font-normal">
            Clique em uma linha para ver todos os dados da nota
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {displayItems.length === 0 ? (
            <p className="text-xs text-muted-foreground text-center py-8">
              {loading ? 'Carregando...' : 'Nenhuma nota encontrada. Use os filtros acima.'}
            </p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="py-1.5 px-2 text-[11px]">CNPJ Emitente</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px]">Filial / UF</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px]">Cliente</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px]">Data</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] text-center">Série</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] text-center">Nº Nota</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] text-center">Mod</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] text-right">Valor Total (vNF)</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {displayItems.map(row => (
                    <TableRow
                      key={row.id}
                      className="cursor-pointer hover:bg-muted/50 h-8"
                      onClick={() => setSelected(row)}
                    >
                      <TableCell className="py-1 px-2 font-mono text-[11px]">
                        {fmtCNPJ(row.emit_cnpj)}
                      </TableCell>
                      <TableCell className="py-1 px-2">
                        <div className="text-[11px] font-medium leading-tight">{row.emit_nome || '—'}</div>
                        <div className="text-[10px] text-muted-foreground leading-tight">{row.emit_uf}</div>
                      </TableCell>
                      <TableCell className="py-1 px-2">
                        <div className="text-[11px] font-medium leading-tight">{row.dest_nome || '—'}</div>
                        <div className="text-[10px] text-muted-foreground font-mono leading-tight">
                          {fmtCNPJ(row.dest_cnpj_cpf)}
                        </div>
                      </TableCell>
                      <TableCell className="py-1 px-2 text-[11px] whitespace-nowrap">
                        {row.data_emissao}
                      </TableCell>
                      <TableCell className="py-1 px-2 text-[11px] text-center">{row.serie}</TableCell>
                      <TableCell className="py-1 px-2 text-[11px] text-center font-mono">{row.numero_nfe}</TableCell>
                      <TableCell className="py-1 px-2 text-center">
                        <Badge variant="outline" className="text-[10px] px-1 py-0">{row.modelo}</Badge>
                      </TableCell>
                      <TableCell className="py-1 px-2 text-[11px] text-right font-semibold">
                        {fmtBRL(row.v_nf)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      {/* ── Dialog de detalhe ── */}
      {selected && (
        <DetalheNFe nfe={selected} onClose={() => setSelected(null)} />
      )}
    </div>
  );
}
