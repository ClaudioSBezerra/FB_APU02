#!/bin/bash
# Script para limpar branches antigos do GitHub
# Data: 06/02/2026

echo "=== LIMPEZA DE BRANCHES ANTIGOS ==="
echo ""
echo "Branches que serão deletados:"
echo "  - origin/backup-02022026 (backup de 02/02/2026)"
echo "  - origin/backup/power-outage-save (backup de 02/02/2026)"
echo ""
echo "Todos os commits desses branches já estão no main."
echo ""

# Criar branch de backup local antes de deletar remotos
echo "1. Criando branch de segurança local..."
git branch backup-seguranca-antes-limpeza-$(date +%Y%m%d)
echo "   ✓ Branch backup-seguranca-antes-limpeza-$(date +%Y%m%d) criado"
echo ""

read -p "2. Deseja deletar os branches remotos antigos? (s/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Ss]$ ]]; then
    echo ""
    echo "Deletando branches remotos..."

    echo "   Deletando backup-02022026..."
    git push origin --delete backup-02022026

    echo "   Deletando backup/power-outage-save..."
    git push origin --delete backup/power-outage-save

    echo ""
    echo "✅ Limpeza concluída!"
    echo ""
    echo "Branches restantes:"
    git branch -r | grep origin
else
    echo "❌ Operação cancelada."
fi
