import { useSearchParams, Link } from "react-router-dom";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { AlertCircle } from "lucide-react";

// Tela de erro do fluxo de login via Keycloak — usa os componentes shadcn já
// estabelecidos no projeto (Card/Button), em vez dos componentes Mantine do
// template original da skill.
export default function AuthError() {
  const [params] = useSearchParams();
  const message = params.get("message") || "Não foi possível concluir o login via Keycloak.";

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-100 px-4">
      <Card className="w-full max-w-md shadow-lg">
        <CardHeader className="flex flex-col items-center gap-2 text-center">
          <AlertCircle className="h-10 w-10 text-red-500" />
          <CardTitle className="text-base font-semibold">Erro no login via SSO</CardTitle>
          <CardDescription className="text-sm">{message}</CardDescription>
        </CardHeader>
        <CardContent className="flex justify-center">
          <Link to="/login?password">
            <Button variant="outline">Voltar para o login</Button>
          </Link>
        </CardContent>
      </Card>
    </div>
  );
}
