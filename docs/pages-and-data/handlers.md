---
summary: 'POST and other method handlers: typed input, files, body limit, calling from a page'
audience: app
order: 4
read_when:
  - Writing an API route or a method handler in route.go
  - Taking files in a method handler, or giving a route a body limit or read time of its own with `c.SetBodyLimit` and `c.SetReadTimeout`
  - Calling a method handler from a page, or the page does not show what a write changed
  - Changing the rules in internal/analyze and you need to know what users were promised
---

# Method handlers

## Method handlers with input and result

A method handler may take an input after the Ctx and return a result before the error. The framework decodes the input and writes the result:

```go
func POST(c *tifa.Ctx) error                       // writes its own response
func POST(c *tifa.Ctx, in ShipInput) error         // answers 204 on success
func POST(c *tifa.Ctx) (Shipment, error)           // answers 200 with the result as JSON
func POST(c *tifa.Ctx, in ShipInput) (Shipment, error)
```

- The input is a struct, not a pointer to one. POST, PUT and PATCH decode it from the request body: JSON, or a URL-encoded or multipart form. A form fills only fields that are strings, numbers, bools or slices of them, and the files (see [Files in the input](#files-in-the-input)), and a form value for any other field is answered with 400. A form decodes each value by its kind and never calls `MarshalText` or `UnmarshalText`, so a type with one of them, or a slice of such types, is such another field, and so is a slice type with one of its own, such as `type Tags []string`, which TypeScript takes for one string. GET and DELETE decode it from the query string, so its fields may only be strings, numbers, bools or slices of them, by the same rule, with no embedded fields, and no `MarshalText` or `UnmarshalText` on the input type itself. The build checks this, and `c.BindQuery` returns an error for any other field whatever the query holds.
- Fields are named by their `json` tags, in a form body and the query string too. A `form` tag fails the build, also in a struct passed to `c.Bind` or `c.BindQuery`. Path parameters, headers and cookies still come from `c`.
- The input and the result follow the [JSON rules](../reference/json.md), the same as the result of `Load`. The input must also read back from the JSON its TypeScript type describes: a type that is a string because of `MarshalText` needs `UnmarshalText([]byte) error` too, a type with `UnmarshalText` must be a string in TypeScript unless it is a map key (a key is always a JSON string), and `UnmarshalJSON` is not accepted (except on `time.Time`).
- When the input type has a method `Validate` (it must be `func() error` or `func(c *tifa.Ctx) error`), it runs after decoding and before the handler. `nil` goes on. With the Ctx, `Validate` can read the request: `c.Language()` for a message in the visitor's language (see [Validation messages](../guides/i18n.md#validation-messages)), `c.Param`, `c.Query`, `c.QueryValues`, `c.Header`, `c.Cookie`, `c.Path()`, `c.Context()`, `c.Request()`, and `tifa.Session`. It checks and answers with its error only, so the build fails when `Validate`, or a function it passes the Ctx to, writes the response (`c.JSON`, `c.Text`, `c.NoContent`, `c.SetHeader`, `c.SetCookie`, `c.Writer`), reads the body again (`c.Bind`, `c.BindQuery`, `c.SetBodyLimit`, `c.SetReadTimeout`), calls `tifa.SetSession` or `tifa.ClearSession`, or calls `tifa.Use`. The Ctx may go where it may in `Load` (see [How Load may use Ctx](loading-data.md#how-load-may-use-ctx)). A `tifa.Error` or `tifa.Fields` is answered as is, so return `tifa.Fields` to mark single fields. Collect them in a `tifa.Fields{}` and end with `return fields.Err()`, which is nil when nothing was added (an empty `tifa.Fields` is still an error). Any other error is an error of the app, like one the handler returns: it goes to `OnError`, is answered with 500 when there is none, and the user never sees its text (see [What an error you return becomes](errors.md#what-an-error-you-return-becomes)).
- A POST, PUT, PATCH or DELETE that a browser sends from another site, a form or multipart included, is answered with 403 before the handler runs, so a handler needs no check of its own for it. `server.trusted_origins` and `server.cross_site_paths` let chosen origins and paths through (see [Cross-site writes](../build-and-deploy/config.md#cross-site-writes)).
- A body or query string that does not decode is answered with 400 (413 for a body over the limit, 415 for another content type or none, or for JSON to an input that holds files, 408 for a body that does not arrive in time), and the handler does not run.
- A handler that writes its own response decodes input with `c.Bind(&in)` for the body and `c.BindQuery(&in)` for the query string. These name a field by its `json` tag, then its Go name, as a typed input does. A `form` tag in a struct passed to them fails the build, as it does for a typed input (a value the build cannot see the type of, such as one behind an interface, is not checked, and the tag is ignored). `c.Bind` fills `*multipart.FileHeader` and `[]*multipart.FileHeader` fields from a multipart form too.

### Files in the input

A field of the input of a POST, PUT or PATCH handler of type `*multipart.FileHeader` or `[]*multipart.FileHeader` holds files of an upload, and so does a named type whose underlying type is one of them, such as `type Photos []*multipart.FileHeader`. The generated function then sends the input as a multipart form, so a page uploads a file the way it sends any other input:

```go
package photos

type UploadInput struct {
	Title  string                  `json:"title"`
	Photo  *multipart.FileHeader   `json:"photo"`
	Extras []*multipart.FileHeader `json:"extras"`
}

func (in UploadInput) Validate() error {
	if in.Photo == nil {
		return tifa.Fields{"photo": "Choose a picture."}
	}
	return nil
}

func POST(c *tifa.Ctx, in UploadInput) (Photo, error) {
	f, err := in.Photo.Open()
	if err != nil {
		return Photo{}, err
	}
	defer f.Close()
	// check the bytes, copy the file where it is kept
}
```

```tsx
const file = form.get('photo') // a File from <input type="file">
await POST({ input: { title, photo: file instanceof File ? file : null, extras: [] } })
```

- In TypeScript a `*multipart.FileHeader` is `Blob | null` and a `[]*multipart.FileHeader` is `Blob[]`. A `File` is a `Blob`, and its name goes with it. A field with `omitempty` or `omitzero` is optional.
- `*multipart.FileHeader` gets the first file of its name and stays `nil` when there is none. The slice gets every file of the name, `nil` for none. An empty text value of the name, which a browser sends for a file input left empty, counts as no file. A text value with something in it is answered with 400.
- Such an input comes only as a multipart form or a URL-encoded one (which can hold no file): a JSON body is answered with 415. Its other fields come as text, so they follow the rules of a GET input: strings, numbers, bools or slices of them, no embedded fields, and no `MarshalText` or `UnmarshalText` on the input type, on a field or on the element of a field.
- A file can only be a field of the input itself. A file in a GET or DELETE input, in a nested struct, a map or an array, in a result, in the data of `Load` or a `Layout`, or in the session fails the build. So does a file type with a `MarshalText` or `UnmarshalText` method, which TypeScript would take for a string.
- The name, the size and the `Content-Type` of a file (`in.Photo.Header.Get("Content-Type")`) are what the client sent. Decide what a file is from its bytes, and make the name you keep it under yourself, see [Uploads](../guides/uploads.md).
- The form is parsed with the budget of Go's `mime/multipart`: up to 32 MiB of file content stays in memory, sharing about 42 MiB with the text fields and the names and headers of the parts, and a file that does not fit goes to a temporary file, which Tifa removes when the request ends. The peak can be higher than the budget: a file that ends on disk is first read into a growing buffer of up to 32 MiB, so one large file can take about 64 MiB for a moment, and each upload at the same time takes its own. On a machine with little memory, let only a few uploads parse at once in `OnRequest`, see [Uploads](../guides/uploads.md#how-much-memory-an-upload-takes). Copy what you keep before the handler returns, and do not hand the `*multipart.FileHeader` to a goroutine that outlives the request.

### Body limit and read time of a route

Every request body is limited to `server.body_limit`, and every request has `server.read_timeout` to send it. A route that takes uploads needs more of both. Set them in `OnRequest`, for the requests that need them, before anything reads the body. Match the method too: a GET of a page has no body to wait for.

```go
// hooks.go
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if c.Pattern() == "/photos" && c.Request().Method == http.MethodPost {
		if err := c.SetBodyLimit(100 << 20); err != nil {
			return err
		}
		if err := c.SetReadTimeout(5 * time.Minute); err != nil {
			return err
		}
	}
	return next(c)
}
```

- `c.SetBodyLimit(n)` is a number of bytes. A body over it is answered with 413 before the handler runs. The limit counts the whole body: the files, the other fields and the multipart framing. Take the number from an [option of the app](../build-and-deploy/config.md#options-of-the-app) if it should change without a build: `c.SetBodyLimit(int64(UploadLimit))` with `UploadLimit = tifa.Size("upload.limit", 4<<20, "Largest upload")`.
- `c.SetReadTimeout(d)` gives the body `d` from now to arrive, and the answer `server.write_timeout` after it, since it is written once the body is in. A body still coming when the time is over is answered with 408. A request with no body, such as a GET, keeps its deadlines and the call does nothing.
- Both return an error once the body is being read, and `SetReadTimeout` also when `d` is not over 0 or when the response cannot reach the connection, such as behind a middleware of `tifa.SetHTTPHandler` that does not unwrap it. Check the error and return it.

## Where a write goes

A folder has one handler for each method, so a write is a method and a folder. Three shapes cover what apps need:

| The write | Where it goes | Such as |
| --- | --- | --- |
| Changes fields of a thing | The `PATCH` of the folder of the thing | `PATCH /orders/42` with the fields to change |
| Makes a new thing below another | The `POST` of a folder for those things | `POST /orders/42/notes` |
| An operation with a name of its own | A folder with that name, and its `POST` | `POST /orders/42/ship` |

- **One `PATCH` changes any of the fields.** A field of the input with `omitempty` or `omitzero` is optional in TypeScript, and a pointer field tells a field that was left out (`nil`) from an empty one.
- **An operation gets a folder when it has rules of its own:** who may do it, when it is allowed, or what else it sets off. Shipping an order is allowed once, changes the status of the order and may send a mail, so it is `ship/`, not one more field of the `PATCH`.
- **The folder is small.** It is a Go package with a `route.go` of a few lines that calls the domain:

  ```go
  // app/orders/order+/ship/route.go
  package ship

  type Input struct {
  	Carrier    string `json:"carrier"`
  	TrackingNo string `json:"trackingNo"`
  }

  func POST(c *tifa.Ctx, in Input) (orders.Order, error) {
  	return orders.Ship(c.Context(), c.Param("order"), in.Carrier, in.TrackingNo)
  }
  ```

  The page imports its function under a name that says what it does: `import { POST as shipOrder } from './ship/route.gen'`.

Each write then has an address, an input type and a result type of its own. `OnRequest` can decide who may do it from the method and `c.Pattern()`, and a proxy, a rate limit and the log tell the writes apart by method and path alone.

Tifa does not check any of this. A handler may also read `c.Query("action")` and do one of several things, but those then share one address, one input type and one result type. Why Tifa has no functions that the browser calls by name is in [Philosophy](../start/philosophy.md#one-address-for-each-write).

## Calling method handlers from the browser

The `route.gen` module of a folder with method handlers exports one function per handler, named after its method. API routes get a `route.gen` module too, so a page can import the handlers of any folder:

```tsx
import { POST } from './route.gen'
import { TifaError } from 'tifa/client'

async function ship() {
  try {
    const shipment = await POST({ params: { project, order }, input: { carrier: 'SF' } })
  } catch (e) {
    if (e instanceof TifaError) setErrors(e.fields) // e.status, e.message, e.fields
  }
}
```

- The function takes one object. `params` holds the path parameters and is left out when the folder has none. A `name++` folder's key is `_splat`. A value that would send the request to another path is refused before any request and rejects the promise: an empty value, `.` or `..` for a `name+` parameter, and a `.` or `..` segment or an empty segment before the end for `_splat`. An empty `_splat` is fine. `input` is the handler's input and is left out when it takes none. `signal` aborts the request.
- GET and DELETE send the input in the query string, the other methods as a JSON body, or as a multipart form when the input holds files (see [Files in the input](handlers.md#files-in-the-input)). The result is typed from the Go result, with `| undefined` for a redirect (below): `func(c, in) (Out, error)` gives `Promise<POSTOutput | undefined>`. `func(c, in) error` gives `void`. `func(c) error` writes its own response, so its function gives back the `Response` as it is, also for an error status.
- A call only sends its request and, when the answer carries a session, applies it (see below). It never reloads the data of a page, on success or on failure, for any method. When the page should show what the write changed, call `router.invalidate()` after it (see the example below).
- Tifa never reloads page data for you. After a write, a handler function or a `fetch` of your own, the page shows what it showed before until you ask: call TanStack's `router.invalidate()` to load the data of the current page and all its Layouts again, merged into one request. `await router.invalidate({ sync: true })` resolves once the page shows the new data. A plain `await router.invalidate()` resolves at once and the page updates in the background.
- An error answer throws `TifaError` with `status`, `message` and `fields` (the messages of a `tifa.Fields`, empty otherwise). A network failure throws as `fetch` does. A call from a page of an older build than the server fails with status 409, see [Pages open while you deploy](../build-and-deploy/deploy.md#pages-open-while-you-deploy).
- When the handler or `OnRequest` returns `tifa.Redirect(url, status)`, the browser goes to that URL: through the router for a URL of this site, without reloading the page, and as a whole page load for another site. The new page shows what the router has for it and loads what is missing, as after any navigation. A redirect to the page that is showing stays where it is and loads nothing. It counts as the same page when the path, the hash and the query values match, whatever the order and encoding of the query. The promise resolves once the navigation has finished, to `undefined`, also for a handler with a result. Whether a call is redirected is only known from its answer, since `OnRequest` may redirect any request, so the result type of every handler function with a result includes `undefined`. The function of a handler that writes its own response gets the `Response` of the redirect (`ok`, with the status in `X-Tifa-Redirect`). A relative URL is resolved against the URL of the handler, not of the page. A 3xx the handler wrote itself through `c.Writer()`, or one a proxy wrote, cannot be read in the browser and rejects the promise with an `Error`.
- A call that signs someone in or out brings the new session in its answer, and the copy of the session in the browser follows (see [Sessions in the browser](sessions.md#in-the-browser)).
- Call these functions from event handlers or effects. Calling one while the page renders on the server throws.
- `route.gen` also exports `POSTInput`, `POSTOutput` and so on for the types of each handler that has them.

The error that `tifa.Redirect` returns is a `*tifa.RedirectError` with the fields `URL` and `Status`. In `OnRequest` or a test, recognize it with `errors.As`.

To go to the page of what a handler created, return a redirect from the handler, or return the result and navigate yourself. A call reloads nothing, so the page you leave is not loaded again just before. A write the page itself shows stays on screen after the call. Ask for it with `router.invalidate()`:

```go
func POST(c *tifa.Ctx, in AddInput) error {
	todo, err := AddTodo(in.Title)
	if err != nil {
		return err
	}
	return tifa.Redirect("/todos/"+todo.ID, http.StatusSeeOther)
}
```

```tsx
const router = useRouter() // from @tanstack/react-router
// in the event handler, where POST returns (Todo, error):
const todo = await POST({ input: { title } })
if (todo) await router.navigate({ to: '/todos/$todo', params: { todo: todo.id } }) // undefined: redirected
```
