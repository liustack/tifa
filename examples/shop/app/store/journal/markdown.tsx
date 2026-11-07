import Markdown, { type Components } from 'react-markdown'
import rehypeHighlight from 'rehype-highlight'
import remarkGfm from 'remark-gfm'
import 'highlight.js/styles/github.css'
import { cn } from 'cn'

// Each Markdown element gets the shop's type styles. Code blocks are highlighted by
// highlight.js with the languages lowlight bundles as "common".
const components: Components = {
  h2: ({ node: _, ...props }) => <h2 className="mt-10 mb-4 scroll-mt-20 font-heading text-2xl font-medium" {...props} />,
  h3: ({ node: _, ...props }) => <h3 className="mt-8 mb-3 font-semibold" {...props} />,
  p: ({ node: _, ...props }) => <p className="my-4 leading-7" {...props} />,
  a: ({ node: _, ...props }) => <a className="text-primary underline underline-offset-4" {...props} />,
  ul: ({ node: _, ...props }) => <ul className="my-4 list-disc pl-6 [&>li]:mt-2" {...props} />,
  ol: ({ node: _, ...props }) => <ol className="my-4 list-decimal pl-6 [&>li]:mt-2" {...props} />,
  blockquote: ({ node: _, ...props }) => (
    <blockquote className="my-6 border-l-4 border-primary/40 bg-secondary/50 py-2 pl-4 text-muted-foreground" {...props} />
  ),
  table: ({ node: _, ...props }) => (
    <div className="my-6 overflow-x-auto border">
      <table className="w-full text-sm" {...props} />
    </div>
  ),
  th: ({ node: _, ...props }) => <th className="border-b bg-muted px-3 py-2 text-left font-medium" {...props} />,
  td: ({ node: _, ...props }) => <td className="border-b px-3 py-2 align-top" {...props} />,
  pre: ({ node: _, ...props }) => <pre className="my-6 overflow-x-auto border bg-card p-4 text-[13px] leading-6" {...props} />,
  code: ({ node: _, className, ...props }) => (
    <code className={cn(className?.includes('hljs') ? 'font-mono' : 'rounded bg-muted px-1.5 py-0.5 font-mono text-[0.9em]', className)} {...props} />
  ),
}

export function Article({ body }: { body: string }) {
  return (
    <div className="text-[15px]">
      <Markdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]} components={components}>
        {body}
      </Markdown>
    </div>
  )
}
