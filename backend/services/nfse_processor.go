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

// ExecutarCicloNFSe executa UM ciclo completo de busca de NFS-e no Portal Nacional ADN para
// companyID: valida o certificado ativo, descriptografa, monta o client mTLS, extrai o CNPJ
// titular, busca todos os documentos disponíveis (BuscarTodosDocumentosADN) e persiste os novos
// em nfse_entradas/nfse_saidas, sem duplicar. Chamada tanto pelo disparo manual
// (handlers.BuscarNFSeHandler) quanto pelo agendado (StartNFSeScheduler) — nenhum dos dois
// conhece os detalhes de como o ciclo é executado, só o resultado (recebidos, novos, ignorados,
// err).
//
// ctx é propagado para BuscarTodosDocumentosADN — permite que o chamador HTTP (handler) cancele
// a chamada mTLS em andamento se o cliente desconectar (r.Context()), e que o scheduler imponha
// um timeout próprio por empresa (ver nfse_scheduler.go) em vez de depender só do timeout interno
// de 5 minutos de BuscarTodosDocumentosADN.
//
// recebidos é SEMPRE len(documentos) devolvidos pelo ADN, ANTES de qualquer filtragem — contado
// independentemente de novos/ignorados. Isso importa porque novos/ignorados NÃO incrementam
// quando um documento já existia de um ciclo anterior (ON CONFLICT DO NOTHING, 0 linhas
// afetadas): sem um contador à parte, um ciclo repetido com só documentos duplicados reportaria
// "0 recebidos" mesmo tendo vindo N documentos do governo (ver Spec Change Log, loopback 1, item
// 1, de spec-nfse-agendamento-persistencia.md).
//
// NÃO verifica NFSeBuscaRL — rate limiting é responsabilidade de quem chama (handler ou
// scheduler), igual ao uso de services.SolicitarApuracaoParaEmpresa no módulo RFB (o limite
// diário é resolvido pelo chamador).
//
// Os erros devolvidos são sempre um dos ErrNFSe* sentinela acima (envolvendo o erro original via
// %w, continua funcionando com errors.Is) ou um ErrNFSeADN* de nfse_adn.go (propagado sem
// modificação quando a falha ocorre na etapa de busca no ADN).
func ExecutarCicloNFSe(ctx context.Context, db *sql.DB, companyID string) (recebidos, novos, ignorados int, err error) {
	// Mesma query de status calculado usada em GetNFSeCertificadoHandler
	// (handlers/nfse_credentials.go) e no scheduler (nfse_scheduler.go): um certificado com
	// status='ativo' persistido mas validade já vencida é reportado como 'vencido' mesmo que a
	// coluna ainda diga 'ativo'.
	var pfxEnc, senhaEnc, status string
	qerr := db.QueryRow(`
		SELECT certificado_pfx, certificado_senha,
		       CASE WHEN status = 'ativo' AND validade IS NOT NULL AND validade < CURRENT_DATE
		            THEN 'vencido' ELSE status END
		FROM nfse_certificados WHERE company_id = $1
	`, companyID).Scan(&pfxEnc, &senhaEnc, &status)
	if qerr == sql.ErrNoRows {
		return 0, 0, 0, ErrNFSeCertificadoNaoCadastrado
	}
	if qerr != nil {
		return 0, 0, 0, fmt.Errorf("%w: consulta certificado: %v", ErrNFSeCertificadoProcessamento, qerr)
	}
	if status != "ativo" {
		return 0, 0, 0, ErrNFSeCertificadoIndisponivel
	}

	certDecrypted, derr := DecryptNFSeCertificado(pfxEnc, senhaEnc)
	if derr != nil {
		return 0, 0, 0, fmt.Errorf("%w: %v", ErrNFSeCertificadoProcessamento, derr)
	}

	httpClient, x509Cert, berr := BuildNFSeMTLSClient(certDecrypted)
	if berr != nil {
		log.Printf("[NFSeProcessor] Falha ao montar client mTLS companyID=%s: %v", companyID, berr)
		return 0, 0, 0, fmt.Errorf("%w: %v", ErrNFSeMTLSClientInvalido, berr)
	}

	cnpjEmpresa, cerr := ExtractCNPJFromCertificateSubject(x509Cert)
	if cerr != nil {
		log.Printf("[NFSeProcessor] Falha ao identificar CNPJ do certificado companyID=%s: %v", companyID, cerr)
		return 0, 0, 0, fmt.Errorf("%w: %v", ErrNFSeCNPJNaoIdentificado, cerr)
	}

	baseURL := ResolveNFSeADNBaseURL()
	documentos, aerr := BuscarTodosDocumentosADN(ctx, httpClient, baseURL)
	recebidos = len(documentos)
	if aerr != nil {
		switch {
		case errors.Is(aerr, ErrNFSeADNFormatoInesperado):
			log.Printf("[NFSeProcessor] Formato de resposta inesperado do ADN companyID=%s: %v", companyID, aerr)
		case errors.Is(aerr, ErrNFSeADNComunicacaoFalhou):
			log.Printf("[NFSeProcessor] Falha de comunicação com o Portal Nacional ADN companyID=%s: %v", companyID, aerr)
		default:
			log.Printf("[NFSeProcessor] Falha na comunicação com o Portal Nacional ADN companyID=%s: %v", companyID, aerr)
		}
		return recebidos, 0, 0, aerr
	}

	novos, ignorados, perr := persistirDocumentosNFSe(db, companyID, cnpjEmpresa, documentos)
	if perr != nil {
		return recebidos, novos, ignorados, fmt.Errorf("%w: %v", ErrNFSePersistencia, perr)
	}
	return recebidos, novos, ignorados, nil
}

// nfseTabela identifica a tabela de destino de um INSERT de documento NFS-e — usado em vez de
// montar o nome da tabela por fmt.Sprintf na query (prática arriscada de copiar num refactor
// futuro): as duas queries abaixo são strings literais, escolhidas via switch.
type nfseTabela int

const (
	nfseTabelaEntradas nfseTabela = iota
	nfseTabelaSaidas
)

const nfseInsertEntradasQuery = `
	INSERT INTO nfse_entradas (company_id, identificador_nfse, cnpj_prestador, cnpj_tomador, data_emissao, mes_ano, valor_servicos)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (company_id, identificador_nfse) DO NOTHING
`

const nfseInsertSaidasQuery = `
	INSERT INTO nfse_saidas (company_id, identificador_nfse, cnpj_prestador, cnpj_tomador, data_emissao, mes_ano, valor_servicos)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (company_id, identificador_nfse) DO NOTHING
`

// inserirNFSeDocumento executa o INSERT idempotente (ON CONFLICT DO NOTHING) de um documento em
// nfse_entradas ou nfse_saidas, dentro da transação tx. Devolve quantas linhas foram
// efetivamente inseridas (0 ou 1).
func inserirNFSeDocumento(tx *sql.Tx, tabela nfseTabela, companyID, identificador, cnpjPrestador, cnpjTomador string, dataEmissao, mesAno interface{}, valorServicos float64) (int, error) {
	var query, nomeTabela string
	switch tabela {
	case nfseTabelaEntradas:
		query = nfseInsertEntradasQuery
		nomeTabela = "nfse_entradas"
	case nfseTabelaSaidas:
		query = nfseInsertSaidasQuery
		nomeTabela = "nfse_saidas"
	default:
		return 0, fmt.Errorf("tabela NFS-e desconhecida: %v", tabela)
	}

	result, err := tx.Exec(query, companyID, identificador, nullableNFSeCNPJ(cnpjPrestador), nullableNFSeCNPJ(cnpjTomador), dataEmissao, mesAno, valorServicos)
	if err != nil {
		return 0, fmt.Errorf("insert em %s: %w", nomeTabela, err)
	}
	rows, rerr := result.RowsAffected()
	if rerr != nil {
		log.Printf("[NFSeProcessor] Falha ao obter RowsAffected após insert em %s (documento %s): %v", nomeTabela, identificador, rerr)
	}
	return int(rows), nil
}

// persistirDocumentosNFSe grava cada documento em nfse_entradas (a empresa, identificada pelo
// CNPJ titular do certificado mTLS, é a tomadora) ou nfse_saidas (a empresa é a prestadora),
// com ON CONFLICT (company_id, identificador_nfse) DO NOTHING — idempotente entre ciclos
// repetidos (manuais ou agendados). Devolve quantos documentos foram efetivamente inseridos
// (novos) e quantos foram ignorados (CNPJ não reconhecido). Todo o lote é gravado dentro de uma
// única transação — se um INSERT falhar no meio, nenhuma linha fica parcialmente persistida.
//
// Documentos onde o CNPJ do certificado não é nem prestador nem tomador são ignorados (só
// logados) — não deveriam ocorrer dado que a consulta ao ADN é feita com o certificado da
// própria empresa, mas defensivamente nunca gravamos um documento sem saber de que lado a
// empresa está. O mesmo vale para CNPJs de prestador/tomador que, quando informados, não têm
// exatamente 14 dígitos — tratados como CNPJ não reconhecido.
//
// Quando a empresa é simultaneamente prestadora E tomadora do mesmo documento (autoemitido —
// situação pouco comum), o documento é gravado em AMBAS as tabelas.
func persistirDocumentosNFSe(db *sql.DB, companyID, cnpjEmpresa string, documentos []NFSeADNDocumento) (novos int, ignorados int, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, doc := range documentos {
		identificador := doc.IdentificadorNFSe
		if identificador == "" {
			continue
		}

		cnpjPrestador := OnlyDigits(doc.CNPJPrestador)
		cnpjTomador := OnlyDigits(doc.CNPJTomador)

		if (cnpjPrestador != "" && len(cnpjPrestador) != 14) || (cnpjTomador != "" && len(cnpjTomador) != 14) {
			log.Printf("[NFSeProcessor] Documento %s ignorado — CNPJ prestador (%q) ou tomador (%q) não tem 14 dígitos",
				identificador, doc.CNPJPrestador, doc.CNPJTomador)
			ignorados++
			continue
		}

		var dataEmissao interface{}
		var mesAno interface{}
		if t, perr := time.Parse("2006-01-02", doc.DataEmissao); perr == nil {
			dataEmissao = t
			mesAno = t.Format("01/2006")
		} else {
			log.Printf("[NFSeProcessor] Documento %s — data de emissão não pôde ser interpretada (valor recebido: %q): %v",
				identificador, doc.DataEmissao, perr)
		}

		ehTomador := cnpjTomador != "" && cnpjEmpresa == cnpjTomador
		ehPrestador := cnpjPrestador != "" && cnpjEmpresa == cnpjPrestador

		if !ehTomador && !ehPrestador {
			log.Printf("[NFSeProcessor] Documento %s ignorado — CNPJ do certificado (%s) não é prestador (%s) nem tomador (%s)",
				identificador, cnpjEmpresa, cnpjPrestador, cnpjTomador)
			ignorados++
			continue
		}

		if ehTomador && ehPrestador {
			log.Printf("[NFSeProcessor] Documento %s autoemitido — CNPJ do certificado (%s) é prestador e tomador simultaneamente, gravando em nfse_entradas e nfse_saidas",
				identificador, cnpjEmpresa)
			n, ierr := inserirNFSeDocumento(tx, nfseTabelaEntradas, companyID, identificador, cnpjPrestador, cnpjTomador, dataEmissao, mesAno, doc.ValorServicos)
			if ierr != nil {
				return novos, ignorados, ierr
			}
			novos += n
			n, ierr = inserirNFSeDocumento(tx, nfseTabelaSaidas, companyID, identificador, cnpjPrestador, cnpjTomador, dataEmissao, mesAno, doc.ValorServicos)
			if ierr != nil {
				return novos, ignorados, ierr
			}
			novos += n
			continue
		}

		tabela := nfseTabelaSaidas
		if ehTomador {
			tabela = nfseTabelaEntradas
		}
		n, ierr := inserirNFSeDocumento(tx, tabela, companyID, identificador, cnpjPrestador, cnpjTomador, dataEmissao, mesAno, doc.ValorServicos)
		if ierr != nil {
			return novos, ignorados, ierr
		}
		novos += n
	}

	if cerr := tx.Commit(); cerr != nil {
		return novos, ignorados, fmt.Errorf("commit tx: %w", cerr)
	}
	return novos, ignorados, nil
}

func nullableNFSeCNPJ(c string) interface{} {
	if c == "" {
		return nil
	}
	return c
}
