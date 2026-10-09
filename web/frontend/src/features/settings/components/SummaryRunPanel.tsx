import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { useAuth } from "@/features/auth/hooks/useAuth";
import { Loader2, Play, Square } from "lucide-react";

interface RunStatus {
  running: boolean;
  queued: number;
  done: number;
  skipped: number;
  failed: number;
  current?: { job_id: string; template_id: string; reason: string };
  started_at?: string;
  recent: { job_id: string; template: string; status: string; error?: string; finished_at: string }[];
}

interface TemplateOption {
  id: string;
  name: string;
  is_default?: boolean;
}

interface TagCount {
  tag: string;
  count: number;
}

type Scope = "missing" | "tag" | "all";
type Mode = "summarize" | "retag";

const selectClass =
  "h-9 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-main)] px-2 text-sm text-[var(--text-primary)] outline-none focus:ring-2 focus:ring-[var(--brand-solid)]/20";

// SummaryRunPanel summarizes existing recordings in the background:
// recordings without a summary, with a tag, or all of them.
export function SummaryRunPanel({ disabled = false }: { disabled?: boolean }) {
  const { getAuthHeaders } = useAuth();
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<Mode>("summarize");
  const [includeEdited, setIncludeEdited] = useState(false);
  const [scope, setScope] = useState<Scope>("missing");
  const [tag, setTag] = useState("");
  const [templateId, setTemplateId] = useState("");
  const [tagTemplates, setTagTemplates] = useState(true);
  const [force, setForce] = useState(false);
  const [preview, setPreview] = useState<number | null>(null);
  const [skippedEdited, setSkippedEdited] = useState(0);
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const { data: templates = [] } = useQuery({
    queryKey: ["summaryTemplates"],
    queryFn: async () => {
      const res = await fetch("/api/v1/summaries", { headers: getAuthHeaders() });
      return res.ok ? (res.json() as Promise<TemplateOption[]>) : [];
    },
  });
  const { data: tags = [] } = useQuery({
    queryKey: ["tags"],
    queryFn: async () => {
      const res = await fetch("/api/v1/tags", { headers: getAuthHeaders() });
      return res.ok ? (res.json() as Promise<TagCount[]>) : [];
    },
  });
  const { data: status } = useQuery({
    queryKey: ["summaryRun"],
    queryFn: async () => {
      const res = await fetch("/api/v1/summaries/run", { headers: getAuthHeaders() });
      if (!res.ok) throw new Error("status");
      return res.json() as Promise<RunStatus>;
    },
    refetchInterval: (q) => (q.state.data?.running ? 3000 : 15000),
  });

  const defaultTemplate = templates.find(t => t.is_default);
  const chosen = templateId && mode !== "retag" ? templates.find(t => t.id === templateId) : defaultTemplate;
  const usesDefault = !!chosen?.is_default;

  const retag = mode === "retag";
  const effScope: Scope = retag && scope === "missing" ? "all" : scope;
  const body = (dryRun: boolean) => ({
    retag,
    include_edited: retag && includeEdited,
    missing_only: effScope === "missing",
    all: effScope === "all",
    tag: effScope === "tag" ? tag : "",
    template_id: templateId,
    include_tag_templates: usesDefault && tagTemplates,
    force,
    dry_run: dryRun,
  });

  // Preview how many recordings match whenever the selection changes
  useEffect(() => {
    setPreview(null);
    setSkippedEdited(0);
    if (disabled || (effScope === "tag" && !tag) || !chosen) return;
    const ctrl = new AbortController();
    fetch("/api/v1/summaries/run", {
      method: "POST",
      headers: { "Content-Type": "application/json", ...getAuthHeaders() },
      body: JSON.stringify(body(true)),
      signal: ctrl.signal,
    })
      .then(r => (r.ok ? r.json() : null))
      .then(d => { if (d) { setPreview(d.matched); setSkippedEdited(d.skipped_edited || 0); } })
      .catch(() => { /* aborted or offline */ });
    return () => ctrl.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scope, tag, templateId, disabled, chosen?.id, mode, includeEdited]);

  const start = async () => {
    setBusy(true);
    setMessage(null);
    try {
      const res = await fetch("/api/v1/summaries/run", {
        method: "POST",
        headers: { "Content-Type": "application/json", ...getAuthHeaders() },
        body: JSON.stringify(body(false)),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data?.error || "Could not start");
      const what = retag ? "for retagging" : `with ${data.template}`;
      setMessage(data.queued ? `Queued ${data.queued} recording${data.queued === 1 ? "" : "s"} ${what}.` : "Nothing to do: no recordings matched.");
      queryClient.invalidateQueries({ queryKey: ["summaryRun"] });
      queryClient.invalidateQueries({ queryKey: ["audioFiles"] });
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Could not start");
    } finally {
      setBusy(false);
    }
  };

  const cancel = async () => {
    await fetch("/api/v1/summaries/run", { method: "DELETE", headers: getAuthHeaders() });
    queryClient.invalidateQueries({ queryKey: ["summaryRun"] });
    queryClient.invalidateQueries({ queryKey: ["audioFiles"] });
  };

  const total = status ? status.done + status.skipped + status.failed + status.queued + (status.current ? 1 : 0) : 0;
  const finished = status ? status.done + status.skipped + status.failed : 0;
  const pct = total ? Math.round((finished / total) * 100) : 0;

  return (
    <div className="mb-6 pb-6 border-b border-[var(--border-subtle)] space-y-3">
      <div>
        <h4 className="text-sm font-medium text-[var(--text-primary)]">Summarize existing recordings</h4>
        <p className="text-xs text-[var(--text-secondary)] mt-1 max-w-xl">
          Runs in the background, one recording at a time. With a local model it shares the GPU with transcription.
          The default template also refreshes the title, brief and tags. Retag only reruns that last step on the
          existing summary, which takes a few seconds per recording.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <select className={selectClass} value={mode} onChange={e => setMode(e.target.value as Mode)} disabled={disabled} aria-label="Action">
          <option value="summarize">Write summaries</option>
          <option value="retag">Retag only</option>
        </select>
        <select className={selectClass} value={effScope} onChange={e => setScope(e.target.value as Scope)} disabled={disabled} aria-label="Recordings">
          {!retag && <option value="missing">Recordings without a summary</option>}
          <option value="tag">Recordings with a tag</option>
          <option value="all">All recordings</option>
        </select>
        {effScope === "tag" && (
          <select className={selectClass} value={tag} onChange={e => setTag(e.target.value)} disabled={disabled} aria-label="Tag">
            <option value="">Choose a tag</option>
            {tags.map(t => <option key={t.tag} value={t.tag}>{t.tag} ({t.count})</option>)}
          </select>
        )}
        {!retag && (
          <select className={selectClass} value={templateId} onChange={e => setTemplateId(e.target.value)} disabled={disabled} aria-label="Template">
            <option value="">{defaultTemplate ? `Default: ${defaultTemplate.name}` : "Default (none set)"}</option>
            {templates.filter(t => !t.is_default).map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
          </select>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm text-[var(--text-secondary)]">
        {usesDefault && (
          <label className="flex items-center gap-2">
            <Switch checked={tagTemplates} onCheckedChange={setTagTemplates} disabled={disabled} />
            Also run tag-linked templates
          </label>
        )}
        {retag ? (
          <label className="flex items-center gap-2">
            <Switch checked={includeEdited} onCheckedChange={setIncludeEdited} disabled={disabled} />
            Include tags I edited by hand
          </label>
        ) : (
          <label className="flex items-center gap-2">
            <Switch checked={force} onCheckedChange={setForce} disabled={disabled} />
            Re-run where this template already has a summary
          </label>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Button size="sm" onClick={start} disabled={disabled || busy || !chosen || (effScope === "tag" && !tag) || preview === 0}>
          {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />} Start
        </Button>
        {status?.running && (
          <Button size="sm" variant="outline" onClick={cancel}>
            <Square className="h-4 w-4" /> Cancel queued
          </Button>
        )}
        <span className="text-xs text-[var(--text-tertiary)]">
          {!chosen ? "Set a default template first." : preview === null ? "" : `${preview} recording${preview === 1 ? "" : "s"} match.`}
          {retag && skippedEdited > 0 && ` ${skippedEdited} with hand-edited tags skipped; turn on "Include tags I edited by hand" to include them.`}
        </span>
      </div>
      {message && <p className="text-xs text-[var(--text-secondary)]">{message}</p>}

      {status && (status.running || finished > 0) && (
        <div className="space-y-2">
          <div className="h-1.5 rounded-full bg-[var(--bg-main)] overflow-hidden max-w-md">
            <div className="h-full bg-[var(--brand-solid)] transition-all duration-300" style={{ width: `${pct}%` }} />
          </div>
          <p className="text-xs text-[var(--text-secondary)]">
            {status.running ? "Running" : "Finished"}: {status.done} done, {status.skipped} skipped, {status.failed} failed
            {status.queued ? `, ${status.queued} queued` : ""}
          </p>
          {status.recent.some(r => r.status === "failed") && (
            <ul className="text-xs text-[var(--error)] space-y-0.5">
              {status.recent.filter(r => r.status === "failed").slice(0, 5).map(r => (
                <li key={r.job_id + r.finished_at}>{r.template || "Template"} on {r.job_id.slice(0, 8)}: {r.error}</li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
