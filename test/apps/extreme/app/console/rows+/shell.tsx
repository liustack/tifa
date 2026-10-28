import type { ReactNode } from 'react'
import {
  Activity,
  Bell,
  Box,
  ChartColumn,
  ChevronRight,
  Crown,
  File,
  Gauge,
  Inbox,
  Megaphone,
  Moon,
  Package,
  PenLine,
  Plug,
  ReceiptText,
  Search,
  Shield,
  Star,
  Store,
  Tags,
  Ticket,
  Undo2,
  Users,
  Warehouse,
  type LucideIcon,
} from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Separator } from '@/components/ui/separator'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  SidebarTrigger,
} from '@/components/ui/sidebar'
import { useCommand, useFormats, useSession, useTheme, useUnread } from './providers'
import { route } from './+route'

const icons: Record<string, LucideIcon> = {
  gauge: Gauge,
  activity: Activity,
  chart: ChartColumn,
  receipt: ReceiptText,
  package: Package,
  undo: Undo2,
  file: File,
  box: Box,
  warehouse: Warehouse,
  tags: Tags,
  star: Star,
  users: Users,
  crown: Crown,
  inbox: Inbox,
  ticket: Ticket,
  megaphone: Megaphone,
  pen: PenLine,
  store: Store,
  shield: Shield,
  plug: Plug,
}

const active = 'data-[active=true]:shadow-[inset_2px_0_0_var(--sidebar-primary)]'
const pill = 'rounded-none border border-sidebar-border font-mono text-[11px]'

export function AppSidebar() {
  const data = route.useLoaderData()
  const session = useSession()
  return (
    <Sidebar collapsible="icon">
      <SidebarHeader className="border-b border-sidebar-border">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" tooltip={session.shop}>
              <span className="flex aspect-square size-8 items-center justify-center bg-sidebar-primary font-heading text-lg text-sidebar-primary-foreground">山</span>
              <span className="grid flex-1 text-left leading-tight">
                <span className="font-heading text-base font-medium tracking-[0.06em]">{session.shop}</span>
                <span className="text-xs text-sidebar-foreground/60">店铺后台</span>
              </span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        {(data.nav ?? []).map((group, g) => (
          <Collapsible key={group.title} defaultOpen className="group/collapsible">
            <SidebarGroup>
              <SidebarGroupLabel asChild className="text-sidebar-foreground/50">
                <CollapsibleTrigger>
                  {group.title}
                  <ChevronRight className="ml-auto transition-transform group-data-[state=open]/collapsible:rotate-90" />
                </CollapsibleTrigger>
              </SidebarGroupLabel>
              <CollapsibleContent>
                <SidebarGroupContent>
                  <SidebarMenu>
                    {(group.items ?? []).map((item, i) => {
                      const Icon = icons[item.icon] ?? Box
                      return (
                        <SidebarMenuItem key={item.label}>
                          <SidebarMenuButton asChild tooltip={item.label} isActive={g === 1 && i === 0} className={active}>
                            <a href={`#nav-${g}-${i}`}>
                              <Icon />
                              <span>{item.label}</span>
                            </a>
                          </SidebarMenuButton>
                          {item.count > 0 && <SidebarMenuBadge className={pill}>{item.count}</SidebarMenuBadge>}
                        </SidebarMenuItem>
                      )
                    })}
                  </SidebarMenu>
                </SidebarGroupContent>
              </CollapsibleContent>
            </SidebarGroup>
          </Collapsible>
        ))}
      </SidebarContent>
      <SidebarFooter className="border-t border-sidebar-border">
        <div className="flex items-center gap-2 p-1">
          <Avatar className="size-8 rounded-none">
            <AvatarFallback className="rounded-none bg-sidebar-primary text-sidebar-primary-foreground">{session.user.slice(0, 1)}</AvatarFallback>
          </Avatar>
          <div className="grid leading-tight group-data-[collapsible=icon]:hidden">
            <span className="text-sm font-medium">{session.user}</span>
            <span className="text-xs text-sidebar-foreground/60">@{session.login}</span>
          </div>
        </div>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}

export function Topbar() {
  const { setOpen } = useCommand()
  const theme = useTheme()
  return (
    <header className="sticky top-0 z-10 flex h-14 shrink-0 items-center gap-2 border-b bg-background/90 px-4 backdrop-blur md:px-6">
      <SidebarTrigger className="-ml-1" />
      <Separator orientation="vertical" className="mr-1 data-vertical:h-4 data-vertical:self-center" />
      <Breadcrumb className="hidden md:block">
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink href="#home">后台</BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbPage>今日看板</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
      <div className="ml-auto flex items-center gap-2">
        <Button variant="outline" size="sm" className="w-56 justify-start text-muted-foreground" onClick={() => setOpen(true)}>
          <Search />
          搜索订单、顾客、商品
          <kbd className="ml-auto font-mono text-[10px]">⌘K</kbd>
        </Button>
        <Button variant="ghost" size="icon" aria-label="切换深色模式" onClick={() => theme.setMode(theme.mode === 'dark' ? 'light' : 'dark')}>
          <Moon />
        </Button>
        <Notifications />
      </div>
    </header>
  )
}

function Notifications() {
  const data = route.useLoaderData()
  const { unread, markRead } = useUnread()
  const f = useFormats()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={`通知，${unread} 条未读`} className="relative">
          <Bell />
          {unread > 0 && <span className="absolute top-1 right-1 flex size-4 items-center justify-center bg-destructive font-mono text-[9px] text-white">{unread}</span>}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-80">
        <DropdownMenuLabel className="flex items-center justify-between">
          通知
          <button type="button" onClick={markRead} className="text-xs text-primary">
            全部已读
          </button>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {(data.notices ?? []).map((n, i) => (
          <DropdownMenuItem key={i} className="flex flex-col items-start gap-0.5">
            <span className="text-sm">{n.title}</span>
            <span className="text-xs text-muted-foreground">{n.body}</span>
            <span className="font-mono text-[10px] text-muted-foreground">{f.ago(n.at)}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function Page({ children }: { children: ReactNode }) {
  return (
    <SidebarInset>
      <Topbar />
      <div id="main-content" className="flex-1 px-4 py-6 md:px-8">
        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-8">{children}</div>
      </div>
      <footer className="flex justify-between border-t px-4 py-4 font-mono text-[11px] text-muted-foreground md:px-8">
        <span>山海工作室</span>
        <span>演示数据</span>
      </footer>
    </SidebarInset>
  )
}
