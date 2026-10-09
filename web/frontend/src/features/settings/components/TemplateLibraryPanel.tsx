import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { useAuth } from "@/features/auth/hooks/useAuth";
import { ChevronDown, ChevronUp, Loader2 } from "lucide-react";

interface LibraryItem {
  name: string;
  description: string;
  auto_tags?: string[];
  replaces?: string[];
  status: "missing" | "installed" | "different";
  existing_name?: string;
}

const statusLabel: Record<LibraryItem["status"], string> = {
  missing: "Not installed",
  installed: "Up to date",
  different: "Differs",
};

// TemplateLibraryPanel installs or updates the recommended templates. Each
// runs automatically on a recording type or topic from the tag lists.
export function TemplateLibraryPanel({ disabled = false, onApplied }: { disabled?: boolean; onApplied?: () => void }) {
  const { getAuthHeaders } = useAuth();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [model, setModel] = useState("");

  const { data: items = [], refetch } = useQuery({
    queryKey: ["summaryLibrary"],
    queryFn: async () => {
      const res = await fetch("/api/v1/summaries/library", { headers: getAuthHeaders() });
      return res.ok ? (res.json() as Promise<LibraryItem[]>) : [];
    },
    enabled: open,
  });

  const { data: models = [] } = useQuery({
    queryKey: ["chatModels"],
    queryFn: async () => {
      const res = await fetch("/api/v1/chat/models", { headers: getAuthHeaders() });
      if (!res.ok) return [] as string[];
      const data = await res.json();
      return (data.models || []) as string[];
    },
    enabled: open,
  });
  const { data: installed = [] } = useQuery({
    queryKey: ["summaryTemplates"],
    queryFn: async () => {
      const res = await fetch("/api/v1/summaries", { headers: getAuthHeaders() });
      return res.ok ? (res.json() as Promise<{ model: string; is_default?: boolean }[]>) : [];
    },
    enabled: open,
  });

  // Preselect the default template's model, else the first available.
  useEffect(() => {
    if (model || models.length === 0) return;
    const fromDefault = installed.find(t => t.is_default)?.model || installed[0]?.model;
    setModel(fromDefault && models.includes(fromDefault) ? fromDefault : models[0]);
  }, [models, installed, model]);

  const pending = items.filter(i => i.status !== "installed").length;
  const creating = items.some(i => picked.has(i.name) && i.status === "missing");

  const toggle = (name: string, on: boolean) =>
    setPicked(p => {
      const n = new Set(p);
      if (on) n.add(name); else n.delete(name);
      return n;
    });

  const applyPicked = async () => {
    setBusy(true);
    setMessage(null);
    try {
      const res = await fetch("/api/v1/summaries/library", {
        method: "POST",
        headers: { "Content-Type": "application/json", ...getAuthHeaders() },
        body: JSON.stringify({ names: [...picked], model }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data?.error || "Could not apply");
      setMessage(`Created ${data.created}, updated ${data.updated}. Model, Reasoning and Default were kept on updated templates.`);
      setPicked(new Set());
      queryClient.invalidateQueries({ queryKey: ["summaryTemplates"] });
      await refetch();
      onApplied?.();
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Could not apply");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mb-6 pb-6 border-b border-[var(--border-subtle)] space-y-3">
      <button type="button" onClick={() => setOpen(o => !o)} className="flex items-center gap-1 text-sm font-medium text-[var(--text-primary)]" aria-expanded={open}>
        {open ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
        Recommended templates
        {open && pending > 0 && <span className="ml-2 text-xs font-normal text-[var(--text-tertiary)]">{pending} to install or update</span>}
      </button>
      {open && (
        <>
          <p className="text-xs text-[var(--text-secondary)] max-w-xl">
            Each template runs automatically on the recording type or topic shown. Updating keeps the template's
            existing summaries, model and Reasoning setting; older names are renamed.
          </p>
          <ul className="space-y-2 max-w-3xl">
            {items.map(it => (
              <li key={it.name} className="flex items-start gap-3 text-sm">
                <Checkbox
                  checked={picked.has(it.name)}
                  onCheckedChange={v => toggle(it.name, v === true)}
                  disabled={disabled || it.status === "installed"}
                  aria-label={it.name}
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium text-[var(--text-primary)]">{it.name}</span>
                    <span className="text-xs text-[var(--text-tertiary)]">{statusLabel[it.status]}</span>
                    {it.auto_tags && it.auto_tags.length > 0 && (
                      <span className="text-xs px-1.5 py-0.5 rounded border border-[var(--border-subtle)] text-[var(--text-secondary)]">Auto: {it.auto_tags.join(", ")}</span>
                    )}
                    {it.existing_name && it.existing_name !== it.name && (
                      <span className="text-xs text-[var(--text-tertiary)]">replaces {it.existing_name}</span>
                    )}
                  </div>
                  <p className="text-xs text-[var(--text-secondary)]">{it.description}</p>
                </div>
              </li>
            ))}
          </ul>
          <div className="flex flex-wrap items-center gap-3">
            <label className="flex items-center gap-2 text-sm text-[var(--text-secondary)]">
              Model for new templates
              <select
                className="h-9 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-main)] px-2 text-sm text-[var(--text-primary)]"
                value={model}
                onChange={e => setModel(e.target.value)}
                disabled={disabled || models.length === 0}
                aria-label="Model for new templates"
              >
                {models.length === 0 && <option value="">No models found</option>}
                {models.map(m => <option key={m} value={m}>{m}</option>)}
              </select>
            </label>
            <Button size="sm" variant="outline" disabled={disabled || busy || pending === 0}
              onClick={() => setPicked(new Set(items.filter(i => i.status !== "installed").map(i => i.name)))}>
              Select all
            </Button>
            <Button size="sm" disabled={disabled || busy || picked.size === 0 || (creating && !model)} onClick={applyPicked}>
              {busy && <Loader2 className="h-4 w-4 animate-spin" />} Install or update {picked.size || ""}
            </Button>
          </div>
          {message && <p className="text-xs text-[var(--text-secondary)]">{message}</p>}
        </>
      )}
    </div>
  );
}
