import { useEffect, useState } from "react";
import { Switch } from "@/components/ui/switch";
import { useAuth } from "@/features/auth/hooks/useAuth";

interface AutoSummarySettingProps {
  disabled?: boolean;
}

// AutoSummarySetting turns on a summary for every finished transcription,
// using the template marked as default.
export function AutoSummarySetting({ disabled = false }: AutoSummarySettingProps) {
  const { getAuthHeaders } = useAuth();
  const [enabled, setEnabled] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [models, setModels] = useState<string[]>([]);
  const [defaultModel, setDefaultModel] = useState("");

  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/v1/summaries/settings', { headers: { ...getAuthHeaders() } });
        if (res.ok) {
          const data = await res.json();
          setEnabled(!!data.auto_summarize);
          setDefaultModel(data.default_model || "");
        }
      } finally {
        setLoaded(true);
      }
    })();
  }, [getAuthHeaders]);

  useEffect(() => {
    fetch('/api/v1/chat/models', { headers: { ...getAuthHeaders() } })
      .then(r => (r.ok ? r.json() : null))
      .then(d => d && setModels(d.models || []))
      .catch(() => { /* no provider yet */ });
  }, [getAuthHeaders]);

  const saveModel = async (m: string) => {
    const prev = defaultModel;
    setDefaultModel(m);
    setError(null);
    const res = await fetch('/api/v1/summaries/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...getAuthHeaders() },
      body: JSON.stringify({ default_model: m }),
    });
    if (!res.ok) {
      setDefaultModel(prev);
      setError('Could not save the model.');
    }
  };

  const toggle = async (next: boolean) => {
    setSaving(true);
    setError(null);
    setEnabled(next);
    try {
      const res = await fetch('/api/v1/summaries/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...getAuthHeaders() },
        body: JSON.stringify({ auto_summarize: next }),
      });
      if (!res.ok) throw new Error();
    } catch {
      setEnabled(!next);
      setError('Could not save the setting.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="mb-6 pb-6 border-b border-[var(--border-subtle)] space-y-4">
    <div className="flex items-start justify-between gap-4">
      <div>
        <label htmlFor="autoSummarize" className="text-sm font-medium text-[var(--text-primary)] cursor-pointer">
          Summarize automatically after transcription
        </label>
        <p className="text-xs text-[var(--text-secondary)] mt-1 max-w-xl">
          Runs the default template on every finished recording, then suggests a title, a one-line brief and tags.
          With a local model it waits for the GPU, so it never runs alongside a transcription.
        </p>
        {error && <p className="text-xs text-[var(--error)] mt-1">{error}</p>}
      </div>
      <Switch
        id="autoSummarize"
        checked={enabled}
        onCheckedChange={toggle}
        disabled={disabled || !loaded || saving}
      />
    </div>
    <label className="flex flex-wrap items-center gap-2 text-sm text-[var(--text-secondary)]">
      Model for templates without one
      <select
        className="h-9 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-main)] px-2 text-sm text-[var(--text-primary)]"
        value={defaultModel}
        onChange={e => saveModel(e.target.value)}
        disabled={disabled || !loaded}
        aria-label="Model for templates without one"
      >
        <option value="">Same as the default template</option>
        {models.map(m => <option key={m} value={m}>{m}</option>)}
      </select>
    </label>
    </div>
  );
}
