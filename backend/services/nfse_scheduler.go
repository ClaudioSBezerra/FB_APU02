package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// nfseSchedulerIntervaloRetentativa é a espera mínima antes de tentar de novo, no MESMO dia, uma
// empresa cujo ciclo terminou com erro ou parcial por falha (transitório esgotado, cursor sem
// avanço, cancelamento) — evita martelar o ADN a cada tick com um certificado recusado. Parcial
// por backlog (limite de lotes/tempo) tenta de novo já no tick seguinte.
const nfseSchedulerIntervaloRetentativa = 15 * time.Minute

// nfseSchedulerEstado guarda, em memória (reinicia a cada restart do processo — aceitável
// conforme a spec), por empresa: a data (YYYY-MM-DD) em que o ciclo do dia foi CONCLUÍDO e o
// instante mínimo da próxima tentativa. Um ciclo parcial ou com erro não marca o dia como feito.
type nfseSchedulerEstado struct {
	mu               sync.Mutex
	concluidoEm      map[string]string    // companyID → hojeYYYYMMDD
	proximaTentativa map[string]time.Time // companyID → não disparar antes disto
	emExecucao       map[string]bool      // companyID → goroutine do scheduler ainda rodando
}

func novoNFSeSchedulerEstado() *nfseSchedulerEstado {
	return &nfseSchedulerEstado{
		concluidoEm:      map[string]string{},
		proximaTentativa: map[string]time.Time{},
		emExecucao:       map[string]bool{},
	}
}

// reservar decide (e marca) se a empresa deve ser disparada neste tick.
func (e *nfseSchedulerEstado) reservar(companyID, nowHHMM, horario, hoje string, agora time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.emExecucao[companyID] {
		return false
	}
	if !deveExecutarCicloNFSe(nowHHMM, horario, hoje, e.concluidoEm[companyID]) {
		return false
	}
	if t, ok := e.proximaTentativa[companyID]; ok && agora.Before(t) {
		return false
	}
	e.emExecucao[companyID] = true
	return true
}

// registrar anota o desfecho do ciclo de uma empresa (ver nfseSchedulerDesfecho).
func (e *nfseSchedulerEstado) registrar(companyID, hoje string, agora time.Time, res NFSeResultadoCiclo, executou bool, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.emExecucao, companyID)
	switch nfseSchedulerDesfecho(res, executou, err) {
	case nfseDesfechoConcluido:
		e.concluidoEm[companyID] = hoje
		delete(e.proximaTentativa, companyID)
	case nfseDesfechoBacklog:
		delete(e.proximaTentativa, companyID) // continua já no próximo tick
	case nfseDesfechoRetentar:
		e.proximaTentativa[companyID] = agora.Add(nfseSchedulerIntervaloRetentativa)
	case nfseDesfechoPulado:
		// ocupado/rate limit: nada a registrar, tenta no próximo tick
	}
}

type nfseDesfechoScheduler int

const (
	nfseDesfechoConcluido nfseDesfechoScheduler = iota // fila esgotada — dia feito
	nfseDesfechoBacklog                                // limite de lotes/tempo — continuar já
	nfseDesfechoRetentar                               // erro ou parcial por falha — retentar mais tarde
	nfseDesfechoPulado                                 // não chegou a rodar (ocupado/rate limit)
)

// nfseSchedulerDesfecho classifica o resultado de um ciclo agendado. Certificado ausente/vencido
// conta como concluído (nada a fazer hoje; a empresa nem deveria estar elegível).
func nfseSchedulerDesfecho(res NFSeResultadoCiclo, executou bool, err error) nfseDesfechoScheduler {
	switch {
	case !executou || errors.Is(err, ErrNFSeCicloEmAndamento):
		return nfseDesfechoPulado
	case errors.Is(err, ErrNFSeCertificadoNaoCadastrado) || errors.Is(err, ErrNFSeCertificadoIndisponivel):
		return nfseDesfechoConcluido
	case err != nil:
		return nfseDesfechoRetentar
	case !res.Parcial():
		return nfseDesfechoConcluido
	case res.PorBacklog():
		return nfseDesfechoBacklog
	default:
		return nfseDesfechoRetentar
	}
}

// nfseSchedulerProcessCompany executa UM ciclo de ExecutarCicloNFSe para UMA empresa — extraída
// do laço de goroutines de runNFSeSchedulerCycle para ser testável isoladamente (ex.: forçar um
// panic passando db=nil, ver TestNfseSchedulerProcessCompany_PanicNaoDerrubaOProcesso em
// nfse_scheduler_test.go). aoTerminar (opcional) recebe o resultado; executou=false quando o
// ciclo nem foi tentado (rate limit). Um panic é reportado a aoTerminar como erro.
//
// O recover() de panic é o PRIMEIRO defer registrado, antes de qualquer outra lógica — a
// Boundaries da spec já garantia que um erro numa empresa não interrompe as demais; um panic
// (não só um error) numa goroutine sem recover() derrubaria o PROCESSO INTEIRO, contradizendo
// essa garantia (ver Spec Change Log, loopback 1, item 3).
func nfseSchedulerProcessCompany(db *sql.DB, companyID string, aoTerminar func(res NFSeResultadoCiclo, executou bool, err error)) {
	var (
		res      NFSeResultadoCiclo
		executou bool
		runErr   error
	)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[NFSe Scheduler] PANIC recuperado ao processar companyID=%s: %v", companyID, r)
			executou, runErr = true, fmt.Errorf("panic: %v", r)
		}
		if aoTerminar != nil {
			aoTerminar(res, executou, runErr)
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

	executou = true
	res, runErr = ExecutarCicloNFSe(ctx, db, companyID, NFSeCicloOpcoes{})
	switch {
	case errors.Is(runErr, ErrNFSeCicloEmAndamento):
		log.Printf("[NFSe Scheduler] companyID=%s pulado — captura já em andamento (busca manual?)", companyID)
	case runErr != nil:
		log.Printf("[NFSe Scheduler] [ERRO] companyID=%s: %v (recebidos=%d novos=%d falhas=%d)", companyID, runErr, res.Recebidos, res.Novos, res.Falhas)
	default:
		log.Printf("[NFSe Scheduler] [OK] companyID=%s — encerramento=%s parcial=%t documentosRecebidos=%d documentosNovos=%d documentosAtualizados=%d documentosIgnorados=%d documentosFalhas=%d",
			companyID, res.Encerramento, res.Parcial(), res.Recebidos, res.Novos, res.Atualizados, res.Ignorados, res.Falhas)
	}
}

// runNFSeSchedulerCycle executa UM ciclo do agendamento para TODAS as empresas elegíveis, sem
// estado de dia (usado pelos testes). Ver runNFSeSchedulerCycleComEstado.
func runNFSeSchedulerCycle(db *sql.DB) ([]string, *sync.WaitGroup) {
	return runNFSeSchedulerCycleComEstado(db, nil, "", "", "", time.Time{})
}

// runNFSeSchedulerCycleComEstado executa UM tick do agendamento: busca as empresas cujo
// certificado NFS-e tem status calculado 'ativo' (mesma expressão CASE usada em
// handlers/nfse_credentials.go, handlers/nfse_busca.go e ExecutarCicloNFSe) e dispara
// nfseSchedulerProcessCompany em uma goroutine por empresa (que aplica o MESMO NFSeBuscaRL usado
// pelo disparo manual e o semáforo de concorrência, ver nfseSchedulerSemaphore). Com estado != nil,
// só dispara as empresas que estado.reservar aceitar (dia não concluído, fora do intervalo de
// retentativa, não em execução) e registra o desfecho ao terminar. Uma empresa sem certificado ou
// com status != 'ativo' nunca aparece na lista — é pulada silenciosamente, não é um erro.
//
// Devolve a lista de empresas despachadas e o *sync.WaitGroup usado internamente. O loop de
// produção (StartNFSeScheduler) NÃO espera por ele — dispara e segue para o próximo tick, mesmo
// princípio não-bloqueante de StartRFBScheduler (rfb_scheduler.go:359-368). Os testes usam o
// WaitGroup pra esperar deterministicamente o fim das goroutines antes de checar o resultado.
func runNFSeSchedulerCycleComEstado(db *sql.DB, estado *nfseSchedulerEstado, nowHHMM, horario, hoje string, agora time.Time) ([]string, *sync.WaitGroup) {
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
		if estado != nil && !estado.reservar(cid, nowHHMM, horario, hoje, agora) {
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
		var aoTerminar func(NFSeResultadoCiclo, bool, error)
		if estado != nil {
			aoTerminar = func(res NFSeResultadoCiclo, executou bool, err error) {
				estado.registrar(cid, hoje, time.Now(), res, executou, err)
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			nfseSchedulerProcessCompany(db, cid, aoTerminar)
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
// O dia só é marcado como feito POR EMPRESA e só quando a fila foi esgotada (nfseSchedulerEstado):
// parcial por backlog continua no tick seguinte; erro/parcial por falha retenta depois de
// nfseSchedulerIntervaloRetentativa.
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

	estado := novoNFSeSchedulerEstado()

	for range ticker.C {
		now := time.Now().In(loc)
		nowHHMM := now.Format("15:04")
		if nowHHMM < horario {
			continue
		}
		companies, _ := runNFSeSchedulerCycleComEstado(db, estado, nowHHMM, horario, now.Format("2006-01-02"), now)
		if len(companies) > 0 {
			log.Printf("[NFSe Scheduler] %s — %d empresa(s) com certificado NFS-e ativo e ciclo pendente — buscas disparadas", now.Format("2006-01-02 15:04"), len(companies))
		}
	}
}
