#!/bin/bash
# Túnel SSH para o banco de dados de produção (Hostinger)
# Uso: ./scripts/tunnel-prod-db.sh [start|stop|status]

SSH_KEY="$HOME/.ssh/coolify_hostinger"
SSH_HOST="root@76.13.171.196"
REMOTE_DB_HOST="10.0.3.3"
REMOTE_DB_PORT=5432
LOCAL_PORT=5435
PID_FILE="/tmp/tunnel-prod-db.pid"

start() {
  if [ -f "$PID_FILE" ] && kill -0 "$(cat $PID_FILE)" 2>/dev/null; then
    echo "Túnel já está rodando (PID: $(cat $PID_FILE))"
    echo "Porta local: $LOCAL_PORT"
    return
  fi

  echo "Iniciando túnel SSH para o banco de produção..."
  ssh -i "$SSH_KEY" \
      -o StrictHostKeyChecking=no \
      -o ServerAliveInterval=30 \
      -o ServerAliveCountMax=3 \
      -N -L "0.0.0.0:$LOCAL_PORT:$REMOTE_DB_HOST:$REMOTE_DB_PORT" \
      "$SSH_HOST" &

  echo $! > "$PID_FILE"
  sleep 2

  if kill -0 "$(cat $PID_FILE)" 2>/dev/null; then
    echo "Túnel ativo!"
    echo ""
    echo "Conexão VS Code (pgsql extension):"
    echo "  Host:     172.19.217.72"
    echo "  Port:     $LOCAL_PORT"
    echo "  Database: fb_apu01"
    echo "  Username: postgres"
    echo "  SSL:      disable"
    echo ""
    echo "Para encerrar: ./scripts/tunnel-prod-db.sh stop"
  else
    echo "Falha ao iniciar o túnel."
    rm -f "$PID_FILE"
  fi
}

stop() {
  if [ -f "$PID_FILE" ] && kill -0 "$(cat $PID_FILE)" 2>/dev/null; then
    kill "$(cat $PID_FILE)"
    rm -f "$PID_FILE"
    echo "Túnel encerrado."
  else
    echo "Nenhum túnel ativo."
    rm -f "$PID_FILE"
  fi
}

status() {
  if [ -f "$PID_FILE" ] && kill -0 "$(cat $PID_FILE)" 2>/dev/null; then
    echo "Túnel ATIVO (PID: $(cat $PID_FILE)) — porta local: $LOCAL_PORT"
  else
    echo "Túnel INATIVO"
  fi
}

case "${1:-start}" in
  start)  start ;;
  stop)   stop ;;
  status) status ;;
  *)      echo "Uso: $0 [start|stop|status]" ;;
esac
