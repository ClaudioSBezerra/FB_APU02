import { useEffect, useState } from "react";
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
import { Skeleton } from "@/components/ui/skeleton";

interface TaxRate {
  ano: number;
  perc_ibs_uf: number;
  perc_ibs_mun: number;
  perc_cbs: number;
  perc_reduc_icms: number;
  perc_reduc_piscofins: number;
}

export default function TabelaAliquotas() {
  const [rates, setRates] = useState<TaxRate[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch("/api/config/aliquotas")
      .then((res) => res.json())
      .then((data) => {
        setRates(data || []);
        setLoading(false);
      })
      .catch((err) => {
        console.error("Failed to fetch tax rates", err);
        setLoading(false);
      });
  }, []);

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="space-y-2">
            <Skeleton className="h-8 w-[300px]" />
            <Skeleton className="h-4 w-[500px]" />
        </div>
        <div className="border rounded-md p-4">
            <div className="space-y-4">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
            </div>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Tabela de Alíquotas</h1>
        <p className="text-muted-foreground">
          Cronograma de transição tributária e alíquotas projetadas (2027-2033).
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Alíquotas de Referência</CardTitle>
          <CardDescription>
            Percentuais utilizados para o cálculo dos impostos projetados (IBS, CBS) e reduções graduais.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[100px]">Ano</TableHead>
                  <TableHead className="text-right">IBS (UF) %</TableHead>
                  <TableHead className="text-right">IBS (Mun) %</TableHead>
                  <TableHead className="text-right font-bold text-blue-600">IBS Total %</TableHead>
                  <TableHead className="text-right">CBS %</TableHead>
                  <TableHead className="text-right">Redução ICMS %</TableHead>
                  <TableHead className="text-right">Redução PIS/COFINS %</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rates.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} className="h-24 text-center">
                      Nenhuma alíquota encontrada.
                    </TableCell>
                  </TableRow>
                ) : (
                  rates.map((rate) => (
                    <TableRow key={rate.ano}>
                      <TableCell className="font-medium">{rate.ano}</TableCell>
                      <TableCell className="text-right">{rate.perc_ibs_uf.toFixed(2)}%</TableCell>
                      <TableCell className="text-right">{rate.perc_ibs_mun.toFixed(2)}%</TableCell>
                      <TableCell className="text-right font-bold text-blue-600">
                          {(rate.perc_ibs_uf + rate.perc_ibs_mun).toFixed(2)}%
                      </TableCell>
                      <TableCell className="text-right">{rate.perc_cbs.toFixed(2)}%</TableCell>
                      <TableCell className="text-right text-red-500">-{rate.perc_reduc_icms.toFixed(2)}%</TableCell>
                      <TableCell className="text-right text-red-500">-{rate.perc_reduc_piscofins.toFixed(2)}%</TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}