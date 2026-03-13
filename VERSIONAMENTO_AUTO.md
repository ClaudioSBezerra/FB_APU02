# FBTax Cloud - Versionamento Automatizado (CI/CD Completo)

## Ambientes e Deploy Automático

| Ambiente | Servidor | Branch/Tag | Trigger | Deploy |
|----------|----------|------------|---------|--------|
| **DEV** | Local (WSL) | `develop` | Manual | `docker-compose up -d` |
| **QA** | Hostinger (Coolify) | `main` | Push automático | Coolify webhook |
| **STAGING** | Azure VM | Tags `v*-rc*` | Push tag | GitHub Actions SSH |
| **PROD** | Clientes | Tags `v*` (sem -rc) | Manual | Instruções aos clientes |

## Fluxo Totalmente Automatizado

```
develop (DEV local)
    ↓ git push
    ↓
[PR] develop → main
    ↓ merge
main → QA (Coolify deploy automático)
    ↓ testes OK
    ↓
git tag v5.2.0-rc1 (release candidate)
    ↓
GitHub Actions:
  - Build imagens
  - SSH no Azure
  - docker compose pull
  - docker compose up -d
    ↓
STAGING (Azure - deploy automático)
    ↓ cliente aprova
    ↓
git tag v5.2.0 (release final)
    ↓
GitHub Actions:
  - Build imagens :v5.2.0
  - Notifica clientes
    ↓
PROD (clientes executam update.sh)
```

## Versionamento com Release Candidates

### Desenvolvimento Normal
- `v5.2.0-rc1` → STAGING (auto)
- `v5.2.0-rc2` → STAGING (auto, se houver correções)
- `v5.2.0` → PROD (manual pelos clientes)

### Hotfix Urgente
- `v5.1.1-rc1` → STAGING (auto)
- `v5.1.1` → PROD (manual pelos clientes)

## GitHub Actions - Deploy Automático STAGING

Vou criar `.github/workflows/deploy-staging.yml`:

```yaml
name: Deploy to STAGING (Azure)

on:
  push:
    tags:
      - 'v*-rc*'  # Apenas release candidates

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

jobs:
  deploy-to-staging:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Extract version from tag
        id: version
        run: echo "VERSION=${GITHUB_REF#refs/tags/}" >> $GITHUB_OUTPUT

      - name: Deploy to Azure STAGING
        uses: appleboy/ssh-action@v1.0.0
        with:
          host: ${{ secrets.STAGING_HOST }}
          username: ${{ secrets.STAGING_USER }}
          key: ${{ secrets.STAGING_SSH_KEY }}
          script: |
            cd ~/fbtax

            # Atualizar imagens
            sudo docker compose pull

            # Reiniciar serviços
            sudo docker compose up -d

            # Aguardar health check
            echo "Aguardando sistema iniciar..."
            for i in {1..20}; do
              if curl -sf http://localhost/api/health > /dev/null 2>&1; then
                echo "✅ Deploy STAGING concluído!"
                exit 0
              fi
              sleep 5
            done

            echo "❌ Timeout no health check"
            sudo docker compose logs api --tail 50
            exit 1

      - name: Notify deployment
        if: always()
        run: |
          if [ "${{ job.status }}" == "success" ]; then
            echo "🎉 STAGING Deploy Successful: ${{ steps.version.outputs.VERSION }}"
            echo "URL: http://${{ secrets.STAGING_HOST }}"
          else
            echo "❌ STAGING Deploy Failed"
          fi
```

## Secrets Necessários no GitHub

**Settings > Secrets and variables > Actions > New repository secret:**

| Secret | Valor | Descrição |
|--------|-------|-----------|
| `STAGING_HOST` | `172.203.83.76` | IP do Azure VM |
| `STAGING_USER` | `azureuser` | Usuário SSH |
| `STAGING_SSH_KEY` | Conteúdo de `azurefb.pem` | Chave privada SSH |

## Workflow Diário Simplificado

### 1. Desenvolver (DEV)
```bash
git checkout develop
git checkout -b feature/nova-funcionalidade

# Desenvolver...
docker-compose up -d

git add .
git commit -m "feat: nova funcionalidade"
git push origin feature/nova-funcionalidade
```

### 2. Enviar para QA (Automático)
```bash
# Criar PR no GitHub: feature → develop
gh pr create --base develop --head feature/nova-funcionalidade

# Após merge em develop, criar PR: develop → main
gh pr create --base main --head develop

# Ao mergear em main → Coolify deploya automaticamente em QA
```

### 3. Promover para STAGING (Automático)
```bash
# Após testes OK em QA, criar release candidate
git checkout main
git pull origin main
git tag v5.2.0-rc1
git push origin v5.2.0-rc1

# GitHub Actions deploya automaticamente no Azure
# Acompanhar em: https://github.com/ClaudioSBezerra/FB_APU01/actions
```

### 4. Homologação STAGING
```bash
# Cliente testa em http://172.203.83.76
# Se OK → criar release final
# Se NOK → corrigir e criar v5.2.0-rc2
```

### 5. Release Final (PROD)
```bash
# Após aprovação do cliente em STAGING
git tag v5.2.0
git push origin v5.2.0

# GitHub Actions builda imagens :v5.2.0
# Enviar email aos clientes com instruções
```

### 6. Clientes Atualizam (Manual)
```bash
# Cada cliente executa no servidor deles:
cd ~/fbtax
sudo ./update.sh
```

## Automação Extra (Opcional)

### Notificação por Email/Slack

Adicionar ao final do workflow:

```yaml
      - name: Send notification
        uses: dawidd6/action-send-mail@v3
        with:
          server_address: ${{ secrets.SMTP_HOST }}
          server_port: ${{ secrets.SMTP_PORT }}
          username: ${{ secrets.SMTP_USER }}
          password: ${{ secrets.SMTP_PASSWORD }}
          subject: "Deploy STAGING ${{ steps.version.outputs.VERSION }} - ${{ job.status }}"
          body: |
            Deploy em STAGING finalizado

            Versão: ${{ steps.version.outputs.VERSION }}
            Status: ${{ job.status }}
            URL: http://${{ secrets.STAGING_HOST }}

            Logs: https://github.com/${{ github.repository }}/actions/runs/${{ github.run_id }}
          to: claudio@fortesbezerra.com.br
          from: FBTax CI/CD <noreply@fbtax.cloud>
```

### Deploy em Múltiplos Clientes PROD (Futuro)

Quando tiver múltiplos clientes, podemos criar:

```yaml
name: Deploy to Production Clients

on:
  workflow_dispatch:  # Manual trigger
    inputs:
      version:
        description: 'Version to deploy (e.g., v5.2.0)'
        required: true
      clients:
        description: 'Clients to deploy (comma-separated)'
        required: true
        default: 'all'

jobs:
  deploy-clients:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        client: ${{ fromJson(github.event.inputs.clients) }}

    steps:
      - name: Deploy to ${{ matrix.client }}
        uses: appleboy/ssh-action@v1.0.0
        with:
          host: ${{ secrets[format('CLIENT_{0}_HOST', matrix.client)] }}
          username: ${{ secrets[format('CLIENT_{0}_USER', matrix.client)] }}
          key: ${{ secrets[format('CLIENT_{0}_SSH_KEY', matrix.client)] }}
          script: |
            cd ~/fbtax
            sudo docker compose pull
            sudo docker compose up -d
```

## Rollback Automático

Se deploy em STAGING falhar:

```yaml
      - name: Rollback on failure
        if: failure()
        uses: appleboy/ssh-action@v1.0.0
        with:
          host: ${{ secrets.STAGING_HOST }}
          username: ${{ secrets.STAGING_USER }}
          key: ${{ secrets.STAGING_SSH_KEY }}
          script: |
            cd ~/fbtax
            # Voltar para última versão estável
            git fetch --tags
            LAST_STABLE=$(git tag -l 'v*' --sort=-v:refname | grep -v 'rc' | head -1)
            echo "Rolling back to $LAST_STABLE"

            # Atualizar docker-compose para usar versão estável
            sed -i "s/:latest/:$LAST_STABLE/g" docker-compose.yml
            sudo docker compose pull
            sudo docker compose up -d
```

## Monitoramento (Opcional)

### Health Check Contínuo

```yaml
name: Health Check STAGING

on:
  schedule:
    - cron: '*/15 * * * *'  # A cada 15 minutos
  workflow_dispatch:

jobs:
  health-check:
    runs-on: ubuntu-latest
    steps:
      - name: Check STAGING health
        run: |
          RESPONSE=$(curl -sf http://${{ secrets.STAGING_HOST }}/api/health || echo "FAILED")

          if [[ "$RESPONSE" == "FAILED" ]]; then
            echo "❌ STAGING está DOWN!"
            exit 1
          fi

          VERSION=$(echo $RESPONSE | jq -r .version)
          STATUS=$(echo $RESPONSE | jq -r .status)

          echo "✅ STAGING OK - Version: $VERSION, Status: $STATUS"
```

## Checklist de Implementação (Fazer Agora)

- [ ] Criar `.github/workflows/deploy-staging.yml`
- [ ] Adicionar secrets no GitHub (STAGING_HOST, STAGING_USER, STAGING_SSH_KEY)
- [ ] Criar branch `develop`
- [ ] Proteger branches `main` e `develop` no GitHub
- [ ] Testar workflow com tag `v5.1.1-rc1` (teste)
- [ ] Atualizar `MEMORY.md` com novo fluxo
- [ ] Criar `CHANGELOG.md`

## Vantagens do Fluxo Automatizado

✅ **Zero intervenção manual** entre QA e STAGING
✅ **Rastreabilidade completa** (cada deploy tem um workflow run)
✅ **Rollback rápido** se algo falhar
✅ **Release candidates** permitem iterações rápidas
✅ **Cliente sempre testa versão final** antes de PROD
✅ **Logs centralizados** no GitHub Actions

## Resumo de Comandos

```bash
# Desenvolvimento local
git checkout develop
git pull
# ... desenvolver ...
git push

# QA (automático após PR)
gh pr create --base main --head develop

# STAGING (automático após tag RC)
git tag v5.2.0-rc1
git push origin v5.2.0-rc1

# PROD (manual pelos clientes)
git tag v5.2.0
git push origin v5.2.0
# Cliente executa: sudo ./update.sh
```
