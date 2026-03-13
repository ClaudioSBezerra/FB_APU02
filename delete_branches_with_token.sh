#!/bin/bash
# Script para deletar branches antigos usando token
# Uso: ./delete_branches_with_token.sh SEU_TOKEN_AQUI

TOKEN=$1

if [ -z "$TOKEN" ]; then
    echo "Uso: $0 SEU_GITHUB_TOKEN"
    echo ""
    echo "Para obter um token:"
    echo "1. Acesse: https://github.com/settings/tokens"
    echo "2. Clique em 'Generate new token (classic)'"
    echo "3. Marque 'delete_repo' e clique em 'Generate token'"
    echo "4. Copie o token gerado"
    exit 1
fi

echo "=== DELETANDO BRANCHES ANTIGOS ==="
echo ""
echo "Branch de segurança já criado: backup-seguranca-antes-limpeza-20260206"
echo ""

echo "1. Deletando backup-02022026..."
git push https://ClaudioSBezerra:$TOKEN@github.com/ClaudioSBezerra/FB_APU01.git --delete backup-02022026

echo ""
echo "2. Deletando backup/power-outage-save..."
git push https://ClaudioSBezerra:$TOKEN@github.com/ClaudioSBezerra/FB_APU01.git --delete backup/power-outage-save

echo ""
echo "✅ Limpeza concluída!"
