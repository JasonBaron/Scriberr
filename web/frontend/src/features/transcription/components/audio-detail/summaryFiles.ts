// markdownToText strips common Markdown syntax for a plain-text download.
export function markdownToText(md: string): string {
    return md
        .replace(/<think>[\s\S]*?<\/think>/gi, '')
        .replace(/^#{1,6}\s+/gm, '')
        .replace(/\*\*(.+?)\*\*/g, '$1')
        .replace(/__(.+?)__/g, '$1')
        .replace(/(^|[^*])\*(?!\s)(.+?)\*/g, '$1$2')
        .replace(/`{1,3}([^`]*)`{1,3}/g, '$1')
        .replace(/\[(.+?)\]\((.+?)\)/g, '$1 ($2)')
        .replace(/^\s*[-*+]\s+/gm, '- ')
        .replace(/\n{3,}/g, '\n\n')
        .trim() + '\n';
}

// downloadText saves text as a file in the browser.
export function downloadText(content: string, filename: string, type: string) {
    const blob = new Blob([content], { type });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
}

// summaryFilename builds "<title>-summary.<ext>" with a safe title.
export function summaryFilename(title: string | undefined, ext: string): string {
    const base = (title || '').replace(/[^a-z0-9]+/gi, '_').replace(/^_+|_+$/g, '').toLowerCase();
    return base ? `${base}-summary.${ext}` : `summary.${ext}`;
}
