---
summary: '在 tifa 应用里怎么写表单：用生成的函数把表单发给 Go 的方法函数，把 tifa.Fields 的字段错误显示在每个输入框旁边，发送时禁用按钮，成功后重新加载页面数据，并在服务器回答之前先显示新的一行（React 19 的 useActionState、useFormStatus 和 useOptimistic）'
read_when:
  - 加一个新建或修改数据的表单，想看从输入到屏幕上的错误提示的完整做法
  - 表单不显示字段错误、按钮能连点两次，或者表单提交后列表没变
  - 在 React 19 的表单 hook 和 react-hook-form 这类表单库之间做选择
---

[English](forms.md) · 简体中文

# 表单

一个留言簿。访客填名字和留言，Go 这边检查并保存，页面显示出来。做完后，表单会在每个填错的输入框下面显示错误，发送时禁用按钮，立刻显示新留言，服务器回答后再换成真正的列表。

代码照原样跑过，一共两个文件。每个调用的细节见参考文档：[方法函数](../app.zh-CN.md#带入参和返回值的方法函数)，[在浏览器里调用](../app.zh-CN.md#在浏览器里调用方法函数)。

## Go 这边

`app/route.go`：

```go
package app

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/liustack/tifa"
)

// Message 是留言簿里的一条。
type Message struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Text string `json:"text"`
}

// 真实的应用把这些放在数据库里。
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

// SignInput 就是表单。字段用 json 标签命名。
type SignInput struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// Validate 在请求体解码之后、POST 之前运行。每个填错的字段都给一条消息，
// 一条都没有时 Err 返回 nil。
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

// POST 加上这条留言，并把它作为结果返回。
func POST(c *tifa.Ctx, in SignInput) (Message, error) {
	mu.Lock()
	defer mu.Unlock()
	lastID++
	m := Message{ID: lastID, Name: strings.TrimSpace(in.Name), Text: strings.TrimSpace(in.Text)}
	messages = append([]Message{m}, messages...)
	return m, nil
}
```

- 规则只写在 `Validate` 里。不管谁来调，它都会运行，不只是你的页面。`tifa.Fields` 回答 422，内容是 `{"error": ..., "fields": {"name": "..."}}`，键是入参的 json 名。`Validate` 返回的其他错误回答 400，错误文字就是消息，所以消息属于某一个输入框时用 `tifa.Fields`。
- 先把所有消息收集起来，最后 `return fields.Err()`，访客就能一次看到所有错误。什么都没加时 `Err` 返回 nil。
- 浏览器里也检查（`required` 属性、schema）只是省一次往返。不要因为它就省掉 Go 里的检查。

## 页面

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

// 在服务端和水合期间是 false，浏览器接管之后是 true。
const useHydrated = () =>
  useSyncExternalStore(
    () => () => {},
    () => true,
    () => false,
  )

export default function Guestbook() {
  const messages = page.useLoaderData()
  const router = useRouter()

  // action 运行期间，先把新留言显示出来。action 结束后，列表就是页面数据里的内容：
  // 重新加载带回了新留言就有它，调用失败就还是旧列表。
  const [shown, addOptimistic] = useOptimistic(messages as Message[], (current, m: Message) => [m, ...current])

  const [state, submit] = useActionState(async (_previous: FormState, data: FormData): Promise<FormState> => {
    const values = { name: String(data.get('name') ?? ''), text: String(data.get('text') ?? '') }
    addOptimistic({ id: -1, ...values, pending: true })
    try {
      await POST({ input: values })
      // 调用从不重新加载页面数据。等它重新加载完，先显示的留言就会无缝换成真的那条。
      await router.invalidate({ sync: true })
      return empty
    } catch (e) {
      if (!(e instanceof TifaError)) throw e
      // e.fields 是每个输入框一条消息，来自 Go 里的 tifa.Fields。其他错误只有 message。
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

// useFormStatus 读的是外面的那个 form，所以它必须放在单独的组件里。
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
      {/* action 结束后 React 会重置表单，所以输入框从 action 返回的值开始。 */}
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

访客点按钮之后发生的事：

1. React 在一个 transition 里，用表单数据调用你交给 `useActionState` 的函数。`useFormStatus` 报告 `pending`，按钮被禁用，显示 "Sending…"。
2. `addOptimistic` 把新留言放在列表最上面，颜色变淡。它必须在 action 里调用（见 [useOptimistic](https://react.dev/reference/react/useOptimistic)）。
3. `POST` 把入参作为 JSON 发出去。Go 拒绝时，调用抛出 `TifaError`。`e.fields` 是 `tifa.Fields` 的消息，每个输入框一条，其他错误的文字在 `e.message` 里。
4. 成功时，`router.invalidate({ sync: true })` 重新加载页面数据，页面显示出来才结束。这时 action 才结束，变淡的留言换成真的那条。见[重新加载页面数据](../app.zh-CN.md#pagetsx)。
5. 失败时 action 返回刚才输入的值和消息。先显示的留言会自己消失，因为 action 结束了，页面数据没有变。

## 要做的选择

**用 React hook 还是表单库。** 三个 hook（`useActionState`、`useFormStatus`、`useOptimistic`，见 [useActionState](https://react.dev/reference/react/useActionState) 和 [useFormStatus](https://react.dev/reference/react-dom/hooks/useFormStatus)）够写字段不多、没有浏览器端规则的表单。字段很多、要边输入边检查、或者有字段数组时，用表单库。`examples/shop` 用的是 react-hook-form 加 Zod schema（`app/projects/project+/orders/order+/ship-form.tsx`），服务端要注意什么见 [app.zh-CN.md](../app.zh-CN.md#在服务端渲染得不一样的库)。tifa 这部分对任何库都一样：捕获 `TifaError`，把 `e.fields` 交给那个库设置错误的办法。react-hook-form 是 `form.setError(name, { message })`。

**水合之前按钮保持禁用。** 用函数当 `action` 时，服务端写出的是 `action="javascript:throw new Error('React form unexpectedly submitted.')"`。脚本运行之前表单什么也发不出去，在输入框里按回车就会撞上它。`useHydrated` 让按钮在这之前保持禁用，回车也就发不出去了。

**重新加载用不用 `sync: true`。** `await router.invalidate({ sync: true })` 在页面显示新数据之后才结束，所以先显示的留言才能不闪地让位。单写 `await router.invalidate()` 会立刻结束，页面在后台更新。表单里没有先显示的内容、也不介意稍等一下时，选后者。

**要不要先显示。** 写入几乎总会成功、结果又容易猜到时（比如这里），先显示新留言值得。结果由服务器决定时（价格、排队号），不用 `useOptimistic`，靠禁用的按钮就够了。

**不是 `TifaError` 的错误。** 网络故障或你代码里的 bug，不该显示在某个输入框下面。例子里把它重新抛出，React 会把 action 抛出的错误交给最近的错误边界（见 [useActionState](https://react.dev/reference/react/useActionState)）。想让表单留在原地，就从 action 里返回一条消息。

**表单之后跳转。** 表单创建的东西有自己的页面时，让方法函数返回 `tifa.Redirect(url, 303)`。调用会通过路由器去那个页面，函数的结果是 `undefined`。

## 检查一下

```sh
go tool tifa check
go tool tifa dev
```

空表单提交，两条消息都显示。在留言里输入 201 个字符，名字保留，错误显示在留言下面。提交一条正常的，输入框清空。在 `POST` 里加一个 `time.Sleep` 把方法函数变慢，就能看到变淡的留言和 "Sending…" 按钮。
