#!/bin/bash
set -e

echo ">>> Iniciando configuração do ambiente WSL..."

# 1. Instalar Go 1.22.5
if ! command -v go &> /dev/null; then
    echo ">>> Instalando Go 1.22.5..."
    apt-get update > /dev/null
    apt-get install -y wget > /dev/null
    wget -q https://go.dev/dl/go1.22.5.linux-amd64.tar.gz
    rm -rf /usr/local/go && tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz
    rm go1.22.5.linux-amd64.tar.gz
    ln -sf /usr/local/go/bin/go /usr/bin/go
else
    echo ">>> Go já instalado."
fi

# 2. Configurar PostgreSQL
echo ">>> Iniciando PostgreSQL..."
service postgresql start

echo ">>> Configurando Banco de Dados..."
# Garante que o usuário postgres tem a senha correta (usuário já existe por padrão)
su - postgres -c "psql -c \"ALTER USER postgres WITH PASSWORD 'postgres';\""
# Garante privilégios
su - postgres -c "psql -c \"ALTER USER postgres WITH SUPERUSER;\""

# Cria banco se não existir
if ! su - postgres -c "psql -lqt" | cut -d \| -f 1 | grep -qw fiscal_db; then
    echo ">>> Criando banco de dados fiscal_db..."
    su - postgres -c "createdb fiscal_db"
else
    echo ">>> Banco de dados fiscal_db já existe."
fi

echo ">>> Ambiente WSL Configurado com Sucesso!"
go version
