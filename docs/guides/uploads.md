---
summary: 'How to let visitors upload a file in a Tifa app: a file field in the input of a handler, which the generated function sends as a form, the body limit and read time of the upload route, checking the type, where to keep the file, how to serve it back and how to test the handler'
read_when:
  - Adding a file upload, such as a picture, an avatar or an attachment
  - An upload fails with 400, 408, 413, 415 or 422, or the handler does not see the file
  - Deciding where uploaded files live and how the app serves them
---

English · [简体中文](uploads.zh-CN.md)

# Uploads

A page where visitors upload pictures with a title, and see them listed. The handler checks the title, the size and the kind of file, keeps the file in a folder and serves it back.

The code was run as written: it is the app `test/apps/upload` of the Tifa repository, which its end-to-end tests build and drive in a browser. The reference docs for the calls used here: [files in the input](../app.md#files-in-the-input), [body limit and read time of a route](../app.md#body-limit-and-read-time-of-a-route), [settings of the app](../config.md#settings-of-the-app).

## How a file gets to the handler

A field of type `*multipart.FileHeader` in the input of a POST, PUT or PATCH handler holds the file (`[]*multipart.FileHeader` holds several). The rest works as in [Forms](forms.md):

- The generated function takes the file as a `Blob`, a `File` from a file input included, and sends the input as a multipart form. The page calls it like any other handler function, and a refusal throws a `TifaError` with the message of each field.
- The text fields fill the input by their `json` names, the file fills its field, and then `Validate` runs. So a missing file is a message under its input, like a missing title.
- The handler opens the file with `in.File.Open()`.

## The settings and the body limit

`config.go` in the root package:

```go
// Package uploads is the root package of the app: its settings and hooks.
package uploads

import "github.com/liustack/tifa"

var (
	// UploadLimit is the largest request body of an upload.
	UploadLimit = tifa.Size("upload.limit", 4<<20, "Largest upload")
	// UploadDir is where uploaded files are kept, relative to the folder the app runs in.
	UploadDir = tifa.String("upload.dir", "data/uploads", "Folder for uploaded files")
)
```

They are also the flag `--upload-limit`, the variable `UPLOAD_LIMIT` and so on, like every [setting of the app](../config.md#settings-of-the-app). `4MiB` and `4<<20` mean the same. `hooks.go` makes the folder when the app starts:

```go
package uploads

import (
	"context"
	"os"
)

func OnStart(context.Context) error {
	return os.MkdirAll(UploadDir, 0o755)
}
```

Every request body is limited to `server.body_limit`, 1 MiB by default, and every request has the 10 seconds of `server.read_timeout` to arrive. Raise both for the upload route only, so the other routes keep the small ones. At the top of `app/photos/route.go`:

```go
// BodyLimit lets the requests of this route be as large as upload.limit. The other routes keep
// server.body_limit.
var BodyLimit = uploads.UploadLimit

// ReadTimeout gives a phone on a slow line the time to send a large picture.
const ReadTimeout = 5 * time.Minute
```

- A body over `BodyLimit` is answered with 413 and the handler does not run. The limit counts the whole body: the file, the other fields and the multipart framing. A file a little under the limit can still be refused.
- `ReadTimeout` counts from the moment the route is matched. A body still coming when it is over is answered with 408. The answer gets `server.write_timeout` after it.
- Both hold for the requests the method handlers of this `route.go` answer, and the GET of the page keeps the settings. A variable is read once when the app starts, so `--upload-limit 8MiB` works.
- A proxy or CDN in front of the app has a body limit and timeouts of its own. Raise them to match, or the visitor gets its error and not yours.

### How much memory an upload takes

The form is parsed with the budget of Go's `mime/multipart`. Up to 32 MiB of file content stays in memory, and the text fields and the names and headers of the parts share one budget of about 42 MiB with it. A file that does not fit goes to a temporary file, which Tifa removes when the request ends. A body limit above that is fine: the rest goes to disk.

The budget is not the peak. A file that ends on disk is first read into a buffer of up to 32 MiB, which grows as it fills, so one large file can take about 64 MiB for a moment, and each upload at the same time takes its own. On a machine with little memory, let only a few uploads read their form at once. The rest of `hooks.go`, which also imports `github.com/liustack/tifa`:

```go
// parsing lets two uploads read their form at a time. The others wait here, before their body is
// read, so a burst of uploads does not hold the memory of all of them at once.
var parsing = make(chan struct{}, 2)

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if c.Request().Method == "POST" && c.Pattern() == "/photos" {
		select {
		case parsing <- struct{}{}:
			defer func() { <-parsing }()
		case <-c.Context().Done():
			return c.Context().Err()
		}
	}
	return next(c)
}
```

The time an upload waits here counts toward its `ReadTimeout`, which starts when the route is matched, so leave room for the wait.


## The handler

The rest of `app/photos/route.go`:

```go
package photos

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/liustack/tifa"

	"uploads"
)

// (BodyLimit and ReadTimeout, as above)

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

// UploadInput is the form: a title and the picture.
type UploadInput struct {
	Title string                `json:"title"`
	File  *multipart.FileHeader `json:"file"`
}

func (in UploadInput) Validate() error {
	fields := tifa.Fields{}
	if strings.TrimSpace(in.Title) == "" {
		fields["title"] = "Give the picture a title."
	}
	if in.File == nil {
		fields["file"] = "Choose a picture."
	}
	return fields.Err()
}

// The kinds of picture the app accepts, by what the bytes say they are.
var extensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// POST stores the picture. The framework has refused a body over BodyLimit with 413 and run
// Validate before this runs.
func POST(c *tifa.Ctx, in UploadInput) (Photo, error) {
	file, err := in.File.Open()
	if err != nil {
		return Photo{}, err
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

- **Check what the file is, not what it says it is.** `http.DetectContentType` looks at the first 512 bytes. The file name and the `Content-Type` of the part (`in.File.Header`) come from the browser, so anyone can write anything there. Accept a short list and refuse the rest. This tells you the kind of the file, not that its content is harmless. It keeps scripts and HTML from being stored and served as pictures.
- **Make the name yourself.** The example never puts a client's file name (`in.File.Filename`) in a path, which closes the door on `../` names, names that overwrite another file, and odd characters. If you want to show the original name, keep it in the database.
- **Copy the file before the handler returns.** The temporary file behind `in.File` is removed when the request ends, so a goroutine that outlives the request cannot read it.
- **Limits on the count and on the person.** The body limit caps one request. How many files one visitor may keep, and who may upload at all, is your rule: read `tifa.Session` (see [Sign in](sign-in.md)) at the top of the handler.
- A form field that holds text in place of the file is answered with 400, and a JSON body with 415. The generated function never sends either.

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
```

- `POST` is the generated function of the handler. Its input type has `file: Blob | null`, so a `File` from the input goes in as it is.
- A file input left empty still gives `FormData` a `File`, with an empty name. The page sends `null` then, and `Validate` asks for a picture.
- A refusal throws a `TifaError`. `e.fields` holds the messages of `tifa.Fields`, by the `json` names of the input. A 413 has no fields, so the page gives its own message.
- The browser's own `accept` filter is a hint for the file picker and not a check. The Go side decides.
- A call to your own site is a same-origin write, so the [cross-site check](../config.md#cross-site-writes) lets it through. A form on another site is answered with 403.
- A call does not reload the page data, so the page calls `router.invalidate({ sync: true })` after a good answer.

## Testing the handler

`tifatest.File` gives the `*multipart.FileHeader` of a file, so a test passes it in the input as the server would. `app/photos/route_test.go`:

```go
package photos

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"

	"uploads"
)

func TestValidate(t *testing.T) {
	in := UploadInput{Title: "Sea", File: tifatest.File(t, "sea.png", []byte("\x89PNG\r\n\x1a\n"))}
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	in.File = nil
	var fields tifa.Fields
	if err := in.Validate(); !errors.As(err, &fields) || fields["file"] == "" {
		t.Fatalf("no file: %v", err)
	}
}

func TestPOSTRefusesText(t *testing.T) {
	uploads.UploadDir = t.TempDir()
	c, _ := tifatest.NewCtx(t, httptest.NewRequest("POST", "/photos", nil))
	_, err := POST(c, UploadInput{Title: "Text", File: tifatest.File(t, "a.png", []byte("just text"))})
	var fields tifa.Fields
	if !errors.As(err, &fields) || fields["file"] != "Only JPEG, PNG, WebP and GIF pictures." {
		t.Fatalf("text named a.png: %v", err)
	}
}
```

The test calls the handler directly: `Validate`, the body limit and the read time are the server's part, see [Testing](../app.md#testing).

## Decisions

**Where files live.** A folder on the machine, as here, is the least to run. It needs a volume that survives deploys, and it is backed up with the rest of the data (see [Deploying](../deploy.md)). One process on one machine is the target of Tifa, so that is usually enough. If you have more than one machine, or the files are large, keep them in object storage and let the handler write to it. The checks above stay the same. Serve them from there or redirect to a signed link.

**How big.** Pick the smallest limit that fits the use. A profile picture needs 1 or 2 MiB. Show the limit in the form, and check `file.size` in the browser to give a quick message before sending. The server limit is still the one that counts.

**How long.** `ReadTimeout` is how long the slowest visitor you serve may take to send the largest file you accept. 4 MiB over a line of 1 Mbit/s takes about 34 seconds, so 5 minutes leaves room. A long time lets a slow client hold a connection that long, so do not give it to routes that do not need it.

**What is accepted.** A list of kinds, decided by the bytes. For documents, do the same with their own types and serve them with `Content-Disposition: attachment` so the browser saves them and does not open them.

## Check it

```sh
go tool tifa check
go tool tifa dev
```

Open `/photos`. Send without a title or without a file and each message shows under its input. Send a text file named `a.png` and it is refused by its bytes. Send a file over the limit and the form says it is too large. Send a picture and it appears in the list, served from `/photos/{id}`.
