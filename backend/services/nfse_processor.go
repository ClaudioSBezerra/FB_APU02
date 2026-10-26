// Package services — ciclo completo de busca/persistência de NFS-e do Portal Nacional ADN.
//
// A lógica deste arquivo morava em handlers/nfse_busca.go (BuscarNFSeHandler), presa ao disparo
// manual via HTTP. A sub-spec spec-nfse-agendamento-persistencia.md extraiu TUDO que não é HTTP
// (validar certificado, montar client mTLS, buscar no ADN, persistir) pra cá, numa única função
// reaproveitável — mesmo padrão já usado pelo módulo RFB (services.SolicitarApuracaoParaEmpresa,
// chamada tanto por handlers/rfb_apuracao.go quanto por services/rfb_scheduler.go). Agora tanto
// o handler manual (handlers.BuscarNFSeHandler) quanto o scheduler automático
// (StartNFSeScheduler, nfse_scheduler.go) chamam ExecutarCicloNFSe — nenhum dos dois tem cópia
// própria da lógica de persistência.
package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// NFSeBuscaRL limita a 1 ciclo de busca de NFS-e a cada 30 segundos por empresa — compartilhado
// entre o disparo manual (handlers.BuscarNFSeHandler, mesmo padrão de LoginRL/RegisterRL em
// handlers/middleware.go) e o agendado (StartNFSeScheduler, nfse_scheduler.go), para que uma
// colisão entre as duas origens pra mesma empresa no mesmo minuto seja barrada por UM único
// contador. Vive aqui (não em handlers) porque o scheduler também precisa dele e `services` não
// pode depender de `handlers` — ver Boundaries da spec-nfse-agendamento-persistencia.md.
var NFSeBuscaRL = NewRateLimiter(1, 30*time.Second)

// Erros sentinela devolvidos por ExecutarCicloNFSe — permitem ao handler HTTP (ou a qualquer
// outro chamador) escolher a resposta certa sem acoplar esta função a detalhes de transporte
// HTTP (mesmo princípio dos ErrNFSeADN* em nfse_adn.go, que são propagados sem alteração quando
// o erro vem da etapa de busca no ADN).
var (
	// ErrNFSeCertificadoNaoCadastrado: empresa sem nenhuma linha em nfse_certificados.
	ErrNFSeCertificadoNaoCadastrado = errors.New("certificado NFS-e não cadastrado para esta empresa")
	// ErrNFSeCertificadoIndisponivel: certificado existe mas o status calculado (ver query
	// abaixo) não é 'ativo' — vencido ou revogado.
	ErrNFSeCertificadoIndisponivel = errors.New("certificado NFS-e indisponível (vencido ou revogado)")
	// ErrNFSeCertificadoProcessamento: falha ao consultar (erro de banco diferente de
	// sql.ErrNoRows) ou descriptografar o certificado.
	ErrNFSeCertificadoProcessamento = errors.New("falha ao processar certificado NFS-e")
	// ErrNFSeMTLSClientInvalido: falha ao decodificar o .pfx / montar o client mTLS (senha ou
	// arquivo inválidos).
	ErrNFSeMTLSClientInvalido = errors.New("falha ao montar client mTLS para o Portal Nacional ADN")
	// ErrNFSeCNPJNaoIdentificado: falha ao extrair o CNPJ titular do Subject do certificado.
	ErrNFSeCNPJNaoIdentificado = errors.New("não foi possível identificar o CNPJ titular do certificado NFS-e")
	// ErrNFSePersistencia: falha ao gravar os documentos recebidos (erro de banco na transação).
	ErrNFSePersistencia = errors.New("falha ao gravar documentos NFS-e")
)

// ErrNFSeCicloEmAndamento: já existe um ciclo de busca rodando para a empresa NESTE processo
// (disparo manual e agendado, ou dois manuais). O handler responde 409; o scheduler pula.
var ErrNFSeCicloEmAndamento = errors.New("captura de NFS-e já em andamento para esta empresa")

// ─── Exclusão mútua por empresa ─────────────────────────────────────────────

// nfseCiclosEmAndamento guarda um *sync.Mutex por empresa — dois ciclos simultâneos da mesma
// empresa leriam o MESMO ultimo_nsu e baixariam/gravariam os mesmos lotes em paralelo.
//
// LIMITAÇÃO CONHECIDA: a exclusão é só dentro do processo. Com mais de uma réplica do backend,
// dois ciclos da mesma empresa em réplicas diferentes ainda podem coincidir (o upsert idempotente
// por chave, a guarda de NSU e o GREATEST do cursor tornam isso inofensivo para os dados, só
// desperdiça chamadas ao ADN). pg_try_advisory_xact_lock não resolve, porque o ciclo atravessa
// várias transações (uma por lote); um advisory lock de SESSÃO exigiria prender uma conexão do
// pool durante o ciclo inteiro — fica para quando houver mais de uma réplica.
var nfseCiclosEmAndamento sync.Map // companyID → *sync.Mutex

// TentarReservarCicloNFSe reserva o ciclo da empresa (sem bloquear). ok=false quando já há um em
// andamento; com ok=true, liberar DEVE ser chamada ao terminar. Exportada para os testes do
// handler simularem um ciclo em andamento; ExecutarCicloNFSe já a chama sozinha.
func TentarReservarCicloNFSe(companyID string) (liberar func(), ok bool) {
	v, _ := nfseCiclosEmAndamento.LoadOrStore(companyID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}

// ─── Resultado e opções do ciclo ────────────────────────────────────────────

// NFSeCicloManualMaxDuracao é o tempo máximo do ciclo disparado pelo botão "Buscar Agora": menor
// que o WriteTimeout de 300 s do servidor HTTP (main.go), para a resposta sempre chegar ao
// cliente. O agendado usa o padrão de nfseADNFilaConfigPadrao (5 min).
const NFSeCicloManualMaxDuracao = 4 * time.Minute

// NFSeCicloOpcoes parametriza ExecutarCicloNFSe. Valor zero = padrões de nfseADNFilaConfigPadrao.
type NFSeCicloOpcoes struct {
	// MaxDuracao do percurso da fila (0 = padrão). Atingida, o ciclo encerra com sucesso parcial.
	MaxDuracao time.Duration
}

// NFSeResultadoCiclo resume um ciclo de ExecutarCicloNFSe — devolvido também junto com erro
// (contagens do que já foi gravado antes da falha).
//
// Recebidos = documentos de todos os tipos nos lotes gravados; Novos = linhas INSERIDAS (nota da
// empresa nos dois lados conta 2); Atualizados = linhas existentes atualizadas; Ignorados =
// documentos conscientemente não gravados (empresa não é parte, tipo/evento não tratado, reenvio
// com NSU menor que o já gravado); Falhas = documentos em quarentena (nfse_adn_falhas);
// Cancelamentos = linhas marcadas canceladas; CancelamentosPendentes = eventos guardados à espera
// da nota.
type NFSeResultadoCiclo struct {
	Recebidos              int
	Novos                  int
	Atualizados            int
	Ignorados              int
	Falhas                 int
	Cancelamentos          int
	CancelamentosPendentes int
	CamposInvalidos        int
	Lotes                  int
	NSUInicial             int64
	NSUFinal               int64
	// Encerramento é o motivo de fim do percurso (NFSeADNEncerramento*); "" quando o ciclo
	// falhou antes de percorrer a fila.
	Encerramento string
}

// Parcial indica que a fila NÃO foi esgotada neste ciclo (limite de lotes/tempo, transitório
// esgotado, cancelamento, cursor sem avanço ou erro) — o próximo ciclo continua do cursor salvo.
func (r NFSeResultadoCiclo) Parcial() bool {
	return r.Encerramento != NFSeADNEncerramentoFimDaFila
}

// PorBacklog indica encerramento parcial só por limite de lotes/tempo (há mais documentos na
// fila, nada deu errado).
func (r NFSeResultadoCiclo) PorBacklog() bool {
	return r.Encerramento == NFSeADNEncerramentoLimiteLotes || r.Encerramento == NFSeADNEncerramentoLimiteTempo
}

// ExecutarCicloNFSe executa UM ciclo completo de busca de NFS-e no Portal Nacional ADN para
// companyID: reserva o ciclo da empresa (TentarReservarCicloNFSe — ocupado → ErrNFSeCicloEmAndamento),
// valida o certificado ativo, descriptografa, monta o client mTLS, extrai o CNPJ titular e
// percorre a fila de distribuição (PercorrerFilaADN) a partir de nfse_certificados.ultimo_nsu.
// Cada lote é gravado em UMA transação junto com o novo ultimo_nsu (persistirLoteNFSe) — um erro
// no lote N mantém os lotes anteriores e o cursor do lote N-1. Chamada tanto pelo disparo manual
// (handlers.BuscarNFSeHandler) quanto pelo agendado (StartNFSeScheduler).
//
// ctx é propagado para a busca — o handler cancela se o cliente desconectar (r.Context()) e o
// scheduler impõe timeout próprio. Fim do prazo, limite de lotes, 429/5xx/rede com tentativas
// esgotadas ou cursor sem avanço encerram o ciclo com SUCESSO PARCIAL (err nil,
// res.Parcial()=true, motivo em res.Encerramento): o progresso já está salvo e o próximo ciclo
// continua dali.
//
// NÃO verifica NFSeBuscaRL — rate limiting é responsabilidade de quem chama.
//
// Os erros devolvidos são sempre um dos ErrNFSe* sentinela acima (envolvendo o erro original via
// %w) ou um ErrNFSeADN* de nfse_adn.go (propagado sem modificação). O resultado acompanha o erro
// com as contagens do que já foi gravado.
func ExecutarCicloNFSe(ctx context.Context, db *sql.DB, companyID string, opts NFSeCicloOpcoes) (NFSeResultadoCiclo, error) {
	var out NFSeResultadoCiclo

	liberar, ok := TentarReservarCicloNFSe(companyID)
	if !ok {
		return out, ErrNFSeCicloEmAndamento
	}
	defer liberar()

	// Mesma query de status calculado usada em GetNFSeCertificadoHandler
	// (handlers/nfse_credentials.go) e no scheduler (nfse_scheduler.go): um certificado com
	// status='ativo' persistido mas validade já vencida é reportado como 'vencido' mesmo que a
	// coluna ainda diga 'ativo'.
	var pfxEnc, senhaEnc, status string
	var ultimoNSU int64
	qerr := db.QueryRow(`
		SELECT certificado_pfx, certificado_senha,
		       CASE WHEN status = 'ativo' AND validade IS NOT NULL AND validade < CURRENT_DATE
		            THEN 'vencido' ELSE status END,
		       ultimo_nsu
		FROM nfse_certificados WHERE company_id = $1
	`, companyID).Scan(&pfxEnc, &senhaEnc, &status, &ultimoNSU)
	if qerr == sql.ErrNoRows {
		return out, ErrNFSeCertificadoNaoCadastrado
	}
	if qerr != nil {
		return out, fmt.Errorf("%w: consulta certificado: %v", ErrNFSeCertificadoProcessamento, qerr)
	}
	if status != "ativo" {
		return out, ErrNFSeCertificadoIndisponivel
	}
	out.NSUInicial, out.NSUFinal = ultimoNSU, ultimoNSU

	certDecrypted, derr := DecryptNFSeCertificado(pfxEnc, senhaEnc)
	if derr != nil {
		return out, fmt.Errorf("%w: %v", ErrNFSeCertificadoProcessamento, derr)
	}

	httpClient, x509Cert, berr := BuildNFSeMTLSClient(certDecrypted)
	if berr != nil {
		log.Printf("[NFSeProcessor] Falha ao montar client mTLS companyID=%s: %v", companyID, berr)
		return out, fmt.Errorf("%w: %v", ErrNFSeMTLSClientInvalido, berr)
	}

	cnpjEmpresa, cerr := ExtractCNPJFromCertificateSubject(x509Cert)
	if cerr != nil {
		log.Printf("[NFSeProcessor] Falha ao identificar CNPJ do certificado companyID=%s: %v", companyID, cerr)
		return out, fmt.Errorf("%w: %v", ErrNFSeCNPJNaoIdentificado, cerr)
	}

	cfg := nfseADNFilaConfigPadrao
	if opts.MaxDuracao > 0 {
		cfg.MaxDuracao = opts.MaxDuracao
	}

	var total nfseContagemLote
	baseURL := ResolveNFSeADNBaseURL()
	res, aerr := PercorrerFilaADN(ctx, httpClient, baseURL, ultimoNSU, cfg, func(lote NFSeADNLote, maiorNSU int64) error {
		c, perr := persistirLoteNFSe(db, companyID, cnpjEmpresa, lote, maiorNSU)
		if perr != nil {
			return fmt.Errorf("%w: %v", ErrNFSePersistencia, perr)
		}
		total.somar(c)
		return nil
	})
	out.Recebidos = res.Documentos
	out.Novos, out.Atualizados, out.Ignorados, out.Falhas = total.novos, total.atualizados, total.ignorados, total.falhas
	out.Cancelamentos, out.CancelamentosPendentes, out.CamposInvalidos = total.cancelamentos, total.pendentes, total.camposInvalidos
	out.Lotes, out.NSUFinal, out.Encerramento = res.Lotes, res.UltimoNSU, res.Encerramento

	if aerr != nil {
		switch {
		case errors.Is(aerr, ErrNFSePersistencia):
			log.Printf("[NFSeProcessor] Falha ao gravar lote companyID=%s (lotes anteriores e cursor NSU=%d preservados): %v", companyID, res.UltimoNSU, aerr)
		case errors.Is(aerr, ErrNFSeADNFormatoInesperado):
			log.Printf("[NFSeProcessor] Formato de resposta inesperado do ADN companyID=%s NSU=%d: %v", companyID, res.UltimoNSU, aerr)
		case errors.Is(aerr, ErrNFSeADNRejeicao):
			log.Printf("[NFSeProcessor] Consulta rejeitada pelo ADN companyID=%s NSU=%d: %v", companyID, res.UltimoNSU, aerr)
		default:
			log.Printf("[NFSeProcessor] Falha na comunicação com o Portal Nacional ADN companyID=%s NSU=%d: %v", companyID, res.UltimoNSU, aerr)
		}
		return out, aerr
	}

	if res.Encerramento == NFSeADNEncerramentoCursorSemAvanco {
		// Um lote veio com documentos mas sem NSU maior que o cursor: anomalia do ADN (ou do
		// cursor). Marcado para alerta operacional.
		log.Printf("[NFSe] cursor_sem_avanco companyID=%s NSU %d→%d lotes_gravados=%d — o ADN devolveu documentos sem NSU maior que o cursor",
			companyID, ultimoNSU, res.UltimoNSU, res.Lotes)
	}

	log.Printf("[NFSeProcessor] Ciclo concluído companyID=%s encerramento=%s parcial=%t lotes=%d NSU %d→%d recebidos=%d novos=%d atualizados=%d ignorados=%d falhas=%d cancelamentos=%d cancelamentos_pendentes=%d campos_invalidos=%d",
		companyID, res.Encerramento, out.Parcial(), res.Lotes, ultimoNSU, res.UltimoNSU, out.Recebidos, out.Novos, out.Atualizados, out.Ignorados, out.Falhas,
		out.Cancelamentos, out.CancelamentosPendentes, out.CamposInvalidos)
	return out, nil
}

// nfseContagemLote acumula o resultado da gravação de um lote.
type nfseContagemLote struct {
	novos              int // linhas inseridas
	atualizados        int // linhas já existentes atualizadas (ON CONFLICT DO UPDATE)
	ignorados          int // documentos conscientemente não gravados (soma dos 4 abaixo)
	naoParte           int //   empresa não é prestadora nem tomadora
	tipoNaoTratado     int //   TipoDocumento conhecido mas não processado (DPS, CNC, ...)
	eventoNaoCanc      int //   evento conhecido que não é cancelamento
	obsoletos          int //   reenvio com NSU menor que o já gravado (linha não alterada)
	falhas             int // documentos em quarentena (nfse_adn_falhas)
	cancelamentos      int // linhas marcadas como canceladas
	pendentes          int // cancelamentos guardados em nfse_cancelamentos_pendentes
	pendentesAplicados int // cancelamentos pendentes aplicados ao chegar a nota
	camposInvalidos    int // campos decimais presentes mas não interpretáveis (gravados como 0)
}

func (c *nfseContagemLote) somar(o nfseContagemLote) {
	c.novos += o.novos
	c.atualizados += o.atualizados
	c.ignorados += o.ignorados
	c.naoParte += o.naoParte
	c.tipoNaoTratado += o.tipoNaoTratado
	c.eventoNaoCanc += o.eventoNaoCanc
	c.obsoletos += o.obsoletos
	c.falhas += o.falhas
	c.cancelamentos += o.cancelamentos
	c.pendentes += o.pendentes
	c.pendentesAplicados += o.pendentesAplicados
	c.camposInvalidos += o.camposInvalidos
}

// nfseTabela identifica a tabela de destino de um INSERT de documento NFS-e — usado em vez de
// montar o nome da tabela por fmt.Sprintf na query: as queries abaixo são strings literais,
// escolhidas via switch.
type nfseTabela int

const (
	nfseTabelaEntradas nfseTabela = iota
	nfseTabelaSaidas
)

// Tipos de documento do enum TipoDocumentoRequisicao que são CONHECIDOS mas não processados
// (ignorados, contados). Qualquer outro valor que não NFSE/EVENTO — inclusive enum numérico —
// vai para a quarentena.
var nfseADNTiposDocumentoNaoTratados = map[string]bool{
	"NENHUM": true, "DPS": true, "PEDIDO_REGISTRO_EVENTO": true, "CNC": true,
}

// nfseADNEventosNaoCancelamento são os TipoEvento CONHECIDOS que não cancelam a nota (ignorados,
// contados). SUPOSIÇÃO: nomes derivados dos eventos do leiaute nacional (manifestação
// prestador/tomador/intermediário, confirmação tácita, rejeição/anulação, análise fiscal,
// bloqueio/desbloqueio por ofício) — o Swagger não lista o enum completo. Um valor fora desta
// lista e de nfseADNEventosCancelamento vai para a quarentena (nunca é perdido; basta incluí-lo
// aqui e reprocessar).
var nfseADNEventosNaoCancelamento = map[string]bool{
	"SOLICITACAO_CANCELAMENTO_ANALISE_FISCAL": true,
	"CANCELAMENTO_INDEFERIDO_ANALISE_FISCAL":  true,
	"CONFIRMACAO_PRESTADOR":                   true,
	"CONFIRMACAO_TOMADOR":                     true,
	"CONFIRMACAO_INTERMEDIARIO":               true,
	"CONFIRMACAO_TACITA":                      true,
	"REJEICAO_PRESTADOR":                      true,
	"REJEICAO_TOMADOR":                        true,
	"REJEICAO_INTERMEDIARIO":                  true,
	"ANULACAO_REJEICAO":                       true,
	"BLOQUEIO_POR_OFICIO":                     true,
	"DESBLOQUEIO_POR_OFICIO":                  true,
}

// DECISÃO (spec-nfse-adn-parser-real.md): no conflito (mesma ChaveAcesso já gravada pra
// empresa) os campos extraídos, o XML, o NSU e o número são ATUALIZADOS — uma nota reenviada
// pelo ADN corrige dados — mas SÓ quando o NSU recebido não é menor que o gravado (um replay
// antigo, ex.: cursor zerado, nunca regride valores). `cancelada`/`data_cancelamento` NUNCA
// entram no SET: reenvio jamais desmarca um cancelamento. RETURNING (xmax = 0) distingue
// inserção (true) de atualização; sem linha devolvida = conflito barrado pela guarda de NSU.
const nfseUpsertColunas = `(company_id, identificador_nfse, cnpj_prestador, cnpj_tomador, data_emissao, mes_ano, valor_servicos,
		iss, iss_retido, pis, cofins, irrf, csll, inss, xml_gzip_b64, nsu, numero_nfse)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	ON CONFLICT (company_id, identificador_nfse) DO UPDATE SET
		cnpj_prestador = EXCLUDED.cnpj_prestador,
		cnpj_tomador   = EXCLUDED.cnpj_tomador,
		data_emissao   = EXCLUDED.data_emissao,
		mes_ano        = EXCLUDED.mes_ano,
		valor_servicos = EXCLUDED.valor_servicos,
		iss            = EXCLUDED.iss,
		iss_retido     = EXCLUDED.iss_retido,
		pis            = EXCLUDED.pis,
		cofins         = EXCLUDED.cofins,
		irrf           = EXCLUDED.irrf,
		csll           = EXCLUDED.csll,
		inss           = EXCLUDED.inss,
		xml_gzip_b64   = EXCLUDED.xml_gzip_b64,
		nsu            = EXCLUDED.nsu,
		numero_nfse    = EXCLUDED.numero_nfse`

const nfseUpsertEntradasQuery = `INSERT INTO nfse_entradas ` + nfseUpsertColunas + `
	WHERE EXCLUDED.nsu >= nfse_entradas.nsu OR nfse_entradas.nsu IS NULL
	RETURNING (xmax = 0)`
const nfseUpsertSaidasQuery = `INSERT INTO nfse_saidas ` + nfseUpsertColunas + `
	WHERE EXCLUDED.nsu >= nfse_saidas.nsu OR nfse_saidas.nsu IS NULL
	RETURNING (xmax = 0)`

const nfseCancelarEntradasQuery = `UPDATE nfse_entradas SET cancelada = true, data_cancelamento = COALESCE(data_cancelamento, $3, NOW())
	WHERE company_id = $1 AND identificador_nfse = $2`
const nfseCancelarSaidasQuery = `UPDATE nfse_saidas SET cancelada = true, data_cancelamento = COALESCE(data_cancelamento, $3, NOW())
	WHERE company_id = $1 AND identificador_nfse = $2`

// Aplica um cancelamento pendente (evento chegou antes da nota) à linha recém-gravada.
const nfseAplicarPendenteEntradasQuery = `UPDATE nfse_entradas t
	SET cancelada = true, data_cancelamento = COALESCE(t.data_cancelamento, p.data_cancelamento, NOW())
	FROM nfse_cancelamentos_pendentes p
	WHERE t.company_id = $1 AND t.identificador_nfse = $2 AND NOT t.cancelada
	  AND p.company_id = t.company_id AND p.chave_acesso = t.identificador_nfse`
const nfseAplicarPendenteSaidasQuery = `UPDATE nfse_saidas t
	SET cancelada = true, data_cancelamento = COALESCE(t.data_cancelamento, p.data_cancelamento, NOW())
	FROM nfse_cancelamentos_pendentes p
	WHERE t.company_id = $1 AND t.identificador_nfse = $2 AND NOT t.cancelada
	  AND p.company_id = t.company_id AND p.chave_acesso = t.identificador_nfse`

// Herda o cancelamento da tabela irmã: a mesma chave já cancelada em nfse_saidas (ou entradas)
// quando a linha da outra tabela é criada depois (ex.: nota nos dois lados em momentos distintos).
const nfseHerdarCancelamentoEntradasQuery = `UPDATE nfse_entradas t
	SET cancelada = true, data_cancelamento = COALESCE(t.data_cancelamento, s.data_cancelamento, NOW())
	FROM nfse_saidas s
	WHERE t.company_id = $1 AND t.identificador_nfse = $2 AND NOT t.cancelada
	  AND s.company_id = t.company_id AND s.identificador_nfse = t.identificador_nfse AND s.cancelada`
const nfseHerdarCancelamentoSaidasQuery = `UPDATE nfse_saidas t
	SET cancelada = true, data_cancelamento = COALESCE(t.data_cancelamento, s.data_cancelamento, NOW())
	FROM nfse_entradas s
	WHERE t.company_id = $1 AND t.identificador_nfse = $2 AND NOT t.cancelada
	  AND s.company_id = t.company_id AND s.identificador_nfse = t.identificador_nfse AND s.cancelada`

// nfseDocumentoGravar reúne o que vai para uma linha de nfse_entradas/nfse_saidas.
type nfseDocumentoGravar struct {
	identificador string
	cnpjPrestador string
	cnpjTomador   string
	nsu           int64
	arquivoXml    string
	dados         NFSeDadosXML
}

// nfseResultadoUpsert é o desfecho de upsertNFSeDocumento.
type nfseResultadoUpsert int

const (
	nfseUpsertInserido nfseResultadoUpsert = iota
	nfseUpsertAtualizado
	nfseUpsertObsoleto // já existia com NSU maior — linha intocada
)

// upsertNFSeDocumento grava (ou atualiza) um documento numa das tabelas, dentro de tx.
func upsertNFSeDocumento(tx *sql.Tx, tabela nfseTabela, companyID string, doc nfseDocumentoGravar) (nfseResultadoUpsert, error) {
	var query, nomeTabela string
	switch tabela {
	case nfseTabelaEntradas:
		query, nomeTabela = nfseUpsertEntradasQuery, "nfse_entradas"
	case nfseTabelaSaidas:
		query, nomeTabela = nfseUpsertSaidasQuery, "nfse_saidas"
	default:
		return 0, fmt.Errorf("tabela NFS-e desconhecida: %v", tabela)
	}
	d := doc.dados
	numero := d.NumeroNFSe
	if len(numero) > 20 {
		numero = numero[:20]
	}
	var inserido bool
	err := tx.QueryRow(query,
		companyID, doc.identificador, nullableNFSeCNPJ(doc.cnpjPrestador), nullableNFSeCNPJ(doc.cnpjTomador),
		nullableNFSeTexto(d.DataEmissao), nullableNFSeTexto(d.MesAno), d.ValorServicos,
		d.ISS, d.ISSRetido, d.PIS, d.COFINS, d.IRRF, d.CSLL, d.INSS,
		doc.arquivoXml, doc.nsu, nullableNFSeTexto(numero),
	).Scan(&inserido)
	if err == sql.ErrNoRows {
		return nfseUpsertObsoleto, nil
	}
	if err != nil {
		return 0, fmt.Errorf("upsert em %s: %w", nomeTabela, err)
	}
	if inserido {
		return nfseUpsertInserido, nil
	}
	return nfseUpsertAtualizado, nil
}

// documentoParteNFSe escolhe o documento a gravar na coluna de CNPJ: CNPJ de 14 posições;
// senão CPF de 11 dígitos (prestador/tomador pessoa física); senão vazio (NULL).
func documentoParteNFSe(cnpjs []string, cpf string) string {
	for _, c := range cnpjs {
		if len(c) == 14 {
			return c
		}
	}
	if len(cpf) == 11 {
		return cpf
	}
	return ""
}

// ordenarLoteNFSe devolve os itens do lote com TODAS as NFSE (e demais tipos) antes dos EVENTO,
// cada grupo em ordem de NSU — um cancelamento que chega no mesmo lote que a nota (mesmo com NSU
// menor) encontra a nota já gravada.
func ordenarLoteNFSe(itens []NFSeADNDistribuicao) []NFSeADNDistribuicao {
	out := append([]NFSeADNDistribuicao(nil), itens...)
	sort.SliceStable(out, func(i, j int) bool {
		ei := string(out[i].TipoDocumento) == NFSeADNTipoDocumentoEvento
		ej := string(out[j].TipoDocumento) == NFSeADNTipoDocumentoEvento
		if ei != ej {
			return !ei
		}
		return out[i].NSUValor() < out[j].NSUValor()
	})
	return out
}

// persistirLoteNFSe grava UM lote do ADN numa única transação, incluindo a atualização de
// nfse_certificados.ultimo_nsu para maiorNSU — ou tudo, ou nada.
//
// Ordem: todas as NFSE antes dos EVENTO (ordenarLoteNFSe). Cada documento roda dentro de um
// SAVEPOINT: erro de banco num documento desfaz SÓ aquele documento (ROLLBACK TO SAVEPOINT) e ele
// vai para a quarentena — o lote segue. Também vão para a quarentena (nfse_adn_falhas, na MESMA
// transação do cursor — nada se perde em silêncio): ArquivoXml inválido (base64/gzip/tamanho/XML),
// NFSE sem chave, TipoDocumento ou TipoEvento desconhecido (inclusive numérico) e cancelamento sem
// ChaveAcesso. Só um erro ao gravar a PRÓPRIA quarentena (ou o cursor) aborta o lote.
//
// Por documento:
//   - NFSE: direção pela RAIZ do CNPJ (8 primeiros caracteres) contra a raiz do CNPJ do
//     certificado: prestadora se a raiz de emit/CNPJ ou prest/CNPJ casa → nfse_saidas; tomadora
//     se a raiz de toma/CNPJ casa → nfse_entradas; ambos → as duas tabelas; nenhum → ignorado.
//     Gravada a nota, aplica cancelamento pendente da chave e herda cancelamento da tabela irmã.
//   - EVENTO de cancelamento (EhEventoCancelamentoNFSe): marca cancelada/data_cancelamento nas
//     duas tabelas para a mesma ChaveAcesso; nota inexistente → nfse_cancelamentos_pendentes.
//   - Tipos/eventos conhecidos não tratados: ignorados (contados).
//
// Log: uma linha de resumo por lote (contagens); por documento, só as falhas (com NSU e chave) —
// nunca CNPJ de terceiros.
func persistirLoteNFSe(db *sql.DB, companyID, cnpjEmpresa string, lote NFSeADNLote, maiorNSU int64) (c nfseContagemLote, err error) {
	raizEmpresa := raizCNPJNFSe(cnpjEmpresa)

	tx, err := db.Begin()
	if err != nil {
		return c, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, item := range ordenarLoteNFSe(lote.LoteDFe) {
		if _, serr := tx.Exec(`SAVEPOINT nfse_doc`); serr != nil {
			return c, fmt.Errorf("savepoint: %w", serr)
		}
		var parcial nfseContagemLote
		motivo, derr := processarDocumentoNFSe(tx, companyID, raizEmpresa, item, &parcial)
		if derr != nil {
			if _, rerr := tx.Exec(`ROLLBACK TO SAVEPOINT nfse_doc`); rerr != nil {
				return c, fmt.Errorf("rollback to savepoint (NSU %d, após %v): %w", item.NSUValor(), derr, rerr)
			}
			parcial = nfseContagemLote{} // o que o documento gravou foi desfeito
			motivo = "erro ao gravar: " + derr.Error()
		}
		if motivo != "" {
			if qerr := quarentenarDocumentoNFSe(tx, companyID, item, motivo); qerr != nil {
				return c, qerr
			}
			log.Printf("[NFSeProcessor] NSU=%d chave=%q tipo=%q evento=%q em quarentena (nfse_adn_falhas): %q",
				item.NSUValor(), strings.TrimSpace(item.ChaveAcesso), item.TipoDocumento, item.TipoEvento, TruncateRunes(motivo, 300))
			parcial.falhas++
		}
		if _, rerr := tx.Exec(`RELEASE SAVEPOINT nfse_doc`); rerr != nil {
			return c, fmt.Errorf("release savepoint: %w", rerr)
		}
		c.somar(parcial)
	}

	r, err := tx.Exec(`UPDATE nfse_certificados SET ultimo_nsu = GREATEST(ultimo_nsu, $2) WHERE company_id = $1`, companyID, maiorNSU)
	if err != nil {
		return c, fmt.Errorf("atualizar ultimo_nsu: %w", err)
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return c, fmt.Errorf("atualizar ultimo_nsu: certificado da empresa não encontrado (removido durante o ciclo?)")
	}

	if cerr := tx.Commit(); cerr != nil {
		return c, fmt.Errorf("commit tx: %w", cerr)
	}

	log.Printf("[NFSeProcessor] Lote gravado companyID=%s NSU≤%d documentos=%d novos=%d atualizados=%d ignorados=%d (nao_parte=%d tipo_nao_tratado=%d evento_nao_cancelamento=%d nsu_antigo=%d) falhas=%d cancelamentos=%d pendentes=%d pendentes_aplicados=%d campos_invalidos=%d",
		companyID, maiorNSU, len(lote.LoteDFe), c.novos, c.atualizados, c.ignorados, c.naoParte, c.tipoNaoTratado, c.eventoNaoCanc, c.obsoletos,
		c.falhas, c.cancelamentos, c.pendentes, c.pendentesAplicados, c.camposInvalidos)
	return c, nil
}

// processarDocumentoNFSe trata UM documento dentro do savepoint corrente. motivo != "" = o
// documento vai para a quarentena (falha de decodificação/tipo desconhecido); err != nil = erro
// de banco (o chamador desfaz o savepoint e põe o documento em quarentena).
func processarDocumentoNFSe(tx *sql.Tx, companyID, raizEmpresa string, item NFSeADNDistribuicao, c *nfseContagemLote) (motivo string, err error) {
	nsu := item.NSUValor()
	chave := strings.TrimSpace(item.ChaveAcesso)
	tipoDoc := string(item.TipoDocumento)

	switch {
	case tipoDoc == NFSeADNTipoDocumentoNFSe:
		xmlBytes, derr := decodificarArquivoXmlADN(item.ArquivoXml)
		if derr != nil {
			return "ArquivoXml: " + derr.Error(), nil
		}
		dados, invalidos, xerr := extrairDadosNFSeXMLDetalhado(xmlBytes)
		if xerr != nil {
			return xerr.Error(), nil
		}
		if chave == "" {
			chave = strings.TrimPrefix(dados.IDInfNFSe, "NFS")
		}
		if chave == "" {
			return "NFS-e sem ChaveAcesso nem Id em infNFSe", nil
		}
		if len(invalidos) > 0 {
			log.Printf("[NFSeProcessor] NSU=%d campo(s) decimal(is) não interpretável(is) gravado(s) como 0: %q", nsu, invalidos)
			c.camposInvalidos += len(invalidos)
		}

		ehPrestador := raizEmpresa != "" &&
			(raizCNPJNFSe(dados.CNPJEmitente) == raizEmpresa || raizCNPJNFSe(dados.CNPJPrestador) == raizEmpresa)
		ehTomador := raizEmpresa != "" && raizCNPJNFSe(dados.CNPJTomador) == raizEmpresa
		if !ehPrestador && !ehTomador {
			c.ignorados++
			c.naoParte++
			return "", nil
		}

		doc := nfseDocumentoGravar{
			identificador: chave,
			cnpjPrestador: documentoParteNFSe([]string{dados.CNPJPrestador, dados.CNPJEmitente}, firstNonEmpty(dados.CPFPrestador, dados.CPFEmitente)),
			cnpjTomador:   documentoParteNFSe([]string{dados.CNPJTomador}, dados.CPFTomador),
			nsu:           nsu,
			arquivoXml:    item.ArquivoXml,
			dados:         dados,
		}
		var tabelas []nfseTabela
		if ehTomador {
			tabelas = append(tabelas, nfseTabelaEntradas)
		}
		if ehPrestador {
			tabelas = append(tabelas, nfseTabelaSaidas)
		}
		gravou := false
		for _, tab := range tabelas {
			res, ierr := upsertNFSeDocumento(tx, tab, companyID, doc)
			if ierr != nil {
				return "", ierr
			}
			switch res {
			case nfseUpsertInserido:
				c.novos++
				gravou = true
			case nfseUpsertAtualizado:
				c.atualizados++
				gravou = true
			case nfseUpsertObsoleto:
				c.obsoletos++
			}
		}
		if len(tabelas) > 0 && c.obsoletos > 0 && !gravou {
			c.ignorados++ // documento inteiro descartado por NSU antigo
		}
		if gravou {
			if aerr := aplicarCancelamentosNotaNFSe(tx, companyID, chave, tabelas, c); aerr != nil {
				return "", aerr
			}
		}
		return "", nil

	case tipoDoc == NFSeADNTipoDocumentoEvento:
		tipoEvento := string(item.TipoEvento)
		if !EhEventoCancelamentoNFSe(tipoEvento) {
			if nfseADNEventosNaoCancelamento[tipoEvento] {
				c.ignorados++
				c.eventoNaoCanc++
				return "", nil
			}
			return fmt.Sprintf("TipoEvento desconhecido %q", TruncateRunes(tipoEvento, 60)), nil
		}
		if chave == "" {
			return "evento de cancelamento sem ChaveAcesso", nil
		}
		var dataCanc interface{}
		if t, ok := parseDataHoraADN(item.DataHoraGeracao); ok {
			dataCanc = t
		}
		afetadas := 0
		for _, q := range []string{nfseCancelarEntradasQuery, nfseCancelarSaidasQuery} {
			r, uerr := tx.Exec(q, companyID, chave, dataCanc)
			if uerr != nil {
				return "", fmt.Errorf("cancelamento da NFS-e: %w", uerr)
			}
			n, _ := r.RowsAffected()
			afetadas += int(n)
		}
		if afetadas > 0 {
			c.cancelamentos += afetadas
			return "", nil
		}
		// Nota ainda não gravada: guarda o cancelamento para aplicar quando ela chegar. O
		// primeiro evento registrado prevalece.
		if _, perr := tx.Exec(`INSERT INTO nfse_cancelamentos_pendentes (company_id, chave_acesso, data_cancelamento, tipo_evento, nsu)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT (company_id, chave_acesso) DO NOTHING`,
			companyID, chave, dataCanc, tipoEvento, nullableNFSeNSU(item.NSU)); perr != nil {
			return "", fmt.Errorf("registrar cancelamento pendente: %w", perr)
		}
		c.pendentes++
		return "", nil

	case nfseADNTiposDocumentoNaoTratados[tipoDoc]:
		c.ignorados++
		c.tipoNaoTratado++
		return "", nil

	default:
		return fmt.Sprintf("TipoDocumento desconhecido %q", TruncateRunes(tipoDoc, 60)), nil
	}
}

// aplicarCancelamentosNotaNFSe, logo depois de gravar a nota nas tabelas dadas: aplica o
// cancelamento pendente da chave (e remove a pendência) e herda o cancelamento da tabela irmã.
func aplicarCancelamentosNotaNFSe(tx *sql.Tx, companyID, chave string, tabelas []nfseTabela, c *nfseContagemLote) error {
	for _, tab := range tabelas {
		pendQ, herdarQ := nfseAplicarPendenteEntradasQuery, nfseHerdarCancelamentoEntradasQuery
		if tab == nfseTabelaSaidas {
			pendQ, herdarQ = nfseAplicarPendenteSaidasQuery, nfseHerdarCancelamentoSaidasQuery
		}
		for _, q := range []string{pendQ, herdarQ} {
			r, err := tx.Exec(q, companyID, chave)
			if err != nil {
				return fmt.Errorf("aplicar cancelamento à nota recém-gravada: %w", err)
			}
			n, _ := r.RowsAffected()
			c.cancelamentos += int(n)
		}
	}
	r, err := tx.Exec(`DELETE FROM nfse_cancelamentos_pendentes WHERE company_id = $1 AND chave_acesso = $2`, companyID, chave)
	if err != nil {
		return fmt.Errorf("remover cancelamento pendente: %w", err)
	}
	if n, _ := r.RowsAffected(); n > 0 {
		c.pendentesAplicados += int(n)
	}
	return nil
}

// quarentenarDocumentoNFSe grava (ou atualiza, se o NSU já estava em quarentena) o documento em
// nfse_adn_falhas, com o ArquivoXml exatamente como recebido.
func quarentenarDocumentoNFSe(tx *sql.Tx, companyID string, item NFSeADNDistribuicao, motivo string) error {
	if _, err := tx.Exec(`INSERT INTO nfse_adn_falhas (company_id, nsu, chave_acesso, tipo_documento, tipo_evento, motivo, arquivo_xml)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (company_id, nsu) DO UPDATE SET
			chave_acesso = EXCLUDED.chave_acesso, tipo_documento = EXCLUDED.tipo_documento, tipo_evento = EXCLUDED.tipo_evento,
			motivo = EXCLUDED.motivo, arquivo_xml = EXCLUDED.arquivo_xml, created_at = NOW()`,
		companyID, nullableNFSeNSU(item.NSU), nullableNFSeTexto(strings.TrimSpace(item.ChaveAcesso)),
		nullableNFSeTexto(string(item.TipoDocumento)), nullableNFSeTexto(string(item.TipoEvento)),
		TruncateRunes(motivo, 2000), nullableNFSeTexto(item.ArquivoXml)); err != nil {
		return fmt.Errorf("gravar quarentena nfse_adn_falhas (NSU %d): %w", item.NSUValor(), err)
	}
	return nil
}

// parseDataHoraADN interpreta DataHoraGeracao (date-time do Swagger) com as mesmas formas
// aceitas para dhEmi (parseDataHoraNFSe); sem fuso, assume America/Sao_Paulo.
func parseDataHoraADN(v string) (time.Time, bool) {
	return parseDataHoraNFSe(v)
}

func nullableNFSeNSU(n *int64) interface{} {
	if n == nil {
		return nil
	}
	return *n
}

func nullableNFSeCNPJ(c string) interface{} {
	if c == "" {
		return nil
	}
	return c
}

func nullableNFSeTexto(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
