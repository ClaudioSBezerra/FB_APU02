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
  orientation?: 'horizontal' | 'vertical'
}

export const modules: Record<string, ModuleConfig> = {
  painel: {
    label: 'Painel',
    tabs: [
      { label: 'Resumo Fiscal',   path: '/painel/resumo-fiscal' },
      { label: 'Créditos em Risco', path: '/apuracao/creditos-perdidos', danger: true },
    ],
  },
  notas: {
    label: 'Notas Importadas',
    tabs: [],
  },
  importacoes: {
    label: 'Importações VIA ERP',
    tabs: [
      { label: 'Importar via ERP', path: '/importacoes/erp-bridge' },
      { label: 'Logs ERP',         path: '/importacoes/erp-bridge/logs' },
    ],
  },
  cgibs: {
    label: 'CGIBS - Apuração Assistida IBS',
    orientation: 'vertical',
    tabs: [
      { label: 'Apuração IBS',        path: '/cgibs/apuracao-ibs' },
      { label: 'Importar Movimento',  path: '/cgibs/apuracao' },
      { label: 'Débitos IBS',         path: '/cgibs/debitos' },
      { label: 'Créditos IBS',        path: '#', disabled: true },
      { label: 'Pagamentos IBS',      path: '#', disabled: true },
      { label: 'Pgtos Fornecedores',  path: '#', disabled: true },
      { label: 'Concluir Apuração',   path: '#', disabled: true },
    ],
  },
  rfb: {
    label: 'Receita Federal - Apuração Assistida',
    orientation: 'vertical',
    tabs: [
      { label: 'Gestão CBS RFB',       path: '/rfb/gestao-creditos' },
      { label: 'Importar Movimento',  path: '/rfb/apuracao' },
      { label: 'Débitos mês',         path: '/rfb/debitos' },
      { label: 'Créditos CBS',        path: '/rfb/creditos-cbs' },
      { label: 'Pagamentos CBS',      path: '/rfb/pagamentos-cbs',          disabled: true },
      { label: 'Pgtos Fornecedores',       path: '/rfb/pagamentos-fornecedores' },
      { label: 'Gestão Eventos Cred/Deb.', path: '/rfb/gestao-eventos-cred-deb', disabled: true },
      { label: 'Concluir apuração',        path: '/rfb/concluir-apuracao',        disabled: true },
    ],
  },
  malha: {
    label: 'Malha Fina',
    tabs: [],
  },
  dfes: {
    label: "Importação DFe-s e Outros Docs",
    tabs: [],
  },
  argus: {
    label: 'Portal Fiscal CBS/IBS (Demo)',
    tabs: [
      { label: 'Portal', path: '/argus' },
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
      { label: 'Credenciais RFB',    path: '/rfb/credenciais',    adminOnly: true },
      { label: 'Credenciais CGIBS', path: '/cgibs/credenciais',  adminOnly: true },
      { label: 'Cred. ERP Bridge',  path: '/config/erp-bridge',  adminOnly: true },
      { label: 'Credenciais SAP',   path: '/config/sap-credenciais', adminOnly: true },
      { label: 'Sincronizações SAP', path: '/config/sap-sincronizacoes', adminOnly: true },
      { label: 'Usuários',          path: '/config/usuarios',        adminOnly: true },
      { label: 'Atividade',         path: '/config/user-activity',   adminOnly: true },
      { label: 'Limpeza de Base',    path: '/config/limpar-dados', danger: true, adminOnly: true },
    ],
  },
}

export function getActiveModule(pathname: string): string {
  if (pathname === '/' || pathname === '/apuracao/creditos-perdidos') return 'painel'

  if (pathname.includes('/notas')) return 'notas'

  if (pathname.startsWith('/importacoes/')) return 'importacoes'

  if (pathname.startsWith('/dfes')) return 'dfes'

  const cgibsPaths = ['/cgibs/apuracao-ibs', '/cgibs/apuracao', '/cgibs/debitos']
  if (cgibsPaths.some(p => pathname.startsWith(p))) return 'cgibs'

  if (pathname.startsWith('/rfb/') && pathname !== '/rfb/credenciais') return 'rfb'

  if (pathname.startsWith('/malha-fina/')) return 'malha'

  if (pathname.startsWith('/argus')) return 'argus'

  if (pathname.startsWith('/config/') || pathname === '/rfb/credenciais' || pathname === '/cgibs/credenciais') return 'config'

  return 'painel'
}
