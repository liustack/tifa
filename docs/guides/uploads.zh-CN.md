---
summary: '在 tifa 应用里怎么让访客上传文件：为什么生成的函数发不了文件、用 fetch 加 FormData 就行，读文本字段和文件的 Go 方法函数，大小限制，检查文件类型，文件放哪里、怎么再提供给访客'
read_when:
  - 加文件上传功能，比如图片、头像或附件
  - 上传报 413、415 或 400，或者方法函数看不到文件
  - 决定上传的文件放在哪里、应用怎么把它们提供出去
---

[English](uploads.md) · 简体中文

# 上传

一个页面，访客上传带标题的图片，并看到列表。方法函数检查标题、大小和文件类型，把文件存进一个文件夹，再把它提供出去。

代码照原样跑过。用到的调用，参考文档在这里：[方法函数](../app.zh-CN.md#带入参和返回值的方法函数)，[Ctx](../app.zh-CN.md#ctx)，[应用的配置](../config.zh-CN.md#应用的配置)。

## 现在能做什么

方法函数生成的函数发不了文件。它把入参作为 JSON 发出去（GET 和 DELETE 放在查询字符串里），而文件不是 JSON。通过生成的函数发文件还没做。所以上传分两部分：

- Go 方法函数是普通的方法函数。它接受 multipart 表单（`Content-Type: multipart/form-data`），也接受 JSON 和 URL 编码的表单。文本字段按 `json` 名填进入参结构体，所以 `Validate`、`tifa.Fields` 和 422 的回答和[表单](forms.zh-CN.md)里一样。表单的值只能填进字符串、数字、布尔或它们的切片字段。
- 页面用 `fetch` 加 `FormData` 发请求，回答要自己读，因为 `TifaError` 只有生成的函数才会抛。

文件不是入参结构体的一部分。方法函数用 `c.Request().FormFile("file")` 从请求里读它。

## 配置和请求体限制

根包里的 `config.go`：

```go
package uploads

import "github.com/liustack/tifa"

var (
	// UploadLimit 是一次上传的请求体上限。
	UploadLimit = tifa.Size("upload.limit", 4<<20, "Largest upload")
	// UploadDir 是上传文件存放的文件夹，相对于应用运行的文件夹。
	UploadDir = tifa.String("upload.dir", "data/uploads", "Folder for uploaded files")
)
```

它们同样是参数 `--upload-limit`、环境变量 `UPLOAD_LIMIT` 等等，和所有[应用的配置](../config.zh-CN.md#应用的配置)一样。`4MiB` 和 `4<<20` 是一个意思。

所有请求体都受 `server.body_limit` 限制，默认 1 MiB。更大的回答 413，方法函数不会运行。只给上传路由放宽，其他路由保持小限制。`hooks.go`：

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

// OnRequest 给上传路由单独的请求体限制。要在任何东西读请求体之前设置。
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if c.Request().Method == "POST" && c.Pattern() == "/photos" {
		if err := c.SetBodyLimit(UploadLimit); err != nil {
			return err
		}
	}
	return next(c)
}
```

- 限制算的是整个请求体：文件、其他字段和 multipart 的分隔部分。一个略小于限制的文件也可能被拒绝。
- 用带类型的入参时，框架在方法函数运行之前就解码了请求体，所以限制要更早改，放在 `OnRequest` 里。自己读请求体的方法函数，只要还没有东西读过请求体，可以在开头调 `c.SetBodyLimit`（见下文）。
- multipart 请求体最多 32 MiB 放在内存里，超出的部分写到临时文件，请求结束时 tifa 会删掉它们。限制可以高于这个数，但同时来几个上传就很占内存，所以按需要尽量设低。
- 应用前面的代理或 CDN 有自己的请求体限制。要一起调高，否则访客看到的是它的错误，不是你的。
- 手机在慢网络上传大文件，可能超过 `server.read_timeout` 的 10 秒，上传就会传到一半失败。在同一个 `if` 里、读请求体之前，给上传路由放宽期限，其他请求的配置保持短（见[耗时长的请求](../config.zh-CN.md#耗时长的请求)）：

  ```go
  rc := http.NewResponseController(c.Writer())
  if err := rc.SetReadDeadline(time.Now().Add(5 * time.Minute)); err != nil {
  	return err
  }
  // 回答在请求体之后才写，所以它也需要时间。
  if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil {
  	return err
  }
  ```

## 方法函数

`app/photos/route.go`：

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

// Photo 是一张存下来的图片。ID 同时是它的文件名。
type Photo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func Load(c *tifa.Ctx) ([]Photo, error) {
	entries, err := os.ReadDir(uploads.UploadDir)
	if err != nil {
		return nil, err
	}
	// 最新的在前：名字是随机的，所以按修改时间排。
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

// UploadInput 是表单里的文本字段。文件直接从请求里读。
type UploadInput struct {
	Title string `json:"title"`
}

func (in UploadInput) Validate() error {
	if strings.TrimSpace(in.Title) == "" {
		return tifa.Fields{"title": "Give the picture a title."}
	}
	return nil
}

// 应用接受的图片种类，按字节内容判断。
var extensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// POST 保存名为 "file" 的那一部分。框架已经把文本字段读进 in，
// 请求体超过 upload.limit 的在这之前就已经回答了 413。
func POST(c *tifa.Ctx, in UploadInput) (Photo, error) {
	file, _, err := c.Request().FormFile("file")
	if err != nil {
		return Photo{}, tifa.Fields{"file": "Choose a picture."}
	}
	defer file.Close()

	// 看开头的字节判断类型，不看文件名，也不看浏览器发来的 Content-Type。
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

	// 名字由我们起：随机的，加上我们判断出的扩展名。路径里没有任何客户端给的东西。
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

标题存在图片旁边的一个小文件里，这样例子不需要数据库。真实的应用会把标题、所有者和文件名放进一张表。

- **看文件实际是什么，不看它自称是什么。** `http.DetectContentType` 看开头的 512 个字节。文件名和这一部分的 `Content-Type` 都是浏览器给的，谁都能随便写。只接受一个短名单，其余拒绝。这只能告诉你文件的种类，不能保证内容无害。它能防止脚本和 HTML 被当成图片存下来再提供出去。
- **文件名自己起。** 例子里从不把客户端的文件名放进路径，这就堵住了带 `../` 的名字、覆盖别的文件的名字和奇怪的字符。想显示原来的文件名，把它存进数据库。
- **限制数量和人。** 请求体限制只管一次请求。一个访客能留多少文件、谁能上传，是你自己的规则：在方法函数开头读 `tifa.Session`（见[登录](sign-in.zh-CN.md)）。

### 自己写回答的方法函数

想自己读表单，就只收 Ctx。`c.Bind` 解码文本字段（先看 `form` 标签，再看 `json`，最后用 Go 字段名），请求体超过限制时报 413，回答由你来写：

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
	// 还没有东西读过请求体，所以这里还能改限制。
	if err := c.SetBodyLimit(uploads.UploadLimit); err != nil {
		return err
	}
	var in form
	if err := c.Bind(&in); err != nil { // 请求体超限是 413，请求体损坏是 400
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

这里没有 `Validate`，字段要自己检查。它生成的函数不带入参，返回的是 `Response`，所以对发文件也没有帮助。

## 把文件提供出去

不要把上传的文件放进 `public/`。public 文件夹是构建时复制的，应用只提供那时已经在里面的文件。应用运行时加进去的文件找不到。

用方法函数来提供。`app/photos/id+/route.go` 回答 `/photos/{id}`：

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

// 我们写出来的名字：24 个十六进制字符加一个已知的扩展名。
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
	// 同一个名字的内容不会变，所以浏览器可以一直缓存。
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// ServeContent 按扩展名定 Content-Type，也处理 Range 和条件请求。
	http.ServeContent(w, c.Request(), name, info.ModTime(), f)
	return nil
}
```

- 这个路由只接受应用自己写出的名字，所以标题文件和各种路径花招都是 404。永远不要不检查就把 URL 里的名字拼到文件夹路径上。
- `Content-Type` 来自我们看过字节之后定的扩展名，`nosniff` 不让浏览器自己猜别的类型。
- 只有所有者能看的文件，要在这个方法函数里对照 `tifa.Session` 检查，`Cache-Control` 里也不要写 `public`。
- 页面里的 `<img src="/photos/...">` 和别的请求一样，会走到这个方法函数。

## 页面

`app/photos/page.tsx`：

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

// 生成的 POST 函数发的是 JSON，所以文件走 fetch 加 FormData。
// 不要自己设 Content-Type：浏览器会写上它，并带上分隔符。
async function upload(form: HTMLFormElement): Promise<POSTOutput | Failure> {
  const response = await fetch('/photos', { method: 'POST', body: new FormData(form) })
  if (response.ok) return (await response.json()) as POSTOutput
  // 方法函数的错误是 {"error": "...", "fields": {...}}。代理可能回答别的东西。
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
      // 上传不会重新加载页面数据，要自己要求。
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

- `POSTOutput` 是方法函数结果生成的类型，所以 `Photo` 会跟着 Go 一起变。
- 不要自己设 `Content-Type` 头。只有浏览器自己写这个头时才会带上分隔符。
- 浏览器的 `accept` 过滤只是给文件选择框的提示，不是检查。由 Go 这边决定。
- 向自己的站点发 `fetch` 是同源写请求，[跨站检查](../config.zh-CN.md#跨站写请求)会放行。别的站点的表单会得到 403。
- 你自己写的调用不会重新加载页面数据，所以页面在得到成功的回答之后调 `router.invalidate({ sync: true })`。

## 要做的选择

**文件放哪里。** 像这里一样放在机器上的一个文件夹里，要运行的东西最少。它需要一个部署后还在的卷，也要和其他数据一起备份（见[部署](../deploy.zh-CN.md)）。tifa 的目标是一台机器上一个进程，所以通常够用。有多台机器，或者文件很大时，把它们放进对象存储，让方法函数写到那里。上面的检查都不变。从那里提供，或者重定向到一个带签名的链接。

**多大。** 选够用的最小限制。头像要 1 到 2 MiB。在表单里写明限制，并在浏览器里检查 `file.size`，发送之前就给出提示。起作用的仍然是服务器的限制。

**接受什么。** 一个种类名单，按字节判断。文档类的文件，用它们自己的类型做同样的事，并用 `Content-Disposition: attachment` 提供，让浏览器保存而不是直接打开。

## 检查一下

```sh
go tool tifa check
go tool tifa dev
```

打开 `/photos`。不填标题或不选文件就发送，每条消息显示在各自的输入框下面。发一个名叫 `a.png` 的文本文件，会因为它的字节被拒绝。发一个超过限制的文件，表单说它太大。发一张图片，它出现在列表里，从 `/photos/{id}` 提供。
