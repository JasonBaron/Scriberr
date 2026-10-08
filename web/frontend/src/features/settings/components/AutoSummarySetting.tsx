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

  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/v1/summaries/settings', { headers: { ...getAuthHeaders() } });
        if (res.ok) {
          const data = await res.json();
          setEnabled(!!data.auto_summarize);
        }
      } finally {
        setLoaded(true);
      }
    })();
  }, [getAuthHeaders]);

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
    <div className="flex items-start justify-between gap-4 mb-6 pb-6 border-b border-[var(--border-subtle)]">
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
  );
}
