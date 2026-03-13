#!/bin/bash
set -e

# FBTax Cloud - Script de Instalacao
# Uso: chmod +x install.sh && ./install.sh

COMPOSE_FILE="docker-compose.yml"
ENV_FILE=".env"
ENV_TEMPLATE=".env.template"

echo "========================================="
echo "  FBTax Cloud - Instalador"
echo "========================================="
echo ""

# 1. Verificar se esta no diretorio correto
if [ ! -f "$COMPOSE_FILE" ]; then
    echo "[ERRO] Arquivo $COMPOSE_FILE nao encontrado."
    echo "Execute este script dentro do diretorio installer/"
    exit 1
fi

# 2. Verificar/instalar Docker
if ! command -v docker &> /dev/null; then
    echo "[INFO] Docker nao encontrado. Instalando..."
    curl -fsSL https://get.docker.com | sh
    sudo systemctl enable docker
    sudo systemctl start docker
    echo "[OK] Docker instalado com sucesso."
else
    echo "[OK] Docker encontrado: $(docker --version)"
fi

# 3. Verificar Docker Compose
if ! docker compose version &> /dev/null; then
    echo "[ERRO] Docker Compose (plugin) nao encontrado."
    echo "Instale com: sudo apt install docker-compose-plugin"
    exit 1
else
    echo "[OK] Docker Compose encontrado: $(docker compose version --short)"
fi

# 4. Verificar se usuario pode rodar Docker
if ! docker info &> /dev/null; then
    echo "[AVISO] Seu usuario nao tem permissao para rodar Docker."
    echo "Execute: sudo usermod -aG docker \$USER"
    echo "Depois faca logout e login novamente."
    echo "Ou execute este script com sudo."
    exit 1
fi

# 5. Configurar .env
if [ ! -f "$ENV_FILE" ]; then
    echo ""
    echo "[INFO] Criando arquivo de configuracao .env..."
    cp "$ENV_TEMPLATE" "$ENV_FILE"

    # Gerar JWT_SECRET automaticamente
    JWT=$(openssl rand -hex 32 2>/dev/null || head -c 64 /dev/urandom | od -An -tx1 | tr -d ' \n')
    sed -i "s/TROCAR_JWT_SECRET_AQUI/$JWT/" "$ENV_FILE"

    # Gerar senha do banco automaticamente
    DB_PASS=$(openssl rand -hex 16 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')
    sed -i "s/TROCAR_SENHA_AQUI/$DB_PASS/" "$ENV_FILE"

    echo "[OK] Arquivo .env criado com senhas geradas automaticamente."
    echo ""
    echo "========================================="
    echo "  IMPORTANTE: Configure o email (SMTP)"
    echo "========================================="
    echo ""
    echo "Edite o arquivo .env e preencha:"
    echo "  - SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASSWORD, SMTP_FROM"
    echo "  - APP_URL (URL de acesso ao sistema)"
    echo ""
    read -p "Pressione ENTER apos editar o .env (ou ENTER para continuar sem SMTP)... "
else
    echo "[OK] Arquivo .env ja existe."
fi

# 6. Puxar imagens
echo ""
echo "[INFO] Baixando imagens Docker..."
docker compose pull

# 7. Subir servicos
echo ""
echo "[INFO] Iniciando servicos..."
docker compose up -d

# 8. Health check
echo ""
echo "[INFO] Aguardando sistema iniciar..."
MAX_ATTEMPTS=20
for i in $(seq 1 $MAX_ATTEMPTS); do
    if curl -sf http://localhost:8081/api/health > /dev/null 2>&1; then
        echo ""
        echo "========================================="
        echo "  FBTax Cloud instalado com sucesso!"
        echo "========================================="
        echo ""
        echo "  Acesse: http://$(hostname -I | awk '{print $1}')"
        echo "  API:    http://$(hostname -I | awk '{print $1}'):8081/api/health"
        echo ""
        echo "  Comandos uteis:"
        echo "    docker compose logs -f    # Ver logs"
        echo "    docker compose ps         # Status dos servicos"
        echo "    ./update.sh               # Atualizar sistema"
        echo ""
        exit 0
    fi
    echo "  Tentativa $i/$MAX_ATTEMPTS - aguardando..."
    sleep 5
done

echo ""
echo "[AVISO] Sistema ainda iniciando. Verifique os logs:"
echo "  docker compose logs -f"
