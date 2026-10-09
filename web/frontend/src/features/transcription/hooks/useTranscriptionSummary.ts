import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuth } from "@/features/auth/hooks/useAuth";
import { useState } from "react";

export interface SummaryTemplate {
    id: string;
    name: string;
    model: string;
    prompt: string;
    include_speaker_info?: boolean;
    reasoning?: boolean;
    is_default?: boolean;
    enabled?: boolean;
}

const LAST_TEMPLATE_KEY = "scriberr.summary.lastTemplateId";

export function getLastTemplateId(): string | null {
    try {
        return window.localStorage.getItem(LAST_TEMPLATE_KEY);
    } catch {
        return null;
    }
}

export function setLastTemplateId(id: string) {
    try {
        window.localStorage.setItem(LAST_TEMPLATE_KEY, id);
    } catch {
        /* storage unavailable; preselect falls back to the default template */
    }
}

// pickInitialTemplate returns the template to preselect: the default
// template, else the last one used, else the only one.
export function pickInitialTemplate(templates: SummaryTemplate[]): string {
    const def = templates.find(t => t.is_default);
    if (def) return def.id;
    const last = getLastTemplateId();
    if (last && templates.some(t => t.id === last)) return last;
    if (templates.length === 1) return templates[0].id;
    return "";
}

export function useSummaryTemplates() {
    const { getAuthHeaders } = useAuth();
    return useQuery({
        queryKey: ["summaryTemplates"],
        queryFn: async () => {
            const response = await fetch("/api/v1/summaries", {
                headers: getAuthHeaders(),
            });
            if (!response.ok) throw new Error("Failed to load summary templates");
            return response.json() as Promise<SummaryTemplate[]>;
        },
        staleTime: 5 * 60 * 1000, // Templates don't change often
    });
}

export function useExistingSummary(audioId: string) {
    const { getAuthHeaders } = useAuth();
    return useQuery({
        queryKey: ["summary", audioId],
        queryFn: async () => {
            const response = await fetch(`/api/v1/transcription/${audioId}/summary`, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) return null; // No summary exists
            return response.json() as Promise<{ content: string }>;
        },
        retry: false,
    });
}

export interface StoredSummary {
    id: string;
    template_id?: string;
    template_name?: string;
    model: string;
    content: string;
    created_at: string;
}

export function useSummaries(audioId: string, enabled = true) {
    const { getAuthHeaders } = useAuth();
    return useQuery({
        queryKey: ["summaries", audioId],
        queryFn: async () => {
            const response = await fetch(`/api/v1/transcription/${audioId}/summaries`, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) return [] as StoredSummary[];
            return response.json() as Promise<StoredSummary[]>;
        },
        enabled: enabled && !!audioId,
    });
}

export function useSummarizer(audioId: string) {
    const { getAuthHeaders } = useAuth();
    const queryClient = useQueryClient();
    const [isStreaming, setIsStreaming] = useState(false);
    const [streamContent, setStreamContent] = useState("");
    const [error, setError] = useState<string | null>(null);

    const generateSummary = async (templateId: string, model: string, prompt: string, transcriptText: string, includeSpeakerInfo?: boolean) => {
        setIsStreaming(true);
        setStreamContent("");
        setError(null);

        const transcriptLabel = includeSpeakerInfo
            ? 'Transcript (with speaker labels - each line is prefixed with [SPEAKER_NAME]):'
            : 'Transcript:';
        const combinedContent = `${transcriptLabel}\n${transcriptText}\n\nInstructions:\n${prompt}`;

        try {
            const res = await fetch('/api/v1/summarize', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', ...getAuthHeaders() },
                body: JSON.stringify({
                    model: model,
                    content: combinedContent,
                    transcription_id: audioId,
                    template_id: templateId
                }),
            });

            if (!res.ok) {
                let message = `Summary request failed (${res.status})`;
                try {
                    const data = await res.json();
                    if (data?.error) message = data.error;
                } catch { /* not JSON */ }
                throw new Error(message);
            }
            if (!res.body) {
                throw new Error('Failed to start summary stream.');
            }
            setLastTemplateId(templateId);

            const reader = res.body.getReader();
            const decoder = new TextDecoder();

            while (true) {
                const { done, value } = await reader.read();
                if (done) {
                    const tail = decoder.decode();
                    if (tail) setStreamContent(prev => prev + tail);
                    break;
                }
                const chunk = decoder.decode(value, { stream: true });
                if (chunk) setStreamContent(prev => prev + chunk);
            }

            // Refresh the stored summary, the summary list and the job (the
            // server may have applied a suggested title and tags).
            queryClient.invalidateQueries({ queryKey: ["summary", audioId] });
            queryClient.invalidateQueries({ queryKey: ["summaries", audioId] });
            // Title and tags are generated in the background after the stream
            // closes; refetch the job a few times while they land.
            for (const delay of [3000, 10000, 30000]) {
                setTimeout(() => {
                    queryClient.invalidateQueries({ queryKey: ["audio", audioId] });
                    queryClient.invalidateQueries({ queryKey: ["audioFiles"] });
                }, delay);
            }

        } catch (e) {
            setError(e instanceof Error ? e.message : "Summary generation failed");
        } finally {
            setIsStreaming(false);
        }
    };

    return { generateSummary, isStreaming, streamContent, error };
}
