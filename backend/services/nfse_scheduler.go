package services

import (
	"context"
	"database/sql"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// nfseSchedulerHorarioEnv define o horário fixo e GLOBAL (HH:MM, fuso America/Sao_Paulo) em que
// o ciclo agendado de busca de NFS-e roda para TODAS as empresas. Decisão do usuário de
// 2026-10-23 (ver Boundaries da spec-nfse-agendamento-persistencia.md): ao contrário do módulo
// RFB (agendamento configurável por empresa via rfb_credentials.horario_agendamento), NFS-e não
// ganha coluna de agendamento em nfse_certificados nesta sub-spec — um único horário pra todas.
const nfseSchedulerHorarioEnv = "NFSE_SCHEDULER_HORARIO"

// nfseSchedulerHorarioDefault é usado quando NFSE_SCHEDULER_HORARIO está ausente ou em formato
// inválido — nunca derruba a inicialização do scheduler (ver I/O Matrix da spec).
const nfseSchedulerHorarioDefault = "06:00"

// nfseSchedulerConcurrencyLimit limita quantas goroutines de ExecutarCicloNFSe rodam
// simultaneamente — protege o pool de conexões do Postgres (50 abertas/15 ociosas) e evita um
// pico de tráfego incomum contra o Portal Nacional ADN se o número de empresas crescer. Valor
// fixo, não configurável (ver Spec Change Log, loopback 1, item 7, da
// spec-nfse-agendamento-persistencia.md).
const nfseSchedulerConcurrencyLimit = 10

// nfseSchedulerSemaphore é o semáforo compartilhado por todas as chamadas de
// nfseSchedulerProcessCompany — cada goroutine adquire um slot antes de chamar
// ExecutarCicloNFSe e libera ao terminar (via defer, mesmo em caso de panic).
var nfseSchedulerSemaphore = make(chan struct{}, nfseSchedulerConcurrencyLimit)

// nfseSchedulerCicloTimeout limita quanto tempo UMA empresa pode levar dentro de
// ExecutarCicloNFSe no ciclo agendado — o scheduler não tem uma requisição HTTP de origem (ao
// contrário do disparo manual, que propaga r.Context()), então usa este timeout como ctx raiz
// para nunca deixar uma goroutine presa indefinidamente.
const nfseSchedulerCicloTimeout = 10 * time.Minute

// resolveNFSeSchedulerHorario lê NFSE_SCHEDULER_HORARIO e valida o formato HH:MM
// (time.Parse("15:04", ...)). Ausente ou inválida: loga aviso e devolve o default. Um valor
// válido mas sem zero à esquerda (ex.: "6:00") é CANONICALIZADO para "06:00" — sem isso, o
// valor cru nunca bateria contra now.Format("15:04") (sempre zero-paddado) a cada tick, e o
// scheduler nunca dispararia, silenciosamente (ver Spec Change Log, loopback 1, item 4).
func resolveNFSeSchedulerHorario() string {
	v := strings.TrimSpace(os.Getenv(nfseSchedulerHorarioEnv))
	if v == "" {
		log.Printf("[NFSe Scheduler] %s não definida — usando horário padrão %s", nfseSchedulerHorarioEnv, nfseSchedulerHorarioDefault)
		return nfseSchedulerHorarioDefault
	}
	t, err := time.Parse("15:04", v)
	if err != nil {
		log.Printf("[NFSe Scheduler] %s=%q em formato inválido (esperado HH:MM): %v — usando horário padrão %s",
			nfseSchedulerHorarioEnv, v, err, nfseSchedulerHorarioDefault)
		return nfseSchedulerHorarioDefault
	}
	return t.Format("15:04")
}

// deveExecutarCicloNFSe decide se o ciclo agendado deve disparar agora: dispara se o horário
// atual (nowHHMM, "HH:MM" zero-paddado) já é maior ou igual ao horário configurado
// (horarioConfigurado, idem, já canonicalizado por resolveNFSeSchedulerHorario) E o ciclo ainda
// não rodou hoje (ultimaExecucaoData != hojeYYYYMMDD). Isso implementa "catch-up dentro do mesmo
// dia": se o processo reiniciar ou estiver ocupado exatamente no minuto configurado, o ciclo
// ainda dispara no primeiro tick seguinte em vez de pular o dia inteiro (ver Spec Change Log,
// loopback 1, item 5). Extraída do loop de StartNFSeScheduler para ser testável sem depender de
// um ticker real nem de time.Now() real.
func deveExecutarCicloNFSe(nowHHMM, horarioConfigurado, hojeYYYYMMDD, ultimaExecucaoData string) bool {
	if nowHHMM < horarioConfigurado {
		return false
	}
	return ultimaExecucaoData != hojeYYYYMMDD
}

// nfseSchedulerProcessCompany executa UM ciclo de ExecutarCicloNFSe para UMA empresa — extraída
// do laço de goroutines de runNFSeSchedulerCycle para ser testável isoladamente (ex.: forçar um
// panic passando db=nil, ver TestNfseSchedulerProcessCompany_PanicNaoDerrubaOProcesso em
// nfse_scheduler_test.go).
//
// O recover() de panic é o PRIMEIRO defer registrado, antes de qualquer outra lógica — a
// Boundaries da spec já garantia que um erro numa empresa não interrompe as demais; um panic
// (não só um error) numa goroutine sem recover() derrubaria o PROCESSO INTEIRO, contradizendo
// essa garantia (ver Spec Change Log, loopback 1, item 3).
func nfseSchedulerProcessCompany(db *sql.DB, companyID string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[NFSe Scheduler] PANIC recuperado ao processar companyID=%s: %v", companyID, r)
		}
	}()

	nfseSchedulerSemaphore <- struct{}{}
	defer func() { <-nfseSchedulerSemaphore }()

	if !NFSeBuscaRL.Allow(companyID) {
		log.Printf("[NFSe Scheduler] companyID=%s pulado — rate limit já atingido (busca manual/agendada recente)", companyID)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), nfseSchedulerCicloTimeout)
	defer cancel()

	recebidos, novos, ignorados, runErr := ExecutarCicloNFSe(ctx, db, companyID)
	if runErr != nil {
		log.Printf("[NFSe Scheduler] [ERRO] companyID=%s: %v", companyID, runErr)
		return
	}
	log.Printf("[NFSe Scheduler] [OK] companyID=%s — documentosRecebidos=%d documentosNovos=%d documentosIgnorados=%d",
		companyID, recebidos, novos, ignorados)
}

// runNFSeSchedulerCycle executa UM ciclo do agendamento: busca as empresas cujo certificado
// NFS-e tem status calculado 'ativo' (mesma expressão CASE usada em
// handlers/nfse_credentials.go, handlers/nfse_busca.go e ExecutarCicloNFSe) e dispara
// nfseSchedulerProcessCompany em uma goroutine por empresa (que aplica o MESMO NFSeBuscaRL usado
// pelo disparo manual e o semáforo de concorrência, ver nfseSchedulerSemaphore). Uma empresa sem
// certificado ou com status != 'ativo' nunca aparece na lista devolvida — é pulada
// silenciosamente, não é um erro.
//
// Devolve a lista de empresas despachadas e o *sync.WaitGroup usado internamente. O loop de
// produção (StartNFSeScheduler) NÃO espera por ele — dispara e segue para o próximo tick, mesmo
// princípio não-bloqueante de StartRFBScheduler (rfb_scheduler.go:359-368). Os testes usam o
// WaitGroup pra esperar deterministicamente o fim das goroutines antes de checar o resultado.
func runNFSeSchedulerCycle(db *sql.DB) ([]string, *sync.WaitGroup) {
	var wg sync.WaitGroup

	rows, err := db.Query(`
		SELECT company_id FROM nfse_certificados
		WHERE (CASE WHEN status = 'ativo' AND validade IS NOT NULL AND validade < CURRENT_DATE
		            THEN 'vencido' ELSE status END) = 'ativo'
	`)
	if err != nil {
		log.Printf("[NFSe Scheduler] Erro ao consultar empresas com certificado NFS-e ativo (query falhou): %v", err)
		return nil, &wg
	}

	var companies []string
	for rows.Next() {
		var cid string
		if scanErr := rows.Scan(&cid); scanErr != nil {
			log.Printf("[NFSe Scheduler] erro ao ler company_id de uma linha do resultado: %v", scanErr)
			continue
		}
		companies = append(companies, cid)
	}
	if iterErr := rows.Err(); iterErr != nil {
		log.Printf("[NFSe Scheduler] erro ao iterar empresas elegíveis: %v", iterErr)
	}
	rows.Close()

	for _, companyID := range companies {
		cid := companyID
		wg.Add(1)
		go func() {
			defer wg.Done()
			nfseSchedulerProcessCompany(db, cid)
		}()
	}

	return companies, &wg
}

// StartNFSeScheduler inicia o loop de agendamento automático de busca de NFS-e — mesmo padrão
// de StartRFBScheduler (rfb_scheduler.go:298-369): aguarda o banco ficar pronto, ticker de 1
// minuto, compara o horário atual (America/Sao_Paulo) contra um horário fixo (em vez de por
// empresa, como no RFB), dispara uma goroutine por empresa elegível, erro de uma empresa não
// aborta o ciclo das demais.
//
// Deve ser chamado como goroutine no startup (main.go, logo após
// `go services.StartRFBScheduler(getDB)`). Usa dbFn para obter o DB após ele estar pronto.
func StartNFSeScheduler(dbFn func() *sql.DB) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		log.Printf("[NFSe Scheduler] Erro ao carregar timezone: %v — scheduler desativado", err)
		return
	}

	horario := resolveNFSeSchedulerHorario()
	log.Printf("[NFSe Scheduler] Horário agendado: %s (America/Sao_Paulo, fixo e global para todas as empresas)", horario)

	log.Println("[NFSe Scheduler] Aguardando banco de dados...")
	var db *sql.DB
	for {
		db = dbFn()
		if db != nil {
			if pingErr := db.Ping(); pingErr == nil {
				break
			}
		}
		time.Sleep(5 * time.Second)
	}
	log.Println("[NFSe Scheduler] Banco pronto. Scheduler NFS-e iniciado.")

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// ultimaExecucaoData rastreia (só em memória — reinicia a cada restart do processo, aceitável
	// conforme a spec) a data (YYYY-MM-DD) do último ciclo disparado, usada por
	// deveExecutarCicloNFSe para o catch-up dentro do mesmo dia.
	var ultimaExecucaoData string

	for range ticker.C {
		now := time.Now().In(loc)
		hoje := now.Format("2006-01-02")

		if !deveExecutarCicloNFSe(now.Format("15:04"), horario, hoje, ultimaExecucaoData) {
			continue
		}
		ultimaExecucaoData = hoje

		companies, _ := runNFSeSchedulerCycle(db)
		if len(companies) == 0 {
			log.Printf("[NFSe Scheduler] %s — nenhuma empresa com certificado NFS-e ativo — ciclo encerrado", now.Format("2006-01-02 15:04"))
			continue
		}
		log.Printf("[NFSe Scheduler] %s — %d empresa(s) com certificado NFS-e ativo — buscas disparadas", now.Format("2006-01-02 15:04"), len(companies))
	}
}
