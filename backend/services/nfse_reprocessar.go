// Package services — reprocessamento das NFS-e já gravadas quando o parser do XML evolui
// (spec-nfse-ibscbs-xml.md): reextrai os campos de xml_gzip_b64 das linhas com
// xml_parser_versao < NFSeXMLParserVersao.
package services

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

// nfseReprocessarLote é o número de linhas reextraídas por transação — lotes pequenos para não
// segurar locks nem uma conexão do pool por muito tempo durante o boot.
const nfseReprocessarLote = 200

// nfseReprocessarEsperaMigration: no boot, as migrations rodam em paralelo ao scheduler
// (main.go/onDBConnected). O reprocessamento espera a coluna xml_parser_versao existir, checando
// a cada nfseReprocessarEsperaIntervalo, por no máximo nfseReprocessarEsperaMax.
var (
	nfseReprocessarEsperaIntervalo = 10 * time.Second
	nfseReprocessarEsperaMax       = 15 * time.Minute
)

// NFSeResultadoReprocessamento resume uma execução de ReprocessarXMLNFSe.
type NFSeResultadoReprocessamento struct {
	Atualizadas     int // linhas reextraídas e gravadas
	Falhas          int // linhas cujo XML não pôde ser lido/gravado (marcadas mesmo assim, ver abaixo)
	CamposInvalidos int // campos decimais presentes mas não interpretáveis
}

// nfseUpdateXMLEntradasQuery/nfseUpdateXMLSaidasQuery atualizam SÓ as colunas extraídas do XML
// (nfseColunasXML, inclusive xml_parser_versao) — nunca cancelada/data_cancelamento/nsu/xml.
// $1 = id; $2.. = valoresXMLNFSe.
var (
	nfseUpdateXMLEntradasQuery = montarUpdateXMLNFSe("nfse_entradas")
	nfseUpdateXMLSaidasQuery   = montarUpdateXMLNFSe("nfse_saidas")
)

func montarUpdateXMLNFSe(tabela string) string {
	sets := make([]string, len(nfseColunasXML))
	for i, c := range nfseColunasXML {
		sets[i] = fmt.Sprintf("%s = $%d", c, i+2)
	}
	return "UPDATE " + tabela + " SET " + strings.Join(sets, ", ") + " WHERE id = $1"
}

// IniciarReprocessamentoXMLNFSe roda ReprocessarXMLNFSe em goroutine (não bloqueia o boot), depois
// de esperar a migration 135. Panic é recuperado e logado.
func IniciarReprocessamentoXMLNFSe(db *sql.DB) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[NFSeReprocessar] PANIC recuperado: %v", r)
			}
		}()
		if !aguardarColunaParserVersaoNFSe(db) {
			log.Printf("[NFSeReprocessar] coluna xml_parser_versao não apareceu em %s — reprocessamento não executado neste boot", nfseReprocessarEsperaMax)
			return
		}
		if _, err := ReprocessarXMLNFSe(db); err != nil {
			log.Printf("[NFSeReprocessar] interrompido: %v", err)
		}
	}()
}

// aguardarColunaParserVersaoNFSe espera a migration 135 ter criado xml_parser_versao nas duas tabelas.
func aguardarColunaParserVersaoNFSe(db *sql.DB) bool {
	limite := time.Now().Add(nfseReprocessarEsperaMax)
	for {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
			WHERE table_name IN ('nfse_entradas', 'nfse_saidas') AND column_name = 'xml_parser_versao'`).Scan(&n)
		if err == nil && n == 2 {
			return true
		}
		if time.Now().After(limite) {
			return false
		}
		time.Sleep(nfseReprocessarEsperaIntervalo)
	}
}

// ReprocessarXMLNFSe reextrai de xml_gzip_b64 as linhas de nfse_entradas e nfse_saidas com
// xml_parser_versao < NFSeXMLParserVersao, em lotes de nfseReprocessarLote linhas por transação,
// atualizando só os campos extraídos do XML e marcando a versão atual. Uma linha que falha
// (ArquivoXml/XML inválido ou erro de banco ao gravar) é logada e marcada com a versão atual
// mesmo assim — nunca é reprocessada em loop. Linhas sem xml_gzip_b64 (anteriores à 134) ficam
// como estão: não há o que reextrair.
//
// Erro só quando o lote em si não pode ser lido/gravado (o que já foi commitado fica).
func ReprocessarXMLNFSe(db *sql.DB) (NFSeResultadoReprocessamento, error) {
	var total NFSeResultadoReprocessamento
	inicio := time.Now()
	for _, tab := range []nfseTabela{nfseTabelaEntradas, nfseTabelaSaidas} {
		for {
			n, err := reprocessarLoteXMLNFSe(db, tab, &total)
			if err != nil {
				return total, err
			}
			if n == 0 {
				break
			}
		}
	}
	if total.Atualizadas > 0 || total.Falhas > 0 {
		log.Printf("[NFSeReprocessar] Concluído em %s: atualizadas=%d falhas=%d campos_invalidos=%d (parser v%d)",
			time.Since(inicio).Round(time.Millisecond), total.Atualizadas, total.Falhas, total.CamposInvalidos, NFSeXMLParserVersao)
	}
	return total, nil
}

// reprocessarLoteXMLNFSe processa UM lote de uma tabela numa transação. Devolve quantas linhas
// foram marcadas (0 = nada pendente).
func reprocessarLoteXMLNFSe(db *sql.DB, tab nfseTabela, total *NFSeResultadoReprocessamento) (int, error) {
	var selectQ, updateQ, marcarQ, nomeTabela string
	switch tab {
	case nfseTabelaEntradas:
		nomeTabela, updateQ = "nfse_entradas", nfseUpdateXMLEntradasQuery
		selectQ = `SELECT id, nsu, xml_gzip_b64 FROM nfse_entradas
			WHERE xml_parser_versao < $1 AND xml_gzip_b64 IS NOT NULL ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED`
		marcarQ = `UPDATE nfse_entradas SET xml_parser_versao = $2 WHERE id = $1`
	case nfseTabelaSaidas:
		nomeTabela, updateQ = "nfse_saidas", nfseUpdateXMLSaidasQuery
		selectQ = `SELECT id, nsu, xml_gzip_b64 FROM nfse_saidas
			WHERE xml_parser_versao < $1 AND xml_gzip_b64 IS NOT NULL ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED`
		marcarQ = `UPDATE nfse_saidas SET xml_parser_versao = $2 WHERE id = $1`
	default:
		return 0, fmt.Errorf("tabela NFS-e desconhecida: %v", tab)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	type linha struct {
		id  string
		nsu sql.NullInt64
		xml string
	}
	rows, err := tx.Query(selectQ, NFSeXMLParserVersao, nfseReprocessarLote)
	if err != nil {
		return 0, fmt.Errorf("selecionar pendentes de %s: %w", nomeTabela, err)
	}
	var linhas []linha
	for rows.Next() {
		var l linha
		if err := rows.Scan(&l.id, &l.nsu, &l.xml); err != nil {
			rows.Close()
			return 0, fmt.Errorf("ler pendente de %s: %w", nomeTabela, err)
		}
		linhas = append(linhas, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterar pendentes de %s: %w", nomeTabela, err)
	}
	if len(linhas) == 0 {
		return 0, nil
	}

	for _, l := range linhas {
		motivo := ""
		xmlBytes, derr := decodificarArquivoXmlADN(l.xml)
		var dados NFSeDadosXML
		var invalidos []string
		if derr != nil {
			motivo = "ArquivoXml: " + derr.Error()
		} else if dados, invalidos, derr = extrairDadosNFSeXMLDetalhado(xmlBytes); derr != nil {
			motivo = derr.Error()
		}
		if motivo == "" {
			if len(invalidos) > 0 {
				log.Printf("[NFSeReprocessar] %s id=%s NSU=%d campo(s) decimal(is) não interpretável(is) (gravados como 0; IBS/CBS: NULL): %q",
					nomeTabela, l.id, l.nsu.Int64, invalidos)
				total.CamposInvalidos += len(invalidos)
			}
			if _, err := tx.Exec(`SAVEPOINT nfse_reproc`); err != nil {
				return 0, fmt.Errorf("savepoint: %w", err)
			}
			args := append([]interface{}{l.id}, valoresXMLNFSe(dados)...)
			if _, uerr := tx.Exec(updateQ, args...); uerr != nil {
				if _, rerr := tx.Exec(`ROLLBACK TO SAVEPOINT nfse_reproc`); rerr != nil {
					return 0, fmt.Errorf("rollback to savepoint (id %s, após %v): %w", l.id, uerr, rerr)
				}
				motivo = "erro ao gravar: " + uerr.Error()
			} else if _, err := tx.Exec(`RELEASE SAVEPOINT nfse_reproc`); err != nil {
				return 0, fmt.Errorf("release savepoint: %w", err)
			}
		}
		if motivo != "" {
			// Falha na linha: loga e marca a versão mesmo assim (não repete em loop a cada boot).
			log.Printf("[NFSeReprocessar] %s id=%s NSU=%d não reprocessada (marcada v%d): %s",
				nomeTabela, l.id, l.nsu.Int64, NFSeXMLParserVersao, TruncateRunes(motivo, 300))
			if _, err := tx.Exec(marcarQ, l.id, NFSeXMLParserVersao); err != nil {
				return 0, fmt.Errorf("marcar versão em %s id %s: %w", nomeTabela, l.id, err)
			}
			total.Falhas++
			continue
		}
		total.Atualizadas++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return len(linhas), nil
}
