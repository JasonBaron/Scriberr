import ReactMarkdown from 'react-markdown';
import remarkMath from 'remark-math';
import rehypeRaw from 'rehype-raw';
import rehypeKatex from 'rehype-katex';
import rehypeHighlight from 'rehype-highlight';

// SummaryMarkdown renders summary text with the reading styles used by the
// Summary dialog and the summary panel on the job page.
export function SummaryMarkdown({ content }: { content: string }) {
    return (
        <ReactMarkdown
            remarkPlugins={[remarkMath]}
            rehypePlugins={[rehypeRaw as any, rehypeKatex as any, rehypeHighlight as any]} // eslint-disable-line @typescript-eslint/no-explicit-any
            components={{
                p: ({ ...props }) => <p className="text-[#525252] dark:text-[#A3A3A3] leading-7 mb-4" {...props} />,
                h1: ({ ...props }) => <h1 className="text-[#171717] dark:text-[#EDEDED] font-bold text-2xl mt-6 mb-4" {...props} />,
                h2: ({ ...props }) => <h2 className="text-[#171717] dark:text-[#EDEDED] font-bold text-xl mt-6 mb-3" {...props} />,
                h3: ({ ...props }) => <h3 className="text-[#171717] dark:text-[#EDEDED] font-bold text-lg mt-5 mb-2" {...props} />,
                li: ({ ...props }) => <li className="pl-1 text-[#525252] dark:text-[#A3A3A3] mb-1" {...props} />,
                strong: ({ ...props }) => <strong className="text-[#171717] dark:text-[#EDEDED] font-bold" {...props} />,
                ul: ({ ...props }) => <ul className="list-disc pl-5 mb-4" {...props} />,
                ol: ({ ...props }) => <ol className="list-decimal pl-5 mb-4" {...props} />,
            }}
        >
            {content}
        </ReactMarkdown>
    );
}
