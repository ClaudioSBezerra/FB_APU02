/**
 * Formata CPF completo com pontuação
 * Entrada: "12345678901" → Saída: "123.456.789-01"
 */
export function formatCPF(cpf: string | null | undefined): string {
  if (!cpf) return '';
  const cleaned = cpf.replace(/\D/g, '');
  if (cleaned.length !== 11) return cpf;
  return cleaned.replace(/^(\d{3})(\d{3})(\d{3})(\d{2})$/, '$1.$2.$3-$4');
}

/**
 * Formata CNPJ completo com pontuação
 * Entrada: "12345678000190" → Saída: "12.345.678/0001-90"
 */
export function formatCNPJ(cnpj: string | null | undefined): string {
  if (!cnpj) return '';
  const cleaned = cnpj.replace(/\D/g, '');
  if (cleaned.length !== 14) return cnpj;
  return cleaned.replace(/^(\d{2})(\d{3})(\d{3})(\d{4})(\d{2})$/, '$1.$2.$3/$4-$5');
}

/**
 * Formata CPF ou CNPJ completo com pontuação (detecta automaticamente)
 */
export function formatDocumento(doc: string | null | undefined): string {
  if (!doc) return '';
  const cleaned = doc.replace(/\D/g, '');
  if (cleaned.length === 11) return formatCPF(doc);
  if (cleaned.length === 14) return formatCNPJ(doc);
  return doc;
}

/**
 * Formata CPF com máscara de privacidade
 * Entrada: "12345678901" → Saída: "***.***.***-01"
 * Retorna string original se não for CPF válido de 11 dígitos
 */
export function formatCPFMasked(cpf: string | null | undefined): string {
  if (!cpf) return '';
  const cleaned = cpf.replace(/\D/g, '');
  if (cleaned.length !== 11) return cpf;
  
  // Máscara de privacidade: ***.***.***-{dv}
  const dv = cleaned.substring(9, 11);
  return `***.***.***-${dv}`;
}

/**
 * Formata CNPJ com máscara completa
 * Entrada: "10230480000189" → Saída: "**********\0001-89"
 * Retorna string original se não for CNPJ válido de 14 dígitos
 */
export function formatCNPJMasked(cnpj: string | null | undefined): string {
  if (!cnpj) return '';
  const cleaned = cnpj.replace(/\D/g, '');
  if (cleaned.length !== 14) return cnpj;
  
  // Máscara de privacidade: **.***.***/{filial}-{dv}
  const filial = cleaned.substring(8, 12);
  const dv = cleaned.substring(12, 14);
  return `**.***.***/${filial}-${dv}`;
}

/**
 * Formata CPF ou CNPJ com máscara de privacidade
 * Detecta automaticamente pelo tamanho: 11 dígitos = CPF, 14 dígitos = CNPJ
 */
export function formatDocumentoMasked(doc: string | null | undefined): string {
  if (!doc) return '';
  const cleaned = doc.replace(/\D/g, '');
  
  if (cleaned.length === 11) {
    return formatCPFMasked(doc);
  }
  if (cleaned.length === 14) {
    return formatCNPJMasked(doc);
  }
  
  return doc;
}

/**
 * Formata o display de uma filial com COD_EST e CNPJ
 * Formato: FC010102 - 10230480001889
 */
export function formatFilialDisplay(codEst: string | null | undefined, cnpj: string | null | undefined): string {
  const cleanedCnpj = cnpj?.replace(/\D/g, '') || '';
  
  if (codEst && codEst.trim()) {
    return `${codEst} - ${cleanedCnpj}`;
  }
  
  // Fallback: só o CNPJ limpo
  return cleanedCnpj;
}

/**
 * Formata o display de uma filial com COD_EST e CNPJ formatado
 * Formato: FC010102 - 10.230.480/0001-89
 */
export function formatFilialDisplayFormatted(codEst: string | null | undefined, cnpj: string | null | undefined): string {
  const formattedCnpj = formatCNPJ(cnpj);
  
  if (codEst && codEst.trim()) {
    return `${codEst} - ${formattedCnpj}`;
  }
  
  return formattedCnpj;
}

/**
 * Extrai o COD_EST e CNPJ de um nome de filial que pode conter ambos
 * Suporta formatos:
 *   - "FC011001 - 10230480000130"
 *   - "FC011001 - 10.230.480/0001-30"
 *   - "10230480000130"
 *   - "10.230.480/0001-30"
 */
export function parseFilialName(filialNome: string): { codEst: string | null; cnpj: string | null } {
  if (!filialNome) return { codEst: null, cnpj: null };
  
  // Verifica se o nome está no formato "COD_EST - CNPJ" (14 dígitos)
  const matchCodCnpj = filialNome.match(/^([A-Z0-9]+)\s*-\s*(\d{14})$/);
  if (matchCodCnpj) {
    return { codEst: matchCodCnpj[1], cnpj: matchCodCnpj[2] };
  }
  
  // Verifica formato "COD_EST - CNPJ formatado"
  const matchCodCnpjFormatted = filialNome.match(/^([A-Z0-9]+)\s*-\s*(\d{2}\.\d{3}\.\d{3}\/\d{4}-\d{2})$/);
  if (matchCodCnpjFormatted) {
    return { codEst: matchCodCnpjFormatted[1], cnpj: matchCodCnpjFormatted[2].replace(/\D/g, '') };
  }
  
  // Extrai CNPJ do nome se presente (14 dígitos ou formatado)
  const cnpjMatch = filialNome.match(/(\d{2}\.?\d{3}\.?\d{3}\/?\d{4}-?\d{2})/);
  if (cnpjMatch) {
    return { codEst: null, cnpj: cnpjMatch[1].replace(/\D/g, '') };
  }
  
  return { codEst: null, cnpj: null };
}

/**
 * Helper robusto para formatação de filial a partir de um row de dados
 * Lida com casos onde filial_cnpj está vazio mas filial_nome contém as informações
 */
export function formatFilialFromRow(row: {
  filial_cod_est?: string | null;
  filial_cnpj?: string | null;
  filial_nome?: string | null;
}): string {
  // Se temos cod_est e cnpj válido, usa diretamente
  const cleanedCnpj = row.filial_cnpj?.replace(/\D/g, '') || '';
  if (cleanedCnpj.length === 14) {
    return formatFilialDisplayFormatted(row.filial_cod_est, row.filial_cnpj);
  }
  
  // Fallback: tenta extrair do filial_nome
  if (row.filial_nome) {
    const parsed = parseFilialName(row.filial_nome);
    if (parsed.cnpj && parsed.cnpj.length === 14) {
      // Usa cod_est do row se existir, senão usa o extraído do nome
      const codEstFinal = row.filial_cod_est?.trim() || parsed.codEst;
      return formatFilialDisplayFormatted(codEstFinal, parsed.cnpj);
    }
  }
  
  // Último fallback: retorna o que tiver disponível
  if (row.filial_cod_est?.trim()) {
    return row.filial_cod_est;
  }
  
  return row.filial_nome || '-';
}

/**
 * Returns "XX.XXX.XXX/XXXX-XX - APELIDO" if an alias exists for the CNPJ,
 * otherwise returns just the formatted CNPJ.
 */
export function formatCnpjComApelido(
  cnpj: string,
  apelidos: Record<string, string>
): string {
  const digits = cnpj.replace(/\D/g, "");
  const apelido = apelidos[digits];
  const cnpjFormatado = formatCNPJMasked(digits);
  return apelido ? `${cnpjFormatado} - ${apelido}` : cnpjFormatado;
}
