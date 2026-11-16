import { Accordion, AspectRatio, Collapsible, Dialog, DropdownMenu, HoverCard, NavigationMenu, Popover, RadioGroup, ScrollArea, Tabs, Toggle, ToggleGroup, Tooltip as TooltipP } from 'radix-ui'
import { DayPicker } from 'react-day-picker'
import { Command } from 'cmdk'
import {
  Activity, ArrowUpDown, Bell, Calendar as CalIcon, Check, ChevronDown, ChevronRight, CircleAlert, CreditCard, Ellipsis, FileText, Folder, House, Inbox, LogOut, Mail,
  Search, Settings, Star, Trash2, TrendingDown, TrendingUp, User, Users, type LucideIcon,
} from 'lucide-react'
import { page, type PageData } from './route.gen'
import { Alert, Avatar, Badge, Button, buttonVariants, Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle, Checkbox, cn, Input, Label, Progress, Select, Separator, Skeleton, Slider, Switch, Textarea, toggleVariants, Tooltip } from './ui'

type Row = PageData['rows'][number]

export const head = () => ({ meta: [{ title: 'shadcn/ui · Library pages' }] })

const STATS: [string, number, number, string][] = [
  ['Total Revenue', 1250, 12.5, 'Trending up this month'],
  ['New Customers', 1234, -20, 'Down 20% this period'],
  ['Active Accounts', 45678, 12.5, 'Strong user retention'],
  ['Growth Rate', 4.5, 4.5, 'Steady performance increase'],
]
const NAV: [string, LucideIcon][] = [['Dashboard', House], ['Lifecycle', Activity], ['Analytics', TrendingUp], ['Projects', Folder], ['Team', Users], ['Inbox', Inbox]]

function money(v: number) {
  return '$' + v.toFixed(2).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}

function StatusBadge({ status }: { status: string }) {
  const v = status === 'Done' ? 'secondary' : status === 'Blocked' ? 'destructive' : 'outline'
  return (
    <Badge variant={v} className={cn('text-muted-foreground px-1.5', status === 'Done' && 'text-green-700', status === 'Blocked' && 'text-white')}>
      {status === 'Done' ? <Check className="fill-green-500" /> : status === 'Blocked' ? <CircleAlert /> : <Activity />}
      {status}
    </Badge>
  )
}

function RowMenu({ row }: { row: Row }) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <Button variant="ghost" size="icon" className="data-[state=open]:bg-muted text-muted-foreground flex size-8" aria-label={'Open menu for ' + row.name}>
          <Ellipsis />
        </Button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content align="end" className="w-32">
          <DropdownMenu.Item>Edit</DropdownMenu.Item>
          <DropdownMenu.Item>Make a copy</DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Item>Delete</DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}

function DataTable({ rows }: { rows: Row[] }) {
  return (
    <div className="overflow-hidden rounded-lg border">
      <table data-slot="table" className="w-full caption-bottom text-sm">
        <thead data-slot="table-header" className="bg-muted sticky top-0 z-10 [&_tr]:border-b">
          <tr data-slot="table-row" className="hover:bg-muted/50 data-[state=selected]:bg-muted border-b transition-colors">
            <th className="h-10 px-2 text-left align-middle font-medium whitespace-nowrap [&:has([role=checkbox])]:pr-0">
              <Checkbox aria-label="Select all" />
            </th>
            {['Header', 'Section Type', 'Status', 'Target', 'Limit', 'Reviewer', 'Progress', ''].map((h, i) => (
              <th key={i} className={cn('text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap', i === 3 || i === 4 ? 'text-right' : '')}>
                {h && (
                  <Button variant="ghost" size="sm" className="-ml-3 h-8">
                    {h}
                    {i < 3 && <ArrowUpDown />}
                  </Button>
                )}
              </th>
            ))}
          </tr>
        </thead>
        <tbody data-slot="table-body" className="[&_tr:last-child]:border-0">
          {rows.map((r) => (
            <tr key={r.id} data-slot="table-row" data-state={r.id % 7 === 0 ? 'selected' : undefined} className="hover:bg-muted/50 data-[state=selected]:bg-muted border-b transition-colors">
              <td className="p-2 align-middle whitespace-nowrap">
                <Checkbox defaultChecked={r.id % 7 === 0} aria-label="Select row" />
              </td>
              <td className="p-2 align-middle">
                <HoverCard.Root>
                  <HoverCard.Trigger asChild>
                    <Button variant="link" className="text-foreground w-fit px-0 text-left">
                      {r.name}
                    </Button>
                  </HoverCard.Trigger>
                </HoverCard.Root>
              </td>
              <td className="p-2 align-middle">
                <Badge variant="outline" className="text-muted-foreground px-1.5">
                  {r.type}
                </Badge>
              </td>
              <td className="p-2 align-middle">
                <StatusBadge status={r.status} />
              </td>
              <td className="p-2 text-right align-middle">
                <Label htmlFor={r.id + '-target'} className="sr-only">
                  Target
                </Label>
                <Input className="hover:bg-input/30 h-8 w-16 border-transparent bg-transparent text-right shadow-none" defaultValue={r.target} id={r.id + '-target'} />
              </td>
              <td className="p-2 text-right align-middle">
                <Input className="h-8 w-16 border-transparent bg-transparent text-right shadow-none" defaultValue={r.limit} />
              </td>
              <td className="p-2 align-middle">{r.reviewer || <Select items={['Eddie Lake', 'Jamik Tashpulatov']} placeholder="Assign reviewer" className="w-38" />}</td>
              <td className="p-2 align-middle">
                <Progress value={r.done} className="w-24" />
              </td>
              <td className="p-2 align-middle">
                <RowMenu row={r} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function SettingsCard({ bio }: { bio: string }) {
  return (
    <Tabs.Root defaultValue="account" className="flex flex-col gap-2 w-[420px]">
      <Tabs.List className="bg-muted text-muted-foreground inline-flex h-9 w-fit items-center justify-center rounded-lg p-[3px]">
        {['account', 'password', 'notifications'].map((t) => (
          <Tabs.Trigger key={t} value={t} className="data-[state=active]:bg-background inline-flex h-[calc(100%-1px)] flex-1 items-center justify-center gap-1.5 rounded-md border border-transparent px-2 py-1 text-sm font-medium whitespace-nowrap capitalize">
            {t}
          </Tabs.Trigger>
        ))}
      </Tabs.List>
      <Tabs.Content value="account" className="flex-1 outline-none">
        <Card>
          <CardHeader>
            <CardTitle>Account</CardTitle>
            <CardDescription>Make changes to your account here. Click save when you are done.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-6">
            {['Name', 'Username', 'Email', 'Website'].map((f) => (
              <div key={f} className="grid gap-3">
                <Label htmlFor={'acc-' + f}>{f}</Label>
                <Input id={'acc-' + f} defaultValue={f.toLowerCase() + ' of the account'} />
              </div>
            ))}
            <div className="grid gap-3">
              <Label htmlFor="bio">Bio</Label>
              <Textarea id="bio" defaultValue={bio} />
            </div>
            <div className="grid gap-3">
              <Label>Language</Label>
              <Select value="English" items={['English', 'Deutsch', 'Français', '日本語']} />
            </div>
            <RadioGroup.Root defaultValue="comfortable" className="grid gap-3">
              {['default', 'comfortable', 'compact'].map((v) => (
                <div key={v} className="flex items-center gap-3">
                  <RadioGroup.Item value={v} id={'r-' + v} className="border-input text-primary aspect-square size-4 shrink-0 rounded-full border shadow-xs">
                    <RadioGroup.Indicator className="relative flex items-center justify-center">
                      <span className="bg-primary absolute top-1/2 left-1/2 size-2 -translate-x-1/2 -translate-y-1/2 rounded-full" />
                    </RadioGroup.Indicator>
                  </RadioGroup.Item>
                  <Label htmlFor={'r-' + v}>{v}</Label>
                </div>
              ))}
            </RadioGroup.Root>
            {['Marketing emails', 'Security emails', 'Product updates', 'Weekly digest'].map((s, i) => (
              <div key={s} className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
                <div className="space-y-0.5">
                  <Label>{s}</Label>
                  <p className="text-muted-foreground text-sm">Receive emails about {s.toLowerCase()}.</p>
                </div>
                <Switch defaultChecked={i % 2 === 0} />
              </div>
            ))}
            <div className="grid gap-3">
              <Label>Volume</Label>
              <Slider defaultValue={[33]} max={100} step={1} />
              <Slider defaultValue={[20, 80]} max={100} step={5} />
            </div>
            <ToggleGroup.Root type="multiple" defaultValue={['bold']} className="flex w-fit items-center rounded-md">
              {['bold', 'italic', 'underline'].map((v) => (
                <ToggleGroup.Item key={v} value={v} className={cn(toggleVariants({ variant: 'outline', size: 'sm' }), 'min-w-0 flex-1 shrink-0 rounded-none shadow-none first:rounded-l-md last:rounded-r-md')}>
                  {v}
                </ToggleGroup.Item>
              ))}
            </ToggleGroup.Root>
            <Toggle.Root aria-label="Toggle star" className={cn(toggleVariants({ variant: 'outline' }))}>
              <Star /> Star
            </Toggle.Root>
          </CardContent>
          <CardFooter>
            <Button>Save changes</Button>
          </CardFooter>
        </Card>
      </Tabs.Content>
      <Tabs.Content value="password">
        <Card>
          <CardContent>Change your password here.</CardContent>
        </Card>
      </Tabs.Content>
    </Tabs.Root>
  )
}

function CommandMenu({ pages }: { pages: string[] }) {
  const groups: [string, [string, LucideIcon][]][] = [
    ['Suggestions', [['Calendar', CalIcon], ['Search Emoji', Search], ['Calculator', Activity]]],
    ['Settings', [['Profile', User], ['Billing', CreditCard], ['Settings', Settings]]],
    ['Pages', pages.map((p): [string, LucideIcon] => [p, FileText])],
  ]
  return (
    <Command className="bg-popover text-popover-foreground flex h-full w-full flex-col overflow-hidden rounded-md border shadow-md" label="Command menu">
      <div data-slot="command-input-wrapper" className="flex h-9 items-center gap-2 border-b px-3">
        <Search className="size-4 shrink-0 opacity-50" />
        <Command.Input placeholder="Type a command or search..." className="placeholder:text-muted-foreground flex h-10 w-full rounded-md bg-transparent py-3 text-sm outline-hidden" />
      </div>
      <Command.List className="max-h-[300px] scroll-py-1 overflow-x-hidden overflow-y-auto">
        <Command.Empty className="py-6 text-center text-sm">No results found.</Command.Empty>
        {groups.map(([g, items], gi) => (
          <Command.Group key={g} heading={g} className="text-foreground overflow-hidden p-1 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium">
            {items.map(([n, I]) => (
              <Command.Item key={n} className="data-[selected=true]:bg-accent relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none">
                <I />
                {n}
              </Command.Item>
            ))}
            {gi < 2 && <Command.Separator className="bg-border -mx-1 h-px" />}
          </Command.Group>
        ))}
      </Command.List>
    </Command>
  )
}

export default function Shadcn() {
  const data = page.useLoaderData()
  const now = new Date(data.now + 'T00:00:00Z')
  return (
    <TooltipP.Provider delayDuration={0}>
      <div className="group/sidebar-wrapper flex min-h-svh w-full" style={{ ['--sidebar-width' as string]: 'calc(var(--spacing) * 72)' }}>
        <aside className="bg-sidebar text-sidebar-foreground flex h-full w-(--sidebar-width) flex-col">
          <NavigationMenu.Root orientation="vertical" className="relative flex max-w-max flex-1 items-center justify-center">
            <NavigationMenu.List className="group flex flex-1 list-none flex-col gap-1">
              {NAV.map(([n, I], i) => (
                <NavigationMenu.Item key={n}>
                  <NavigationMenu.Link href={'#' + n.toLowerCase()} active={i === 0} className={cn('data-[active=true]:bg-accent/50 hover:bg-accent flex flex-row items-center gap-2 rounded-sm p-2 text-sm transition-all', i === 0 && 'font-semibold')}>
                    <I className="size-4" />
                    {n}
                  </NavigationMenu.Link>
                </NavigationMenu.Item>
              ))}
            </NavigationMenu.List>
          </NavigationMenu.Root>
          <Collapsible.Root defaultOpen className="group/collapsible">
            <Collapsible.Trigger asChild>
              <Button variant="ghost" className="w-full justify-between">
                Documents
                <ChevronRight className="transition-transform group-data-[state=open]/collapsible:rotate-90" />
              </Button>
            </Collapsible.Trigger>
            <Collapsible.Content>
              <ScrollArea.Root className="relative h-48">
                <ScrollArea.Viewport className="size-full rounded-[inherit]">
                  {data.notes.map((n, i) => (
                    <a key={i} href={'#doc-' + i} className="hover:bg-sidebar-accent flex h-7 min-w-0 -translate-x-px items-center gap-2 overflow-hidden rounded-md px-2 text-sm">
                      <FileText className="size-4" />
                      {n.title}
                    </a>
                  ))}
                </ScrollArea.Viewport>
                <ScrollArea.Scrollbar orientation="vertical" className="flex touch-none p-px transition-colors select-none h-full w-2.5 border-l border-l-transparent">
                  <ScrollArea.Thumb className="bg-border relative flex-1 rounded-full" />
                </ScrollArea.Scrollbar>
              </ScrollArea.Root>
            </Collapsible.Content>
          </Collapsible.Root>
          <DropdownMenu.Root>
            <DropdownMenu.Trigger asChild>
              <Button variant="ghost" size="lg" className="data-[state=open]:bg-sidebar-accent">
                <Avatar name="Shad Cn" src="/avatars/shadcn.jpg" className="rounded-lg grayscale" />
                <span className="truncate font-medium">shadcn</span>
                <ChevronDown className="ml-auto size-4" />
              </Button>
            </DropdownMenu.Trigger>
            <DropdownMenu.Portal>
              <DropdownMenu.Content>
                <DropdownMenu.Item>
                  <User />
                  Account
                </DropdownMenu.Item>
                <DropdownMenu.Item>
                  <Bell />
                  Notifications
                </DropdownMenu.Item>
                <DropdownMenu.Item>
                  <LogOut />
                  Log out
                </DropdownMenu.Item>
              </DropdownMenu.Content>
            </DropdownMenu.Portal>
          </DropdownMenu.Root>
        </aside>
        <main className="bg-background relative flex w-full flex-1 flex-col">
          <header className="flex h-12 shrink-0 items-center gap-2 border-b">
            <nav aria-label="breadcrumb">
              <ol className="text-muted-foreground flex flex-wrap items-center gap-1.5 text-sm break-words sm:gap-2.5">
                {['Home', 'Projects', 'Proposal'].map((b, i) => (
                  <li key={b} className="inline-flex items-center gap-1.5">
                    {i > 0 && <ChevronRight className="size-3.5" />}
                    {i < 2 ? (
                      <a className="hover:text-foreground transition-colors" href="#">
                        {b}
                      </a>
                    ) : (
                      <span aria-current="page" className="text-foreground font-normal">
                        {b}
                      </span>
                    )}
                  </li>
                ))}
              </ol>
            </nav>
            <Separator orientation="vertical" className="mx-2 data-[orientation=vertical]:h-4" />
            <div className="ml-auto flex items-center gap-2">
              <Tooltip label="Notifications">
                <Button variant="ghost" size="icon" aria-label="Notifications">
                  <Bell />
                </Button>
              </Tooltip>
              <Tooltip label="Mail">
                <Button variant="ghost" size="icon" aria-label="Mail">
                  <Mail />
                </Button>
              </Tooltip>
              <Popover.Root>
                <Popover.Trigger asChild>
                  <Button variant="outline" size="sm">
                    <CalIcon />
                    Pick a date
                  </Button>
                </Popover.Trigger>
              </Popover.Root>
              <Dialog.Root>
                <Dialog.Trigger asChild>
                  <Button variant="outline" size="sm">
                    Share
                  </Button>
                </Dialog.Trigger>
                <Dialog.Portal>
                  <Dialog.Overlay className="fixed inset-0 z-50 bg-black/50" />
                  <Dialog.Content className="bg-background fixed top-[50%] left-[50%] z-50 grid w-full max-w-lg">
                    <Dialog.Title>Share link</Dialog.Title>
                    <Dialog.Description>Anyone who has this link will be able to view this.</Dialog.Description>
                  </Dialog.Content>
                </Dialog.Portal>
              </Dialog.Root>
            </div>
          </header>
          <div className="@container/main flex flex-1 flex-col gap-2">
            <div className="grid grid-cols-1 gap-4 px-4 lg:px-6 @xl/main:grid-cols-2 @5xl/main:grid-cols-4">
              {STATS.map(([t, v, d, f]) => (
                <Card key={t} className="@container/card">
                  <CardHeader>
                    <CardDescription>{t}</CardDescription>
                    <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">{t === 'Total Revenue' ? money(v) : t === 'Growth Rate' ? v + '%' : v.toLocaleString('en-US')}</CardTitle>
                    <div data-slot="card-action" className="col-start-2 row-span-2 row-start-1 self-start justify-self-end">
                      <Badge variant="outline">
                        {d > 0 ? <TrendingUp /> : <TrendingDown />}
                        {d > 0 ? '+' : ''}
                        {d}%
                      </Badge>
                    </div>
                  </CardHeader>
                  <CardFooter className="flex-col items-start gap-1.5 text-sm">
                    <div className="line-clamp-1 flex gap-2 font-medium">
                      {f} {d > 0 ? <TrendingUp className="size-4" /> : <TrendingDown className="size-4" />}
                    </div>
                    <div className="text-muted-foreground">Visitors for the last 6 months</div>
                  </CardFooter>
                </Card>
              ))}
            </div>
            <Alert variant="destructive">
              <CircleAlert />
              <div data-slot="alert-title" className="col-start-2 line-clamp-1 min-h-4 font-medium tracking-tight">
                Unable to process your payment.
              </div>
              <div data-slot="alert-description" className="text-muted-foreground col-start-2 grid justify-items-start gap-1 text-sm">
                Please verify your billing information and try again.
              </div>
            </Alert>
            <DataTable rows={data.rows} />
            <nav role="navigation" aria-label="pagination" className="mx-auto flex w-full justify-center">
              <ul className="flex flex-row items-center gap-1">
                {[1, 2, 3, 0, 6].map((p, i) => (
                  <li key={i}>
                    {p === 0 ? (
                      <span aria-hidden className="flex size-9 items-center justify-center">
                        <Ellipsis className="size-4" />
                      </span>
                    ) : (
                      <a aria-current={p === 1 ? 'page' : undefined} href={'?page=' + p} className={cn(buttonVariants({ variant: p === 1 ? 'outline' : 'ghost', size: 'icon' }))}>
                        {p}
                      </a>
                    )}
                  </li>
                ))}
              </ul>
            </nav>
            <div className="grid gap-4 lg:grid-cols-3">
              <SettingsCard bio={data.bio} />
              <Card>
                <CardHeader>
                  <CardTitle>Schedule</CardTitle>
                </CardHeader>
                <CardContent>
                  <DayPicker
                    mode="single"
                    today={now}
                    month={now}
                    selected={new Date(now.getTime() + 3 * 86400000)}
                    showOutsideDays
                    numberOfMonths={2}
                    timeZone="UTC"
                    className="bg-background group/calendar p-3"
                    classNames={{ months: 'flex gap-4 flex-col md:flex-row relative', month: 'flex flex-col w-full gap-4', day: 'relative w-full h-full p-0 text-center group/day aspect-square select-none', today: 'bg-accent text-accent-foreground rounded-md', outside: 'text-muted-foreground aria-selected:text-muted-foreground' }}
                  />
                  <AspectRatio.Root ratio={16 / 9} className="bg-muted rounded-lg">
                    <img src="/cover.jpg" alt="Cover" className="h-full w-full rounded-lg object-cover" />
                  </AspectRatio.Root>
                </CardContent>
              </Card>
              <Card>
                <CardHeader>
                  <CardTitle>Team members</CardTitle>
                  <CardDescription>Invite your team members to collaborate.</CardDescription>
                </CardHeader>
                <CardContent className="grid gap-6">
                  {data.people.map((p) => (
                    <div key={p.email} className="flex items-center justify-between gap-4">
                      <div className="flex items-center gap-4">
                        <Avatar name={p.name} src={'/a/' + p.email + '.png'} />
                        <div>
                          <p className="text-sm leading-none font-medium">{p.name}</p>
                          <p className="text-muted-foreground text-sm">{p.email}</p>
                        </div>
                      </div>
                      <Select value={p.role} items={['Owner', 'Member', 'Viewer']} className="ml-auto" />
                    </div>
                  ))}
                  {[0, 1, 2].map((i) => (
                    <div key={i} className="flex items-center space-x-4">
                      <Skeleton className="h-12 w-12 rounded-full" />
                      <div className="space-y-2">
                        <Skeleton className="h-4 w-[250px]" />
                        <Skeleton className="h-4 w-[200px]" />
                      </div>
                    </div>
                  ))}
                </CardContent>
              </Card>
            </div>
            <Accordion.Root type="single" collapsible defaultValue="item-1" className="w-full">
              {data.notes.map((n, i) => (
                <Accordion.Item key={i} value={'item-' + i} className="border-b last:border-b-0">
                  <Accordion.Header className="flex">
                    <Accordion.Trigger className="flex flex-1 items-start justify-between gap-4 rounded-md py-4 text-left text-sm font-medium transition-all outline-none hover:underline [&[data-state=open]>svg]:rotate-180">
                      {n.title}
                      <ChevronDown className="text-muted-foreground pointer-events-none size-4 shrink-0 translate-y-0.5 transition-transform duration-200" />
                    </Accordion.Trigger>
                  </Accordion.Header>
                  <Accordion.Content className="data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down overflow-hidden text-sm">
                    <div className="pt-0 pb-4">{n.body}</div>
                  </Accordion.Content>
                </Accordion.Item>
              ))}
            </Accordion.Root>
            <CommandMenu pages={data.pages} />
            <Button variant="destructive" size="sm">
              <Trash2 />
              Delete project
            </Button>
          </div>
        </main>
      </div>
    </TooltipP.Provider>
  )
}
