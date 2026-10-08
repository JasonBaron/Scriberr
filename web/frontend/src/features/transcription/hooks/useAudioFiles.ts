import { useQuery, useMutation, useQueryClient, keepPreviousData, useInfiniteQuery } from '@tanstack/react-query';
import { useAuth } from '@/features/auth/hooks/useAuth';
import { useToast } from '@/components/ui/toast';

export interface AudioFile {
    id: string;
    title?: string;
    status: "uploaded" | "pending" | "processing" | "completed" | "failed";
    created_at: string;
    audio_path: string;
    diarization?: boolean;
    is_multi_track?: boolean;
    error_message?: string;
    pipeline_stage?: string;
    pipeline_progress?: number;
    pipeline_message?: string;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    individual_transcripts?: any;
    speakers?: number;
    duration?: number;
    suggested_tags?: string[];
    tags?: string[];
    tags_edited?: boolean;
    summary_brief?: string;
    file_hash?: string;
    file_size?: number;
    original_filename?: string;
    duplicates?: { id: string; title: string; created_at: string }[];
    summary_status?: string;
    recorded_at?: string;
    recorded_at_source?: string;
}

export interface AudioFilesResponse {
    jobs: AudioFile[];
    pagination: {
        page: number;
        limit: number;
        total: number;
        pages: number;
    };
}

interface AudioListParams {
    page: number;
    limit: number;
    search?: string;
    sortBy?: string;
    sortOrder?: 'asc' | 'desc';
}

export function useAudioList(params: AudioListParams) {
    const { getAuthHeaders } = useAuth();

    return useQuery({
        queryKey: ['audioFiles', params],
        queryFn: async () => {
            const searchParams = new URLSearchParams({
                page: params.page.toString(),
                limit: params.limit.toString(),
            });

            if (params.search) searchParams.set('q', params.search);
            if (params.sortBy) {
                searchParams.set('sort_by', params.sortBy);
                searchParams.set('sort_order', params.sortOrder || 'desc');
            }

            const response = await fetch(`/api/v1/transcription/list?${searchParams}`, {
                headers: getAuthHeaders(),
            });

            if (!response.ok) {
                throw new Error('Failed to fetch audio files');
            }

            return response.json() as Promise<AudioFilesResponse>;
        },
        placeholderData: keepPreviousData,
        refetchInterval: false
    });
}

export function useAudioListInfinite(params: Omit<AudioListParams, 'page'>) {
    const { getAuthHeaders } = useAuth();

    return useInfiniteQuery({
        queryKey: ['audioFiles', 'infinite', params],
        queryFn: async ({ pageParam = 1 }) => {
            const searchParams = new URLSearchParams({
                page: pageParam.toString(),
                limit: params.limit.toString(),
            });

            if (params.search) searchParams.set('q', params.search);
            if (params.sortBy) {
                searchParams.set('sort_by', params.sortBy);
                searchParams.set('sort_order', params.sortOrder || 'desc');
            }

            const response = await fetch(`/api/v1/transcription/list?${searchParams}`, {
                headers: getAuthHeaders(),
            });

            if (!response.ok) {
                throw new Error('Failed to fetch audio files');
            }

            return response.json() as Promise<AudioFilesResponse>;
        },
        getNextPageParam: (lastPage) => {
            if (lastPage.pagination.page < lastPage.pagination.pages) {
                return lastPage.pagination.page + 1;
            }
            return undefined;
        },
        initialPageParam: 1,
        // Poll while a job is queued or processing, or an automatic summary
        // is pending, so status, title, brief and tags update without a
        // reload. Live events speed this up but can be missed (for example
        // after a page refresh mid-job).
        refetchInterval: (query) => {
            const busy = query.state.data?.pages.some(p => p.jobs.some(j =>
                j.summary_status || j.status === 'processing' || j.status === 'pending'));
            return busy ? 5000 : false;
        },
    });
}

export function useAudioUpload() {
    const { getAuthHeaders } = useAuth();
    const queryClient = useQueryClient();
    const { toast } = useToast();

    return useMutation({
        mutationFn: async ({ file, isVideo }: { file: File, isVideo: boolean }) => {
            const formData = new FormData();
            const fieldName = isVideo ? 'video' : 'audio';
            const endpoint = isVideo ? '/api/v1/transcription/upload-video' : '/api/v1/transcription/upload';

            formData.append(fieldName, file);
            formData.append('title', file.name.replace(/\.[^/.]+$/, ''));
            // Fallback recording time when the file has no date tag
            if (file.lastModified) formData.append('last_modified', String(file.lastModified));

            const response = await fetch(endpoint, {
                method: 'POST',
                headers: getAuthHeaders(),
                body: formData,
            });

            if (!response.ok) {
                throw new Error('Upload failed');
            }
            return response.json() as Promise<AudioFile>;
        },
        onSuccess: (job: AudioFile) => {
            queryClient.invalidateQueries({ queryKey: ['audioFiles'] });
            const dup = job?.duplicates?.[0];
            if (dup) {
                const more = job.duplicates!.length > 1 ? ` and ${job.duplicates!.length - 1} more` : '';
                toast({
                    title: 'This file was already uploaded',
                    description: `Same file as "${dup.title || dup.id}" (${new Date(dup.created_at).toLocaleDateString()})${more}. File Info on either recording shows the match.`,
                });
            }
        },
    });
}

export function useMultiTrackUpload() {
    const { getAuthHeaders } = useAuth();
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async ({ files, aupFile, title }: { files: File[], aupFile: File, title: string }) => {
            const formData = new FormData();
            formData.append('title', title);
            formData.append('aup', aupFile);

            files.forEach(file => {
                formData.append('tracks', file);
            });

            const response = await fetch("/api/v1/transcription/upload-multitrack", {
                method: "POST",
                headers: getAuthHeaders(),
                body: formData,
            });

            if (!response.ok) {
                const errorData = await response.json();
                throw new Error(errorData.error || 'Upload failed');
            }
            return response.json();
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['audioFiles'] });
        }
    });
}

export function useYouTubeDownload() {
    const { getAuthHeaders } = useAuth();
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async ({ url, title }: { url: string, title?: string }) => {
            const response = await fetch("/api/v1/transcription/youtube", {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                    ...getAuthHeaders(),
                },
                body: JSON.stringify({
                    url: url.trim(),
                    title: title?.trim() || undefined,
                }),
            });

            if (!response.ok) {
                const errorData = await response.json();
                throw new Error(errorData.error || "Failed to download YouTube audio");
            }
            return response.json();
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['audioFiles'] });
        },
    });
}

export interface Profile {
    id: string;
    name: string;
    description?: string;
    is_default: boolean;
}

export function useTranscriptionProfiles() {
    const { getAuthHeaders } = useAuth();
    return useQuery({
        queryKey: ['transcriptionProfiles'],
        queryFn: async () => {
            const response = await fetch("/api/v1/profiles/", {
                headers: getAuthHeaders(),
            });
            if (!response.ok) throw new Error('Failed to load profiles');
            return response.json() as Promise<Profile[]>;
        }
    });
}

export function useQuickTranscription() {
    const { getAuthHeaders } = useAuth();
    return useMutation({
        mutationFn: async ({ file, profileName }: { file: File, profileName?: string }) => {
            const formData = new FormData();
            formData.append("audio", file);
            if (profileName) formData.append("profile_name", profileName);

            const response = await fetch("/api/v1/transcription/quick", {
                method: "POST",
                headers: getAuthHeaders(),
                body: formData,
            });

            if (!response.ok) {
                const errorData = await response.json();
                throw new Error(errorData.error || "Failed to submit transcription");
            }
            return response.json();
        }
    });
}
