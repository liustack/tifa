import { SidebarProvider } from '@/components/ui/sidebar'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { TooltipProvider } from '@/components/ui/tooltip'
import { Orders } from './orders'
import { Charts, Kpis } from './overview'
import { AppProviders } from './providers'
import { AppSidebar, Page } from './shell'
import { Thread } from './thread'
import { route } from './+route'

export const head = () => ({ meta: [{ title: '今日看板 · 山海工作室后台' }] })

export default function ConsolePage() {
  const data = route.useLoaderData()
  const unread = (data.notices ?? []).filter((n) => n.unread).length
  return (
    <AppProviders session={{ user: data.user, login: data.login, shop: data.shop, role: 'owner' }} timeZone={data.timeZone} today={data.today} unread={unread}>
      <TooltipProvider>
        <SidebarProvider>
          <AppSidebar />
          <Page>
            <div className="flex flex-col gap-1">
              <p className="font-mono text-[11px] tracking-[0.14em] text-muted-foreground">{data.today}</p>
              <h1 className="font-heading text-3xl font-medium tracking-[0.04em]">今日看板</h1>
            </div>
            <Kpis />
            <Charts />
            <Tabs defaultValue="orders">
              <TabsList variant="line">
                <TabsTrigger value="orders">订单</TabsTrigger>
                <TabsTrigger value="ticket">工单 #{data.ticket.number}</TabsTrigger>
              </TabsList>
              <TabsContent value="orders" forceMount className="pt-4 data-[state=inactive]:hidden">
                <Orders />
              </TabsContent>
              <TabsContent value="ticket" forceMount className="pt-4 data-[state=inactive]:hidden">
                <Thread />
              </TabsContent>
            </Tabs>
          </Page>
        </SidebarProvider>
      </TooltipProvider>
    </AppProviders>
  )
}
