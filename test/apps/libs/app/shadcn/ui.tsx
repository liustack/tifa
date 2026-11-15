// The shadcn/ui components this page uses, as the shadcn CLI writes them (new-york style): Radix
// primitives from the radix-ui package, variants from class-variance-authority, classes merged by
// clsx and tailwind-merge.
import type { ComponentProps, ReactNode } from 'react'
import { Avatar as AvatarP, Checkbox as CheckboxP, Label as LabelP, Progress as ProgressP, Select as SelectP, Separator as SeparatorP,
  Slider as SliderP, Slot, Switch as SwitchP, Tooltip as TooltipP } from 'radix-ui'
import { cva, type VariantProps } from 'class-variance-authority'
import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'
import { Check, ChevronDown } from 'lucide-react'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-all disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-4 shrink-0 [&_svg]:shrink-0 outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px] aria-invalid:ring-destructive/20 aria-invalid:border-destructive",
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground shadow-xs hover:bg-primary/90',
        destructive: 'bg-destructive text-white shadow-xs hover:bg-destructive/90 focus-visible:ring-destructive/20',
        outline: 'border bg-background shadow-xs hover:bg-accent hover:text-accent-foreground',
        secondary: 'bg-secondary text-secondary-foreground shadow-xs hover:bg-secondary/80',
        ghost: 'hover:bg-accent hover:text-accent-foreground',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: { default: 'h-9 px-4 py-2 has-[>svg]:px-3', sm: 'h-8 rounded-md gap-1.5 px-3 has-[>svg]:px-2.5', lg: 'h-10 rounded-md px-6 has-[>svg]:px-4', icon: 'size-9' },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)
const badgeVariants = cva(
  'inline-flex items-center justify-center rounded-md border px-2 py-0.5 text-xs font-medium w-fit whitespace-nowrap shrink-0 [&>svg]:size-3 gap-1 [&>svg]:pointer-events-none transition-[color,box-shadow] overflow-hidden',
  {
    variants: {
      variant: {
        default: 'border-transparent bg-primary text-primary-foreground',
        secondary: 'border-transparent bg-secondary text-secondary-foreground',
        destructive: 'border-transparent bg-destructive text-white',
        outline: 'text-foreground',
      },
    },
    defaultVariants: { variant: 'default' },
  },
)
const alertVariants = cva(
  'relative w-full rounded-lg border px-4 py-3 text-sm grid has-[>svg]:grid-cols-[calc(var(--spacing)*4)_1fr] grid-cols-[0_1fr] has-[>svg]:gap-x-3 gap-y-0.5 items-start',
  { variants: { variant: { default: 'bg-card text-card-foreground', destructive: 'text-destructive bg-card [&>svg]:text-current' } }, defaultVariants: { variant: 'default' } },
)
export const toggleVariants = cva(
  'inline-flex items-center justify-center gap-2 rounded-md text-sm font-medium hover:bg-muted hover:text-muted-foreground disabled:pointer-events-none disabled:opacity-50 data-[state=on]:bg-accent data-[state=on]:text-accent-foreground',
  {
    variants: { variant: { default: 'bg-transparent', outline: 'border border-input bg-transparent shadow-xs hover:bg-accent' }, size: { default: 'h-9 px-2 min-w-9', sm: 'h-8 px-1.5 min-w-8', lg: 'h-10 px-2.5 min-w-10' } },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export function Button({ className, variant, size, asChild = false, ...props }: ComponentProps<'button'> & VariantProps<typeof buttonVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : 'button'
  return <Comp data-slot="button" className={cn(buttonVariants({ variant, size, className }))} {...props} />
}
export function Badge({ className, variant, ...props }: ComponentProps<'span'> & VariantProps<typeof badgeVariants>) {
  return <span data-slot="badge" className={cn(badgeVariants({ variant }), className)} {...props} />
}
export function Card({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="card" className={cn('bg-card text-card-foreground flex flex-col gap-6 rounded-xl border py-6 shadow-sm', className)} {...props} />
}
export function CardHeader({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="card-header" className={cn('@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 has-data-[slot=card-action]:grid-cols-[1fr_auto] [.border-b]:pb-6', className)} {...props} />
}
export function CardTitle({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="card-title" className={cn('leading-none font-semibold', className)} {...props} />
}
export function CardDescription({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="card-description" className={cn('text-muted-foreground text-sm', className)} {...props} />
}
export function CardContent({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="card-content" className={cn('px-6', className)} {...props} />
}
export function CardFooter({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="card-footer" className={cn('flex items-center px-6 [.border-t]:pt-6', className)} {...props} />
}
export function Input({ className, type, ...props }: ComponentProps<'input'>) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        'file:text-foreground placeholder:text-muted-foreground selection:bg-primary selection:text-primary-foreground dark:bg-input/30 border-input flex h-9 w-full min-w-0 rounded-md border bg-transparent px-3 py-1 text-base shadow-xs transition-[color,box-shadow] outline-none md:text-sm',
        'focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]',
        'aria-invalid:ring-destructive/20 aria-invalid:border-destructive',
        className,
      )}
      {...props}
    />
  )
}
export function Textarea({ className, ...props }: ComponentProps<'textarea'>) {
  return <textarea data-slot="textarea" className={cn('border-input placeholder:text-muted-foreground flex field-sizing-content min-h-16 w-full rounded-md border bg-transparent px-3 py-2 text-base shadow-xs md:text-sm', className)} {...props} />
}
export function Label({ className, ...props }: ComponentProps<typeof LabelP.Root>) {
  return <LabelP.Root data-slot="label" className={cn('flex items-center gap-2 text-sm leading-none font-medium select-none peer-disabled:cursor-not-allowed peer-disabled:opacity-50', className)} {...props} />
}
export function Checkbox({ className, ...props }: ComponentProps<typeof CheckboxP.Root>) {
  return (
    <CheckboxP.Root data-slot="checkbox" className={cn('peer border-input data-[state=checked]:bg-primary data-[state=checked]:text-primary-foreground size-4 shrink-0 rounded-[4px] border shadow-xs', className)} {...props}>
      <CheckboxP.Indicator data-slot="checkbox-indicator" className="flex items-center justify-center text-current transition-none">
        <Check className="size-3.5" />
      </CheckboxP.Indicator>
    </CheckboxP.Root>
  )
}
export function Switch({ className, ...props }: ComponentProps<typeof SwitchP.Root>) {
  return (
    <SwitchP.Root data-slot="switch" className={cn('peer data-[state=checked]:bg-primary data-[state=unchecked]:bg-input inline-flex h-[1.15rem] w-8 shrink-0 items-center rounded-full border border-transparent shadow-xs', className)} {...props}>
      <SwitchP.Thumb data-slot="switch-thumb" className="bg-background pointer-events-none block size-4 rounded-full ring-0 transition-transform data-[state=checked]:translate-x-[calc(100%-2px)] data-[state=unchecked]:translate-x-0" />
    </SwitchP.Root>
  )
}
export function Progress({ className, value, ...props }: ComponentProps<typeof ProgressP.Root>) {
  return (
    <ProgressP.Root data-slot="progress" className={cn('bg-primary/20 relative h-2 w-full overflow-hidden rounded-full', className)} value={value} {...props}>
      <ProgressP.Indicator data-slot="progress-indicator" className="bg-primary h-full w-full flex-1 transition-all" style={{ transform: `translateX(-${100 - (value || 0)}%)` }} />
    </ProgressP.Root>
  )
}
export function Slider({ className, defaultValue, min = 0, max = 100, ...props }: ComponentProps<typeof SliderP.Root> & { defaultValue: number[] }) {
  return (
    <SliderP.Root data-slot="slider" defaultValue={defaultValue} min={min} max={max} className={cn('relative flex w-full touch-none items-center select-none data-[disabled]:opacity-50 data-[orientation=vertical]:h-full', className)} {...props}>
      <SliderP.Track data-slot="slider-track" className="bg-muted relative grow overflow-hidden rounded-full data-[orientation=horizontal]:h-1.5 data-[orientation=horizontal]:w-full">
        <SliderP.Range data-slot="slider-range" className="bg-primary absolute data-[orientation=horizontal]:h-full" />
      </SliderP.Track>
      {defaultValue.map((_, i) => (
        <SliderP.Thumb data-slot="slider-thumb" key={i} className="border-primary bg-background ring-ring/50 block size-4 shrink-0 rounded-full border shadow-sm" />
      ))}
    </SliderP.Root>
  )
}
export function Separator({ className, orientation = 'horizontal', decorative = true, ...props }: ComponentProps<typeof SeparatorP.Root>) {
  return (
    <SeparatorP.Root
      data-slot="separator"
      decorative={decorative}
      orientation={orientation}
      className={cn('bg-border shrink-0 data-[orientation=horizontal]:h-px data-[orientation=horizontal]:w-full data-[orientation=vertical]:h-full data-[orientation=vertical]:w-px', className)}
      {...props}
    />
  )
}
export function Avatar({ src, name, className }: { src: string; name: string; className?: string }) {
  return (
    <AvatarP.Root data-slot="avatar" className={cn('relative flex size-8 shrink-0 overflow-hidden rounded-full', className)}>
      <AvatarP.Image data-slot="avatar-image" className="aspect-square size-full" src={src} alt={name} />
      <AvatarP.Fallback data-slot="avatar-fallback" className="bg-muted flex size-full items-center justify-center rounded-full">
        {name
          .split(' ')
          .map((w) => w[0])
          .join('')
          .toUpperCase()}
      </AvatarP.Fallback>
    </AvatarP.Root>
  )
}
export function Alert({ className, variant, ...props }: ComponentProps<'div'> & VariantProps<typeof alertVariants>) {
  return <div data-slot="alert" role="alert" className={cn(alertVariants({ variant }), className)} {...props} />
}
export function Skeleton({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="skeleton" className={cn('bg-accent animate-pulse rounded-md', className)} {...props} />
}
export function Tooltip({ label, children }: { label: string; children: ReactNode }) {
  return (
    <TooltipP.Root>
      <TooltipP.Trigger asChild>{children}</TooltipP.Trigger>
      <TooltipP.Portal>
        <TooltipP.Content sideOffset={4} className="bg-primary text-primary-foreground animate-in fade-in-0 zoom-in-95 z-50 w-fit rounded-md px-3 py-1.5 text-xs text-balance">
          {label}
          <TooltipP.Arrow className="bg-primary fill-primary z-50 size-2.5 translate-y-[calc(-50%_-_2px)] rotate-45 rounded-[2px]" />
        </TooltipP.Content>
      </TooltipP.Portal>
    </TooltipP.Root>
  )
}
export function Select({ value, items, placeholder, className }: { value?: string; items: string[]; placeholder?: string; className?: string }) {
  return (
    <SelectP.Root defaultValue={value}>
      <SelectP.Trigger
        data-slot="select-trigger"
        data-size="default"
        className={cn(
          "border-input data-[placeholder]:text-muted-foreground [&_svg:not([class*='text-'])]:text-muted-foreground flex w-fit items-center justify-between gap-2 rounded-md border bg-transparent px-3 py-2 text-sm whitespace-nowrap shadow-xs data-[size=default]:h-9",
          className,
        )}
      >
        <SelectP.Value placeholder={placeholder} />
        <SelectP.Icon asChild>
          <ChevronDown className="size-4 opacity-50" />
        </SelectP.Icon>
      </SelectP.Trigger>
      <SelectP.Portal>
        <SelectP.Content position="popper">
          <SelectP.Viewport>
            {items.map((it) => (
              <SelectP.Item key={it} value={it} className="relative flex w-full cursor-default items-center gap-2 rounded-sm py-1.5 pr-8 pl-2 text-sm">
                <SelectP.ItemText>{it}</SelectP.ItemText>
              </SelectP.Item>
            ))}
          </SelectP.Viewport>
        </SelectP.Content>
      </SelectP.Portal>
    </SelectP.Root>
  )
}
