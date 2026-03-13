import {
  Table,
  Users,
  Building,
  FileText,
  Upload,
  Download,
  LogOut,
  Store,
  CreditCard,
  Wallet,
  Truck,
  CheckCircle,
  BarChart3,
  Tag,
  ShieldAlert,
  ChevronDown,
  Settings,
  FolderInput,
  Calculator,
  Landmark,
  KeyRound,
} from "lucide-react"
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarHeader,
  SidebarFooter,
} from "@/components/ui/sidebar"
import { Link, useLocation } from "react-router-dom"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog"
import { useAuth } from "@/contexts/AuthContext"
import { CompanySwitcher } from "@/components/CompanySwitcher"
import { FilialSelector } from "@/components/FilialSelector"
import { cn } from "@/lib/utils"
import { useState } from "react"
import { toast } from "sonner"

// ---------------------------------------------------------------------------
// Tipos
// ---------------------------------------------------------------------------
interface NavItem {
  title: string;
  url: string;
  icon: React.ElementType;
  disabled?: boolean;
  adminOnly?: boolean;
  danger?: boolean;
}

interface NavSection {
  id: string;
  title: string;
  sectionIcon: React.ElementType;
  adminOnly?: boolean;
  items: NavItem[];
}

// ---------------------------------------------------------------------------
// Definição das seções (estrutura flat — sem subgrupos aninhados)
// ---------------------------------------------------------------------------
const sections: NavSection[] = [
  {
    id: "config",
    title: "Configurações e Tabelas",
    sectionIcon: Settings,
    items: [
      { title: "Tabela de Alíquotas",    url: "/config/aliquotas",        icon: Table },
      { title: "Tabela CFOP",             url: "/config/cfop",              icon: Table },
      { title: "Simples Nacional",        url: "/config/forn-simples",      icon: Store },
      { title: "Apelidos de Filiais",     url: "/config/apelidos-filiais",  icon: Tag },
      { title: "Gestores de Relatórios",  url: "/config/gestores",          icon: Users },
      { title: "Gestão de Ambiente",      url: "/config/ambiente",          icon: Building },
      { title: "Credenciais API RFB",     url: "/rfb/credenciais",          icon: KeyRound, adminOnly: true },
      { title: "Gestão de Usuários",      url: "/config/usuarios",          icon: Users, adminOnly: true },
    ],
  },
  {
    id: "importar",
    title: "Importar XMLs",
    sectionIcon: FolderInput,
    adminOnly: true,
    items: [
      { title: "Entradas Mod. 55",    url: "/apuracao/entrada",     icon: Upload },
      { title: "Saídas Mod. 55/65",   url: "/apuracao/saida",       icon: Upload },
      { title: "CT-e — Entradas",     url: "/apuracao/cte-entrada", icon: Upload },
      { title: "Serviços — Entradas", url: "#",                     icon: Upload, disabled: true },
      { title: "Serviços — Saídas",   url: "#",                     icon: Upload, disabled: true },
    ],
  },
  {
    id: "apuracao",
    title: "Apuração IBS / CBS",
    sectionIcon: Calculator,
    adminOnly: true,
    items: [
      { title: "Entradas Mod. 55",    url: "/apuracao/entrada/notas",     icon: FileText },
      { title: "Saídas Mod. 55/65",   url: "/apuracao/saida/notas",       icon: FileText },
      { title: "CT-e — Entradas",     url: "/apuracao/cte-entrada/notas", icon: FileText },
      { title: "Serviços — Entradas", url: "#",                           icon: FileText, disabled: true },
      { title: "Serviços — Saídas",   url: "#",                           icon: FileText, disabled: true },
      { title: "Créditos em Risco",   url: "/apuracao/creditos-perdidos", icon: ShieldAlert, danger: true },
      { title: "Apuração IBS — mês",  url: "/rfb/apuracao-ibs",          icon: BarChart3 },
      { title: "Apuração CBS — mês",  url: "/rfb/apuracao-cbs",          icon: BarChart3 },
    ],
  },
  {
    id: "rfb",
    title: "Receita Federal",
    sectionIcon: Landmark,
    adminOnly: true,
    items: [
      { title: "Gestão IBS/CBS",              url: "/rfb/gestao-creditos",         icon: BarChart3 },
      { title: "Importar débitos CBS",        url: "/rfb/apuracao",                icon: Download },
      { title: "Débitos mês corrente",        url: "/rfb/debitos",                 icon: FileText },
      { title: "Créditos CBS — mês",          url: "/rfb/creditos-cbs",            icon: CreditCard,  disabled: true },
      { title: "Pagamentos CBS — mês",        url: "/rfb/pagamentos-cbs",          icon: Wallet,      disabled: true },
      { title: "Pgtos CBS a Fornecedores",    url: "/rfb/pagamentos-fornecedores", icon: Truck,       disabled: true },
      { title: "Concluir apuração mês ant.",  url: "/rfb/concluir-apuracao",       icon: CheckCircle, disabled: true },
    ],
  },
]

// ---------------------------------------------------------------------------
// AppSidebar
// ---------------------------------------------------------------------------
export function AppSidebar() {
  const location = useLocation()
  const { user, company, logout, token } = useAuth()
  const isAdmin = user?.role === "admin"

  // Estado de expansão de cada seção (todas abertas por padrão)
  const [openSections, setOpenSections] = useState<Record<string, boolean>>(
    () => Object.fromEntries(sections.map((s) => [s.id, false]))
  )

  // Estado do dialog de troca de senha
  const [pwDialog, setPwDialog] = useState(false)
  const [pwCurrent, setPwCurrent] = useState("")
  const [pwNew, setPwNew] = useState("")
  const [pwConfirm, setPwConfirm] = useState("")
  const [pwLoading, setPwLoading] = useState(false)

  async function handleChangePassword() {
    if (pwNew !== pwConfirm) {
      toast.error("A nova senha e a confirmação não coincidem")
      return
    }
    if (pwNew.length < 6) {
      toast.error("A nova senha deve ter no mínimo 6 caracteres")
      return
    }
    setPwLoading(true)
    try {
      const res = await fetch("/api/auth/change-password", {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
        body: JSON.stringify({ current_password: pwCurrent, new_password: pwNew }),
      })
      const data = await res.json()
      if (!res.ok) {
        toast.error(data.error || "Erro ao alterar senha")
        return
      }
      toast.success("Senha alterada com sucesso")
      setPwDialog(false)
      setPwCurrent(""); setPwNew(""); setPwConfirm("")
    } catch {
      toast.error("Erro de conexão")
    } finally {
      setPwLoading(false)
    }
  }

  function toggleSection(id: string) {
    setOpenSections((prev) => ({ ...prev, [id]: !prev[id] }))
  }

  function isActive(url: string) {
    if (url === "#") return false
    return location.pathname === url.split("?")[0]
  }

  return (
    <>
    <Sidebar collapsible="icon">
      {/* ── Header ── */}
      <SidebarHeader className="border-b pb-2">
        <div className="flex items-center gap-2.5 px-3 py-2">
          <img
            src="/favicon-fc.png"
            alt="Ferreira Costa"
            className="size-8 rounded-lg shrink-0 object-cover"
          />
          <div className="grid flex-1 text-left leading-tight">
            <span className="font-bold text-sm truncate">FBTax Cloud</span>
            <span className="text-[10px] text-muted-foreground truncate">
              Apuração Assistida
            </span>
          </div>
        </div>
      </SidebarHeader>

      {/* ── Conteúdo ── */}
      <SidebarContent>
        {sections.map((section) => {
          if (section.adminOnly && !isAdmin) return null
          const visibleItems = section.items.filter(
            (item) => !item.adminOnly || isAdmin
          )
          if (visibleItems.length === 0) return null

          const isOpen = openSections[section.id] ?? true

          return (
            <SidebarGroup key={section.id} className="pt-0 pb-0 pl-0">
              {/* Label da seção — colado à esquerda, itálico e negrito */}
              <SidebarGroupLabel
                className={cn(
                  "flex items-center gap-1.5 mt-2 mb-0.5 pl-1 pr-2 py-1",
                  "text-[10px] uppercase tracking-wider font-bold italic",
                  "text-sidebar-foreground cursor-pointer select-none",
                  "hover:text-sidebar-foreground/80 transition-colors",
                )}
                onClick={() => toggleSection(section.id)}
              >
                <section.sectionIcon className="h-3.5 w-3.5 shrink-0 opacity-70" />
                {section.title}
                <ChevronDown
                  className={cn(
                    "ml-auto h-3 w-3 shrink-0 transition-transform duration-200",
                    !isOpen && "-rotate-90"
                  )}
                />
              </SidebarGroupLabel>

              {isOpen && (
              <SidebarGroupContent>
                <SidebarMenu>
                  {visibleItems.map((item) => (
                    <SidebarMenuItem key={item.title}>
                      {item.disabled ? (
                        /* Item desabilitado (em desenvolvimento) */
                        <SidebarMenuButton
                          className="h-8 px-3 opacity-40 pointer-events-none"
                          tooltip={`${item.title} (em desenvolvimento)`}
                        >
                          <item.icon className="h-4 w-4 shrink-0" />
                          <span className="text-xs">{item.title}</span>
                          <span className="ml-auto text-[9px] bg-sidebar-accent text-sidebar-foreground/50 px-1 py-0.5 rounded font-normal">
                            dev
                          </span>
                        </SidebarMenuButton>
                      ) : (
                        /* Item ativo */
                        <SidebarMenuButton
                          asChild
                          isActive={isActive(item.url)}
                          tooltip={item.title}
                          className={cn(
                            "h-8 px-3",
                            item.danger && [
                              "text-red-600 hover:text-red-700 hover:bg-red-50",
                              "dark:hover:bg-red-950/20 font-semibold",
                            ],
                            isActive(item.url) && item.danger && [
                              "bg-red-50 dark:bg-red-950/20 text-red-700",
                            ],
                          )}
                        >
                          <Link to={item.url}>
                            <item.icon className="h-4 w-4 shrink-0" />
                            <span className="text-xs">{item.title}</span>
                          </Link>
                        </SidebarMenuButton>
                      )}
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
              )}
            </SidebarGroup>
          )
        })}
      </SidebarContent>

      {/* ── Footer ── */}
      <SidebarFooter className="border-t">
        {user && (
          <div className="p-2">
            <div className="flex flex-col gap-1 px-2 py-2 bg-sidebar-accent rounded-lg">
              <p className="text-[10px] italic truncate text-sidebar-foreground/60 leading-tight">
                {company || "Empresa não identificada"}
              </p>
              <p className="text-xs font-medium truncate leading-tight text-sidebar-foreground">{user.full_name}</p>
              <div className="flex items-center gap-1.5">
                <span className="bg-yellow-100 text-yellow-700 border border-yellow-200 px-1.5 py-0.5 rounded text-[9px] font-medium">
                  Vence: {new Date(user.trial_ends_at).toLocaleDateString("pt-BR")}
                </span>
                <button
                  onClick={() => setPwDialog(true)}
                  title="Trocar senha"
                  className="text-sidebar-foreground/50 hover:text-sidebar-foreground transition-colors"
                >
                  <KeyRound className="h-3 w-3" />
                </button>
              </div>
              <div className="mt-1 pt-1 border-t border-sidebar-border flex flex-col gap-0.5">
                <FilialSelector />
                <CompanySwitcher compact />
              </div>
              <Button
                variant="ghost"
                size="sm"
                className="w-full justify-start h-7 px-1 text-sidebar-foreground/60 hover:text-sidebar-foreground mt-0.5"
                onClick={logout}
              >
                <LogOut className="mr-2 h-3.5 w-3.5" />
                <span className="text-xs">Sair</span>
              </Button>
            </div>
          </div>
        )}
      </SidebarFooter>

    </Sidebar>

    <Dialog open={pwDialog} onOpenChange={(o) => { setPwDialog(o); if (!o) { setPwCurrent(""); setPwNew(""); setPwConfirm("") } }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Trocar Senha</DialogTitle>
        </DialogHeader>
        <div className="grid gap-4 py-2">
          <div className="grid gap-1.5">
            <Label htmlFor="pw-current">Senha atual</Label>
            <Input id="pw-current" type="password" value={pwCurrent} onChange={(e) => setPwCurrent(e.target.value)} />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="pw-new">Nova senha</Label>
            <Input id="pw-new" type="password" value={pwNew} onChange={(e) => setPwNew(e.target.value)} />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="pw-confirm">Confirmar nova senha</Label>
            <Input id="pw-confirm" type="password" value={pwConfirm} onChange={(e) => setPwConfirm(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && handleChangePassword()} />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setPwDialog(false)}>Cancelar</Button>
          <Button onClick={handleChangePassword} disabled={pwLoading || !pwCurrent || !pwNew || !pwConfirm}>
            {pwLoading ? "Salvando..." : "Salvar"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
    </>
  )
}
