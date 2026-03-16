import { useState } from 'react';
import { AlertTriangle, Trash2, CheckCircle2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { toast } from 'sonner';

interface LimparResult {
  nfe_saidas: number;
  nfe_entradas: number;
  cte_entradas: number;
  dfe_xml: number;
}

function fmtN(n: number) {
  return new Intl.NumberFormat('pt-BR').format(n);
}

const CONFIRM_WORD = 'LIMPAR';

const DATA_ITEMS = [
  { label: 'NF-e Saídas',      desc: 'Notas fiscais de saída importadas' },
  { label: 'NF-e Entradas',    desc: 'Notas fiscais de entrada importadas' },
  { label: 'CT-e Entradas',    desc: 'Conhecimentos de transporte importados' },
  { label: 'XMLs armazenados', desc: 'Arquivos XML brutos (dfe_xml)' },
];

export default function LimparDadosApuracao() {
  const [confirmText, setConfirmText] = useState('');
  const [loading,     setLoading]     = useState(false);
  const [result,      setResult]      = useState<LimparResult | null>(null);

  const confirmed = confirmText === CONFIRM_WORD;

  async function handleLimpar() {
    if (!confirmed) return;
    setLoading(true);
    try {
      const res = await fetch('/api/admin/limpar-apuracao', {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${localStorage.getItem('token')}`,
          'X-Company-ID':  localStorage.getItem('companyId') || '',
        },
      });
      if (!res.ok) {
        const msg = await res.text();
        toast.error('Erro ao limpar dados: ' + msg);
        return;
      }
      const data = await res.json();
      setResult(data.totals);
      setConfirmText('');
      toast.success('Dados de apuração removidos com sucesso.');
    } catch {
      toast.error('Erro de conexão ao tentar limpar os dados.');
    } finally {
      setLoading(false);
    }
  }

  if (result) {
    const total = Object.values(result).reduce((a, b) => a + b, 0);
    return (
      <div className="max-w-lg mx-auto mt-8 space-y-4">
        <div className="flex items-center gap-3 p-4 rounded-lg border border-green-200 bg-green-50">
          <CheckCircle2 className="h-6 w-6 text-green-600 shrink-0" />
          <div>
            <p className="font-semibold text-green-800">Limpeza concluída</p>
            <p className="text-sm text-green-700">{fmtN(total)} registros removidos no total.</p>
          </div>
        </div>

        <div className="border rounded-lg divide-y text-sm">
          {[
            { label: 'NF-e Saídas',      value: result.nfe_saidas },
            { label: 'NF-e Entradas',    value: result.nfe_entradas },
            { label: 'CT-e Entradas',    value: result.cte_entradas },
            { label: 'XMLs armazenados', value: result.dfe_xml },
          ].map(row => (
            <div key={row.label} className="flex justify-between items-center px-4 py-2">
              <span className="text-muted-foreground">{row.label}</span>
              <span className="font-mono font-medium">{fmtN(row.value)}</span>
            </div>
          ))}
        </div>

        <Button variant="outline" className="w-full" onClick={() => setResult(null)}>
          Fechar
        </Button>
      </div>
    );
  }

  return (
    <div className="max-w-lg mx-auto mt-8 space-y-6">

      {/* Aviso */}
      <div className="flex gap-3 p-4 rounded-lg border border-red-200 bg-red-50">
        <AlertTriangle className="h-5 w-5 text-red-600 shrink-0 mt-0.5" />
        <div className="space-y-1">
          <p className="font-semibold text-red-800 text-sm">Ação irreversível</p>
          <p className="text-sm text-red-700">
            Esta operação remove <strong>permanentemente</strong> todos os dados de apuração
            IBS/CBS da empresa ativa. Use apenas para limpar dados de teste antes de iniciar
            a operação com dados reais.
          </p>
        </div>
      </div>

      {/* O que será apagado */}
      <div>
        <p className="text-sm font-medium text-muted-foreground uppercase tracking-wide mb-2">
          Dados que serão removidos
        </p>
        <div className="border rounded-lg divide-y">
          {DATA_ITEMS.map(item => (
            <div key={item.label} className="flex items-start gap-3 px-4 py-2.5">
              <Trash2 className="h-3.5 w-3.5 text-red-400 mt-0.5 shrink-0" />
              <div>
                <p className="text-sm font-medium">{item.label}</p>
                <p className="text-xs text-muted-foreground">{item.desc}</p>
              </div>
            </div>
          ))}
        </div>
        <p className="text-xs text-muted-foreground mt-2">
          Importações da RFB (débitos CBS), configurações (alíquotas, CFOP, credenciais) e usuários <strong>não</strong> serão afetados.
        </p>
      </div>

      {/* Confirmação */}
      <div className="space-y-2">
        <Label className="text-sm">
          Para confirmar, digite <span className="font-mono font-bold text-red-600">{CONFIRM_WORD}</span> abaixo:
        </Label>
        <Input
          value={confirmText}
          onChange={e => setConfirmText(e.target.value.toUpperCase())}
          placeholder={CONFIRM_WORD}
          className="font-mono"
          disabled={loading}
        />
      </div>

      <Button
        variant="destructive"
        className="w-full gap-2"
        disabled={!confirmed || loading}
        onClick={handleLimpar}
      >
        <Trash2 className="h-4 w-4" />
        {loading ? 'Removendo dados...' : 'Limpar todos os dados de apuração'}
      </Button>
    </div>
  );
}
