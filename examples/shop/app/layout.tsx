import { Link, Outlet, useRouterState } from '@tanstack/react-router'
import { LayoutGrid, SearchX, ShoppingBag } from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Separator } from '@/components/ui/separator'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
} from '@/components/ui/sidebar'
import { TooltipProvider } from '@/components/ui/tooltip'
import { route } from './+layout'
import './styles.css'

export const htmlAttributes = { lang: 'zh-CN' }

export const documentHead = {
  meta: [{ title: '山海工作台' }, { name: 'theme-color', content: '#f7f8f5' }],
  links: [{ rel: 'icon', href: 'data:,' }],
}

export default function AppLayout() {
  const shell = route.useLoaderData()
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const loading = useRouterState({ select: (state) => state.isLoading })

  // The storefront is for customers: its Layout draws its own header and footer, without the
  // workbench around it, and layout.go gives its pages no workbench data.
  if (pathname.startsWith('/store/')) return <Outlet />

  return (
    <TooltipProvider>
      <SidebarProvider defaultOpen={shell.sidebarOpen}>
        <a
          href="#main-content"
          className="sr-only z-50 rounded-md bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
        >
          跳至主要内容
        </a>
        <Sidebar collapsible="icon">
          <SidebarHeader>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton size="lg" asChild tooltip="山海工作台">
                  <Link to="/projects/$project" params={{ project: 'studio' }}>
                    <span className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary font-serif text-lg text-sidebar-primary-foreground">
                      山
                    </span>
                    <span className="grid flex-1 text-left leading-tight">
                      <span className="font-serif text-base font-semibold">山海</span>
                      <span className="text-xs text-muted-foreground">店铺工作台</span>
                    </span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarHeader>
          <SidebarContent>
            <SidebarGroup>
              <SidebarGroupLabel>我的店铺</SidebarGroupLabel>
              <nav aria-label="工作区导航">
                <SidebarMenu>
                  {(shell.projects ?? []).map((project) => (
                    <SidebarMenuItem key={project.id}>
                      <SidebarMenuButton
                        asChild
                        isActive={pathname.startsWith(`/projects/${project.id}`)}
                        tooltip={project.name}
                      >
                        <Link to="/projects/$project" params={{ project: project.id }}>
                          {project.id === 'studio' ? <LayoutGrid /> : <ShoppingBag />}
                          <span>{project.name}</span>
                        </Link>
                      </SidebarMenuButton>
                      {project.pending > 0 && (
                        <SidebarMenuBadge>
                          {project.pending}
                          <span className="sr-only"> 单待发货</span>
                        </SidebarMenuBadge>
                      )}
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </nav>
            </SidebarGroup>
          </SidebarContent>
          <SidebarFooter>
            <div className="flex items-center gap-2 p-1">
              <Avatar className="size-8">
                <AvatarFallback className="bg-secondary text-secondary-foreground">
                  {shell.viewer.initials}
                </AvatarFallback>
              </Avatar>
              <div className="grid leading-tight group-data-[collapsible=icon]:hidden">
                <span className="text-sm font-medium">{shell.viewer.name}</span>
                <span className="text-xs text-muted-foreground">让小生意，慢慢生长。</span>
              </div>
            </div>
          </SidebarFooter>
          <SidebarRail />
        </Sidebar>
        <SidebarInset>
          <header className="sticky top-0 z-10 flex h-14 shrink-0 items-center gap-2 border-b bg-background/90 px-4 backdrop-blur md:px-6">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-1 data-vertical:h-4 data-vertical:self-center" />
            <span className="text-sm text-muted-foreground">工作台</span>
            <Badge variant="secondary" className="ml-auto">
              演示工作区
            </Badge>
            {loading && (
              <div
                role="status"
                aria-label="正在加载页面"
                className="absolute inset-x-0 bottom-0 h-0.5 animate-pulse bg-primary"
              />
            )}
          </header>
          <div id="main-content" tabIndex={-1} className="flex-1 px-4 py-6 outline-none md:px-8 md:py-8">
            <div className="mx-auto w-full max-w-6xl">
              <Outlet />
            </div>
          </div>
          <footer className="flex justify-between border-t px-4 py-4 text-xs text-muted-foreground md:px-8">
            <span>山海工作室</span>
            <span>这里的订单均为演示数据</span>
          </footer>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  )
}

// NotFound shows inside this Layout when a project does not exist, and on its own for a URL
// that matches no page, so it reads no loader data.
export function NotFound() {
  return (
    <Empty className="min-h-[60vh]">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchX />
        </EmptyMedia>
        <EmptyTitle>没有找到这个页面</EmptyTitle>
        <EmptyDescription>地址可能有误，或者这家店铺已经不在了。</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" asChild>
          <a href="/projects/studio">返回工作台</a>
        </Button>
      </EmptyContent>
    </Empty>
  )
}
