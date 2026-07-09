# Guia de Deploy — FB_APU02

---

## Infraestrutura de Produção

| Componente | Tecnologia | URL |
|-----------|-----------|-----|
| Orquestrador | Coolify (PaaS) | Servidor `srv1306085` (76.13.171.196) |
| Reverse Proxy | Traefik (gerenciado pelo Coolify) | — |
| TLS | Let's Encrypt via Traefik | Auto-renovado |
| Backend | Container Docker (Alpine) | `:8081` interno |
| Frontend | Container Docker (Nginx) | `:80` interno |
| Banco de dados | PostgreSQL 15 (container Coolify) | `:5432` interno |
| CI/CD | GitHub Actions | Push em `main` dispara deploy |

### Domínios

| Domínio | Serviço |
|---------|---------|
| `fctax.fcxlabs.com` | Aplicação principal (prod) |
| `apuracao.fbtax.cloud` | Aplicação principal (prod alternativo) |

---

## Docker Compose de Produção

O arquivo `docker-compose.prod.yml` define a stack de produção gerenciada pelo Coolify:

```yaml
# Estrutura simplificada
services:
  backend:
    image: ghcr.io/ferreiracosta/fb_apu02-backend:latest
    environment:
      DATABASE_URL: ${DATABASE_URL}
      JWT_SECRET: ${JWT_SECRET}
      # ... demais variáveis via Coolify env block
    networks:
      - coolify  # rede externa do Coolify (necessária para Traefik routing)
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.apu02-backend.rule=Host(`fctax.fcxlabs.com`) && PathPrefix(`/api`)"
      - "traefik.http.services.apu02-backend.loadbalancer.server.port=8081"
      - "traefik.http.routers.apu02-backend.tls.certresolver=letsencrypt"

  frontend:
    image: ghcr.io/ferreiracosta/fb_apu02-frontend:latest
    networks:
      - coolify
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.apu02-frontend.rule=Host(`fctax.fcxlabs.com`)"
      - "traefik.http.services.apu02-frontend.loadbalancer.server.port=80"
      - "traefik.http.routers.apu02-frontend.tls.certresolver=letsencrypt"

networks:
  coolify:
    external: true
```

> **Atenção:** A rede `coolify` é **externa** — criada pelo Coolify. Não incluir a rede `coolify` no `docker-compose.yml` de desenvolvimento.

---

## Pipeline CI/CD (GitHub Actions)

### `.github/workflows/deploy-production.yml`

Dispara em push para `main`:

1. Build Docker do backend (`backend/Dockerfile`)
2. Build Docker do frontend (`frontend/Dockerfile`)
3. Push para GHCR (`ghcr.io/ferreiracosta/fb_apu02-*:latest`)
4. Notifica Coolify via webhook para fazer pull e restart dos containers

### `.github/workflows/deploy-staging.yml`

Mesmo fluxo, para branch de staging (ambiente de teste).

### `.github/workflows/deploy-cliente-aws.yml`

Deploy do ERP Bridge Python no servidor AWS do cliente:
- SSH para o servidor AWS
- `git pull` no diretório do bridge
- `systemctl restart erp-bridge`

---

## Variáveis de Ambiente em Produção

Gerenciadas pelo Coolify via painel de variáveis de ambiente da aplicação.

### Backend

| Variável | Descrição | Obrigatória |
|----------|-----------|-------------|
| `DATABASE_URL` | `postgres://user:pass@host:5432/db?sslmode=require` | Sim |
| `JWT_SECRET` | Secret HS256 — mínimo 32 bytes | Sim |
| `ENCRYPTION_KEY` | Chave de criptografia de credenciais RFB/CGIBS | Recomendado |
| `RFB_WEBHOOK_URL` | `https://fctax.fcxlabs.com/api/rfb/webhook` | Sim |
| `APP_URL` | `https://fctax.fcxlabs.com` | Não |
| `ALLOWED_ORIGINS` | `https://fctax.fcxlabs.com,https://apuracao.fbtax.cloud` | Sim |
| `COOKIE_SECURE` | `true` | Sim |
| `PORT` | `8081` | Não (padrão) |
| `SMTP_HOST` | Servidor SMTP para e-mails transacionais | Não |
| `SMTP_PORT` | Porta SMTP | Não |
| `SMTP_USER` | Usuário SMTP | Não |
| `SMTP_PASSWORD` | Senha SMTP | Não |
| `SMTP_FROM` | E-mail remetente | Não |

---

## Build Docker

### Backend (`backend/Dockerfile`)

Multi-stage build:

```dockerfile
# Stage 1: Build
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server .

# Stage 2: Runtime
FROM alpine:latest
RUN apk add --no-cache tzdata
ENV TZ=America/Sao_Paulo
COPY --from=builder /app/server /server
COPY migrations/ /migrations/
COPY static/ /static/
EXPOSE 8081
CMD ["/server"]
```

### Frontend (`frontend/Dockerfile`)

Multi-stage build:

```dockerfile
# Stage 1: Build
FROM node:18-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# Stage 2: Serve
FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
```

---

## Migrations em Produção

As migrations são **executadas automaticamente** quando o backend inicia. Não há passo manual de migration.

**Ordem de deploy:**
1. Deploy do banco de dados (se nova instância)
2. Deploy do backend → migrations rodam no startup
3. Deploy do frontend

**Em caso de migration problemática:**
```bash
# Verificar migrations executadas:
SELECT filename, executed_at FROM schema_migrations ORDER BY executed_at DESC LIMIT 20;

# Desabilitar migration com problema (não delete!):
# Renomear o arquivo para adicionar .disabled no servidor
# Ou fazer deploy de nova migration que reverte
```

---

## ERP Bridge em Produção (AWS)

### Deploy Inicial

```bash
# No servidor AWS
sudo mkdir -p /opt/apps/fbtax/erp-bridge
cd /opt/apps/fbtax/erp-bridge

# Copiar bridge.py e config.yaml
# Criar e ativar virtualenv
python3 -m venv venv
venv/bin/pip install oracledb requests pyyaml

# Configurar config.yaml com credenciais reais
cp config.yaml.example config.yaml
nano config.yaml

# Instalar e habilitar o serviço systemd
sudo cp erp-bridge.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable erp-bridge
sudo systemctl start erp-bridge
```

### Atualizando o Bridge

```bash
# No servidor AWS (feito pelo GitHub Actions ou manualmente):
cd /opt/apps/fbtax/erp-bridge
git pull
sudo systemctl restart erp-bridge
sudo systemctl status erp-bridge
```

### Monitoramento

```bash
# Status do serviço:
sudo systemctl status erp-bridge

# Logs em tempo real:
journalctl -u erp-bridge -f

# Logs de arquivo:
ls -la /opt/apps/fbtax/erp-bridge/logs/
tail -f /opt/apps/fbtax/erp-bridge/logs/bridge_*.log
```

---

## SSL e Certificados

### Produção (Traefik + Let's Encrypt)

Gerenciado automaticamente pelo Traefik via label `tls.certresolver=letsencrypt`.  
Renovação automática antes do vencimento.

### Servidor Nginx do Cliente (`fctax.fcxlabs.com`)

Gerenciado por Certbot + Nginx no servidor do cliente:

```bash
# Verificar validade do certificado:
openssl s_client -connect fctax.fcxlabs.com:443 -servername fctax.fcxlabs.com 2>/dev/null | openssl x509 -noout -dates

# Renovação manual se necessário:
docker run --rm \
  -v nginx_certbot_certs:/etc/letsencrypt \
  -v nginx_certbot_webroot:/var/www/certbot \
  certbot/certbot certonly --webroot \
  -w /var/www/certbot \
  -d fctax.fcxlabs.com \
  --non-interactive --agree-tos -m admin@ferreiracosta.com.br

# Após renovação, reiniciar nginx:
docker restart nginx_proxy
```

> **Importante:** O certificado SSL deve estar válido para que a RFB aceite o webhook URL. SSL expirado = apuração CBS bloqueada.

---

## Rollback

```bash
# Fazer rollback via Coolify (UI) para imagem anterior
# Ou via CLI do Coolify:
coolify rollback --service fb_apu02-backend --tag previous
coolify rollback --service fb_apu02-frontend --tag previous
```

Para rollback de migration problemática: criar nova migration que desfaz as mudanças.  
**Nunca** deletar ou alterar migrations já executadas em produção.

---

## Checklist de Deploy

- [ ] Build passou no CI/CD (GitHub Actions) sem erros
- [ ] TypeScript compilou sem erros (`npx tsc --noEmit`)
- [ ] Novas variáveis de ambiente adicionadas no Coolify
- [ ] Novas migrations testadas em staging antes de prod
- [ ] RFB_WEBHOOK_URL acessível via HTTPS (SSL válido)
- [ ] Health check OK após deploy: `GET /api/health`
- [ ] Solicitações de apuração RFB funcionando após deploy crítico
