import React, { useEffect, useState } from "react";
// Tabela Fornecedores Simples Nacional
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import { Skeleton } from "@/components/ui/skeleton";
import { Trash2, Plus } from "lucide-react";

interface FornSimples {
  cnpj: string;
}

export default function TabelaFornSimples() {
  const [data, setData] = useState<FornSimples[]>([]);
  const [loading, setLoading] = useState(true);
  const [uploading, setUploading] = useState(false);
  const [newCnpj, setNewCnpj] = useState("");

  const fetchData = () => {
    setLoading(true);
    fetch("/api/config/forn-simples")
      .then(async (res) => {
        if (!res.ok) {
            const text = await res.text();
            throw new Error(`Erro ${res.status}: ${text.slice(0, 50)}...`);
        }
        return res.json();
      })
      .then((data) => {
        setData(data || []);
        setLoading(false);
      })
      .catch((err) => {
        console.error("Failed to fetch FornSimples", err);
        toast.error("Erro ao carregar tabela: " + err.message);
        setLoading(false);
      });
  };

  useEffect(() => {
    fetchData();
  }, []);

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (!e.target.files || e.target.files.length === 0) return;
    
    const file = e.target.files[0];
    const formData = new FormData();
    formData.append("file", file);

    setUploading(true);
    try {
      const res = await fetch("/api/config/forn-simples/import", {
        method: "POST",
        body: formData,
      });
      
      if (!res.ok) {
        let errorMsg = "Falha na importação";
        try {
          const text = await res.text();
          try {
            const errData = JSON.parse(text);
            errorMsg = errData.error || errData.message || errorMsg;
          } catch {
            if (text) errorMsg = text;
          }
        } catch (e) {
            console.error(e);
        }
        throw new Error(errorMsg);
      }
      
      toast.success("Importação concluída com sucesso!");
      fetchData();
    } catch (error: any) {
      console.error(error);
      toast.error(`Erro ao importar arquivo CSV: ${error.message}`);
    } finally {
        setUploading(false);
        e.target.value = "";
    }
  };

  const handleAdd = async () => {
      if (!newCnpj) return;
      try {
          const res = await fetch("/api/config/forn-simples", {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({ cnpj: newCnpj }),
          });
          if (!res.ok) {
              const text = await res.text();
              throw new Error(text);
          }
          toast.success("CNPJ adicionado com sucesso!");
          setNewCnpj("");
          fetchData();
      } catch (err: any) {
          toast.error("Erro ao adicionar CNPJ: " + err.message);
      }
  };

  const handleDelete = async (cnpj: string) => {
      if (!confirm(`Deseja remover o CNPJ ${cnpj}?`)) return;
      try {
          const res = await fetch(`/api/config/forn-simples?cnpj=${cnpj}`, {
              method: "DELETE",
          });
          if (!res.ok) {
              const text = await res.text();
              throw new Error(text);
          }
          toast.success("CNPJ removido com sucesso!");
          fetchData();
      } catch (err: any) {
          toast.error("Erro ao remover CNPJ: " + err.message);
      }
  };

  return (
    <div className="container mx-auto p-2 md:p-4 space-y-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-lg md:text-xl lg:text-2xl">Fornecedores Simples Nacional</CardTitle>
          <CardDescription className="text-[10px] md:text-sm">
            Gerencie os CNPJs de fornecedores do Simples Nacional. Importe via CSV (coluna única: CNPJ, delimitador ';').
          </CardDescription>
        </CardHeader>
        <CardContent>
            <div className="flex flex-col gap-6 mb-6">
                <div className="flex items-end gap-4">
                    <div className="grid w-full max-w-sm items-center gap-1.5">
                        <Label htmlFor="cnpj_input" className="text-[10px]">Adicionar CNPJ Manualmente</Label>
                        <Input 
                            id="cnpj_input" 
                            placeholder="00.000.000/0000-00" 
                            value={newCnpj} 
                            onChange={(e) => setNewCnpj(e.target.value)} 
                            className="h-8"
                        />
                    </div>
                    <Button onClick={handleAdd} disabled={!newCnpj}>
                        <Plus className="mr-2 h-4 w-4" /> Adicionar
                    </Button>
                </div>

                <div className="grid w-full max-w-sm items-center gap-1.5">
                    <Label htmlFor="csv_upload" className="text-[10px]">Importar CSV (Lista de CNPJs)</Label>
                    <Input id="csv_upload" type="file" accept=".csv" onChange={handleFileUpload} disabled={uploading} />
                </div>
            </div>

            {loading ? (
                 <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
                    <Skeleton className="h-10 w-full" />
                    <Skeleton className="h-10 w-full" />
                    <Skeleton className="h-10 w-full" />
                    <Skeleton className="h-10 w-full" />
                 </div>
            ) : (
                <>
                {data.length === 0 ? (
                    <div className="text-center text-muted-foreground p-8 border rounded-md">
                        Nenhum registro encontrado.
                    </div>
                ) : (
                    <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
                        {data.map((item) => (
                            <div key={item.cnpj} className="flex items-center justify-between p-2 border rounded-md bg-card text-card-foreground shadow-sm hover:bg-accent/50 transition-colors">
                                <span className="font-medium font-mono text-[10px] pl-2">{item.cnpj}</span>
                                <Button variant="ghost" size="icon" onClick={() => handleDelete(item.cnpj)} className="h-6 w-6 text-muted-foreground hover:text-red-500">
                                    <Trash2 className="h-3 w-3" />
                                </Button>
                            </div>
                        ))}
                    </div>
                )}
                </>
            )}
        </CardContent>
      </Card>
    </div>
  );
}
