import { useCallback, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import { formatCNPJ } from '@/lib/formatFilial';

interface Parceiro {
  cnpj: string;
  nome: string;
  aderiu_split_payment: boolean;
  data_adesao_split_payment: string | null;
}

function formatDate(dateStr: string | null): string {
  if (!dateStr) return '—';
  const parts = dateStr.split('-');
  if (parts.length === 3) return `${parts[2]}/${parts[1]}/${parts[0]}`;
  return dateStr;
}

export default function CadastroFornecedores() {
  const [items, setItems] = useState<Parceiro[]>([]);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize] = useState(50);
  const [total, setTotal] = useState(0);

  const [filterCnpj, setFilterCnpj] = useState('');
  const [filterNome, setFilterNome] = useState('');
  const [appliedFilters, setAppliedFilters] = useState({ cnpj: '', nome: '' });

  const [savingCnpj, setSavingCnpj] = useState<string | null>(null);

  const fetchParceiros = useCallback(async (currentPage: number, filters: typeof appliedFilters) => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (filters.cnpj) params.set('cnpj', filters.cnpj);
      if (filters.nome) params.set('nome', filters.nome);
      params.set('page', String(currentPage));
      params.set('page_size', String(pageSize));

      const res = await fetch(`/api/parceiros?${params.toString()}`);
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json() as { items: Parceiro[]; total: number };
      setItems(data.items ?? []);
      setTotal(data.total ?? 0);
    } catch (err: unknown) {
      toast.error('Erro ao carregar fornecedores: ' + String(err));
    } finally {
      setLoading(false);
    }
  }, [pageSize]);

  useEffect(() => {
    fetchParceiros(page, appliedFilters);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, appliedFilters]);

  const handleApplyFilters = () => {
    setPage(1);
    setAppliedFilters({ cnpj: filterCnpj, nome: filterNome });
  };

  const handleClearFilters = () => {
    setFilterCnpj('');
    setFilterNome('');
    setPage(1);
    setAppliedFilters({ cnpj: '', nome: '' });
  };

  const handleToggleSplitPayment = async (parceiro: Parceiro, checked: boolean) => {
    setSavingCnpj(parceiro.cnpj);
    try {
      const res = await fetch('/api/parceiros/split-payment', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ cnpj: parceiro.cnpj, aderiu_split_payment: checked }),
      });
      const data = await res.json() as { data_adesao_split_payment?: string | null; error?: string };
      if (!res.ok) {
        toast.error('Erro ao atualizar adesão: ' + (data.error ?? res.statusText));
        return;
      }
      setItems(prev => prev.map(p => p.cnpj === parceiro.cnpj
        ? { ...p, aderiu_split_payment: checked, data_adesao_split_payment: data.data_adesao_split_payment ?? p.data_adesao_split_payment }
        : p));
      toast.success('Adesão atualizada.');
    } catch (err: unknown) {
      toast.error('Erro ao atualizar adesão: ' + String(err));
    } finally {
      setSavingCnpj(null);
    }
  };

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Cadastro de Fornecedores</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Consulte os fornecedores/clientes sincronizados via ERP Bridge e registre a adesão
          voluntária ao Split Payment (Fase 1, Reforma Tributária).
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Filtros</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap gap-3 items-end">
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">CNPJ</label>
              <input
                type="text"
                placeholder="00.000.000/0000-00"
                value={filterCnpj}
                onChange={e => setFilterCnpj(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleApplyFilters()}
                className="border rounded-md px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring w-48"
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground">Nome</label>
              <input
                type="text"
                placeholder="Buscar por nome..."
                value={filterNome}
                onChange={e => setFilterNome(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleApplyFilters()}
                className="border rounded-md px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring w-64"
              />
            </div>
            <div className="flex gap-2 pt-4">
              <Button size="sm" onClick={handleApplyFilters}>Buscar</Button>
              <Button size="sm" variant="outline" onClick={handleClearFilters}>Limpar</Button>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">
            Fornecedores
            {total > 0 && (
              <span className="ml-2 text-sm font-normal text-muted-foreground">
                ({total} registro{total !== 1 ? 's' : ''})
              </span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <p className="text-sm text-muted-foreground">Carregando...</p>
          ) : items.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Nenhum fornecedor encontrado para os filtros selecionados.
            </p>
          ) : (
            <div className="space-y-3">
              <div className="overflow-x-auto">
                <table className="w-full text-xs border-collapse">
                  <thead>
                    <tr className="border-b bg-muted/50">
                      <th className="text-left px-2 py-2 font-medium">CNPJ</th>
                      <th className="text-left px-2 py-2 font-medium">Nome</th>
                      <th className="text-left px-2 py-2 font-medium">Aderiu Split Payment</th>
                      <th className="text-left px-2 py-2 font-medium">Data Adesão</th>
                    </tr>
                  </thead>
                  <tbody>
                    {items.map(item => (
                      <tr key={item.cnpj} className="border-b hover:bg-muted/30">
                        <td className="px-2 py-1.5 whitespace-nowrap font-mono">{formatCNPJ(item.cnpj)}</td>
                        <td className="px-2 py-1.5">{item.nome || <span className="text-muted-foreground italic">—</span>}</td>
                        <td className="px-2 py-1.5">
                          <Switch
                            checked={item.aderiu_split_payment}
                            disabled={savingCnpj === item.cnpj}
                            onCheckedChange={checked => handleToggleSplitPayment(item, checked)}
                          />
                        </td>
                        <td className="px-2 py-1.5 whitespace-nowrap">{formatDate(item.data_adesao_split_payment)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

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
    </div>
  );
}
