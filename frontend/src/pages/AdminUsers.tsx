import { useState, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { toast } from "sonner";
import { Check, Trash2, UserCheck, Building2, ArrowRightLeft } from "lucide-react";
import { useAuth } from "@/contexts/AuthContext";

interface User {
  id: string;
  email: string;
  full_name: string;
  is_verified: boolean;
  trial_ends_at: string;
  role: string;
  created_at: string;
  environment_id: string | null;
  environment_name: string | null;
  group_id: string | null;
  group_name: string | null;
  company_id: string | null;
  company_name: string | null;
}

interface HierarchyItem {
  id: string;
  name: string;
}

function HierarchyCascadeSelects({
  token,
  envId,
  groupId,
  companyId,
  onEnvChange,
  onGroupChange,
  onCompanyChange,
}: {
  token: string;
  envId: string;
  groupId: string;
  companyId: string;
  onEnvChange: (id: string) => void;
  onGroupChange: (id: string) => void;
  onCompanyChange: (id: string) => void;
}) {
  const [environments, setEnvironments] = useState<HierarchyItem[]>([]);
  const [groups, setGroups] = useState<HierarchyItem[]>([]);
  const [companies, setCompanies] = useState<HierarchyItem[]>([]);

  useEffect(() => {
    fetch('/api/config/environments', { headers: { Authorization: `Bearer ${token}` } })
      .then(r => r.json())
      .then(data => setEnvironments(data || []))
      .catch(() => setEnvironments([]));
  }, [token]);

  useEffect(() => {
    if (!envId) { setGroups([]); return; }
    fetch(`/api/config/groups?environment_id=${envId}`, { headers: { Authorization: `Bearer ${token}` } })
      .then(r => r.json())
      .then(data => setGroups(data || []))
      .catch(() => setGroups([]));
  }, [envId, token]);

  useEffect(() => {
    if (!groupId) { setCompanies([]); return; }
    fetch(`/api/config/companies?group_id=${groupId}`, { headers: { Authorization: `Bearer ${token}` } })
      .then(r => r.json())
      .then(data => setCompanies(data || []))
      .catch(() => setCompanies([]));
  }, [groupId, token]);

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-4 items-center gap-4">
        <Label className="text-right">Ambiente</Label>
        <Select value={envId} onValueChange={(val) => { onEnvChange(val); onGroupChange(""); onCompanyChange(""); }}>
          <SelectTrigger className="col-span-3">
            <SelectValue placeholder="Selecione o ambiente..." />
          </SelectTrigger>
          <SelectContent>
            {environments.map(e => (
              <SelectItem key={e.id} value={e.id}>{e.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="grid grid-cols-4 items-center gap-4">
        <Label className="text-right">Grupo</Label>
        <Select value={groupId} onValueChange={(val) => { onGroupChange(val); onCompanyChange(""); }} disabled={!envId}>
          <SelectTrigger className="col-span-3">
            <SelectValue placeholder={envId ? "Selecione o grupo..." : "Selecione um ambiente primeiro"} />
          </SelectTrigger>
          <SelectContent>
            {groups.map(g => (
              <SelectItem key={g.id} value={g.id}>{g.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="grid grid-cols-4 items-center gap-4">
        <Label className="text-right">Empresa</Label>
        <Select value={companyId} onValueChange={onCompanyChange} disabled={!groupId}>
          <SelectTrigger className="col-span-3">
            <SelectValue placeholder={groupId ? "Selecione a empresa..." : "Selecione um grupo primeiro"} />
          </SelectTrigger>
          <SelectContent>
            {companies.map(c => (
              <SelectItem key={c.id} value={c.id}>{c.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </div>
  );
}

export default function AdminUsers() {
  const { token } = useAuth();
  const queryClient = useQueryClient();
  const [promoteDialogOpen, setPromoteDialogOpen] = useState(false);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [selectedUser, setSelectedUser] = useState<User | null>(null);

  // State for Promote/Edit
  const [newRole, setNewRole] = useState<string>("user");
  const [extendDays, setExtendDays] = useState<number>(0);
  const [isOfficial, setIsOfficial] = useState<boolean>(false);
  const [showReassign, setShowReassign] = useState(false);
  const [reassignEnvId, setReassignEnvId] = useState("");
  const [reassignGroupId, setReassignGroupId] = useState("");
  const [reassignCompanyId, setReassignCompanyId] = useState("");

  // State for Create
  const [newUser, setNewUser] = useState({ fullName: "", email: "", password: "", role: "user" });
  const [hierarchyMode, setHierarchyMode] = useState<"new" | "existing">("new");
  const [createEnvId, setCreateEnvId] = useState("");
  const [createGroupId, setCreateGroupId] = useState("");
  const [createCompanyId, setCreateCompanyId] = useState("");

  const { data: users, isLoading } = useQuery<User[]>({
    queryKey: ['admin-users'],
    queryFn: async () => {
      const response = await fetch(`/api/admin/users`, {
        headers: { Authorization: `Bearer ${token}` }
      });
      if (!response.ok) {
        const text = await response.text();
        try {
          const json = JSON.parse(text);
          throw new Error(json.message || `Erro: ${response.status} ${response.statusText}`);
        } catch {
          throw new Error(`Erro de Servidor (${response.status}): A API não respondeu corretamente.`);
        }
      }
      return response.json();
    },
    enabled: !!token
  });

  const createMutation = useMutation({
    mutationFn: async (data: typeof newUser) => {
      const body: Record<string, string> = {
        full_name: data.fullName,
        email: data.email,
        password: data.password,
        role: data.role,
      };
      if (hierarchyMode === "existing" && createEnvId) {
        body.environment_id = createEnvId;
        if (createGroupId) body.group_id = createGroupId;
        if (createCompanyId) body.company_id = createCompanyId;
      }
      const response = await fetch(`/api/admin/users/create`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`
        },
        body: JSON.stringify(body)
      });
      if (!response.ok) {
        const text = await response.text();
        try {
          const json = JSON.parse(text);
          throw new Error(json.message || 'Falha ao criar usuário');
        } catch {
          throw new Error(`Erro de Servidor (${response.status})`);
        }
      }
      return response.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin-users'] });
      toast.success("Usuário criado com sucesso");
      setCreateDialogOpen(false);
      setNewUser({ fullName: "", email: "", password: "", role: "user" });
      setHierarchyMode("new");
      setCreateEnvId("");
      setCreateGroupId("");
      setCreateCompanyId("");
    },
    onError: (error: Error) => toast.error(error.message || "Erro ao criar usuário")
  });

  const promoteMutation = useMutation({
    mutationFn: async (data: { userId: string, role: string, extendDays: number, isOfficial: boolean }) => {
      const response = await fetch(`/api/admin/users/promote?id=${data.userId}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`
        },
        body: JSON.stringify({ role: data.role, extend_days: data.extendDays, is_official: data.isOfficial })
      });
      if (!response.ok) throw new Error('Failed to update user');
      return response.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin-users'] });
      toast.success("Usuário atualizado com sucesso");
      setPromoteDialogOpen(false);
    },
    onError: () => toast.error("Erro ao atualizar usuário")
  });

  const reassignMutation = useMutation({
    mutationFn: async (data: { user_id: string, environment_id: string, group_id: string, company_id: string }) => {
      const response = await fetch(`/api/admin/users/reassign`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`
        },
        body: JSON.stringify(data)
      });
      if (!response.ok) throw new Error('Failed to reassign user');
      return response.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin-users'] });
      toast.success("Hierarquia alterada com sucesso");
      setShowReassign(false);
    },
    onError: () => toast.error("Erro ao alterar hierarquia")
  });

  const deleteMutation = useMutation({
    mutationFn: async (userId: string) => {
      const response = await fetch(`/api/admin/users/delete?id=${userId}`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` }
      });
      if (!response.ok) throw new Error('Failed to delete user');
      return response.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin-users'] });
      toast.success("Usuário removido com sucesso");
    },
    onError: () => toast.error("Erro ao remover usuário")
  });

  const handleCreate = () => {
    if (!newUser.fullName || !newUser.email || !newUser.password) {
      toast.error("Preencha todos os campos obrigatórios");
      return;
    }
    if (hierarchyMode === "existing" && !createEnvId) {
      toast.error("Selecione um ambiente para vincular");
      return;
    }
    createMutation.mutate(newUser);
  };

  const handleOpenPromote = (user: User) => {
    setSelectedUser(user);
    setNewRole(user.role);
    setExtendDays(0);
    setIsOfficial(false);
    setShowReassign(false);
    setReassignEnvId("");
    setReassignGroupId("");
    setReassignCompanyId("");
    setPromoteDialogOpen(true);
  };

  const handleSave = () => {
    if (!selectedUser) return;
    if (showReassign && reassignEnvId) {
      // Salva hierarquia e permissões juntos; fecha o dialog no onSuccess do promote
      reassignMutation.mutate({
        user_id: selectedUser.id,
        environment_id: reassignEnvId,
        group_id: reassignGroupId,
        company_id: reassignCompanyId,
      });
    }
    promoteMutation.mutate({
      userId: selectedUser.id,
      role: newRole,
      extendDays: extendDays,
      isOfficial: isOfficial
    });
  };

  const handleDelete = (userId: string) => {
    if (confirm("Tem certeza que deseja excluir este usuário? Esta ação não pode ser desfeita.")) {
      deleteMutation.mutate(userId);
    }
  };

  if (isLoading) return <div>Carregando usuários...</div>;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight">Gestão de Usuários</h2>
        <Button onClick={() => setCreateDialogOpen(true)}>
          <Check className="mr-2 h-4 w-4" /> Novo Usuário
        </Button>
      </div>

      <div className="rounded-md border overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow className="h-7">
              <TableHead className="text-[10px] uppercase tracking-wide py-1.5 px-2">Nome</TableHead>
              <TableHead className="text-[10px] uppercase tracking-wide py-1.5 px-2">Email</TableHead>
              <TableHead className="text-[10px] uppercase tracking-wide py-1.5 px-2">Status</TableHead>
              <TableHead className="text-[10px] uppercase tracking-wide py-1.5 px-2">Role</TableHead>
              <TableHead className="text-[10px] uppercase tracking-wide py-1.5 px-2">Ambiente</TableHead>
              <TableHead className="text-[10px] uppercase tracking-wide py-1.5 px-2">Grupo</TableHead>
              <TableHead className="text-[10px] uppercase tracking-wide py-1.5 px-2">Empresa</TableHead>
              <TableHead className="text-[10px] uppercase tracking-wide py-1.5 px-2">Trial</TableHead>
              <TableHead className="text-[10px] uppercase tracking-wide py-1.5 px-2">Criado</TableHead>
              <TableHead className="text-[10px] uppercase tracking-wide py-1.5 px-2 text-right">Ações</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {users?.map((user) => (
              <TableRow key={user.id} className="h-7">
                <TableCell className="text-xs font-medium py-1 px-2 whitespace-nowrap">{user.full_name}</TableCell>
                <TableCell className="text-xs py-1 px-2 whitespace-nowrap">{user.email}</TableCell>
                <TableCell className="py-1 px-2">
                  {user.is_verified ? (
                    <Badge variant="outline" className="text-[9px] px-1.5 py-0 h-4 bg-green-50 text-green-700 border-green-200">Verificado</Badge>
                  ) : (
                    <Badge variant="outline" className="text-[9px] px-1.5 py-0 h-4 bg-yellow-50 text-yellow-700 border-yellow-200">Pendente</Badge>
                  )}
                </TableCell>
                <TableCell className="py-1 px-2">
                  <Badge variant={user.role === 'admin' ? "default" : "secondary"} className="text-[9px] px-1.5 py-0 h-4">
                    {user.role}
                  </Badge>
                </TableCell>
                <TableCell className="text-xs text-muted-foreground py-1 px-2 whitespace-nowrap">
                  {user.environment_name || <span className="italic">—</span>}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground py-1 px-2 whitespace-nowrap">
                  {user.group_name || <span className="italic">—</span>}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground py-1 px-2 whitespace-nowrap">
                  {user.company_name || <span className="italic">—</span>}
                </TableCell>
                <TableCell className="text-xs py-1 px-2 whitespace-nowrap">
                  {new Date(user.trial_ends_at).toLocaleDateString('pt-BR')}
                  {new Date(user.trial_ends_at) < new Date() && (
                    <span className="ml-1 text-[10px] text-red-500 font-medium">Exp.</span>
                  )}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground py-1 px-2 whitespace-nowrap">
                  {new Date(user.created_at).toLocaleDateString('pt-BR')}
                </TableCell>
                <TableCell className="py-1 px-2 text-right">
                  <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => handleOpenPromote(user)} title="Editar usuário">
                    <UserCheck className="h-3.5 w-3.5" />
                  </Button>
                  <Button variant="ghost" size="icon" className="h-6 w-6 text-red-500 hover:text-red-600" onClick={() => handleDelete(user.id)} title="Excluir usuário">
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      {/* Create User Dialog */}
      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Novo Usuário</DialogTitle>
            <DialogDescription>
              Criar um novo usuário manualmente.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="newName" className="text-right">Nome</Label>
              <Input
                id="newName"
                value={newUser.fullName}
                onChange={(e) => setNewUser({...newUser, fullName: e.target.value})}
                className="col-span-3"
              />
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="newEmail" className="text-right">Email</Label>
              <Input
                id="newEmail"
                type="email"
                value={newUser.email}
                onChange={(e) => setNewUser({...newUser, email: e.target.value})}
                className="col-span-3"
              />
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="newPassword" className="text-right">Senha</Label>
              <Input
                id="newPassword"
                type="password"
                value={newUser.password}
                onChange={(e) => setNewUser({...newUser, password: e.target.value})}
                className="col-span-3"
              />
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="newRole" className="text-right">Role</Label>
              <Select value={newUser.role} onValueChange={(val) => setNewUser({...newUser, role: val})}>
                <SelectTrigger className="col-span-3">
                  <SelectValue placeholder="Selecione..." />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="user">User</SelectItem>
                  <SelectItem value="admin">Admin</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {/* Hierarchy Section */}
            <div className="border-t pt-4 mt-2">
              <Label className="text-sm font-semibold mb-3 block">Vincular a Hierarquia</Label>
              <RadioGroup value={hierarchyMode} onValueChange={(val) => setHierarchyMode(val as "new" | "existing")} className="flex gap-4 mb-4">
                <div className="flex items-center space-x-2">
                  <RadioGroupItem value="new" id="mode-new" />
                  <Label htmlFor="mode-new" className="cursor-pointer">Criar novo ambiente</Label>
                </div>
                <div className="flex items-center space-x-2">
                  <RadioGroupItem value="existing" id="mode-existing" />
                  <Label htmlFor="mode-existing" className="cursor-pointer">Vincular a existente</Label>
                </div>
              </RadioGroup>

              {hierarchyMode === "existing" && token && (
                <HierarchyCascadeSelects
                  token={token}
                  envId={createEnvId}
                  groupId={createGroupId}
                  companyId={createCompanyId}
                  onEnvChange={setCreateEnvId}
                  onGroupChange={setCreateGroupId}
                  onCompanyChange={setCreateCompanyId}
                />
              )}
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>Cancelar</Button>
            <Button onClick={handleCreate} disabled={createMutation.isPending}>
              {createMutation.isPending ? "Criando..." : "Criar Usuário"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit User Dialog */}
      <Dialog open={promoteDialogOpen} onOpenChange={setPromoteDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Editar Usuário</DialogTitle>
            <DialogDescription>
              Alterar permissões ou estender período de trial para {selectedUser?.full_name}.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="role" className="text-right">Role</Label>
              <Select value={newRole} onValueChange={setNewRole}>
                <SelectTrigger className="col-span-3">
                  <SelectValue placeholder="Selecione..." />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="user">User</SelectItem>
                  <SelectItem value="admin">Admin</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="extendDays" className="text-right">Estender (dias)</Label>
              <Input
                id="extendDays"
                type="number"
                value={extendDays}
                onChange={(e) => setExtendDays(Number(e.target.value))}
                className="col-span-3"
                disabled={isOfficial}
              />
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="isOfficial" className="text-right">Cliente Oficial</Label>
              <div className="col-span-3 flex items-center space-x-2">
                <Checkbox
                  id="isOfficial"
                  checked={isOfficial}
                  onCheckedChange={(checked) => setIsOfficial(checked as boolean)}
                />
                <label htmlFor="isOfficial" className="text-sm font-medium leading-none">
                  Definir como cliente permanente (Até 2099)
                </label>
              </div>
            </div>

            {/* Hierarchy Section */}
            <div className="border-t pt-4 mt-2">
              <div className="flex items-center justify-between mb-3">
                <Label className="text-sm font-semibold">Hierarquia Atual</Label>
                <Button variant="outline" size="sm" onClick={() => setShowReassign(!showReassign)}>
                  <ArrowRightLeft className="mr-2 h-3 w-3" />
                  {showReassign ? "Cancelar" : "Alterar Hierarquia"}
                </Button>
              </div>
              <div className="text-sm text-muted-foreground space-y-1 mb-3">
                <div><Building2 className="inline h-3 w-3 mr-1" /> Ambiente: <strong>{selectedUser?.environment_name || "—"}</strong></div>
                <div className="ml-4">Grupo: <strong>{selectedUser?.group_name || "—"}</strong></div>
                <div className="ml-8">Empresa: <strong>{selectedUser?.company_name || "—"}</strong></div>
              </div>

              {showReassign && token && (
                <div className="border rounded-md p-3 bg-muted/30">
                  <Label className="text-sm font-medium mb-3 block">Nova Hierarquia</Label>
                  <HierarchyCascadeSelects
                    token={token}
                    envId={reassignEnvId}
                    groupId={reassignGroupId}
                    companyId={reassignCompanyId}
                    onEnvChange={setReassignEnvId}
                    onGroupChange={setReassignGroupId}
                    onCompanyChange={setReassignCompanyId}
                  />
                </div>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPromoteDialogOpen(false)}>Cancelar</Button>
            <Button onClick={handleSave} disabled={promoteMutation.isPending || reassignMutation.isPending || (showReassign && !reassignEnvId)}>
              {(promoteMutation.isPending || reassignMutation.isPending) ? "Salvando..." : "Salvar Alterações"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
