---
summary: 'Which codecs and levels tifa compresses answers with and why: gzip, zstd and brotli measured on the pages, navigation data and bundle files, what each costs per request, and why cached pages keep their compressed forms'
read_when:
  - Changing a codec, a level or where answers are compressed
  - Someone proposes brotli, zstd files from the build, or turning compression off by default
  - Asking why a compressed cache hit is cheaper than an uncompressed one
measured: '2026-10-19'
versions: 'tifa from 6723300 to f8c3283, quickjs v0.26.14, github.com/klauspost/compress v1.20.3, github.com/andybalholm/brotli, Go 1.27, the example shop'
---

# Compression

[Compression in Build](../build.md#compression) says what `server.compress` does. [Compression in Performance](../performance.md#compression) has the summary and the whole request table. This page adds every input, the CPU per request and the cache before and after.

## Question

A tifa program runs with no proxy in front, so it has to compress its own answers. Which codecs and levels, and at what cost to a render, to navigation data and to the bundle?

## Conclusions

- **For pages, compressing is lost in the render.** gzip takes 131 to 280 µs a page and zstd at its default level 107 to 152 µs, against renders of 10 to 35 ms (see [How fast a page renders](../performance.md#how-fast-a-page-renders)). Whole page requests are as fast either way, and pages go out about 6 times smaller.
- **zstd at its default level, not at gzhttp's fastest.** The fastest level saves 14 to 31 µs a page but gives pages 3% to 11% larger than gzip. The default level is within 3% smaller to 4% larger than gzip and still faster.
- **No brotli.** At levels 4 and 5 the pure Go encoder takes 3.7 to 5.6 times as long as gzip, for pages 3% to 16% smaller. Who wants brotli can put a proxy in front and turn `server.compress` off.
- **Navigation data shows the cost.** Its answers take 0.15 to 0.26 ms of CPU, so 22 to 43 µs of compressing is 8% to 20% fewer answers a second on a saturated core, for answers 1.8 to 5.4 times smaller.
- **The bundle is compressed once, at build.** Sending the build's `.gz` file of the 700 KB chunk gave 11,549 to 12,182 answers a second at 64 in flight, against 3,854 to 5,105 for the raw file.
- **A cache hit must not compress again.** When hits went through the compressor, a zstd hit took 0.45 ms of CPU against 0.19 ms uncompressed. Keeping the compressed forms with the page made a compressed hit 0.15 ms, cheaper than an uncompressed one.

## How it was measured

- **Machine:** a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13, Linux. cpu n and n+4 share a physical core.
- **Codecs alone:** the six pages of the example shop and the navigation data of a list, an order and the stats, each compressed in one process with the encoders reused as the server reuses them, the mean of 200 runs, two rounds. The bundle files were compressed once at the best level of each codec, for size only.
- **Whole requests:** the built shop with `--cache-size 0`, `GOMAXPROCS=2` and 2 VMs pinned to cpus 0 and 4 (one physical core), oha pinned to cpus 1 and 5 with 2 worker threads. Each step warmed up for 3 seconds and ran 15 seconds, two rounds. The server's CPU came from `/proc/<pid>/stat` before and after each step. The other four cpus were 0% to 3% busy during the steps.
- **Encodings asked for:** `identity`, `gzip, zstd` (which gets zstd) and `gzip`.
- **Cache:** the two order pages, which have `Cache = true`, in turn from the HTML cache, two rounds before and two after the commit that keeps the compressed forms (f8c3283), on the same machine and setup.

## Data

Codecs alone, bytes and µs, ranges over the two rounds. Lists, orders and stats are the two projects' pages:

| Input | Raw | gzip, default (5) | zstd, fastest | zstd, default | brotli 1 | brotli 4 | brotli 5 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| List 1 | 85,783 | 9,810 in 279 to 280 | 10,145 in 128 to 138 | 9,546 in 150 to 152 | 10,575 in 257 to 282 | 9,075 in 1,022 to 1,025 | 8,240 in 1,178 to 1,183 |
| List 2 | 80,714 | 9,290 in 256 to 259 | 9,614 in 119 | 8,984 in 143 to 150 | 9,954 in 246 to 261 | 8,546 in 957 to 971 | 7,782 in 1,080 to 1,084 |
| Order 1 | 39,068 | 8,372 in 179 to 181 | 9,236 in 96 | 8,653 in 126 to 127 | 9,467 in 194 | 8,158 in 742 to 750 | 7,459 in 831 to 836 |
| Order 2 | 37,869 | 8,263 in 177 to 180 | 9,130 in 95 to 96 | 8,563 in 126 to 128 | 9,361 in 194 to 195 | 8,032 in 750 to 751 | 7,387 in 819 to 823 |
| Stats 1 | 34,063 | 7,049 in 131 to 138 | 7,742 in 83 | 7,315 in 108 | 7,947 in 167 to 189 | 6,812 in 648 | 6,271 in 728 to 731 |
| Stats 2 | 33,870 | 6,964 in 136 | 7,659 in 82 | 7,229 in 107 | 7,844 in 165 to 186 | 6,747 in 650 to 651 | 6,209 in 720 to 723 |
| Navigation data, list | 6,632 | 1,258 in 32 | 1,248 in 18 | 1,227 in 21 | 1,393 in 34 | 1,188 in 129 to 131 | 1,063 in 134 |
| Navigation data, order | 1,350 | 737 in 18 | 750 in 11 | 739 in 13 | 774 in 21 | 707 in 71 | 651 in 68 to 69 |
| Navigation data, stats | 2,947 | 734 in 19 | 762 in 16 | 777 in 15 | 810 in 23 | 690 in 82 to 84 | 638 in 76 |

Bundle files at the best level of each codec, bytes:

| File | Raw | gzip 9 | zstd, best | brotli 11 |
| --- | --- | --- | --- | --- |
| Main chunk | 699,833 | 224,176 | 212,838 | 191,930 |
| Second chunk | 332,758 | 96,424 | 89,995 | 81,197 |
| Chunk | 59,866 | 18,462 | 18,154 | 16,455 |
| Chunk | 26,976 | 7,966 | 7,859 | 7,169 |
| Chunk | 21,474 | 7,116 | 7,067 | 6,390 |
| Stylesheet | 103,476 | 15,935 | 15,575 | 13,682 |
| List 1, for comparison | 85,783 | 9,283 | 8,794 | 7,310 |

Server CPU per request in ms, two rounds. zstd is what `gzip, zstd` gets:

| Request | In flight | Identity | zstd | gzip |
| --- | --- | --- | --- | --- |
| Six pages in turn | 1 | 22.62 to 23.32 | 23.00 to 23.23 | 23.09 to 24.17 |
| Six pages in turn | 2 | 37.47 to 38.28 | 37.13 to 37.69 | 38.60 to 38.85 |
| Navigation data, list | 1 | 0.238 to 0.256 | 0.277 to 0.278 | 0.284 to 0.287 |
| Navigation data, list | 64 | 0.243 to 0.244 | 0.266 to 0.269 | 0.284 to 0.286 |
| Navigation data, order | 64 | 0.148 to 0.154 | 0.179 | 0.185 to 0.186 |
| Navigation data, stats | 64 | 0.226 to 0.227 | 0.253 to 0.254 | 0.260 to 0.263 |
| Main chunk | 1 | 0.495 to 0.588 | the `.gz` file: 0.224 to 0.242 | the same file |
| Main chunk | 64 | 0.389 to 0.515 | the `.gz` file: 0.162 to 0.171 | the same file |

Requests a second, P50 and P99 are in [Performance](../performance.md#compression).

Cached order pages, before and after the cache kept the compressed forms:

| | In flight | Identity | zstd | gzip |
| --- | --- | --- | --- | --- |
| Before, requests/s | 1 | 5,976 to 5,980 | 3,125 to 3,180 | 2,513 to 2,527 |
| Before, requests/s | 64 | 10,095 to 10,159 | 4,410 to 4,423 | 3,711 to 3,712 |
| Before, P99 at 64 | | 16.0 to 16.1 ms | 33.6 to 33.7 ms | 36.4 to 36.6 ms |
| Before, CPU per hit | 64 | 0.194 to 0.195 ms | 0.451 to 0.452 ms | 0.537 ms |
| After, requests/s | 1 | 6,077 to 6,115 | 6,737 to 6,852 | 6,943 to 6,975 |
| After, requests/s | 64 | 10,653 to 11,174 | 13,140 to 13,245 | 13,230 to 13,294 |
| After, P99 at 64 | | 13.8 to 14.6 ms | 12.6 to 12.8 ms | 12.7 ms |
| After, CPU per hit | 64 | 0.177 to 0.185 ms | 0.150 to 0.151 ms | 0.149 to 0.150 ms |

The pages are 38,467 to 38,471 bytes as HTML, 8,607 to 8,610 with zstd and 8,317 to 8,320 with gzip.

## Why

- **A render is two orders of magnitude slower than compressing its HTML.** That is why the page rows move by less than the noise between rounds, about 1 ms of CPU per request.
- **Short answers have little else to cost.** Navigation data takes 0.15 to 0.26 ms of CPU in all, so the same tens of µs that vanish in a page show as a share of it. The data of an order is 1,350 bytes and gets only 1.8 times smaller.
- **zstd's default level costs little more than its fastest.** On these pages it takes 14 to 31 µs more and closes the size gap with gzip, and it is still faster than gzip.
- **Brotli's gain needs its slow levels.** Level 1 is larger than gzip and no faster. Levels 4 and 5 beat gzip on size but not on time, with the pure Go encoder.
- **A cache hit is bytes written.** An uncompressed hit writes 38 KB. A compressed hit with the stored form writes about 8.5 KB, so it costs less CPU than identity. Compressing on each hit put 0.26 to 0.34 ms of work back onto a 0.19 ms answer.
- **The bundle is the same for everyone.** gzip at level 9 at build costs nothing at run time. zstd and brotli files would be 5% and 14% smaller for the main chunk, but would need a second and third copy in the program and a choice by `Accept-Encoding`.

## What it means for tifa

- Done: `server.compress` is on by default, pages and data go out with gzip or zstd at the default level of each through gzhttp, the bundle as the `.gz` files `tifa build` writes, and cached pages keep their compressed forms. Every answer carries `Vary: Accept-Encoding`, and cached pages carry `Content-Length` in every form.
- Not done: brotli, for the time it takes. zstd or brotli files from the build, for the size of the program and a gain of 5% to 14% on the main chunk.

## Not measured, or uncertain

- Real networks and browsers: all requests came over the loopback, so the time saved by smaller answers is not in these numbers.
- The version of the brotli encoder was not written down.
- The identity rows of the main chunk spread from 3,854 to 5,105 answers a second between the two rounds.
- HTTP/2 and TLS: the server answered plain HTTP/1.1.
- Memory per encoder is in [Memory](../performance.md#memory), not here.
