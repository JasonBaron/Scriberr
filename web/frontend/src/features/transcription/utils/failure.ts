// failureReason shortens a failed job's error (which carries the whole job
// log) to the error itself, e.g. "ModuleNotFoundError: No module named ...".
export function failureReason(msg?: string | null): string {
  if (!msg) return "";
  const lines = msg.split("\n").map(l => l.trim());
  const marked = lines.find(l => l.startsWith("Error       :"));
  if (marked) return marked.replace(/^Error\s+:\s*/, "");
  for (let i = lines.length - 1; i >= 0; i--) {
    const m = /^([A-Za-z_.]*(Error|Exception)):\s/.exec(lines[i]);
    if (m) return lines[i];
  }
  return (lines[0] || "").replace(/\s*Logs:.*$/, "");
}

// retryJobs queues failed recordings again with their saved settings: the
// given ones, or every failed recording when ids is empty.
export async function retryJobs(headers: Record<string, string>, ids: string[] = []): Promise<{ queued: number; skipped: number; problems?: string[] }> {
  const res = ids.length === 1
    ? await fetch(`/api/v1/transcription/${ids[0]}/retry`, { method: "POST", headers })
    : await fetch("/api/v1/transcription/retry-failed", {
        method: "POST",
        headers: { "Content-Type": "application/json", ...headers },
        body: JSON.stringify({ job_ids: ids }),
      });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data?.error || "Retry failed");
  return { queued: data.queued ?? 0, skipped: data.skipped ?? 0, problems: data.problems };
}
