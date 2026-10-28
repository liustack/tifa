import { useState } from 'react'
import Markdown, { type Components } from 'react-markdown'
import rehypeHighlight from 'rehype-highlight'
import remarkGfm from 'remark-gfm'
import 'highlight.js/styles/github.css'
import { CircleDot, GitCommitHorizontal, Lock, MessageSquare, MoreHorizontal, Pencil, SmilePlus, Tag, UserPlus, XCircle } from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from 'cn'
import { useFormats, useSession } from './providers'
import { route, type Data } from './+route'

type Event = NonNullable<Data['ticket']['events']>[number]

const markdown: Components = {
  p: ({ node: _, ...props }) => <p className="my-2 leading-6" {...props} />,
  ul: ({ node: _, ...props }) => <ul className="my-2 list-disc pl-5" {...props} />,
  blockquote: ({ node: _, ...props }) => <blockquote className="my-2 border-l-2 pl-3 text-muted-foreground" {...props} />,
  table: ({ node: _, ...props }) => <table className="my-2 border text-xs" {...props} />,
  th: ({ node: _, ...props }) => <th className="border-b bg-muted px-2 py-1 text-left font-medium" {...props} />,
  td: ({ node: _, ...props }) => <td className="border-b px-2 py-1" {...props} />,
  pre: ({ node: _, ...props }) => <pre className="my-2 overflow-x-auto border bg-background p-3 text-xs leading-5" {...props} />,
  code: ({ node: _, className, ...props }) => (
    <code className={cn(className?.includes('hljs') ? 'font-mono' : 'bg-muted px-1 font-mono text-[0.9em]', className)} {...props} />
  ),
}

export function Thread() {
  const data = route.useLoaderData()
  const ticket = data.ticket
  const f = useFormats()
  const events = ticket.events ?? []
  const comments = events.filter((e) => e.kind === 'comment').length
  return (
    <section aria-labelledby="ticket-title" className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_260px]">
      <div className="flex flex-col gap-4">
        <header className="flex flex-col gap-2 border-b pb-4">
          <h2 id="ticket-title" className="font-heading text-2xl font-medium">
            {ticket.title} <span className="font-mono text-lg text-muted-foreground">#{ticket.number}</span>
          </h2>
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Badge className="rounded-none bg-[#2f6b3f] text-white">
              <CircleDot />
              处理中
            </Badge>
            {ticket.author} 于 {f.ago(ticket.opened)} 创建 · <span id="comments">{comments}</span> 条回复
          </p>
        </header>
        <ol className="relative flex flex-col gap-4 before:absolute before:top-0 before:bottom-0 before:left-4 before:w-px before:bg-border">
          {events.map((e) => (e.kind === 'comment' ? <Comment key={e.id} event={e} /> : <TimelineItem key={e.id} event={e} />))}
        </ol>
        <Composer />
      </div>
      <aside className="flex flex-col gap-5 text-sm">
        <SideBlock title="负责人">
          {(ticket.assignees ?? []).map((a) => (
            <p key={a} className="flex items-center gap-2">
              <Avatar className="size-5 rounded-none">
                <AvatarFallback className="rounded-none text-[10px]">{a.slice(0, 1)}</AvatarFallback>
              </Avatar>
              {a}
            </p>
          ))}
        </SideBlock>
        <SideBlock title="标签">
          <span className="flex flex-wrap gap-1.5">
            {(ticket.labels ?? []).map((l) => (
              <Badge key={l.name} variant="outline" className="rounded-none" style={{ borderColor: l.color, color: l.color }}>
                {l.name}
              </Badge>
            ))}
          </span>
        </SideBlock>
        <SideBlock title="里程碑">{ticket.milestone}</SideBlock>
        <SideBlock title={`参与者 ${(ticket.participants ?? []).length}`}>
          <span className="flex flex-wrap gap-1">
            {(ticket.participants ?? []).map((p, i) => (
              <Tooltip key={i}>
                <TooltipTrigger asChild>
                  <Avatar className="size-6 rounded-none">
                    <AvatarFallback className="rounded-none text-[10px]">{p.slice(0, 1)}</AvatarFallback>
                  </Avatar>
                </TooltipTrigger>
                <TooltipContent>{p}</TooltipContent>
              </Tooltip>
            ))}
          </span>
        </SideBlock>
        <SideBlock title="操作记录">
          <ol className="flex flex-col gap-1.5 text-xs">
            {(data.audit ?? []).map((a, i) => (
              <li key={i}>
                <span className="font-medium">{a.actor}</span> {a.action} <span className="font-mono">{a.target}</span>
                <span className="block text-muted-foreground">{f.ago(a.at)}</span>
              </li>
            ))}
          </ol>
        </SideBlock>
        <Button variant="outline" size="sm">
          <Lock />
          锁定会话
        </Button>
      </aside>
    </section>
  )
}

function SideBlock({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-2 border-b pb-4">
      <h3 className="font-mono text-[11px] tracking-[0.12em] text-muted-foreground">{title}</h3>
      {children}
    </div>
  )
}

function Comment({ event }: { event: Event }) {
  const f = useFormats()
  const session = useSession()
  return (
    <li className="relative flex gap-3">
      <Avatar className="z-10 size-8 rounded-none">
        <AvatarFallback className="rounded-none text-xs">{event.author.slice(0, 1)}</AvatarFallback>
      </Avatar>
      <article className="flex-1 border bg-card">
        <header className="flex items-center gap-2 border-b bg-muted/40 px-3 py-1.5 text-xs">
          <span className="font-medium">{event.author}</span>
          <span className="text-muted-foreground">@{event.login}</span>
          <Tooltip>
            <TooltipTrigger asChild>
              <a href={`#${event.id}`} className="text-muted-foreground hover:underline">
                {f.ago(event.at)}
              </a>
            </TooltipTrigger>
            <TooltipContent>{f.time(event.at)}</TooltipContent>
          </Tooltip>
          {event.edited && <span className="text-muted-foreground">已编辑</span>}
          {event.login === session.login && <Badge variant="outline" className="rounded-none text-[10px]">我</Badge>}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon-xs" className="ml-auto" aria-label="回复的操作">
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem>复制链接</DropdownMenuItem>
              <DropdownMenuItem>引用回复</DropdownMenuItem>
              <DropdownMenuItem>
                <Pencil />
                编辑
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </header>
        <div className="px-3 text-[13px]">
          <Markdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]} components={markdown}>
            {event.body}
          </Markdown>
        </div>
        <Reactions reactions={event.reactions ?? []} />
      </article>
    </li>
  )
}

function Reactions({ reactions }: { reactions: NonNullable<Event['reactions']> }) {
  const [mine, setMine] = useState<string | null>(null)
  return (
    <footer className="flex flex-wrap items-center gap-1.5 px-3 pb-2">
      {reactions.map((r) => (
        <Button key={r.emoji} variant="outline" size="xs" aria-pressed={mine === r.emoji} onClick={() => setMine(mine === r.emoji ? null : r.emoji)} className="rounded-none">
          {r.emoji}
          <span className="font-mono">{r.count + (mine === r.emoji ? 1 : 0)}</span>
        </Button>
      ))}
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="ghost" size="icon-xs" aria-label="添加表情">
            <SmilePlus />
          </Button>
        </TooltipTrigger>
        <TooltipContent>添加表情</TooltipContent>
      </Tooltip>
    </footer>
  )
}

const eventIcons = { labeled: Tag, assigned: UserPlus, referenced: GitCommitHorizontal, closed: XCircle, reopened: CircleDot }

function TimelineItem({ event }: { event: Event }) {
  const f = useFormats()
  const Icon = eventIcons[event.kind as keyof typeof eventIcons] ?? MessageSquare
  return (
    <li className="relative flex items-center gap-3 pl-1.5 text-xs text-muted-foreground">
      <span className="z-10 flex size-6 items-center justify-center rounded-full border bg-background">
        <Icon className="size-3" />
      </span>
      <span>
        <span className="font-medium text-foreground">{event.author}</span> {event.body} · {f.ago(event.at)}
      </span>
    </li>
  )
}

function Composer() {
  const [text, setText] = useState('')
  return (
    <form className="flex flex-col gap-2 border bg-card p-3" onSubmit={(e) => e.preventDefault()}>
      <label htmlFor="reply" className="text-xs text-muted-foreground">
        回复（支持 Markdown）
      </label>
      <textarea id="reply" value={text} onChange={(e) => setText(e.target.value)} rows={4} className="w-full resize-y border bg-background p-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring" />
      <div className="flex justify-end gap-2">
        <Button variant="outline" size="sm" type="button">
          关闭工单
        </Button>
        <Button size="sm" type="submit" disabled={text.trim() === ''}>
          回复
        </Button>
      </div>
    </form>
  )
}
