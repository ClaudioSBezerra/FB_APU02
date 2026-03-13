import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from "@/components/ui/sonner";
import TabelaAliquotas from './pages/TabelaAliquotas';
import TabelaCFOP from './pages/TabelaCFOP';
import TabelaFornSimples from './pages/TabelaFornSimples';
import ApelidosFiliais from './pages/ApelidosFiliais';
import GestaoAmbiente from './pages/GestaoAmbiente';
import Managers from './pages/Managers';
import RFBCredentials from './pages/RFBCredentials';
import RFBApuracao from './pages/RFBApuracao';
import RFBDebitos from './pages/RFBDebitos';
import GestaoCredIBSCBS from './pages/GestaoCredIBSCBS';
import PainelApuracaoIBS from './pages/PainelApuracaoIBS';
import PainelApuracaoCBS from './pages/PainelApuracaoCBS';
import ImportarXMLsSaida from './pages/ImportarXMLsSaida';
import ConsultaNFeSaidas from './pages/ConsultaNFeSaidas';
import ImportarXMLsEntrada from './pages/ImportarXMLsEntrada';
import ConsultaNFesEntradas from './pages/ConsultaNFesEntradas';
import ImportarXMLsCTe from './pages/ImportarXMLsCTe';
import ConsultaCTesEntradas from './pages/ConsultaCTesEntradas';
import ApuracaoCredPerdidos from './pages/ApuracaoCredPerdidos';
import AdminUsers from './pages/AdminUsers';
import Login from './pages/Login';
import Register from './pages/Register';
import ForgotPassword from './pages/ForgotPassword';
import ResetPassword from './pages/ResetPassword';
import { SidebarProvider, SidebarTrigger, SidebarInset } from '@/components/ui/sidebar';
import { AppSidebar } from '@/components/AppSidebar';
import { Separator } from '@/components/ui/separator';
import { AuthProvider, useAuth } from './contexts/AuthContext';
import { FilialProvider } from './contexts/FilialContext';

const queryClient = new QueryClient();

function ComingSoon({ title }: { title: string }) {
  return (
    <div className="flex flex-col items-center justify-center h-[50vh] space-y-4">
      <h1 className="text-2xl font-bold text-muted-foreground">{title}</h1>
      <p className="text-sm text-muted-foreground">Este módulo está em desenvolvimento.</p>
    </div>
  );
}

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, loading } = useAuth();
  const location = useLocation();

  if (loading) return null;

  if (!isAuthenticated) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  return <>{children}</>;
}

function AdminRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, loading, user } = useAuth();
  const location = useLocation();

  if (loading) return null;

  if (!isAuthenticated) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  if (user?.role !== 'admin') {
    return <Navigate to="/" replace />;
  }

  return <>{children}</>;
}

function AppLayout() {
  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <header className="flex h-16 shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger className="-ml-1" />
          <Separator orientation="vertical" className="mr-2 h-4" />
          <div className="flex items-center gap-2 text-sm font-medium">
            FBTax Cloud / Apuração Assistida
          </div>
        </header>
        <div className="flex-1 space-y-4 p-4 pt-6">
          <Routes>
            <Route path="/" element={<Navigate to="/apuracao/saida" replace />} />

            {/* Configurações */}
            <Route path="/config/aliquotas" element={<TabelaAliquotas />} />
            <Route path="/config/cfop" element={<TabelaCFOP />} />
            <Route path="/config/forn-simples" element={<TabelaFornSimples />} />
            <Route path="/config/apelidos-filiais" element={<ApelidosFiliais />} />
            <Route path="/config/gestores" element={<Managers />} />
            <Route path="/config/ambiente" element={
              <ProtectedRoute>
                <GestaoAmbiente />
              </ProtectedRoute>
            } />
            <Route path="/config/usuarios" element={
              <AdminRoute>
                <AdminUsers />
              </AdminRoute>
            } />
            <Route path="/rfb/credenciais" element={<RFBCredentials />} />

            {/* Apuração */}
            <Route path="/apuracao/entrada" element={<ImportarXMLsEntrada />} />
            <Route path="/apuracao/entrada/notas" element={<ConsultaNFesEntradas />} />
            <Route path="/apuracao/saida" element={<ImportarXMLsSaida />} />
            <Route path="/apuracao/saida/notas" element={<ConsultaNFeSaidas />} />
            <Route path="/apuracao/cte-entrada" element={<ImportarXMLsCTe />} />
            <Route path="/apuracao/cte-entrada/notas" element={<ConsultaCTesEntradas />} />
            <Route path="/apuracao/creditos-perdidos" element={<ApuracaoCredPerdidos />} />
            <Route path="/apuracao/nfse" element={<ComingSoon title="Importar XMLs NFS-e" />} />

            {/* RFB */}
            <Route path="/rfb/apuracao" element={<RFBApuracao />} />
            <Route path="/rfb/debitos" element={<RFBDebitos />} />
            <Route path="/rfb/gestao-creditos" element={<GestaoCredIBSCBS />} />
            <Route path="/rfb/apuracao-ibs" element={<PainelApuracaoIBS />} />
            <Route path="/rfb/apuracao-cbs" element={<PainelApuracaoCBS />} />
            <Route path="/rfb/creditos-cbs" element={<ComingSoon title="Créditos CBS mês corrente" />} />
            <Route path="/rfb/pagamentos-cbs" element={<ComingSoon title="Pagamentos CBS mês corrente" />} />
            <Route path="/rfb/pagamentos-fornecedores" element={<ComingSoon title="Pagamentos CBS a Fornecedores" />} />
            <Route path="/rfb/concluir-apuracao" element={<ComingSoon title="Concluir apuração mês anterior" />} />
          </Routes>
        </div>
        <Toaster />
      </SidebarInset>
    </SidebarProvider>
  );
}

function App() {
  console.log("App Version: 1.0.0 - FB_APU02 Apuração Assistida");
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/register" element={<Register />} />
            <Route path="/forgot-password" element={<ForgotPassword />} />
            <Route path="/reset-senha" element={<ResetPassword />} />
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
  );
}

export default App;
