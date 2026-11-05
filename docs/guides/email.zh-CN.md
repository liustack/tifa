---
summary: '怎么用纯 Go 的库在 Tifa 应用里通过 SMTP 发邮件：邮件服务器和密码的设置，一个 mail.go 把邮件和改动放在同一个事务里排队、由后台任务发出，为什么请求从不等邮件服务器，以及怎么不真发邮件地测试'
read_when:
  - 从 Tifa 应用发欢迎邮件、收据、密码重置或任何其他邮件
  - 加 SMTP 设置，或不想把 SMTP 密码写进代码
  - 邮件服务器挂了或程序重启时，邮件不能丢
  - 给会发邮件的代码写 Go 测试
---

[English](email.md) · 简体中文

# 发送邮件

Tifa 没有邮件发送器。应用通过邮件服务商的 SMTP 服务器发邮件，而且是在后台任务里发，所以访客不用等，邮件服务器挂了邮件也不会丢。

流程是：处理函数把邮件和它的改动放在同一个事务里排队。[后台任务](jobs.zh-CN.md)里的 worker 取走任务并发出。邮件服务器出错，任务稍后重试。

代码来自用 `tifa new` 生成的试验应用（模块 `example.com/notes`），用到[用 SQLite 存数据](database.zh-CN.md)里的数据库和[后台任务](jobs.zh-CN.md)里的 `jobs` 包。

## 用哪个库

用 [go-mail](https://github.com/wneessen/go-mail)（`github.com/wneessen/go-mail`）。它是纯 Go 的，所以 `CGO_ENABLED=0` 照样能用，而且替你处理了手写最容易出错的部分：中文主题和正文的编码、STARTTLS，以及 465 端口上一连上就用 TLS。标准库有 `net/smtp`，但它自己的文档说它已冻结、不再加新功能，而且构造一封格式正确的邮件要你自己来。

```sh
go get github.com/wneessen/go-mail@v0.8.1
```

示例用的是 v0.8.1。升级时对照它的[文档](https://pkg.go.dev/github.com/wneessen/go-mail)。

## 设置

主机、端口和密码在每台机器上都不同，所以它们是应用的设置（见[应用的配置](../config.zh-CN.md#应用的配置)）：

```go
// config.go
var (
	// SMTPHost is the mail server. Empty means mail is written to the log and not sent.
	SMTPHost = tifa.String("smtp.host", "", "SMTP server host, empty to log mail instead of sending it")
	SMTPPort = tifa.Int("smtp.port", 587, "SMTP server port")
	// SMTPTLS is starttls (port 587), tls (port 465, TLS from the first byte) or none (a local
	// mail catcher only).
	SMTPTLS  = tifa.String("smtp.tls", "starttls", "How to secure the connection: starttls, tls or none")
	SMTPUser = tifa.String("smtp.user", "", "SMTP user name")
	// SMTPPassword comes from the environment, .env or the file of -c, never from a flag.
	SMTPPassword = tifa.Secret("smtp.password", "SMTP password")
	// MailFrom is the sender address of every mail.
	MailFrom = tifa.String("mail.from", "notes@example.com", "Sender address of mail")
)
```

在服务器上：

```sh
# .env, in the folder the program runs in. Not in git.
SMTP_HOST=smtp.example.com
SMTP_USER=notes@example.com
SMTP_PASSWORD=...
MAIL_FROM=notes@example.com
```

- **密码不要写进代码。** `tifa.Secret` 没有默认值，用命令行参数给它程序会报错，因为参数会出现在进程列表里。用环境变量，或工作目录里的 `.env` 文件（见 [.env](../config.zh-CN.md#env)）。`tifa.yaml` 只放 Tifa 自己的设置，不是放它的地方。`./myapp config` 只写密码的来源，不写密码。
- **`smtp.host` 为空就什么也不发。** 邮件连同正文被写进日志。`tifa dev` 下和第一次部署时这正是你要的，但不要在生产环境留着，否则重置链接会留在日志里。
- **用邮件服务商的 SMTP 服务**，从你自己域名下的地址发，并设好服务商告诉你的 SPF 和 DKIM 记录。直接从小 VPS 发的邮件常常进垃圾箱，有的主机还封了出站的 25 端口。服务商接受 587 端口（`starttls`）或 465 端口（`tls`）。
- `smtp.tls` 写错应该让应用在启动时停下，而不是等到第一封邮件。`OnStart` 调用 `clientOptions`（见下）并返回它的错误。

## mail.go

根目录的 `mail.go` 是应用的共享资源（见[应用的代码结构](../structure.zh-CN.md#2-共享资源和其他依赖)）：

```go
// mail.go
package notes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	netmail "net/mail"
	"time"

	"github.com/wneessen/go-mail"

	"example.com/notes/jobs"
)

// Mail is one plain text message.
type Mail struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// SendMail delivers m. Tests replace it with a function that records the mail.
var SendMail = sendSMTP

// QueueMail adds a job that sends m. Pass the transaction of the change that causes the mail,
// and the mail is queued if and only if the change is saved. The request does not wait for the
// mail server.
func QueueMail(ctx context.Context, db jobs.Execer, m Mail) error {
	// A wrong address would fail five times and then sit in the table. Refuse it here, where the
	// caller can still tell the visitor.
	if _, err := netmail.ParseAddress(m.To); err != nil {
		return fmt.Errorf("mail: recipient: %w", err)
	}
	return jobs.Enqueue(ctx, db, "mail", m)
}

// mailJob is the handler of the "mail" job. An error makes the job try again later.
func mailJob(ctx context.Context, payload []byte) error {
	var m Mail
	if err := json.Unmarshal(payload, &m); err != nil {
		return err
	}
	return SendMail(ctx, m)
}

func sendSMTP(ctx context.Context, m Mail) error {
	if SMTPHost == "" {
		slog.InfoContext(ctx, "mail not sent, smtp.host is empty", "to", m.To, "subject", m.Subject, "text", m.Text)
		return nil
	}
	msg := mail.NewMsg()
	if err := msg.From(MailFrom); err != nil {
		return err
	}
	if err := msg.To(m.To); err != nil {
		return err
	}
	msg.Subject(m.Subject)
	msg.SetBodyString(mail.TypeTextPlain, m.Text)

	opts, err := clientOptions()
	if err != nil {
		return err
	}
	c, err := mail.NewClient(SMTPHost, opts...)
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	return c.DialAndSendWithContext(ctx, msg)
}

// clientOptions turns the settings into the options of the SMTP client. OnStart calls it too, so
// a wrong value stops the app at start and not at the first mail.
func clientOptions() ([]mail.Option, error) {
	opts := []mail.Option{mail.WithPort(SMTPPort), mail.WithTimeout(20 * time.Second)}
	switch SMTPTLS {
	case "starttls":
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	case "tls":
		opts = append(opts, mail.WithSSL())
	case "none":
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	default:
		return nil, fmt.Errorf("smtp.tls is %q, want starttls, tls or none", SMTPTLS)
	}
	if SMTPUser != "" {
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover), mail.WithUsername(SMTPUser), mail.WithPassword(SMTPPassword))
	}
	return opts, nil
}
```

```go
// hooks.go, at the start of OnStart
if _, err := clientOptions(); err != nil {
	return err
}
```

```go
// worker.go, in startBackground
jobs.Handle("mail", mailJob)
```

其中的取舍：

- **每封邮件一个新连接。** `DialAndSendWithContext` 连上、发送、关闭。小应用发的邮件不多，邮件之间也没有开着的连接会断。只有成批地发时，才值得一直开着一个连接。
- **`starttls` 要求 TLS**（`TLSMandatory`）：服务器不提供 TLS 时，发送失败、任务稍后重试，密码不会明文发出去。`none` 只给你自己机器上的邮件接收器用。
- **先发纯文本。** 邮件是 `text/plain`。要 HTML 部分，go-mail 在 `SetBodyString` 之后有 `msg.AddAlternativeString(mail.TypeTextHTML, html)`。HTML 用 `html/template` 生成，访客输入的内容才会被转义。
- **重试管的是服务器挂了，不是地址写错了。** 每个错误都会让任务重试，最多五次（先等 30 秒，再越来越久）。写错的地址更早就在 `QueueMail` 里用 `net/mail` 拒绝了。
- **邮件至少发一次。** 如果服务器收下了邮件，而程序在任务标成 `done` 之前停了，这封邮件会发两遍。对这很要紧的邮件，在正文里放一个 id，或把发送状态记在自己的表里。
- **Date、Message-ID 和中文主题的编码**都是 go-mail 加的。

## 在处理函数里排入邮件

和改动放在同一个事务里（见[事务](database.zh-CN.md#一次执行几条语句)）：

```go
// app/todos/todo.go
func AddTodo(ctx context.Context, title string) (Todo, error) {
	t := Todo{Title: strings.TrimSpace(title)}
	tx, err := notes.DB.BeginTx(ctx, nil)
	if err != nil {
		return Todo{}, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `INSERT INTO todos (title, created_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now')) RETURNING id`, t.Title).Scan(&t.ID)
	if err != nil {
		return Todo{}, err
	}
	if notes.MailNotify != "" {
		m := notes.Mail{To: notes.MailNotify, Subject: "New todo: " + t.Title, Text: "Someone added a todo:\n\n" + t.Title + "\n"}
		if err := notes.QueueMail(ctx, tx, m); err != nil {
			return Todo{}, err
		}
	}
	return t, tx.Commit()
}
```

`MailNotify` 是又一项设置（`tifa.String("mail.notify", "", ...)`）：为空就不发。事务一提交，处理函数就回答。待办和它的邮件要么都保存，要么都不保存。

发给访客的邮件，比如收据，用同样的方式排队，`To` 里放访客的地址。访客主动要的邮件（密码重置）也仍然是任务：页面说「已发送」，邮件一秒后到。

## 不真发邮件地测试

三个层次，由浅到深。

**1. 排队邮件的代码。** 数任务行。不运行任何邮件代码：

```go
// app/todos/todo_test.go
func TestAddTodoQueuesAMailOnlyWhenAskedTo(t *testing.T) {
	testdb.Use(t)
	ctx := context.Background()
	count := func() (n int) {
		if err := notes.DB.QueryRow(`SELECT count(*) FROM jobs WHERE kind = 'mail'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := AddTodo(ctx, "No mail"); err != nil || count() != 0 {
		t.Fatalf("without mail.notify: %d mails queued, %v", count(), err)
	}
	old := notes.MailNotify
	notes.MailNotify = "me@example.com"
	t.Cleanup(func() { notes.MailNotify = old })
	if _, err := AddTodo(ctx, "With mail"); err != nil || count() != 1 {
		t.Fatalf("with mail.notify: %d mails queued, %v", count(), err)
	}
}
```

**2. 任务。** 替换 `SendMail` 并运行 worker，就像试验应用里的 `TestMailJobSendsWhatWasQueued`。把替换函数放进数据库那篇的 `testdb` 包，让每个测试都能用：

```go
// testdb/testdb.go
// CaptureMail replaces notes.SendMail for the length of the test and returns the mail it was
// asked to send, in order.
func CaptureMail(t *testing.T) *[]notes.Mail {
	t.Helper()
	var sent []notes.Mail
	old := notes.SendMail
	notes.SendMail = func(_ context.Context, m notes.Mail) error {
		sent = append(sent, m)
		return nil
	}
	t.Cleanup(func() { notes.SendMail = old })
	return &sent
}
```

**3. SMTP 代码本身。** 在本机端口上起一个假服务器，收一封邮件，`smtp.tls` 设成 `none`。设置、库和邮件能不能配在一起，靠这个测试才看得出来：

```go
// mail_test.go
// smtpSink listens on a local port, speaks just enough SMTP to take one message, and sends the
// whole conversation on the returned channel.
func smtpSink(t *testing.T) (port int, got <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	out := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		var seen strings.Builder
		say := func(s string) { conn.Write([]byte(s + "\r\n")) }
		say("220 sink ready")
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			seen.WriteString(line)
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case inData:
				if cmd == "." {
					inData = false
					say("250 queued")
				}
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 sink")
			case cmd == "DATA":
				inData = true
				say("354 go ahead")
			case cmd == "QUIT":
				say("221 bye")
				out <- seen.String()
				return
			default:
				say("250 ok")
			}
		}
		out <- seen.String()
	}()
	return ln.Addr().(*net.TCPAddr).Port, out
}

func TestSendSMTPDeliversToTheServer(t *testing.T) {
	port, got := smtpSink(t)
	oh, op, ot := SMTPHost, SMTPPort, SMTPTLS
	SMTPHost, SMTPPort, SMTPTLS = "127.0.0.1", port, "none"
	t.Cleanup(func() { SMTPHost, SMTPPort, SMTPTLS = oh, op, ot })
	err := sendSMTP(context.Background(), Mail{To: "you@example.com", Subject: "Hello 你好", Text: "Body text"})
	if err != nil {
		t.Fatal(err)
	}
	var talk string
	select {
	case talk = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("the server saw nothing")
	}
	for _, want := range []string{"RCPT TO:<you@example.com>", "MAIL FROM:<notes@example.com>", "Subject: ", "Body text"} {
		if !strings.Contains(talk, want) {
			t.Errorf("the conversation lacks %q:\n%s", want, talk)
		}
	}
}
```

这个测试不覆盖登录和 TLS，因为假服务器两样都不会。这两样要对着真实服务器手动查一次，给自己发一封邮件。

## 试一试

把同样的假服务器写成一个小程序，监听 2525 端口，用 `SMTP_HOST=127.0.0.1 SMTP_PORT=2525 SMTP_TLS=none MAIL_NOTIFY=me@example.com` 启动构建好的应用，再添加一个待办。假服务器会打印出这封邮件，主题编码成 `=?UTF-8?q?...?=`，正文是 quoted-printable。停掉假服务器，再添加一个待办，日志会写 `jobs: will retry ... connection refused`。停掉再启动应用，把假服务器开回来，邮件在重试时间到了之后送达。
