export interface ModuleTab {
  label: string
  path: string
  disabled?: boolean
  danger?: boolean
  adminOnly?: boolean
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
  notas: {
    label: 'Notas Importadas',
    tabs: [
      { label: 'NF-e Entradas',    path: '/apuracao/entrada/notas' },
      { label: 'NF-e Saídas',      path: '/apuracao/saida/notas' },
      { label: 'CT-e Entradas',    path: '/apuracao/cte-entrada/notas' },
      { label: 'CT-e Saídas',      path: '#', disabled: true },
      { label: 'NFS-e Entradas',   path: '#', disabled: true },
      { label: 'NFS-e Saídas',     path: '#', disabled: true },
      { label: 'BP-e',             path: '#', disabled: true },
      { label: 'NF3-e',            path: '#', disabled: true },
      { label: 'NFCom-e',          path: '#', disabled: true },
      { label: 'NFag-e',           path: '#', disabled: true },
      { label: 'NF-e ABI',         path: '#', disabled: true },
      { label: 'ND-e',             path: '#', disabled: true },
      { label: 'NC-e',             path: '#', disabled: true },
      { label: 'Importar ERP',     path: '/importacoes/erp-bridge',      adminOnly: true },
      { label: 'Logs Importação',  path: '/importacoes/erp-bridge/logs', adminOnly: true },
    ],
  },
  apuracao: {
    label: 'Apuração IBS / CBS',
    tabs: [
      { label: 'Créditos em Risco',  path: '/apuracao/creditos-perdidos', danger: true },
      { label: 'Apuração IBS',       path: '/rfb/apuracao-ibs' },
      { label: 'Apuração CBS',       path: '/rfb/apuracao-cbs' },
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
      { label: 'Pgtos Fornecedores',       path: '/rfb/pagamentos-fornecedores',  disabled: true },
      { label: 'Gestão Eventos Cred/Deb.', path: '/rfb/gestao-eventos-cred-deb', disabled: true },
      { label: 'Concluir apuração',        path: '/rfb/concluir-apuracao',        disabled: true },
    ],
  },
  malha: {
    label: 'Malha Fina',
    tabs: [
      { label: 'NF-e Entradas',    path: '/malha-fina/nfe-entradas' },
      { label: 'NF-e Saídas',      path: '/malha-fina/nfe-saidas' },
      { label: 'CT-e Entradas',    path: '/malha-fina/cte' },
      { label: 'CT-e Saídas',      path: '#', disabled: true },
      { label: 'NFS-e Entradas',   path: '#', disabled: true },
      { label: 'NFS-e Saídas',     path: '#', disabled: true },
      { label: 'BP-e',             path: '#', disabled: true },
      { label: 'NF3-e',            path: '#', disabled: true },
      { label: 'NFCom-e',          path: '#', disabled: true },
      { label: 'NFag-e',           path: '#', disabled: true },
      { label: 'NF-e ABI',         path: '#', disabled: true },
      { label: 'ND-e',             path: '#', disabled: true },
      { label: 'NC-e',             path: '#', disabled: true },
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
      { label: 'Credenciais RFB',   path: '/rfb/credenciais',    adminOnly: true },
      { label: 'Cred. ERP Bridge',  path: '/config/erp-bridge',  adminOnly: true },
      { label: 'Usuários',          path: '/config/usuarios',    adminOnly: true },
      { label: 'Limpar Dados',      path: '/config/limpar-dados', danger: true, adminOnly: true },
    ],
  },
}

export function getActiveModule(pathname: string): string {
  if (pathname === '/') return 'painel'

  if (pathname.includes('/notas')) return 'notas'

  if (pathname.startsWith('/importacoes/')) return 'notas'

  const apuracaoPaths = ['/apuracao/creditos-perdidos', '/rfb/apuracao-ibs', '/rfb/apuracao-cbs']
  if (apuracaoPaths.includes(pathname)) return 'apuracao'

  const rfbExclude = ['/rfb/credenciais', '/rfb/apuracao-ibs', '/rfb/apuracao-cbs']
  if (pathname.startsWith('/rfb/') && !rfbExclude.includes(pathname)) return 'rfb'

  if (pathname.startsWith('/malha-fina/')) return 'malha'

  if (pathname.startsWith('/config/') || pathname === '/rfb/credenciais') return 'config'

  return 'painel'
}
