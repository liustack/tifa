---
summary: 'How to let visitors upload a file in a tifa app: why the generated function cannot send a file and a plain fetch with FormData does, the Go handler that reads the text fields and the file, the size limit, checking the type, where to keep the file and how to serve it back'
read_when:
  - Adding a file upload, such as a picture, an avatar or an attachment
  - An upload fails with 413, 415 or 400, or the handler does not see the file
  - Deciding where uploaded files live and how the app serves them
---

English · [简体中文](uploads.zh-CN.md)

# Uploads

A page where visitors upload pictures with a title, and see them listed. The handler checks the title, the size and the kind of file, keeps the file in a folder and serves it back.

The code was run as written. The reference docs for the calls used here: [method handlers](../app.md#method-handlers-with-input-and-result), [Ctx](../app.md#ctx), [settings of the app](../config.md#settings-of-the-app).

## What works today

The generated function of a handler cannot send a file. It sends its input as JSON (or as a query string for GET and DELETE), and a file is not JSON. Sending files through the generated function is not built yet. So an upload takes two parts:

- The Go handler is an ordinary method handler. It accepts a multipart form (`Content-Type: multipart/form-data`), as well as JSON and URL-encoded forms. The text fields fill the input struct by their `json` names, so `Validate`, `tifa.Fields` and the 422 answer work as in [Forms](forms.md). A form value fills only fields that are strings, numbers, bools or slices of them.
- The page sends the request with `fetch` and a `FormData`. You read the answer yourself, because the `TifaError` of the generated functions is thrown only by them.

The file is not part of the input struct. The handler reads it from the request with `c.Request().FormFile("file")`.

## The settings and the body limit

`config.go` in the root package:

```go
package uploads

import "github.com/liustack/tifa"

var (
	// UploadLimit is the largest request body of an upload.
	UploadLimit = tifa.Size("upload.limit", 4<<20, "Largest upload")
	// UploadDir is where uploaded files are kept, relative to the folder the app runs in.
	UploadDir = tifa.String("upload.dir", "data/uploads", "Folder for uploaded files")
)
```

They are also the flag `--upload-limit`, the variable `UPLOAD_LIMIT` and so on, like every [setting of the app](../config.md#settings-of-the-app). `4MiB` and `4<<20` mean the same.

Every request body is limited to `server.body_limit`, 1 MiB by default. A bigger one is answered with 413 and the handler does not run. Raise the limit for the upload route only, so the other routes keep the small one. `hooks.go`:

```go
package uploads

import (
	"context"
	"os"

	"github.com/liustack/tifa"
)

func OnStart(context.Context) error {
	return os.MkdirAll(UploadDir, 0o755)
}

// OnRequest gives the upload route a body limit of its own. Set it before anything reads the body.
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if c.Request().Method == "POST" && c.Pattern() == "/photos" {
		if err := c.SetBodyLimit(UploadLimit); err != nil {
			return err
		}
	}
	return next(c)
}
```

- The limit counts the whole body: the file, the other fields and the multipart framing. A file a little under the limit can still be refused.
- With a typed input the framework decodes the body before your handler runs, so the limit has to change earlier, in `OnRequest`. A handler that reads the body itself can call `c.SetBodyLimit` at its top, as long as nothing has read the body yet (see below).
- A multipart body is read with at most 32 MiB in memory. What is over that goes to temporary files, which tifa removes when the request ends. A limit above that is fine, but a few uploads at once cost memory, so keep it as low as the use needs.
- A proxy or CDN in front of the app has a body limit of its own. Raise it to match, or the visitor gets its error and not yours.
- A large file from a phone on a slow line can take longer than the 10 seconds of `server.read_timeout`, and the upload then fails halfway. Lift the deadlines of the upload route in the same `if`, before the body is read, and leave the settings short for the rest (see [Long requests](../config.md#long-requests)):

  ```go
  rc := http.NewResponseController(c.Writer())
  if err := rc.SetReadDeadline(time.Now().Add(5 * time.Minute)); err != nil {
  	return err
  }
  // The answer is written after the body, so it needs the time too.
  if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil {
  	return err
  }
  ```

## The handler

`app/photos/route.go`:

```go
package photos

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/liustack/tifa"

	"uploads"
)

// Photo is one stored picture. ID is also its file name.
type Photo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func Load(c *tifa.Ctx) ([]Photo, error) {
	entries, err := os.ReadDir(uploads.UploadDir)
	if err != nil {
		return nil, err
	}
	// Newest first: the names are random, so sort by modification time.
	sort.Slice(entries, func(i, j int) bool {
		a, _ := entries[i].Info()
		b, _ := entries[j].Info()
		return a != nil && b != nil && a.ModTime().After(b.ModTime())
	})
	photos := []Photo{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".title") || e.IsDir() {
			continue
		}
		title, _ := os.ReadFile(filepath.Join(uploads.UploadDir, e.Name()+".title"))
		photos = append(photos, Photo{ID: e.Name(), Title: string(title)})
	}
	return photos, nil
}

// UploadInput holds the text fields of the form. The file is read from the request itself.
type UploadInput struct {
	Title string `json:"title"`
}

func (in UploadInput) Validate() error {
	if strings.TrimSpace(in.Title) == "" {
		return tifa.Fields{"title": "Give the picture a title."}
	}
	return nil
}

// The kinds of picture the app accepts, by what the bytes say they are.
var extensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// POST stores the file of the "file" part. The framework has read the text fields into in and
// refused a body over upload.limit with 413 before this runs.
func POST(c *tifa.Ctx, in UploadInput) (Photo, error) {
	file, _, err := c.Request().FormFile("file")
	if err != nil {
		return Photo{}, tifa.Fields{"file": "Choose a picture."}
	}
	defer file.Close()

	// Decide the type from the first bytes, not from the name or the Content-Type the browser sent.
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return Photo{}, err
	}
	ext, ok := extensions[http.DetectContentType(head[:n])]
	if !ok {
		return Photo{}, tifa.Fields{"file": "Only JPEG, PNG, WebP and GIF pictures."}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Photo{}, err
	}

	// The name is ours: random, with the extension we decided. Nothing from the client is in the path.
	id, err := newID()
	if err != nil {
		return Photo{}, err
	}
	name := id + ext
	dst, err := os.OpenFile(filepath.Join(uploads.UploadDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return Photo{}, err
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.Remove(dst.Name())
		return Photo{}, err
	}
	if err := dst.Close(); err != nil {
		return Photo{}, err
	}
	title := strings.TrimSpace(in.Title)
	if err := os.WriteFile(dst.Name()+".title", []byte(title), 0o644); err != nil {
		return Photo{}, err
	}
	return Photo{ID: name, Title: title}, nil
}

func newID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

The title is kept in a small file next to the picture so the example needs no database. A real app keeps the title, the owner and the file name in a table.

- **Check what the file is, not what it says it is.** `http.DetectContentType` looks at the first 512 bytes. The file name and the `Content-Type` of the part come from the browser, so anyone can write anything there. Accept a short list and refuse the rest. This tells you the kind of the file, not that its content is harmless. It keeps scripts and HTML from being stored and served as pictures.
- **Make the name yourself.** The example never puts a client's file name in a path, which closes the door on `../` names, names that overwrite another file, and odd characters. If you want to show the original name, keep it in the database.
- **Limits on the count and on the person.** The body limit caps one request. How many files one visitor may keep, and who may upload at all, is your rule: read `tifa.Session` (see [Sign in](sign-in.md)) at the top of the handler.

### A handler that writes its own response

If you want to read the form yourself, take only the Ctx. `c.Bind` decodes the text fields (by the `form` tag, then `json`, then the Go name) and reports 413 for a body over the limit, and you write the answer:

```go
package avatar

import (
	"io"

	"github.com/liustack/tifa"

	"uploads"
)

type form struct {
	Name string `json:"name"`
}

func POST(c *tifa.Ctx) error {
	// Nothing has read the body yet, so the limit can still change here.
	if err := c.SetBodyLimit(uploads.UploadLimit); err != nil {
		return err
	}
	var in form
	if err := c.Bind(&in); err != nil { // 413 for a body over the limit, 400 for a broken one
		return err
	}
	file, header, err := c.Request().FormFile("file")
	if err != nil {
		return tifa.Error{Status: 400, Message: "Choose a file."}
	}
	defer file.Close()
	n, err := io.Copy(io.Discard, file)
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]any{"name": in.Name, "bytes": n, "filename": header.Filename})
}
```

Here there is no `Validate`, so you check the fields yourself. The generated function for it takes no input and gives back the `Response`, so it is no help for a file either.

## Serving the files

Do not put uploads in `public/`. The public folder is copied when you build, and the app serves only the files that were in it then. A file added while the app runs is not found.

Serve them from a handler. `app/photos/id+/route.go` answers `/photos/{id}`:

```go
package id

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/liustack/tifa"

	"uploads"
)

// Names we wrote: 24 hex characters and one of the known extensions.
var valid = regexp.MustCompile(`^[0-9a-f]{24}\.(jpg|png|webp|gif)$`)

func GET(c *tifa.Ctx) error {
	name := c.Param("id")
	if !valid.MatchString(name) {
		return tifa.ErrNotFound
	}
	f, err := os.Open(filepath.Join(uploads.UploadDir, name))
	if err != nil {
		return tifa.ErrNotFound
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	w := c.Writer()
	// The name never changes its content, so browsers may keep it.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// ServeContent picks the Content-Type from the extension and handles Range and conditional requests.
	http.ServeContent(w, c.Request(), name, info.ModTime(), f)
	return nil
}
```

- The route accepts only names the app wrote, so the title files and any path trick are a 404. Never join a name from the URL to a folder without a check like this.
- `Content-Type` comes from the extension we chose after looking at the bytes, and `nosniff` stops a browser from guessing another type.
- Files that only their owner may see need a check against `tifa.Session` in this handler, and no `public` in `Cache-Control`.
- The `<img src="/photos/...">` in the page goes to this handler like any other request.

## The page

`app/photos/page.tsx`:

```tsx
import { useRouter } from '@tanstack/react-router'
import { useState, useSyncExternalStore, type FormEvent } from 'react'
import { page, type POSTOutput } from './route.gen'

const useHydrated = () =>
  useSyncExternalStore(
    () => () => {},
    () => true,
    () => false,
  )

type Failure = { fields: Record<string, string>; message?: string }

// The generated POST function sends JSON, so a file goes through fetch with a FormData.
// Do not set Content-Type: the browser writes it, with the boundary.
async function upload(form: HTMLFormElement): Promise<POSTOutput | Failure> {
  const response = await fetch('/photos', { method: 'POST', body: new FormData(form) })
  if (response.ok) return (await response.json()) as POSTOutput
  // A handler error is {"error": "...", "fields": {...}}. A proxy may answer with something else.
  const body = (await response.json().catch(() => ({}))) as { error?: string; fields?: Record<string, string> }
  const message = response.status === 413 ? 'That file is too large.' : (body.error ?? 'The upload failed.')
  return { fields: body.fields ?? {}, message: body.fields ? undefined : message }
}

export default function Photos() {
  const photos = page.useLoaderData()
  const router = useRouter()
  const hydrated = useHydrated()
  const [failure, setFailure] = useState<Failure>({ fields: {} })
  const [pending, setPending] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    setPending(true)
    try {
      const result = await upload(form)
      if ('fields' in result) {
        setFailure(result)
        return
      }
      setFailure({ fields: {} })
      form.reset()
      // The upload does not reload the page data. Ask for it.
      await router.invalidate({ sync: true })
    } catch {
      setFailure({ fields: {}, message: 'Could not reach the server.' })
    } finally {
      setPending(false)
    }
  }

  return (
    <>
      <h1>Photos</h1>
      <form onSubmit={submit} noValidate>
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
      <ul>
        {photos.map((p) => (
          <li key={p.id}>
            <img src={`/photos/${p.id}`} alt={p.title} width={160} /> {p.title}
          </li>
        ))}
      </ul>
    </>
  )
}
```

- `POSTOutput` is the generated type of the handler's result, so `Photo` stays in step with Go.
- Do not set the `Content-Type` header yourself. The browser adds the boundary only when it writes the header.
- The browser's own `accept` filter is a hint for the file picker and not a check. The Go side decides.
- A `fetch` to your own site is a same-origin write, so the [cross-site check](../config.md#cross-site-writes) lets it through. A form on another site is answered with 403.
- A call you write yourself does not reload the page data, so the page calls `router.invalidate({ sync: true })` after a good answer.

## Decisions

**Where files live.** A folder on the machine, as here, is the least to run. It needs a volume that survives deploys, and it is backed up with the rest of the data (see [Deploying](../deploy.md)). One process on one machine is the target of tifa, so that is usually enough. If you have more than one machine, or the files are large, keep them in object storage and let the handler write to it. The checks above stay the same. Serve them from there or redirect to a signed link.

**How big.** Pick the smallest limit that fits the use. A profile picture needs 1 or 2 MiB. Show the limit in the form, and check `file.size` in the browser to give a quick message before sending. The server limit is still the one that counts.

**What is accepted.** A list of kinds, decided by the bytes. For documents, do the same with their own types and serve them with `Content-Disposition: attachment` so the browser saves them and does not open them.

## Check it

```sh
go tool tifa check
go tool tifa dev
```

Open `/photos`. Send without a title or without a file and each message shows under its input. Send a text file named `a.png` and it is refused by its bytes. Send a file over the limit and the form says it is too large. Send a picture and it appears in the list, served from `/photos/{id}`.
