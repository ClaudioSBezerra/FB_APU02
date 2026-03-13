import { useState, useRef, useCallback } from 'react';
import { useAuth } from '@/contexts/AuthContext';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Upload, FolderOpen, FileText, CheckCircle, AlertCircle, SkipForward, Truck } from 'lucide-react';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------
interface UploadError {
  arquivo: string;
  erro: string;
}

interface UploadResult {
  importados: number;
  ignorados: number;
  erros: UploadError[];
}

interface CteRow {
  id: string;
  chave_cte: string;
  modelo: number;
  serie: string;
  numero_cte: string;
  data_emissao: string;
  mes_ano: string;
  nat_op: string;
  cfop: string;
  modal: string;
  emit_cnpj: string;
  emit_nome: string;
  emit_uf: string;
  rem_cnpj_cpf: string;
  rem_nome: string;
  rem_uf: string;
  dest_cnpj_cpf: string;
  dest_nome: string;
  dest_uf: string;
  v_prest: number;
  v_rec: number;
  v_carga: number;
  v_bc_icms: number;
  v_icms: number;
  v_bc_ibs_cbs: number | null;
  v_ibs: number | null;
  v_cbs: number | null;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function fmtBRL(v: number | null | undefined): string {
  if (v == null) return '—';
  return v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });
}

function fmtCNPJ(v: string): string {
  if (!v || v.length !== 14) return v || '—';
  return `${v.slice(0,2)}.${v.slice(2,5)}.${v.slice(5,8)}/${v.slice(8,12)}-${v.slice(12)}`;
}

const MODAL_LABELS: Record<string, string> = {
  '01': 'Rodoviário',
  '02': 'Aéreo',
  '03': 'Aquaviário',
  '04': 'Ferroviário',
  '05': 'Dutoviário',
  '06': 'Multimodal',
};

function fmtModal(m: string): string {
  return MODAL_LABELS[m] || m || '—';
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------
export default function ImportarXMLsCTe() {
  const { token, companyId } = useAuth();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [xmlFiles, setXmlFiles] = useState<File[]>([]);
  const [uploading, setUploading] = useState(false);
  const [result, setResult] = useState<UploadResult | null>(null);
  const [cteList, setCteList] = useState<CteRow[]>([]);
  const [loadingList, setLoadingList] = useState(false);
  const [filterMes, setFilterMes] = useState('');

  const authHeaders = {
    Authorization: `Bearer ${token}`,
    'X-Company-ID': companyId || '',
  };

  const handleFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files || []).filter(f =>
      f.name.toLowerCase().endsWith('.xml')
    );
    setXmlFiles(files);
    setResult(null);
  }, []);

  const handleUpload = async () => {
    if (xmlFiles.length === 0) {
      toast.error('Selecione uma pasta com arquivos XML antes de importar.');
      return;
    }

    setUploading(true);
    setResult(null);

    try {
      const formData = new FormData();
      xmlFiles.forEach(f => formData.append('xmls', f));

      const res = await fetch('/api/cte-entradas/upload', {
        method: 'POST',
        headers: authHeaders,
        body: formData,
      });

      const data: UploadResult = await res.json();

      if (!res.ok) {
        toast.error('Erro no upload: ' + ((data as unknown as { error: string }).error || res.statusText));
        return;
      }

      setResult(data);

      if (data.importados > 0) {
        toast.success(`${data.importados} CT-e(s) importado(s) com sucesso.`);
        fetchList();
      } else if (data.ignorados > 0 && data.importados === 0) {
        toast.info('Todos os CT-es já estavam importados (duplicatas ignoradas).');
      }

      if (data.erros && data.erros.length > 0) {
        toast.warning(`${data.erros.length} arquivo(s) com erro — veja detalhes abaixo.`);
      }
    } catch (err: unknown) {
      toast.error('Erro inesperado: ' + String(err));
    } finally {
      setUploading(false);
    }
  };

  const fetchList = async (mes?: string) => {
    setLoadingList(true);
    try {
      const params = new URLSearchParams();
      const mesFilter = mes ?? filterMes;
      if (mesFilter) params.set('mes_ano', mesFilter);

      const res = await fetch(`/api/cte-entradas?${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      const data = await res.json();
      setCteList(data.items || []);
    } catch (err: unknown) {
      toast.error('Erro ao carregar lista: ' + String(err));
    } finally {
      setLoadingList(false);
    }
  };

  const handleFilterMes = (e: React.ChangeEvent<HTMLInputElement>) => {
    setFilterMes(e.target.value);
  };

  const handleFilterSearch = () => fetchList(filterMes);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Importar XMLs CT-e de Entrada</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Importe Conhecimentos de Transporte Eletrônico (CT-e mod. 57) recebidos de transportadoras a partir de arquivos XML.
          Selecione a pasta e clique em Importar.
        </p>
      </div>

      {/* ── Card de upload ── */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <FolderOpen className="h-4 w-4" />
            Selecionar pasta de XMLs
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <input
            ref={fileInputRef}
            type="file"
            // @ts-expect-error webkitdirectory não está no tipo padrão
            webkitdirectory=""
            multiple
            accept=".xml"
            className="hidden"
            onChange={handleFileChange}
          />

          <div className="flex items-center gap-3 flex-wrap">
            <Button
              variant="outline"
              onClick={() => fileInputRef.current?.click()}
              disabled={uploading}
            >
              <FolderOpen className="h-4 w-4 mr-2" />
              Selecionar Pasta
            </Button>

            {xmlFiles.length > 0 && (
              <span className="text-sm text-muted-foreground">
                <FileText className="h-4 w-4 inline mr-1" />
                {xmlFiles.length} arquivo(s) .xml encontrado(s)
              </span>
            )}

            <Button
              onClick={handleUpload}
              disabled={uploading || xmlFiles.length === 0}
            >
              <Upload className="h-4 w-4 mr-2" />
              {uploading ? 'Importando...' : 'Importar'}
            </Button>
          </div>

          {/* ── Resultado do upload ── */}
          {result && (
            <div className="rounded-lg border p-4 space-y-3">
              <div className="flex gap-4 flex-wrap">
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-4 w-4 text-green-600" />
                  <span className="text-sm font-medium">Importados:</span>
                  <Badge variant="default" className="bg-green-600">{result.importados}</Badge>
                </div>
                <div className="flex items-center gap-2">
                  <SkipForward className="h-4 w-4 text-yellow-600" />
                  <span className="text-sm font-medium">Ignorados (duplicatas):</span>
                  <Badge variant="secondary">{result.ignorados}</Badge>
                </div>
                {result.erros.length > 0 && (
                  <div className="flex items-center gap-2">
                    <AlertCircle className="h-4 w-4 text-red-600" />
                    <span className="text-sm font-medium">Erros:</span>
                    <Badge variant="destructive">{result.erros.length}</Badge>
                  </div>
                )}
              </div>

              {result.erros.length > 0 && (
                <div className="text-xs space-y-1 max-h-40 overflow-auto">
                  {result.erros.map((e, i) => (
                    <div key={i} className="text-red-600">
                      <span className="font-medium">{e.arquivo}:</span> {e.erro}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* ── Card de listagem ── */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-4 flex-wrap">
            <CardTitle className="flex items-center gap-2 text-base">
              <Truck className="h-4 w-4" />
              CT-e Entradas Importados
            </CardTitle>
            <div className="flex items-center gap-2">
              <input
                type="text"
                placeholder="MM/YYYY"
                value={filterMes}
                onChange={handleFilterMes}
                className="h-8 w-28 rounded-md border border-input bg-background px-3 py-1 text-sm"
              />
              <Button size="sm" variant="outline" onClick={handleFilterSearch} disabled={loadingList}>
                {loadingList ? 'Buscando...' : 'Buscar'}
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {cteList.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-8">
              {loadingList
                ? 'Carregando...'
                : 'Nenhum CT-e importado. Faça uma importação ou filtre por Mês/Ano.'}
            </p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="py-1.5 px-2 text-[11px]">Modal</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px]">Série/Nº</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px]">Data</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px]">Transportadora</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px]">UF</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px]">Remetente</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px]">Destinatário</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] text-right">vPrest</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] text-right">vICMS</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] text-right">vIBS</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] text-right">vCBS</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {cteList.map(row => (
                    <TableRow key={row.id} className="h-8">
                      <TableCell className="py-1 px-2 text-center">
                        <Badge variant="outline" className="text-[10px] px-1 py-0 whitespace-nowrap">
                          {fmtModal(row.modal)}
                        </Badge>
                      </TableCell>
                      <TableCell className="py-1 px-2 text-[11px] whitespace-nowrap font-mono">
                        {row.serie}/{row.numero_cte}
                      </TableCell>
                      <TableCell className="py-1 px-2 text-[11px] whitespace-nowrap">{row.data_emissao}</TableCell>
                      <TableCell className="py-1 px-2">
                        <div className="text-[11px] font-medium leading-tight">{row.emit_nome || '—'}</div>
                        <div className="text-[10px] text-muted-foreground font-mono leading-tight">{fmtCNPJ(row.emit_cnpj)}</div>
                      </TableCell>
                      <TableCell className="py-1 px-2 text-[11px]">{row.emit_uf || '—'}</TableCell>
                      <TableCell className="py-1 px-2">
                        <div className="text-[11px] leading-tight">{row.rem_nome || '—'}</div>
                        <div className="text-[10px] text-muted-foreground font-mono leading-tight">{fmtCNPJ(row.rem_cnpj_cpf)}</div>
                      </TableCell>
                      <TableCell className="py-1 px-2">
                        <div className="text-[11px] leading-tight">{row.dest_nome || '—'}</div>
                        <div className="text-[10px] text-muted-foreground font-mono leading-tight">{fmtCNPJ(row.dest_cnpj_cpf)}</div>
                      </TableCell>
                      <TableCell className="py-1 px-2 text-right text-[11px] font-semibold">{fmtBRL(row.v_prest)}</TableCell>
                      <TableCell className="py-1 px-2 text-right text-[11px]">{fmtBRL(row.v_icms)}</TableCell>
                      <TableCell className="py-1 px-2 text-right text-[11px]">
                        {row.v_ibs != null ? fmtBRL(row.v_ibs) : <span className="text-muted-foreground">—</span>}
                      </TableCell>
                      <TableCell className="py-1 px-2 text-right text-[11px]">
                        {row.v_cbs != null ? fmtBRL(row.v_cbs) : <span className="text-muted-foreground">—</span>}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
