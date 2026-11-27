---
summary: '应用的设置从哪里来：tifa.yaml、参数、环境变量、.env、PORT、应用的选项、值的格式、配置错误'
audience: app
order: 17
translatedFrom: bd4ab618903f
read_when:
  - '用 tifa.String 之类的函数定义应用的选项，或者在 OnStart 里读取它'
  - '浏览器需要一个只有程序运行时才知道的值（比如公钥或 API 基础地址），而 `import.meta.env.VITE_*` 在构建时就固定了'
  - '编写 .env 文件，或者部署到设置了 PORT 的平台上'
  - '程序因配置错误拒绝启动'
  - '修改 internal/config'
---

# 配置 {#configuration}

Tifa 应用的默认值是为小机器定的，开箱即用。只有需要时才改设置。

## 设置从哪里来 {#where-settings-come-from}

优先级从高到低：

1. 命令行参数，比如 `--server-addr 0.0.0.0:9527`。
2. 环境变量，比如 `SERVER_ADDR=0.0.0.0:9527`。
3. 程序运行所在文件夹里的 `.env` 文件。
4. 用 `-c path.yaml`（或 `--config path.yaml`）指定的部署文件。
5. 应用根目录里的项目文件 `tifa.yaml`。`tifa build` 读取它，并把它的值构建进程序。
6. 默认值：Tifa 的设置用[设置表](../reference/settings.zh-CN.md#settings)里的，[应用的选项](#options-of-the-app)用 `options.go` 里的。

每个来源只设置它写出的键，所以部署文件只放它要改的内容。程序只读自己的键：`-c` 的文件里的其他键和其他环境变量都不管。`tifa.yaml` 更严格，见下文。既不是 Tifa 的设置、也不是应用的设置的参数，比如用 `--addr` 代替 `--server-addr`，会让程序以 `unknown flag` 停止。

`tifa.yaml` 放的是 Tifa 设置的值。`build.*`、`i18n.*`、`site.*` 和 `deploy.*` 设置只能在这里设置，值的类型或格式不对时 `tifa build` 会失败。程序不读这个文件：改了它要重新构建。

`tifa.yaml` 只放 Tifa 的设置，不放应用的任何键。里面有不是 Tifa 设置的键，比如 `deploy.ssh` 或拼错的 `server.adr`，会让 `tifa check`、`tifa build`、`tifa dev` 和部署命令在那一行失败，附近有相近的名字时会给出最接近的那个。应用自己的选项，即使放在 Tifa 的某一节里（比如 `server.name`），也只能用参数、环境变量、`.env` 或 `-c` 的文件来设置，永远不要写进 `tifa.yaml`。

```yaml
# tifa.yaml
build:
  target: es2020,safari14
server:
  addr: 0.0.0.0:9527
  body_limit: 4MiB
ssr:
  timeout: 2s
  memo_size: 128MiB
```

运行 `./bin/app config` 会打印每一项设置（Tifa 的和应用的），带它的值和值的来源（`default`、`tifa.yaml`、`file prod.yaml`、`.env SSR_VMS`、`env PORT`、`flag --ssr-vms` 等等）。输出是一个带注释的 YAML 文件，是写部署文件的好起点。它不包含应用的密钥的值（[`tifa.Secret`](#options-of-the-app)）。

### 命名 {#names}

每项设置都有一个键、一个参数和一个环境变量，都出自同一条规则：

| 键 | 参数 | 环境变量 |
| --- | --- | --- |
| `db.url` | `--db-url` | `DB_URL` |
| `server.body_limit` | `--server-body-limit` | `SERVER_BODY_LIMIT` |

- 键由小写字母、数字和下划线组成，至少两段，用点连接。
- 参数把点和下划线换成连字符。环境变量把点换成下划线，并用大写。没有前缀。
- Kubernetes 会为命名空间里的每个 Service 往每个 pod 里放一个环境变量，比如名为 `db` 的 Service 会得到 `DB_PORT=tcp://10.0.0.5:5432`。键以某个 Service 的名字加 `.port` 结尾的选项或设置，会读到这个值。请在 pod spec 里设置 `enableServiceLinks: false`，或者不要把选项的键命名为 `<service>.port`。
- 在 YAML 文件里，键的每一段是一层：`db.url` 是 `db:` 下的 `url`。
- 参数的值可以跟在空格后面，也可以跟在 `=` 后面：`--ssr-vms 4` 或 `--ssr-vms=4`。以 `-` 开头的词一定是参数，从不当作值，所以以 `-` 开头的值（比如负数）要放在 `=` 后面：`--ssr-vms=-1`。参数没有值会失败。程序不读 `--` 之后的任何参数。

### .env {#env}

程序读取它运行所在文件夹里的 `.env` 文件，每行是 `NAME=value`。真正的环境变量优先于 `.env` 里的同名变量。没有这个文件时什么也不会发生。`tifa build` 不会把它放进程序，每台机器只有一个 `.env`，没有 `.env.local` 或 `.env.production`：不要把它放进 git。

```sh
# .env
DB_URL=/var/lib/shop/shop.db
SERVER_ADDR=0.0.0.0:9527
export SESSION_MAX_AGE=24h   # a line may start with export
GREETING="Hello # not a comment\n"
```

- 以 `#` 开头的行和空行会跳过。不带引号的值在空格或制表符之后的 `#` 处结束，并去掉两边的空格，所以 `TLS_CERT= # none` 是空的，`COLOR=#fff` 是 `#fff`。
- 双引号里的值可以包含 `\n`、`\t`、`\"` 和 `\\`。单引号里的值原样取用。带引号的值必须在一行里。
- 读不了的行会让程序停止，并给出它的行号。
- `GOMEMLIMIT`、`GOGC` 和 `GOMAXPROCS` 写在 `.env` 里和写在环境变量里效果一样。Go 在程序读 `.env` 之前就读这几个名字，所以程序启动时先自己把它们设上，`ssr.vms` 也会跟着 `.env` 里的 `GOMAXPROCS` 走。Go 不接受的值会让程序停止。Go 读的其他名字（比如 `GODEBUG`）只在真正的环境变量里有效。
- `tifa deploy` 也从这个文件读它的输入：`TIFA_DEPLOY_HOST` 用来知道服务器，其他以 `TIFA_DEPLOY_` 开头的名字也一样。程序会忽略它们。没有 `TIFA_DEPLOY_HOST` 时，`tifa deploy` 用它记在 `~/.tifa/config.yaml` 里的服务器，`TIFA_HOME` 可以移动这个文件夹（见[用 tifa deploy 部署](deploy.zh-CN.md#deploy-with-tifa-deploy)）。

`tifa dev` 在应用根目录里运行应用，所以应用读取那里的 `.env`，`.env` 变了会让应用重新启动。Vite 不读 `.env`（`envDir: false`）：页面从它的 `Load` 拿它需要的值。

### PORT 和 HOST {#port-and-host}

Cloud Run、Heroku、Render、Railway 和 Fly 这样的托管平台，通过 `PORT` 告诉程序它的端口。程序为 `server.addr` 读取 `PORT` 和 `HOST`（不读 `HOSTNAME`，shell 和 Docker 会把它设成机器名）：

- 它们的位置在 `SERVER_ADDR` 之下：`--server-addr` > `SERVER_ADDR` > `PORT` 和 `HOST` > `-c` 的文件 > `tifa.yaml` > 默认值。`.env` 里也可以放它们，优先级低于真正的环境变量。
- 只有 `PORT` 时，监听所有网卡：`PORT=8080` 就是 `:8080`，这是那些平台需要的。
- 只有 `HOST` 时，只改主机，端口沿用下面各来源里 `server.addr` 的端口。
- 空值算作没设置。

`tifa dev` 通过 `SERVER_ADDR` 把地址交给应用，所以你 shell 里的 `PORT` 不会改动它。`tifa dev` 本身监听 `--addr`，不读 `PORT`。

## 应用的选项 {#options-of-the-app}

应用在它根目录的 `options.go` 里，把自己的选项定义为包级变量：

```go
// options.go
package shop

import (
	"time"

	"github.com/liustack/tifa"
)

var (
	DBURL   = tifa.String("db.url", "app.db", "Database file")
	Workers = tifa.Int("jobs.workers", 4, "Background workers")
	Hosts   = tifa.Strings("feed.hosts", nil, "Hosts the feed is read from")
	Open    = tifa.Bool("shop.open", false, "Let visitors in without signing in")
	Idle    = tifa.Duration("db.idle", time.Minute, "How long an idle connection stays open")
	Upload  = tifa.Size("upload.limit", 8<<20, "Largest upload")
	APIKey  = tifa.Secret("payments.api_key", "Key of the payment provider")
)
```

- 参数是键、默认值和一行帮助。每个都返回一个普通的值：读 `shop.DBURL` 即可，测试里可以设 `shop.DBURL = "test.db"`。
- 值来自和 Tifa 设置相同的来源：参数 `--db-url`、变量 `DB_URL`、`.env`、`-c` 的文件，然后是默认值。`tifa.yaml` 只放 Tifa 的设置。
- 类型有：`String`（`string`）、`Strings`（`[]string`）、`Int`（`int`）、`Bool`（`bool`）、`Duration`（`time.Duration`）和 `Size`（`int64` 字节）。它们的值的写法和 Tifa 设置的一样，见[值的格式](#value-formats)。`Strings` 是用逗号分隔的文本，比如 `FEED_HOSTS=a.example,b.example`。
- `Secret`（`string`）用于密码、令牌或密钥。它没有默认值，没有来源设置它时是空的。它来自环境变量、`.env` 或 `-c` 的文件，不来自参数：参数会出现在进程列表里，机器上的每个用户都看得到，所以程序拒绝它，并说明该把值放在哪里。`./bin/app config` 会写出它来自哪里，但不写值，只写注释 `# api_key: (hidden)`。关于密钥的错误，比如文件里有一个没加引号的数字，会写出键和来源，但不写值。以 `*`、`!`、`&` 或其他 YAML 符号开头的密钥要放进引号里。
- 值都是单个值。结构化的数据放进应用的一个文件里，选项只保存它的路径。
- 无效的值会得到默认值，程序会把它和其他无效的设置和选项一起列出来，并在 `OnStart` 之前停止。
- 在 `go test` 里，不管参数、变量和 `.env` 是什么，选项都保持默认值。
- 根包导入的包读不到这些变量（那会造成导入循环）：由 `OnStart` 把值传给它们，比如 `store.Open(DBURL)`。
- 需要更多的应用，可以在 `OnStart` 里运行它自己的配置。

`tifa build` 在这些情况下会失败，并写出文件和行号：`tifa.String` 之类在根目录 `options.go` 里的包级 `var` 的值之外的任何地方被调用，键不是字符串字面量或不符合[命名](#names)的规则，两项设置（应用的或 Tifa 的）会有同样的参数或环境变量（比如 `a.b_c` 和 `a_b.c`），以及一个键是另一个键的上级（比如 `db.url` 旁边有 `db.url.timeout`，或者 Tifa 的 `server.addr` 旁边有 `server.addr.port`）。如果这些错误漏过了构建，程序会在同样的错误上 panic。

### 运行时才知道的、给浏览器的值 {#a-value-for-the-browser-that-is-known-at-run-time}

`import.meta.env.VITE_*` 在 `tifa build` 运行时就固定了，所以新值需要重新构建。对于只有程序运行时才知道的值，比如公钥或 API 的基础 URL，把它定义为选项，并从 `Load` 返回（或者从 `Layout` 返回，这样它下面的每个页面都有）。`Load` 返回的一切都在页面里，所以绝不要返回 `tifa.Secret`。见[来自环境的值](../guides/environment.zh-CN.md)。

## 值的格式 {#value-formats}

- 大小是整数，带可选的单位：`B`、`KiB`、`MiB` 或 `GiB`，比如 `512KiB`。纯数字是字节。`MB`、`KB`、`GB` 和 `4 MiB` 会被拒绝，`1.5MiB` 这样的小数也一样（写成 `1536KiB`）。只有 `ssr.memo_size` 可以是 0。
- 时长用 Go 的格式，必须为正：`500ms`、`3s`、`1m30s`。应用的 `tifa.Duration` 也可以是 0 或负数。
- 在 YAML 文件里，整数设置是数字（`vms: 2`，不是 `"2"`），`true` 或 `false` 的设置是布尔值，其他每个设置是文本。大小可以是字节数，也可以是带单位的文本。
- `ssr.vms` 是至少为 1 的整数。
- `ssr.queue_limit` 是至少为 0 的整数。即使队列限制是 0，`ssr.wait_timeout` 也必须为正。
- `ssr.vm_stack_depth` 是 1 到 10000 的整数，10000 是引擎允许调用嵌套的最深值。
- `build.skip_tanstack_check`、`build.debug_info`、`build.i18next_cache`、`server.compress` 和 `server.hsts` 是 `true` 或 `false`，在文件里不加引号。单独的参数就是 `true`，只有跟在 `=` 后面才取别的值：`--server-compress=false`。它从不取后面的那个词。
- `server.trusted_origins` 和 `server.cross_site_paths` 是用逗号分隔的列表，里面没有括号或引号，可以为空。更高的来源可以清空更低的来源设置的内容：`SERVER_TRUSTED_ORIGINS=` 会清掉项目文件里的 origin。origin 需要 `http` 或 `https`、一个主机，没有路径、查询或用户名，不合格的会失败，并说明哪里不对。`http.ServeMux` 拒绝的模式，或者和列表里另一个冲突的模式，也会失败。
- `server.tls_domains` 是用逗号分隔的主机名列表，不带协议、端口或路径。每个都需要有一个点。`server.tls_ca` 是 `https` URL。`server.http_addr` 是带端口的地址，比如 `:80` 或 `0.0.0.0:80`，或者小写的 `off`。读起来像开关的词，比如 `OFF`、`false` 或 `none`，会失败，并说明该写什么。
- `server.csp` 是小写的 `off`、`on` 或 `report-only`。`true`、`false`、`yes` 之类会失败，并说明该写什么。`server.csp_policy` 不能为空：不想发策略，就把 `server.csp` 设为 `off`。
- `server.tls_domains`、`server.tls_email`、`server.tls_cache`、`server.tls_cert` 和 `server.tls_key` 可以为空，这会关掉更低的来源设置的内容：`SERVER_TLS_DOMAINS=` 加 `--server-tls-cert`，就会在项目文件要求 Let's Encrypt 的地方提供你自己的证书。

## 错误 {#errors}

`tifa.yaml` 里的值类型或格式不对时，`tifa build` 会失败，并写出它的行号。写成一个小节或列表的设置，比如 `ssr: {vms: {count: 2}}`，也是类型不对的值。在这些情况下，程序拒绝启动，并列出每个问题，各带来源和键：

- Tifa 的设置或应用的设置的值，类型或格式不对，比如 `ssr.timeout: 3`、`SHOP_OPEN=maybe`，或者 `-c` 文件里 `ssr.vms` 下有一个小节，
- 应用的密钥（`tifa.Secret`）是作为参数给出的，
- `-c` 的文件读不了或不是 YAML，或者 `.env` 的某一行读不了。

不是设置的参数会让程序以 `unknown flag` 停止。不是设置的键或变量不算错误，所以拼错的，比如 `SERVER_PORT`，什么也改不了：`./bin/app config` 会显示每项设置最终是什么。`build.*` 设置只从 `tifa.yaml` 读：部署文件、`BUILD_*` 变量或 `--build-*` 参数都改不了它们。

在这些情况下，它也会在 `OnStart` 之前拒绝启动，并写出相关的设置：

- 开启了 HTTPS，而 `server.addr` 是默认的 `127.0.0.1:9527`（省略或写出来都算），并且 systemd 没有为它传入 socket，
- 把所有来源合并之后，HTTPS 设置对不上：`tls_domains` 和 `tls_cert` 同时设置，有 `tls_cert` 没有 `tls_key` 或反过来，有 `tls_domains` 没有 `tls_cache`，或者有 `tls_email` 或 `tls_cache` 没有 `tls_domains`。只看项目文件时，可以省略部署补上的内容，所以 `tifa build` 不检查这一点，
- `tls_cert` 和 `tls_key` 的文件加载不了，或者 `tls_cache` 创建不了。

当 `server.addr` 或 `server.http_addr` 没法监听时，错误里会写出这项设置，对于只有 root 才能占用的端口，还会说明该怎么做。

`serve --dist <folder>` 和 `serve --lifeline-fd <fd>` 是只给 [`tifa dev`](dev.zh-CN.md) 用的参数。它们不是设置，也没有环境变量。`tifa build` 生成的程序没有 `--lifeline-fd`。
