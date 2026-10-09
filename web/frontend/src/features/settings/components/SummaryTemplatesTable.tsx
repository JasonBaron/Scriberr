import { useEffect, useState, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, AlertDialogTrigger } from "@/components/ui/alert-dialog";
import { Switch } from "@/components/ui/switch";
import { Trash2, FileText, RotateCcw } from "lucide-react";
import type { SummaryTemplate } from "./SummaryTemplateDialog";
import { useAuth } from "@/features/auth/hooks/useAuth";

interface SummaryTemplatesTableProps {
  onEdit: (tpl: SummaryTemplate) => void;
  onChanged?: () => void;
  refreshTrigger?: number;
  disabled?: boolean;
}

export function SummaryTemplatesTable({ onEdit, onChanged, refreshTrigger = 0, disabled = false }: SummaryTemplatesTableProps) {
  const { getAuthHeaders } = useAuth();
  const [items, setItems] = useState<SummaryTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [openPop, setOpenPop] = useState<Record<string, boolean>>({});
  const [deleting, setDeleting] = useState<Set<string>>(new Set());

  const [models, setModels] = useState<string[]>([]);
  useEffect(() => {
    fetch('/api/v1/chat/models', { headers: { ...getAuthHeaders() } })
      .then(r => (r.ok ? r.json() : null))
      .then(d => d && setModels(d.models || []))
      .catch(() => { /* no provider yet */ });
  }, [getAuthHeaders]);

  const fetchItems = useCallback(async () => {
    try {
      setLoading(true);
      const res = await fetch('/api/v1/summaries', { headers: { ...getAuthHeaders() } });
      if (res.ok) {
        const data: SummaryTemplate[] = await res.json();
        setItems(data);
      }
    } finally {
      setLoading(false);
    }
  }, [getAuthHeaders]);

  useEffect(() => { fetchItems(); }, [fetchItems, refreshTrigger]);

  const handleDelete = async (id: string) => {
    setOpenPop(prev => ({ ...prev, [id]: false }));
    try {
      setDeleting(prev => new Set(prev).add(id));
      const res = await fetch(`/api/v1/summaries/${id}`, { method: 'DELETE', headers: { ...getAuthHeaders() } });
      if (res.ok) {
        setItems(prev => prev.filter(i => i.id !== id));
      } else {
        alert('Failed to delete');
      }
    } finally {
      setDeleting(prev => { const s = new Set(prev); s.delete(id); return s; });
    }
  };

  const replaceItem = (tpl: SummaryTemplate) => setItems(prev => prev.map(i => (i.id === tpl.id ? tpl : i)));

  const patch = async (tpl: SummaryTemplate, body: { model?: string; enabled?: boolean }) => {
    const res = await fetch(`/api/v1/summaries/${tpl.id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json', ...getAuthHeaders() },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    if (res.ok) {
      replaceItem(data);
      onChanged?.();
    } else {
      alert(data?.error || 'Could not update the template');
    }
  };
  const setEnabled = (tpl: SummaryTemplate, enabled: boolean) => patch(tpl, { enabled });
  const setModel = (tpl: SummaryTemplate, model: string) => patch(tpl, { model });

  const handleReset = async (tpl: SummaryTemplate) => {
    setOpenPop(prev => ({ ...prev, [tpl.id!]: false }));
    const res = await fetch(`/api/v1/summaries/${tpl.id}/reset`, { method: 'POST', headers: { ...getAuthHeaders() } });
    const data = await res.json();
    if (res.ok) replaceItem(data); else alert(data?.error || 'Could not reset the template');
  };

  if (loading) {
    return (
      <div className="space-y-2">
        {[...Array(3)].map((_, i) => (
          <div key={i} className="bg-carbon-100 dark:bg-carbon-800 rounded-lg p-4 animate-pulse h-16" />
        ))}
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className={`text-center py-16 ${disabled ? 'opacity-60 pointer-events-none' : ''}`}>
        <div className="bg-[var(--bg-main)] rounded-full w-16 h-16 mx-auto mb-4 flex items-center justify-center border border-[var(--border-subtle)]">
          <FileText className="h-8 w-8 text-[var(--text-tertiary)]" />
        </div>
        <h3 className="text-lg font-medium text-[var(--text-primary)] mb-2">No summary templates</h3>
        <p className="text-[var(--text-secondary)] mb-6 max-w-sm mx-auto">Built-in templates appear here after the server starts. You can also create your own.</p>
      </div>
    );
  }

  return (
    <div className={`space-y-2 ${disabled ? 'opacity-60 pointer-events-none' : ''}`}>
      {items.map(tpl => (
        <div key={tpl.id} className={`group bg-[var(--bg-card)] border border-[var(--border-subtle)] rounded-lg p-4 hover:border-[var(--brand-solid)] transition-all duration-200 cursor-pointer shadow-sm ${tpl.enabled === false ? 'opacity-60' : ''}`} onClick={() => !disabled && onEdit(tpl)}>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3 flex-1 min-w-0">
              <div className="bg-[var(--bg-main)] rounded-md p-1.5 text-[var(--text-tertiary)]">
                <FileText className="h-3.5 w-3.5" />
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-3">
                  <h3 className="text-sm font-medium text-[var(--text-primary)] truncate">{tpl.name}</h3>
                  {tpl.is_default && (
                    <span className="text-[10px] font-medium uppercase tracking-wide px-1.5 py-0.5 rounded bg-[var(--brand-solid)]/10 text-[var(--brand-solid)]">Default</span>
                  )}
                  {!tpl.is_default && (tpl.auto_tags?.length ?? 0) > 0 && (
                    <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-[var(--bg-main)] text-[var(--text-tertiary)] border border-[var(--border-subtle)]">Auto: {tpl.auto_tags!.join(", ")}</span>
                  )}
                  {tpl.builtin_key && (
                    <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-[var(--bg-main)] text-[var(--text-tertiary)] border border-[var(--border-subtle)]">{tpl.customized ? 'Built-in, edited' : 'Built-in'}</span>
                  )}
                  {tpl.reasoning && (
                    <span className="text-[10px] font-medium uppercase tracking-wide px-1.5 py-0.5 rounded bg-[var(--bg-main)] text-[var(--text-tertiary)] border border-[var(--border-subtle)]">Reasoning</span>
                  )}
                </div>
                {tpl.description && (
                  <p className="text-xs text-[var(--text-secondary)] truncate mt-1">{tpl.description}</p>
                )}
              </div>
            </div>
            <div className="flex items-center gap-2 shrink-0" onClick={(e) => e.stopPropagation()}>
              <select
                className="h-8 max-w-[11rem] rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-main)] px-2 text-xs text-[var(--text-primary)]"
                value={tpl.model || ''}
                onChange={(e) => setModel(tpl, e.target.value)}
                disabled={disabled}
                aria-label={`Model for ${tpl.name}`}
                title="Model"
              >
                {!tpl.model && <option value="">No model</option>}
                {tpl.model && !models.includes(tpl.model) && <option value={tpl.model}>{tpl.model}</option>}
                {models.map(m => <option key={m} value={m}>{m}</option>)}
              </select>
              <Switch
                checked={tpl.enabled !== false}
                onCheckedChange={(v) => setEnabled(tpl, v)}
                disabled={disabled || (tpl.is_default && tpl.enabled !== false)}
                aria-label={tpl.enabled === false ? `Enable ${tpl.name}` : `Disable ${tpl.name}`}
                title={tpl.is_default ? 'The default template is always on' : tpl.enabled === false ? 'Off: never runs automatically' : 'On'}
              />
              {!disabled && (tpl.builtin_key ? tpl.customized : true) && (
                <Popover open={openPop[tpl.id!] || false} onOpenChange={(open) => setOpenPop(prev => ({ ...prev, [tpl.id!]: open }))}>
                  <PopoverTrigger asChild>
                    <Button variant="ghost" size="sm" className="h-7 w-7 p-0 hover:bg-carbon-300 dark:hover:bg-carbon-600">
                      ⋮
                    </Button>
                  </PopoverTrigger>
                  <PopoverContent className="w-40 bg-[var(--bg-card)] border-[var(--border-subtle)] p-1 text-[var(--text-primary)]">
                    {tpl.builtin_key ? (
                    <AlertDialog>
                      <AlertDialogTrigger asChild>
                        <Button variant="ghost" size="sm" className="w-full justify-start h-7 text-xs">
                          <RotateCcw className="mr-2 h-3 w-3" /> Reset to default
                        </Button>
                      </AlertDialogTrigger>
                      <AlertDialogContent className="bg-[var(--bg-card)] border-[var(--border-subtle)]">
                        <AlertDialogHeader>
                          <AlertDialogTitle className="text-[var(--text-primary)]">Reset Template</AlertDialogTitle>
                          <AlertDialogDescription className="text-[var(--text-secondary)]">Restore the shipped name, prompt, description and auto tags for "{tpl.name}"? Your model, Reasoning and on/off settings are kept.</AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                          <AlertDialogCancel className="bg-[var(--bg-secondary)] border-[var(--border-subtle)] text-[var(--text-primary)] hover:bg-[var(--bg-main)]">Cancel</AlertDialogCancel>
                          <AlertDialogAction onClick={() => handleReset(tpl)}>Reset</AlertDialogAction>
                        </AlertDialogFooter>
                      </AlertDialogContent>
                    </AlertDialog>
                    ) : (
                    <AlertDialog>
                      <AlertDialogTrigger asChild>
                        <Button variant="ghost" size="sm" className="w-full justify-start h-7 text-xs hover:bg-[var(--error)]/10 text-[var(--error)] hover:text-[var(--error)]" disabled={deleting.has(tpl.id!)}>
                          <Trash2 className="mr-2 h-3 w-3" /> Delete
                        </Button>
                      </AlertDialogTrigger>
                      <AlertDialogContent className="bg-[var(--bg-card)] border-[var(--border-subtle)]">
                        <AlertDialogHeader>
                          <AlertDialogTitle className="text-[var(--text-primary)]">Delete Template</AlertDialogTitle>
                          <AlertDialogDescription className="text-[var(--text-secondary)]">Are you sure you want to delete "{tpl.name}"?</AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                          <AlertDialogCancel className="bg-[var(--bg-secondary)] border-[var(--border-subtle)] text-[var(--text-primary)] hover:bg-[var(--bg-main)]">Cancel</AlertDialogCancel>
                          <AlertDialogAction className="bg-[var(--error)] text-white hover:bg-[var(--error)]/90" onClick={() => handleDelete(tpl.id!)}>Delete</AlertDialogAction>
                        </AlertDialogFooter>
                      </AlertDialogContent>
                    </AlertDialog>
                    )}
                  </PopoverContent>
                </Popover>
              )}
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}
