import { useState, useRef, useCallback, useEffect } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
  Upload, Download, FileText, Trash2, ChevronDown, ChevronUp,
  AlertCircle, CheckCircle, SkipForward,
} from 'lucide-react';

interface Pagamento {
  id: number;
  chave_doc: string;
  tipo_doc: string;
  forn_cnpj: string;
  forn_nome: string | null;
  data_pagamento: string;
  valor_pagamento: number;
  num_doc_pagamento: string | null;
  descricao: string | null;
  mes_ano: string;
  import_id: string;
  importado_em: string;
}

interface ImportBatch {
  id: string;
  mes_ano: string | null;
  filename: string | null;
  total_linhas: number;
  importados: number;
  duplicados: number;
  erros: number;
  importado_em: string;
}

interface ImportError {
  linha: number;
  campo: string;
  erro: string;
}

interface ImportResult {
  import_id: string;
  total_linhas: number;
  importados: number;
  duplicados: number;
  erros: ImportError[];
}

// ─── Formatadores ─────────────────────────────────────────────────────────────

function formatCNPJ(cnpj: string): string {
  const d = cnpj.replace(/\D/g, '');
  if (d.length !== 14) return cnpj;
  return d.replace(/^(\d{2})(\d{3})(\d{3})(\d{4})(\d{2})$/, '$1.$2.$3/$4-$5');
}

function formatDate(dateStr: string): string {
  if (!dateStr) return '';
  const parts = dateStr.split('-');
  if (parts.length === 3) return `${parts[2]}/${parts[1]}/${parts[0]}`;
  return dateStr;
}

function formatBRL(value: number): string {
  return new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' }).format(value);
}

function truncate(str: string, maxLen: number): string {
  if (str.length <= maxLen) return str;
  return str.substring(0, maxLen) + '...';
}

// ─── Componente principal ────────────────────────────────────────────────────

export default function ImportarPagamentosFornecedores() {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [csvFile, setCsvFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);
  const [lastResult, setLastResult] = useState<ImportResult | null>(null);
  const [downloadingTemplate, setDownloadingTemplate] = useState(false);

  // Filtros
  const [filterMesAno, setFilterMesAno] = useState('');
  const [filterCNPJ, setFilterCNPJ] = useState('');
  const [filterChaveDoc, setFilterChaveDoc] = useState('');
  const [appliedFilters, setAppliedFilters] = useState({ mes_ano: '', forn_cnpj: '', chave_doc: '' });

  // Tabela de pagamentos
  const [pagamentos, setPagamentos] = useState<Pagamento[]>([]);
  const [loadingPagamentos, setLoadingPagamentos] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize] = useState(50);
  const [total, setTotal] = useState(0);
  const [pagRefreshKey, setPagRefreshKey] = useState(0);

  // Histórico de imports
  const [showHistorico, setShowHistorico] = useState(false);
  const [historico, setHistorico] = useState<ImportBatch[]>([]);
  const [loadingHistorico, setLoadingHistorico] = useState(false);
  const [histRefreshKey, setHistRefreshKey] = useState(0);

  // ─── Buscar pagamentos ─────────────────────────────────────────────────────

  const fetchPagamentos = useCallback(async (currentPage: number, filters: typeof appliedFilters) => {
    setLoadingPagamentos(true);
    try {
      const params = new URLSearchParams();
      if (filters.mes_ano) params.set('mes_ano', filters.mes_ano);
      if (filters.forn_cnpj) params.set('forn_cnpj', filters.forn_cnpj);
      if (filters.chave_doc) params.set('chave_doc', filters.chave_doc);
      params.set('page', String(currentPage));
      params.set('page_size', String(pageSize));

      const res = await fetch(`/api/pagamentos-fornecedores?${params.toString()}`);
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json() as { items: Pagamento[]; total: number };
      setPagamentos(data.items ?? []);
      setTotal(data.total ?? 0);
    } catch (err: unknown) {
      toast.error('Erro ao carregar pagamentos: ' + String(err));
    } finally {
      setLoadingPagamentos(false);
    }
  }, [pageSize]);

  useEffect(() => {
    fetchPagamentos(page, appliedFilters);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, appliedFilters, pagRefreshKey]);

  // ─── Buscar histórico ──────────────────────────────────────────────────────

  const fetchHistorico = useCallback(async () => {
    setLoadingHistorico(true);
    try {
      const res = await fetch('/api/pagamentos-fornecedores/imports');
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json() as { items: ImportBatch[] };
      setHistorico(data.items ?? []);
    } catch (err: unknown) {
      toast.error('Erro ao carregar histórico: ' + String(err));
    } finally {
      setLoadingHistorico(false);
    }
  }, []);

  useEffect(() => {
    if (showHistorico) {
      fetchHistorico();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [showHistorico, histRefreshKey]);

  // ─── Download template ─────────────────────────────────────────────────────

  const handleDownloadTemplate = async () => {
    setDownloadingTemplate(true);
    try {
      const res = await fetch('/api/pagamentos-fornecedores/template');
      if (!res.ok) throw new Error('Erro ao baixar template');
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'template_pagamentos_fornecedores.csv';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      toast.success('Template baixado com sucesso.');
    } catch (err: unknown) {
      toast.error('Erro ao baixar template: ' + String(err));
    } finally {
      setDownloadingTemplate(false);
    }
  };

  // ─── Upload CSV ────────────────────────────────────────────────────────────

  const handleFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0] ?? null;
    setCsvFile(file);
    setLastResult(null);
  }, []);

  const handleImport = async () => {
    if (!csvFile) {
      toast.error('Selecione um arquivo CSV antes de importar.');
      return;
    }
    setUploading(true);
    setLastResult(null);
    try {
      const formData = new FormData();
      formData.append('csv', csvFile);
      const res = await fetch('/api/pagamentos-fornecedores/import', { method: 'POST', body: formData });
      const raw = await res.json() as ImportResult & { error?: string };
      if (!res.ok) {
        toast.error('Erro no import: ' + (raw.error ?? res.statusText));
        return;
      }
      // Normaliza erros para array: backend pode responder `null` quando não
      // há nenhum erro (slice nil do Go serializado como JSON null) — sem essa
      // normalização, .length abaixo (e no render) quebra com "Cannot read
      // properties of null".
      const data: ImportResult = { ...raw, erros: raw.erros ?? [] };
      setLastResult(data);
      if (data.importados > 0 && data.duplicados === 0 && data.erros.length === 0) {
        toast.success(`${data.importados} pagamento(s) importado(s) com sucesso.`);
      } else if (data.importados > 0) {
        toast.info(`${data.importados} importado(s), ${data.duplicados} duplicado(s), ${data.erros.length} erro(s).`);
      } else if (data.duplicados > 0 && data.importados === 0) {
        toast.warning('Todos os registros já estavam importados (duplicatas ignoradas).');
      } else if (data.erros.length > 0) {
        toast.error(`Import concluído com ${data.erros.length} erro(s). Verifique os detalhes abaixo.`);
      }
      if (data.importados > 0 || data.duplicados > 0) {
        setPagRefreshKey(k => k + 1);
        setHistRefreshKey(k => k + 1);
      }
    } catch (err: unknown) {
      toast.error('Erro inesperado: ' + String(err));
    } finally {
      setUploading(false);
    }
  };

  // ─── Aplicar filtros ───────────────────────────────────────────────────────

  const handleApplyFilters = () => {
    setPage(1);
    setAppliedFilters({
      mes_ano: filterMesAno,
      forn_cnpj: filterCNPJ,
      chave_doc: filterChaveDoc,
    });
  };

  const handleClearFilters = () => {
    setFilterMesAno('');
    setFilterCNPJ('');
    setFilterChaveDoc('');
    setPage(1);
    setAppliedFilters({ mes_ano: '', forn_cnpj: '', chave_doc: '' });
  };

  // ─── Desfazer import ───────────────────────────────────────────────────────

  const handleDesfazer = async (batch: ImportBatch) => {
    const confirmMsg = `Tem certeza? Isto vai apagar ${batch.importados} pagamento(s) deste batch.`;
    if (!window.confirm(confirmMsg)) return;
    try {
      const res = await fetch(`/api/pagamentos-fornecedores/imports/${batch.id}`, { method: 'DELETE' });
      const data = await res.json() as { deleted?: number; error?: string };
      if (!res.ok) {
        toast.error('Erro ao desfazer: ' + (data.error ?? res.statusText));
        return;
      }
      toast.success(`${data.deleted ?? 0} pagamento(s) removido(s).`);
      setHistRefreshKey(k => k + 1);
      setPagRefreshKey(k => k + 1);
    } catch (err: unknown) {
      toast.error('Erro ao desfazer import: ' + String(err));
    }
  };

  // ─── Render ────────────────────────────────────────────────────────────────

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className="space-y-6">
      {/* Cabeçalho */}
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Pagamentos a Fornecedores</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Importe e rastreie pagamentos realizados a fornecedores via CSV.
          Os dados alimentarão futuramente o cruzamento com créditos RFB (IBS/CBS).
        </p>
      </div>

      {/* Card Filtros */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Filtros</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap gap-3 items-end">
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Mês/Ano</label>
              <input
                type="month"
                value={filterMesAno}
                onChange={e => setFilterMesAno(e.target.value)}
                className="border rounded-md px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring w-40"
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">CNPJ Fornecedor</label>
              <input
                type="text"
                placeholder="00.000.000/0000-00"
                value={filterCNPJ}
                onChange={e => setFilterCNPJ(e.target.value)}
                className="border rounded-md px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring w-48"
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Chave Doc</label>
              <input
                type="text"
                placeholder="Buscar chave..."
                value={filterChaveDoc}
                onChange={e => setFilterChaveDoc(e.target.value)}
                className="border rounded-md px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring w-48"
              />
            </div>
            <div className="flex gap-2 pt-4">
              <Button size="sm" onClick={handleApplyFilters}>Aplicar</Button>
              <Button size="sm" variant="outline" onClick={handleClearFilters}>Limpar</Button>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Card Importar CSV */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Upload className="h-4 w-4" />
            Importar CSV
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap items-center gap-3">
            <Button
              variant="outline"
              size="sm"
              onClick={handleDownloadTemplate}
              disabled={downloadingTemplate}
            >
              <Download className="h-4 w-4 mr-2" />
              {downloadingTemplate ? 'Baixando...' : 'Baixar Template'}
            </Button>

            <input
              ref={fileInputRef}
              type="file"
              accept=".csv"
              className="hidden"
              onChange={handleFileChange}
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() => fileInputRef.current?.click()}
              disabled={uploading}
            >
              <FileText className="h-4 w-4 mr-2" />
              {csvFile ? csvFile.name : 'Selecionar CSV'}
            </Button>
            <Button
              size="sm"
              onClick={handleImport}
              disabled={uploading || !csvFile}
            >
              <Upload className="h-4 w-4 mr-2" />
              {uploading ? 'Importando...' : 'Importar'}
            </Button>
          </div>

          {/* Resultado do import */}
          {lastResult && (
            <div className="rounded-lg border p-4 space-y-3">
              <div className="flex flex-wrap gap-4">
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-4 w-4 text-green-600" />
                  <span className="text-sm font-medium">Importados:</span>
                  <Badge variant="default" className="bg-green-600">{lastResult.importados}</Badge>
                </div>
                <div className="flex items-center gap-2">
                  <SkipForward className="h-4 w-4 text-yellow-600" />
                  <span className="text-sm font-medium">Duplicados:</span>
                  <Badge variant="secondary">{lastResult.duplicados}</Badge>
                </div>
                {lastResult.erros.length > 0 && (
                  <div className="flex items-center gap-2">
                    <AlertCircle className="h-4 w-4 text-red-600" />
                    <span className="text-sm font-medium">Erros:</span>
                    <Badge variant="destructive">{lastResult.erros.length}</Badge>
                  </div>
                )}
              </div>
              {lastResult.erros.length > 0 && (
                <div className="text-xs space-y-1 max-h-40 overflow-auto border rounded p-2 bg-red-50">
                  {lastResult.erros.map((e, i) => (
                    <div key={i} className="text-red-600">
                      <span className="font-medium">Linha {e.linha} ({e.campo}):</span> {e.erro}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Card Tabela de Pagamentos */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">
            Pagamentos importados
            {total > 0 && (
              <span className="ml-2 text-sm font-normal text-muted-foreground">
                ({total} registro{total !== 1 ? 's' : ''})
              </span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {loadingPagamentos ? (
            <p className="text-sm text-muted-foreground">Carregando...</p>
          ) : pagamentos.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Nenhum pagamento importado para os filtros selecionados.
            </p>
          ) : (
            <div className="space-y-3">
              <div className="overflow-x-auto">
                <table className="w-full text-xs border-collapse">
                  <thead>
                    <tr className="border-b bg-muted/50">
                      <th className="text-left px-2 py-2 font-medium">Data Pagto</th>
                      <th className="text-left px-2 py-2 font-medium">Tipo</th>
                      <th className="text-left px-2 py-2 font-medium">Chave Doc</th>
                      <th className="text-left px-2 py-2 font-medium">CNPJ Fornecedor</th>
                      <th className="text-left px-2 py-2 font-medium">Nome Fornecedor</th>
                      <th className="text-right px-2 py-2 font-medium">Valor</th>
                      <th className="text-left px-2 py-2 font-medium">Num Doc</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pagamentos.map(pag => (
                      <tr key={pag.id} className="border-b hover:bg-muted/30">
                        <td className="px-2 py-1.5 whitespace-nowrap">{formatDate(pag.data_pagamento)}</td>
                        <td className="px-2 py-1.5">
                          <Badge variant="outline" className="text-xs">{pag.tipo_doc}</Badge>
                        </td>
                        <td className="px-2 py-1.5 font-mono" title={pag.chave_doc}>
                          {truncate(pag.chave_doc, 20)}
                        </td>
                        <td className="px-2 py-1.5 whitespace-nowrap">{formatCNPJ(pag.forn_cnpj)}</td>
                        <td className="px-2 py-1.5">{pag.forn_nome || <span className="text-muted-foreground italic">—</span>}</td>
                        <td className="px-2 py-1.5 text-right whitespace-nowrap font-medium">{formatBRL(pag.valor_pagamento)}</td>
                        <td className="px-2 py-1.5">{pag.num_doc_pagamento || <span className="text-muted-foreground italic">—</span>}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              {/* Paginação */}
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">
                  Página {page} de {totalPages}
                </span>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setPage(p => Math.max(1, p - 1))}
                    disabled={page <= 1}
                  >
                    Anterior
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                    disabled={page >= totalPages}
                  >
                    Próximo
                  </Button>
                </div>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Card Histórico de Imports */}
      <Card>
        <CardHeader>
          <CardTitle
            className="text-base flex items-center gap-2 cursor-pointer select-none"
            onClick={() => setShowHistorico(v => !v)}
          >
            {showHistorico ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
            Histórico de Imports
          </CardTitle>
        </CardHeader>
        {showHistorico && (
          <CardContent>
            {loadingHistorico ? (
              <p className="text-sm text-muted-foreground">Carregando histórico...</p>
            ) : historico.length === 0 ? (
              <p className="text-sm text-muted-foreground">Nenhum import registrado.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-xs border-collapse">
                  <thead>
                    <tr className="border-b bg-muted/50">
                      <th className="text-left px-2 py-2 font-medium">Data/Hora</th>
                      <th className="text-left px-2 py-2 font-medium">Arquivo</th>
                      <th className="text-right px-2 py-2 font-medium">Total</th>
                      <th className="text-right px-2 py-2 font-medium">Import.</th>
                      <th className="text-right px-2 py-2 font-medium">Duplic.</th>
                      <th className="text-right px-2 py-2 font-medium">Erros</th>
                      <th className="px-2 py-2"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {historico.map(batch => (
                      <tr key={batch.id} className="border-b hover:bg-muted/30">
                        <td className="px-2 py-1.5 whitespace-nowrap">
                          {new Date(batch.importado_em).toLocaleString('pt-BR')}
                        </td>
                        <td className="px-2 py-1.5">{batch.filename || <span className="text-muted-foreground italic">—</span>}</td>
                        <td className="px-2 py-1.5 text-right">{batch.total_linhas}</td>
                        <td className="px-2 py-1.5 text-right text-green-700">{batch.importados}</td>
                        <td className="px-2 py-1.5 text-right text-yellow-700">{batch.duplicados}</td>
                        <td className="px-2 py-1.5 text-right text-red-700">{batch.erros}</td>
                        <td className="px-2 py-1.5">
                          <Button
                            size="sm"
                            variant="destructive"
                            className="h-6 px-2 text-xs"
                            onClick={() => handleDesfazer(batch)}
                          >
                            <Trash2 className="h-3 w-3 mr-1" />
                            Desfazer
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        )}
      </Card>
    </div>
  );
}
