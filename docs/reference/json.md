---
summary: 'Go types Load and handlers may take and return, and their TypeScript types'
audience: app
order: 5
read_when:
  - Deciding which Go types a Load or a handler may take and return, or the build rejects one
  - The build reports an error about a Load return type and you want to fix it
  - Changing the rules in internal/analyze and you need to know what users were promised
---

# JSON rules

The value `Load` or a `Layout` function returns, and the input and result of a method handler, travel as JSON, and Tifa generates TypeScript types from them. Tifa writes and reads this JSON with `encoding/json/v2` and its defaults, so the build accepts only the types below, which json v2 writes without an error and in a form the TypeScript type describes. `c.JSON`, `c.Bind` and the session use the same rules.

| Go | TypeScript |
| --- | --- |
| `bool` | `boolean` |
| integers, floats, `json.Number` | `number` |
| `string`, `time.Time` | `string` |
| `tifa.HTML` | `HTML` of `tifa/client`, see [Showing HTML the app trusts](loading-data.md#showing-html-the-app-trusts). Not a map key, a handler input, in the session or a whole result |
| `*multipart.FileHeader`, `[]*multipart.FileHeader` | `Blob \| null`, `Blob[]`. Only in a field of the input of a POST, PUT or PATCH handler, see [Files in the input](../pages-and-data/handlers.md#files-in-the-input) |
| pointer | `T \| null` |
| slice | `T[]`. A nil slice is written as `[]` |
| `[]byte`, `[N]byte` (the element is exactly `byte` or `uint8`) | `string`, written as base64. A nil `[]byte` is `""`. A slice or array of a named byte type is an ordinary array of numbers |
| array | `T[]`, or `[]` when the length is 0 |
| map | `Record<string, V>`. A nil map is written as `{}` |
| struct | an object of its exported fields. A named struct is also declared in `route.gen` under its Go name, see [page.tsx](loading-data.md#pagetsx) |

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
