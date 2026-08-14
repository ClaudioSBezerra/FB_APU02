import { useRef, useEffect } from 'react'
import { BrowserRouter, Routes, Route, Navigate, useLocation, Link } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Toaster } from '@/components/ui/sonner'
import TabelaAliquotas from './pages/TabelaAliquotas'
import TabelaCFOP from './pages/TabelaCFOP'
import TabelaFornSimples from './pages/TabelaFornSimples'
import ApelidosFiliais from './pages/ApelidosFiliais'
import GestaoAmbiente from './pages/GestaoAmbiente'
import Managers from './pages/Managers'
import RFBCredentials from './pages/RFBCredentials'
import SAPCredentials from './pages/SAPCredentials'
import SAPSyncHistory from './pages/SAPSyncHistory'
import RFBApuracao from './pages/RFBApuracao'
import RFBDebitos from './pages/RFBDebitos'
import RFBCreditosCBS from './pages/RFBCreditosCBS'
import RFBPagamentosFornecedores from './pages/RFBPagamentosFornecedores'
import SimuladorProjetado from './pages/SimuladorProjetado'
import GestaoCredIBSCBS from './pages/GestaoCredIBSCBS'
import PainelApuracaoIBS from './pages/PainelApuracaoIBS'
import PainelApuracaoCBS from './pages/PainelApuracaoCBS'
import NotasImportadasModulo from './pages/NotasImportadasModulo'
import ERPBridgeConfig from './pages/ERPBridgeConfig'
import ERPBridgeLogs from './pages/ERPBridgeLogs'
import ERPBridgeCredenciais from './pages/ERPBridgeCredenciais'
import ImportarXMLsEntrada from './pages/ImportarXMLsEntrada'
import ImportarXMLsSaida from './pages/ImportarXMLsSaida'
import ImportarXMLsCTe from './pages/ImportarXMLsCTe'
import DFesModulo from './pages/DFesModulo'
import ImportarPagamentosFornecedores from './pages/ImportarPagamentosFornecedores'
import CadastroFornecedores from './pages/CadastroFornecedores'
import ApuracaoCredPerdidos from './pages/ApuracaoCredPerdidos'
import MalhaFinaModulo from './pages/MalhaFinaModulo'
import ArgusPortal from './pages/ArgusPortal'
import AdminUsers from './pages/AdminUsers'
import UserActivity from './pages/UserActivity'
import LimparDadosApuracao from './pages/LimparDadosApuracao'
import CGIBSPainel from './pages/CGIBSPainel'
import CGIBSApuracao from './pages/CGIBSApuracao'
import CGIBSCredentials from './pages/CGIBSCredentials'
import CGIBSDebitos from './pages/CGIBSDebitos'
import DashboardResumo from './pages/DashboardResumo'
import Login from './pages/Login'
import Register from './pages/Register'
import ForgotPassword from './pages/ForgotPassword'
import ResetPassword from './pages/ResetPassword'
import { AppRail } from '@/components/AppRail'
import { FilialSelector } from '@/components/FilialSelector'
import { CompanySwitcher } from '@/components/CompanySwitcher'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import { FilialProvider } from './contexts/FilialContext'
import { getActiveModule, modules } from '@/lib/navigation'
import { cn } from '@/lib/utils'

const queryClient = new QueryClient()

function ComingSoon({ title }: { title: string }) {
  return (
    <div className="flex flex-col items-center justify-center h-[50vh] space-y-4">
      <h1 className="text-2xl font-bold text-muted-foreground">{title}</h1>
      <p className="text-sm text-muted-foreground">Este módulo está em desenvolvimento.</p>
    </div>
  )
}

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, loading } = useAuth()
  const location = useLocation()
  if (loading) return null
  if (!isAuthenticated) return <Navigate to="/login" state={{ from: location }} replace />
  return <>{children}</>
}

function AdminRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, loading, user } = useAuth()
  const location = useLocation()
  if (loading) return null
  if (!isAuthenticated) return <Navigate to="/login" state={{ from: location }} replace />
  if (user?.role !== 'admin') return <Navigate to="/" replace />
  return <>{children}</>
}

// ── Tracking de atividade por rota ───────────────────────────────────────────
function getModuleLabel(pathname: string): string | null {
  for (const moduleConfig of Object.values(modules)) {
    const tab = moduleConfig.tabs.find(t => t.path === pathname && !t.disabled)
    if (tab) return tab.label
  }
  const moduleId = getActiveModule(pathname)
  if (moduleId === 'painel') return null
  return modules[moduleId]?.label ?? null
}

function useRouteActivityLogger() {
  const location  = useLocation()
  const prevPath  = useRef(location.pathname)
  const startTime = useRef(Date.now())

  useEffect(() => {
    if (prevPath.current === location.pathname) return
    const duration = Math.floor((Date.now() - startTime.current) / 1000)
    const label    = getModuleLabel(prevPath.current)
    if (label && duration >= 3) {
      fetch('/api/activity/log', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ module: label, duration_seconds: duration }),
        keepalive: true,
      }).catch(() => {})
    }
    prevPath.current  = location.pathname
    startTime.current = Date.now()
  }, [location.pathname])

  useEffect(() => {
    return () => {
      const duration = Math.floor((Date.now() - startTime.current) / 1000)
      const label    = getModuleLabel(prevPath.current)
      if (label && duration >= 3) {
        fetch('/api/activity/log', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ module: label, duration_seconds: duration }),
          keepalive: true,
        }).catch(() => {})
      }
    }
  }, [])
}

// ── Barra de abas por módulo ─────────────────────────────────────────────────
function ModuleTabs() {
  const location   = useLocation()
  const { user }   = useAuth()
  const isAdmin    = user?.role === 'admin'
  const moduleId   = getActiveModule(location.pathname)
  const moduleCfg  = modules[moduleId]

  if (!moduleCfg || moduleCfg.tabs.length === 0) return null

  const visibleTabs = moduleCfg.tabs.filter(t => !t.adminOnly || isAdmin)

  return (
    <div key={moduleId} className="border-b bg-white px-4 flex items-center gap-0.5 overflow-x-auto shrink-0 h-10">
      {visibleTabs.map(tab => {
        const isActive   = location.pathname === tab.path
        const isDisabled = tab.disabled
        return isDisabled ? (
          <span
            key={tab.path}
            className="px-3 py-1.5 text-xs rounded-md text-muted-foreground/50 cursor-not-allowed whitespace-nowrap"
          >
            {tab.label}
          </span>
        ) : (
          <Link
            key={tab.path}
            to={tab.path}
            className={cn(
              'px-3 py-1.5 text-xs rounded-md whitespace-nowrap transition-colors',
              isActive
                ? tab.danger
                  ? 'bg-red-50 text-red-700 font-semibold'
                  : 'bg-primary/10 text-primary font-semibold'
                : tab.danger
                  ? 'text-red-500 hover:bg-red-50 hover:text-red-700'
                  : 'text-muted-foreground hover:bg-gray-100 hover:text-foreground'
            )}
          >
            {tab.label}
          </Link>
        )
      })}
    </div>
  )
}

// ── Submenu vertical por módulo ──────────────────────────────────────────────
function ModuleSideNav() {
  const location   = useLocation()
  const { user }   = useAuth()
  const isAdmin    = user?.role === 'admin'
  const moduleId   = getActiveModule(location.pathname)
  const moduleCfg  = modules[moduleId]

  if (!moduleCfg || moduleCfg.tabs.length === 0) return null

  const visibleTabs = moduleCfg.tabs.filter(t => !t.adminOnly || isAdmin)

  return (
    <aside key={moduleId} className="w-56 shrink-0 border-r bg-white overflow-y-auto">
      <div className="flex flex-col gap-0.5 p-2">
        {visibleTabs.map(tab => {
          const isActive   = location.pathname === tab.path
          const isDisabled = tab.disabled
          return isDisabled ? (
            <span
              key={tab.path}
              className="px-3 py-1.5 text-xs rounded-md text-muted-foreground/50 cursor-not-allowed whitespace-nowrap"
            >
              {tab.label}
            </span>
          ) : (
            <Link
              key={tab.path}
              to={tab.path}
              className={cn(
                'px-3 py-1.5 text-xs rounded-md whitespace-nowrap transition-colors',
                isActive
                  ? tab.danger
                    ? 'bg-red-50 text-red-700 font-semibold'
                    : 'bg-primary/10 text-primary font-semibold'
                  : tab.danger
                    ? 'text-red-500 hover:bg-red-50 hover:text-red-700'
                    : 'text-muted-foreground hover:bg-gray-100 hover:text-foreground'
              )}
            >
              {tab.label}
            </Link>
          )
        })}
      </div>
    </aside>
  )
}

// ── Cabeçalho (módulo + controles globais) ───────────────────────────────────
function AppHeader() {
  const location  = useLocation()
  const moduleId  = getActiveModule(location.pathname)
  const moduleCfg = modules[moduleId]

  return (
    <header className="flex items-center justify-between h-12 border-b bg-white px-4 shrink-0">
      <span className="text-sm font-semibold text-foreground">
        {moduleCfg?.label ?? 'Apuração Assistida'}
      </span>
      <div className="flex items-center gap-2">
        <FilialSelector />
        <CompanySwitcher compact />
      </div>
    </header>
  )
}

// ── Layout principal ─────────────────────────────────────────────────────────
function AppLayout() {
  useRouteActivityLogger()
  const location  = useLocation()
  const moduleId  = getActiveModule(location.pathname)
  const moduleCfg = modules[moduleId]
  const isVertical = moduleCfg?.orientation === 'vertical' && moduleCfg.tabs.length > 0

  const routes = (
    <Routes>
      <Route path="/" element={<Navigate to="/rfb/gestao-creditos" replace />} />

      {/* Painel */}
      <Route path="/painel/resumo-fiscal" element={<DashboardResumo />} />

      {/* Configurações */}
      <Route path="/config/aliquotas"       element={<TabelaAliquotas />} />
      <Route path="/config/cfop"            element={<TabelaCFOP />} />
      <Route path="/config/forn-simples"    element={<TabelaFornSimples />} />
      <Route path="/config/apelidos-filiais" element={<ApelidosFiliais />} />
      <Route path="/config/gestores"        element={<Managers />} />
      <Route path="/config/ambiente"        element={<ProtectedRoute><GestaoAmbiente /></ProtectedRoute>} />
      <Route path="/config/usuarios"        element={<AdminRoute><AdminUsers /></AdminRoute>} />
      <Route path="/config/user-activity"   element={<AdminRoute><UserActivity /></AdminRoute>} />
      <Route path="/config/limpar-dados"    element={<AdminRoute><LimparDadosApuracao /></AdminRoute>} />
      <Route path="/config/erp-bridge"      element={<AdminRoute><ERPBridgeCredenciais /></AdminRoute>} />
      <Route path="/config/sap-credenciais" element={<AdminRoute><SAPCredentials /></AdminRoute>} />
      <Route path="/config/sap-sincronizacoes" element={<AdminRoute><SAPSyncHistory /></AdminRoute>} />
      <Route path="/rfb/credenciais"        element={<RFBCredentials />} />

      {/* ERP Bridge */}
      <Route path="/importacoes/nfe-entrada"      element={<ImportarXMLsEntrada />} />
      <Route path="/importacoes/nfe-saida"       element={<ImportarXMLsSaida />} />
      <Route path="/importacoes/cte-entrada"     element={<ImportarXMLsCTe />} />
      <Route path="/importacoes/pagamentos-fornecedores" element={<ImportarPagamentosFornecedores />} />
      <Route path="/importacoes/erp-bridge"      element={<ERPBridgeConfig />} />
      <Route path="/importacoes/erp-bridge/logs" element={<ERPBridgeLogs />} />
      <Route path="/importacoes/fornecedores"    element={<CadastroFornecedores />} />

      {/* DF-e's */}
      <Route path="/dfes" element={<DFesModulo />} />

      {/* Apuração */}
      <Route path="/apuracao/saida/notas"       element={<NotasImportadasModulo tipoInicial="nfe" direcaoInicial="saida" />} />
      <Route path="/apuracao/entrada/notas"     element={<NotasImportadasModulo tipoInicial="nfe" direcaoInicial="entrada" />} />
      <Route path="/apuracao/cte-entrada/notas" element={<NotasImportadasModulo tipoInicial="cte" direcaoInicial="entrada" />} />
      <Route path="/apuracao/creditos-perdidos" element={<ApuracaoCredPerdidos />} />
      <Route path="/apuracao/limpar-dados"     element={<AdminRoute><LimparDadosApuracao /></AdminRoute>} />
      <Route path="/rfb/apuracao-ibs"           element={<PainelApuracaoIBS />} />
      <Route path="/rfb/apuracao-cbs"           element={<PainelApuracaoCBS />} />

      {/* Malha Fina */}
      <Route path="/malha-fina/nfe-entradas"  element={<MalhaFinaModulo tipoInicial="nfe" direcaoInicial="entrada" />} />
      <Route path="/malha-fina/nfe-saidas"    element={<MalhaFinaModulo tipoInicial="nfe" direcaoInicial="saida" />} />
      <Route path="/malha-fina/cte"           element={<MalhaFinaModulo tipoInicial="cte" direcaoInicial="entrada" />} />

      {/* Portal Argus (Demo) */}
      <Route path="/argus" element={<ArgusPortal />} />

      {/* CGIBS */}
      <Route path="/cgibs/apuracao-ibs"   element={<CGIBSPainel />} />
      <Route path="/cgibs/apuracao"        element={<CGIBSApuracao />} />
      <Route path="/cgibs/debitos"         element={<CGIBSDebitos />} />
      <Route path="/cgibs/credenciais"     element={<AdminRoute><CGIBSCredentials /></AdminRoute>} />

      {/* Receita Federal */}
      <Route path="/rfb/gestao-creditos"        element={<GestaoCredIBSCBS />} />
      <Route path="/rfb/apuracao"               element={<RFBApuracao />} />
      <Route path="/rfb/debitos"                element={<RFBDebitos />} />
      <Route path="/rfb/creditos-cbs"           element={<RFBCreditosCBS />} />
      <Route path="/rfb/pagamentos-cbs"         element={<ComingSoon title="Pagamentos CBS mês corrente" />} />
      <Route path="/rfb/pagamentos-fornecedores" element={<RFBPagamentosFornecedores />} />
      <Route path="/rfb/concluir-apuracao"      element={<ComingSoon title="Concluir apuração mês anterior" />} />
      <Route path="/rfb/simulador-projetado"    element={<SimuladorProjetado />} />
    </Routes>
  )

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      <AppRail />
      <div className="flex flex-col flex-1 min-w-0">
        <AppHeader />
        {!isVertical && <ModuleTabs />}
        <div className="flex flex-1 min-h-0">
          {isVertical && <ModuleSideNav />}
          <main className="flex-1 overflow-auto">
            <div className="p-4">
              {routes}
            </div>
          </main>
        </div>
      </div>
      <Toaster />
    </div>
  )
}

// ── App root ─────────────────────────────────────────────────────────────────
function App() {
  console.log('App Version: 2.0.5 — FB_APU02 Apuração Assistida SAP S/4HANA')
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
        <AuthProvider>
          <Routes>
            <Route path="/login"        element={<Login />} />
            <Route path="/register"     element={<Register />} />
            <Route path="/forgot-password" element={<ForgotPassword />} />
            <Route path="/reset-senha"  element={<ResetPassword />} />
            <Route path="/*" element={
              <ProtectedRoute>
                <FilialProvider>
                  <AppLayout />
                </FilialProvider>
              </ProtectedRoute>
            } />
          </Routes>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  )
}

export default App
