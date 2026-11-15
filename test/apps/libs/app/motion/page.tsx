import { motion, AnimatePresence, MotionConfig, useMotionValue, useTransform, useSpring, LazyMotion, domAnimation, m as lm, type Variants } from 'motion/react'
import { page, type PageData } from './route.gen'

type Feature = PageData['features'][number]

export const head = () => ({ meta: [{ title: 'Motion · Library pages' }] })

const PRICES: [string, number][] = [['Hobby', 0], ['Pro', 20], ['Team', 49], ['Enterprise', 199]]
const container: Variants = { hidden: { opacity: 0 }, show: { opacity: 1, transition: { staggerChildren: 0.06, delayChildren: 0.2 } } }
const item: Variants = {
  hidden: { opacity: 0, y: 24, scale: 0.96, filter: 'blur(4px)' },
  show: { opacity: 1, y: 0, scale: 1, filter: 'blur(0px)', transition: { type: 'spring', stiffness: 300, damping: 24 } },
}

// useMotionValue, useTransform and useSpring make MotionValues, which read performance.now() when
// they are made, on the server too.
function Hero() {
  const x = useMotionValue(0)
  const rotate = useTransform(x, [-200, 200], [-12, 12])
  const opacity = useTransform(x, [-200, 0, 200], [0.2, 1, 0.2])
  const scale = useSpring(1.05, { stiffness: 200 })
  return (
    <motion.section className="hero" initial={{ opacity: 0, y: -40 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.6, ease: [0.22, 1, 0.36, 1] }}>
      <motion.h1 initial={{ letterSpacing: '-0.04em', color: '#111827' }} animate={{ letterSpacing: '0em', color: '#4f46e5' }}>
        Ship faster
      </motion.h1>
      <motion.div className="card" style={{ x, rotate, opacity, scale, transformPerspective: 800, originX: 0.5 }} drag="x" dragConstraints={{ left: -200, right: 200 }} whileHover={{ scale: 1.08 }} whileTap={{ scale: 0.95 }}>
        Drag me
      </motion.div>
      <motion.svg width="120" height="120" viewBox="0 0 120 120">
        <motion.circle cx="60" cy="60" r="50" stroke="#4f46e5" strokeWidth="6" fill="none" initial={{ pathLength: 0, rotate: -90 }} animate={{ pathLength: 0.75 }} />
        <motion.path d="M30 60 L55 85 L95 35" initial={{ pathLength: 0, opacity: 0 }} animate={{ pathLength: 1, opacity: 1 }} stroke="#16a34a" strokeWidth="8" fill="none" />
      </motion.svg>
    </motion.section>
  )
}

function Features({ features }: { features: Feature[] }) {
  return (
    <motion.ul className="features" variants={container} initial="hidden" whileInView="show" viewport={{ once: true, amount: 0.3 }}>
      {features.map((f, i) => (
        <motion.li key={i} variants={item} className="feature" style={{ backgroundColor: `hsl(${f.hue} 80% 96%)`, borderColor: `hsl(${f.hue} 70% 80%)` }} whileHover={{ y: -4, boxShadow: '0 10px 30px rgba(0,0,0,0.12)' }}>
          <motion.span className="icon" initial={{ rotate: -30, scale: 0.5 }} animate={{ rotate: 0, scale: 1 }} transition={{ delay: i * 0.03 }}>
            ★
          </motion.span>
          <motion.h3 layout="position">{f.title}</motion.h3>
          <motion.p initial={{ opacity: 0 }} animate={{ opacity: 0.8 }}>
            {f.text}
          </motion.p>
        </motion.li>
      ))}
    </motion.ul>
  )
}

function Logos({ logos }: { logos: string[] }) {
  return (
    <div className="marquee">
      <motion.div className="track" initial={{ x: '0%' }} animate={{ x: '-50%' }} transition={{ repeat: Infinity, duration: 30, ease: 'linear' }}>
        {logos.concat(logos).map((l, i) => (
          <motion.span key={i} initial={{ opacity: 0.4, filter: 'grayscale(1)' }} whileHover={{ opacity: 1, filter: 'grayscale(0)' }}>
            {l}
          </motion.span>
        ))}
      </motion.div>
    </div>
  )
}

function Pricing({ yearly, features }: { yearly: boolean; features: Feature[] }) {
  return (
    <section className="pricing">
      <motion.div layout className="toggle" data-on={yearly}>
        <motion.span layoutId="pill" className="pill" initial={false} animate={{ x: yearly ? 96 : 0 }} />
      </motion.div>
      <div className="plans">
        {PRICES.map(([n, p], i) => (
          <motion.article key={n} initial={{ opacity: 0, y: 30, rotateX: 15 }} animate={{ opacity: 1, y: 0, rotateX: 0 }} transition={{ delay: 0.1 * i, type: 'spring', bounce: 0.3 }} whileHover={{ scale: 1.03, borderColor: '#4f46e5' }} className={i === 1 ? 'plan popular' : 'plan'}>
            <h3>{n}</h3>
            <AnimatePresence mode="wait" initial={false}>
              <motion.p key={yearly ? 'y' : 'm'} initial={{ y: 10, opacity: 0 }} animate={{ y: 0, opacity: 1 }} exit={{ y: -10, opacity: 0 }} className="price">
                ${yearly ? p * 10 : p}
                <small>/{yearly ? 'year' : 'month'}</small>
              </motion.p>
            </AnimatePresence>
            <ul>
              {Array.from({ length: 6 }, (_, k) => (
                <motion.li key={k} initial={{ opacity: 0, x: -8 }} animate={{ opacity: 1, x: 0 }} transition={{ delay: 0.05 * k }}>
                  {features[(i * 6 + k) % features.length].title}
                </motion.li>
              ))}
            </ul>
            <motion.button whileTap={{ scale: 0.97 }} whileHover={{ backgroundColor: '#4338ca' }} style={{ backgroundColor: '#4f46e5', color: '#fff' }}>
              Choose {n}
            </motion.button>
          </motion.article>
        ))}
      </div>
    </section>
  )
}

function Faq({ features }: { features: Feature[] }) {
  return (
    <LazyMotion features={domAnimation}>
      <section className="faq">
        {features.slice(0, 12).map((f, i) => (
          <lm.details key={i} initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: 'auto' }} transition={{ duration: 0.3, delay: i * 0.02 }}>
            <summary>{f.title}?</summary>
            <lm.p initial={{ y: -6 }} animate={{ y: 0 }}>
              {f.text}
            </lm.p>
          </lm.details>
        ))}
      </section>
    </LazyMotion>
  )
}

export default function Motion() {
  const { features, logos } = page.useLoaderData()
  return (
    <MotionConfig reducedMotion="never" transition={{ duration: 0.4 }}>
      <motion.nav initial={{ y: -64 }} animate={{ y: 0 }} style={{ position: 'sticky', top: 0, zIndex: 10, backdropFilter: 'blur(8px)' }}>
        {['Product', 'Pricing', 'Docs', 'Blog'].map((n) => (
          <motion.a key={n} href={'#' + n.toLowerCase()} whileHover={{ color: '#4f46e5' }}>
            {n}
          </motion.a>
        ))}
      </motion.nav>
      <Hero />
      <Logos logos={logos} />
      <Features features={features} />
      <Pricing yearly={false} features={features} />
      <Faq features={features} />
      <AnimatePresence>
        {[1, 2, 3].map((k) => (
          <motion.div key={k} className="toast" initial={{ opacity: 0, y: 50, scale: 0.3 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, scale: 0.5, transition: { duration: 0.2 } }}>
            Saved {k}
          </motion.div>
        ))}
      </AnimatePresence>
    </MotionConfig>
  )
}
