import { useAuth } from "@/contexts/AuthContext";

export function Footer() {
  const { user, environment, group, company, isAuthenticated } = useAuth();

  if (!isAuthenticated || !user) {
    return null;
  }

  return (
    <footer className="border-t bg-muted/50 py-2 px-4 text-xs text-muted-foreground mt-auto">
      <div className="flex flex-col gap-1">
        <div className="font-semibold text-sm text-foreground">
          {company || "Empresa não identificada"}
        </div>
        <div className="flex items-center gap-2">
          <span>{user.full_name}</span>
          <span className="bg-yellow-100 text-yellow-700 px-2 py-0.5 rounded text-[10px] font-medium border border-yellow-200 ml-1">
             Vencimento: {new Date(user.trial_ends_at).toLocaleDateString()}
          </span>
        </div>
      </div>
    </footer>
  );
}
