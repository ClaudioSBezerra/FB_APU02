import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Plus, Trash2, Building, Layers, Factory } from "lucide-react";
import { toast } from "sonner";
import { useAuth } from "@/contexts/AuthContext";

interface Environment {
  id: string;
  name: string;
  description: string;
  created_at: string;
}

interface EnterpriseGroup {
  id: string;
  environment_id: string;
  name: string;
  description: string;
  created_at: string;
}

interface Company {
  id: string;
  group_id: string;
  cnpj: string;
  name: string;
  trade_name: string;
  created_at: string;
}

interface Branch {
  cnpj: string;
  company_name: string;
}

interface UserHierarchy {
  environment: Environment;
  group: EnterpriseGroup;
  company: Company;
  branches: Branch[];
}

export default function GestaoAmbiente() {
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [selectedEnv, setSelectedEnv] = useState<Environment | null>(null);
  const [groups, setGroups] = useState<EnterpriseGroup[]>([]);
  const [selectedGroup, setSelectedGroup] = useState<EnterpriseGroup | null>(null);
  const [companies, setCompanies] = useState<Company[]>([]);
  
  // Modal states
  const [isEnvModalOpen, setIsEnvModalOpen] = useState(false);
  const [isGroupModalOpen, setIsGroupModalOpen] = useState(false);
  const [isCompanyModalOpen, setIsCompanyModalOpen] = useState(false);
  
  // Form states
  const [newEnvName, setNewEnvName] = useState("");
  const [newEnvDesc, setNewEnvDesc] = useState("");
  const [newGroupName, setNewGroupName] = useState("");
  const [newGroupDesc, setNewGroupDesc] = useState("");
  const [newCompanyCNPJ, setNewCompanyCNPJ] = useState("");
  const [newCompanyName, setNewCompanyName] = useState("");
  const [newCompanyTradeName, setNewCompanyTradeName] = useState("");

  const [loading, setLoading] = useState(false);
  const { user } = useAuth();
  const [userHierarchy, setUserHierarchy] = useState<UserHierarchy | null>(null);

  // Initial Load
  useEffect(() => {
    if (user?.role === 'admin') {
      fetchEnvironments();
    } else if (user) {
      fetchUserHierarchy();
    }
  }, [user]);

  // Load Groups when Env selected
  useEffect(() => {
    if (selectedEnv) {
      fetchGroups(selectedEnv.id);
      setGroups([]);
      setSelectedGroup(null);
      setCompanies([]);
    } else {
      setGroups([]);
      setSelectedGroup(null);
      setCompanies([]);
    }
  }, [selectedEnv]);

  // Load Companies when Group selected
  useEffect(() => {
    if (selectedGroup) {
      fetchCompanies(selectedGroup.id);
    } else {
      setCompanies([]);
    }
  }, [selectedGroup]);

  const fetchUserHierarchy = async () => {
    try {
      setLoading(true);
      const token = localStorage.getItem('token');
      const res = await fetch("/api/user/hierarchy", {
        headers: { 
            "Authorization": `Bearer ${token}` 
        }
      });
      if (!res.ok) throw new Error("Failed to fetch hierarchy");
      const data = await res.json();
      setUserHierarchy(data);
    } catch (error) {
      console.error(error);
      toast.error("Erro ao carregar dados do usuário");
    } finally {
      setLoading(false);
    }
  };

  const fetchEnvironments = async () => {
    try {
      setLoading(true);
      const token = localStorage.getItem('token');
      const res = await fetch("/api/config/environments", {
        headers: { "Authorization": `Bearer ${token}` }
      });
      if (!res.ok) throw new Error("Failed to fetch environments");
      const data = await res.json();
      setEnvironments(data);
      // Select first one by default if none selected and data exists
      if (!selectedEnv && data.length > 0) {
        setSelectedEnv(data[0]);
      }
    } catch (error) {
      console.error(error);
      toast.error("Erro ao carregar ambientes");
    } finally {
      setLoading(false);
    }
  };

  const fetchGroups = async (envId: string) => {
    try {
      const token = localStorage.getItem('token');
      const res = await fetch(`/api/config/groups?environment_id=${envId}`, {
        headers: { "Authorization": `Bearer ${token}` }
      });
      if (!res.ok) throw new Error("Failed to fetch groups");
      const data = await res.json();
      setGroups(data);
    } catch (error) {
      console.error(error);
      toast.error("Erro ao carregar grupos de empresas");
    }
  };

  const fetchCompanies = async (groupId: string) => {
    try {
      const token = localStorage.getItem('token');
      const res = await fetch(`/api/config/companies?group_id=${groupId}`, {
        headers: { "Authorization": `Bearer ${token}` }
      });
      if (!res.ok) throw new Error("Failed to fetch companies");
      const data = await res.json();
      setCompanies(data);
    } catch (error) {
      console.error(error);
      toast.error("Erro ao carregar empresas");
    }
  };

  const handleCreateEnvironment = async () => {
    if (!newEnvName) {
      toast.error("Nome do ambiente é obrigatório");
      return;
    }

    try {
      const token = localStorage.getItem('token');
      const res = await fetch("/api/config/environments", {
        method: "POST",
        headers: { 
          "Content-Type": "application/json",
          "Authorization": `Bearer ${token}`
        },
        body: JSON.stringify({ name: newEnvName, description: newEnvDesc }),
      });

      if (!res.ok) throw new Error("Failed to create");
      
      toast.success("Ambiente criado com sucesso!");
      setIsEnvModalOpen(false);
      setNewEnvName("");
      setNewEnvDesc("");
      fetchEnvironments();
    } catch (error) {
      toast.error("Erro ao criar ambiente");
    }
  };

  const handleCreateGroup = async () => {
    if (!selectedEnv) return;
    if (!newGroupName) {
      toast.error("Nome do grupo é obrigatório");
      return;
    }

    try {
      const token = localStorage.getItem('token');
      const res = await fetch("/api/config/groups", {
        method: "POST",
        headers: { 
          "Content-Type": "application/json",
          "Authorization": `Bearer ${token}`
        },
        body: JSON.stringify({
          environment_id: selectedEnv.id,
          name: newGroupName,
          description: newGroupDesc
        }),
      });

      if (!res.ok) throw new Error("Failed to create");
      
      toast.success("Grupo criado com sucesso!");
      setIsGroupModalOpen(false);
      setNewGroupName("");
      setNewGroupDesc("");
      fetchGroups(selectedEnv.id);
    } catch (error) {
      toast.error("Erro ao criar grupo");
    }
  };

  const handleCreateCompany = async () => {
    if (!selectedGroup) return;
    if (!newCompanyName) {
      toast.error("Razão Social é obrigatória");
      return;
    }

    try {
      const token = localStorage.getItem('token');
      const res = await fetch("/api/config/companies", {
        method: "POST",
        headers: { 
          "Content-Type": "application/json",
          "Authorization": `Bearer ${token}`
        },
        body: JSON.stringify({
          group_id: selectedGroup.id,
          cnpj: newCompanyCNPJ,
          name: newCompanyName,
          trade_name: newCompanyTradeName
        }),
      });

      if (!res.ok) throw new Error("Failed to create");
      
      toast.success("Empresa cadastrada com sucesso!");
      setIsCompanyModalOpen(false);
      setNewCompanyCNPJ("");
      setNewCompanyName("");
      setNewCompanyTradeName("");
      fetchCompanies(selectedGroup.id);
    } catch (error) {
      toast.error("Erro ao criar empresa");
    }
  };

  const handleDeleteEnvironment = async (id: string) => {
    if (!confirm("Tem certeza? Isso apagará TODOS os grupos e empresas vinculados.")) return;
    
    try {
      const token = localStorage.getItem('token');
      const res = await fetch(`/api/config/environments?id=${id}`, { 
        method: "DELETE",
        headers: { "Authorization": `Bearer ${token}` }
      });
      if (!res.ok) throw new Error("Failed to delete");
      toast.success("Ambiente removido");
      if (selectedEnv?.id === id) setSelectedEnv(null);
      fetchEnvironments();
    } catch (error) {
      toast.error("Erro ao remover ambiente");
    }
  };

  const handleDeleteGroup = async (id: string) => {
    if (!confirm("Tem certeza? Isso apagará TODAS as empresas vinculadas.")) return;
    
    try {
      const token = localStorage.getItem('token');
      const res = await fetch(`/api/config/groups?id=${id}`, { 
        method: "DELETE",
        headers: { "Authorization": `Bearer ${token}` }
      });
      if (!res.ok) throw new Error("Failed to delete");
      toast.success("Grupo removido");
      if (selectedGroup?.id === id) setSelectedGroup(null);
      if (selectedEnv) fetchGroups(selectedEnv.id);
    } catch (error) {
      toast.error("Erro ao remover grupo");
    }
  };

  const handleDeleteCompany = async (id: string) => {
    if (!confirm("Tem certeza?")) return;
    
    try {
      const token = localStorage.getItem('token');
      const res = await fetch(`/api/config/companies?id=${id}`, { 
        method: "DELETE",
        headers: { "Authorization": `Bearer ${token}` }
      });
      if (!res.ok) throw new Error("Failed to delete");
      toast.success("Empresa removida");
      if (selectedGroup) fetchCompanies(selectedGroup.id);
    } catch (error) {
      toast.error("Erro ao remover empresa");
    }
  };

  if (user?.role !== 'admin') {
    return (
      <div className="container mx-auto p-4 space-y-6">
        <div>
            <h1 className="text-3xl font-bold text-gray-900">Meu Ambiente</h1>
            <p className="text-gray-500 mt-1">
                Visualização dos dados vinculados ao seu usuário
            </p>
        </div>

        {loading ? (
             <p>Carregando...</p>
        ) : !userHierarchy ? (
             <p>Nenhum dado encontrado. Contate o administrador.</p>
        ) : (
            <>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                {/* Environment */}
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2">
                            <Layers className="h-5 w-5" />
                            Ambiente
                        </CardTitle>
                    </CardHeader>
                    <CardContent>
                         <div className="text-lg font-medium">{userHierarchy.environment.name}</div>
                         <div className="text-sm text-muted-foreground">{userHierarchy.environment.description}</div>
                    </CardContent>
                </Card>

                {/* Group */}
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2">
                            <Building className="h-5 w-5" />
                            Grupo
                        </CardTitle>
                    </CardHeader>
                    <CardContent>
                         <div className="text-lg font-medium">{userHierarchy.group.name}</div>
                         <div className="text-sm text-muted-foreground">{userHierarchy.group.description}</div>
                    </CardContent>
                </Card>

                {/* Company */}
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2">
                            <Factory className="h-5 w-5" />
                            Empresa
                        </CardTitle>
                    </CardHeader>
                    <CardContent>
                         <div className="text-lg font-medium">{userHierarchy.company.name}</div>
                         <p className="text-[10px] text-gray-400 font-mono truncate mb-1" title={userHierarchy.company.id}>ID: {userHierarchy.company.id}</p>
                         {userHierarchy.company.cnpj && <div className="text-sm text-muted-foreground">CNPJ: {userHierarchy.company.cnpj}</div>}
                    </CardContent>
                </Card>
            </div>

            <div className="space-y-4">
                <h2 className="text-xl font-semibold">Filiais Importadas</h2>
                <div className="border rounded-md p-4 bg-white">
                    {userHierarchy.branches.length === 0 ? (
                        <p className="text-muted-foreground">Nenhuma filial identificada nas importações.</p>
                    ) : (
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>CNPJ</TableHead>
                                    <TableHead>Razão Social (Importada)</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {userHierarchy.branches.map((branch, idx) => (
                                    <TableRow key={idx}>
                                        <TableCell className="font-mono">{branch.cnpj}</TableCell>
                                        <TableCell>{branch.company_name}</TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    )}
                </div>
            </div>
            </>
        )}
      </div>
    );
  }

  return (
    <div className="container mx-auto p-4 space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-gray-900">Gestão de Ambientes</h1>
        <p className="text-gray-500 mt-1">
          Configuração Hierárquica: Ambiente &gt; Grupo &gt; Empresa
        </p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6 h-[calc(100vh-12rem)]">
        {/* Column 1: Environments */}
        <div className="flex flex-col space-y-4 h-full">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold flex items-center gap-2">
              <Layers className="w-5 h-5" /> Ambientes
            </h2>
            <Dialog open={isEnvModalOpen} onOpenChange={setIsEnvModalOpen}>
              <DialogTrigger asChild>
                <Button size="sm" variant="outline"><Plus className="w-4 h-4" /></Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>Novo Ambiente</DialogTitle>
                  <DialogDescription>Crie um novo ambiente (Ex: Produção, Homologação).</DialogDescription>
                </DialogHeader>
                <div className="space-y-4 py-4">
                  <div className="space-y-2">
                    <Label>Nome</Label>
                    <Input value={newEnvName} onChange={(e) => setNewEnvName(e.target.value)} placeholder="Ex: Ambiente Produção" />
                  </div>
                  <div className="space-y-2">
                    <Label>Descrição</Label>
                    <Input value={newEnvDesc} onChange={(e) => setNewEnvDesc(e.target.value)} placeholder="Opcional" />
                  </div>
                </div>
                <DialogFooter>
                  <Button onClick={handleCreateEnvironment}>Criar</Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>
          
          <div className="flex-1 overflow-y-auto space-y-2 border rounded-md p-2 bg-gray-50/50">
            {loading && <p className="text-sm text-muted-foreground p-2">Carregando...</p>}
            {!loading && environments.length === 0 && (
              <p className="text-sm text-muted-foreground p-2">Nenhum ambiente.</p>
            )}
            {environments.map((env) => (
              <div
                key={env.id}
                className={`flex items-center justify-between p-3 rounded-md border cursor-pointer transition-all ${
                  selectedEnv?.id === env.id
                    ? "bg-white border-primary shadow-sm ring-1 ring-primary"
                    : "bg-white border-gray-200 hover:border-primary/50"
                }`}
                onClick={() => setSelectedEnv(env)}
              >
                <div className="overflow-hidden">
                  <p className="font-medium text-sm truncate">{env.name}</p>
                  {env.description && <p className="text-xs text-gray-500 truncate">{env.description}</p>}
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-6 w-6 text-gray-400 hover:text-red-500"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleDeleteEnvironment(env.id);
                  }}
                >
                  <Trash2 className="w-3 h-3" />
                </Button>
              </div>
            ))}
          </div>
        </div>

        {/* Column 2: Groups */}
        <div className="flex flex-col space-y-4 h-full">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold flex items-center gap-2">
              <Building className="w-5 h-5" /> Grupos
            </h2>
            <Dialog open={isGroupModalOpen} onOpenChange={setIsGroupModalOpen}>
              <DialogTrigger asChild>
                <Button size="sm" variant="outline" disabled={!selectedEnv}><Plus className="w-4 h-4" /></Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>Novo Grupo</DialogTitle>
                  <DialogDescription>Vinculado a: {selectedEnv?.name}</DialogDescription>
                </DialogHeader>
                <div className="space-y-4 py-4">
                  <div className="space-y-2">
                    <Label>Nome do Grupo</Label>
                    <Input value={newGroupName} onChange={(e) => setNewGroupName(e.target.value)} placeholder="Ex: Grupo Varejo X" />
                  </div>
                  <div className="space-y-2">
                    <Label>Descrição</Label>
                    <Input value={newGroupDesc} onChange={(e) => setNewGroupDesc(e.target.value)} placeholder="Opcional" />
                  </div>
                </div>
                <DialogFooter>
                  <Button onClick={handleCreateGroup}>Criar</Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>

          <div className="flex-1 overflow-y-auto space-y-2 border rounded-md p-2 bg-gray-50/50">
            {!selectedEnv ? (
              <div className="h-full flex items-center justify-center text-gray-400 text-sm">
                Selecione um ambiente
              </div>
            ) : groups.length === 0 ? (
               <div className="h-full flex items-center justify-center text-gray-400 text-sm">
                Nenhum grupo cadastrado
              </div>
            ) : (
              groups.map((group) => (
                <div
                  key={group.id}
                  className={`flex items-center justify-between p-3 rounded-md border cursor-pointer transition-all ${
                    selectedGroup?.id === group.id
                      ? "bg-white border-primary shadow-sm ring-1 ring-primary"
                      : "bg-white border-gray-200 hover:border-primary/50"
                  }`}
                  onClick={() => setSelectedGroup(group)}
                >
                  <div className="overflow-hidden">
                    <p className="font-medium text-sm truncate">{group.name}</p>
                    <p className="text-[10px] text-gray-400 font-mono truncate" title={group.id}>ID: {group.id}</p>
                    {group.description && <p className="text-xs text-gray-500 truncate">{group.description}</p>}
                  </div>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-6 w-6 text-gray-400 hover:text-red-500"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleDeleteGroup(group.id);
                    }}
                  >
                    <Trash2 className="w-3 h-3" />
                  </Button>
                </div>
              ))
            )}
          </div>
        </div>

        {/* Column 3: Companies */}
        <div className="flex flex-col space-y-4 h-full">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold flex items-center gap-2">
              <Factory className="w-5 h-5" /> Empresas
            </h2>
            <Dialog open={isCompanyModalOpen} onOpenChange={setIsCompanyModalOpen}>
              <DialogTrigger asChild>
                <Button size="sm" variant="outline" disabled={!selectedGroup}><Plus className="w-4 h-4" /></Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>Nova Empresa</DialogTitle>
                  <DialogDescription>Vinculada a: {selectedGroup?.name}</DialogDescription>
                </DialogHeader>
                <div className="space-y-4 py-4">
                  <div className="space-y-2">
                    <Label>CNPJ (apenas números)</Label>
                    <Input value={newCompanyCNPJ} onChange={(e) => setNewCompanyCNPJ(e.target.value)} placeholder="Opcional" maxLength={14} />
                  </div>
                  <div className="space-y-2">
                    <Label>Razão Social</Label>
                    <Input value={newCompanyName} onChange={(e) => setNewCompanyName(e.target.value)} placeholder="Empresa S/A" />
                  </div>
                  <div className="space-y-2">
                    <Label>Nome Fantasia</Label>
                    <Input value={newCompanyTradeName} onChange={(e) => setNewCompanyTradeName(e.target.value)} placeholder="Empresa X" />
                  </div>
                </div>
                <DialogFooter>
                  <Button onClick={handleCreateCompany}>Cadastrar</Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>

          <div className="flex-1 overflow-y-auto space-y-2 border rounded-md p-2 bg-gray-50/50">
             {!selectedGroup ? (
              <div className="h-full flex items-center justify-center text-gray-400 text-sm">
                Selecione um grupo
              </div>
            ) : companies.length === 0 ? (
               <div className="h-full flex items-center justify-center text-gray-400 text-sm">
                Nenhuma empresa cadastrada
              </div>
            ) : (
              companies.map((company) => (
                <div
                  key={company.id}
                  className="flex items-center justify-between p-3 rounded-md border bg-white border-gray-200 hover:border-primary/50 transition-all"
                >
                  <div className="overflow-hidden">
                    <p className="font-medium text-sm truncate">{company.name}</p>
                    <p className="text-[10px] text-gray-400 font-mono truncate" title={company.id}>ID: {company.id}</p>
                    {company.cnpj && <p className="text-xs text-gray-500 font-mono">{company.cnpj}</p>}
                    {company.trade_name && <p className="text-xs text-gray-400 truncate">{company.trade_name}</p>}
                  </div>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-6 w-6 text-gray-400 hover:text-red-500"
                    onClick={() => handleDeleteCompany(company.id)}
                  >
                    <Trash2 className="w-3 h-3" />
                  </Button>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
