#!/bin/bash
set -e

# FBTax Cloud - Setup Servidor Cliente AWS
# Uso: chmod +x setup.sh && ./setup.sh

COMPOSE_FILE="docker-compose.yml"
ENV_FILE=".env"
ENV_TEMPLATE=".env.template"
GHCR_IMAGE_API="ghcr.io/claudiosbezerra/fb_apu01-api:latest"
GHCR_IMAGE_WEB="ghcr.io/claudiosbezerra/fb_apu01-web:latest"

echo "========================================="
echo "  FBTax Cloud - Setup Cliente AWS"
echo "========================================="
echo ""

# 1. Verificar se esta no diretorio correto
if [ ! -f "$COMPOSE_FILE" ]; then
    echo "[ERRO] Arquivo $COMPOSE_FILE nao encontrado."
    echo "Execute este script dentro do diretorio installer/cliente-aws/"
    exit 1
fi

# 2. Verificar/instalar Docker
if ! command -v docker &> /dev/null; then
    echo "[INFO] Docker nao encontrado. Instalando..."
    curl -fsSL https://get.docker.com | sh
    sudo systemctl enable docker
    sudo systemctl start docker
    sudo usermod -aG docker "$USER"
    echo "[OK] Docker instalado. Pode ser necessario fazer logout/login para usar sem sudo."
else
    echo "[OK] Docker encontrado: $(docker --version)"
fi

# 3. Verificar Docker Compose
if ! docker compose version &> /dev/null; then
    echo "[ERRO] Docker Compose (plugin) nao encontrado."
    echo "Instale com: sudo apt install docker-compose-plugin"
    exit 1
else
    echo "[OK] Docker Compose: $(docker compose version --short)"
fi

# 4. Autenticar no GHCR (GitHub Container Registry)
echo ""
echo "[INFO] Verificando autenticacao no GitHub Container Registry (GHCR)..."
if docker pull ghcr.io/claudiosbezerra/fb_apu01-api:latest > /dev/null 2>&1; then
    echo "[OK] GHCR ja autenticado."
else
    echo "Voce precisara de um GitHub Personal Access Token (PAT) com escopo 'read:packages'."
    echo "Gere em: https://github.com/settings/tokens"
    echo ""
    read -p "  GitHub username: " GHCR_USER
    read -s -p "  GitHub PAT (read:packages): " GHCR_TOKEN
    echo ""
    echo "$GHCR_TOKEN" | docker login ghcr.io -u "$GHCR_USER" --password-stdin
    echo "[OK] Autenticado no GHCR."
fi

# 5. Configurar .env
if [ ! -f "$ENV_FILE" ]; then
    echo ""
    echo "[INFO] Criando arquivo de configuracao .env..."
    cp "$ENV_TEMPLATE" "$ENV_FILE"

    JWT=$(openssl rand -hex 32 2>/dev/null || head -c 64 /dev/urandom | od -An -tx1 | tr -d ' \n')
    sed -i "s/TROCAR_JWT_SECRET_AQUI/$JWT/" "$ENV_FILE"

    DB_PASS=$(openssl rand -hex 16 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')
    sed -i "s/TROCAR_SENHA_AQUI/$DB_PASS/" "$ENV_FILE"

    echo "[OK] Arquivo .env criado com senhas geradas automaticamente."
    echo ""
    echo "========================================="
    echo "  IMPORTANTE: Configure o email (SMTP)"
    echo "========================================="
    echo "  Edite o arquivo .env e preencha:"
    echo "  - SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASSWORD, SMTP_FROM"
    echo ""
    read -p "Pressione ENTER apos editar o .env (ou ENTER para continuar sem SMTP)... "
else
    echo "[OK] Arquivo .env ja existe."
fi

# 6. Puxar imagens
echo ""
echo "[INFO] Baixando imagens Docker do GHCR..."
docker compose pull

# 7. Subir servicos
echo ""
echo "[INFO] Iniciando servicos..."
docker compose up -d

# 8. Health check
echo ""
echo "[INFO] Aguardando sistema iniciar..."
PORT=$(grep WEB_PORT "$ENV_FILE" | cut -d= -f2 | tr -d ' ' || echo "3006")
MAX_ATTEMPTS=20
for i in $(seq 1 $MAX_ATTEMPTS); do
    if curl -sf "http://localhost:${PORT}/api/health" > /dev/null 2>&1; then
        echo ""
        echo "========================================="
        echo "  FBTax Cloud instalado com sucesso!"
        echo "========================================="
        echo ""
        echo "  Acesse: http://$(hostname -I | awk '{print $1}'):${PORT}"
        echo ""
        break
    fi
    echo "  Tentativa $i/$MAX_ATTEMPTS - aguardando..."
    sleep 5
done

# 9. Instrucoes para o GitHub Actions Runner
echo ""
echo "========================================="
echo "  Proximo passo: GitHub Actions Runner"
echo "========================================="
echo ""
echo "Para deploy automatico a cada push no main, instale o runner:"
echo ""
echo "  1. Gere um Registration Token em:"
echo "     https://github.com/claudiosbezerra/FB_APU02/settings/actions/runners/new"
echo ""
echo "  2. Execute no servidor (como claudio):"
echo "     mkdir /opt/apps/actions-runner && cd /opt/apps/actions-runner"
echo "     curl -O -L https://github.com/actions/runner/releases/latest/download/actions-runner-linux-x64-2.321.0.tar.gz"
echo "     tar xzf ./actions-runner-linux-x64-2.321.0.tar.gz"
echo "     ./config.sh --url https://github.com/claudiosbezerra/FB_APU02 --token <TOKEN> --labels cliente-aws --unattended"
echo "     sudo ./svc.sh install && sudo ./svc.sh start"
echo ""
echo "  3. O runner deve aparecer em:"
echo "     https://github.com/claudiosbezerra/FB_APU02/settings/actions/runners"
echo ""
