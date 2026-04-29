import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { TrendingUp, RefreshCw, CheckCircle2, Info } from 'lucide-react';
import { Link } from 'react-router-dom';

interface RFBCreditoResumo {
  total_creditos: number;
  valor_cbs_total: number;
  valor_cbs_nao_extinto: number;
  total_corrente: number;
  total_ajuste: number;
  data_apuracao: string;
}

interface RFBCreditoRequest {
  id: string;
  cnpj_base: string;
  status: string;
  created_at: string;
  resumo?: RFBCreditoResumo;
}

function formatCNPJBase(cnpj: string): string {
  if (cnpj.length === 8) return `${cnpj.slice(0, 2)}.${cnpj.slice(2, 5)}.${cnpj.slice(5)}`;
  return cnpj;
}

function formatPeriodo(p: string): string {
  if (p && p.length === 6) return `${p.slice(4, 6)}/${p.slice(0, 4)}`;
  return p || '—';
}

function formatNumber(n: number): string {
  return new Intl.NumberFormat('pt-BR').format(n);
}

function formatCurrency(value: number): string {
  return new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' }).format(value);
}

export default function RFBCreditosCBS() {
  const [requests, setRequests] = useState<RFBCreditoRequest[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchRequests = useCallback(async () => {
    try {
      const token = localStorage.getItem('token');
      const companyId = localStorage.getItem('companyId');
      const response = await fetch('/api/rfb/creditos/status', {
        headers: { 'Authorization': `Bearer ${token}`, 'X-Company-ID': companyId || '' },
      });
      if (response.ok) {
        const data = await response.json();
        setRequests(data.requests || []);
      }
    } catch {
      // silent
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchRequests(); }, [fetchRequests]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary"></div>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto px-4 py-6">
      <div className="md:flex md:items-center md:justify-between mb-6">
        <div>
          <h2 className="text-2xl font-bold flex items-center gap-2">
            <TrendingUp className="h-6 w-6" />
            Créditos CBS
          </h2>
          <p className="mt-1 text-sm text-gray-600">
            Créditos CBS extraídos automaticamente das importações de débitos da Receita Federal.
          </p>
        </div>
        <Button variant="outline" onClick={fetchRequests}>
          <RefreshCw className="mr-2 h-4 w-4" /> Atualizar
        </Button>
      </div>

      <div className="mb-4 flex items-start gap-3 rounded-md border border-blue-200 bg-blue-50 px-4 py-3 text-blue-800">
        <Info className="h-4 w-4 shrink-0 mt-0.5" />
        <p className="text-sm">
          A Receita Federal retorna créditos e débitos no mesmo arquivo de apuração CBS.
          Os créditos são extraídos automaticamente em cada importação. Para solicitar uma nova
          importação, acesse{' '}
          <Link to="/rfb/apuracao" className="font-semibold underline hover:no-underline">
            Importar Débitos
          </Link>.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Créditos por Importação</CardTitle>
          <CardDescription>
            Resumo de créditos CBS encontrados em cada importação concluída.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {requests.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">
              <TrendingUp className="mx-auto h-12 w-12 mb-3 opacity-30" />
              <p>Nenhuma importação com créditos CBS encontrada.</p>
              <p className="text-xs mt-1">
                Os créditos aparecem após a conclusão de uma{' '}
                <Link to="/rfb/apuracao" className="text-primary hover:underline">
                  importação de débitos
                </Link>.
              </p>
            </div>
          ) : (
            <div className="space-y-4">
              {requests.map((req) => {
                const { resumo } = req;
                const hasCredits = resumo && resumo.total_creditos > 0;

                return (
                  <div key={req.id} className="rounded-lg border overflow-hidden">
                    <div className="flex items-center justify-between p-4">
                      <div className="flex items-center gap-3">
                        <CheckCircle2 className="h-5 w-5 text-green-600 shrink-0" />
                        <div>
                          <span className="font-medium text-sm">CNPJ: {formatCNPJBase(req.cnpj_base)}</span>
                          <p className="text-xs text-muted-foreground mt-0.5">
                            Importado em {new Date(req.created_at).toLocaleString('pt-BR')}
                          </p>
                        </div>
                      </div>
                      {!hasCredits && (
                        <span className="text-xs text-muted-foreground italic">
                          Sem créditos nesta importação
                        </span>
                      )}
                    </div>

                    {hasCredits && resumo && (
                      <div className="border-t bg-gray-50 px-4 py-3 grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
                        <div>
                          <span className="text-xs text-muted-foreground block">Período</span>
                          <span className="font-semibold">{formatPeriodo(resumo.data_apuracao)}</span>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground block">Total de créditos</span>
                          <span className="font-semibold">{formatNumber(resumo.total_creditos)}</span>
                          <span className="text-xs text-muted-foreground ml-1">
                            ({formatNumber(resumo.total_corrente)} corr. + {formatNumber(resumo.total_ajuste)} ajuste)
                          </span>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground block">CBS Total</span>
                          <span className="font-semibold text-green-700">{formatCurrency(resumo.valor_cbs_total)}</span>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground block">CBS Não Extinto</span>
                          <span className="font-semibold text-blue-700">{formatCurrency(resumo.valor_cbs_nao_extinto)}</span>
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
