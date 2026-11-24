---
summary: '怎样在 Tifa 应用里做一个表单：方法处理函数、tifa.Fields 错误、useActionState、useOptimistic'
audience: app
order: 1
translatedFrom: af03d9f15173
read_when:
  - '添加一个创建或修改数据的表单，想要从输入框到屏幕上错误信息的完整路径'
  - '表单不显示字段错误、按钮可以点两次，或者表单发送后列表没有变化'
  - '在 React 19 的表单 hooks 和 react-hook-form 之类的表单库之间做选择'
---

# 表单 {#forms}

一本留言簿。访客输入名字和留言，Go 一侧检查并保存留言，页面显示它。做完之后，表单会在每个错误的输入下面显示错误，发送时禁用按钮，立刻显示新留言，并在服务器回答之后加载真实的列表。

这些代码是照写出来的样子运行过的。一共两个文件。参考文档说明了每个调用做什么：[方法处理函数](../pages-and-data/routes.zh-CN.md#method-handlers-with-input-and-result)、[从浏览器调用它们](../pages-and-data/pages.zh-CN.md#calling-method-handlers-from-the-browser)。

## Go 一侧 {#the-go-side}

`app/route.go`：

```go
package app

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/liustack/tifa"
)

// Message is one entry of the guestbook.
type Message struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Text string `json:"text"`
}

// A real app keeps these in a database.
var (
	mu       sync.Mutex
	messages = []Message{}
	lastID   int64
)

func Load(c *tifa.Ctx) ([]Message, error) {
	mu.Lock()
	defer mu.Unlock()
	return append([]Message{}, messages...), nil
}

// SignInput is the form. Fields are named by their json tags.
type SignInput struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// Validate runs after the body is decoded and before POST. Every wrong field gets a message,
// and Err is nil when none did.
func (in SignInput) Validate() error {
	fields := tifa.Fields{}
	if strings.TrimSpace(in.Name) == "" {
		fields["name"] = "Tell us your name."
	}
	switch n := utf8.RuneCountInString(strings.TrimSpace(in.Text)); {
	case n == 0:
		fields["text"] = "Write a message."
	case n > 200:
		fields["text"] = "Keep it under 200 characters."
	}
	return fields.Err()
}

// POST adds the message and answers with it.
func POST(c *tifa.Ctx, in SignInput) (Message, error) {
	mu.Lock()
	defer mu.Unlock()
	lastID++
	m := Message{ID: lastID, Name: strings.TrimSpace(in.Name), Text: strings.TrimSpace(in.Text)}
	messages = append([]Message{m}, messages...)
	return m, nil
}
```

- `Validate` 是规则唯一放的地方。它对每个调用者都运行，不只是你的页面。`tifa.Fields` 以 422 响应，带 `{"error": ..., "fields": {"name": "..."}}`，键是输入的 json 名字。`Validate` 返回的任何其他错误以 400 响应，把它的文字作为消息，所以当某条消息属于某个输入时，请用 `tifa.Fields`。
- 先收集所有的消息，最后用 `fields.Err()` 结束。这样访客能一次看到每个错误。你什么都没加的时候，`Err` 是 nil。
- 在浏览器里也检查（`required` 属性、一个 schema）只能省一次往返。不要因此跳过 Go 里的检查。

## 页面 {#the-page}

`app/page.tsx`：

```tsx
import { useRouter } from '@tanstack/react-router'
import { useActionState, useOptimistic, useSyncExternalStore } from 'react'
import { useFormStatus } from 'react-dom'
import { TifaError } from 'tifa/client'
import { page, POST, type PageData } from './route.gen'

type Message = PageData[number] & { pending?: boolean }

type FormState = {
  values: { name: string; text: string }
  fields: Record<string, string>
  message?: string
}

const empty: FormState = { values: { name: '', text: '' }, fields: {} }

// False on the server and during hydration, true once the browser has taken over.
const useHydrated = () =>
  useSyncExternalStore(
    () => () => {},
    () => true,
    () => false,
  )

export default function Guestbook() {
  const messages = page.useLoaderData()
  const router = useRouter()

  // While the action runs, show the new message at once. When it ends, the list is whatever
  // the page data holds: the new message if the reload brought it, the old list if the call failed.
  const [shown, addOptimistic] = useOptimistic(messages as Message[], (current, m: Message) => [m, ...current])

  const [state, submit] = useActionState(async (_previous: FormState, data: FormData): Promise<FormState> => {
    const values = { name: String(data.get('name') ?? ''), text: String(data.get('text') ?? '') }
    addOptimistic({ id: -1, ...values, pending: true })
    try {
      await POST({ input: values })
      // A call never reloads the page data. Wait for the reload, so the optimistic message
      // is replaced by the real one without a gap.
      await router.invalidate({ sync: true })
      return empty
    } catch (e) {
      if (!(e instanceof TifaError)) throw e
      // e.fields holds a message per input, from tifa.Fields in Go. Any other error has only a message.
      return { values, fields: e.fields, message: Object.keys(e.fields).length === 0 ? e.message : undefined }
    }
  }, empty)

  return (
    <>
      <h1>Guestbook</h1>
      <form action={submit} noValidate>
        <Field label="Your name" name="name" state={state} />
        <Field label="Message" name="text" state={state} />
        {state.message && <p role="alert">{state.message}</p>}
        <SubmitButton />
      </form>
      <ul>
        {shown.map((m) => (
          <li key={m.id} style={{ opacity: m.pending ? 0.5 : 1 }}>
            <strong>{m.name}</strong>: {m.text}
          </li>
        ))}
      </ul>
    </>
  )
}

// useFormStatus reads the form around it, so it must sit in a component of its own.
function SubmitButton() {
  const { pending } = useFormStatus()
  const hydrated = useHydrated()
  return (
    <button type="submit" disabled={pending || !hydrated}>
      {pending ? 'Sending…' : 'Sign'}
    </button>
  )
}

function Field({ label, name, state }: { label: string; name: 'name' | 'text'; state: FormState }) {
  const error = state.fields[name]
  return (
    <p>
      <label htmlFor={name}>{label}</label>
      {/* React resets the form after an action, so the input starts from what the action returned. */}
      <input
        id={name}
        name={name}
        defaultValue={state.values[name]}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${name}-error` : undefined}
      />
      {error && (
        <span id={`${name}-error`} className="error">
          {error}
        </span>
      )}
    </p>
  )
}
```

访客按下按钮时发生的事：

1. React 在一个 transition 里，用表单数据调用你给 `useActionState` 的函数。`useFormStatus` 报告 `pending`，所以按钮被禁用，并显示「Sending…」。
2. `addOptimistic` 把新留言放在列表最上面，调暗显示。它必须在 action 里调用（见 [useOptimistic](https://react.dev/reference/react/useOptimistic)）。
3. `POST` 把输入作为 JSON 发出。如果 Go 拒绝了它，这个调用会抛出 `TifaError`。`e.fields` 放着 `tifa.Fields` 的消息，每个输入一条，`e.message` 是其他任何错误的文字。
4. 成功时，`router.invalidate({ sync: true })` 重新加载页面数据，并在页面显示它时完成。到那时 action 才结束，调暗的留言被真实的留言替换。见 [重新加载页面数据](../pages-and-data/pages.zh-CN.md#pagetsx)。
5. 失败时，action 返回输入的值和消息。乐观的留言会自己消失，因为 action 结束时没有新的页面数据。

## 取舍 {#decisions}

**用 React hooks 还是表单库。** 这三个 hooks（`useActionState`、`useFormStatus`、`useOptimistic`，见 [useActionState](https://react.dev/reference/react/useActionState) 和 [useFormStatus](https://react.dev/reference/react-dom/hooks/useFormStatus)）足够应付几个字段、没有客户端规则的表单。字段很多、规则要在访客输入时运行，或者有字段数组时，就用库。`examples/shop` 用 react-hook-form 加一个 Zod schema（`app/projects/project+/orders/order+/ship-form.tsx`），[服务端渲染](../pages-and-data/server-render.zh-CN.md#libraries-that-render-differently-on-the-server) 列出了为它在服务器上要做什么。Tifa 这一部分对任何库都一样：捕获 `TifaError`，把 `e.fields` 交给库设置错误的办法。对 react-hook-form 就是 `form.setError(name, { message })`。

**水合之前让按钮保持禁用。** 用函数作为 `action` 时，服务器会写出 `action="javascript:throw new Error('React form unexpectedly submitted.')"`。在脚本运行之前，表单什么也发不出去，在输入框里按 Enter 就会碰上这个。`useHydrated` 让按钮在那之前一直禁用，这也挡住了 Enter 键。

**要不要用 `sync: true` 重新加载。** `await router.invalidate({ sync: true })` 在页面显示新数据时才完成，这正是乐观留言能无闪烁地让位的原因。普通的 `await router.invalidate()` 立刻就完成，页面在后台更新。表单没有任何乐观内容、你又不介意短暂的等待时，就选它。

**要不要乐观。** 当写入几乎总是成功、结果又容易猜到时，像这里这样，在回答之前就显示留言是值得的。当结果由服务器决定（价格、排队号码）时，不要用 `useOptimistic`，靠禁用的按钮就行。

**不是 `TifaError` 的错误。** 网络故障或你代码里的 bug，不是应该显示在输入框下面的东西。这个例子把它重新抛出，React 会把 action 抛出的错误交给最近的错误边界（见 [useActionState](https://react.dev/reference/react/useActionState)）。如果你想让表单留下来，就从 action 里返回一条消息。

**用访客的语言显示消息。** 在多语言站点上，`Validate` 可以接收 Ctx 并用 `c.Language()` 作答，或者返回由页面翻译的代码。哪种合适，见 [校验消息](i18n.zh-CN.md#validation-messages)。

**表单之后重定向。** 如果表单创建的东西有它自己的页面，就从处理函数里返回 `tifa.Redirect(url, 303)`。这样调用会通过路由前往那个页面，函数的结果是 `undefined`。

## 检查一下 {#check-it}

```sh
go tool tifa check
go tool tifa dev
```

发送空表单，两条消息都会显示。在留言里输入 201 个字符，名字会保留，错误显示在留言下面。发送一个正确的，字段会清空。把处理函数放慢（在 `POST` 里加一个 `time.Sleep`），可以看到调暗的留言和「Sending…」按钮。
