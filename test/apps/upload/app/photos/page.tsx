import { useRouter } from '@tanstack/react-router'
import { useState, useSyncExternalStore, type FormEvent } from 'react'
import { TifaError } from 'tifa/client'
import { page, POST } from './route.gen'

const useHydrated = () =>
  useSyncExternalStore(
    () => () => {},
    () => true,
    () => false,
  )

type Failure = { fields: Record<string, string>; message?: string }

export default function Photos() {
  const photos = page.useLoaderData()
  const router = useRouter()
  const hydrated = useHydrated()
  const [failure, setFailure] = useState<Failure>({ fields: {} })
  const [pending, setPending] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const data = new FormData(form)
    const file = data.get('file')
    setPending(true)
    try {
      // An empty file input gives a File with no name: send no file then, and Validate says so.
      await POST({ input: { title: String(data.get('title') ?? ''), file: file instanceof File && file.name !== '' ? file : null } })
      setFailure({ fields: {} })
      form.reset()
      // The upload does not reload the page data. Ask for it.
      await router.invalidate({ sync: true })
    } catch (e) {
      if (!(e instanceof TifaError)) {
        setFailure({ fields: {}, message: 'Could not reach the server.' })
        return
      }
      const message = e.status === 413 ? 'That file is too large.' : e.message
      setFailure({ fields: e.fields, message: Object.keys(e.fields).length === 0 ? message : undefined })
    } finally {
      setPending(false)
    }
  }

  return (
    <>
      <h1>Photos</h1>
      <form id="upload" onSubmit={submit} noValidate>
        <p>
          <label htmlFor="title">Title</label>
          <input id="title" name="title" />
          {failure.fields.title && <span className="error">{failure.fields.title}</span>}
        </p>
        <p>
          <label htmlFor="file">Picture</label>
          <input id="file" name="file" type="file" accept="image/jpeg,image/png,image/webp,image/gif" />
          {failure.fields.file && <span className="error">{failure.fields.file}</span>}
        </p>
        {failure.message && <p role="alert">{failure.message}</p>}
        <button type="submit" disabled={pending || !hydrated}>
          {pending ? 'Uploading…' : 'Upload'}
        </button>
      </form>
      <ul id="photos">
        {photos.map((p) => (
          <li key={p.id}>
            <img src={`/photos/${p.id}`} alt={p.title} width={160} /> {p.title}
          </li>
        ))}
      </ul>
    </>
  )
}
