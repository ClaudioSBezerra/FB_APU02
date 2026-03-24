export interface ModuleTab {
  label: string
  path: string
  disabled?: boolean
  danger?: boolean
}

export interface ModuleConfig {
  label: string
  tabs: ModuleTab[]
}

export const modules: Record<string, ModuleConfig> = {
  painel: {
    label: 'Painel',
    tabs: [],
  },
  importacoes: {
    label: 'Importações',
    tabs: [
      { label: 'NF-e Saídas',    path: '/apuracao/saida' },
      { label: 'NF-e Entradas',  path: '/apuracao/entrada' },
      { label: 'CT-e Entradas',  path: '/apuracao/cte-entrada' },
      { label: 'NFS-e',          path: '#', disabled: true },
      { label: 'ERP Bridge',     path: '/importacoes/erp-bridge' },
      { label: 'Logs Bridge',    path: '/importacoes/erp-bridge/logs' },
      { label: 'Limpar Dados',   path: '/importacoes/limpar-dados', danger: true },
    ],
  },
  apuracao: {
    label: 'Apuração IBS / CBS',
    tabs: [
      { label: 'NF-e Entradas',      path: '/apuracao/entrada/notas' },
      { label: 'NF-e Saídas',        path: '/apuracao/saida/notas' },
      { label: 'CT-e Entradas',      path: '/apuracao/cte-entrada/notas' },
      { label: 'Créditos em Risco',  path: '/apuracao/creditos-perdidos', danger: true },
      { label: 'Apuração IBS',       path: '/rfb/apuracao-ibs' },
      { label: 'Apuração CBS',       path: '/rfb/apuracao-cbs' },
      { label: 'Limpar Dados',       path: '/apuracao/limpar-dados', danger: true },
    ],
  },
  rfb: {
    label: 'Receita Federal',
    tabs: [
      { label: 'Gestão IBS/CBS',      path: '/rfb/gestao-creditos' },
      { label: 'Importar Débitos',    path: '/rfb/apuracao' },
      { label: 'Débitos mês',         path: '/rfb/debitos' },
      { label: 'Créditos CBS',        path: '/rfb/creditos-cbs',            disabled: true },
      { label: 'Pagamentos CBS',      path: '/rfb/pagamentos-cbs',          disabled: true },
      { label: 'Pgtos Fornecedores',  path: '/rfb/pagamentos-fornecedores', disabled: true },
      { label: 'Concluir apuração',   path: '/rfb/concluir-apuracao',       disabled: true },
    ],
  },
  malha: {
    label: 'Malha Fina',
    tabs: [
      { label: 'Resumo Geral',   path: '/malha-fina/resumo-geral' },
      { label: 'NF-e Entradas',  path: '/malha-fina/nfe-entradas' },
      { label: 'NF-e Saídas',    path: '/malha-fina/nfe-saidas' },
      { label: 'CT-e',           path: '/malha-fina/cte' },
      { label: 'NFS-e Entradas', path: '#', disabled: true },
      { label: 'NFS-e Saídas',   path: '#', disabled: true },
    ],
  },
  config: {
    label: 'Configurações',
    tabs: [
      { label: 'Alíquotas',        path: '/config/aliquotas' },
      { label: 'CFOP',             path: '/config/cfop' },
      { label: 'Simples Nacional', path: '/config/forn-simples' },
      { label: 'Apelidos Filiais', path: '/config/apelidos-filiais' },
      { label: 'Gestores',         path: '/config/gestores' },
      { label: 'Ambiente',         path: '/config/ambiente' },
      { label: 'Credenciais RFB',  path: '/rfb/credenciais' },
      { label: 'Usuários',         path: '/config/usuarios' },
      { label: 'Limpar Dados',     path: '/config/limpar-dados', danger: true },
    ],
  },
}

export function getActiveModule(pathname: string): string {
  if (pathname === '/') return 'painel'

  const importPaths = ['/apuracao/saida', '/apuracao/entrada', '/apuracao/cte-entrada']
  if (importPaths.includes(pathname) || pathname.startsWith('/importacoes/')) return 'importacoes'

  const apuracaoPaths = ['/apuracao/creditos-perdidos', '/rfb/apuracao-ibs', '/rfb/apuracao-cbs', '/apuracao/limpar-dados']
  if (pathname.includes('/notas') || apuracaoPaths.includes(pathname)) return 'apuracao'

  const rfbExclude = ['/rfb/credenciais', '/rfb/apuracao-ibs', '/rfb/apuracao-cbs']
  if (pathname.startsWith('/rfb/') && !rfbExclude.includes(pathname)) return 'rfb'

  if (pathname.startsWith('/malha-fina/')) return 'malha'

  if (pathname.startsWith('/config/') || pathname === '/rfb/credenciais') return 'config'

  return 'painel'
}
