import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuth } from "@/features/auth/hooks/useAuth";

export interface TagCount {
    tag: string;
    count: number;
}

// useTagCounts lists every tag in use, most used first.
export function useTagCounts(enabled = true) {
    const { getAuthHeaders } = useAuth();
    return useQuery({
        queryKey: ["tags"],
        queryFn: async () => {
            const res = await fetch("/api/v1/tags", { headers: getAuthHeaders() });
            if (!res.ok) return [] as TagCount[];
            return res.json() as Promise<TagCount[]>;
        },
        enabled,
        staleTime: 30 * 1000,
    });
}

export function useUpdateTags(audioId: string) {
    const { getAuthHeaders } = useAuth();
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (tags: string[]) => {
            const res = await fetch(`/api/v1/transcription/${audioId}/tags`, {
                method: "PUT",
                headers: { "Content-Type": "application/json", ...getAuthHeaders() },
                body: JSON.stringify({ tags }),
            });
            if (!res.ok) throw new Error("Failed to update tags");
            return res.json() as Promise<{ tags: string[] }>;
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["audio", audioId] });
            queryClient.invalidateQueries({ queryKey: ["audioFiles"] });
            queryClient.invalidateQueries({ queryKey: ["tags"] });
        },
    });
}

// normalizeTag mirrors the server's NormalizeTag so the editor shows what
// will be saved.
export function normalizeTag(tag: string): string {
    return tag
        .toLowerCase()
        .trim()
        .replace(/^[#"'`]+|[#"'`]+$/g, "")
        .replace(/_/g, " ")
        .split(/\s+/)
        .filter(Boolean)
        .join(" ")
        .replace(/[\p{P}]+$/u, "");
}
