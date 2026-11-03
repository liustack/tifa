package scan

import "testing"

func TestCountWords(t *testing.T) {
	for _, c := range []struct {
		name, body string
		want       int
	}{
		{"english", "Hello, world. Don't panic: it's 42.", 6},
		{"chinese", "模型推理漂亮，就是不知道。", 11},
		{"mixed", "用 Go 写 SSR 框架", 6},
		{"japanese and korean", "ひらがなカタカナ 한국어", 11},
		{"fenced code", "one\n\n```go\nfunc main() {}\n```\n\ntwo", 2},
		{"tilde fence with a longer close", "one\n~~~\ncode here\n~~~~\ntwo", 2},
		{"indented code", "one\n\n    code here\n    more code\n\ntwo", 2},
		{"indented line of a paragraph", "one\n    two", 2},
		{"indented line of a list", "- one\n\n    two three", 3},
		{"html comment", "one <!-- a note\nover lines --> two", 2},
		{"html tags", `<div class="note">one</div> <img src="x.png" alt="a b">`, 1},
		{"link and image addresses", "[one two](https://example.org/a-b-c) ![three](./img/four.png \"five\")", 3},
		{"autolink", "see <https://example.org/a/b> and <me@example.org>", 2},
		{"reference definition", "one [two][x]\n\n[x]: https://example.org/three", 3},
		{"headings and emphasis", "## One *two* **three** `four`", 4},
		{"empty", "", 0},
	} {
		if got := countWords([]byte(c.body)); got != c.want {
			t.Errorf("%s: countWords(%q) = %d, want %d", c.name, c.body, got, c.want)
		}
	}
}
