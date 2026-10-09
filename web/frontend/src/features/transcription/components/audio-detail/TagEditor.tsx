import { useMemo, useRef, useState } from "react";
import { Tag, X, Check, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useToast } from "@/components/ui/toast";
import { cn } from "@/lib/utils";
import { normalizeTag, useTagCounts, useUpdateTags } from "@/features/transcription/hooks/useTags";
import { flagTagClass } from "@/features/transcription/utils/tagStyle";

interface TagEditorProps {
    audioId: string;
    tags: string[];
}

const chip = "text-xs px-2 py-0.5 rounded-full border border-[var(--border-subtle)] bg-[var(--bg-main)] text-[var(--text-secondary)]";

// TagEditor shows a recording's tags. Clicking the tag icon switches to
// editing: remove tags, type new ones, or pick from tags used on other
// recordings.
export function TagEditor({ audioId, tags }: TagEditorProps) {
    const { toast } = useToast();
    const [editing, setEditing] = useState(false);
    const [draft, setDraft] = useState<string[]>(tags);
    const [input, setInput] = useState("");
    const inputRef = useRef<HTMLInputElement>(null);
    const { data: counts = [] } = useTagCounts(editing);
    const { mutate: save, isPending } = useUpdateTags(audioId);

    const startEdit = () => {
        setDraft(tags);
        setInput("");
        setEditing(true);
        setTimeout(() => inputRef.current?.focus(), 0);
    };

    const add = (raw: string) => {
        const t = normalizeTag(raw);
        if (t && !draft.includes(t)) setDraft([...draft, t]);
        setInput("");
    };

    // Tags used elsewhere, filtered by what is typed, not already applied
    const suggestions = useMemo(() => {
        const q = normalizeTag(input);
        return counts
            .filter(c => !draft.includes(c.tag) && (!q || c.tag.includes(q)))
            .slice(0, q ? 8 : 12);
    }, [counts, draft, input]);

    const commit = () => {
        const pending = normalizeTag(input);
        const next = pending && !draft.includes(pending) ? [...draft, pending] : draft;
        save(next, {
            onSuccess: () => { setEditing(false); setInput(""); },
            onError: () => toast({ title: "Could not save tags" }),
        });
    };

    if (!editing) {
        return (
            <div className="flex flex-wrap items-center gap-1.5">
                <button
                    type="button"
                    onClick={startEdit}
                    className="p-1 -ml-1 rounded-md text-[var(--text-tertiary)] hover:text-[var(--brand-solid)] hover:bg-[var(--bg-main)] transition-colors"
                    title="Edit tags"
                    aria-label="Edit tags"
                >
                    <Tag className="h-3.5 w-3.5" />
                </button>
                {tags.length === 0 ? (
                    <button type="button" onClick={startEdit} className="text-xs text-[var(--text-tertiary)] hover:text-[var(--brand-solid)]">
                        Add tags
                    </button>
                ) : tags.map(t => <span key={t} className={cn(chip, flagTagClass(t))}>{t}</span>)}
            </div>
        );
    }

    return (
        <div className="space-y-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-main)] p-3">
            <div className="flex flex-wrap items-center gap-1.5">
                <Tag className="h-3.5 w-3.5 text-[var(--brand-solid)]" />
                {draft.map(t => (
                    <span key={t} className={cn(chip, "inline-flex items-center gap-1 pr-1 bg-[var(--bg-card)]")}>
                        {t}
                        <button
                            type="button"
                            onClick={() => setDraft(draft.filter(d => d !== t))}
                            className="rounded-full p-0.5 hover:bg-[var(--error)]/10 hover:text-[var(--error)]"
                            aria-label={`Remove ${t}`}
                        >
                            <X className="h-3 w-3" />
                        </button>
                    </span>
                ))}
                <input
                    ref={inputRef}
                    value={input}
                    onChange={(e) => setInput(e.target.value)}
                    onKeyDown={(e) => {
                        if ((e.key === "Enter" || e.key === ",") && input.trim()) {
                            e.preventDefault();
                            add(input);
                        } else if (e.key === "Enter") {
                            e.preventDefault();
                            commit();
                        } else if (e.key === "Backspace" && !input && draft.length) {
                            setDraft(draft.slice(0, -1));
                        } else if (e.key === "Escape") {
                            setEditing(false);
                        }
                    }}
                    placeholder={draft.length ? "Add a tag" : "Type a tag and press Enter"}
                    className="flex-1 min-w-[8rem] bg-transparent text-sm text-[var(--text-primary)] outline-none placeholder:text-[var(--text-tertiary)] py-0.5"
                />
            </div>

            {suggestions.length > 0 && (
                <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-[11px] uppercase tracking-wide text-[var(--text-tertiary)] mr-1">
                        {input.trim() ? "Matching" : "Used elsewhere"}
                    </span>
                    {suggestions.map(s => (
                        <button
                            key={s.tag}
                            type="button"
                            onClick={() => add(s.tag)}
                            className={cn(chip, "inline-flex items-center gap-1 hover:border-[var(--brand-solid)] hover:text-[var(--brand-solid)]")}
                        >
                            <Plus className="h-3 w-3" />{s.tag}
                            <span className="text-[10px] text-[var(--text-tertiary)]">{s.count}</span>
                        </button>
                    ))}
                </div>
            )}

            <div className="flex justify-end gap-2">
                <Button variant="ghost" size="sm" className="h-8 rounded-full text-xs" onClick={() => setEditing(false)} disabled={isPending}>
                    Cancel
                </Button>
                <Button size="sm" className="h-8 rounded-full text-xs gap-1" onClick={commit} disabled={isPending}>
                    <Check className="h-3.5 w-3.5" /> Save tags
                </Button>
            </div>
        </div>
    );
}
