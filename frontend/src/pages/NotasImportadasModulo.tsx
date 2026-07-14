import { useState, useCallback } from 'react';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { ArrowDownToLine, ArrowUpFromLine } from 'lucide-react';
import ConsultaNFesEntradas from './ConsultaNFesEntradas';
import ConsultaNFeSaidas from './ConsultaNFeSaidas';
import ConsultaCTesEntradas from './ConsultaCTesEntradas';

type Direcao = 'entrada' | 'saida';

interface TipoDocumento {
  key: string;
  label: string;
  enabled: boolean;
}

const TIPOS: TipoDocumento[] = [
  { key: 'nfe',     label: 'Nota Fiscal Eletrônica (NF-e)',     enabled: true },
  { key: 'nfce',    label: 'Nota Fiscal Consumidor (NFC-e)',    enabled: false },
  { key: 'nfse',    label: 'Nota Fiscal de Serviços (NFS-e)',   enabled: false },
  { key: 'cte',     label: 'Conhecimento de Frete (CT-e)',     enabled: true },
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

const CONSULTA_POR_TIPO: Record<string, React.ComponentType | undefined> = {
  'nfe-entrada': ConsultaNFesEntradas,
  'nfe-saida': ConsultaNFeSaidas,
  'cte-entrada': ConsultaCTesEntradas,
};

const DIRECOES_DISPONIVEIS: Record<string, Direcao[]> = {
  nfe: ['entrada', 'saida'],
  cte: ['entrada'],
};

interface NotasImportadasModuloProps {
  tipoInicial?: string;
  direcaoInicial?: Direcao;
}

export default function NotasImportadasModulo({ tipoInicial = 'nfe', direcaoInicial = 'entrada' }: NotasImportadasModuloProps) {
  const [tipoAtivo, setTipoAtivo] = useState(tipoInicial);
  const [direcao, setDirecao] = useState<Direcao>(() => {
    const direcoes = DIRECOES_DISPONIVEIS[tipoInicial] ?? [];
    return direcoes.includes(direcaoInicial) ? direcaoInicial : (direcoes[0] ?? 'entrada');
  });

  const direcoesDoTipo = DIRECOES_DISPONIVEIS[tipoAtivo] ?? [];
  const Consulta = CONSULTA_POR_TIPO[`${tipoAtivo}-${direcao}`];

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
            {!tipo.enabled && <Badge variant="secondary" className="text-[10px] shrink-0">Em breve</Badge>}
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
                  {!disponivel && <Badge variant="secondary" className="ml-2 text-[10px]">Em breve</Badge>}
                </Button>
              );
            })}
          </div>
        )}

        {Consulta ? (
          <Consulta />
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
