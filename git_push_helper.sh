#!/bin/bash
# Script para facilitar o push para o GitHub com token
# Uso: ./git_push_helper.sh SEU_TOKEN_AQUI

TOKEN=$1

if [ -z "$TOKEN" ]; then
    echo "Uso: $0 SEU_GITHUB_TOKEN"
    echo ""
    echo "Para obter um token:"
    echo "1. Acesse: https://github.com/settings/tokens"
    echo "2. Clique em 'Generate new token (classic)'"
    echo "3. Marque 'repo' e clique em 'Generate token'"
    echo "4. Copie o token gerado"
    echo ""
    echo "Depois execute: $0 SEU_TOKEN_COPIADO"
    exit 1
fi

echo "Fazendo push para o GitHub..."
git push https://ClaudioSBezerra:$TOKEN@github.com/ClaudioSBezerra/FB_APU01.git main

echo ""
echo "Se o push foi bem-sucedido, você pode configurar o git para não precisar do token novamente:"
echo "  git config --global credential.helper store"
