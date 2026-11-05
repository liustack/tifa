---
summary: 'How to build a form in a Tifa app: post it to a Go method handler through the generated function, show the field errors from tifa.Fields next to each input, disable the button while it sends, reload the page data after success and show the new row before the answer comes (React 19 useActionState, useFormStatus and useOptimistic)'
read_when:
  - Adding a form that creates or changes something, and you want the whole path from the input to the error message on screen
  - A form shows no field errors, the button can be clicked twice, or the list does not change after the form was sent
  - Choosing between React 19 form hooks and a form library such as react-hook-form
---

English · [简体中文](forms.zh-CN.md)

# Forms

A guestbook. The visitor types a name and a message, the Go side checks them and keeps the message, and the page shows it. By the end the form shows an error under each wrong input, disables its button while it sends, shows the new message at once and loads the real list when the server has answered.

The code was run as written. It is two files. The reference docs say what each call does: [method handlers](../app.md#method-handlers-with-input-and-result), [calling them from the browser](../app.md#calling-method-handlers-from-the-browser).

## The Go side

`app/route.go`:

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

- `Validate` is the one place the rules live. It runs for every caller, not only your page. `tifa.Fields` answers 422 with `{"error": ..., "fields": {"name": "..."}}`, and the keys are the json names of the input. Any other error from `Validate` answers 400 with its text as the message, so use `tifa.Fields` when a message belongs to one input.
- Collect all the messages first and end with `fields.Err()`. The visitor then sees every mistake at once. `Err` is nil when you added nothing.
- Checking in the browser too (a `required` attribute, a schema) only saves a round trip. Do not skip the Go check because of it.

## The page

`app/page.tsx`:

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

What happens when the visitor presses the button:

1. React calls the function you gave `useActionState` with the form data, inside a transition. `useFormStatus` reports `pending`, so the button is disabled and says "Sending…".
2. `addOptimistic` puts the new message on top of the list, dimmed. It must be called inside the action (see [useOptimistic](https://react.dev/reference/react/useOptimistic)).
3. `POST` sends the input as JSON. If Go refuses it, the call throws a `TifaError`. `e.fields` holds the messages of the `tifa.Fields`, one per input, and `e.message` is the text for any other error.
4. On success, `router.invalidate({ sync: true })` loads the page data again and resolves when the page shows it. Only then does the action end, and the dimmed message is replaced by the real one. See [reloading page data](../app.md#pagetsx).
5. On failure the action returns the typed values and the messages. The optimistic message goes away by itself, because the action ended without new page data.

## Decisions

**React hooks or a form library.** The three hooks (`useActionState`, `useFormStatus`, `useOptimistic`, see [useActionState](https://react.dev/reference/react/useActionState) and [useFormStatus](https://react.dev/reference/react-dom/hooks/useFormStatus)) cover a form with a few fields and no client-side rules. For many fields, rules that run as the visitor types, or arrays of fields, use a library. `examples/shop` uses react-hook-form with a Zod schema (`app/projects/project+/orders/order+/ship-form.tsx`), and [app.md](../app.md#libraries-that-render-differently-on-the-server) lists what to do for it on the server. The Tifa part is the same for any library: catch `TifaError` and give `e.fields` to the library's way of setting errors. For react-hook-form that is `form.setError(name, { message })`.

**Keep the button disabled until hydration.** With a function as `action`, the server writes `action="javascript:throw new Error('React form unexpectedly submitted.')"`. Until the scripts run, the form cannot send anything, and Enter in a field would hit that. `useHydrated` keeps the button disabled before that, which also stops the Enter key.

**Reload with `sync: true` or not.** `await router.invalidate({ sync: true })` resolves once the page shows the new data, which is what lets the optimistic message give way without a flicker. A plain `await router.invalidate()` resolves at once and the page updates in the background. Choose that when the form has nothing optimistic and you do not mind the short wait.

**Optimistic or not.** Showing the message before the answer is worth it when the write almost always works and the result is easy to guess, as here. When the server decides the result (a price, a queue number), leave `useOptimistic` out and rely on the disabled button.

**Errors that are not `TifaError`.** A network failure or a bug in your code is not something to show under an input. The example rethrows it, and React gives a thrown error of an action to the nearest error boundary (see [useActionState](https://react.dev/reference/react/useActionState)). Return a message from the action instead if you want the form to stay.

**Redirecting after the form.** If a form creates something with a page of its own, return `tifa.Redirect(url, 303)` from the handler. The call then goes to that page through the router, and the function resolves to `undefined`.

## Check it

```sh
go tool tifa check
go tool tifa dev
```

Send the form empty and both messages show. Type 201 characters in the message and the name stays, the error shows under the message. Send a good one and the field clears. Slow the handler down (a `time.Sleep` in `POST`) to see the dimmed message and the "Sending…" button.
