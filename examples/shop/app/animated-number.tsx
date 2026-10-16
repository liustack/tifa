import { animated, useSpring } from '@react-spring/web'

const whole = (n: number) => Math.round(n).toString()

// AnimatedNumber shows value and tweens to a new one when the data changes, as after shipping.
// There is no `from`: the first render, on the server too, shows the value itself, so the HTML and
// the first render in the browser are the same.
export function AnimatedNumber({
  value,
  format = whole,
  className,
}: {
  value: number
  format?: (n: number) => string
  className?: string
}) {
  const spring = useSpring({ value })
  return <animated.span className={className}>{spring.value.to(format)}</animated.span>
}
