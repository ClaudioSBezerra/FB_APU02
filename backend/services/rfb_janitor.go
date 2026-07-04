package services

import (
	"database/sql"
	"log"
	"os"
	"strconv"
	"time"
)

// StartRawJSONJanitor limpa o raw_json de solicitações RFB concluídas mais
// antigas que RFB_RAW_JSON_RETENTION_DAYS dias, controlando o crescimento da
// tabela rfb_requests (o JSON completo da apuração só é necessário para
// reprocessamento; os dados normalizados ficam em rfb_debitos/rfb_creditos).
//
// Opt-in explícito: com a env ausente ou 0 nada é apagado.
// Só afeta status 'completed' — requests com erro mantêm o raw_json para debug.
func StartRawJSONJanitor(dbFn func() *sql.DB) {
	days, _ := strconv.Atoi(os.Getenv("RFB_RAW_JSON_RETENTION_DAYS"))
	if days <= 0 {
		log.Println("[RFB Janitor] Retenção de raw_json desabilitada (RFB_RAW_JSON_RETENTION_DAYS ausente ou 0)")
		return
	}
	log.Printf("[RFB Janitor] Retenção de raw_json ativa: %d dias (verificação a cada 12h)", days)

	// Aguarda a conexão inicial do banco antes da primeira varredura
	time.Sleep(1 * time.Minute)

	for {
		if db := dbFn(); db != nil {
			res, err := db.Exec(`
				UPDATE rfb_requests SET raw_json = NULL
				WHERE raw_json IS NOT NULL
				  AND status = 'completed'
				  AND updated_at < NOW() - make_interval(days => $1)
			`, days)
			if err != nil {
				log.Printf("[RFB Janitor] Erro ao limpar raw_json: %v", err)
			} else if n, _ := res.RowsAffected(); n > 0 {
				log.Printf("[RFB Janitor] raw_json limpo em %d solicitações concluídas há mais de %d dias", n, days)
			}
		}
		time.Sleep(12 * time.Hour)
	}
}
