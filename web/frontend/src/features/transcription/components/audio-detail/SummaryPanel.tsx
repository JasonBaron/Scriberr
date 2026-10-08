import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { useToast } from "@/components/ui/toast";
import { Sparkles, Copy, FileDown, FileText, RefreshCw, ChevronDown, ChevronUp, Wand2, Tag } from "lucide-react";
import { useSummaries } from "@/features/transcription/hooks/useTranscriptionSummary";
import { useUpdateTitle, type AudioFile } from "@/features/transcription/hooks/useAudioDetail";
import { SummaryMarkdown } from "./SummaryMarkdown";
import { downloadText, markdownToText, summaryFilename } from "./summaryFiles";

interface SummaryPanelProps {
    audioId: string;
    audioFile: AudioFile;
    onRegenerate: () => void;
}

const pillButton = "h-8 rounded-full border-[var(--border-subtle)] bg-[var(--bg-card)] hover:bg-[var(--bg-main)] text-xs gap-1.5 px-3";

function formatWhen(iso: string): string {
    const d = new Date(iso);
    if (isNaN(d.getTime())) return "";
    return d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

// SummaryPanel shows the stored summary on the job page, with copy and
// download, a switcher when there are several, and the suggested title and
// tags from the last summary.
export function SummaryPanel({ audioId, audioFile, onRegenerate }: SummaryPanelProps) {
    const { toast } = useToast();
    const { data: summaries = [] } = useSummaries(audioId);
    const { mutate: updateTitle, isPending: applying } = useUpdateTitle(audioId);
    const [selectedId, setSelectedId] = useState<string>("");
    const [expanded, setExpanded] = useState(true);

    // Show the newest summary when the panel loads and whenever a new one
    // arrives; the user can still switch to an older one in between.
    const newestId = summaries[0]?.id ?? "";
    useEffect(() => {
        if (newestId) setSelectedId(newestId);
    }, [newestId]);

    const selected = summaries.find(s => s.id === selectedId) || summaries[0];
    const suggestedTitle = audioFile.suggested_title?.trim();
    const showTitleSuggestion = !!suggestedTitle && suggestedTitle !== (audioFile.title || "").trim();
    const tags = audioFile.suggested_tags || [];

    if (!selected) return null;

    const handleCopy = async () => {
        try {
            await navigator.clipboard.writeText(selected.content);
            toast({ title: "Summary copied to clipboard" });
        } catch {
            toast({ title: "Copy failed", description: "Clipboard access was denied" });
        }
    };

    const label = (s: typeof selected) =>
        `${s.template_name || "Summary"} · ${formatWhen(s.created_at)}${s.model ? ` · ${s.model}` : ""}`;

    return (
        <section className="glass-card rounded-[var(--radius-card)] border-[var(--border-subtle)] shadow-[var(--shadow-card)] p-4 md:p-6">
            <div className="flex flex-wrap items-center justify-between gap-3">
                <button
                    type="button"
                    onClick={() => setExpanded(e => !e)}
                    className="flex items-center gap-2 text-left min-w-0"
                    aria-expanded={expanded}
                >
                    <div className="h-7 w-7 rounded-full bg-gradient-to-br from-[#FFAB40] to-[#FF6D20] flex items-center justify-center shrink-0">
                        <Sparkles className="h-3.5 w-3.5 text-white" />
                    </div>
                    <h2 className="text-lg font-semibold text-[var(--text-primary)]">Summary</h2>
                    {expanded ? <ChevronUp className="h-4 w-4 text-[var(--text-tertiary)]" /> : <ChevronDown className="h-4 w-4 text-[var(--text-tertiary)]" />}
                </button>

                <div className="flex flex-wrap items-center gap-2">
                    <Button variant="outline" size="sm" className={pillButton} onClick={handleCopy}>
                        <Copy className="h-3.5 w-3.5" /> Copy
                    </Button>
                    <Button variant="outline" size="sm" className={pillButton}
                        onClick={() => downloadText(selected.content, summaryFilename(audioFile.title, "md"), "text/markdown")}>
                        <FileDown className="h-3.5 w-3.5" /> .md
                    </Button>
                    <Button variant="outline" size="sm" className={pillButton}
                        onClick={() => downloadText(markdownToText(selected.content), summaryFilename(audioFile.title, "txt"), "text/plain")}>
                        <FileText className="h-3.5 w-3.5" /> .txt
                    </Button>
                    <Button variant="outline" size="sm" className={pillButton} onClick={onRegenerate}>
                        <RefreshCw className="h-3.5 w-3.5" /> New
                    </Button>
                </div>
            </div>

            {summaries.length > 1 ? (
                <select
                    value={selected.id}
                    onChange={(e) => setSelectedId(e.target.value)}
                    className="mt-3 w-full sm:w-auto max-w-full h-8 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-main)] px-2 text-xs text-[var(--text-secondary)] outline-none focus:ring-2 focus:ring-[var(--brand-solid)]/20"
                    aria-label="Choose summary"
                >
                    {summaries.map(s => (
                        <option key={s.id} value={s.id}>{label(s)}</option>
                    ))}
                </select>
            ) : (
                <p className="mt-2 text-xs text-[var(--text-tertiary)]">{label(selected)}</p>
            )}

            {(showTitleSuggestion || tags.length > 0) && (
                <div className="mt-3 space-y-2">
                    {showTitleSuggestion && (
                        <div className="flex flex-wrap items-center gap-2 text-sm">
                            <Wand2 className="h-3.5 w-3.5 text-[var(--brand-solid)] shrink-0" />
                            <span className="text-[var(--text-tertiary)]">Suggested title:</span>
                            <span className="text-[var(--text-primary)] font-medium break-all">{suggestedTitle}</span>
                            <Button
                                variant="outline"
                                size="sm"
                                className={pillButton}
                                disabled={applying}
                                onClick={() => updateTitle(suggestedTitle!, {
                                    onSuccess: () => toast({ title: "Title updated" }),
                                    onError: () => toast({ title: "Could not update the title" }),
                                })}
                            >
                                Apply
                            </Button>
                        </div>
                    )}
                    {tags.length > 0 && (
                        <div className="flex flex-wrap items-center gap-1.5">
                            <Tag className="h-3.5 w-3.5 text-[var(--text-tertiary)] shrink-0" />
                            {tags.map(t => (
                                <span key={t} className="text-xs px-2 py-0.5 rounded-full bg-[var(--bg-main)] border border-[var(--border-subtle)] text-[var(--text-secondary)]">
                                    {t}
                                </span>
                            ))}
                        </div>
                    )}
                </div>
            )}

            {expanded && (
                <div className="mt-4 max-h-[60vh] overflow-y-auto font-reading prose prose-stone dark:prose-invert max-w-none text-[#171717] dark:text-[#EDEDED] leading-relaxed">
                    <SummaryMarkdown content={selected.content} />
                </div>
            )}
        </section>
    );
}
