#!/bin/bash
# Setup completo do ambiente de desenvolvimento WSL — FB_APU02
set -e

PROJECT_DIR="/home/claudiobezerra/projetos/FB_APU02"
DB_NAME="fiscal_db"
DB_USER="postgres"
DB_PASS="postgres"

echo "=========================================="
echo "  FB_APU02 — Setup Ambiente Dev WSL"
echo "=========================================="

# ── 1. PostgreSQL ──────────────────────────────
echo ""
echo "[1/4] Configurando PostgreSQL..."

# Garante que o cluster está rodando
pg_ctlcluster 18 main start 2>/dev/null || true
sleep 1

# Configura pg_hba.conf para aceitar conexão local com senha
HBA_FILE=$(psql -U postgres -tAc "SHOW hba_file;" 2>/dev/null || find /etc/postgresql -name pg_hba.conf | head -1)
echo "  pg_hba.conf: $HBA_FILE"

# Adiciona regra md5 para localhost se não existir
if ! grep -q "^host.*fiscal_db.*127.0.0.1" "$HBA_FILE" 2>/dev/null; then
    # Insere antes da primeira linha host existente
    sed -i '/^host/i host    all             postgres        127.0.0.1\/32            scram-sha-256' "$HBA_FILE"
fi

# Recarrega configuração
pg_ctlcluster 18 main reload 2>/dev/null || service postgresql reload

# Define senha do usuário postgres
psql -U postgres -c "ALTER USER postgres WITH PASSWORD '$DB_PASS';" 2>/dev/null || \
  su -c "psql -c \"ALTER USER postgres WITH PASSWORD '$DB_PASS';\"" postgres

# Cria banco se não existir
if ! psql -U postgres -lqt 2>/dev/null | grep -qw "$DB_NAME"; then
    echo "  Criando banco $DB_NAME..."
    psql -U postgres -c "CREATE DATABASE $DB_NAME;" 2>/dev/null || \
      su -c "createdb $DB_NAME" postgres
else
    echo "  Banco $DB_NAME já existe."
fi

echo "  PostgreSQL OK"

# ── 2. Verifica se .env já existe ─────────────
echo ""
echo "[2/4] Criando .env para backend..."

ENV_FILE="$PROJECT_DIR/backend/.env"

if [ -f "$ENV_FILE" ]; then
    echo "  .env já existe em backend/.env — pulando criação."
else
    JWT_SECRET=$(openssl rand -hex 32)
    ENCRYPTION_KEY=$(openssl rand -hex 32)

    cat > "$ENV_FILE" <<EOF
# Ambiente de desenvolvimento local — gerado por setup_dev_env.sh
DATABASE_URL=postgres://postgres:postgres@localhost:5432/fiscal_db?sslmode=disable
JWT_SECRET=$JWT_SECRET
ENCRYPTION_KEY=$ENCRYPTION_KEY
PORT=8081
COOKIE_SECURE=false
APP_URL=http://localhost:3000
ALLOWED_ORIGINS=http://localhost:3000

# SMTP (opcional em dev — deixe vazio para desabilitar emails)
SMTP_HOST=
SMTP_PORT=
SMTP_USER=
SMTP_PASSWORD=
SMTP_FROM=

# RFB (opcional em dev)
RFB_API_URL=
RFB_TOKEN_URL=
RFB_WEBHOOK_URL=
RFB_WEBHOOK_SECRET=
EOF

    chown claudiobezerra:claudiobezerra "$ENV_FILE"
    echo "  .env criado em backend/.env"
fi

# Cria symlink na raiz para o godotenv achar (main.go faz godotenv.Load() da raiz)
if [ ! -f "$PROJECT_DIR/.env" ] && [ -f "$ENV_FILE" ]; then
    ln -sf "$ENV_FILE" "$PROJECT_DIR/.env"
    echo "  Symlink .env na raiz criado"
fi

# ── 3. Frontend — npm install ──────────────────
echo ""
echo "[3/4] Instalando dependências do frontend..."
cd "$PROJECT_DIR/frontend"
npm install --silent
echo "  npm install OK"

# ── 4. Compila backend ─────────────────────────
echo ""
echo "[4/4] Compilando backend Go (verifica dependências)..."
cd "$PROJECT_DIR/backend"
go build -o /tmp/fb_apu02_check . && rm /tmp/fb_apu02_check
echo "  go build OK"

# ── Resumo ─────────────────────────────────────
echo ""
echo "=========================================="
echo "  Setup concluído!"
echo "=========================================="
echo ""
echo "  Para iniciar o desenvolvimento:"
echo ""
echo "  Terminal 1 — Backend:"
echo "    cd $PROJECT_DIR/backend && go run main.go"
echo ""
echo "  Terminal 2 — Frontend:"
echo "    cd $PROJECT_DIR/frontend && npm run dev"
echo ""
echo "  URL: http://localhost:3000"
echo "  API: http://localhost:8081"
echo ""
