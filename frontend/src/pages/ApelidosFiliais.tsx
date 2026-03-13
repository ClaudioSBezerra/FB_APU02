import { useEffect, useRef, useState } from "react";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { toast } from "sonner";
import { Upload, Trash2, Tag } from "lucide-react";
import { useAuth } from "@/contexts/AuthContext";

interface FilialApelido {
  cnpj: string;
  apelido: string;
}

function formatCNPJ(cnpj: string): string {
  const d = cnpj.replace(/\D/g, "");
  if (d.length !== 14) return cnpj;
  return `${d.slice(0, 2)}.${d.slice(2, 5)}.${d.slice(5, 8)}/${d.slice(8, 12)}-${d.slice(12)}`;
}

export default function ApelidosFiliais() {
  const { token, companyId } = useAuth();
  const [data, setData] = useState<FilialApelido[]>([]);
  const [loading, setLoading] = useState(true);
  const [importing, setImporting] = useState(false);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [importErrors, setImportErrors] = useState<string[]>([]);
  const [showErrors, setShowErrors] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const authHeaders = {
    Authorization: `Bearer ${token}`,
    "X-Company-ID": companyId || "",
  };

  const fetchData = () => {
    setLoading(true);
    fetch("/api/config/filial-apelidos", { headers: authHeaders })
      .then(async (res) => {
        if (!res.ok) {
          const text = await res.text();
          throw new Error(`Erro ${res.status}: ${text.slice(0, 100)}`);
        }
        return res.json();
      })
      .then((list: FilialApelido[]) => {
        setData(list || []);
        setLoading(false);
      })
      .catch((err) => {
        console.error("FilialApelidos fetch error:", err);
        toast.error("Erro ao carregar apelidos: " + err.message);
        setLoading(false);
      });
  };

  useEffect(() => {
    if (token) fetchData();
  }, [token, companyId]);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0] || null;
    setSelectedFile(f);
    setImportErrors([]);
    setShowErrors(false);
  };

  const handleImport = async () => {
    if (!selectedFile) {
      toast.error("Selecione um arquivo CSV antes de importar.");
      return;
    }

    setImporting(true);
    setImportErrors([]);
    setShowErrors(false);

    const formData = new FormData();
    formData.append("file", selectedFile);

    try {
      const res = await fetch("/api/config/filial-apelidos/import", {
        method: "POST",
        headers: authHeaders,
        body: formData,
      });

      const json = await res.json();

      if (!res.ok) {
        toast.error("Erro na importação: " + (json.error || res.statusText));
        return;
      }

      const { imported, skipped, errors } = json;
      const msg = `${imported} apelido(s) importado(s)${skipped > 0 ? `, ${skipped} ignorado(s)` : ""}.`;

      if (errors && errors.length > 0) {
        setImportErrors(errors);
        setShowErrors(true);
        toast.warning(msg + " Veja os erros abaixo.");
      } else {
        toast.success(msg);
      }

      setSelectedFile(null);
      if (fileInputRef.current) fileInputRef.current.value = "";
      fetchData();
    } catch (err: any) {
      toast.error("Erro de conexão: " + err.message);
    } finally {
      setImporting(false);
    }
  };

  const handleClearAll = async () => {
    if (!window.confirm("Remover TODOS os apelidos desta empresa? Esta ação não pode ser desfeita.")) {
      return;
    }

    try {
      const res = await fetch("/api/config/filial-apelidos", {
        method: "DELETE",
        headers: authHeaders,
      });

      if (!res.ok) {
        const text = await res.text();
        toast.error("Erro ao limpar apelidos: " + text.slice(0, 100));
        return;
      }

      toast.success("Todos os apelidos foram removidos.");
      setData([]);
    } catch (err: any) {
      toast.error("Erro de conexão: " + err.message);
    }
  };

  return (
    <div className="container mx-auto p-4 space-y-6 max-w-3xl">
      <div className="flex items-center gap-2">
        <Tag className="h-6 w-6 text-primary" />
        <div>
          <h1 className="text-2xl font-bold">Apelidos de Filiais</h1>
          <p className="text-sm text-muted-foreground">
            Defina apelidos curtos para identificar cada filial nos filtros e tabelas do sistema.
          </p>
        </div>
      </div>

      {/* Upload CSV */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Importar CSV</CardTitle>
          <CardDescription>
            Arquivo delimitado por <code className="font-mono text-xs bg-muted px-1 rounded">;</code> com colunas{" "}
            <code className="font-mono text-xs bg-muted px-1 rounded">CNPJ;APELIDO</code>.
            Apelido: 2–20 caracteres. A linha de cabeçalho é opcional.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            <div className="flex flex-col gap-1">
              <p className="text-xs text-muted-foreground">Exemplo de conteúdo:</p>
              <pre className="text-xs bg-muted rounded p-2 font-mono">
                {`CNPJ;APELIDO\n10.230.480/0001-30;GUS\n10.230.480/0002-11;REC`}
              </pre>
            </div>
            <div className="flex items-center gap-3 flex-wrap">
              <input
                ref={fileInputRef}
                type="file"
                accept=".csv,.txt"
                onChange={handleFileChange}
                className="text-sm file:mr-3 file:py-1 file:px-3 file:rounded file:border-0 file:text-sm file:font-medium file:bg-primary file:text-primary-foreground hover:file:bg-primary/90 cursor-pointer"
              />
              <Button
                onClick={handleImport}
                disabled={!selectedFile || importing}
                size="sm"
              >
                <Upload className="h-4 w-4 mr-2" />
                {importing ? "Importando..." : "Importar"}
              </Button>
            </div>
            {selectedFile && (
              <p className="text-xs text-muted-foreground">
                Arquivo selecionado: <span className="font-medium">{selectedFile.name}</span>
              </p>
            )}
          </div>

          {/* Error panel */}
          {importErrors.length > 0 && (
            <div className="mt-4">
              <button
                onClick={() => setShowErrors((v) => !v)}
                className="text-sm text-yellow-700 underline"
              >
                {showErrors ? "Ocultar" : "Mostrar"} {importErrors.length} erro(s) de linha
              </button>
              {showErrors && (
                <ul className="mt-2 text-xs text-yellow-800 bg-yellow-50 border border-yellow-200 rounded p-3 space-y-1 max-h-40 overflow-y-auto">
                  {importErrors.map((e, i) => (
                    <li key={i}>{e}</li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Table */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-3">
          <CardTitle className="text-base">
            Apelidos cadastrados{data.length > 0 && <span className="text-muted-foreground font-normal ml-2">({data.length})</span>}
          </CardTitle>
          {data.length > 0 && (
            <Button variant="destructive" size="sm" onClick={handleClearAll}>
              <Trash2 className="h-4 w-4 mr-1" />
              Limpar Tudo
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="space-y-2">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-8 bg-muted animate-pulse rounded" />
              ))}
            </div>
          ) : data.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-6">
              Nenhum apelido cadastrado. Importe um arquivo CSV para começar.
            </p>
          ) : (
            <div className="rounded-md border overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>CNPJ</TableHead>
                    <TableHead>Apelido</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.map((row) => (
                    <TableRow key={row.cnpj}>
                      <TableCell className="font-mono text-sm">{formatCNPJ(row.cnpj)}</TableCell>
                      <TableCell className="font-medium">{row.apelido}</TableCell>
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
