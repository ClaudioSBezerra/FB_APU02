# FBTax Cloud - Estratégia de Versionamento e Ambientes

## Ambientes

| Ambiente | Servidor | Propósito | Branch | Deploy |
|----------|----------|-----------|--------|--------|
| **DEV** | Local (WSL) | Desenvolvimento ativo | `develop` | Manual (`docker-compose.yml`) |
| **QA** | Hostinger (Coolify) | Testes de qualidade | `main` | Automático (GitHub Actions) |
| **STAGING** | Azure VM | Cliente teste/homologação | `release/*` | Manual (installer) |
| **PROD** | Servidores clientes | Produção | Tags `v*` | Manual (installer) |

## Fluxo de Trabalho (GitFlow Simplificado)

```
develop (DEV local)
    ↓
    git push origin develop
    ↓
[Pull Request] develop → main
    ↓
main (QA Hostinger - auto deploy via Coolify)
    ↓
[Testes aprovados em QA]
    ↓
git tag v5.2.0
    ↓
[Deploy manual no Azure STAGING]
    ↓
[Aprovação do cliente]
    ↓
[Deploy em PROD clientes]
```

## Versionamento Semântico

Seguimos **Semantic Versioning 2.0.0** (`MAJOR.MINOR.PATCH`):

- **MAJOR** (5.x.x): Mudanças incompatíveis (breaking changes)
- **MINOR** (x.2.x): Novas funcionalidades (backward compatible)
- **PATCH** (x.x.1): Correções de bugs (backward compatible)

### Versão Atual
- **5.1.0** (2026-02-11)

### Próximas Versões Planejadas
- **5.2.0** - Próximo desenvolvimento (features novas)
- **5.1.1** - Hotfix se necessário (apenas bugs)

## Branches

### Branches Principais

| Branch | Descrição | Deploy |
|--------|-----------|--------|
| `main` | Código estável em QA | Coolify (Hostinger) |
| `develop` | Desenvolvimento ativo | Local |

### Branches de Suporte

| Tipo | Nomenclatura | Exemplo | Propósito |
|------|--------------|---------|-----------|
| Feature | `feature/nome-curto` | `feature/relatorio-vendas` | Novas funcionalidades |
| Bugfix | `fix/nome-curto` | `fix/login-timeout` | Correções em desenvolvimento |
| Hotfix | `hotfix/v5.1.1` | `hotfix/v5.1.1` | Correções urgentes em produção |
| Release | `release/v5.2.0` | `release/v5.2.0` | Preparação para release |

## Workflow Diário (A partir de amanhã)

### 1. Iniciar Desenvolvimento (DEV)

```bash
# Garantir que está na develop
git checkout develop
git pull origin develop

# Criar branch de feature
git checkout -b feature/nome-da-funcionalidade

# Desenvolver localmente
docker-compose up -d
# ... fazer alterações ...

# Commitar
git add .
git commit -m "feat: descrição da funcionalidade"
```

### 2. Enviar para QA (Hostinger/Coolify)

```bash
# Voltar para develop e mergear
git checkout develop
git merge feature/nome-da-funcionalidade
git push origin develop

# Criar Pull Request: develop → main
# Via GitHub UI ou gh CLI:
gh pr create --base main --head develop --title "Release v5.2.0" --body "Features:\n- Item 1\n- Item 2"

# Após aprovação do PR, mergear no GitHub
# Coolify detecta push em main e faz deploy automático
```

### 3. Testar em QA (Hostinger)

```bash
# Acessar https://fbtax.cloud
# Validar funcionalidades
# Verificar logs no Coolify
```

### 4. Promover para STAGING (Azure)

```bash
# Criar tag de release
git checkout main
git pull origin main
git tag -a v5.2.0 -m "Release 5.2.0 - Descrição"
git push origin v5.2.0

# GitHub Actions builda as imagens com tag v5.2.0

# No servidor Azure:
ssh -i ~/Downloads/azurefb.pem azureuser@172.203.83.76
cd ~/fbtax

# Editar docker-compose.yml para usar versão específica
sudo nano docker-compose.yml
# Mudar de :latest para :v5.2.0

# Atualizar
sudo ./update.sh
```

### 5. Homologação Cliente (STAGING)

```bash
# Cliente testa em http://172.203.83.76
# Se aprovado → Deploy em PROD
# Se reprovado → Voltar para DEV e corrigir
```

### 6. Deploy Produção (Clientes)

```bash
# Em cada servidor do cliente:
cd ~/fbtax
sudo ./update.sh  # Puxa :latest ou :v5.2.0
```

## Hotfix (Correção Urgente)

Quando um bug crítico é encontrado em produção:

```bash
# 1. Criar branch de hotfix a partir da main
git checkout main
git pull origin main
git checkout -b hotfix/v5.1.1

# 2. Corrigir o bug
# ... fazer alterações ...
git commit -m "fix: descrição do bug crítico"

# 3. Mergear em main E develop
git checkout main
git merge hotfix/v5.1.1
git push origin main

git checkout develop
git merge hotfix/v5.1.1
git push origin develop

# 4. Criar tag
git tag -a v5.1.1 -m "Hotfix 5.1.1 - Correção crítica"
git push origin v5.1.1

# 5. Deploy imediato em QA (automático) e STAGING/PROD (manual)
```

## Tags e Releases

### Criar Release no GitHub

```bash
# Via CLI
gh release create v5.2.0 --title "v5.2.0 - Nome da Release" --notes "
## Novidades
- Feature 1
- Feature 2

## Correções
- Bug 1
- Bug 2

## Breaking Changes
- Mudança incompatível (se houver)
"

# Anexar binários/assets se necessário
gh release upload v5.2.0 installer.tar.gz
```

### Listar Releases

```bash
gh release list
git tag -l
```

## Configuração Inicial (Fazer Amanhã)

### 1. Criar branch develop

```bash
git checkout -b develop
git push -u origin develop
```

### 2. Configurar branch develop como padrão local

```bash
git config branch.develop.remote origin
git config branch.develop.merge refs/heads/develop
```

### 3. Proteger branches no GitHub

**Settings > Branches > Branch protection rules:**

#### Para `main`:
- ✅ Require pull request reviews before merging
- ✅ Require status checks to pass (CI)
- ✅ Include administrators (opcional)

#### Para `develop`:
- ✅ Allow force pushes (apenas você)

### 4. Configurar Docker Compose para DEV

```bash
# Usar docker-compose.yml (não o prod)
cp docker-compose.yml docker-compose.dev.yml
# Ajustar portas se necessário (8080, 8081, etc.)
```

## Changelog

Manter `CHANGELOG.md` atualizado:

```markdown
# Changelog

## [5.2.0] - 2026-02-12

### Added
- Nova funcionalidade X
- Nova funcionalidade Y

### Changed
- Melhoria em Z

### Fixed
- Correção de bug W

## [5.1.0] - 2026-02-11

### Added
- Instalador comercial standalone
- Questionário de implantação cliente
- Deploy dual (Coolify + Installer)
...
```

## Checklist de Release

Antes de criar uma nova release:

- [ ] Todos os testes passando em DEV
- [ ] PR aprovado e mergeado em `main`
- [ ] Deploy automático em QA (Coolify) funcionando
- [ ] Testes manuais em QA aprovados
- [ ] `CHANGELOG.md` atualizado
- [ ] Versão atualizada em `backend/main.go` (constante VERSION)
- [ ] Tag criada com mensagem descritiva
- [ ] Release notes publicadas no GitHub
- [ ] Deploy em STAGING (Azure) realizado
- [ ] Cliente homologou em STAGING
- [ ] Comunicação aos clientes PROD enviada

## Rollback

Se algo der errado em produção:

### Rollback em QA (Coolify)
```bash
# Via Coolify UI: Deploy anterior
# Ou via Git: reverter commit e push
git revert <commit-hash>
git push origin main
```

### Rollback em STAGING/PROD
```bash
# No servidor:
cd ~/fbtax
sudo nano docker-compose.yml
# Mudar tag de :v5.2.0 para :v5.1.0
sudo docker compose pull
sudo docker compose up -d
```

## Ferramentas Úteis

```bash
# Ver diferenças entre branches
git diff develop..main

# Ver commits não mergeados
git log main..develop --oneline

# Ver última tag
git describe --tags --abbrev=0

# Ver versão atual em produção
curl http://172.203.83.76/api/health | jq .version
```

## Referências

- [Semantic Versioning](https://semver.org/)
- [GitFlow](https://nvie.com/posts/a-successful-git-branching-model/)
- [Conventional Commits](https://www.conventionalcommits.org/)
