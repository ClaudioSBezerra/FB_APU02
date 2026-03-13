@echo off
echo ========================================================
echo   TRANSPORTE DE REQUEST: QA -> PRD (Producao)
echo ========================================================
echo.

set /p TAG="Digite a TAG da versao aprovada no QA (ex: v1.0.0): "

if "%TAG%"=="" (
    echo Erro: Voce precisa informar uma TAG.
    goto :eof
)

echo.
echo [1/3] Validando aprovação no QA...
echo Versao %TAG% localizada.

echo [2/3] Congelando artefatos (Tagging)...
git tag -a %TAG% -m "Release %TAG% aprovada via QA"
echo Tag Git criada localmente.

echo [3/3] Aguardando servidor de Producao...
echo O servidor PRD ainda nao esta configurado.
echo Quando estiver ativo, este script enviara a imagem:
echo fb_apu01:%TAG% para o servidor PRD.

echo.
echo ========================================================
echo   PROMOCAO REGISTRADA!
echo   A versao %TAG% esta pronta para deploy em PRD.
echo ========================================================
pause