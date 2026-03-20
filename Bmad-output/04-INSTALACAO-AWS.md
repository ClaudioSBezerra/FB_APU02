# Guia de Instalação — FBTax Apuração Assistida (AWS / Coolify)

**Projeto:** FB_APU02
**Destino:** Nova instância AWS com Coolify self-hosted

---

## 1. Pré-requisitos de Infraestrutura

### 1.1 Servidor AWS (EC2 recomendado)

| Recurso | Mínimo | Recomendado |
|---------|--------|-------------|
| CPU | 2 vCPU | 4 vCPU |
| RAM | 4 GB | 8 GB |
| Disco | 40 GB SSD | 100 GB SSD |
| SO | Ubuntu 22.04 LTS | Ubuntu 22.04 LTS |
| Arch | x86_64 | x86_64 |

**Portas liberadas no Security Group:**
```
22    — SSH (restrito ao IP do time)
80    — HTTP  (Traefik → redireciona para HTTPS)
443   — HTTPS (Traefik → aplicação)
8000  — Coolify dashboard (opcional, pode restringir por IP)
```

### 1.2 Domínio / DNS

Antes de instalar, configure os registros DNS apontando para o IP público da instância:

```
A   apuracao.seucliente.com.br   →   <IP público EC2>
A   coolify.seucliente.com.br    →   <IP público EC2>   (opcional, para acesso ao painel)
```

---

## 2. Instalação do Coolify

Execute como root (ou com sudo) na instância EC2:

```bash
curl -fsSL https://cdn.coollabs.io/coolify/install.sh | bash
```

Aguardar a instalação (~5 minutos). O Coolify sobe com Docker e Traefik automaticamente.

Acesse o painel em: `http://<IP>:8000`

1. Crie conta de administrador no primeiro acesso
2. Configure o domínio do Coolify (opcional): Settings → Instance Settings
3. Ative Let's Encrypt: Settings → SSL

---

## 3. Banco de Dados PostgreSQL

### Opção A — PostgreSQL via Coolify (recomendado para começar)

No painel Coolify:
1. **New Resource → Database → PostgreSQL 15**
2. Configure:
   - **Name:** `fb_apu02_db`
   - **PostgreSQL User:** `fb_apu02`
   - **PostgreSQL Password:** `<senha forte gerada>`
   - **PostgreSQL DB:** `fb_apu02`
   - **Volume:** persistido automaticamente
3. Salve e inicie

A `DATABASE_URL` ficará disponível como variável de ambiente no Coolify.

### Opção B — RDS PostgreSQL (produção de alta disponibilidade)

```
Engine: PostgreSQL 15.x
Instance: db.t3.medium (mínimo)
Multi-AZ: sim (recomendado)
Storage: 100 GB gp3
Backup: 7 dias
VPC: mesma da EC2
Security Group: liberar porta 5432 apenas para EC2
```

---

## 4. Configuração do Repositório no Coolify

1. **New Resource → Application → Public Git Repository** (ou Private com deploy key)
2. **Repository URL:** `https://github.com/seu-org/FB_APU02`
3. **Branch:** `main`
4. **Build Pack:** Dockerfile
5. **Dockerfile Path:** `./Dockerfile`
6. **Port:** `8081`

---

## 5. Variáveis de Ambiente

Configure em **Coolify → Application → Environment Variables**.

> Nunca versione este arquivo com valores reais.

```env
# ─── Banco de Dados ────────────────────────────────────────────
DATABASE_URL=postgres://fb_apu02:<SENHA_DB>@<HOST_DB>:5432/fb_apu02?sslmode=require

# ─── Servidor ──────────────────────────────────────────────────
PORT=8081
ENVIRONMENT=production

# ─── Segurança JWT ─────────────────────────────────────────────
# Gerar com: openssl rand -hex 32
JWT_SECRET=<string_aleatoria_minimo_64_chars>

# ─── Criptografia de Credenciais RFB ───────────────────────────
# Gerar com: openssl rand -hex 32   (DIFERENTE do JWT_SECRET)
ENCRYPTION_KEY=<string_aleatoria_minimo_64_chars>

# ─── CORS ──────────────────────────────────────────────────────
ALLOWED_ORIGINS=https://apuracao.seucliente.com.br

# ─── Cookie Seguro ─────────────────────────────────────────────
COOKIE_SECURE=true

# ─── Webhook RFB ───────────────────────────────────────────────
# Segredo combinado com a RFB (informado no cadastro do webhook)
RFB_WEBHOOK_SECRET=<segredo_acordado_com_rfb>

# ─── E-mail (Reset de Senha) ────────────────────────────────────
SMTP_HOST=smtp.hostinger.com
SMTP_PORT=465
SMTP_USER=noreply@seucliente.com.br
SMTP_PASS=<senha_smtp>
SMTP_FROM=noreply@seucliente.com.br

# ─── URL Pública ───────────────────────────────────────────────
APP_URL=https://apuracao.seucliente.com.br
```

### Como gerar segredos

```bash
# JWT_SECRET e ENCRYPTION_KEY (run localmente):
openssl rand -hex 32

# Exemplo de saída (NÃO use este valor):
# a3f8c2e1d4b7a9f0e3c6b5d8a1f4e7c2b5d8a1f4e7c2b5d8a1f4e7c2b5d8a1
```

---

## 6. Configuração de Domínio e SSL no Coolify

1. Em **Application → Domains**, adicione: `apuracao.seucliente.com.br`
2. Marque **Force HTTPS**
3. Coolify configura Traefik + Let's Encrypt automaticamente
4. Aguarde emissão do certificado (~30 segundos após DNS propagar)

---

## 7. Primeiro Deploy

1. No Coolify: **Deploy** → aguardar build (~3-5 minutos)
2. O container executa automaticamente as migrações na startup
3. Verificar logs: **Application → Logs**

Saída esperada nos logs:
```
Migration 001_initial_schema.sql executed successfully.
Migration 002_...sql executed successfully.
...
Migration 073_fix_mv_malha_fina_resumo.sql executed successfully.
Server starting on port 8081
```

---

## 8. Pós-Instalação: Primeiro Acesso

### 8.1 Criar usuário administrador

A aplicação não cria usuário admin automaticamente. Use a rota de registro:

```
https://apuracao.seucliente.com.br/register
```

Ou via API (se o endpoint de registro estiver ativo):
```bash
curl -X POST https://apuracao.seucliente.com.br/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"name":"Admin","email":"admin@seucliente.com.br","password":"<senha>"}'
```

Após criar o usuário, promova para admin direto no banco:
```sql
UPDATE users SET role = 'admin' WHERE email = 'admin@seucliente.com.br';
```

### 8.2 Configurar Ambiente / Grupo / Empresa

Via interface:
1. Login como admin
2. Criar **Ambiente** (ex: "Produção")
3. Criar **Grupo** (ex: "Ferreira Costa")
4. Criar **Empresa** com CNPJ base
5. Associar usuários à empresa

### 8.3 Configurar Credenciais RFB

1. Menu **Configurações → Credenciais RFB**
2. Informar Client ID e Client Secret fornecidos pela RFB
3. Selecionar ambiente: `producao` ou `producao_restrita`
4. Configurar horário de agendamento automático

---

## 9. Checklist de Go-Live

- [ ] DNS propagado (verificar com `dig apuracao.seucliente.com.br`)
- [ ] HTTPS funcionando (certificado Let's Encrypt emitido)
- [ ] Login funcionando
- [ ] `DATABASE_URL` com usuário de mínimos privilégios
- [ ] `JWT_SECRET` ≥ 64 chars, aleatório, não reutilizado
- [ ] `ENCRYPTION_KEY` diferente do `JWT_SECRET`
- [ ] `RFB_WEBHOOK_SECRET` sincronizado com a RFB
- [ ] `ALLOWED_ORIGINS` apenas com domínio de produção
- [ ] `COOKIE_SECURE=true`
- [ ] Backup automático do volume PostgreSQL configurado (Coolify ou AWS Backup)
- [ ] PostgreSQL **não** exposto à internet (apenas interno ao Docker network)
- [ ] Migrações executadas (todas as 073 sem erro nos logs)
- [ ] Primeiro usuário admin criado e role promovida
- [ ] Credenciais RFB configuradas e testadas
- [ ] E-mail de reset de senha testado
- [ ] SMTP com TLS funcionando
- [ ] Logs sem secrets aparecendo

---

## 10. Manutenção

### Atualização de versão

O Coolify monitora o branch `main`. A cada push:
1. Build automático dispara
2. Container novo substitui o anterior (zero-downtime via Traefik)
3. Migrações novas executam na startup do novo container

### Backup do banco

```bash
# Manual (executar no host ou via container)
docker exec <container_postgres> pg_dump -U fb_apu02 fb_apu02 | gzip > backup_$(date +%Y%m%d).sql.gz
```

Para automação, usar AWS Backup com o volume EBS ou script cron no EC2.

### Ver logs em tempo real

```bash
docker logs -f <container_fb_apu02> 2>&1
```

### Acesso emergencial ao banco

```bash
docker exec -it <container_postgres> psql -U fb_apu02 -d fb_apu02
```

---

## 11. ERP Bridge (Instalação no cliente)

O ERP Bridge é executado na rede interna do cliente (servidor Windows com acesso ao Oracle):

1. Copiar pasta `erp-bridge/` para o servidor Windows
2. Instalar Python 3.x + Oracle Instant Client 23.0
3. `pip install -r requirements.txt`
4. Editar `config.yaml`:

```yaml
api:
  base_url: https://apuracao.seucliente.com.br
  email: usuario@seucliente.com.br
  password: <senha_do_usuario_fbtax>

oracle:
  dsn: <HOST_ORACLE>:<PORTA>/<SERVICE>
  user: <USUARIO_ORACLE>
  password: <SENHA_ORACLE>

periodo: "202601"   # AAAAMM

servidores:
  - nome: "FCSAL"
    dsn: <DSN_SERVIDOR_1>
  - nome: "FCREC"
    dsn: <DSN_SERVIDOR_2>
```

5. Executar: `executar.bat`
6. O `tracker.db` mantém controle de chaves já enviadas — não deletar entre execuções
