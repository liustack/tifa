import Markdown, { type Components } from 'react-markdown'
import rehypeHighlight from 'rehype-highlight'
import remarkGfm from 'remark-gfm'
import 'highlight.js/styles/github.css'
import { cn } from 'cn'
import { Frame } from '../../frame'
import { route } from './+route'

export const head = () => ({ meta: [{ title: '长 Markdown' }] })

const components: Components = {
  h2: ({ node: _, ...props }) => <h2 className="mt-10 mb-4 font-heading text-2xl font-medium" {...props} />,
  h3: ({ node: _, ...props }) => <h3 className="mt-6 mb-2 font-semibold" {...props} />,
  p: ({ node: _, ...props }) => <p className="my-3 leading-7" {...props} />,
  a: ({ node: _, ...props }) => <a className="text-primary underline underline-offset-4" {...props} />,
  ul: ({ node: _, ...props }) => <ul className="my-3 list-disc pl-6" {...props} />,
  blockquote: ({ node: _, ...props }) => <blockquote className="my-4 border-l-4 border-primary/40 pl-4 text-muted-foreground" {...props} />,
  table: ({ node: _, ...props }) => <table className="my-4 w-full border text-sm" {...props} />,
  th: ({ node: _, ...props }) => <th className="border-b bg-muted px-3 py-1.5 text-left font-medium" {...props} />,
  td: ({ node: _, ...props }) => <td className="border-b px-3 py-1.5" {...props} />,
  pre: ({ node: _, ...props }) => <pre className="my-4 overflow-x-auto border bg-card p-4 text-[13px] leading-6" {...props} />,
  code: ({ node: _, className, ...props }) => (
    <code className={cn(className?.includes('hljs') ? 'font-mono' : 'bg-muted px-1 font-mono text-[0.9em]', className)} {...props} />
  ),
}

export default function MarkdownPage() {
  const doc = route.useLoaderData()
  return (
    <Frame title={doc.title} size={doc.kilobytes} unit="KB">
      <p className="text-sm text-muted-foreground">
        <span id="blocks">{doc.blocks}</span> 段代码
      </p>
      <article className="max-w-3xl text-[15px]">
        <Markdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]} components={components}>
          {doc.body}
        </Markdown>
      </article>
    </Frame>
  )
}
