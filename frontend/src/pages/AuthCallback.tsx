import { useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "@/contexts/AuthContext";
import { handleCallback, CallbackError } from "@/lib/keycloak/handleCallback";

// Tela intermediária do fluxo OIDC/PKCE — troca o code do Keycloak por uma sessão
// própria do FB_APU02 e navega pra dentro do app. Sem Mantine (diferente do template
// original da skill): usa o mesmo spinner simples já usado em outras telas do projeto.
export default function AuthCallback() {
  const navigate = useNavigate();
  const { login } = useAuth();
  const handled = useRef(false);

  useEffect(() => {
    if (handled.current) return;
    handled.current = true;

    const run = async () => {
      try {
        const data = await handleCallback(new URLSearchParams(window.location.search));
        login(data);
        navigate("/rfb/gestao-creditos", { replace: true });
      } catch (err) {
        const message = err instanceof CallbackError ? err.message : "Erro ao concluir login via Keycloak.";
        navigate(`/auth/error?message=${encodeURIComponent(message)}`, { replace: true });
      }
    };
    run();
  }, [login, navigate]);

  return (
    <div className="min-h-screen flex flex-col items-center justify-center gap-3 bg-gray-100">
      <div className="animate-spin rounded-full h-10 w-10 border-b-2 border-primary" />
      <p className="text-sm text-muted-foreground">Concluindo login via Keycloak...</p>
    </div>
  );
}
