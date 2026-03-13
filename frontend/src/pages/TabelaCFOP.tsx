import React, { useEffect, useState } from "react";
// Tabela CFOP
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toast } from "sonner";
import { Skeleton } from "@/components/ui/skeleton";

interface CFOP {
  cfop: string;
  descricao_cfop: string;
  tipo: string;
}

export default function TabelaCFOP() {
  const [cfops, setCfops] = useState<CFOP[]>([]);
  const [loading, setLoading] = useState(true);
  const [uploading, setUploading] = useState(false);

  const fetchCFOPs = () => {
    setLoading(true);
    fetch("/api/config/cfop")
      .then(async (res) => {
        if (!res.ok) {
            const text = await res.text();
            throw new Error(`Erro ${res.status}: ${text.slice(0, 50)}...`);
        }
        return res.json();
      })
      .then((data) => {
        setCfops(data || []);
        setLoading(false);
      })
      .catch((err) => {
        console.error("Failed to fetch CFOPs", err);
        toast.error("Erro ao carregar tabela CFOP: " + err.message);
        setLoading(false);
      });
  };

  useEffect(() => {
    fetchCFOPs();
  }, []);

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (!e.target.files || e.target.files.length === 0) return;
    
    const file = e.target.files[0];
    const formData = new FormData();
    formData.append("file", file);

    setUploading(true);
    try {
      const res = await fetch("/api/config/cfop/import", {
        method: "POST",
        body: formData,
      });
      
      if (!res.ok) {
        let errorMsg = "Falha na importação";
        try {
          const text = await res.text();
          console.log("Server response:", text);
          try {
            const errData = JSON.parse(text);
            errorMsg = errData.error || errorMsg;
          } catch {
            if (text) errorMsg = text;
          }
        } catch (e) {
          console.error("Error reading error response:", e);
        }
        throw new Error(errorMsg);
      }
      
      toast.success("Importação concluída com sucesso!");
      fetchCFOPs();
    } catch (error: any) {
      console.error(error);
      toast.error(`Erro ao importar arquivo CSV: ${error.message}`);
    } finally {
        setUploading(false);
        e.target.value = "";
    }
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Tabela CFOP</CardTitle>
          <CardDescription>
            Gerencie os Códigos Fiscais de Operações e Prestações. Importe via CSV (CFOP;Descrição;Tipo).
          </CardDescription>
        </CardHeader>
        <CardContent>
            <div className="flex items-center gap-4 mb-6">
                <div className="grid w-full max-w-sm items-center gap-1.5">
                    <Label htmlFor="csv_upload">Importar CSV</Label>
                    <Input id="csv_upload" type="file" accept=".csv" onChange={handleFileUpload} disabled={uploading} />
                </div>
            </div>

            {loading ? (
                 <div className="space-y-2">
                    <Skeleton className="h-8 w-full" />
                    <Skeleton className="h-8 w-full" />
                    <Skeleton className="h-8 w-full" />
                 </div>
            ) : (
                <div className="rounded-md border">
                <Table>
                    <TableHeader>
                    <TableRow>
                        <TableHead className="w-[100px]">CFOP</TableHead>
                        <TableHead>Descrição</TableHead>
                        <TableHead className="w-[100px]">Tipo</TableHead>
                    </TableRow>
                    </TableHeader>
                    <TableBody>
                    {cfops.length === 0 ? (
                        <TableRow>
                            <TableCell colSpan={3} className="text-center">Nenhum registro encontrado.</TableCell>
                        </TableRow>
                    ) : (
                        cfops.map((c) => (
                            <TableRow key={c.cfop} className="h-8">
                            <TableCell className="font-medium py-1">{c.cfop}</TableCell>
                            <TableCell className="py-1">{c.descricao_cfop}</TableCell>
                            <TableCell className="py-1">{c.tipo}</TableCell>
                            </TableRow>
                        ))
                    )}
                    </TableBody>
                </Table>
                </div>
            )}
        </CardContent>
      </Card>
    </div>
  );
}