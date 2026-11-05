---
summary: 'How to send email from a Tifa app over SMTP with a pure Go library: the settings for the server and its password, one mail.go that queues a mail in the same transaction as the change and sends it from a background job, why a request never waits for the mail server, and how to test without sending'
read_when:
  - Sending a welcome mail, a receipt, a password reset or any other mail from a Tifa app
  - Adding SMTP settings, or keeping an SMTP password out of the code
  - A mail must not be lost when the mail server is down or the program restarts
  - Writing Go tests for code that sends mail
---

English · [简体中文](email.zh-CN.md)

# Sending email

Tifa has no mailer. The app sends mail through the SMTP server of a mail provider, from a background job, so the visitor never waits for it and a mail is not lost when the server is down.

The flow: a handler queues a mail in the same transaction as its change. The worker of [Background work](jobs.md) takes the job and sends it. If the mail server fails, the job is tried again later.

The code is from a scratch app made with `tifa new` (module `example.com/notes`), with the database of [Keeping data in SQLite](database.md) and the `jobs` package of [Background work](jobs.md).

## The library

Use [go-mail](https://github.com/wneessen/go-mail) (`github.com/wneessen/go-mail`). It is pure Go, so `CGO_ENABLED=0` still works, and it takes care of what is easy to get wrong by hand: encoding a subject or body in Chinese, STARTTLS, and TLS from the first byte on port 465. The standard library has `net/smtp`, but its own documentation says it is frozen and takes no new features, and it leaves building a correct message to you.

```sh
go get github.com/wneessen/go-mail@v0.8.1
```

The examples use v0.8.1. Check the [documentation](https://pkg.go.dev/github.com/wneessen/go-mail) when you upgrade.

## Settings

The host, port and password are different on every machine, so they are settings of the app ([Settings of the app](../config.md#settings-of-the-app)):

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

On the server:

```sh
# .env, in the folder the program runs in. Not in git.
SMTP_HOST=smtp.example.com
SMTP_USER=notes@example.com
SMTP_PASSWORD=...
MAIL_FROM=notes@example.com
```

- **Keep the password out of the code.** `tifa.Secret` has no default, and the program refuses it as a flag, which shows in the process list. Use the environment or the `.env` file of the working folder ([.env](../config.md#env)). `tifa.yaml` holds only settings of Tifa, so it is not a place for it. `./myapp config` says where the password came from and leaves it out.
- **An empty `smtp.host` sends nothing.** The mail is written to the log with its text. That is what you want under `tifa dev` and in a first deploy, but do not leave it on in production, since a reset link would sit in the log.
- **Use the SMTP service of a mail provider** and send from an address on a domain you own, with the SPF and DKIM records the provider tells you to set. Mail sent straight from a small VPS often lands in spam, and some hosts block outgoing port 25. Providers take port 587 (`starttls`) or 465 (`tls`).
- A wrong `smtp.tls` should stop the app at start, not at the first mail. `OnStart` calls `clientOptions` (below) and returns its error.

## mail.go

`mail.go` at the root is a shared resource of the app ([Structuring an app](../structure.md#2-shared-resources-and-other-dependencies)):

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

Decisions in it:

- **A new connection for each mail.** `DialAndSendWithContext` connects, sends and closes. A small app sends few mails, and nothing stays open between them to break. Keep one connection open only when you send in bursts.
- **TLS is required** with `starttls` (`TLSMandatory`): if the server does not offer it, the send fails and the job is tried again, instead of the password going out in the clear. `none` is for a catcher on your own machine.
- **Plain text first.** The mail is `text/plain`. For an HTML part, go-mail has `msg.AddAlternativeString(mail.TypeTextHTML, html)` after `SetBodyString`. Build the HTML with `html/template`, so what a visitor typed is escaped.
- **The retry covers a down server, not a wrong address.** Every error makes the job try again, up to five times (30 seconds, then longer). A wrong address is refused earlier, in `QueueMail`, with `net/mail`.
- **The mail is at least once.** If the program stops after the server took the mail and before the job says `done`, the mail goes out twice. For mails where that matters, put an id in the text, or keep the sending state in your own table.
- **Date, Message-ID and the encoding of a Chinese subject** are added by go-mail.

## Queue a mail from a handler

In the same transaction as the change ([a transaction](database.md#several-statements-at-once)):

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

`MailNotify` is one more setting (`tifa.String("mail.notify", "", ...)`): empty sends none. The handler answers as soon as the transaction commits. Either the todo and its mail are both saved or neither is.

A mail to a visitor, such as a receipt, is queued the same way, with the visitor's address in `To`. A mail the visitor asked for (a password reset) is still a job: the page says "we sent it", and the mail arrives a second later.

## Test without sending

Three levels, from cheap to thorough.

**1. The code that queues a mail.** Count the job rows. No mail code runs:

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

**2. The job.** Replace `SendMail` and run the worker, as `TestMailJobSendsWhatWasQueued` does in the scratch app. Put the replacement in the `testdb` package of the database guide so every test can use it:

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

**3. The SMTP code itself.** A fake server on a local port that takes one message, with `smtp.tls` set to `none`. This test is what shows that the settings, the library and the message fit together:

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

This test does not cover the login or TLS, since the sink speaks neither. Check those once by hand against the real server, with a mail to yourself.

## Try it

Run the same sink as a small program on port 2525, start the built app with `SMTP_HOST=127.0.0.1 SMTP_PORT=2525 SMTP_TLS=none MAIL_NOTIFY=me@example.com`, and add a todo. The sink prints the message, with the subject encoded as `=?UTF-8?q?...?=` and the body as quoted-printable. Stop the sink, add another todo, and the log says `jobs: will retry ... connection refused`. Stop and start the app, bring the sink back, and the mail arrives after its retry time.
