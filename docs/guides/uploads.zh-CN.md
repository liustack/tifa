---
summary: '怎样让访客在 Tifa 应用里上传文件：文件输入、请求体上限、类型检查、存放位置、测试'
audience: app
order: 4
translatedFrom: cc8ed5741c91
read_when:
  - '添加文件上传，比如图片、头像或附件'
  - '上传失败，返回 400、408、413、415 或 422，或者处理函数看不到文件'
  - '决定上传的文件放在哪里，以及应用怎样提供它们'
---

# 上传文件 {#uploads}

一个让访客上传带标题的图片、并看到它们列表的页面。处理函数检查标题、大小和文件种类，把文件保存在一个文件夹里，再把它提供回去。

这些代码是照写出来的样子运行过的：它就是 Tifa 仓库里的应用 `test/apps/upload`，端到端测试会构建它并在浏览器里操作它。这里用到的调用的参考文档：[输入里的文件](../pages-and-data/handlers.zh-CN.md#files-in-the-input)、[路由的请求体上限和读取时间](../pages-and-data/handlers.zh-CN.md#body-limit-and-read-time-of-a-route)、[应用的选项](../build-and-deploy/config.zh-CN.md#options-of-the-app)。

## 文件如何到达处理函数 {#how-a-file-gets-to-the-handler}

POST、PUT 或 PATCH 处理函数的输入里，类型为 `*multipart.FileHeader` 的字段保存文件（`[]*multipart.FileHeader` 保存多个）。其余的和 [表单](forms.zh-CN.md) 里一样：

- 生成的函数把文件当作 `Blob` 接收，文件输入框里的 `File` 也包括在内，并把输入作为 multipart 表单发出。页面像调用任何其他处理函数一样调用它，被拒绝时会抛出 `TifaError`，带着每个字段的消息。
- 文本字段按它们的 `json` 名字填入输入，文件填入它自己的字段，然后运行 `Validate`。所以缺少文件时，消息会显示在它的输入框下面，和缺少标题一样。
- 处理函数用 `in.File.Open()` 打开文件。

## 设置和请求体上限 {#the-settings-and-the-body-limit}

根包里的 `options.go`：

```go
// Package uploads is the root package of the app: its options and hooks.
package uploads

import "github.com/liustack/tifa"

var (
	// UploadLimit is the largest request body of an upload.
	UploadLimit = tifa.Size("upload.limit", 4<<20, "Largest upload")
	// UploadDir is where uploaded files are kept, relative to the folder the app runs in.
	UploadDir = tifa.String("upload.dir", "data/uploads", "Folder for uploaded files")
)
```

它们同时也是命令行参数 `--upload-limit`、环境变量 `UPLOAD_LIMIT` 等等，和每个 [应用的选项](../build-and-deploy/config.zh-CN.md#options-of-the-app) 一样。`4MiB` 和 `4<<20` 是一个意思。`hooks.go` 在应用启动时创建这个文件夹：

```go
package uploads

import (
	"context"
	"os"
	"time"

	"github.com/liustack/tifa"
)

func OnStart(context.Context) error {
	return os.MkdirAll(UploadDir, 0o755)
}
```

每个请求体都限制在 `server.body_limit`，默认 1 MiB，每个请求有 `server.read_timeout` 的 10 秒来送达。只对上传路由把两者调高，让其他路由保持小的值。`hooks.go` 里的 `OnRequest` 来做这件事，只针对这个路由的 POST：

```go
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if c.Request().Method == "POST" && c.Pattern() == "/photos" {
		// The requests of this route may be as large as upload.limit. The other routes keep
		// server.body_limit.
		if err := c.SetBodyLimit(int64(UploadLimit)); err != nil {
			return err
		}
		// A phone on a slow line needs time to send a large picture.
		if err := c.SetReadTimeout(5 * time.Minute); err != nil {
			return err
		}
	}
	return next(c)
}
```

- 超过上限的请求体会以 413 响应，处理函数不会运行。这个上限计算的是整个请求体：文件、其他字段和 multipart 的封装。比上限稍小一点的文件，仍然可能被拒绝。
- 读取时间从调用 `SetReadTimeout` 的那一刻开始算，在任何东西读取请求体之前。时间到了请求体还在传，就以 408 响应。响应之后还有 `server.write_timeout` 的时间。
- 请求体一旦开始被读取，这两个调用就会返回错误，所以要在 `OnRequest` 里调用，并把错误返回。`UploadLimit` 在请求到来时读取，所以 `--upload-limit 8MiB` 可以用。
- 应用前面的代理或 CDN 有它们自己的请求体上限和超时。请相应调高，否则访客会得到它们的错误，而不是你的。

### 一次上传占多少内存 {#how-much-memory-an-upload-takes}

表单用 Go 的 `mime/multipart` 的预算来解析。最多 32 MiB 的文件内容留在内存里，文本字段以及各部分的名字和头，与它共用约 42 MiB 的一份预算。放不下的文件会进入临时文件，Tifa 在请求结束时把它删掉。请求体上限比这个高也没问题：多出来的部分进磁盘。

这份预算不是峰值。最终落在磁盘上的文件，先要读进一个最多 32 MiB 的缓冲区，它随着填充而增长，所以一个大文件在一瞬间可能占到约 64 MiB，每个同时进行的上传都要占它自己的一份。在内存很少的机器上，只让少数几个上传同时读它们的表单。把下面的内容加到 `hooks.go`，等待的部分加进上面的 `OnRequest`：

```go
// parsing lets two uploads read their form at a time. The others wait here, before their body is
// read, so a burst of uploads does not hold the memory of all of them at once.
var parsing = make(chan struct{}, 2)

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if c.Request().Method == "POST" && c.Pattern() == "/photos" {
		// (SetBodyLimit and SetReadTimeout, as above)
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

上传在这里等待的时间，要算进它的读取时间，所以要给等待留出余地。


## 处理函数 {#the-handler}

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

// POST stores the picture. The framework has refused a body over upload.limit with 413 and run
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

标题保存在图片旁边的一个小文件里，这样这个例子不需要数据库。真实的应用会把标题、所有者和文件名保存在一张表里。

- **检查文件实际是什么，而不是它自称是什么。** `http.DetectContentType` 看前 512 个字节。文件名和这一部分的 `Content-Type`（`in.File.Header`）来自浏览器，所以任何人都可以在那里随便写。接受一个简短的名单，拒绝其余的。这只告诉你文件的种类，不说明它的内容无害。它能防止脚本和 HTML 被当成图片保存和提供。
- **自己生成名字。** 这个例子从不把客户端的文件名（`in.File.Filename`）放进路径，这就堵住了 `../` 这样的名字、会覆盖别的文件的名字，以及奇怪的字符。如果想显示原来的名字，把它保存在数据库里。
- **在处理函数返回之前复制文件。** `in.File` 背后的临时文件在请求结束时就删除了，所以比请求活得更久的 goroutine 读不到它。
- **对数量和对人的限制。** 请求体上限只限制一个请求。一个访客可以保留多少文件、谁能上传，是你自己的规则：在处理函数的开头读 `tifa.Session`（见 [登录](sign-in.zh-CN.md)）。
- 一个字段里放的是文本而不是文件，会以 400 响应，JSON 请求体会以 415 响应。生成的函数从来不发这两种。

## 提供这些文件 {#serving-the-files}

不要把上传的文件放进 `public/`。public 文件夹在构建时被复制，应用只提供当时在里面的文件。应用运行时加进去的文件找不到。

用一个处理函数来提供它们。`app/photos/id+/route.go` 响应 `/photos/{id}`：

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

- 这个路由只接受应用自己写出的名字，所以标题文件和任何路径技巧都是 404。永远不要不经过这样的检查，就把 URL 里的名字拼到文件夹路径上。
- `Content-Type` 来自我们看过字节之后选定的扩展名，`nosniff` 让浏览器不要去猜另一种类型。
- 只有所有者才能看的文件，要在这个处理函数里对照 `tifa.Session` 检查，并且 `Cache-Control` 里不能有 `public`。
- 页面里的 `<img src="/photos/...">` 和其他请求一样，会到达这个处理函数。

## 页面 {#the-page}

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

- `POST` 是处理函数生成的函数。它的输入类型里有 `file: Blob | null`，所以输入框里的 `File` 原样放进去就行。
- 留空的文件输入框仍然会给 `FormData` 一个名字为空的 `File`。这时页面发送 `null`，`Validate` 会要求一张图片。
- 被拒绝时会抛出 `TifaError`。`e.fields` 放着 `tifa.Fields` 的消息，按输入的 `json` 名字。413 没有字段，所以页面给出它自己的消息。
- 浏览器自己的 `accept` 过滤只是给文件选择器的提示，不是检查。决定权在 Go 一侧。
- 对自己站点的调用是同源的写请求，所以 [跨站检查](../build-and-deploy/config.zh-CN.md#cross-site-writes) 会放行它。来自另一个站点的表单会以 403 响应。
- 调用不会重新加载页面数据，所以页面在得到成功的响应之后调用 `router.invalidate({ sync: true })`。

## 测试处理函数 {#testing-the-handler}

`tifatest.File` 给出一个文件的 `*multipart.FileHeader`，所以测试可以像服务器那样，把它放进输入里传入。`app/photos/route_test.go`：

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

这个测试直接调用处理函数：`Validate`、请求体上限和读取时间是服务器的部分，见 [测试](../pages-and-data/testing-apps.zh-CN.md#testing)。

## 取舍 {#decisions}

**文件放在哪里。** 像这里这样放在机器上的一个文件夹里，要运行的东西最少。它需要一个在部署之后还能保留的卷，并且和其余数据一起备份（见 [部署](../build-and-deploy/deploy.zh-CN.md)）。一台机器上的一个进程是 Tifa 的目标，所以这通常就够了。如果你有不止一台机器，或者文件很大，就把它们放在对象存储里，让处理函数往里写。上面的检查保持不变。从那里提供它们，或者重定向到一个签名链接。

**多大。** 选适合用途的最小上限。头像要 1 或 2 MiB。在表单里显示这个上限，并在浏览器里检查 `file.size`，在发送之前给出一个快速的消息。真正算数的仍然是服务器的上限。

**多久。** 读取时间是你服务的最慢的访客发送你接受的最大文件可能花的时间。4 MiB 在 1 Mbit/s 的线路上大约要 34 秒，所以 5 分钟留有余地。时间长会让慢客户端占住连接那么久，所以不要把它给不需要的路由。

**接受什么。** 一份种类的名单，由字节来决定。对文档，用它们自己的类型做同样的事，并用 `Content-Disposition: attachment` 来提供它们，让浏览器保存而不是打开它们。

## 检查一下 {#check-it}

```sh
go tool tifa check
go tool tifa dev
```

打开 `/photos`。不填标题或不选文件就发送，每条消息都显示在各自的输入框下面。发送一个叫 `a.png` 的文本文件，它会因为字节而被拒绝。发送一个超过上限的文件，表单会说它太大。发送一张图片，它会出现在列表里，由 `/photos/{id}` 提供。
