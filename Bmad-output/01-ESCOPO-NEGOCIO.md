# Escopo de Negócio — FBTax Apuração Assistida

**Projeto:** FB_APU02
**Versão:** 1.0.4
**Produto:** FBTax Apuração Assistida — CBS/IBS (Reforma Tributária)
**URL Produção:** https://apuracao.fbtax.cloud

---

## 1. Contexto de Negócio

A Reforma Tributária Brasileira (EC 132/2023 — Lei Complementar 214/2024) criou dois novos tributos:

| Tributo | Responsável | Base |
|---------|-------------|------|
| **CBS** (Contribuição sobre Bens e Serviços) | Receita Federal do Brasil | Substitui PIS/COFINS |
| **IBS** (Imposto sobre Bens e Serviços) | Comitê Gestor (estados/municípios) | Substitui ICMS/ISS |

Durante o período de transição (2026–2032), as empresas precisam:
1. **Apurar débitos CBS/IBS** sobre cada NF-e/CT-e emitida
2. **Gerir créditos** sobre aquisições de insumos
3. **Comparar** os dados da empresa com os que a RFB possui em sua base
4. **Identificar documentos na malha fina** — NF-e/CT-e que a RFB enxerga mas a empresa não importou

---

## 2. Problema Resolvido

As empresas grandes (como Ferreira Costa) têm dezenas de filiais com **centenas de milhares de documentos fiscais** por mês. O processo manual de apuração CBS/IBS é:

- Sujeito a erros humanos
- Demorado (semanas de trabalho)
- Sem visibilidade dos documentos que a RFB possui mas a empresa não importou

O FBTax Apuração Assistida **automatiza e centraliza** todo esse processo.

---

## 3. Funcionalidades de Negócio

### 3.1 Importação de Documentos
- **NF-e Saídas** — Upload de XMLs de notas fiscais emitidas (mod. 55 e 65)
- **NF-e Entradas** — Upload de XMLs de notas fiscais recebidas
- **CT-e Entradas** — Upload de XMLs de conhecimentos de transporte recebidos
- Suporte a importação em lote via **ERP Bridge** (conector Oracle → API)
- Controle de duplicatas (chave de acesso única por empresa)

### 3.2 Apuração IBS / CBS
- Cálculo de **débitos CBS por documento** (NF-e/CT-e emitida)
- Identificação de **créditos IBS/CBS** sobre entradas
- Aplicação de **alíquotas** configuráveis por período
- Tratamento de **CFOP** para distinção de operações tributáveis
- Tratamento de **fornecedores Simples Nacional** (não geram crédito)
- Painel consolidado com totais por período

### 3.3 Créditos em Risco ("Créditos Perdidos")
- Identificação de notas de entrada **cujos emitentes não constam** nos registros da empresa como filiais
- Alerta de documentos que podem gerar **negação de crédito** pela RFB
- Cruzamento entre NF-e Entradas e cadastro de filiais/CNPJs da empresa

### 3.4 Integração com Receita Federal (RFB)
- Conexão direta com a **API RFB CBS v1** (produção e produção restrita)
- Autenticação OAuth2 (client_credentials) com credenciais por empresa
- **Solicitação automática** de apuração via scheduler configurável por horário
- **Webhook** para recebimento de resposta assíncrona
- Download e processamento do JSON de débitos da RFB
- Registro de 368k+ documentos de `rfb_debitos` por empresa

### 3.5 Malha Fina
- Identificação de **documentos que a RFB enxerga** (rfb_debitos) mas que **não foram importados** pela empresa
- Três tipos: NF-e Saídas, NF-e Entradas, CT-e
- Resumo consolidado por tipo + emitente + dia
- View materializada para performance (atualização automática após cada download da RFB)
- Filtro por CNPJ emitente com apelido de filial

### 3.6 Gestão Multiempresa
- Hierarquia: **Ambiente → Grupo → Empresa → Usuário**
- Troca de empresa sem logout (Company Switcher)
- Preferência de empresa persistida por usuário (banco de dados)
- Seletor de filiais para filtrar documentos por CNPJ

### 3.7 Configurações
- **Alíquotas CBS/IBS** por período
- **Tabela CFOP** com classificação de operações
- **Fornecedores Simples Nacional** (importação em lote por CSV/XLSX)
- **Apelidos de Filiais** — nomes amigáveis para CNPJs da empresa
- **Gestores** — coordenadores de relatório por área
- **Credenciais RFB** por empresa (client_id / client_secret, armazenados criptografados)

---

## 4. Usuários do Sistema

| Perfil | Permissões |
|--------|-----------|
| **Admin** | Acesso total: usuários, limpeza de dados, configurações de ambiente |
| **Usuário** | Importação, apuração, malha fina, RFB (dentro de sua(s) empresa(s)) |

---

## 5. Fluxo de Uso Típico (Mensal)

```
1. ERP Bridge extrai XMLs do Oracle ERP → POST /api/nfe-saidas/upload (x N)
2. Usuário acessa Apuração → visualiza totais CBS/IBS por período
3. Scheduler dispara (ex: 09:55) → solicita apuração à RFB
4. RFB processa e chama webhook → sistema baixa e processa JSON
5. Usuário acessa Malha Fina → identifica documentos ausentes
6. Usuário clica "Atualizar Resumo" → refresh da view materializada
7. Importação dos XMLs faltantes → malha fina zerada
8. Painel Gestão IBS/CBS → visão consolidada para declaração
```

---

## 6. ERP Bridge (Ferramenta Auxiliar)

Ferramenta Python separada que conecta ao ERP Oracle e alimenta a API:

- **Arquivo:** `bridge.py` (v1.1)
- **Tipo:** Script Python com Oracle Instant Client
- **Função:** Extrai NF-e Saídas, NF-e Entradas e CT-e Entradas do Oracle e envia para a API
- **Rastreamento:** `tracker.db` (SQLite local) — controla quais chaves já foram enviadas
- **Retomada:** Após interrupção, continua da última chave processada
- **Autenticação:** Login via `/api/auth/login`, renovação automática de token JWT
- **Configuração:** `config.yaml` com DSN Oracle, credenciais FBTax, período, servidores

---

## 7. Volumes de Dados (Referência Ferreira Costa)

| Tabela | Volume Estimado |
|--------|----------------|
| rfb_debitos | ~368.000 registros/mês |
| nfe_saidas | ~80.000/mês por servidor Oracle |
| nfe_entradas | ~4.500/mês |
| cte_entradas | ~1.700/mês |
| Total importações (multi-servidor) | ~120.000+ docs/mês |
