# Ciclo de Vida da Aplicação (ALM) - Modelo "SAP-Like"

Este documento descreve o fluxo de trabalho para promoção de código entre ambientes (DEV -> QA -> PRD), inspirado no sistema de *Transport Requests* da SAP.

## 1. Estrutura de Ambientes (Landscape)

| Ambiente | Sigla | Infraestrutura | Propósito |
|---|---|---|---|
| **Desenvolvimento** | **DEV** | Máquina Local (Docker Compose) | Codificação, testes unitários e prova de conceito. |
| **Qualidade / Testes** | **QAS** | VPS Hostinger (FB_APU01) | Homologação, testes integrados e validação de usuário (UAT). |
| **Produção** | **PRD** | Servidor Dedicado (Futuro) | Ambiente produtivo final com dados reais. |

## 2. Sistema de Transportes (Transport Requests)

Para garantir a integridade, utilizamos o conceito de **Pacotes de Transporte** (baseado em Imagens Docker e Tags Git). O código nunca é editado diretamente em QA ou PRD.

### Fluxo de Transporte

#### A. De DEV para QAS (Transporte de Cópia/Workbench)
1.  O desenvolvedor finaliza uma funcionalidade em DEV.
2.  Executa o script `scripts/transport_to_qa.bat`.
3.  **O que o script faz:**
    *   Gera uma versão "Candidate" (ex: `v0.1.0-rc.1`).
    *   Constrói as imagens Docker.
    *   Envia para o VPS Hostinger.
    *   Reinicia os serviços no VPS.

#### B. De QAS para PRD (Transporte de Release)
1.  A funcionalidade é aprovada em QAS.
2.  Executa o script `scripts/promote_to_prd.bat`.
3.  **O que o script faz:**
    *   Pega a imagem *exata* que foi aprovada em QAS.
    *   Aplica uma Tag de Release Oficial (ex: `v0.1.0`).
    *   (Futuro) Envia para o servidor de Produção.
    *   Garante que **o que foi testado é exatamente o que vai para produção** (Imutabilidade).

## 3. Comandos de Operação

- **Subir para QA:** `.\scripts\transport_to_qa.bat`
- **Promover para PRD:** `.\scripts\promote_to_prd.bat`