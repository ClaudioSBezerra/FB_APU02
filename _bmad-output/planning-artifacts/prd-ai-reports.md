---
stepsCompleted: [1, 2, 3]
inputDocuments:
  - _bmad-output/planning-artifacts/prd-multi-tenancy.md
  - docs/FASE_01_DOCUMENTACAO_COMPLETA.md
  - docs/ARCHITECTURE.md
workflowType: 'prd'
---

# Product Requirements Document - Relatorios com Narrativa IA

**Author:** Claudio Bezerra
**Date:** 2026-02-12
**Version:** 1.0
**Phase:** FASE 2 - AI Reports MVP

---

## 1. Visao do Produto

### 1.1 Problema

Contadores e analistas fiscais gastam horas redigindo relatorios manuais para seus clientes. O empresario (CEO/Controller) recebe tabelas com numeros brutos que nao entende, gerando ligacoes constantes ao escritorio contabil para pedir explicacoes. Nenhum sistema de apuracao fiscal no Brasil oferece relatorios com narrativa inteligente que traduza dados fiscais em linguagem executiva.

### 1.2 Solucao Proposta

Integrar a API Claude (Anthropic) ao FBTax Cloud para gerar relatorios com narrativa automatica em linguagem natural, transformando dados brutos de apuracao em insights acionaveis para CEOs, Controllers e contadores.

### 1.3 Diferencial Competitivo

Nenhum concorrente direto (Dominio, Questor, Mastermaq, Fortes) oferece narrativa IA sobre dados fiscais. O FBTax Cloud sera o primeiro sistema de apuracao fiscal brasileiro com inteligencia artificial integrada aos relatorios.

### 1.4 Usuarios-Alvo

| Perfil | Necessidade | Relatorio |
|--------|-------------|-----------|
| CEO / Dono da empresa | Entender impostos sem jargao tecnico | Resumo Executivo Mensal |
| Controller / Diretor financeiro | Variacao e tendencias tributarias | Resumo Executivo + Comparativo |
| Contador / Analista fiscal | Produtividade na entrega ao cliente | Todos os relatorios |
| Escritorio contabil | Escalar atendimento sem aumentar equipe | PDF com marca do escritorio |

---

## 2. Escopo MVP (v1)

### MVP-1: Resumo Executivo Mensal com Narrativa IA

Relatorio em linguagem natural que resume a apuracao fiscal de um periodo, com destaques, alertas e recomendacoes. Destinado ao CEO/Controller.

### MVP-2: Insight do Dia (Dashboard Narrado)

Frase curta e contextualizada exibida no dashboard ao abrir o sistema. Notifica sobre vencimentos, anomalias, marcos e tendencias.

---

## 3. Requisitos Funcionais

### FR-001: Integracao com API Claude (Anthropic)

O backend Go deve se comunicar com a API da Anthropic para gerar narrativas.

**Criterios de aceitacao:**

- Modulo `backend/services/ai.go` encapsula chamadas a API Claude
- Suporte a modelos Haiku (custo baixo) e Sonnet (analises complexas)
- API key armazenada em variavel de ambiente `ANTHROPIC_API_KEY`
- Timeout configuravel (default 30s)
- Retry com backoff exponencial (max 3 tentativas)
- Resposta em Markdown retornada ao handler
- Erros da API nao quebram o sistema (fallback graceful: retorna dados sem narrativa)

### FR-002: Resumo Executivo Mensal

O sistema deve gerar um resumo executivo em linguagem natural para um periodo/empresa.

**Criterios de aceitacao:**

**Given** um usuario autenticado com empresa e dados importados para o periodo
**When** o usuario solicita o resumo executivo via GET /api/reports/executive-summary
**Then** o sistema agrega dados da apuracao (MVs + tabelas)
**And** monta um prompt com template + dados agregados
**And** chama a API Claude para gerar narrativa
**And** retorna JSON com: narrativa (Markdown), dados brutos, periodo, empresa

**Given** um periodo sem dados importados
**When** o usuario solicita o resumo executivo
**Then** o sistema retorna mensagem informativa sem chamar a IA

**Conteudo obrigatorio do resumo:**

- Situacao geral (1-2 frases)
- Impostos a recolher com valores e variacoes vs periodo anterior
- Destaques positivos e negativos
- Alertas (creditos nao aproveitados, inconsistencias)
- Proximos passos com datas de vencimento
- Tom: executivo, direto, sem jargao fiscal excessivo

### FR-003: Insight do Dia

O sistema deve gerar uma frase curta e contextualizada para o dashboard.

**Criterios de aceitacao:**

**Given** um usuario autenticado com empresa ativa
**When** o dashboard e carregado
**Then** o sistema gera um insight baseado em: vencimentos proximos, anomalias detectadas, marcos de importacao, tendencias
**And** retorna JSON com: texto (1-2 frases), tipo (alerta/info/positivo), acao_url (opcional)

**Given** nenhum dado relevante para gerar insight
**When** o dashboard e carregado
**Then** o sistema retorna insight generico (ex: "Importe seus arquivos SPED para comecar a receber insights")

**Tipos de insight priorizados:**

1. Vencimentos de guias nos proximos 5 dias
2. Creditos tributarios identificados e nao aproveitados
3. Anomalias em NF-es recentes (CFOP, aliquota)
4. Comparativo com mes anterior (aumento/reducao de carga)
5. Marcos (ex: "Voce ja importou 500 NF-es este mes")

### FR-004: Cache de Respostas IA

Narrativas geradas devem ser cacheadas para evitar custos desnecessarios.

**Criterios de aceitacao:**

- Resumo Executivo: cacheado por (company_id + periodo) ate que novos dados sejam importados
- Insight do Dia: cacheado por (company_id + data) com TTL de 6 horas
- Cache armazenado em Redis (ja disponivel na infra)
- Endpoint aceita parametro `?refresh=true` para forcar regeneracao

### FR-005: Endpoint de Dados Agregados para IA

O sistema deve fornecer dados agregados estruturados que alimentam os prompts de IA.

**Criterios de aceitacao:**

- Funcao `getApuracaoResumo(db, companyID, periodo)` retorna struct com:
  - Faturamento bruto e liquido
  - Total de NF-es emitidas/recebidas
  - Impostos por tipo (ICMS, PIS, COFINS, ICMS-ST)
  - Creditos aproveitados e nao aproveitados
  - Comparativo com periodo anterior (deltas absolutos e percentuais)
  - Vencimentos proximos
  - Anomalias detectadas (regras basicas)
- Dados extraidos das MVs existentes (`mv_mercadorias_agregada`, `mv_operacoes_simples`)

---

## 4. Requisitos Nao-Funcionais

### NFR-001: Latencia

- Insight do Dia: < 3 segundos (com cache: < 100ms)
- Resumo Executivo: < 10 segundos (primeira geracao), < 100ms (cache hit)
- Frontend exibe skeleton/loading enquanto IA processa

### NFR-002: Custo

- Usar Haiku como modelo padrao (custo ~10x menor que Sonnet)
- Sonnet apenas para analises comparativas complexas ou sob demanda
- Meta: < $0.05 por relatorio gerado
- Meta: < $0.01 por insight gerado

### NFR-003: Qualidade da Narrativa

- Texto em portugues brasileiro (pt-BR)
- Tom profissional, sem informalidade excessiva
- Valores monetarios formatados (R$ XX.XXX,00)
- Sem alucinacao: IA recebe apenas dados reais, nao inventa numeros
- Prompt instrui IA a dizer "dados insuficientes" quando nao ha informacao

### NFR-004: Seguranca

- Dados enviados a API Claude sao apenas agregados (nunca dados pessoais, CPF, email)
- CNPJ pode ser enviado (dado publico)
- API key protegida em variavel de ambiente
- Logs nao registram conteudo dos prompts (apenas metadata: tokens, latencia)

### NFR-005: Resiliencia

- Se API Claude estiver indisponivel, sistema retorna dados sem narrativa
- Frontend mostra badge "IA indisponivel" em vez de erro
- Circuit breaker apos 3 falhas consecutivas (pausa de 60s)

---

## 5. Fora do Escopo (MVP)

- Comparativo periodo a periodo (v2)
- Mapa de riscos fiscais (v2)
- PDF exportavel com marca do escritorio (v3)
- Briefing de planejamento tributario (v3)
- Configuracao de tom/estilo pelo usuario
- Selecao de idioma (apenas pt-BR no MVP)

---

## 6. Metricas de Sucesso

| Metrica | Meta MVP |
|---------|----------|
| Relatorios gerados por tenant/mes | > 5 |
| Insights visualizados por usuario/semana | > 3 |
| Custo medio por relatorio | < $0.05 |
| Latencia p95 (com cache) | < 200ms |
| Latencia p95 (sem cache) | < 8s |
| Taxa de fallback (IA indisponivel) | < 1% |
| NPS dos relatorios (pesquisa in-app) | > 8/10 |

---

## 7. Riscos e Mitigacoes

| Risco | Probabilidade | Impacto | Mitigacao |
|-------|--------------|---------|-----------|
| IA alucina numeros | Baixa | Critico | Prompt com instrucao explicita + dados pre-calculados |
| Custo escala acima do esperado | Media | Medio | Cache agressivo + Haiku como padrao + rate limit |
| Latencia alta da API Claude | Media | Medio | Cache + skeleton UI + timeout 30s |
| Qualidade da narrativa em pt-BR | Baixa | Medio | Prompt engineering + few-shot examples |
| API Claude fora do ar | Baixa | Baixo | Fallback graceful (dados sem narrativa) |

---

## 8. Dependencias

- **Anthropic API key** (criar conta em console.anthropic.com)
- **Redis** (ja disponivel na infra para cache)
- **Materialized Views** atualizadas com dados do periodo
- **Nenhuma dependencia Go externa** alem do HTTP client padrao
