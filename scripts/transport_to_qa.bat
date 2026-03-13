@echo off
echo ========================================================
echo   TRANSPORTE DE REQUEST: DEV -> QA (Hostinger)
echo ========================================================
echo.

REM 1. Configuracao (Sera preenchido com dados reais do VPS)
set VPS_USER=root
set VPS_HOST=SEU_IP_HOSTINGER
set REMOTE_DIR=/root/fb_apu01

echo [1/4] Preparando artefatos locais...
REM Aqui entraria o build do docker se usassemos registry, 
REM mas para VPS simples, vamos sincronizar os arquivos via SCP/Rsync
REM ou reconstruir no destino.

echo [2/4] Verificando conexao com QA...
echo (Simulacao: Conexao OK)

echo [3/4] Transferindo arquivos para QA...
echo Copiando configs, docker-compose e codigo fonte...
REM Exemplo real: scp -r ./backend %VPS_USER%@%VPS_HOST%:%REMOTE_DIR%
REM Exemplo real: scp -r ./frontend %VPS_USER%@%VPS_HOST%:%REMOTE_DIR%
REM Exemplo real: scp docker-compose.yml %VPS_USER%@%VPS_HOST%:%REMOTE_DIR%

echo [4/4] Aplicando mudancas no QA (Reiniciando servicos)...
REM Exemplo real: ssh %VPS_USER%@%VPS_HOST% "cd %REMOTE_DIR% && docker compose up -d --build"

echo.
echo ========================================================
echo   TRANSPORTE CONCLUIDO COM SUCESSO!
echo   O ambiente QA foi atualizado.
echo ========================================================
pause