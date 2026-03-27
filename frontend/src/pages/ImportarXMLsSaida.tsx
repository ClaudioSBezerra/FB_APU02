import { useState, useEffect, useRef, useCallback } from 'react';
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Upload, FolderOpen, FileText, CheckCircle, AlertCircle, SkipForward } from 'lucide-react';

function buildMesAnoOptions() {
  const opts: { value: string; label: string }[] = [];
  const now = new Date();
  for (let i = 0; i < 24; i++) {
    const d = new Date(now.getFullYear(), now.getMonth() - i, 1);
    const mm = String(d.getMonth() + 1).padStart(2, '0');
    opts.push({ value: `${mm}/${d.getFullYear()}`, label: `${mm}/${d.getFullYear()}` });
  }
  return opts;
}
const MES_ANO_OPTIONS = buildMesAnoOptions();

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
  dest_cnpj_cpf: string;
  dest_nome: string;
  dest_uf: string;
  dest_c_mun: string;
  v_prod: number;
  v_desc: number;
  v_nf: number;
  v_bc: number;
  v_icms: number;
  v_pis: number;
  v_cofins: number;
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

function fmtNum(v: number | null | undefined): string {
  if (v == null) return '—';
  return v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}


// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------
export default function ImportarXMLsSaida() {
  const { token, companyId } = useAuth();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [xmlFiles, setXmlFiles] = useState<File[]>([]);
  const [uploading, setUploading] = useState(false);
  const [result, setResult] = useState<UploadResult | null>(null);
  const [nfeList, setNfeList] = useState<NfeSaidaRow[]>([]);
  const [loadingList, setLoadingList] = useState(false);
  const [filterMes, setFilterMes] = useState(MES_ANO_OPTIONS[0]?.value ?? '');

  const authHeaders = {
    Authorization: `Bearer ${token}`,
    'X-Company-ID': companyId || '',
  };

  // ── Seleção de pasta / arquivos ──────────────────────────────────────────
  const handleFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files || []).filter(f =>
      f.name.toLowerCase().endsWith('.xml')
    );
    setXmlFiles(files);
    setResult(null);
  }, []);

  // ── Upload ───────────────────────────────────────────────────────────────
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

      const res = await fetch('/api/nfe-saidas/upload', {
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
        toast.success(`${data.importados} NF-e(s) importada(s) com sucesso.`);
        fetchList();
      } else if (data.ignorados > 0 && data.importados === 0) {
        toast.info('Todas as NF-es já estavam importadas (duplicatas ignoradas).');
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

  // ── Buscar lista ─────────────────────────────────────────────────────────
  const fetchList = async (mes?: string) => {
    setLoadingList(true);
    try {
      const params = new URLSearchParams();
      const mesFilter = mes ?? filterMes;
      if (mesFilter) params.set('mes_ano', mesFilter);

      const res = await fetch(`/api/nfe-saidas?${params}`, { headers: authHeaders });
      if (!res.ok) throw new Error(res.statusText);
      const data = await res.json();
      setNfeList(data.items || []);
    } catch (err: unknown) {
      toast.error('Erro ao carregar lista: ' + String(err));
    } finally {
      setLoadingList(false);
    }
  };

  useEffect(() => { if (token && companyId) fetchList(filterMes); }, [filterMes, token, companyId]); // eslint-disable-line react-hooks/exhaustive-deps

  // ── Render ───────────────────────────────────────────────────────────────
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Importar XMLs de Saída</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Importe NF-e (mod. 55) e NFC-e (mod. 65) de saída a partir de arquivos XML.
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
          {/* Input oculto com suporte a pasta */}
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
            <CardTitle className="text-base">NF-e Saídas Importadas</CardTitle>
            <div className="flex items-center gap-2">
              <Select value={filterMes} onValueChange={setFilterMes}>
                <SelectTrigger className="h-8 w-32 text-[11px]"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {MES_ANO_OPTIONS.map(o => (
                    <SelectItem key={o.value} value={o.value} className="text-xs">{o.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {loadingList && <span className="text-xs text-muted-foreground">Carregando...</span>}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {nfeList.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-8">
              {loadingList
                ? 'Carregando...'
                : 'Nenhuma NF-e importada. Faça uma importação ou filtre por Mês/Ano.'}
            </p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="py-1.5 px-2 text-[11px] w-8">Mod</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] whitespace-nowrap">Série/Nº</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] whitespace-nowrap">Data</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px]">Emitente</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px]">Destinatário</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] text-right whitespace-nowrap">vNF (R$)</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] text-right">vIBS (R$)</TableHead>
                    <TableHead className="py-1.5 px-2 text-[11px] text-right">vCBS (R$)</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {nfeList.map(row => (
                    <TableRow key={row.id}>
                      <TableCell className="py-0.5 px-2 text-center">
                        <Badge variant="outline" className="text-[10px] px-1 py-0">{row.modelo}</Badge>
                      </TableCell>
                      <TableCell className="py-0.5 px-2 text-[11px] font-mono whitespace-nowrap">
                        {row.serie}/{row.numero_nfe}
                      </TableCell>
                      <TableCell className="py-0.5 px-2 text-[11px] whitespace-nowrap">{row.data_emissao}</TableCell>
                      <TableCell className="py-0.5 px-2 max-w-[180px]">
                        <div className="truncate text-[11px] font-medium" title={`${row.emit_uf} · ${row.emit_nome}`}>
                          <span className="text-[10px] text-muted-foreground mr-1">{row.emit_uf}</span>
                          {row.emit_nome}
                        </div>
                      </TableCell>
                      <TableCell className="py-0.5 px-2 max-w-[180px]">
                        <div className="truncate text-[11px]" title={`${row.dest_uf ?? ''} · ${row.dest_nome}`}>
                          <span className="text-[10px] text-muted-foreground mr-1">{row.dest_uf}</span>
                          {row.dest_nome}
                        </div>
                      </TableCell>
                      <TableCell className="py-0.5 px-2 text-right text-[11px] font-semibold whitespace-nowrap">{fmtNum(row.v_nf)}</TableCell>
                      <TableCell className="py-0.5 px-2 text-right text-[11px]">{fmtNum(row.v_ibs)}</TableCell>
                      <TableCell className="py-0.5 px-2 text-right text-[11px]">{fmtNum(row.v_cbs)}</TableCell>
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
