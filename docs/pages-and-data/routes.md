---
summary: 'How to write the Go side of a route in route.go: Load, method handlers, typed input, errors, Ctx, JSON rules'
audience: app
order: 1
read_when:
  - Writing the route.go of a page or an API route: Load, a method handler, or the errors they return such as `tifa.Fields` and `tifa.Redirect`
  - The build reports an error about Ctx usage or a Load return type and you want to fix it
  - Changing the rules in internal/analyze and you need to know what users were promised
  - Taking files in a method handler, or giving a route a body limit or read time of its own with `c.SetBodyLimit` and `c.SetReadTimeout`
  - Looking up what a method of `*tifa.Ctx` does
  - Deciding which Go types a Load or a handler may take and return, or the build rejects one
---

# Routes and Ctx

## route.go

```go
package order

func Load(c *tifa.Ctx) (orders.Order, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return orders.Order{}, err
	}
	return orders.GetOrder(p.ID, c.Param("order"))
}

type ShipInput struct {
	Carrier string `json:"carrier"`
}

func POST(c *tifa.Ctx, in ShipInput) (orders.Shipment, error) {
	return orders.ShipOrder(c.Param("project"), c.Param("order"), in.Carrier)
}
```

- `Load` returns the page data. Its return type must convert to TypeScript (see "JSON rules" below).
- `GET`, `POST`, `PUT`, `PATCH` and `DELETE` handle the HTTP method they are named after.
- `Layout` returns the data of the Layout of the folder (see [Layout](pages.md#layout)).
- A folder with a `page.tsx` is a page. The framework renders its GET, so you cannot also write `GET`. A page that needs no data and no other method can be `page.tsx` alone, without `route.go`.
- A folder with `route.go` and no `page.tsx` is an API route when it declares a method handler: every method handler answers as written, and `Load` is not allowed, because `Load` loads the data of a page. A `route.go` with only a `Layout` is no API route. It gives the pages below it their Layout data.
- Everything is package-level functions and constants. Use the global variables the app has set up for dependencies. The framework does no dependency injection.
- The names of the switches (`SSR`, `Cache`, `Memo`) are read as written, so a misspelled one would be ignored. The build fails on a bool constant in `route.go` named `cache`, `memo` or `ssr` in lower case, and on an exported constant that is one or two letters off a switch (`Cahce`, `Memoo`) and has a type that switch takes: a bool for `SSR` and `Memo`, a bool or a `time.Duration` for `Cache`. The message names the switch it probably meant. Constants of other types, such as `const Demo = "demo"`, short names such as `ID` or `Max`, and names such as `Mode` that are two edits from a four-letter switch, are left alone.

When an error is returned:

| Returned | Result |
| --- | --- |
| `tifa.ErrNotFound` | 404 |
| `tifa.Redirect(url, status)` | A redirect. Page loads and navigation follow it, and so does a handler function called from the browser (see [Calling method handlers from the browser](pages.md#calling-method-handlers-from-the-browser)). Panics when `status` is not 3xx |
| `tifa.Error{Status, Message}` | Answered with its status code and message, does not go to `OnError`. A status outside 400 to 599 is handled as an unexpected error |
| `tifa.Fields{"carrier": "pick a carrier"}` | 422 with a message per input field by its json name, does not go to `OnError` |
| Any other error, a panic | Goes to `OnError`. Without one, it is logged and answered with 500 |

A method handler that fails answers JSON: `{"error": "message"}`, and for `tifa.Fields` also `"fields": {"carrier": "pick a carrier"}`.

In `Load` you can only set response headers and cookies. Calling `c.JSON`, `c.Text` or `c.NoContent` returns an error.

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
- The input and the result follow the JSON rules below, the same as the result of `Load`. The input must also read back from the JSON its TypeScript type describes: a type that is a string because of `MarshalText` needs `UnmarshalText([]byte) error` too, a type with `UnmarshalText` must be a string in TypeScript unless it is a map key (a key is always a JSON string), and `UnmarshalJSON` is not accepted (except on `time.Time`).
- When the input type has a method `Validate` (it must be `func() error` or `func(c *tifa.Ctx) error`), it runs after decoding and before the handler. `nil` goes on. With the Ctx, `Validate` can read the request: `c.Language()` for a message in the visitor's language (see [Validation messages](../guides/i18n.md#validation-messages)), `c.Param`, `c.Query`, `c.QueryValues`, `c.Header`, `c.Cookie`, `c.Path()`, `c.Context()`, `c.Request()`, and `tifa.Session`. It checks and answers with its error only, so the build fails when `Validate`, or a function it passes the Ctx to, writes the response (`c.JSON`, `c.Text`, `c.NoContent`, `c.SetHeader`, `c.SetCookie`, `c.Writer`), reads the body again (`c.Bind`, `c.BindQuery`, `c.SetBodyLimit`, `c.SetReadTimeout`), calls `tifa.SetSession` or `tifa.ClearSession`, or calls `tifa.Use`. The Ctx may go where it may in `Load` (see [How Load may use Ctx](#how-load-may-use-ctx)). A `tifa.Error` or `tifa.Fields` is answered as is, so return `tifa.Fields` to mark single fields. Collect them in a `tifa.Fields{}` and end with `return fields.Err()`, which is nil when nothing was added (an empty `tifa.Fields` is still an error). Any other error is answered with 400 and its `err.Error()` as the message, which the user sees.
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

## Ctx

`*tifa.Ctx` is the request and its response. `Load`, `Layout`, the method handlers, `OnRequest` and `OnError` get it.

| Method | What it does |
| --- | --- |
| `c.Param(name)` | A path parameter, decoded, such as `c.Param("order")` for the folder `order+` |
| `c.Query(name)` | The first value of a query parameter, `""` when absent |
| `c.QueryValues(name)` | Every value of a query parameter, such as `["a", "b"]` for `?tag=a&tag=b`, `nil` when absent |
| `c.Header(name)`, `c.Cookie(name)` | A request header, a request cookie (`http.ErrNoCookie` when absent) |
| `c.Context()`, `c.WithContext(ctx)` | The request context, and a copy of `c` that carries another one, to store request-scoped values. `c` itself does not change |
| `c.Pattern()` | The path of the matched route, such as `/projects/{project}`, `""` when no route matched |
| `c.Path()` | The path of the page as the visitor sees it in the address bar: escaped, with the language prefix, without the query, such as `/en-US/store/items/a%20b`. The same for a page load and for the data request of a navigation. In a method handler it is the path of the handler. Not allowed in a `Layout`, see [How Load may use Ctx](#how-load-may-use-ctx) |
| `c.Language()` | The language of the route being answered: the language in the URL, the default language when the URL has none, `""` on a site without languages. See [The language in code](../guides/i18n.md#the-language-in-code) |
| `c.SetAlternates(list)` | Replaces the versions of the page in other languages, for `hreflang` and the sitemap. Call it in a `Load`. See [What search engines get](../guides/i18n.md#what-search-engines-get) |
| `c.IsDataRequest()` | Whether this is a navigation data request of the browser router rather than a page load |
| `c.SetHeader(key, value)`, `c.SetCookie(cookie)` | A response header, a response cookie. Allowed in `Load` too |
| `c.JSON(code, v)`, `c.Text(code, s)`, `c.NoContent(code)` | Writes the response. Not allowed in `Load` |
| `c.Bind(&v)`, `c.BindQuery(&v)` | Decodes the body or the query string into a struct, see above. Not allowed in `Load` |
| `c.SetBodyLimit(n)` | Changes the body limit of this request (`server.body_limit` by default, see [Body limit and read time of a route](#body-limit-and-read-time-of-a-route)). Call it before anything reads the body, usually in `OnRequest`. Once reading has started it returns an error |
| `c.SetReadTimeout(d)` | Gives the body of this request `d` to arrive, in place of `server.read_timeout`, and the answer `server.write_timeout` after it. Call it before anything reads the body, usually in `OnRequest`. Once reading has started it returns an error |
| `c.Request()`, `c.Writer()` | The raw `*http.Request` and `http.ResponseWriter`, for method handlers and `OnRequest` only |

## How Load may use Ctx

This section covers `Load`, `Layout` and the functions they call. At build time, Tifa starts from each `Load` and `Layout` function and finds the route parameters (`c.Param`), query parameters (`c.Query` and `c.QueryValues`) and Layouts (`tifa.Use`) it reads. When `Load` passes `*tifa.Ctx` to a function in this module, Tifa follows the call level by level, across packages too, and counts every parameter it finds toward that `Load`. A helper called by several `Load` functions counts toward each of them. Tifa does not follow into its own functions.

So that Tifa finds everything, Ctx may only be used where Tifa can see it. Pass `c.Param`, `c.Query` and `c.QueryValues` a constant string, such as `c.Param("order")`, and call them and `tifa.Use` directly: storing one in a variable or passing it as a value fails the build.

The only types that may hold a Ctx are: `*tifa.Ctx`, `tifa.Ctx`, non-generic structs declared in this module (whose fields hold Ctx only through these types, directly, embedded or indirectly), and pointers to such structs.

A value that holds a Ctx may only appear in these five places:

1. As the receiver of a direct method call, such as `c.Query("tab")` or `req.Page()`.
2. As an argument to a non-generic function from this module or from Tifa, where the parameter has exactly the same type. A function literal called in place also counts.
3. In a composite literal of a struct that holds Ctx, for a field of exactly the same type.
4. Assigned with `:=` or `var x T = ...` to a new local variable of exactly the same type. That variable follows the same rules.
5. With its address taken, where the resulting pointer is still a type that may hold Ctx.

Any other use fails the build and names the file and line. For example: converting it to another type or an interface, assigning it to an existing variable, assigning several values at once, `return`ing it, putting it in a slice, map, array or channel, `range`, package-level variables, `defer`, `go` statements, or passing it to a generic function, an interface method, a function variable or a function outside the module (the standard library and third-party libraries).

`Load`, `Layout` and the functions they call must not call `c.Request()` or `c.Writer()`. Tifa cannot see parameters read through the raw request. Use `c.Param`, `c.Query`, `c.QueryValues`, `c.Header`, `c.Cookie` or `c.Context()` instead. `c.Request()` and `c.Writer()` are only for method handlers and `OnRequest`.

`c.Path()` is the way to get the path of the page in `Load`, such as for a `next` address in a redirect to a sign-in page. It is the path the visitor asked for, with the language prefix and without the query, for a page load and for the data request of a navigation alike, so it is no reason to call `c.Request()`. A `Load` that needs a query parameter in that address reads it with `c.Query("name")`. A `Layout` and the functions it calls must not call `c.Path()`: the data of a Layout is shared by every page below it and is not loaded again when the visitor goes from one of them to another, so it would keep the path of the page that loaded it first. Call it in the `Load` of the page, in `OnRequest`, or in a method handler.

They must not call `c.BindQuery` or `c.Bind` either. Tifa cannot name the query parameters a struct decodes, so the page would not load again when they change, and a page load or the data request of a navigation has no body. Read each query parameter with `c.Query("name")`, or `c.QueryValues("name")` for a repeated one.

When you hit one of these errors, call Ctx methods directly, or pass it to a plain function. If you only need the request context, pass `c.Context()`. A helper that only calls `c.Context()` (such as `auth.User(c)`) collects nothing when Tifa follows it.

Tifa only checks code that `Load` and `Layout` can reach. If you store a Ctx in a global variable or another shared place from `OnRequest`, a method handler or anywhere else, and then take it out in `Load` or `Layout`, Tifa cannot catch it. Do not do this.

## JSON rules

The value `Load` or a `Layout` function returns, and the input and result of a method handler, travel as JSON, and Tifa generates TypeScript types from them. Tifa writes and reads this JSON with `encoding/json/v2` and its defaults, so the build accepts only the types below, which json v2 writes without an error and in a form the TypeScript type describes. `c.JSON`, `c.Bind` and the session use the same rules.

| Go | TypeScript |
| --- | --- |
| `bool` | `boolean` |
| integers, floats, `json.Number` | `number` |
| `string`, `time.Time` | `string` |
| `tifa.HTML` | `HTML` of `tifa/client`, see [Showing HTML the app trusts](pages.md#showing-html-the-app-trusts). Not a map key, a handler input, in the session or a whole result |
| `*multipart.FileHeader`, `[]*multipart.FileHeader` | `Blob \| null`, `Blob[]`. Only in a field of the input of a POST, PUT or PATCH handler, see [Files in the input](#files-in-the-input) |
| pointer | `T \| null` |
| slice | `T[]`. A nil slice is written as `[]` |
| `[]byte`, `[N]byte` (the element is exactly `byte` or `uint8`) | `string`, written as base64. A nil `[]byte` is `""`. A slice or array of a named byte type is an ordinary array of numbers |
| array | `T[]`, or `[]` when the length is 0 |
| map | `Record<string, V>`. A nil map is written as `{}` |
| struct | an object of its exported fields |

- Map keys are written in sorted order, so the same value always gives the same text. They must be string or integer types, or types that output a string under the `MarshalText` rule below. Interfaces and pointers cannot be keys.
- An exported embedded struct (a pointer works too) without a json name has its fields promoted to the outer object. Fields reached through an embedded pointer are optional. When fields of different structs have the same name, the shallowest wins, then the one with a json tag, and if that still leaves several, all of them are dropped. An embedded field needs a json name when its type is not a struct, and an embedded struct may not have json tag options or a method named `MarshalJSON`, `MarshalJSONTo`, `MarshalText`, `AppendText`, `UnmarshalJSON`, `UnmarshalJSONFrom` or `UnmarshalText`. Unexported embedded fields are not accepted unless tagged `json:"-"`.
- Two fields of one struct may not have the same JSON name. An unexported field may not have a json tag other than `json:"-"`, and a struct that has fields needs an exported one or a json tag.
- A json tag name may only use letters, digits, spaces and `` !#$%&()*+-./:;<=>?@[]^_{|}~ ``. The only allowed options are `omitempty`, `omitzero` and `string`. Fields with `omitempty` or `omitzero` are optional. `omitzero` leaves out the zero value of the Go type. `omitempty` leaves out a value whose JSON is empty: `null`, `""`, `[]` or `{}`, so it never leaves out `false` or `0`. The `string` option is only allowed on fields of a predeclared number type, or on unnamed pointers to them, and writes the number as a string. The `string` option on an `int64` or `uint64` field is how to send a big integer: it is typed `string` and written as a JSON string. See the next point.
- A browser, and the server render, read every JSON number as a double, so an integer is safe only up to 2^53 - 1 (9007199254740991). Integers are typed `number`, and Go writes every digit, so a value such as a snowflake ID, a hash or `time.UnixNano()` would arrive rounded, without any error. Tifa refuses to send it instead: the request fails as an unexpected error (`OnError`, a 500, the dev page and `tifa errors`, in development and in production), and the message names the `Load`, `Layout`, handler or session, the path of the number (`rows[3].id`) and the number as written and as a browser reads it. It checks the JSON written, so it does not matter how the Go type is spelled, and a `float64` is never refused. The two ways out: add `json:",string"` to the field (typed `string`), or keep the value within ±9007199254740991 (for a time, send milliseconds). Only what a page reads is checked: the results of `Load` and `Layout`, the session data of `Exposed`, and the result of a handler when the call comes from the generated function of a page. A plain HTTP client (curl, a mobile app) gets the raw number from a handler, and so does `tifatest.Start` without the header `X-Tifa-Call: 1`, so a test that posts to a handler must set that header to see the refusal. `Load` and `Layout` are checked under `tifatest.Start` too. What a handler writes with `c.JSON` and the answer to a client that does not send `X-Tifa-Call`, such as a mobile app, are sent as written. `tifa build`, `tifa check` and `tifa dev` warn, with the file and line, where code puts a number above 2^53 straight into such a field: a constant, `time.Time.UnixNano`, `Int63`, `Int64`, `Uint64` of `math/rand`, a `Sum64` hash, or `binary.Uint64` of bytes from `crypto/rand`, in a composite literal or an assignment to a field, also through a conversion or one local variable. Nothing else is guessed.
- `time.Duration` is not accepted, also as a map key, since json v2 has no form for it. Return an integer of the unit you mean, such as milliseconds, or a string.
- If a type's value or pointer has a method or field named `MarshalJSON`, `MarshalJSONTo`, `AppendText` or `AppendJSON`, it is rejected whatever the signature (except `time.Time` and `json.Number`). Rename it, or return a different type without these methods.
- If a type itself declares a method with exactly the signature `MarshalText() ([]byte, error)`, on the value or the pointer receiver, it is written as a string. A wrong signature, a method promoted from an embedded field, or a method hidden by a field of the same name is always rejected.
- Interfaces are not accepted anywhere, including fields, elements and map keys. Neither are chan, func, complex numbers, `unsafe.Pointer` or type parameters.
- `<`, `>` and `&` are written as they are, also in the data of a page. Tifa escapes them where it puts the data into a page, so no text of the app can end a script element.
- A string with bytes that are not UTF-8 is written with U+FFFD for each of them, as the browser would show it, and does not fail the page.
- Input is read strictly: names must match the json name exactly, also in case, a name may appear once in an object, strings must be UTF-8, `time.Time` must be RFC 3339, base64 may not hold line breaks, and an array must have the length of the Go array. Each mistake answers 400. Names the type does not have are skipped. A session record that does not read back this way signs the visitor out.
