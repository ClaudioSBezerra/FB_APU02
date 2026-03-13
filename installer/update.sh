#!/bin/bash
set -e

# FBTax Cloud - Script de Atualizacao
# Uso: ./update.sh

echo "========================================="
echo "  FBTax Cloud - Atualizacao"
echo "========================================="
echo ""

if [ ! -f "docker-compose.yml" ]; then
    echo "[ERRO] Execute este script dentro do diretorio installer/"
    exit 1
fi

# 1. Baixar novas imagens
echo "[INFO] Baixando imagens atualizadas..."
docker compose pull

# 2. Reiniciar servicos com novas imagens
echo "[INFO] Reiniciando servicos..."
docker compose up -d

# 3. Health check
echo "[INFO] Verificando sistema..."
MAX_ATTEMPTS=20
for i in $(seq 1 $MAX_ATTEMPTS); do
    if curl -sf http://localhost:8081/api/health > /dev/null 2>&1; then
        echo ""
        echo "[OK] Atualizacao concluida com sucesso!"
        echo ""
        # Limpar imagens antigas nao utilizadas
        echo "[INFO] Removendo imagens antigas..."
        docker image prune -f
        exit 0
    fi
    echo "  Tentativa $i/$MAX_ATTEMPTS - aguardando..."
    sleep 5
done

echo ""
echo "[AVISO] Sistema ainda iniciando. Verifique os logs:"
echo "  docker compose logs -f"
