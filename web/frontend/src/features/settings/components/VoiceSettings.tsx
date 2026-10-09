import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { useAuth } from "@/features/auth/hooks/useAuth";
import { Check, Loader2, Pencil, Play, RefreshCw, Square, Trash2, X } from "lucide-react";

interface VoiceProfile {
  id: string;
  name: string;
  samples: number;
  seconds: number;
  recordings: number;
}

export interface VoicesState {
  available: boolean;
  auto: boolean;
  threshold: number;
  default_threshold: number;
  profiles: VoiceProfile[];
  counts: Record<string, number>;
  queue: number;
  current?: string;
  done: number;
  failed: number;
  matching: boolean;
}

const selectClass =
  "h-9 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-main)] px-2 text-sm text-[var(--text-primary)] outline-none focus:ring-2 focus:ring-[var(--brand-solid)]/20";

const thresholds = [
  { v: 0.5, label: "0.50 (loose: more names, more mistakes)" },
  { v: 0.55, label: "0.55" },
  { v: 0.6, label: "0.60 (default)" },
  { v: 0.65, label: "0.65" },
  { v: 0.7, label: "0.70" },
  { v: 0.75, label: "0.75 (strict: fewer names)" },
];

function minutes(sec: number) {
  return sec < 60 ? `${Math.round(sec)}s` : `${Math.round(sec / 60)} min`;
}

// VoiceSettings manages voice ID: known voices, automatic scans of new
// recordings, scanning the library, and the match threshold.
export function VoiceSettings() {
  const { getAuthHeaders } = useAuth();
  const queryClient = useQueryClient();
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [confirmRescan, setConfirmRescan] = useState(false);

  const { data: s } = useQuery({
    queryKey: ["voices"],
    queryFn: async () => {
      const res = await fetch("/api/v1/voices", { headers: getAuthHeaders() });
      if (!res.ok) throw new Error("Could not load voices");
      return (await res.json()) as VoicesState;
    },
    refetchInterval: q => {
      const d = q.state.data as VoicesState | undefined;
      return d && (d.queue > 0 || d.current || d.matching) ? 3000 : false;
    },
  });

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const call = async (method: string, url: string, body?: unknown, note?: (d: any) => string) => {
    setBusy(true);
    setMessage(null);
    try {
      const res = await fetch(url, {
        method,
        headers: { "Content-Type": "application/json", ...getAuthHeaders() },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data?.error || "Request failed");
      const state: VoicesState | undefined = data?.state ?? (data?.profiles ? data : undefined);
      if (state) queryClient.setQueryData(["voices"], state);
      else queryClient.invalidateQueries({ queryKey: ["voices"] });
      if (note) setMessage(note(data));
      return true;
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Request failed");
      return false;
    } finally {
      setBusy(false);
    }
  };

  if (!s) return null;
  const c = s.counts || {};
  const unscanned = c[""] ?? 0;
  const scanned = c["done"] ?? 0;
  const running = s.queue > 0 || !!s.current;

  return (
    <div className="space-y-6">
      <div>
        <h4 className="text-sm font-medium text-[var(--text-primary)]">Voice ID</h4>
        <p className="text-xs text-[var(--text-secondary)] mt-1 max-w-2xl">
          Scriberr takes a voice print of each speaker in a recording. When you name a speaker, that voice is learned,
          and other recordings where the same voice speaks get the name automatically. Names you type are never
          changed. Recordings where one label holds two voices, or one voice got two labels, are flagged <b>redo</b>.
          Voice prints stay on this server.
        </p>
        {!s.available && (
          <p className="text-xs text-amber-600 dark:text-amber-400 mt-2">
            Voice ID is off on this server: it needs the PyAnnote models and an HF_TOKEN.
          </p>
        )}
      </div>

      <div className="grid gap-3 sm:grid-cols-2 max-w-3xl">
        <label className="flex items-center justify-between gap-3 text-sm text-[var(--text-secondary)]">
          Scan new recordings automatically
          <Switch checked={s.auto} disabled={busy || !s.available} onCheckedChange={v => call("POST", "/api/v1/voices/settings", { auto: v })} />
        </label>
        <label className="flex items-center justify-between gap-3 text-sm text-[var(--text-secondary)]">
          Match threshold
          <select
            className={selectClass}
            value={String(s.threshold)}
            disabled={busy}
            onChange={e => call("POST", "/api/v1/voices/settings", { threshold: Number(e.target.value) }, () => "Saved. Re-applying voices to the library.")}
            aria-label="Match threshold"
          >
            {!thresholds.some(t => t.v === s.threshold) && <option value={String(s.threshold)}>{s.threshold.toFixed(2)}</option>}
            {thresholds.map(t => <option key={t.v} value={String(t.v)}>{t.label}</option>)}
          </select>
        </label>
      </div>

      <div className="max-w-3xl space-y-2">
        <div className="text-sm text-[var(--text-secondary)]">
          Library: <b className="text-[var(--text-primary)]">{scanned}</b> scanned, {unscanned} not scanned
          {(c["failed"] ?? 0) > 0 && <>, {c["failed"]} failed</>}
          {(c["none"] ?? 0) > 0 && <>, {c["none"]} without speakers</>}
          {(c["redo"] ?? 0) > 0 && <>, <b className="text-amber-600 dark:text-amber-400">{c["redo"]} flagged redo</b> (search "redo")</>}
        </div>
        {running && (
          <div className="flex items-center gap-2 text-sm text-[var(--text-secondary)]">
            <Loader2 className="h-4 w-4 animate-spin" />
            Scanning: {s.queue + (s.current ? 1 : 0)} left, {s.done} done{s.failed > 0 && `, ${s.failed} failed`}
          </div>
        )}
        <div className="flex flex-wrap gap-2">
          {running ? (
            <Button size="sm" variant="outline" disabled={busy} onClick={() => call("DELETE", "/api/v1/voices/scan", undefined, d => `Stopped; ${d.cancelled} not scanned.`)}>
              <Square className="h-4 w-4" /> Stop after current
            </Button>
          ) : (
            <Button size="sm" disabled={busy || !s.available || unscanned + (c["failed"] ?? 0) === 0}
              onClick={() => call("POST", "/api/v1/voices/scan", {}, d => `Queued ${d.queued} recordings.`)}>
              <Play className="h-4 w-4" /> Scan {unscanned} not scanned
            </Button>
          )}
          {!running && (confirmRescan ? (
            <>
              <Button size="sm" variant="destructive" disabled={busy}
                onClick={() => { setConfirmRescan(false); call("POST", "/api/v1/voices/scan", { rescan: true }, d => `Queued ${d.queued} recordings.`); }}>
                Rescan all {scanned + unscanned + (c["failed"] ?? 0)}
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setConfirmRescan(false)}>Cancel</Button>
            </>
          ) : (
            <Button size="sm" variant="outline" disabled={busy || !s.available} onClick={() => setConfirmRescan(true)}>
              <RefreshCw className="h-4 w-4" /> Rescan all
            </Button>
          ))}
          <Button size="sm" variant="outline" disabled={busy || s.matching || s.profiles.length === 0}
            onClick={() => call("POST", "/api/v1/voices/match", undefined, () => "Applying voices to the library.")}>
            {s.matching ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />} Apply voices now
          </Button>
        </div>
        <p className="text-xs text-[var(--text-tertiary)]">
          A scan uses the GPU for a few seconds per recording and waits for transcriptions. Failed scans are retried by
          "Scan not scanned".
        </p>
      </div>

      <div className="max-w-3xl">
        <h5 className="text-sm font-medium text-[var(--text-primary)] mb-2">Known voices</h5>
        {s.profiles.length === 0 ? (
          <p className="text-xs text-[var(--text-tertiary)]">
            None yet. Open a scanned recording, rename a speaker to a real name, and that voice is learned.
          </p>
        ) : (
          <div className="border border-[var(--border-subtle)] rounded-lg divide-y divide-[var(--border-subtle)]">
            {s.profiles.map(p => (
              <div key={p.id} className="flex items-center gap-3 px-3 py-2 text-sm">
                {editing === p.id ? (
                  <form className="flex items-center gap-2 flex-1" onSubmit={async e => {
                    e.preventDefault();
                    if (await call("PATCH", `/api/v1/voices/${p.id}`, { name })) setEditing(null);
                  }}>
                    <Input value={name} onChange={e => setName(e.target.value)} className="h-8" autoFocus aria-label="Voice name" />
                    <Button size="sm" type="submit" disabled={busy || !name.trim()}><Check className="h-4 w-4" /></Button>
                    <Button size="sm" variant="ghost" type="button" onClick={() => setEditing(null)}><X className="h-4 w-4" /></Button>
                  </form>
                ) : (
                  <>
                    <span className="flex-1 font-medium text-[var(--text-primary)]">{p.name}</span>
                    <span className="text-xs text-[var(--text-tertiary)] w-28 text-right">{p.recordings} recordings</span>
                    <span className="text-xs text-[var(--text-tertiary)] w-32 text-right" title="Recordings where you named this voice">
                      learned from {p.samples}, {minutes(p.seconds)}
                    </span>
                    <Button size="sm" variant="ghost" aria-label={`Rename ${p.name}`} onClick={() => { setEditing(p.id); setName(p.name); }}>
                      <Pencil className="h-4 w-4" />
                    </Button>
                    <Button size="sm" variant="ghost" aria-label={`Delete ${p.name}`} disabled={busy}
                      onClick={() => call("DELETE", `/api/v1/voices/${p.id}`, undefined, () => `Deleted ${p.name}. Names you typed are kept.`)}>
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
      {message && <p className="text-xs text-[var(--text-secondary)]">{message}</p>}
    </div>
  );
}
