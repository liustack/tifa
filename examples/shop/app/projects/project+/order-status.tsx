import { animated, useTransition } from '@react-spring/web'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

export const statusLabels: Record<string, string> = {
  pending: '待发货',
  shipped: '已发货',
}

const statusStyles: Record<string, string> = {
  pending: 'bg-[#f7f1e3] text-[#9a7f3c]',
  shipped: 'bg-[#eaf0e4] text-[#5f7449]',
}

// OrderStatus is the badge of an order's status. When the status changes, as after shipping, the
// new badge fades in over the old one. `initial: null` starts the first render, on the server too,
// at the end of the fade, so the HTML matches the first render in the browser.
export function OrderStatus({ status }: { status: string }) {
  const transitions = useTransition(status, {
    initial: null,
    from: { opacity: 0, y: -6 },
    enter: { opacity: 1, y: 0 },
    leave: { opacity: 0, y: 6 },
  })
  return (
    <span className="inline-grid">
      {transitions((style, s) => (
        <animated.span
          className="[grid-area:1/1]"
          style={{ opacity: style.opacity, transform: style.y.to((y) => `translateY(${y}px)`) }}
        >
          <Badge className={cn('gap-1.5 font-normal', statusStyles[s])}>
            <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
            {statusLabels[s]}
          </Badge>
        </animated.span>
      ))}
    </span>
  )
}
