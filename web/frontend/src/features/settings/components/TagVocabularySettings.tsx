import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useAuth } from "@/features/auth/hooks/useAuth";
import { ChevronDown, ChevronUp, Loader2 } from "lucide-react";

interface Settings {
  owner_name: string;
  redact_pii: boolean;
  tag_strict: boolean;
  tag_types: string;
  tag_topics: string;
  tag_synonyms: string;
  tag_name_hints: string;
  tag_title_prefixes: string;
  default_tag_types: string;
  default_tag_topics: string;
  default_tag_synonyms: string;
  default_tag_name_hints: string;
  default_tag_title_prefixes: string;
  tag_keywords: number;
  type_count: number;
  topic_count: number;
}

type ListKey = "tag_types" | "tag_topics" | "tag_synonyms" | "tag_name_hints" | "tag_title_prefixes";

const lists: { key: ListKey; def: keyof Settings; label: string; help: string; rows: number }[] = [
  {
    key: "tag_types", def: "default_tag_types", label: "Recording types (exactly one per recording)", rows: 9,
    help: "One per line as tag: description. Start a line with ! to mark it sensitive. Template auto tags should use these.",
  },
  {
    key: "tag_topics", def: "default_tag_topics", label: "Topics (1 to 3 per recording)", rows: 10,
    help: "Same format. Keep topics that split your recordings into useful groups; details belong in the brief.",
  },
  {
    key: "tag_synonyms", def: "default_tag_synonyms", label: "Synonyms", rows: 6,
    help: "old, other old = tag. Maps the model's wording onto your tags before anything off the list is dropped.",
  },
  {
    key: "tag_name_hints", def: "default_tag_name_hints", label: "Recording type from the file name", rows: 6,
    help: "words, other words = type. When the name you gave the file contains these words, that type wins over the model. The longest match wins (speech therapy beats therapy).",
  },
  {
    key: "tag_title_prefixes", def: "default_tag_title_prefixes", label: "Title label by type", rows: 4,
    help: "type = Label. Generated titles start with the label after the date: 2026-06-03 Therapy: Boundaries With Family.",
  },
];

// TagVocabularySettings controls how recordings are tagged: a fixed list of
// recording types and topics the model chooses from, automatic sensitive,
// pii and youtube tags, PII redaction and the owner's name for templates.
export function TagVocabularySettings({ disabled = false }: { disabled?: boolean }) {
  const { getAuthHeaders } = useAuth();
  const queryClient = useQueryClient();
  const [s, setS] = useState<Settings | null>(null);
  const [draft, setDraft] = useState<Record<ListKey, string>>({ tag_types: "", tag_topics: "", tag_synonyms: "", tag_name_hints: "", tag_title_prefixes: "" });
  const [owner, setOwner] = useState("");
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  const apply = (data: Settings) => {
    setS(data);
    setOwner(data.owner_name || "");
    setDraft({ tag_types: data.tag_types, tag_topics: data.tag_topics, tag_synonyms: data.tag_synonyms, tag_name_hints: data.tag_name_hints, tag_title_prefixes: data.tag_title_prefixes });
  };

  useEffect(() => {
    fetch("/api/v1/summaries/settings", { headers: getAuthHeaders() })
      .then(r => (r.ok ? r.json() : null))
      .then(d => d && apply(d))
      .catch(() => { /* offline */ });
  }, [getAuthHeaders]);

  const save = async (patch: Partial<Record<string, unknown>>, note = "Saved.") => {
    setSaving(true);
    setMessage(null);
    try {
      const res = await fetch("/api/v1/summaries/settings", {
        method: "POST",
        headers: { "Content-Type": "application/json", ...getAuthHeaders() },
        body: JSON.stringify(patch),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data?.error || "Could not save");
      apply(data);
      setMessage(note);
      queryClient.invalidateQueries({ queryKey: ["summarySettings"] });
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Could not save");
    } finally {
      setSaving(false);
    }
  };

  if (!s) return null;
  const dirty = lists.some(l => draft[l.key] !== s[l.key]);

  return (
    <div className="mb-6 pb-6 border-b border-[var(--border-subtle)] space-y-4">
      <div>
        <h4 className="text-sm font-medium text-[var(--text-primary)]">Tags and privacy</h4>
        <p className="text-xs text-[var(--text-secondary)] mt-1 max-w-xl">
          After the default summary, the model picks one recording type and 2 or 3 topics from fixed lists, plus a
          few free-form keywords for specific details. Scriberr adds <b>youtube</b> for downloaded videos. The flags
          <b> sensitive</b> (sensitive type or topic, or PII) and <b>pii</b> (a date of birth, account number or similar
          was spoken) are kept apart from the tags.
        </p>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 max-w-3xl">
        <label className="flex items-center justify-between gap-3 text-sm text-[var(--text-secondary)]">
          Use the fixed tag lists
          <Switch checked={s.tag_strict} disabled={disabled || saving} onCheckedChange={v => save({ tag_strict: v })} />
        </label>
        <label className="flex items-center justify-between gap-3 text-sm text-[var(--text-secondary)]">
          Redact identifiers in summaries
          <Switch checked={s.redact_pii} disabled={disabled || saving} onCheckedChange={v => save({ redact_pii: v })} />
        </label>
      </div>

      <label className="flex items-center gap-3 text-sm text-[var(--text-secondary)]">
        Free-form keywords per recording
        <select
          className="h-9 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-main)] px-2 text-sm text-[var(--text-primary)]"
          value={s.tag_keywords}
          onChange={e => save({ tag_keywords: Number(e.target.value) })}
          disabled={disabled || saving || !s.tag_strict}
          aria-label="Free-form keywords per recording"
        >
          {[0, 1, 2, 3, 4, 5].map(n => <option key={n} value={n}>{n === 0 ? "None" : `Up to ${n}`}</option>)}
        </select>
      </label>

      <div className="max-w-md">
        <label htmlFor="ownerName" className="text-sm text-[var(--text-secondary)]">Your name as speakers are labeled</label>
        <div className="flex gap-2 mt-1">
          <Input id="ownerName" value={owner} onChange={e => setOwner(e.target.value)} placeholder="e.g. Jason" disabled={disabled} />
          <Button size="sm" variant="outline" disabled={disabled || saving || owner === (s.owner_name || "")} onClick={() => save({ owner_name: owner })}>
            Save
          </Button>
        </div>
        <p className="text-xs text-[var(--text-tertiary)] mt-1">Templates write to this person as "you" (the {"{me}"} placeholder).</p>
      </div>

      <button type="button" onClick={() => setOpen(o => !o)} className="flex items-center gap-1 text-sm text-[var(--brand-solid)]" aria-expanded={open}>
        {open ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
        Edit tag lists ({s.type_count} types, {s.topic_count} topics)
      </button>

      {open && (
        <div className="space-y-4 max-w-3xl">
          {lists.map(l => (
            <div key={l.key}>
              <div className="flex items-center justify-between gap-2">
                <label className="text-sm font-medium text-[var(--text-primary)]">{l.label}</label>
                {draft[l.key] !== s[l.def] && (
                  <button type="button" className="text-xs text-[var(--text-tertiary)] hover:underline"
                    onClick={() => setDraft(d => ({ ...d, [l.key]: s[l.def] as string }))}>
                    Reset to default
                  </button>
                )}
              </div>
              <p className="text-xs text-[var(--text-tertiary)] mb-1">{l.help}</p>
              <Textarea rows={l.rows} value={draft[l.key]} disabled={disabled}
                onChange={e => setDraft(d => ({ ...d, [l.key]: e.target.value }))}
                className="font-mono text-xs" />
            </div>
          ))}
          <div className="flex items-center gap-3">
            <Button size="sm" disabled={disabled || saving || !dirty} onClick={() => save(draft, "Saved. Retag existing recordings below to apply.")}>
              {saving && <Loader2 className="h-4 w-4 animate-spin" />} Save lists
            </Button>
            {dirty && <span className="text-xs text-[var(--text-tertiary)]">Unsaved changes</span>}
          </div>
        </div>
      )}
      {message && <p className="text-xs text-[var(--text-secondary)]">{message}</p>}
    </div>
  );
}
