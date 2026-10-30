package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// ─── Poller de situação (sem depender do webhook) ───────────────────────────
//
// A RFB aceita a solicitação (201) mas o webhook pode não chegar. Este poller consulta
// GET .../v2/situacao/{tiquete} das solicitações 'requested' sem fonte de download e, quando
// CONCLUIDA, obtém a urlAssinada e segue o fluxo de download normal. O webhook continua como
// atalho: o claim 'downloading' dos processadores evita processar duas vezes.

const (
	rfbPollTick   = 2 * time.Minute
	rfbPollMinAge = 2 * time.Minute   // dá tempo ao webhook de chegar sozinho
	rfbPollMaxAge = 240 * time.Minute // limite de processamento da RFB (depois vira EXPIRADA)
)

// rfbPollIntervalo é o backoff por idade da solicitação: consulta mais cedo, espaça depois.
func rfbPollIntervalo(idade time.Duration) time.Duration {
	switch {
	case idade < 30*time.Minute:
		return 2 * time.Minute
	case idade < 2*time.Hour:
		return 5 * time.Minute
	default:
		return 10 * time.Minute
	}
}

type rfbPollEstado struct {
	proxima  time.Time
	desistiu bool
	ultimo   map[string]string // canal ("situacao"/"download") → última resposta logada
	// proximaSonda: antes deste instante a sonda do download v1 não bate na RFB (401 definitivo
	// ou 429 espaçam a sonda em 1h; qualquer outra resposta zera e volta aos 10 min).
	proximaSonda time.Time
}

// rfbPoller guarda o backoff em memória (reinicia com o processo — aceitável: a janela é de
// 4 horas e uma consulta extra após o restart é inofensiva).
type rfbPoller struct {
	mu      sync.Mutex
	estados map[string]*rfbPollEstado
}

func novoRFBPoller() *rfbPoller {
	return &rfbPoller{estados: make(map[string]*rfbPollEstado)}
}

var rfbPollerGlobal = novoRFBPoller()

// devido informa se a solicitação deve ser consultada agora.
func (p *rfbPoller) devido(id string, agora time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.estados[id]
	if !ok {
		return true
	}
	return !e.desistiu && !agora.Before(e.proxima)
}

func (p *rfbPoller) agendar(id string, agora time.Time, intervalo time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.estados[id]
	if e == nil {
		e = &rfbPollEstado{}
		p.estados[id] = e
	}
	e.proxima = agora.Add(intervalo)
}

// mudou registra a resposta mais recente de um canal da solicitação e informa se ela difere da
// anterior. Só uma resposta nova (ou a primeira) merece log: repetir a mesma a cada 10 min encheria
// o log sem informação.
func (p *rfbPoller) mudou(id, canal, resposta string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.estados[id]
	if e == nil {
		e = &rfbPollEstado{}
		p.estados[id] = e
	}
	if e.ultimo == nil {
		e.ultimo = make(map[string]string)
	}
	if anterior, ok := e.ultimo[canal]; ok && anterior == resposta {
		return false
	}
	e.ultimo[canal] = resposta
	return true
}

// rfbSondaEspacamento: intervalo da sonda do download v1 depois de uma resposta que não muda
// (401 de tíquete inexistente) ou de um 429 — a rota de download é a mesma de um download legítimo.
const rfbSondaEspacamento = time.Hour

// sondaLiberada informa se a sonda do download pode bater na RFB agora.
func (p *rfbPoller) sondaLiberada(id string, agora time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.estados[id]
	return e == nil || !agora.Before(e.proximaSonda)
}

// definirProximaSonda agenda a próxima sonda; zero = cadência normal (a de 10 min do poller).
func (p *rfbPoller) definirProximaSonda(id string, ate time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.estados[id]
	if e == nil {
		e = &rfbPollEstado{}
		p.estados[id] = e
	}
	e.proximaSonda = ate
}

// rfbPollIntervaloPara: v1 (rota legada, sem webhook nem situação confiáveis) consulta a cada 10 min
// fixos; v2 segue o backoff por idade.
func rfbPollIntervaloPara(it rfbPollItem, idade time.Duration) time.Duration {
	if it.apiVersao != RFBAPIVersaoV2 {
		return 10 * time.Minute
	}
	return rfbPollIntervalo(idade)
}

func (p *rfbPoller) desistir(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.estados[id] = &rfbPollEstado{desistiu: true}
}

// podar remove o estado de solicitações que saíram da janela (concluídas, com erro, expiradas).
func (p *rfbPoller) podar(ativos map[string]bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id := range p.estados {
		if !ativos[id] {
			delete(p.estados, id)
		}
	}
}

type rfbPollItem struct {
	id, companyID, tiquete, tipo, ambiente, apiVersao string
	criadoEm                                          time.Time
}

// PollarSituacaoRFB roda um ciclo do poller. Chamado pelo scheduler a cada rfbPollTick. Um panic
// aqui derrubaria o processo inteiro (é um goroutine próprio), então é contido e logado.
func PollarSituacaoRFB(db *sql.DB) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[RFB Poller] PANIC contido no ciclo: %v", r)
		}
	}()
	pollarSituacaoRFB(db, rfbPollerGlobal, time.Now)
}

// pollarSituacaoRFB: agora é um relógio (não um instante) porque o ciclo pode demorar — cada item
// usa o seu próprio "agora" para idade e backoff.
func pollarSituacaoRFB(db *sql.DB, p *rfbPoller, agora func() time.Time) {
	inicio := agora()
	rows, err := db.Query(`
		SELECT id, company_id, tiquete, tipo, COALESCE(ambiente, 'producao'), COALESCE(api_versao, 'v1'), created_at
		FROM rfb_requests
		WHERE status = 'requested'
		  AND tiquete IS NOT NULL AND tiquete <> ''
		  AND tiquete_download IS NULL AND url_assinada IS NULL
		  AND created_at < $1 AND created_at > $2
		ORDER BY created_at
	`, inicio.Add(-rfbPollMinAge), inicio.Add(-rfbPollMaxAge))
	if err != nil {
		log.Printf("[RFB Poller] Erro ao listar solicitações pendentes: %v", err)
		return
	}
	var itens []rfbPollItem
	for rows.Next() {
		var it rfbPollItem
		if err := rows.Scan(&it.id, &it.companyID, &it.tiquete, &it.tipo, &it.ambiente, &it.apiVersao, &it.criadoEm); err != nil {
			log.Printf("[RFB Poller] Erro ao ler solicitação pendente: %v", err)
			continue
		}
		itens = append(itens, it)
	}
	rerr := rows.Err()
	rows.Close()
	if rerr != nil {
		// Lista truncada: não poda (apagaria o "desistiu" de quem ainda está na janela).
		log.Printf("[RFB Poller] Erro ao iterar solicitações pendentes — ciclo abortado: %v", rerr)
		return
	}

	ativos := make(map[string]bool, len(itens))
	for _, it := range itens {
		ativos[it.id] = true
	}
	p.podar(ativos)

	for _, it := range itens {
		agoraItem := agora()
		if !p.devido(it.id, agoraItem) {
			continue
		}
		pollarUma(db, p, it, agoraItem)
	}
}

func pollarUma(db *sql.DB, p *rfbPoller, it rfbPollItem, agora time.Time) {
	idade := agora.Sub(it.criadoEm)
	intervalo := rfbPollIntervaloPara(it, idade)
	p.agendar(it.id, agora, intervalo)

	var clientID, clientSecret string
	if err := db.QueryRow(`
		SELECT client_id, client_secret FROM rfb_credentials WHERE company_id = $1 AND ativo = true
	`, it.companyID).Scan(&clientID, &clientSecret); err != nil {
		log.Printf("[RFB Poller] request=%s: credencial RFB ativa não encontrada (%v) — deixando de consultar esta solicitação", it.id, err)
		p.desistir(it.id)
		return
	}

	c := NewRFBClient()
	c.SetAmbiente(it.ambiente)
	c.SetAPIVersao(it.apiVersao)
	token, err := c.GetToken(clientID, clientSecret)
	if err != nil {
		log.Printf("[RFB Poller] request=%s: falha ao obter token — tenta no próximo ciclo: %v", it.id, err)
		return
	}

	sit, tok, err := c.consultarSituacao(token, it.tiquete)
	if err != nil {
		if !rfbPollSemAcesso(err, it, p) {
			log.Printf("[RFB Poller] request=%s: consulta de situação falhou (nova tentativa em %s): %v", it.id, intervalo, err)
			return
		}
		// v2 desistiu (rfbPollSemAcesso); v1 segue tentando: sonda o download legado.
		if it.apiVersao != RFBAPIVersaoV2 {
			sondarDownloadV1(db, p, it, c, tok, agora)
		}
		return
	}

	switch estado := sit.Efetivo(); estado {
	case "CONCLUIDA":
		urlAss, expiraEm := sit.UrlAssinada, sit.UrlAssinadaExpiraEm
		if urlAss == "" {
			u, e, uerr := c.ObterURLDownload(tok, it.tiquete)
			if uerr != nil {
				var he *RFBHTTPError
				if errors.As(uerr, &he) && he.Status == http.StatusNotFound {
					log.Printf("[RFB Poller] request=%s: CONCLUIDA, mas o arquivo ainda não está disponível (404) — nova tentativa em %s", it.id, intervalo)
					return
				}
				if rfbPollSemAcesso(uerr, it, p) {
					return
				}
				log.Printf("[RFB Poller] request=%s: falha ao obter urlAssinada: %v", it.id, uerr)
				return
			}
			urlAss, expiraEm = u, e
		}
		if verr := ValidarURLAssinada(urlAss); verr != nil && !rfbSSRFTestBypass.Load() {
			erroSeRequested(db, it.id, RFBErrURLInvalida, "urlAssinada rejeitada pela validação: "+verr.Error())
			p.desistir(it.id)
			log.Printf("[RFB Poller] request=%s: urlAssinada rejeitada pela validação (host %s)", it.id, hostOfURL(urlAss))
			return
		}
		gravou, werr := gravarURLSeRequested(db, it.id, urlAss, ParseURLExpiraEm(expiraEm))
		if werr != nil {
			log.Printf("[RFB Poller] request=%s: falha ao gravar a urlAssinada — nova tentativa em %s: %v", it.id, intervalo, werr)
			return
		}
		if !gravou {
			log.Printf("[RFB Poller] request=%s: a linha já saiu de 'requested' (webhook/download manual assumiu) — nada a fazer", it.id)
			return
		}
		log.Printf("[RFB Poller] request=%s (%s, api %s): CONCLUIDA sem webhook — iniciando download", it.id, it.tipo, it.apiVersao)
		var perr error
		if it.tipo == "credito" {
			perr = ProcessarDownloadCreditosRFB(db, c, it.id)
		} else {
			perr = ProcessarDownloadRFB(db, c, it.id)
		}
		if perr != nil {
			log.Printf("[RFB Poller] request=%s: processamento do download falhou: %v", it.id, perr)
		}
	case "ERRO", "EXPIRADA":
		code := TruncateRunes(sit.CodigoErro, 50)
		if code == "" {
			code = "RFB_ERRO"
			if estado == "EXPIRADA" {
				code = "EXPIRADA"
			}
		}
		erroSeRequested(db, it.id, code, firstNonEmpty(sit.MensagemErro, "RFB reportou "+estado))
		p.desistir(it.id)
		log.Printf("[RFB Poller] request=%s: situação %s (%s)", it.id, estado, code)
	default:
		log.Printf("[RFB Poller] request=%s (%s, api %s): situação %q após %s — nova consulta em %s",
			it.id, it.tipo, it.apiVersao, estado, idade.Round(time.Second), intervalo)
	}
}

// rfbPollSemAcesso trata 401/403/404 como resultado de diagnóstico (não como falha): o gateway
// não reconhece o tíquete/rota para esta credencial — p.ex. tíquete da rota legada rtc/v1 na
// situação v2, ou credencial sem a rota habilitada. A linha em rfb_requests não é alterada.
// v2: loga UMA vez e para de consultar a linha. v1: continua tentando a cada 10 min e loga só
// quando a resposta muda. Devolve true quando tratou o erro.
func rfbPollSemAcesso(err error, it rfbPollItem, p *rfbPoller) bool {
	var he *RFBHTTPError
	if !errors.As(err, &he) {
		return false
	}
	switch he.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		if it.apiVersao == RFBAPIVersaoV2 {
			log.Printf("[RFB Poller] DIAGNÓSTICO request=%s tipo=%s api=%s ambiente=%s: %s devolveu HTTP %d (%s) — "+
				"o gateway não reconhece este tíquete/rota para esta credencial; deixando de consultar esta solicitação (o webhook segue como atalho)",
				it.id, it.tipo, it.apiVersao, it.ambiente, he.Op, he.Status, he.Body)
			p.desistir(it.id)
			return true
		}
		if p.mudou(it.id, he.Op, fmt.Sprintf("%d|%s", he.Status, TruncateRunes(he.Body, 80))) {
			log.Printf("[RFB Poller] DIAGNÓSTICO request=%s tipo=%s api=%s ambiente=%s: %s devolveu HTTP %d (%s) — "+
				"sem acesso por este caminho; segue tentando a cada 10 min e só volta a logar se a resposta mudar",
				it.id, it.tipo, it.apiVersao, it.ambiente, he.Op, he.Status, he.Body)
		}
		return true
	}
	return false
}

// sondarDownloadV1 tenta o download legado GET /{rtc|prr-rtc}/download/v1/{tíquete da solicitação}.
// É só uma sonda: 401/403/404 e demais erros NUNCA alteram a linha (o fallback antigo foi removido
// em 174243e porque o erro do download sobrescrevia o TIMEOUT real); só logam quando a resposta
// muda. O GET consome o tíquete (uso único), então: (1) reconfere a linha imediatamente antes;
// (2) se vier arquivo, grava o raw_json NA HORA, antes de qualquer outra coisa que possa falhar;
// (3) só entrega ao processador se for JSON (um 200 que não seja o arquivo não pode virar
// PARSE_ERROR); (4) o processador usa os bytes recebidos em vez de baixar de novo.
func sondarDownloadV1(db *sql.DB, p *rfbPoller, it rfbPollItem, c *RFBClient, token string, agora time.Time) {
	if !p.sondaLiberada(it.id, agora) {
		return // 401 definitivo ou 429 recentes: espaçando para não gastar a quota da rota de download
	}
	var status string
	var temFonte bool
	if err := db.QueryRow(`
		SELECT status, (tiquete_download IS NOT NULL OR url_assinada IS NOT NULL) FROM rfb_requests WHERE id = $1
	`, it.id).Scan(&status, &temFonte); err != nil || status != "requested" || temFonte {
		return // o webhook/download manual assumiu: não gastar o tíquete de que eles dependem
	}

	c.silencioso = true
	defer func() { c.silencioso = false }()
	dados, err := c.DownloadArquivo(token, it.tiquete)
	if err != nil {
		var de *RFBDownloadError
		var se *rfbStatusError
		proxima := "em 10 min"
		switch {
		case errors.As(err, &de) && de.Code == RFBErrTicketInvalido:
			// Resposta estável: repetir a cada 10 min só gasta a quota da rota de download.
			p.definirProximaSonda(it.id, agora.Add(rfbSondaEspacamento))
			proxima = "em 1 hora (tíquete inexistente é resposta estável)"
		case errors.As(err, &se) && se.Status == http.StatusTooManyRequests:
			p.definirProximaSonda(it.id, agora.Add(rfbSondaEspacamento))
			proxima = "em 1 hora (429: a rota de download está limitada)"
		default:
			p.definirProximaSonda(it.id, time.Time{})
		}
		if p.mudou(it.id, "download", TruncateRunes(err.Error(), 120)) {
			log.Printf("[RFB Poller] request=%s (%s, api %s): sonda do download v1 sem arquivo: %s — próxima sonda %s; só loga se a resposta mudar",
				it.id, it.tipo, it.apiVersao, TruncateRunes(err.Error(), 300), proxima)
		}
		return
	}
	p.definirProximaSonda(it.id, time.Time{})

	// O tíquete já foi consumido: guarda o que veio antes de tudo (reprocessável por "Reprocessar").
	if _, serr := db.Exec(`UPDATE rfb_requests SET raw_json = COALESCE(raw_json, $1), updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
		string(dados), it.id); serr != nil {
		log.Printf("[RFB Poller] request=%s: ALERTA: falha ao gravar o raw_json do download v1 (%d bytes): %v", it.id, len(dados), serr)
	}
	if !json.Valid(dados) {
		if p.mudou(it.id, "download", "200-nao-json") {
			log.Printf("[RFB Poller] request=%s: sonda do download v1 respondeu 200 com corpo que não é JSON (%d bytes) — guardado em raw_json, linha mantida em 'requested'", it.id, len(dados))
		}
		return
	}
	log.Printf("[RFB Poller] request=%s (%s, api %s): sonda do download v1 devolveu o arquivo (%d bytes) — processando", it.id, it.tipo, it.apiVersao, len(dados))
	var perr error
	if it.tipo == "credito" {
		perr = ProcessarDownloadCreditosRFBComArquivo(db, c, it.id, dados)
	} else {
		perr = ProcessarDownloadRFBComArquivo(db, c, it.id, dados)
	}
	if perr != nil {
		log.Printf("[RFB Poller] request=%s: processamento do arquivo da sonda falhou: %v", it.id, perr)
	}
}

// gravarURLSeRequested persiste a urlAssinada só se a linha ainda está 'requested': o webhook ou o
// download manual podem tê-la concluído durante as chamadas de rede, e uma url gravada numa linha
// 'completed' nunca seria limpa. Devolve false (sem erro) quando a linha já mudou de estado.
func gravarURLSeRequested(db *sql.DB, id, urlAss string, exp *time.Time) (bool, error) {
	var expArg sql.NullTime
	if exp != nil {
		expArg = sql.NullTime{Time: *exp, Valid: true}
	}
	res, err := db.Exec(`
		UPDATE rfb_requests SET url_assinada = $1, url_assinada_expira_em = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND status = 'requested'
	`, urlAss, expArg, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// erroSeRequested marca a linha como erro só se ainda está 'requested' (não sobrescreve uma
// solicitação que o webhook já concluiu ou está baixando). Mesmo formato de updateRequestError.
func erroSeRequested(db *sql.DB, id, code, message string) {
	if _, err := db.Exec(`
		UPDATE rfb_requests SET status = 'error', error_code = $1, error_message = $2,
			url_assinada = NULL, url_assinada_expira_em = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND status = 'requested'
	`, TruncateRunes(code, 50), TruncateRunes(message, 4000), id); err != nil {
		log.Printf("[RFB Poller] Erro ao gravar erro da solicitação %s: %v", id, err)
	}
}
