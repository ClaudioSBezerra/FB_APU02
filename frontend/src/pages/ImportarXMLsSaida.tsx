import { useState, useRef, useCallback } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Upload, FolderOpen, FileText, CheckCircle, AlertCircle, SkipForward } from 'lucide-react';

interface UploadError { arquivo: string; erro: string; }
interface UploadResult { importados: number; ignorados: number; erros: UploadError[]; }

export default function ImportarXMLsSaida() {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [xmlFiles, setXmlFiles] = useState<File[]>([]);
  const [uploading, setUploading] = useState(false);
  const [result, setResult] = useState<UploadResult | null>(null);

  const handleFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    setXmlFiles(Array.from(e.target.files || []).filter(f => f.name.toLowerCase().endsWith('.xml')));
    setResult(null);
  }, []);

  const handleUpload = async () => {
    if (xmlFiles.length === 0) { toast.error('Selecione uma pasta com arquivos XML antes de importar.'); return; }
    setUploading(true);
    setResult(null);
    try {
      const formData = new FormData();
      xmlFiles.forEach(f => formData.append('xmls', f));
      const res = await fetch('/api/nfe-saidas/upload', { method: 'POST', body: formData });
      const data: UploadResult = await res.json();
      if (!res.ok) { toast.error('Erro no upload: ' + ((data as unknown as { error: string }).error || res.statusText)); return; }
      setResult(data);
      if (data.importados > 0) toast.success(`${data.importados} NF-e(s) importada(s). Visualize em "Notas Importadas".`);
      else if (data.ignorados > 0) toast.info('Todas as NF-es já estavam importadas (duplicatas ignoradas).');
      if (data.erros?.length > 0) toast.warning(`${data.erros.length} arquivo(s) com erro — veja detalhes abaixo.`);
    } catch (err: unknown) {
      toast.error('Erro inesperado: ' + String(err));
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Importar XMLs de Saída</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Importe NF-e (mod. 55) e NFC-e (mod. 65) de saída a partir de arquivos XML.
          Após importar, visualize os dados em <strong>Notas Importadas → NF-e Saídas</strong>.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <FolderOpen className="h-4 w-4" />
            Selecionar pasta de XMLs
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <input ref={fileInputRef} type="file"
            // @ts-expect-error webkitdirectory não está no tipo padrão
            webkitdirectory="" multiple accept=".xml" className="hidden" onChange={handleFileChange} />

          <div className="flex items-center gap-3 flex-wrap">
            <Button variant="outline" onClick={() => fileInputRef.current?.click()} disabled={uploading}>
              <FolderOpen className="h-4 w-4 mr-2" />Selecionar Pasta
            </Button>
            {xmlFiles.length > 0 && (
              <span className="text-sm text-muted-foreground">
                <FileText className="h-4 w-4 inline mr-1" />{xmlFiles.length} arquivo(s) .xml encontrado(s)
              </span>
            )}
            <Button onClick={handleUpload} disabled={uploading || xmlFiles.length === 0}>
              <Upload className="h-4 w-4 mr-2" />{uploading ? 'Importando...' : 'Importar'}
            </Button>
          </div>

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
                    <div key={i} className="text-red-600"><span className="font-medium">{e.arquivo}:</span> {e.erro}</div>
                  ))}
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
