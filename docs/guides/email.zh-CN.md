---
summary: '怎样在 Tifa 应用里用纯 Go 的库通过 SMTP 发邮件：服务器和密码的设置、一个 mail.go 在与改动同一个事务里把邮件排进队列，再由后台任务发送、为什么请求永远不等邮件服务器，以及怎样在不真正发送的情况下测试'
audience: app
order: 5
translatedFrom: 85fd1c26ebd9
read_when:
  - '从 Tifa 应用发欢迎邮件、收据、密码重置或其他任何邮件'
  - '添加 SMTP 设置，或者不让 SMTP 密码出现在代码里'
  - '邮件服务器宕机或程序重启时，邮件不能丢'
  - '为发邮件的代码写 Go 测试'
---

# 发送邮件 {#sending-email}

Tifa 没有邮件发送器。应用通过邮件服务商的 SMTP 服务器发邮件，在一个后台任务里发，这样访客不用等它，服务器挂了的时候邮件也不会丢。

流程是这样的：处理函数在改动数据的同一个事务里排入一封邮件。[后台任务](jobs.zh-CN.md) 的 worker 取走这个任务并发送。如果邮件服务器出错，任务稍后重试。

代码来自一个用 `tifa new` 建的临时应用（模块 `example.com/notes`），用到 [在 SQLite 里保存数据](database.zh-CN.md) 里的数据库，以及 [后台任务](jobs.zh-CN.md) 里的 `jobs` 包。

## 库 {#the-library}

用 [go-mail](https://github.com/wneessen/go-mail)（`github.com/wneessen/go-mail`）。它是纯 Go 的，所以 `CGO_ENABLED=0` 仍然可用，而且它处理了手写时容易出错的事：对中文的主题或正文编码、STARTTLS，以及 465 端口上从第一个字节开始的 TLS。标准库有 `net/smtp`，但它自己的文档说它已经冻结，不再加新功能，而且构建一封正确的邮件要你自己来。

```sh
go get github.com/wneessen/go-mail@v0.8.1
```

示例用的是 v0.8.1。升级时请查看 [文档](https://pkg.go.dev/github.com/wneessen/go-mail)。

## 设置 {#settings}

主机、端口和密码在每台机器上都不同，所以它们是应用的选项（[应用的选项](../build-and-deploy/config.zh-CN.md#options-of-the-app)）：

```go
// options.go
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

- **不要把密码放进代码。** `tifa.Secret` 没有默认值，程序也拒绝把它作为命令行参数，因为那会出现在进程列表里。请用环境变量，或工作目录里的 `.env` 文件（[.env](../build-and-deploy/config.zh-CN.md#env)）。`tifa.yaml` 只放 Tifa 自己的设置，所以它不是放密码的地方。`./myapp config` 会说明密码来自哪里，并且不显示它。
- **`smtp.host` 为空时什么也不发。** 邮件连同它的文字一起写进日志。在 `tifa dev` 下和第一次部署时，这正是你想要的，但不要在生产环境里一直这样，因为重置链接会留在日志里。
- **用邮件服务商的 SMTP 服务**，并从你拥有的域名下的地址发送，按服务商告诉你的设置 SPF 和 DKIM 记录。从小 VPS 直接发出的邮件常常进垃圾箱，有些主机还会封锁出站的 25 端口。服务商接受 587 端口（`starttls`）或 465 端口（`tls`）。
- 错误的 `smtp.tls` 应该让应用在启动时就停下，而不是等到第一封邮件。`OnStart` 调用 `clientOptions`（见下文）并返回它的错误。

## mail.go {#mailgo}

根目录的 `mail.go` 是应用的一个共享资源（[组织一个应用](../start/structure.zh-CN.md#2-shared-resources-and-other-dependencies)）：

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

// clientOptions turns the options into the options of the SMTP client. OnStart calls it too, so
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

- **每封邮件一个新连接。** `DialAndSendWithContext` 连接、发送、再关闭。小应用发的邮件很少，邮件之间也没有什么开着的东西会坏。只有在成批发送时才让一个连接一直开着。
- **`starttls` 要求 TLS**（`TLSMandatory`）：如果服务器不提供，发送就失败，任务会重试，而不是让密码明文发出去。`none` 用于你自己机器上的收信工具。
- **先发纯文本。** 邮件是 `text/plain`。要加 HTML 部分，go-mail 有 `msg.AddAlternativeString(mail.TypeTextHTML, html)`，写在 `SetBodyString` 之后。HTML 用 `html/template` 来构建，这样访客输入的内容会被转义。
- **重试应付的是服务器宕机，不是地址写错。** 每个错误都会让任务重试，最多五次（30 秒，之后更久）。写错的地址更早就被拒绝了，在 `QueueMail` 里，用的是 `net/mail`。
- **邮件至少发一次。** 如果服务器收下邮件之后、任务标为 `done` 之前程序停了，邮件会发出两次。对在意这一点的邮件，在正文里放一个 id，或者把发送状态保存在你自己的表里。
- **Date、Message-ID 和中文主题的编码** 都由 go-mail 添加。

## 在处理函数里排入邮件 {#queue-a-mail-from-a-handler}

和数据改动放在同一个事务里（[事务](database.zh-CN.md#several-statements-at-once)）：

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

`MailNotify` 是又一个选项（`tifa.String("mail.notify", "", ...)`）：为空就不发。事务一提交，处理函数就响应。待办和它的邮件要么都保存了，要么都没有。

发给访客的邮件，比如收据，用同样的方式排入，把访客的地址放进 `To`。访客要求发的邮件（密码重置）仍然是一个任务：页面说「我们已经发了」，邮件一秒之后到达。

## 不发送也能测试 {#test-without-sending}

三个层次，从便宜到彻底。

**1. 排入邮件的代码。** 数任务的行数。不会运行任何邮件代码：

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

**2. 任务。** 替换 `SendMail`，排入一封邮件并运行 worker，然后检查这封邮件有没有发出去。把这个替换放进数据库指南里的 `testdb` 包，这样每个测试都能用：

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

**3. SMTP 代码本身。** 在本地端口上放一个只收一封邮件的假服务器，把 `smtp.tls` 设成 `none`。正是这个测试能证明设置、库和邮件能配合起来：

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

这个测试不覆盖登录或 TLS，因为假服务器两者都不说。请对着真实的服务器手动检查一次，给自己发一封邮件。

## 试一试 {#try-it}

做一个类似的假服务器小程序，跑在 2525 端口上，让它一直接收邮件，用 `SMTP_HOST=127.0.0.1 SMTP_PORT=2525 SMTP_TLS=none MAIL_NOTIFY=me@example.com` 启动构建好的应用，然后加一个待办。假服务器会打印出邮件，主题编码成 `=?UTF-8?q?...?=`，正文是 quoted-printable。停掉假服务器，再加一个待办，日志会说 `jobs: will retry ... connection refused`。停掉再启动应用，把假服务器重新开起来，邮件会在重试时间之后到达。
