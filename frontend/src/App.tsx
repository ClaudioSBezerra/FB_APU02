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
import RFBApuracao from './pages/RFBApuracao'
import RFBDebitos from './pages/RFBDebitos'
import GestaoCredIBSCBS from './pages/GestaoCredIBSCBS'
import PainelApuracaoIBS from './pages/PainelApuracaoIBS'
import PainelApuracaoCBS from './pages/PainelApuracaoCBS'
import ImportarXMLsSaida from './pages/ImportarXMLsSaida'
import ConsultaNFeSaidas from './pages/ConsultaNFeSaidas'
import ImportarXMLsEntrada from './pages/ImportarXMLsEntrada'
import ConsultaNFesEntradas from './pages/ConsultaNFesEntradas'
import ImportarXMLsCTe from './pages/ImportarXMLsCTe'
import ConsultaCTesEntradas from './pages/ConsultaCTesEntradas'
import ApuracaoCredPerdidos from './pages/ApuracaoCredPerdidos'
import AdminUsers from './pages/AdminUsers'
import LimparDadosApuracao from './pages/LimparDadosApuracao'
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

// ── Barra de abas por módulo ─────────────────────────────────────────────────
function ModuleTabs() {
  const location   = useLocation()
  const moduleId   = getActiveModule(location.pathname)
  const moduleCfg  = modules[moduleId]

  if (!moduleCfg || moduleCfg.tabs.length === 0) return null

  return (
    <div className="border-b bg-white px-4 flex items-center gap-0.5 overflow-x-auto shrink-0 h-10">
      {moduleCfg.tabs.map(tab => {
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
  return (
    <div className="flex h-screen overflow-hidden bg-background">
      <AppRail />
      <div className="flex flex-col flex-1 min-w-0">
        <AppHeader />
        <ModuleTabs />
        <main className="flex-1 overflow-auto">
          <div className="p-4">
            <Routes>
              <Route path="/" element={<Navigate to="/rfb/gestao-creditos" replace />} />

              {/* Configurações */}
              <Route path="/config/aliquotas"       element={<TabelaAliquotas />} />
              <Route path="/config/cfop"            element={<TabelaCFOP />} />
              <Route path="/config/forn-simples"    element={<TabelaFornSimples />} />
              <Route path="/config/apelidos-filiais" element={<ApelidosFiliais />} />
              <Route path="/config/gestores"        element={<Managers />} />
              <Route path="/config/ambiente"        element={<ProtectedRoute><GestaoAmbiente /></ProtectedRoute>} />
              <Route path="/config/usuarios"        element={<AdminRoute><AdminUsers /></AdminRoute>} />
              <Route path="/config/limpar-dados"    element={<AdminRoute><LimparDadosApuracao /></AdminRoute>} />
              <Route path="/rfb/credenciais"        element={<RFBCredentials />} />

              {/* Importações */}
              <Route path="/apuracao/saida"         element={<ImportarXMLsSaida />} />
              <Route path="/apuracao/entrada"       element={<ImportarXMLsEntrada />} />
              <Route path="/apuracao/cte-entrada"   element={<ImportarXMLsCTe />} />

              {/* Apuração */}
              <Route path="/apuracao/saida/notas"       element={<ConsultaNFeSaidas />} />
              <Route path="/apuracao/entrada/notas"     element={<ConsultaNFesEntradas />} />
              <Route path="/apuracao/cte-entrada/notas" element={<ConsultaCTesEntradas />} />
              <Route path="/apuracao/creditos-perdidos" element={<ApuracaoCredPerdidos />} />
              <Route path="/rfb/apuracao-ibs"           element={<PainelApuracaoIBS />} />
              <Route path="/rfb/apuracao-cbs"           element={<PainelApuracaoCBS />} />

              {/* Receita Federal */}
              <Route path="/rfb/gestao-creditos"        element={<GestaoCredIBSCBS />} />
              <Route path="/rfb/apuracao"               element={<RFBApuracao />} />
              <Route path="/rfb/debitos"                element={<RFBDebitos />} />
              <Route path="/rfb/creditos-cbs"           element={<ComingSoon title="Créditos CBS mês corrente" />} />
              <Route path="/rfb/pagamentos-cbs"         element={<ComingSoon title="Pagamentos CBS mês corrente" />} />
              <Route path="/rfb/pagamentos-fornecedores" element={<ComingSoon title="Pagamentos CBS a Fornecedores" />} />
              <Route path="/rfb/concluir-apuracao"      element={<ComingSoon title="Concluir apuração mês anterior" />} />
            </Routes>
          </div>
        </main>
      </div>
      <Toaster />
    </div>
  )
}

// ── App root ─────────────────────────────────────────────────────────────────
function App() {
  console.log('App Version: 1.0.2 — FB_APU02 Apuração Assistida')
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
