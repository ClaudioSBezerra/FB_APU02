import { useState, useRef, useCallback, useMemo } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { Upload, FolderOpen, FileText, CheckCircle, AlertCircle, SkipForward, ArrowDownToLine, ArrowUpFromLine } from 'lucide-react';

interface UploadError { arquivo: string; erro: string; }
interface UploadResult { importados: number; ignorados: number; erros: UploadError[]; }

type Direcao = 'entrada' | 'saida';

interface TipoDocumento {
  key: string;
  label: string;
  enabled: boolean;
}

const TIPOS: TipoDocumento[] = [
  { key: 'nfe',     label: 'Nota Fiscal Eletrônica (NF-e)',     enabled: true },
  { key: 'cte',     label: 'Conhecimento de Frete (CT-e)',     enabled: true },
  { key: 'nfce',    label: 'Nota Fiscal Consumidor (NFC-e)',    enabled: false },
  { key: 'nfse',    label: 'Nota Fiscal de Serviços (NFS-e)',   enabled: false },
  { key: 'nce',     label: 'Nota de Crédito (NC-e)',            enabled: false },
  { key: 'nde',     label: 'Nota de Débito (ND-e)',             enabled: false },
  { key: 'bpe',     label: 'Bilhete de Passagem (BP-e)',        enabled: false },
  { key: 'nf3e',    label: 'NF de Energia Elétrica (NF3e)',     enabled: false },
  { key: 'nfcom',   label: 'NF de Comunicação (NFCom)',         enabled: false },
  { key: 'agua',    label: 'NF de Água e Esgoto',               enabled: false },
  { key: 'gas',     label: 'NF de Gás encanado',                enabled: false },
  { key: 'eventos', label: 'Eventos',                           enabled: false },
  { key: 'outros',  label: 'Outros',                            enabled: false },
];

interface UploadConfig {
  endpoint: string;
  descricao: string;
  mensagemSucesso: (n: number) => string;
  mensagemDuplicatas: string;
}

const UPLOAD_CONFIG: Record<string, UploadConfig> = {
  'nfe-entrada': {
    endpoint: '/api/nfe-entradas/upload',
    descricao: 'Importe NF-e (mod. 55) de entrada a partir de arquivos XML.',
    mensagemSucesso: n => `${n} NF-e(s) importada(s). Visualize em "Notas Importadas".`,
    mensagemDuplicatas: 'Todas as NF-es já estavam importadas (duplicatas ignoradas).',
  },
  'nfe-saida': {
    endpoint: '/api/nfe-saidas/upload',
    descricao: 'Importe NF-e (mod. 55) de saída a partir de arquivos XML.',
    mensagemSucesso: n => `${n} NF-e(s) importada(s). Visualize em "Notas Importadas".`,
    mensagemDuplicatas: 'Todas as NF-es já estavam importadas (duplicatas ignoradas).',
  },
  'cte-entrada': {
    endpoint: '/api/cte-entradas/upload',
    descricao: 'Importe Conhecimentos de Transporte Eletrônico (CT-e mod. 57) a partir de arquivos XML.',
    mensagemSucesso: n => `${n} CT-e(s) importado(s). Visualize em "Notas Importadas".`,
    mensagemDuplicatas: 'Todos os CT-es já estavam importados (duplicatas ignoradas).',
  },
};

const DIRECOES_DISPONIVEIS: Record<string, Direcao[]> = {
  nfe: ['entrada', 'saida'],
  cte: ['entrada'],
};

export default function DFesModulo() {
  const [tipoAtivo, setTipoAtivo] = useState('nfe');
  const [direcao, setDirecao] = useState<Direcao>('entrada');
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [xmlFiles, setXmlFiles] = useState<File[]>([]);
  const [uploading, setUploading] = useState(false);
  const [result, setResult] = useState<UploadResult | null>(null);

  const direcoesDoTipo = DIRECOES_DISPONIVEIS[tipoAtivo] ?? [];
  const configAtual = UPLOAD_CONFIG[`${tipoAtivo}-${direcao}`];

  const selecionarTipo = useCallback((tipo: TipoDocumento) => {
    if (!tipo.enabled || uploading) return;
    setTipoAtivo(tipo.key);
    const direcoes = DIRECOES_DISPONIVEIS[tipo.key] ?? [];
    setDirecao(direcoes[0] ?? 'entrada');
    setXmlFiles([]);
    setResult(null);
  }, [uploading]);

  const selecionarDirecao = useCallback((d: Direcao) => {
    if (uploading) return;
    setDirecao(d);
    setXmlFiles([]);
    setResult(null);
  }, [uploading]);

  const handleFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    setXmlFiles(Array.from(e.target.files || []).filter(f => f.name.toLowerCase().endsWith('.xml')));
    setResult(null);
  }, []);

  const handleUpload = async () => {
    if (!configAtual) return;
    if (xmlFiles.length === 0) { toast.error('Selecione uma pasta com arquivos XML antes de importar.'); return; }
    setUploading(true);
    setResult(null);
    try {
      const formData = new FormData();
      xmlFiles.forEach(f => formData.append('xmls', f));
      const res = await fetch(configAtual.endpoint, { method: 'POST', body: formData });
      const data: UploadResult = await res.json();
      if (!res.ok) { toast.error('Erro no upload: ' + ((data as unknown as { error: string }).error || res.statusText)); return; }
      setResult(data);
      if (data.importados > 0) toast.success(configAtual.mensagemSucesso(data.importados));
      else if (data.ignorados > 0) toast.info(configAtual.mensagemDuplicatas);
      if (data.erros?.length > 0) toast.warning(`${data.erros.length} arquivo(s) com erro — veja detalhes abaixo.`);
    } catch (err: unknown) {
      toast.error('Erro inesperado: ' + String(err));
    } finally {
      setUploading(false);
    }
  };

  const tipoAtivoInfo = useMemo(() => TIPOS.find(t => t.key === tipoAtivo), [tipoAtivo]);

  return (
    <div className="flex gap-6">
      <aside className="w-64 shrink-0 space-y-1">
        {TIPOS.map(tipo => (
          <button
            key={tipo.key}
            onClick={() => selecionarTipo(tipo)}
            disabled={!tipo.enabled || uploading}
            className={cn(
              'w-full text-left px-3 py-2 rounded-md text-sm transition-colors flex items-center justify-between gap-2',
              (!tipo.enabled || uploading) && 'text-muted-foreground/50 cursor-not-allowed',
              tipo.enabled && tipoAtivo === tipo.key && 'bg-primary/10 text-primary font-semibold',
              tipo.enabled && tipoAtivo !== tipo.key && 'hover:bg-gray-100 text-foreground',
            )}
          >
            <span>{tipo.label}</span>
          </button>
        ))}
      </aside>

      <div className="flex-1 min-w-0 space-y-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{tipoAtivoInfo?.label}</h1>
          {configAtual && <p className="text-sm text-muted-foreground mt-1">{configAtual.descricao}</p>}
        </div>

        {direcoesDoTipo.length > 0 && (
          <div className="flex items-center gap-2">
            {(['entrada', 'saida'] as Direcao[]).map(d => {
              const disponivel = direcoesDoTipo.includes(d);
              const Icon = d === 'entrada' ? ArrowDownToLine : ArrowUpFromLine;
              return (
                <Button
                  key={d}
                  variant={direcao === d ? 'default' : 'outline'}
                  size="sm"
                  disabled={!disponivel || uploading}
                  onClick={() => selecionarDirecao(d)}
                >
                  <Icon className="h-4 w-4 mr-2" />
                  {d === 'entrada' ? 'Entrada' : 'Saída'}
                </Button>
              );
            })}
          </div>
        )}

        {configAtual ? (
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
        ) : (
          <Card>
            <CardContent className="py-10 text-center text-sm text-muted-foreground">
              Esta combinação de tipo de documento e direção ainda não está disponível.
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  );
}
