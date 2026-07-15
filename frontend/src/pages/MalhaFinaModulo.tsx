import { useState, useCallback } from 'react';
import { Card, CardContent } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { ArrowDownToLine, ArrowUpFromLine } from 'lucide-react';
import MalhaFinaPanel, { MalhaFinaTipo } from './MalhaFinaPanel';

type Direcao = 'entrada' | 'saida';

interface TipoDocumento {
  key: string;
  label: string;
  enabled: boolean;
}

const TIPOS: TipoDocumento[] = [
  { key: 'nfe',     label: 'Nota Fiscal Eletrônica (NF-e)',     enabled: true },
  { key: 'cte',     label: 'Conhecimento de Frete (CT-e)',     enabled: true },
  { key: 'nfse',    label: 'Nota Fiscal de Serviços (NFS-e)',   enabled: false },
  { key: 'bpe',     label: 'Bilhete de Passagem (BP-e)',        enabled: false },
  { key: 'nf3e',    label: 'NF de Energia Elétrica (NF3e)',     enabled: false },
  { key: 'nfcom',   label: 'NF de Comunicação (NFCom)',         enabled: false },
  { key: 'nfage',   label: 'NF Agrícola (NFag-e)',              enabled: false },
  { key: 'nfeabi',  label: 'NF-e ABI',                          enabled: false },
  { key: 'nde',     label: 'Nota de Débito (ND-e)',             enabled: false },
  { key: 'nce',     label: 'Nota de Crédito (NC-e)',            enabled: false },
];

interface PanelConfig {
  tipo: MalhaFinaTipo;
  title: string;
  description: string;
  rfbDisponivel: boolean;
}

const PANEL_CONFIG: Record<string, PanelConfig | undefined> = {
  'nfe-entrada': {
    tipo: 'nfe-entradas',
    title: 'Malha Fina — NF-e Entradas',
    description: 'NF-e (mod. 55/65) identificadas pela Receita Federal que não foram importadas como entradas.',
    rfbDisponivel: false,
  },
  'nfe-saida': {
    tipo: 'nfe-saidas',
    title: 'Malha Fina — NF-e Saídas',
    description: 'NF-e (mod. 55/65) identificadas pela Receita Federal que não foram importadas como saídas.',
    rfbDisponivel: true,
  },
  'cte-entrada': {
    tipo: 'cte',
    title: 'Malha Fina — CT-e',
    description: 'CT-e (mod. 57) identificados pela Receita Federal que não foram importados nos registros da empresa.',
    rfbDisponivel: false,
  },
};

const DIRECOES_DISPONIVEIS: Record<string, Direcao[]> = {
  nfe: ['entrada', 'saida'],
  cte: ['entrada'],
};

interface MalhaFinaModuloProps {
  tipoInicial?: 'nfe' | 'cte';
  direcaoInicial?: Direcao;
}

export default function MalhaFinaModulo({ tipoInicial = 'nfe', direcaoInicial = 'entrada' }: MalhaFinaModuloProps) {
  const [tipoAtivo, setTipoAtivo] = useState<string>(tipoInicial);
  const [direcao, setDirecao] = useState<Direcao>(() => {
    const direcoes = DIRECOES_DISPONIVEIS[tipoInicial] ?? [];
    return direcoes.includes(direcaoInicial) ? direcaoInicial : (direcoes[0] ?? 'entrada');
  });

  const direcoesDoTipo = DIRECOES_DISPONIVEIS[tipoAtivo] ?? [];
  const painelConfig = PANEL_CONFIG[`${tipoAtivo}-${direcao}`];

  const selecionarTipo = useCallback((tipo: TipoDocumento) => {
    if (!tipo.enabled) return;
    setTipoAtivo(tipo.key);
    const direcoes = DIRECOES_DISPONIVEIS[tipo.key] ?? [];
    setDirecao(direcoes[0] ?? 'entrada');
  }, []);

  const selecionarDirecao = useCallback((d: Direcao) => {
    setDirecao(d);
  }, []);

  return (
    <div className="flex gap-6">
      <aside className="w-64 shrink-0 space-y-1">
        {TIPOS.map(tipo => (
          <button
            key={tipo.key}
            onClick={() => selecionarTipo(tipo)}
            disabled={!tipo.enabled}
            className={cn(
              'w-full text-left px-3 py-2 rounded-md text-sm transition-colors flex items-center justify-between gap-2',
              !tipo.enabled && 'text-muted-foreground/50 cursor-not-allowed',
              tipo.enabled && tipoAtivo === tipo.key && 'bg-primary/10 text-primary font-semibold',
              tipo.enabled && tipoAtivo !== tipo.key && 'hover:bg-gray-100 text-foreground',
            )}
          >
            <span>{tipo.label}</span>
          </button>
        ))}
      </aside>

      <div className="flex-1 min-w-0 space-y-6">
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
                  disabled={!disponivel}
                  onClick={() => selecionarDirecao(d)}
                >
                  <Icon className="h-4 w-4 mr-2" />
                  {d === 'entrada' ? 'Entrada' : 'Saída'}
                </Button>
              );
            })}
          </div>
        )}

        {painelConfig ? (
          <MalhaFinaPanel
            key={`${tipoAtivo}-${direcao}`}
            tipo={painelConfig.tipo}
            title={painelConfig.title}
            description={painelConfig.description}
            rfbDisponivel={painelConfig.rfbDisponivel}
          />
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
