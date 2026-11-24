---
summary: '服务端渲染有什么、缺什么：beastjs 虚拟机里的浏览器和 Node API、导入 Node 模块的包、在那里渲染结果不同的库、只在浏览器里加载的组件，以及怎样使用 Intl 和 Temporal 让水合结果一致'
audience: app
order: 7
translatedFrom: 251220614700
read_when:
  - '页面在服务端渲染失败或没有水合，想知道服务端有哪些浏览器 API 和库的功能'
  - '某个组件在浏览器画出来之前什么都不显示（比如图表或地图），想知道要不要在服务端渲染它'
  - '在页面里格式化数字或日期，或者 `tifa dev` 对某个 Intl 调用发出警告'
  - '使用 Temporal，或者日期、时间上出现水合不一致'
  - '某个包在服务端崩溃，报错提到 Node 模块、顶层 await 或严格模式'
  - '在模块顶层保存状态，或者某个 store 或缓存看到了另一位访客的数据'
---

# 服务端渲染 {#the-server-render}

## 服务端渲染有什么 {#what-the-server-render-has}

页面在服务器上的一个 beastjs（Tifa 用来渲染的纯 Go JavaScript 引擎）VM 里渲染，不是在 Node 或浏览器里。VM 有 JavaScript 语言本身，包括 `Proxy`、`WeakRef`、`FinalizationRegistry` 和 `new Function`，还有这些：

- `URL` 和 `URLSearchParams`，完整的标准，由引擎在 Go 里解析。主机名会经过 UTS 46，包含它的 Bidi 和连接符检查，和浏览器一样。URL 没有自己的属性，无效的 URL 会抛出 `TypeError: Failed to construct 'URL': Invalid URL`，而 `URL.createObjectURL` 和 `URL.revokeObjectURL` 不存在，因为没有 `Blob`。
- `TextEncoder`、`TextDecoder`、`AbortController`、`Headers`、`Response` 和 `queueMicrotask`。`TextDecoder` 只认识 UTF-8：别的编码，比如 `new TextDecoder('latin1')`，会抛出 `RangeError`。
- 用于 Base64 二进制字符串的 `atob` 和 `btoa`，以及用于 Web API 错误的 `DOMException`。`btoa` 拒绝大于 U+00FF 的字符。
- `performance.now()` 和 `performance.timeOrigin`，`performance` 的其他部分都没有。`timeOrigin` 是 VM 加载宿主代码时的 `Date.now()`。`now()` 是从那时起经过的毫秒数，是一个数字，并且永远不会比上一次调用返回的值更小。两者读的都是 VM 在加载时拥有的 `Date.now`，所以页面替换 `Date.now` 不会改变它们，它们会跟随用 `tifa.WithRenderClock` 设置的渲染时钟。VM 本来就有的 `performance` 会保留。
- `crypto.getRandomValues` 就地填充整数类型的数组，每次调用最多 65,536 字节。指向共享或可调整大小的缓冲区的视图，会在检查元素类型或配额之前就抛出 `TypeError`。`crypto.randomUUID` 生成版本 4 的 UUID。两者都从 Go 的 `crypto/rand` 读取新鲜的字节，模块加载期间也一样。没有随机字节池。这个宿主操作可以读取操作系统的熵源。它不会向应用代码暴露文件或网络访问。要让随机值远离会被水合的 HTML，和对待 `Math.random` 一样。
- `structuredClone` 会保留原始值、对象、数组、Date、RegExp、Map、Set、错误（包括 cause 和 AggregateError）、ArrayBuffer、类型化数组、DataView 和装箱原始值里的环和共享引用。缓冲区会被复制，可调整大小的缓冲区也一样。指向可调整大小的 ArrayBuffer 的视图会抛出 `DataCloneError`，不管它的长度是固定的还是跟着缓冲区变。函数、symbol 和弱集合会抛出 `DataCloneError`。Promise、FinalizationRegistry、原生迭代器和不支持的宿主对象，会按它们的原型链被拒绝。移除或替换这条链，会让这些值看起来像普通对象，所以只会复制它们可枚举的自有属性。和通过内部品牌检查来识别的受支持内置类型不同，这些类型在纯 JS 里不一定能被识别出来。和浏览器不同，普通对象或数组的透明代理，会通过它们的 trap 被当作普通对象或数组来克隆。只是模仿受支持内置原型的对象会被拒绝。SharedArrayBuffer 和非空的转移列表不受支持，会抛出 `DataCloneError`。空的转移列表是允许的。自定义原型、访问器和属性特性不会保留，只会复制普通对象和数组的可枚举自有字符串键。
- `AsyncLocalStorage`，作为全局变量，也作为 `node:async_hooks` 的导出连同 `AsyncResource`，和 Cloudflare Workers 提供的一样：`run`、`exit`、`getStore`、`AsyncLocalStorage.bind` 和 `AsyncLocalStorage.snapshot`，没有 `enterWith` 和 `disable`。存储会跟着 `await` 和 promise 回调走，所以把请求状态放在其中的包可以工作。没有定时器或事件供它跟随。
- `Intl` 和 `toLocale*` 方法，见[服务端渲染里的 Intl](#intl-in-server-rendering)。
- `Temporal` 和 `Date.prototype.toTemporalInstant`，见[服务端渲染里的 Temporal](#temporal-in-server-rendering)。
- `console`，有 `log`、`info`、`debug`、`warn`、`error` 和 `assert`。前四个什么也不打印。`error` 和失败的 `assert` 会抛出，所以渲染会带着它们的消息失败。其他方法，比如 `console.time` 或 `console.table`，不存在，调用会抛出 `TypeError`。
- `Date.now()` 和 `Math.random()`。它们能用，但它们的值不能进入 HTML，见 [SSR 与缓存](ssr-and-cache.zh-CN.md#ssr-and-cache)。

它没有：

- 定时器：`setTimeout`、`setInterval`、`requestAnimationFrame`、`requestIdleCallback`。渲染在微任务用完时结束，没有东西会等时间。
- `performance` 里除 `now()` 和 `timeOrigin` 以外的部分，以及 `crypto.subtle`。
- 浏览器：`window`、`document`、`navigator`、`self`、`location`、`localStorage`、`matchMedia`、`ResizeObserver`、`Event`、`Blob`、`FormData` 等等。
- I/O：`fetch`、`XMLHttpRequest`、`WebSocket`、`require`、`process`、`Buffer`。构建会替换包里的 `process.env.NODE_ENV`，所以只读它的代码可以工作。

React 不会在服务器上运行 effect，所以在 `useEffect`、`useLayoutEffect` 或事件处理函数里用到这些东西的代码没有问题。在渲染期间用到它们的代码会让渲染失败，在模块顶层用到它们的代码会让 VM 无法加载。先用 `typeof` 检查，或者在 effect 里使用，或者把组件挪进 `.client.tsx` 文件。

### 顶层 await 和严格模式 {#top-level-await-and-strict-mode}

SSR 包是一个 ES 模块。每个 VM 在加载时把它运行一次，其中的模块（无论是应用的还是 npm 包的）都可以使用顶层 `await`：VM 会在第一次渲染之前等它完成。VM 没有定时器，也不做 I/O，所以这样的 `await` 必须只靠 promise 就能完成，比如对包里的某个模块做 `await import('./mod')`。等定时器或等请求的 `await` 永远不会完成，服务器也不会启动：VM 加载失败，错误说明该模块的顶层 await 仍然处于等待状态。

作为模块，整个包都在严格模式下运行，和浏览器包一样。只能在非严格模式下工作的包，在服务器上同样会失败。只有宽松模式才允许的语法，比如 `with` 语句、两个同名参数或对 `eval` 赋值，会让 `tifa build` 失败，并指出源文件里的位置：构建会像服务器那样编译 SSR 包，但不运行它。在 `tifa dev` 下，应用加载重新构建的包时会显示同样的错误。严格模式只在代码运行时才会阻止的情况，会在那时抛出：给从未声明的变量赋值会抛出 `ReferenceError`，读取 `arguments.callee` 会抛出 `TypeError`。

### import Node 模块的包 {#packages-that-import-node-modules}

有的包会为了只在 Node 上运行的代码而 import 一个 Node 模块，比如 `node:fs` 或 `node:path`。服务器没有 Node 模块，但这个 import 不会让应用停下：它 import 的每个名字都是一个替身，当渲染用到它（调用它、构造它或读取它的某个属性）时就会抛出。`tifa build` 会列出留在 SSR 包里的 Node 模块，以及 import 它们的包。这个列表只是个提示，因为从不走到那段代码的页面也能渲染。只有 `import()` 才会到达的模块，只在那个 import 运行时才运行。

渲染用到替身时，错误会写明导出名、模块、包，以及它源码里的位置。在模块顶层用到的，会让 VM 无法加载。这时请从 `.client.tsx` 文件里 import 这个包，或者改用不需要 Node 的构建版本，很多包都为浏览器或 worker 提供了这样的版本。对 Node 模块的 `require` 仍然是 `require`，服务器没有它，构建也会用同样的方式警告。`node:async_hooks` 的 `AsyncLocalStorage` 和 `AsyncResource` 不是替身，见上文。

### 会等待的组件 {#components-that-wait}

组件可以在 promise 自己就会完成的工作上挂起：比如模块在包里的 `React.lazy` 组件，像 `lazy(() => import('./panel'))` 这样的，或者对由页面已有内容生成的 promise 使用 `use`。服务器会等它，并把页面完整写出，HTML 里没有 fallback，就像浏览器在一切加载完之后显示的那样。

服务器没有定时器，也不做 I/O，所以只有在还有 promise 任务时才会等。等定时器或等请求的组件永远等不到：渲染会失败，错误说明有组件挂起且永远没有完成。请用 `Load` 把数据交给页面，或者只在浏览器里渲染这个组件。不挂起的页面一次渲染就完成，和以前一样。

### 一次渲染留给下一次的东西 {#what-one-render-leaves-for-the-next}

VM 只加载一次，然后一个接一个地渲染页面，一次渲染一个。模块在顶层保存的东西会在渲染之间留下：变量、缓存、闭包和库的状态，比如在组件之外创建的 store。这个 VM 里的下一次渲染会看到它，不管是为哪个用户渲染。`ssr.vms` 个 VM 里的每一个都有自己的一份，被替换的 VM（出错、超时、内存耗尽或增长过多之后）会从全新的加载重新开始。Node 服务器也以同样的方式，在进程的整个生命期内保留模块状态。

所以不要把请求或用户的数据放在模块作用域里：把 store、query client 之类的东西创建在组件或 provider 里面，比如用 `useState(() => createStore())`。当模块级缓存里的东西对每个请求都一样时（比如一个格式化器或一个解析好的常量），它是没问题的。

### 在服务器上渲染结果不同的库 {#libraries-that-render-differently-on-the-server}

有些库会把一部分工作放在 effect 里，所以服务器渲染出来的内容，比浏览器后来显示的要少。当写成下面这样时，页面仍然可以无错误地水合：

- react-spring：不要给 `useSpring` 传 `from`，给 `useTransition` 传 `initial: null`。这样服务器上和浏览器里的第一次渲染显示的都是最终值，只有之后的变化才会有动画。
- react-hook-form：用 `Controller` 来写字段。服务器会把受控字段的值写进 HTML，而来自 `register` 的字段，只有在水合之后才有值。在页面水合完成之前，让提交按钮保持禁用，比如用 `useSyncExternalStore(() => () => {}, () => true, () => false)`，这样在水合之前按 Enter，不会把表单当成普通的 HTML 表单提交出去。
- Radix Select（shadcn 的 Select）：被选中项的标签，只有在水合之后才会移进触发器。当字段有值时，请把标签作为 `SelectValue` 的 children 传入，否则服务器会渲染出一个空按钮。没有值时，占位符照常渲染。
- recharts 3 在服务器上只渲染它那个空的外壳，水合之后才绘制。请给图表的容器一个固定的高度，让页面不会跳动，并且把关键数字也用文本显示出来。要让图表完全不进入服务端渲染，见[只在浏览器里绘制的组件](#components-that-draw-only-in-the-browser)。

### 只在浏览器里绘制的组件 {#components-that-draw-only-in-the-browser}

有些组件在服务器上什么也不显示：它们要测量自己的盒子、在 canvas 上绘制，或者要等一个 effect 才渲染内容。服务器写出一个空盒子或一个加载状态，浏览器在水合之后替换它。这样的组件在服务器上仍然要付全价：它的代码在 SSR 包里，每个 VM 都要加载它，页面的每次渲染都要运行它。

用 `clientOnly` 加载这样的组件，并配一个同样大小的占位元素。要判断一个组件是否符合条件，就看服务器为它写出的 HTML（查看页面源码）。如果是一个空盒子、一个加载状态，或浏览器会丢掉的标记，那服务器渲染它就什么也得不到。人们在脚本运行之前就该看到的文本，或搜索引擎应该读到的文本，比如图表背后的关键数字，应该放在服务端渲染里：把它作为纯文本写在组件旁边。

```tsx
import { clientOnly } from 'tifa/client'
import { page } from './route.gen'

// sales-chart.client.tsx default-exports the chart.
const SalesChart = clientOnly(() => import('./sales-chart.client'), <div className="h-64 w-full" />)

export default function StatsPage() {
  const { days } = page.useLoaderData()
  return <SalesChart days={days} />
}
```

- `.client.tsx` 文件默认导出这个组件，props 照常传入。
- 服务器和水合时渲染的都是占位元素，所以页面可以无错误地水合。然后浏览器加载这个模块并显示组件。请把占位元素设成组件将占的大小，这样页面不会移位。
- `.client.tsx` 文件 import 的任何东西，都不会进入 SSR 包。在浏览器里，它是一个单独的 chunk，在组件第一次渲染时才获取，即使页面本身属于入口脚本（根布局就是）也一样。
- 组件会比页面的其余部分晚一会儿才出现，因为它的代码是在水合之后才获取的。如果获取失败，组件会抛出错误，最近的 `ErrorBoundary` 会显示出来。

这样把只在浏览器里用的库挡在 SSR 包之外，让示例商店的 SSR 包小了三分之一，统计页的渲染快了 21%。完整的数字见 [优化](../studies/optimizations.md#build)。

## 服务端渲染里的 Intl {#intl-in-server-rendering}

页面在服务器上渲染时可以使用 `Intl`，支持 Chromium 有的每一种语言区域。不带日期的 `Intl.DateTimeFormat.format()` 和 `formatToParts()`，读取的渲染时钟和 Date、Temporal.Now 相同，包括用 `tifa.WithRenderClock` 提供的时钟。所有这些都和 Chromium 里的表现一致：

- `Intl.NumberFormat`、带 `formatRange` 的 `Intl.DateTimeFormat`、`Intl.PluralRules`、`Intl.Collator`、`Intl.Segmenter`、`Intl.RelativeTimeFormat`、`Intl.ListFormat`、`Intl.DisplayNames`、`Intl.DurationFormat`、`Intl.Locale`、`Intl.supportedValuesOf`、`Intl.getCanonicalLocales`，以及每个构造函数的 `supportedLocalesOf`。
- `Date` 的 `toLocaleString`、`toLocaleDateString` 和 `toLocaleTimeString`，`Number` 和 `BigInt` 的 `toLocaleString`，按语言区域的规则排序的 `localeCompare`，以及 `toLocaleUpperCase` 和 `toLocaleLowerCase`。
- Chromium 有的每一种历法，用 `-u-ca-` 或 `calendar` 选项来指定。

不带语言区域时，服务器按 `en-US` 格式化，不带 `timeZone` 时按 `UTC`，不管机器的语言和时区是什么。服务器上 `Date` 的本地时间也始终是 UTC：`getTimezoneOffset()` 是 0，`new Date(2026, 0, 1)`、`getHours` 和 `toString` 都按 UTC 工作。应用的 Go 代码不受影响：进程保持启动时的时区，所以 `time.Local`、日志和 SQLite 用的是那个时区，和任何 Go 程序一样。

时区数据来自内置在程序里的 tz 2026e：从不读取机器的 zoneinfo。输出已经和 Chromium 156 在大约 75,000 个表达式上做过比较。数字、日期和复数形式完全一致。其余的有少数几处不同：`Intl.Segmenter` 对某些语言的分词方式不同，`Intl.DisplayNames` 的名称比 Chromium 多，温尼伯使用永久夏令时，tz 2026e 有这一条，而 Chromium 156 还没有。错误消息的措辞不同。Firefox 和 Safari 自带各自的一份数据。较新的版本很接近，但 macOS 26.4 之前的 Safari 数据较旧（CLDR 46），少数格式可能不同。

### 让水合一致的格式化方式 {#formatting-so-hydration-matches}

同样的调用，只有在语言区域和时区相同时，才会在服务器和浏览器里给出同样的文本。调用里省略的部分，浏览器用它的用户的语言和时区来补，服务器则用 `en-US` 和 `UTC`，于是 React 会报告水合不匹配。所以：

- 给每个调用都传上语言区域，对日期还要传 `timeZone`，它们来自 `Load` 或布局的数据。它们从哪里来由应用决定：用户保存的设置，浏览器把 `Intl.DateTimeFormat().resolvedOptions().timeZone` 存进的 cookie，或者 `c.Header("Accept-Language")`。来自请求的值，在到达页面之前要先检查：未知的时区会抛出 `RangeError`，渲染就会失败。
- 不要用 `Date` 的本地时间方法来渲染（`getHours`、`getDate`、`toString`、`new Date(2026, 0, 1)`）：它们在服务器上给出 UTC，在浏览器里给出用户的时区。请用 UTC 方法，或者带 `timeZone` 的 Intl。
- 依赖当前时间的文本，比如「3 分钟前」，不管你传什么，渲染和水合时都会不同。请在 `Load` 里计算它，或者在 `useEffect` 里渲染它。

```go
type Receipt struct {
	Amount   float64 `json:"amount"`
	PaidAt   string  `json:"paidAt"`
	Locale   string  `json:"locale"`
	TimeZone string  `json:"timeZone"`
}
```

```tsx
import { page } from './route.gen'

export default function ReceiptPage() {
  const { amount, paidAt, locale, timeZone } = page.useLoaderData()
  return (
    <p>
      {new Intl.NumberFormat(locale, { style: 'currency', currency: 'CNY' }).format(amount)}
      {new Date(paidAt).toLocaleDateString(locale, { timeZone })}
    </p>
  )
}
```

当 `locale: 'zh-CN'`、`timeZone: 'Asia/Shanghai'`、`paidAt: '2025-08-16T20:05:09Z'` 时，这会在服务器和浏览器里都渲染出 `¥1,234.50` 和 `2025/8/17`。

### `tifa dev` 里的警告 {#warnings-in-tifa-dev}

在 [`tifa dev`](../build-and-deploy/dev.zh-CN.md) 下，如果某次渲染调用了不带语言区域的 Intl 构造函数或 `toLocale*` 方法，或者不带 `timeZone` 的日期相关调用，就会记录一条警告，里面有这次调用在你源码中的位置和它的调用栈：

```text
2026/10/16 08:00:00 tifa: ssr: Number.prototype.toLocaleString without a locale at app/orders/order+/items.tsx:12:7. The server formats in en-US and UTC, a browser in the language and time zone of its user, so the text can differ from the HTML and React reports a hydration mismatch. Pass the locale and, for a date, the timeZone, from data Load returns, as in toLocaleDateString('zh-CN', {timeZone: 'Asia/Shanghai'}), or format in a useEffect.
    at Price (app/orders/order+/items.tsx:12:7)
    at ...
```

每个位置只记录一次，`tifa dev` 重新加载 SSR 包之后会再记录一次。警告不会改变渲染。构建出的程序不做检查。包内部的调用，会先写出包的文件，你的代码在调用栈更靠下的位置。

### 开销 {#cost}

在某次渲染用到 Intl 之前，VM 里只带着 Intl 的替身，大约 10 KB，用到之后，这个 VM 会增加大约 30 到 45 KB。示例商店的一个 VM 大约占 11 MB，所以这不到它的 1%。在 `tifa dev` 下，每个 VM 都会立刻付出这笔开销，因为警告需要装上 Intl。每种语言在每个进程里只读入内存一次，100 到 180 KB，另外在第一次用到任何语言时，还有所有语言共用的大约 1 MB。语言区域数据让程序大约增大 1.7 MB，生成的 `main.go` 内嵌的时区数据库再增加大约 450 KB。语言区域数据带有一份许可声明，要随程序一起发布，见[随程序发布的声明](../build-and-deploy/build.zh-CN.md#notices-to-ship-with-the-program)。

## 服务端渲染里的 Temporal {#temporal-in-server-rendering}

页面可以使用 `Temporal.Now`、`Instant`、`ZonedDateTime`、`PlainDate`、`PlainTime`、`PlainDateTime`、`PlainYearMonth`、`PlainMonthDay`、`Duration` 和 `Date.prototype.toTemporalInstant`，包括在模块加载时。beastjs 提供 V8 带的那些方法、IANA 时区，以及 Intl 支持的历法。Temporal 用的是和 Intl 相同的内置 tz 2026e 数据，不读取机器的 zoneinfo。

`Date.now()`、`new Date()`、`Date()`、`Temporal.Now` 和不带日期的 Intl 日期格式化，共用 VM 的时钟。默认情况下它在渲染期间保持实时。Go 可以用 `tifa.WithRenderClock` 为一次渲染提供一个时钟，见[渲染时钟](testing-apps.zh-CN.md#render-clock)。`Temporal.Now.instant().epochMilliseconds` 和 `Date.now()` 各自单独读取这个时钟，所以如果两次调用之间过了一点时间，它们可能不同。`Temporal.Now.timeZoneId()` 是 `UTC`，也就是 Intl 的默认时区，其他 Now 方法在没有传入时区时也使用 UTC。进程的 Go 时区保持不变。

`toLocaleString` 使用 Intl。请传入明确的语言区域。纯日期或纯时间没有时区，会忽略 `timeZone`，`Instant` 使用格式化器的时区，`ZonedDateTime` 使用它自己的时区。构造带时区的值或格式化 instant 时，请使用明确的时区。ISO 的 `PlainYearMonth` 和 `PlainMonthDay` 在格式化时需要 `calendar: 'iso8601'`。

为了让水合一致，请从 `Load` 或布局返回 ISO 字符串以及语言区域和时区，然后在两边重建同样的 Temporal 值。比如，当 `day: '2026-10-22'` 且 `locale: 'en-US'` 时：

```tsx
const { day, locale } = page.useLoaderData()
return <time>{Temporal.PlainDate.from(day).toLocaleString(locale, { dateStyle: 'long' })}</time>
```

这会渲染出 `October 22, 2026`。不要把 `Temporal.Now` 的结果放进会水合的 HTML：浏览器运行得更晚，它的默认时区也可能不同。请在 `Load` 里计算一个固定的值，或者在水合之后用 effect 显示当前时间。浏览器的语言区域数据可能不同，见 Intl 一节的说明。Tifa 只给服务端 VM 提供 Temporal，不提供浏览器 polyfill 或 TypeScript 声明。没有 Temporal 的浏览器，需要在页面代码之前加载一个 polyfill，或者页面在那里必须避开它。

Temporal 在每个 VM 里是延迟安装的，在第一次读取 `Temporal` 或调用 `toTemporalInstant` 时才安装。不用它的页面只保留替身。用来安装它的字节码在每个进程里只编译一次。实测的大小、VM 内存和首次使用的开销，见[内存](../reference/performance.zh-CN.md#memory)。现有的[六份声明文件](../build-and-deploy/build.zh-CN.md#notices-to-ship-with-the-program)也涵盖 Temporal。
