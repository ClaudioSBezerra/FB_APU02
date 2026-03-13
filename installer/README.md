# FBTax Cloud - Instalador

Instalador simplificado para deploy do FBTax Cloud em servidores de clientes.

## Requisitos

- Servidor Linux (Ubuntu 20.04+, Debian 11+, RHEL 8+)
- 2GB+ RAM (recomendado: 4GB+)
- Docker e Docker Compose (instalados automaticamente se necessário)
- Portas 80 e 8081 liberadas no firewall

## Instalação Rápida

```bash
# 1. Copiar arquivos do instalador para o servidor
scp -r installer/ usuario@servidor:~/fbtax/

# 2. Conectar no servidor
ssh usuario@servidor

# 3. Executar instalador
cd ~/fbtax
chmod +x install.sh update.sh
sudo ./install.sh
```

O script irá:
- ✅ Verificar/instalar Docker automaticamente
- ✅ Gerar senhas seguras (JWT + banco de dados)
- ✅ Criar arquivo `.env` com configurações
- ✅ Baixar imagens do GitHub Container Registry
- ✅ Subir todos os serviços (API, Web, PostgreSQL, Redis)

## Configuração

### Variáveis de Ambiente

Edite o arquivo `.env` antes ou depois da instalação:

```bash
nano .env
```

**Variáveis importantes:**

| Variável | Descrição | Exemplo |
|----------|-----------|---------|
| `DB_PASSWORD` | Senha do banco (gerada automaticamente) | `senha_segura_123` |
| `JWT_SECRET` | Chave JWT (gerada automaticamente) | `abc123...` |
| `SMTP_HOST` | Servidor SMTP para emails | `smtp.hostinger.com` |
| `SMTP_PORT` | Porta SMTP | `587` ou `465` |
| `SMTP_USER` | Usuário SMTP | `noreply@empresa.com` |
| `SMTP_PASSWORD` | Senha SMTP | `senha_smtp` |
| `SMTP_FROM` | Remetente dos emails | `Sistema <noreply@empresa.com>` |
| `APP_URL` | URL pública do sistema | `http://servidor.empresa.com` |
| `WEB_PORT` | Porta HTTP (padrão: 80) | `80` ou `8080` |

**⚠️ IMPORTANTE:** Senhas **não podem conter** caracteres especiais como `#`, `@`, `:` ou `/` pois quebram a URL de conexão do PostgreSQL.

### Após Configurar o SMTP

```bash
sudo docker compose restart api
```

## Comandos Úteis

```bash
# Ver status dos serviços
sudo docker compose ps

# Ver logs em tempo real
sudo docker compose logs -f

# Ver logs apenas da API
sudo docker compose logs -f api

# Reiniciar todos os serviços
sudo docker compose restart

# Parar todos os serviços
sudo docker compose down

# Parar e remover volumes (CUIDADO: apaga banco!)
sudo docker compose down -v
```

## Atualização

Para atualizar o sistema para uma versão mais recente:

```bash
cd ~/fbtax
sudo ./update.sh
```

O script irá:
- Baixar novas imagens do GHCR
- Reiniciar serviços com as novas versões
- Verificar health check
- Limpar imagens antigas

## Verificação

Após instalação, verifique:

```bash
# Health check da API
curl http://localhost/api/health

# Deve retornar JSON com status "running"
```

**Acessar no browser:**
- Frontend: `http://<ip-do-servidor>`
- Health API: `http://<ip-do-servidor>/api/health`

## Arquitetura

```
┌─────────────────────────────────────────┐
│           fbtax-web (Nginx)             │  Porta 80
│  - Frontend React                       │
│  - Proxy reverso para API               │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│           fbtax-api (Go)                │  Porta 8081 (interna)
│  - Backend REST API                     │
│  - Migrations automáticas               │
└──────┬──────────────────────────┬───────┘
       │                          │
       ▼                          ▼
┌──────────────┐          ┌──────────────┐
│  fbtax-db    │          │ fbtax-redis  │
│ PostgreSQL   │          │    Cache     │
└──────────────┘          └──────────────┘
```

## Solução de Problemas

### API não conecta no banco

```bash
# Ver logs da API
sudo docker compose logs api

# Erro comum: "password authentication failed"
# Solução: Recriar volumes com senha correta
sudo docker compose down -v
sudo docker compose up -d
```

### Porta 80 já em uso

Edite `.env` e mude `WEB_PORT`:

```bash
WEB_PORT=8080
sudo docker compose up -d
```

### Erro "port is already allocated"

Significa que a porta já está em uso. Verifique:

```bash
sudo netstat -tulpn | grep :80
```

Mate o processo conflitante ou mude `WEB_PORT` no `.env`.

### Verificar uso de memória

```bash
docker stats
```

Se estiver acima de 90%, considere aumentar RAM do servidor ou otimizar limites no `docker-compose.yml`.

## Firewall

### Ubuntu/Debian (UFW)

```bash
sudo ufw allow 80/tcp
sudo ufw allow 8081/tcp
sudo ufw reload
```

### RHEL/CentOS (firewalld)

```bash
sudo firewall-cmd --permanent --add-port=80/tcp
sudo firewall-cmd --permanent --add-port=8081/tcp
sudo firewall-cmd --reload
```

### Provedores Cloud

- **AWS:** Liberar portas no Security Group
- **Azure:** Liberar portas no Network Security Group
- **Oracle Cloud:** Liberar portas na Security List + iptables
- **Google Cloud:** Liberar portas nas Firewall Rules

## Backup

Os volumes Docker estão em:

```bash
/var/lib/docker/volumes/fbtax_postgres_data
/var/lib/docker/volumes/fbtax_api_uploads
```

Para backup completo:

```bash
# Backup do banco
sudo docker exec fbtax-db pg_dump -U fbtax fbtax_cloud > backup.sql

# Backup dos uploads
sudo tar -czf uploads_backup.tar.gz /var/lib/docker/volumes/fbtax_api_uploads
```

## Suporte

- Repositório: https://github.com/ClaudioSBezerra/FB_APU01
- Issues: https://github.com/ClaudioSBezerra/FB_APU01/issues
