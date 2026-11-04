---
summary: '在 tifa 应用里怎么让访客上传文件：方法函数入参里的文件字段，生成的函数把它当表单发出，上传路由的请求体上限和读取时间，检查文件类型，文件放哪里、怎么再提供给访客，以及怎么测方法函数'
read_when:
  - 加文件上传功能，比如图片、头像或附件
  - 上传报 400、408、413、415 或 422，或者方法函数看不到文件
  - 决定上传的文件放在哪里、应用怎么把它们提供出去
---

[English](uploads.md) · 简体中文

# 上传

一个页面，访客上传带标题的图片，并看到列表。方法函数检查标题、大小和文件类型，把文件存进一个文件夹，再把它提供出去。

代码照原样跑过：它就是 tifa 仓库里的应用 `test/apps/upload`，端到端测试会构建它，并在浏览器里操作它。用到的调用，参考文档在这里：[入参里的文件](../app.zh-CN.md#入参里的文件)，[路由自己的请求体上限和读取时间](../app.zh-CN.md#路由自己的请求体上限和读取时间)，[应用的配置](../config.zh-CN.md#应用的配置)。

## 文件怎么到方法函数手里

POST、PUT、PATCH 处理函数的入参里，类型为 `*multipart.FileHeader` 的字段装着文件（`[]*multipart.FileHeader` 装多个）。其余和[表单](forms.zh-CN.md)一样：

- 生成的函数把文件当 `Blob` 收下（文件输入框里的 `File` 也是），把入参当 multipart 表单发出。页面像调用别的方法函数一样调用它，被拒绝时抛出 `TifaError`，带着每个字段的消息。
- 文本字段按 `json` 名填进入参，文件填进它的字段，然后运行 `Validate`。所以没选文件和没填标题一样，是显示在输入框下面的一条消息。
- 方法函数用 `in.File.Open()` 打开文件。

## 配置和请求体限制

根包里的 `config.go`：

```go
// Package uploads 是应用的根包：它的配置和钩子。
package uploads

import "github.com/liustack/tifa"

var (
	// UploadLimit 是一次上传的请求体上限。
	UploadLimit = tifa.Size("upload.limit", 4<<20, "Largest upload")
	// UploadDir 是上传文件存放的文件夹，相对于应用运行的文件夹。
	UploadDir = tifa.String("upload.dir", "data/uploads", "Folder for uploaded files")
)
```

它们同样是参数 `--upload-limit`、环境变量 `UPLOAD_LIMIT` 等等，和所有[应用的配置](../config.zh-CN.md#应用的配置)一样。`4MiB` 和 `4<<20` 是一个意思。`hooks.go` 在应用启动时建好文件夹：

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

所有请求体都受 `server.body_limit` 限制，默认 1 MiB，每个请求也只有 `server.read_timeout` 的 10 秒传完。只给上传路由放宽这两项，其他路由保持小的。在 `app/photos/route.go` 开头：

```go
// BodyLimit 让这个路由的请求最大到 upload.limit。其他路由保持 server.body_limit。
var BodyLimit = uploads.UploadLimit

// ReadTimeout 给慢网络上的手机足够的时间发一张大图。
const ReadTimeout = 5 * time.Minute
```

- 请求体超过 `BodyLimit` 时回答 413，方法函数不会运行。限制算的是整个请求体：文件、其他字段和 multipart 的分隔部分。一个略小于限制的文件也可能被拒绝。
- `ReadTimeout` 从匹配到路由时开始算。时间到了请求体还没传完，回答 408。回答在这之后再有 `server.write_timeout` 的时间。
- 两者作用于这个 `route.go` 的方法函数回答的请求，页面的 GET 照旧用配置。变量在应用启动时读一次，所以 `--upload-limit 8MiB` 有效。
- multipart 请求体最多把 32 MiB 的文件内容放在内存里，文本字段和各部分的名字、头另外最多再占 10 MiB。放不下的文件写到临时文件，请求结束时 tifa 会删掉，但写盘之前它最多有 32 MiB 先经过内存里的缓冲区。所以一次上传可能短时间占 40 MiB 左右，不管文件最后落在哪里。请求体限制可以高于这个数：超出的部分进磁盘，不进内存。同时进行的上传各占这么多，小机器上要算一算：同时十个大文件，最多能占 400 MiB 左右。
- 应用前面的代理或 CDN 有自己的请求体限制和超时。要一起调高，否则访客看到的是它的错误，不是你的。

## 方法函数

`app/photos/route.go` 的其余部分：

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

// （BodyLimit 和 ReadTimeout，见上文）

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

// UploadInput 是表单：一个标题和一张图片。
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

// 应用接受的图片种类，按字节内容判断。
var extensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// POST 保存图片。在这之前，框架已经对超过 BodyLimit 的请求体回答了 413，也已经运行了 Validate。
func POST(c *tifa.Ctx, in UploadInput) (Photo, error) {
	file, err := in.File.Open()
	if err != nil {
		return Photo{}, err
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

- **看文件实际是什么，不看它自称是什么。** `http.DetectContentType` 看开头的 512 个字节。文件名和这一部分的 `Content-Type`（`in.File.Header`）都是浏览器给的，谁都能随便写。只接受一个短名单，其余拒绝。这只能告诉你文件的种类，不能保证内容无害。它能防止脚本和 HTML 被当成图片存下来再提供出去。
- **文件名自己起。** 例子里从不把客户端的文件名（`in.File.Filename`）放进路径，这就堵住了带 `../` 的名字、覆盖别的文件的名字和奇怪的字符。想显示原来的文件名，把它存进数据库。
- **在方法函数返回前复制文件。** `in.File` 背后的临时文件在请求结束时删掉，比请求活得久的 goroutine 读不到它。
- **限制数量和人。** 请求体限制只管一次请求。一个访客能留多少文件、谁能上传，是你自己的规则：在方法函数开头读 `tifa.Session`（见[登录](sign-in.zh-CN.md)）。
- 表单里文件的位置上放了文本的，回答 400，JSON 请求体回答 415。生成的函数两种都不会发。

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
      // 没选文件的文件输入框给出一个没有名字的 File：这时不发文件，由 Validate 说明。
      await POST({ input: { title: String(data.get('title') ?? ''), file: file instanceof File && file.name !== '' ? file : null } })
      setFailure({ fields: {} })
      form.reset()
      // 上传不会重新加载页面数据，要自己要求。
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

- `POST` 是方法函数生成的函数。它的入参类型里有 `file: Blob | null`，所以输入框里的 `File` 原样放进去。
- 没选文件的文件输入框，在 `FormData` 里仍然给出一个 `File`，名字为空。这时页面发 `null`，由 `Validate` 要求选图片。
- 被拒绝时抛出 `TifaError`。`e.fields` 是 `tifa.Fields` 的消息，按入参的 `json` 名排。413 没有字段，所以页面给出自己的消息。
- 浏览器的 `accept` 过滤只是给文件选择框的提示，不是检查。由 Go 这边决定。
- 调用自己的站点是同源写请求，[跨站检查](../config.zh-CN.md#跨站写请求)会放行。别的站点的表单会得到 403。
- 调用不会重新加载页面数据，所以页面在得到成功的回答之后调 `router.invalidate({ sync: true })`。

## 测方法函数

`tifatest.File` 给出一个文件的 `*multipart.FileHeader`，测试把它放进入参，和服务器给的一样。`app/photos/route_test.go`：

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

测试直接调用方法函数：`Validate`、请求体上限和读取时间归服务器管，见[测试](../app.zh-CN.md#测试)。

## 要做的选择

**文件放哪里。** 像这里一样放在机器上的一个文件夹里，要运行的东西最少。它需要一个部署后还在的卷，也要和其他数据一起备份（见[部署](../deploy.zh-CN.md)）。tifa 的目标是一台机器上一个进程，所以通常够用。有多台机器，或者文件很大时，把它们放进对象存储，让方法函数写到那里。上面的检查都不变。从那里提供，或者重定向到一个带签名的链接。

**多大。** 选够用的最小限制。头像要 1 到 2 MiB。在表单里写明限制，并在浏览器里检查 `file.size`，发送之前就给出提示。起作用的仍然是服务器的限制。

**多久。** `ReadTimeout` 是你服务的最慢的访客发完你接受的最大文件要花的时间。4 MiB 走 1 Mbit/s 的线路大约要 34 秒，所以 5 分钟留足了余地。时间长了，慢客户端就能占着连接那么久，所以不需要的路由别给。

**接受什么。** 一个种类名单，按字节判断。文档类的文件，用它们自己的类型做同样的事，并用 `Content-Disposition: attachment` 提供，让浏览器保存而不是直接打开。

## 检查一下

```sh
go tool tifa check
go tool tifa dev
```

打开 `/photos`。不填标题或不选文件就发送，每条消息显示在各自的输入框下面。发一个名叫 `a.png` 的文本文件，会因为它的字节被拒绝。发一个超过限制的文件，表单说它太大。发一张图片，它出现在列表里，从 `/photos/{id}` 提供。
