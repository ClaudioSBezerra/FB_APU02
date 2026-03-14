import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { toast } from "sonner";
import { useNavigate, Link } from "react-router-dom";
import { useAuth } from "@/contexts/AuthContext";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { AlertCircle } from "lucide-react";

const FEATURES = [
  "Importação e análise de SPEDs EFD",
  "Simulador de impacto do IBS e CBS",
  "Integração direta com a Receita Federal",
  "Acompanhamento inteligente de riscos de créditos",
];

const Login = () => {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const navigate = useNavigate();
  const { login } = useAuth();

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsLoading(true);
    setErrorMsg(null);

    try {
      const res = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });

      const data = await res.json();

      if (!res.ok) {
        throw new Error(typeof data === 'string' ? data : "Credenciais inválidas");
      }

      login(data);
      toast.success("Login realizado com sucesso!");
      navigate("/rfb/gestao-creditos");
    } catch (error: any) {
      const msg = error.message || "Erro desconhecido";
      setErrorMsg(msg);
      toast.error(msg);
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex">
      {/* ── Painel esquerdo (visível apenas em lg+) ── */}
      <div
        className="hidden lg:flex lg:w-2/5 flex-col justify-between p-10 relative overflow-hidden"
        style={{ background: "#111827" }}
      >
        {/* Círculo decorativo grande — canto superior direito */}
        <div
          className="absolute -top-24 -right-24 w-96 h-96 rounded-full pointer-events-none"
          style={{ background: "radial-gradient(circle, #3b1f3a 0%, #1a0f1e 100%)" }}
        />
        {/* Círculo decorativo menor — canto inferior esquerdo */}
        <div
          className="absolute -bottom-20 -left-20 w-64 h-64 rounded-full pointer-events-none"
          style={{ background: "radial-gradient(circle, #2d1b2e 0%, #111827 100%)" }}
        />

        {/* ── Conteúdo principal ── */}
        <div className="relative z-10">
          {/* Logo Ferreira Costa — fundo vermelho da marca */}
          <div className="mb-12">
            <div
              className="inline-block rounded-2xl px-5 py-3"
              style={{ background: "#DA0812" }}
            >
              <img
                src="/logo-ferreira-costa.png"
                alt="Ferreira Costa Home Center"
                className="h-14 w-auto object-contain"
              />
            </div>
          </div>

          {/* Badge */}
          <span
            className="inline-block px-4 py-1.5 rounded-full text-sm uppercase tracking-widest font-semibold"
            style={{
              background: "rgba(255,255,255,0.08)",
              color: "#e5e7eb",
              border: "1px solid rgba(255,255,255,0.15)",
            }}
          >
            Gestão da Reforma Tributária
          </span>

          {/* Título */}
          <h1 className="text-white text-5xl font-bold leading-tight mt-5">
            Apuração Assistida
            <br />
            IBS/CBS
          </h1>

          {/* Subtítulo */}
          <p className="mt-5 text-base leading-relaxed" style={{ color: "#9ca3af" }}>
            Controle total sobre créditos, débitos e impactos da Reforma Tributária na sua empresa.
          </p>
        </div>

        {/* ── Rodapé do painel ── */}
        <div className="relative z-10 space-y-5">
          {/* Bullets de features */}
          <ul className="space-y-3">
            {FEATURES.map((feature) => (
              <li key={feature} className="flex items-center gap-3 text-sm" style={{ color: "#d1d5db" }}>
                <span
                  className="w-1.5 h-1.5 rounded-full shrink-0"
                  style={{ background: "#ef4444" }}
                />
                {feature}
              </li>
            ))}
          </ul>

          <p className="text-[10px]" style={{ color: "#DA0812" }}>v1.0.2</p>
        </div>
      </div>

      {/* ── Painel direito — formulário de login (inalterado) ── */}
      <div className="flex-1 flex items-center justify-center bg-gray-100 px-4">
        <div className="w-full max-w-[450px]">
          <Card className="w-full shadow-lg">
            <CardHeader className="flex flex-col items-center gap-1 space-y-0 pt-6 pb-4">
              <CardTitle className="text-base font-semibold">Acesse sua conta</CardTitle>
              <CardDescription className="text-xs">Entre com suas credenciais para continuar</CardDescription>
            </CardHeader>
            <CardContent>
              {errorMsg && (
                <Alert variant="destructive" className="mb-4">
                  <AlertCircle className="h-4 w-4" />
                  <AlertTitle>Erro</AlertTitle>
                  <AlertDescription>{errorMsg}</AlertDescription>
                </Alert>
              )}

              <form onSubmit={handleLogin} className="space-y-3">
              <div className="space-y-1.5">
                <Label htmlFor="email" className="text-sm">E-mail</Label>
                <Input
                  id="email"
                  type="email"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="seu@email.com"
                  className="text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="password" className="text-sm">Senha</Label>
                <Input
                  id="password"
                  type="password"
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="text-sm"
                />
              </div>
              <div className="flex justify-end">
                <Link to="/forgot-password" className="text-xs text-primary hover:underline">
                  Esqueci minha senha
                </Link>
              </div>
              <Button type="submit" className="w-full text-sm" disabled={isLoading}>
                {isLoading ? "Entrando..." : "Entrar"}
              </Button>
              <div className="text-center text-xs text-gray-500 mt-2">
                Não tem uma conta?{" "}
                <Link to="/register" className="text-primary hover:underline">
                  Crie grátis
                </Link>
              </div>
              </form>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
};

export default Login;
