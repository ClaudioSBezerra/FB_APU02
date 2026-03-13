# FB_APU01 - Sistema Integrado de Apuração Fiscal (Reforma Tributária)

![Status](https://img.shields.io/badge/Status-Development-yellow) ![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go) ![React Version](https://img.shields.io/badge/React-18-61DAFB?logo=react) ![Docker](https://img.shields.io/badge/Docker-Enabled-2496ED?logo=docker)

## 🏢 Resumo Executivo
O **FB_APU01** é a evolução da plataforma de apuração fiscal da empresa, migrada de arquiteturas legadas para uma stack moderna, escalável e de alta performance. Desenvolvido para atender às exigências da nova Reforma Tributária, o sistema é capaz de processar grandes volumes de dados fiscais (SPED) com latência mínima, garantindo conformidade e agilidade nas tomadas de decisão.

### Principais Diferenciais
- **Performance de Ponta**: Motor de processamento em **Go (Golang)** capaz de analisar arquivos SPED de gigabytes em segundos.
- **Experiência do Usuário**: Interface moderna e responsiva em **React**, focada na produtividade do analista fiscal.
- **Escalabilidade**: Arquitetura baseada em microsserviços containerizados (Docker), pronta para deploy em nuvem ou on-premise.
- **Confiabilidade**: Processamento assíncrono com filas e transações atômicas no banco de dados.

---

## 📚 Documentação Técnica
Para detalhes aprofundados sobre a arquitetura e implementação, consulte nossa documentação oficial na pasta `/docs`:

- [📄 Especificações Técnicas (TECHNICAL_SPECS.md)](docs/TECHNICAL_SPECS.md): Stack tecnológico detalhado, versões e decisões de design.
- [🏗️ Arquitetura do Sistema (ARCHITECTURE.md)](docs/ARCHITECTURE.md): Diagramas de fluxo de dados (Mermaid), ERD do banco de dados e estrutura de componentes.
- [🔌 Referência da API (API_REFERENCE.md)](docs/API_REFERENCE.md): Documentação dos endpoints RESTful disponíveis.
- [🔄 Workflow & ALM (WORKFLOW_ALM.md)](docs/WORKFLOW_ALM.md): Processos de ciclo de vida da aplicação.

---

## 🚀 Guia de Início Rápido (Quick Start)

### Pré-requisitos
- Docker Desktop & Docker Compose V2
- Git

### Instalação e Execução
1. **Clone o repositório**:
   ```bash
   git clone https://github.com/ClaudioSBezerra/FB_APU01.git
   cd FB_APU01
   ```

2. **Configure o Ambiente**:
   Copie o arquivo de exemplo `.env.FB_APU01` para `.env` (se necessário, ajuste as credenciais).

3. **Inicie os Serviços**:
   ```bash
   docker compose --env-file .env.FB_APU01 up -d --build
   ```

4. **Acesse a Aplicação**:
   - **Frontend**: [http://localhost:3000](http://localhost:3000)
   - **API Health Check**: [http://localhost:3000/api/health](http://localhost:3000/api/health)

---

## 🔧 Manutenção e Comandos Úteis

- **Limpar Cache do Docker** (em caso de problemas de build):
  ```bash
  docker compose down --volumes
  docker builder prune -a -f
  ```
- **Rebuild Forçado**:
  ```bash
  docker compose --env-file .env.FB_APU01 build --no-cache
  ```

---

## 📞 Suporte e Contato
Para suporte técnico ou dúvidas sobre a implementação, contate a equipe de desenvolvimento.

> **Propriedade Intelectual**: Este software é de uso exclusivo e confidencial.
