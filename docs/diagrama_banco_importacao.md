# Diagrama do Banco de Dados — Tabelas de Importação
## FB_APU02 — Apuração Assistida IBS/CBS (Ferreira Costa)

---

## Tabelas Principais (dados importados)

### `nfe_saidas` — NF-e / NFC-e de Saída
Importada do Oracle via ERP Bridge. Representa as notas fiscais **emitidas** pelas filiais.

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| id | UUID PK | Identificador único |
| company_id | UUID FK → companies | Empresa no FBTax |
| chave_nfe | VARCHAR(44) | Chave de acesso 44 dígitos (UNIQUE por empresa) |
| modelo | SMALLINT | 55 = NF-e / 65 = NFC-e |
| serie | VARCHAR(3) | Série da nota |
| numero_nfe | VARCHAR(9) | Número da nota |
| data_emissao | DATE | Data de emissão |
| mes_ano | VARCHAR(7) | MM/YYYY — padrão do projeto |
| nat_op | VARCHAR(60) | Natureza da operação |
| **Emitente (Filial)** | | |
| emit_cnpj | VARCHAR(14) | CNPJ da filial emitente |
| emit_nome | VARCHAR(60) | Razão social da filial |
| emit_uf | VARCHAR(2) | UF da filial |
| emit_municipio | VARCHAR(60) | Município da filial |
| **Destinatário** | | |
| dest_cnpj_cpf | VARCHAR(14) | CNPJ ou CPF do destinatário |
| dest_nome | VARCHAR(60) | Nome/razão social do destinatário |
| dest_uf | VARCHAR(2) | UF do destinatário |
| dest_c_mun | VARCHAR(7) | Código IBGE do município |
| **ICMSTot** | | |
| v_bc | NUMERIC(15,2) | Base de cálculo ICMS |
| v_icms | NUMERIC(15,2) | Valor ICMS |
| v_icms_deson | NUMERIC(15,2) | ICMS desonerado |
| v_fcp | NUMERIC(15,2) | Fundo de Combate à Pobreza |
| v_bc_st | NUMERIC(15,2) | Base ICMS ST |
| v_st | NUMERIC(15,2) | Valor ICMS ST |
| v_fcp_st | NUMERIC(15,2) | FCP ST |
| v_fcp_st_ret | NUMERIC(15,2) | FCP ST retido |
| v_prod | NUMERIC(15,2) | Valor total dos produtos |
| v_frete | NUMERIC(15,2) | Frete |
| v_seg | NUMERIC(15,2) | Seguro |
| v_desc | NUMERIC(15,2) | Desconto |
| v_ii | NUMERIC(15,2) | Imposto de importação |
| v_ipi | NUMERIC(15,2) | IPI |
| v_ipi_devol | NUMERIC(15,2) | IPI devolvido |
| v_pis | NUMERIC(15,2) | PIS |
| v_cofins | NUMERIC(15,2) | COFINS |
| v_outro | NUMERIC(15,2) | Outros valores |
| v_nf | NUMERIC(15,2) | **Valor total da nota** |
| **IBSCBSTot — Reforma Tributária** | | |
| v_bc_ibs_cbs | NUMERIC(15,2) | Base única IBS+CBS (nullable) |
| v_ibs_uf | NUMERIC(15,2) | IBS Estadual (nullable) |
| v_ibs_mun | NUMERIC(15,2) | IBS Municipal (nullable) |
| v_ibs | NUMERIC(15,2) | Total IBS (nullable) |
| v_cred_pres_ibs | NUMERIC(15,2) | Crédito presumido IBS (nullable) |
| v_cbs | NUMERIC(15,2) | CBS (nullable) |
| v_cred_pres_cbs | NUMERIC(15,2) | Crédito presumido CBS (nullable) |
| created_at | TIMESTAMPTZ | Data de importação |

---

### `nfe_entradas` — NF-e / NFC-e de Entrada
Importada do Oracle via ERP Bridge. Representa as notas fiscais **recebidas** pelas filiais.

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| id | UUID PK | Identificador único |
| company_id | UUID FK → companies | Empresa no FBTax |
| chave_nfe | VARCHAR(44) | Chave de acesso 44 dígitos (UNIQUE por empresa) |
| modelo | SMALLINT | 55 = NF-e / 65 = NFC-e |
| serie | VARCHAR(3) | Série da nota |
| numero_nfe | VARCHAR(9) | Número da nota |
| data_emissao | DATE | Data de emissão |
| mes_ano | VARCHAR(7) | MM/YYYY |
| nat_op | VARCHAR(60) | Natureza da operação |
| **Fornecedor (emitente)** | | |
| forn_cnpj | VARCHAR(14) | CNPJ do fornecedor |
| forn_nome | VARCHAR(60) | Razão social do fornecedor |
| forn_uf | VARCHAR(2) | UF do fornecedor |
| forn_municipio | VARCHAR(60) | Município do fornecedor |
| **Destinatário (Filial)** | | |
| dest_cnpj_cpf | VARCHAR(14) | CNPJ da filial que recebeu |
| dest_nome | VARCHAR(60) | Nome da filial |
| dest_uf | VARCHAR(2) | UF da filial |
| dest_c_mun | VARCHAR(7) | Código IBGE do município |
| **ICMSTot** | | |
| v_bc | NUMERIC(15,2) | Base de cálculo ICMS |
| v_icms | NUMERIC(15,2) | Valor ICMS |
| v_icms_deson | NUMERIC(15,2) | ICMS desonerado |
| v_fcp | NUMERIC(15,2) | FCP |
| v_bc_st | NUMERIC(15,2) | Base ICMS ST |
| v_st | NUMERIC(15,2) | Valor ICMS ST |
| v_fcp_st | NUMERIC(15,2) | FCP ST |
| v_fcp_st_ret | NUMERIC(15,2) | FCP ST retido |
| v_prod | NUMERIC(15,2) | Valor total dos produtos |
| v_frete | NUMERIC(15,2) | Frete |
| v_seg | NUMERIC(15,2) | Seguro |
| v_desc | NUMERIC(15,2) | Desconto |
| v_ii | NUMERIC(15,2) | Imposto de importação |
| v_ipi | NUMERIC(15,2) | IPI |
| v_ipi_devol | NUMERIC(15,2) | IPI devolvido |
| v_pis | NUMERIC(15,2) | PIS |
| v_cofins | NUMERIC(15,2) | COFINS |
| v_outro | NUMERIC(15,2) | Outros valores |
| v_nf | NUMERIC(15,2) | **Valor total da nota** |
| **IBSCBSTot — Reforma Tributária** | | |
| v_bc_ibs_cbs | NUMERIC(15,2) | Base única IBS+CBS (NOT NULL DEFAULT 0) |
| v_ibs_uf | NUMERIC(15,2) | IBS Estadual (NOT NULL DEFAULT 0) |
| v_ibs_mun | NUMERIC(15,2) | IBS Municipal (NOT NULL DEFAULT 0) |
| v_ibs | NUMERIC(15,2) | Total IBS (NOT NULL DEFAULT 0) |
| v_cred_pres_ibs | NUMERIC(15,2) | Crédito presumido IBS (NOT NULL DEFAULT 0) |
| v_cbs | NUMERIC(15,2) | CBS (NOT NULL DEFAULT 0) |
| v_cred_pres_cbs | NUMERIC(15,2) | Crédito presumido CBS (NOT NULL DEFAULT 0) |
| created_at | TIMESTAMPTZ | Data de importação |

> **Nota:** IBS/CBS são NOT NULL DEFAULT 0 em entradas — fornecedores sem as tags da Reforma ficam com zero, facilitando o cálculo de créditos em risco.

---

### `cte_entradas` — CT-e de Entrada (Conhecimento de Transporte)
Importada do Oracle via ERP Bridge. Representa os fretes **recebidos** pelas filiais.

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| id | UUID PK | Identificador único |
| company_id | UUID FK → companies | Empresa no FBTax |
| chave_cte | VARCHAR(44) | Chave de acesso 44 dígitos (UNIQUE por empresa) |
| modelo | SMALLINT | Sempre 57 (CT-e) |
| serie | VARCHAR(3) | Série |
| numero_cte | VARCHAR(9) | Número do CT-e |
| data_emissao | DATE | Data de emissão |
| mes_ano | VARCHAR(7) | MM/YYYY |
| nat_op | VARCHAR(60) | Natureza da operação |
| cfop | VARCHAR(4) | CFOP |
| modal | VARCHAR(2) | 01=Rodoviário / 02=Aéreo / 03=Aquaviário / 04=Ferroviário |
| **Transportadora (emitente)** | | |
| emit_cnpj | VARCHAR(14) | CNPJ da transportadora |
| emit_nome | VARCHAR(60) | Razão social da transportadora |
| emit_uf | VARCHAR(2) | UF da transportadora |
| **Remetente (origem da carga)** | | |
| rem_cnpj_cpf | VARCHAR(14) | CNPJ/CPF do remetente |
| rem_nome | VARCHAR(60) | Nome do remetente |
| rem_uf | VARCHAR(2) | UF do remetente |
| **Destinatário (destino da carga)** | | |
| dest_cnpj_cpf | VARCHAR(14) | CNPJ/CPF do destinatário |
| dest_nome | VARCHAR(60) | Nome do destinatário |
| dest_uf | VARCHAR(2) | UF do destinatário |
| **Valores da Prestação** | | |
| v_prest | NUMERIC(15,2) | Total da prestação do serviço |
| v_rec | NUMERIC(15,2) | Valor a receber |
| v_carga | NUMERIC(15,2) | Valor da carga |
| v_bc_icms | NUMERIC(15,2) | Base de cálculo ICMS |
| v_icms | NUMERIC(15,2) | Valor ICMS |
| **IBSCBSTot — Reforma Tributária** | | |
| v_bc_ibs_cbs | NUMERIC(15,2) | Base IBS+CBS (nullable) |
| v_ibs | NUMERIC(15,2) | Total IBS (nullable) |
| v_cbs | NUMERIC(15,2) | CBS (nullable) |
| created_at | TIMESTAMPTZ | Data de importação |

> **Nota:** IBS/CBS são nullable em CT-e — transportadoras que ainda não implementaram a Reforma ficam com NULL (exibido como "—" na tela de Créditos em Risco).

---

### `dfe_xml` — XML Bruto dos Documentos Fiscais
Armazena o XML original para geração de DANFE/DACTE sem dependência de serviços externos.

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| id | UUID PK | Identificador único |
| company_id | UUID FK → companies | Empresa no FBTax |
| chave | VARCHAR(44) | Chave do documento (UNIQUE por empresa) |
| tipo | VARCHAR(5) | 'nfe' / 'nfce' / 'cte' |
| modelo | SMALLINT | 55 / 65 / 57 |
| xml_raw | TEXT | XML completo do documento |
| created_at | TIMESTAMPTZ | Data de importação |

---

## Tabelas de Controle — ERP Bridge

### `erp_bridge_config` — Configuração por Empresa
| Coluna | Tipo | Descrição |
|--------|------|-----------|
| company_id | UUID PK FK | Empresa |
| ativo | BOOLEAN | Agendamento ativo? |
| horario | TIME | Horário diário de execução |
| dias_retroativos | INTEGER | Quantos dias atrás importar |
| ultimo_run_em | TIMESTAMPTZ | Data/hora do último run |
| fbtax_email | TEXT | E-mail FBTax (criptografado AES-256) |
| fbtax_password | TEXT | Senha FBTax (criptografada) |
| oracle_usuario | TEXT | Usuário Oracle (criptografado) |
| oracle_senha | TEXT | Senha Oracle (criptografada) |
| api_key | TEXT | Chave de API do daemon |
| api_key_hash | TEXT | SHA-256 da api_key (para lookup) |

### `erp_bridge_runs` — Histórico de Execuções
| Coluna | Tipo | Descrição |
|--------|------|-----------|
| id | UUID PK | Identificador do run |
| company_id | UUID FK | Empresa |
| iniciado_em | TIMESTAMPTZ | Início da execução |
| finalizado_em | TIMESTAMPTZ | Fim da execução |
| status | TEXT | running / success / partial / error |
| data_ini | DATE | Período inicial importado |
| data_fim | DATE | Período final importado |
| total_enviados | INTEGER | Total de documentos importados |
| total_ignorados | INTEGER | Total já existentes (ignorados) |
| total_erros | INTEGER | Total de erros |
| erro_msg | TEXT | Mensagem de erro geral |
| origem | TEXT | manual / scheduler |

### `erp_bridge_run_items` — Detalhe por Servidor/Tipo
| Coluna | Tipo | Descrição |
|--------|------|-----------|
| id | UUID PK | Identificador |
| run_id | UUID FK → erp_bridge_runs | Run pai |
| servidor | TEXT | Nome da filial/servidor Oracle |
| tipo | TEXT | nfe_saidas / nfe_entradas / cte_entradas |
| enviados | INTEGER | Documentos importados |
| ignorados | INTEGER | Já existiam no banco |
| erros | INTEGER | Erros neste servidor/tipo |
| status | TEXT | ok / erro_conexao / erro_query / erro_parcial |
| erro_msg | TEXT | Detalhes do erro |

### `erp_bridge_servidores` — Servidores Registrados (online)
| Coluna | Tipo | Descrição |
|--------|------|-----------|
| company_id | UUID FK | Empresa |
| nome | TEXT | Nome da filial |
| updated_at | TIMESTAMPTZ | Última vez que o daemon registrou |

---

## Relacionamentos

```
companies
  ├── nfe_saidas        (1:N — company_id)
  ├── nfe_entradas      (1:N — company_id)
  ├── cte_entradas      (1:N — company_id)
  ├── dfe_xml           (1:N — company_id, chave = chave_nfe ou chave_cte)
  ├── erp_bridge_config (1:1 — company_id)
  └── erp_bridge_runs   (1:N — company_id)
        └── erp_bridge_run_items (1:N — run_id)

erp_bridge_servidores (registrado pelo daemon ao iniciar)
```

---

## Fonte dos Dados (Oracle → FBTax)

| Tabela FBTax | Tabela Oracle | Mecanismo |
|-------------|--------------|-----------|
| nfe_saidas | nfe_saidas | ERP Bridge (Python) lê via cx_Oracle e envia XML para `/api/nfe-saidas/upload` |
| nfe_entradas | nfe_entradas | ERP Bridge lê via cx_Oracle e envia XML para `/api/nfe-entradas/upload` |
| cte_entradas | cte_entradas | ERP Bridge lê via cx_Oracle e envia XML para `/api/cte-entradas/upload` |
| dfe_xml | dfe_xml | XML bruto lido do Oracle e armazenado junto com o cabeçalho |

> O ERP Bridge lê o campo `xml_doc` (CLOB/BLOB) do Oracle, parseia o XML e envia para o backend Go que extrai os campos e popula as tabelas acima.
