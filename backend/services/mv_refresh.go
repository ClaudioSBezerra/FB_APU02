package services

import (
	"database/sql"
	"log"
	"sync"
	"time"
)

// mvRefreshCooldown é o intervalo mínimo entre dois REFRESH da mesma view.
// Importações em chunks (upload XML) e em lotes (ERP Bridge) disparam dezenas
// de pedidos em sequência — o debounce colapsa tudo em no máximo um refresh
// a cada cooldown, sempre com um refresh final após o último pedido (trailing).
const mvRefreshCooldown = 2 * time.Minute

type mvRefreshEntry struct {
	active  bool      // goroutine de refresh em execução para esta view
	pending bool      // chegou pedido novo durante o refresh — reagendar
	last    time.Time // término do último refresh
}

var mvRefreshState = struct {
	mu      sync.Mutex
	entries map[string]*mvRefreshEntry
}{entries: map[string]*mvRefreshEntry{}}

// RequestMVRefresh agenda um REFRESH MATERIALIZED VIEW CONCURRENTLY com debounce.
// Não bloqueia o chamador. O nome da view deve ser uma constante do código —
// nunca entrada do usuário (é concatenado no SQL).
func RequestMVRefresh(db *sql.DB, view string) {
	mvRefreshState.mu.Lock()
	e, ok := mvRefreshState.entries[view]
	if !ok {
		e = &mvRefreshEntry{}
		mvRefreshState.entries[view] = e
	}
	if e.active {
		e.pending = true
		mvRefreshState.mu.Unlock()
		return
	}
	e.active = true
	mvRefreshState.mu.Unlock()

	go func() {
		for {
			mvRefreshState.mu.Lock()
			wait := mvRefreshCooldown - time.Since(e.last)
			mvRefreshState.mu.Unlock()
			if wait > 0 {
				time.Sleep(wait)
			}

			if _, err := db.Exec("REFRESH MATERIALIZED VIEW CONCURRENTLY " + view); err != nil {
				log.Printf("[MVRefresh] aviso: refresh %s: %v", view, err)
			}

			mvRefreshState.mu.Lock()
			e.last = time.Now()
			if e.pending {
				e.pending = false
				mvRefreshState.mu.Unlock()
				continue
			}
			e.active = false
			mvRefreshState.mu.Unlock()
			return
		}
	}()
}
