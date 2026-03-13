# FB_APU01 - GUIA DE DEPLOY PRODUÇÃO
# Fluxo: GitHub → Coolify → Hostinger
# Data: 06/02/2026 | Versão: 5.0.9

## 🎯 **RESUMO DO FLUXO**

```
GitHub (Push main) → GitHub Actions (Build & Push) → Coolify (Webhook) → Hostinger (Deploy)
```

## 📋 **PRÉ-REQUISITOS**

### 1. Configurar Secrets no GitHub
Vá em: **GitHub → Settings → Secrets and variables → Actions**

```bash
# Secrets obrigatórios:
COOLIFY_WEBHOOK_URL          # Webhook do Coolify
COOLIFY_DEPLOY_TOKEN         # Token de deploy do Coolify  
COOLIFY_DASHBOARD_URL         # URL do dashboard Coolify
PRODUCTION_URL              # URL final da aplicação (https://fbtax.cloud)
GITHUB_TOKEN                # Já existe, usado para push de imagem
```

### 2. Configurar Coolify
- **App**: FB_APU01 Production
- **Image Registry**: GitHub Container Registry (ghcr.io)
- **Environment Variables**: Configurar no Coolify (não no arquivo .env)
- **Health Check**: `/api/health`
- **Auto-deploy**: Ativar webhook do GitHub

### 3. Configurar Hostinger
- **PostgreSQL**: Banco de dados externo
- **Redis**: Cache externo (se necessário)
- **Storage**: Para uploads e backups
- **Domínio**: fbtax.cloud configurado

## 🚀 **PROCESSO DE DEPLOY**

### Passo 1: Push para GitHub
```bash
git checkout main
git add .
git commit -m "Deploy production: v5.0.9 - Ready for Coolify"
git push origin main
```

### Passo 2: GitHub Actions (Automático)
O workflow irá:
1. ✅ **Backup** (se configurado)
2. ✅ **Build** imagem Docker
3. ✅ **Push** para GitHub Container Registry
4. ✅ **Notificar** Coolify via webhook
5. ✅ **Health Check** pós-deploy

### Passo 3: Coolify (Automático)
Coolify irá:
1. 📥 Receber webhook
2. 🔄 Pull da nova imagem
3. 🚀 Deploy no Hostinger
4. 📊 Atualizar health checks

### Passo 4: Verificação
Monitorar em:
- **Coolify Dashboard**: Status do deploy
- **GitHub Actions**: Logs do workflow
- **Produção**: https://fbtax.cloud/api/health

## 🛡️ **BACKUP E SEGURANÇA**

### Backup Automático
```bash
# Script de backup executado no servidor Hostinger
curl -X POST https://fbtax.cloud/api/admin/backup \
  -H "Authorization: Bearer ADMIN_TOKEN" \
  -d '{"type": "full", "encrypt": true}'
```

### Restore de Emergência
```bash
# Via Coolify ou SSH no Hostinger
cd /opt/fb_apu01/backups
pg_restore -h HOST -U USER -d fiscal_db_prod backup_20260206_020000.sql
```

## 📊 **MONITORAMENTO (COOLIFY + COLLIFY)**

### Health Checks
- **Endpoint**: `https://fbtax.cloud/api/health`
- **Intervalo**: 30 segundos
- **Timeout**: 10 segundos
- **Threshold**: 3 falhas consecutivas

### Logs
```bash
# Verificar logs via Coolify Dashboard
# Ou via SSH no Hostinger:
docker logs fb_apu01-production --tail=100
```

## 🔧 **CONFIGURAÇÕES ESPECÍFICAS**

### Variáveis de Ambiente (Coolify)
Configure estas variáveis no painel do Coolify:

```bash
# Aplicação
PORT=8081
ENVIRONMENT=production
JWT_SECRET=super-secure-jwt-secret-2026

# Banco (Hostinger)
DATABASE_URL=postgres://user:pass@host:5432/fiscal_db_prod?sslmode=require

# Cache (se necessário)
REDIS_ADDR=redis:6379

# Segurança
CORS_ORIGINS=https://fbtax.cloud,https://www.fbtax.cloud
RATE_LIMIT_ENABLED=true
AUDIT_LOGS=true
```

### Materialized Views
```bash
# Refresh automático (via API)
curl -X POST https://fbtax.cloud/api/admin/refresh-views \
  -H "Authorization: Bearer ADMIN_TOKEN"
```

## 🚨 **ROLLBACK PLAN**

### Via Coolify (Recomendado)
1. Acessar dashboard Coolify
2. Selecionar "Previous Deployments"
3. Escolher versão anterior funcional
4. Clicar "Rollback"
5. Aguardar deploy automático

### Via GitHub (Alternativo)
```bash
# Tag da versão anterior
git tag -a v5.0.8 -m "Rollback version"
git push origin v5.0.8

# Forçar deploy da tag anterior
curl -X POST "${COOLIFY_WEBHOOK_URL}" \
  -H "Authorization: Bearer ${COOLIFY_DEPLOY_TOKEN}" \
  -d '{"image": "ghcr.io/repo:5.0.8", "action": "deploy"}'
```

## 📋 **CHECKLIST FINAL**

### Antes do Deploy
- [ ] Secrets configurados no GitHub
- [ ] Coolify app configurado com webhook
- [ ] Banco Hostinger pronto e acessível
- [ ] Domínio fbtax.cloud apontando para Hostinger
- [ ] Teste de backup funcional

### Pós-Deploy
- [ ] Health check respondendo
- [ ] Login funcionando
- [ ] Upload de SPEDs OK
- [ ] Dashboard com dados
- [ ] Materialized views atualizadas

### Monitoramento
- [ ] Coolify health checks OK
- [ ] Logs sem erros críticos
- [ ] Backup automático agendado
- [ ] Performance aceitável

## 📞 **SUPORTE**

- **Coolify**: Dashboard e documentação
- **Hostinger**: Suporte técnico
- **GitHub**: Issues e logs
- **Repositório**: https://github.com/USER/FB_APU01

---

## 🎉 **DEPLOY AUTOMÁTICO CONCLUÍDO!**

Com este setup, cada `git push origin main` irá:
1. 🔄 Backup automático
2. 🏗️ Build otimizado
3. 📤 Push para registry
4. 🚀 Deploy sem intervenção manual
5. ✅ Verificação automática

**Fluxo moderno, seguro e totalmente automatizado!** 🚀