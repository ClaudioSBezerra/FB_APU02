import { useState, useEffect } from 'react';
import { useAuth } from '@/contexts/AuthContext';
import { Card, CardContent } from '@/components/ui/card';
import { Loader2, Lightbulb, AlertTriangle, TrendingUp, ArrowRight } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Link } from 'react-router-dom';

interface InsightData {
  texto: string;
  tipo: 'alerta' | 'info' | 'positivo';
  acao_url?: string;
  acao_text?: string;
}

const tipoConfig = {
  alerta: {
    icon: AlertTriangle,
    bg: 'bg-amber-50 border-amber-200',
    iconColor: 'text-amber-600',
    textColor: 'text-amber-900',
  },
  info: {
    icon: Lightbulb,
    bg: 'bg-blue-50 border-blue-200',
    iconColor: 'text-blue-600',
    textColor: 'text-blue-900',
  },
  positivo: {
    icon: TrendingUp,
    bg: 'bg-emerald-50 border-emerald-200',
    iconColor: 'text-emerald-600',
    textColor: 'text-emerald-900',
  },
};

export function InsightCard() {
  const { token, companyId } = useAuth();
  const [insight, setInsight] = useState<InsightData | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchInsight = async () => {
      setLoading(true);
      try {
        const headers: Record<string, string> = {
          Authorization: `Bearer ${token || localStorage.getItem('token')}`,
        };
        if (companyId) {
          headers['X-Company-ID'] = companyId;
        }
        const response = await fetch('/api/insights/daily', { headers });
        if (response.ok) {
          const data = await response.json();
          setInsight(data);
        }
      } catch (error) {
        console.error('Error fetching insight:', error);
      } finally {
        setLoading(false);
      }
    };

    fetchInsight();
  }, [token, companyId]);

  if (loading) {
    return (
      <Card className="border bg-muted/30">
        <CardContent className="flex items-center gap-3 py-3 px-4">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
          <span className="text-sm text-muted-foreground">Carregando insight...</span>
        </CardContent>
      </Card>
    );
  }

  if (!insight) return null;

  const config = tipoConfig[insight.tipo] || tipoConfig.info;
  const Icon = config.icon;

  return (
    <Card className={`border ${config.bg}`}>
      <CardContent className="flex items-center gap-3 py-3 px-4">
        <Icon className={`h-5 w-5 shrink-0 ${config.iconColor}`} />
        <p className={`text-sm flex-1 ${config.textColor}`}>
          {insight.texto}
        </p>
        {insight.acao_url && (
          <Link to={insight.acao_url}>
            <Button variant="ghost" size="sm" className="shrink-0 h-7 text-xs gap-1">
              {insight.acao_text || 'Ver mais'}
              <ArrowRight className="h-3 w-3" />
            </Button>
          </Link>
        )}
      </CardContent>
    </Card>
  );
}
