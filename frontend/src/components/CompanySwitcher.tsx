import { useEffect, useState } from "react";
import { Check, ChevronsUpDown, Building2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useAuth } from "@/contexts/AuthContext";

interface Company {
  id: string;
  name: string;
  cnpj: string;
  is_default: boolean;
}

export function CompanySwitcher({ compact = false }: { compact?: boolean }) {
  const { token, companyId, switchCompany } = useAuth();
  const [open, setOpen] = useState(false);
  const [companies, setCompanies] = useState<Company[]>([]);
  const [selectedCompany, setSelectedCompany] = useState<Company | null>(null);

  useEffect(() => {
    if (token) {
      fetch(`/api/user/companies`, {
        headers: { Authorization: `Bearer ${token}` },
      })
        .then((res) => res.json())
        .then((data) => {
          if (Array.isArray(data)) {
            setCompanies(data);
            const current = data.find((c) => c.id === companyId);
            if (current) setSelectedCompany(current);
            else if (data.length > 0 && !companyId) {
                // If no company selected but list available, select first? 
                // Better not auto-switch to avoid loops, just show placeholder or first in UI without triggering switch
                setSelectedCompany(data[0]);
            }
          }
        })
        .catch((err) => console.error("Failed to fetch companies", err));
    }
  }, [token, companyId]);

  if (companies.length <= 1) return null;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        {compact ? (
          <Button
            variant="ghost"
            size="sm"
            role="combobox"
            aria-expanded={open}
            className="w-full justify-start h-7 px-1 gap-1.5 text-xs text-muted-foreground hover:text-foreground"
          >
            <Building2 className="h-3 w-3 shrink-0" />
            <span>Trocar Empresa</span>
            <ChevronsUpDown className="ml-auto h-3 w-3 shrink-0 opacity-40" />
          </Button>
        ) : (
          <Button
            variant="outline"
            role="combobox"
            aria-expanded={open}
            className="w-full justify-between"
          >
            {selectedCompany ? (
              <div className="flex items-center gap-2 truncate">
                <Building2 className="h-4 w-4 shrink-0 opacity-50" />
                <span className="truncate">{selectedCompany.name}</span>
              </div>
            ) : (
              "Selecione a empresa..."
            )}
            <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </Button>
        )}
      </PopoverTrigger>
      <PopoverContent className="w-[200px] p-0">
        <Command>
          <CommandInput placeholder="Buscar empresa..." />
          <CommandList>
            <CommandEmpty>Nenhuma empresa encontrada.</CommandEmpty>
            <CommandGroup>
              {companies.map((company) => (
                <CommandItem
                  key={company.id}
                  value={company.name}
                  onSelect={() => {
                    setSelectedCompany(company);
                    switchCompany(company.id, company.name, company.cnpj);
                    setOpen(false);
                  }}
                >
                  <Check
                    className={cn(
                      "mr-2 h-4 w-4",
                      companyId === company.id ? "opacity-100" : "opacity-0"
                    )}
                  />
                  {company.name}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
