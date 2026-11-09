import { useState } from 'react'
import { Link, Outlet, useRouterState } from '@tanstack/react-router'
import { Bell, ChartColumn, ExternalLink, LayoutGrid, Package, ReceiptText, Search, SearchX, ShoppingBag } from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
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
import { RouteError, useSession } from 'tifa/client'
import { Account } from './account'
import { layout, type LayoutData } from './route.gen'
import './styles.css'

export const documentHead = {
  meta: [
    { title: 'Shanhai Workbench' },
    { name: 'theme-color', content: '#f2f4f5', media: '(prefers-color-scheme: light)' },
    { name: 'theme-color', content: '#0f141b', media: '(prefers-color-scheme: dark)' },
  ],
  links: [{ rel: 'icon', href: 'data:,' }],
}

type Project = LayoutData['projects'][number]

// where reads the shop, the page and the order or product a workbench URL is at.
function where(pathname: string) {
  const [, , project, page, item] = pathname.split('/')
  const name: keyof typeof pageNames =
    page === 'stats' ? 'stats' : page === 'products' ? (item ? 'product' : 'products') : page === 'orders' && item ? 'order' : 'orders'
  return { project, page: name, item }
}

export default function AppLayout() {
  const shell = layout.useLoaderData()
  // Who is signed in is not in the data of the Layout, which is the same for everyone. Without a
  // session, as in the open workbench of the comparison, the footer shows the owner.
  const { data: me } = useSession()
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const loading = useRouterState({ select: (state) => state.isLoading })

  const projects = shell.projects
  const at = where(pathname)
  const current = projects.find((p) => p.id === at.project)

  return (
    <TooltipProvider>
      <SidebarProvider defaultOpen={shell.sidebarOpen}>
        <a
          href="#main-content"
          className="sr-only z-50 bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
        >
          Skip to main content
        </a>
        <Sidebar collapsible="icon">
          <SidebarHeader className="border-b border-sidebar-border">
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton size="lg" asChild tooltip="Shanhai Workbench">
                  <Link to="/projects/$project" params={{ project: 'studio' }}>
                    <span className="flex aspect-square size-8 items-center justify-center bg-sidebar-primary font-heading text-lg text-sidebar-primary-foreground">
                      S
                    </span>
                    <span className="grid flex-1 text-left leading-tight">
                      <span className="font-heading text-base font-medium tracking-[0.06em]">Shanhai</span>
                      <span className="text-xs text-sidebar-foreground/60">Shop workbench</span>
                    </span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarHeader>
          <SidebarContent>
            <SidebarGroup>
              <SidebarGroupLabel className="text-sidebar-foreground/50">My shops</SidebarGroupLabel>
              <nav aria-label="Workspace navigation">
                <SidebarMenu>
                  {projects.map((project) => (
                    <SidebarMenuItem key={project.id}>
                      <SidebarMenuButton asChild isActive={project.id === at.project} tooltip={project.name} className={active}>
                        <Link to="/projects/$project" params={{ project: project.id }}>
                          {project.id === 'studio' ? <LayoutGrid /> : <ShoppingBag />}
                          <span>{project.name}</span>
                        </Link>
                      </SidebarMenuButton>
                      {project.pending > 0 && (
                        <SidebarMenuBadge className={pill}>
                          {project.pending}
                          <span className="sr-only"> orders to ship</span>
                        </SidebarMenuBadge>
                      )}
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </nav>
            </SidebarGroup>
            {current && (
              <SidebarGroup>
                <SidebarGroupLabel className="text-sidebar-foreground/50">{current.name}</SidebarGroupLabel>
                <nav aria-label="Shop pages">
                  <SidebarMenu>
                    <SidebarMenuItem>
                      <SidebarMenuButton asChild isActive={at.page === 'orders' || at.page === 'order'} tooltip="Orders" className={active}>
                        <Link to="/projects/$project" params={{ project: current.id }}>
                          <ReceiptText />
                          <span>Orders</span>
                        </Link>
                      </SidebarMenuButton>
                      {current.pending > 0 && <SidebarMenuBadge className={pill}>{current.pending}</SidebarMenuBadge>}
                    </SidebarMenuItem>
                    <SidebarMenuItem>
                      <SidebarMenuButton asChild isActive={at.page === 'stats'} tooltip="Stats" className={active}>
                        <Link to="/projects/$project/stats" params={{ project: current.id }}>
                          <ChartColumn />
                          <span>Stats</span>
                        </Link>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                    {current.id === 'studio' && (
                      // The studio shop sells the products of the storefront, and only it has a product list.
                      <SidebarMenuItem>
                        <SidebarMenuButton asChild isActive={at.page === 'products' || at.page === 'product'} tooltip="Products" className={active}>
                          <Link to="/projects/$project/products" params={{ project: current.id }}>
                            <Package />
                            <span>Products</span>
                          </Link>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    )}
                    <SidebarMenuItem>
                      <SidebarMenuButton asChild tooltip="Storefront">
                        <Link to="/store">
                          <ExternalLink />
                          <span>Storefront</span>
                        </Link>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  </SidebarMenu>
                </nav>
              </SidebarGroup>
            )}
          </SidebarContent>
          <SidebarFooter className="border-t border-sidebar-border">
            <div className="flex items-center gap-2 p-1">
              <Avatar className="size-8 rounded-none">
                <AvatarFallback className="rounded-none bg-sidebar-primary text-sidebar-primary-foreground">
                  {me?.initials ?? 'O'}
                </AvatarFallback>
              </Avatar>
              <div className="grid leading-tight group-data-[collapsible=icon]:hidden">
                <span className="text-sm font-medium">{me?.name ?? 'Owner'}</span>
                <span className="text-xs text-sidebar-foreground/60">Small shops, growing slowly.</span>
              </div>
            </div>
          </SidebarFooter>
          <SidebarRail />
        </Sidebar>
        <SidebarInset>
          <header className="sticky top-0 z-10 flex h-14 shrink-0 items-center gap-2 border-b bg-background/90 px-4 backdrop-blur md:px-6">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-1 data-vertical:h-4 data-vertical:self-center" />
            <Crumbs current={current} at={at} />
            <div className="ml-auto flex items-center gap-2">
              <CommandSearch projects={projects} />
              <Notifications projects={projects} />
              <Button variant="outline" size="sm" asChild className="hidden sm:inline-flex">
                <Link to="/store">
                  <ExternalLink />
                  View storefront
                </Link>
              </Button>
              <Account />
            </div>
            {loading && (
              <div role="status" aria-label="Loading page" className="absolute inset-x-0 bottom-0 h-0.5 animate-pulse bg-primary" />
            )}
          </header>
          <div id="main-content" tabIndex={-1} className="flex-1 px-4 py-6 outline-none md:px-8 md:py-8">
            <div className="mx-auto w-full max-w-7xl">
              <Outlet />
            </div>
          </div>
          <footer className="flex justify-between border-t px-4 py-4 font-mono text-[11px] text-muted-foreground md:px-8">
            <span>Shanhai Studio</span>
            <span>The orders here are demo data</span>
          </footer>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  )
}

const active = 'data-[active=true]:shadow-[inset_2px_0_0_var(--sidebar-primary)]'
const pill = 'rounded-none border border-sidebar-border font-mono text-[11px]'

const pageNames = { orders: 'Orders', stats: 'Stats', order: 'Order', products: 'Products', product: 'Products' }

function Crumbs({ current, at }: { current?: Project; at: ReturnType<typeof where> }) {
  return (
    <Breadcrumb className="hidden md:block">
      <BreadcrumbList>
        <BreadcrumbItem>
          <BreadcrumbLink asChild>
            <Link to="/projects/$project" params={{ project: 'studio' }}>
              Workbench
            </Link>
          </BreadcrumbLink>
        </BreadcrumbItem>
        {current && (
          <>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbLink asChild>
                <Link to="/projects/$project" params={{ project: current.id }}>
                  {current.name}
                </Link>
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              {at.page === 'order' ? (
                <BreadcrumbLink asChild>
                  <Link to="/projects/$project" params={{ project: current.id }}>
                    {pageNames.order}
                  </Link>
                </BreadcrumbLink>
              ) : at.page === 'product' ? (
                <BreadcrumbLink asChild>
                  <Link to="/projects/$project/products" params={{ project: current.id }}>
                    {pageNames.product}
                  </Link>
                </BreadcrumbLink>
              ) : (
                <BreadcrumbPage>{pageNames[at.page]}</BreadcrumbPage>
              )}
            </BreadcrumbItem>
            {(at.page === 'order' || at.page === 'product') && (
              <>
                <BreadcrumbSeparator />
                <BreadcrumbItem>
                  <BreadcrumbPage className="font-mono">{at.item}</BreadcrumbPage>
                </BreadcrumbItem>
              </>
            )}
          </>
        )}
      </BreadcrumbList>
    </Breadcrumb>
  )
}

// CommandSearch finds a page of the workbench by name, or an order by its number.
function CommandSearch({ projects }: { projects: Project[] }) {
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const pages = projects.flatMap((p) => [
    { key: `${p.id}-orders`, label: `${p.name} · Orders`, to: `/projects/${p.id}` },
    { key: `${p.id}-stats`, label: `${p.name} · Stats`, to: `/projects/${p.id}/stats` },
  ])
  const q = text.trim().toUpperCase()
  const order = /^(SH-\d{4}|WK-\d{4})$/.test(q) ? q : ''
  const shown = pages.filter((p) => p.label.toUpperCase().includes(q))
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" className="w-9 justify-start gap-2 px-2.5 text-muted-foreground sm:w-56 lg:w-72">
          <Search />
          <span className="hidden sm:inline">Search order numbers or pages</span>
          <kbd className="ml-auto hidden border px-1.5 font-mono text-[10px] lg:inline">⌘K</kbd>
        </Button>
      </DialogTrigger>
      <DialogContent className="gap-3 rounded-none p-0 sm:max-w-lg">
        <DialogHeader className="sr-only">
          <DialogTitle>Search the workbench</DialogTitle>
          <DialogDescription>Enter an order number, or the name of a shop or page.</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2 border-b px-3">
          <Search className="size-4 text-muted-foreground" />
          <Input
            autoFocus
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Order number, such as SH-1041"
            aria-label="Search order numbers or pages"
            className="h-11 border-0 px-0 shadow-none focus-visible:ring-0"
          />
        </div>
        <ul className="flex max-h-80 flex-col overflow-y-auto px-1.5 pb-2">
          {order && (
            <li>
              <a
                href={`/projects/${order.startsWith('SH') ? 'studio' : 'weekend'}/orders/${order}`}
                className="flex items-center gap-2 px-2.5 py-2 text-sm hover:bg-accent"
                onClick={() => setOpen(false)}
              >
                <ReceiptText className="size-4 text-muted-foreground" />
                Open order <span className="font-mono">{order}</span>
              </a>
            </li>
          )}
          {shown.map((p) => (
            <li key={p.key}>
              <a href={p.to} className="flex items-center gap-2 px-2.5 py-2 text-sm hover:bg-accent" onClick={() => setOpen(false)}>
                {p.key.endsWith('stats') ? <ChartColumn className="size-4 text-muted-foreground" /> : <LayoutGrid className="size-4 text-muted-foreground" />}
                {p.label}
              </a>
            </li>
          ))}
          {!order && shown.length === 0 && <li className="px-2.5 py-6 text-center text-sm text-muted-foreground">No matching pages</li>}
        </ul>
      </DialogContent>
    </Dialog>
  )
}

// Notifications lists the shops with orders waiting to be shipped.
function Notifications({ projects }: { projects: Project[] }) {
  const waiting = projects.filter((p) => p.pending > 0)
  const count = waiting.reduce((n, p) => n + p.pending, 0)
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" className="relative" aria-label={count > 0 ? `Notifications, ${count} to ship` : 'Notifications'}>
          <Bell />
          {count > 0 && <span className="absolute top-1.5 right-1.5 size-1.5 rounded-full bg-destructive" aria-hidden />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64 rounded-none">
        <DropdownMenuLabel>Notifications</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {waiting.length === 0 ? (
          <p className="px-2 py-3 text-sm text-muted-foreground">No orders waiting to ship</p>
        ) : (
          waiting.map((p) => (
            <DropdownMenuItem key={p.id} asChild>
              <Link to="/projects/$project" params={{ project: p.id }} className="flex justify-between">
                {p.name}
                <span className="font-mono text-xs text-muted-foreground">{p.pending} to ship</span>
              </Link>
            </DropdownMenuItem>
          ))
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// Error shows inside this Layout when a project does not exist or fails to load, and for a URL
// that matches no page.
export function Error({ error }: { error: unknown }) {
  const missing = error instanceof RouteError && error.status === 404
  return (
    <Empty className="min-h-[60vh]">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchX />
        </EmptyMedia>
        <EmptyTitle>{missing ? 'Page not found' : 'Something went wrong'}</EmptyTitle>
        <EmptyDescription>
          {missing ? 'The address may be wrong, or the shop is gone.' : error instanceof RouteError ? error.message : 'Please try again later.'}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" asChild>
          <a href="/projects/studio">Back to the workbench</a>
        </Button>
      </EmptyContent>
    </Empty>
  )
}
