import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useToast } from "@/components/ui/toast";
import { useAuth } from "@/features/auth/hooks/useAuth";
import { Copy, FileAudio, Loader2 } from "lucide-react";

interface FileInfo {
    job_id: string;
    original_filename?: string;
    stored_filename: string;
    file_exists: boolean;
    stored_size?: number;
    uploaded_size?: number;
    sha256?: string;
    hash_of?: string;
    duplicates: { id: string; title: string; created_at: string }[];
    recorded_at?: string;
    recorded_at_source?: string;
    uploaded_at: string;
    media?: {
        format?: string;
        format_name?: string;
        duration_seconds?: number;
        bit_rate?: number;
        codec?: string;
        sample_rate?: number;
        channels?: number;
        channel_layout?: string;
        tags?: Record<string, string>;
    };
    probe_error?: string;
}

interface FileInfoDialogProps {
    audioId: string;
    isOpen: boolean;
    onClose: (open: boolean) => void;
}

function formatBytes(n?: number): string | undefined {
    if (!n) return undefined;
    const units = ["B", "KB", "MB", "GB"];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]} (${n.toLocaleString()} bytes)`;
}

function formatDuration(s?: number): string | undefined {
    if (!s) return undefined;
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = Math.round(s % 60);
    const mm = String(m).padStart(h ? 2 : 1, "0");
    return `${h ? `${h}:` : ""}${mm}:${String(sec).padStart(2, "0")}`;
}

function formatDate(iso?: string): string | undefined {
    if (!iso) return undefined;
    const d = new Date(iso);
    return isNaN(d.getTime()) ? iso : d.toLocaleString();
}

const sourceLabel: Record<string, string> = {
    metadata: "from a date tag in the file",
    file: "from the file's modified time",
};

function Section({ title, rows }: { title: string; rows: [string, string | undefined][] }) {
    const shown = rows.filter(([, v]) => v);
    if (!shown.length) return null;
    return (
        <section>
            <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)] mb-2">{title}</h3>
            <dl className="grid grid-cols-[minmax(7rem,auto)_1fr] gap-x-4 gap-y-1.5 text-sm">
                {shown.map(([k, v]) => (
                    <div key={k} className="contents">
                        <dt className="text-[var(--text-tertiary)]">{k}</dt>
                        <dd className="text-[var(--text-primary)] break-all">{v}</dd>
                    </div>
                ))}
            </dl>
        </section>
    );
}

// FileInfoDialog shows a recording's file details: format and codec, dates,
// SHA-256 with any other recordings of the same file, and embedded tags.
export function FileInfoDialog({ audioId, isOpen, onClose }: FileInfoDialogProps) {
    const { getAuthHeaders } = useAuth();
    const { toast } = useToast();
    const navigate = useNavigate();
    const { data, isLoading, error } = useQuery({
        queryKey: ["fileInfo", audioId],
        queryFn: async () => {
            const res = await fetch(`/api/v1/transcription/${audioId}/file-info`, { headers: getAuthHeaders() });
            if (!res.ok) throw new Error("Failed to load file info");
            return res.json() as Promise<FileInfo>;
        },
        enabled: isOpen,
    });

    const m = data?.media;
    const tags = Object.entries(m?.tags || {}).sort(([a], [b]) => a.localeCompare(b));
    const channels = m?.channels ? `${m.channels}${m.channel_layout ? ` (${m.channel_layout})` : ""}` : undefined;

    return (
        <Dialog open={isOpen} onOpenChange={onClose}>
            <DialogContent className="w-[calc(100%-2rem)] max-w-2xl bg-[var(--bg-card)] border border-[var(--border-subtle)] rounded-2xl p-0 max-h-[85vh] overflow-hidden flex flex-col">
                <DialogHeader className="p-5 pb-4 border-b border-[var(--border-subtle)]">
                    <DialogTitle className="text-xl font-bold text-[var(--text-primary)] flex items-center gap-2">
                        <FileAudio className="h-5 w-5 text-[var(--brand-solid)]" /> File Info
                    </DialogTitle>
                    <DialogDescription className="text-[var(--text-secondary)]">
                        {data?.original_filename || data?.stored_filename || "Recording file"}
                    </DialogDescription>
                </DialogHeader>

                <div className="p-5 space-y-6 overflow-y-auto">
                    {isLoading && (
                        <div className="flex items-center gap-2 text-sm text-[var(--text-tertiary)]">
                            <Loader2 className="h-4 w-4 animate-spin" /> Reading file...
                        </div>
                    )}
                    {error && <p className="text-sm text-[var(--error)]">Could not load file info.</p>}

                    {data && (
                        <>
                            {!data.file_exists && (
                                <p className="text-sm text-[var(--error)]">The stored audio file is missing.</p>
                            )}

                            {data.duplicates.length > 0 && (
                                <section className="rounded-xl border border-amber-500/30 bg-amber-500/10 p-3">
                                    <h3 className="text-sm font-medium text-amber-600 dark:text-amber-400 mb-1">
                                        Same file as {data.duplicates.length === 1 ? "another recording" : `${data.duplicates.length} other recordings`}
                                    </h3>
                                    <ul className="text-sm space-y-0.5">
                                        {data.duplicates.map(d => (
                                            <li key={d.id}>
                                                <button
                                                    type="button"
                                                    className="text-[var(--brand-solid)] hover:underline text-left"
                                                    onClick={() => { onClose(false); navigate(`/audio/${d.id}`); }}
                                                >
                                                    {d.title || d.id}
                                                </button>
                                                <span className="text-[var(--text-tertiary)]"> · uploaded {formatDate(d.created_at)}</span>
                                            </li>
                                        ))}
                                    </ul>
                                </section>
                            )}

                            <Section title="File" rows={[
                                ["Original name", data.original_filename],
                                ["Stored as", data.stored_filename],
                                ["Size", formatBytes(data.stored_size || data.uploaded_size)],
                                ["Uploaded size", data.uploaded_size && data.stored_size && data.uploaded_size !== data.stored_size ? formatBytes(data.uploaded_size) : undefined],
                                ["Format", m?.format || m?.format_name],
                                ["Codec", m?.codec],
                                ["Duration", formatDuration(m?.duration_seconds)],
                                ["Sample rate", m?.sample_rate ? `${(m.sample_rate / 1000).toFixed(1)} kHz` : undefined],
                                ["Channels", channels],
                                ["Bit rate", m?.bit_rate ? `${Math.round(m.bit_rate / 1000)} kbps` : undefined],
                            ]} />

                            <Section title="Dates" rows={[
                                ["Recorded", data.recorded_at ? `${formatDate(data.recorded_at)} ${sourceLabel[data.recorded_at_source || ""] ? `(${sourceLabel[data.recorded_at_source || ""]})` : ""}` : "Unknown"],
                                ["Uploaded", formatDate(data.uploaded_at)],
                            ]} />

                            {data.sha256 && (
                                <section>
                                    <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)] mb-2">SHA-256</h3>
                                    <div className="flex items-start gap-2">
                                        <code className="text-xs break-all text-[var(--text-primary)] bg-[var(--bg-main)] rounded-md px-2 py-1.5 flex-1">{data.sha256}</code>
                                        <button
                                            type="button"
                                            className="p-1.5 rounded-md hover:bg-[var(--bg-main)] text-[var(--text-tertiary)]"
                                            aria-label="Copy hash"
                                            onClick={async () => {
                                                try {
                                                    await navigator.clipboard.writeText(data.sha256!);
                                                    toast({ title: "Hash copied" });
                                                } catch {
                                                    toast({ title: "Copy failed" });
                                                }
                                            }}
                                        >
                                            <Copy className="h-3.5 w-3.5" />
                                        </button>
                                    </div>
                                    {data.hash_of === "stored file" && (
                                        <p className="text-xs text-[var(--text-tertiary)] mt-1">
                                            Hash of the stored file. This recording was uploaded before hashing, so a converted upload may not match the original.
                                        </p>
                                    )}
                                </section>
                            )}

                            {tags.length > 0 && (
                                <Section title="Embedded tags" rows={tags as [string, string][]} />
                            )}
                            {data.probe_error && <p className="text-xs text-[var(--text-tertiary)]">{data.probe_error}</p>}
                        </>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    );
}
