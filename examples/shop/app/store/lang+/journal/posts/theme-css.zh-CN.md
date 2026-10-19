很多店主问我们：能不能把店铺的颜色改成自己的品牌色？可以，而且不用碰模板。店铺主题里所有颜色、圆角和字体都来自十几个 CSS 变量，在“设置 → 主题 → 自定义 CSS”里覆盖它们就行。

## 主题读哪些变量

| 变量 | 默认值 | 用在哪里 |
| --- | --- | --- |
| `--background` | `#f7f8f5` | 页面底色 |
| `--foreground` | `#26352f` | 正文文字 |
| `--primary` | `#284e3e` | 按钮、链接、价格 |
| `--muted-foreground` | `#7c857e` | 次要文字 |
| `--border` | `#e6e9e3` | 卡片和输入框的边框 |
| `--radius` | `0.625rem` | 所有圆角的基准 |
| `--font-sans` | `Inter Variable` | 正文字体 |

## 换一套颜色

下面是一家花店的例子，把主色换成了深玫瑰色，背景换成暖白：

```css
:root {
  --background: #fbf8f4;
  --foreground: #3a2a2c;
  --primary: #8c3b4a;
  --primary-foreground: #fbf8f4;
  --muted-foreground: #8f7f80;
  --border: #eadfd9;
  --radius: 1rem;
}
```

颜色可以写成十六进制，也可以用 `oklch()`。我们更推荐后者，调亮度时色相不会跑偏：

```css
:root {
  --primary: oklch(0.45 0.12 10);
  --ring: oklch(0.7 0.08 10);
}
```

## 只改某一类页面

每个页面的 `<body>` 上都有一个 `data-page` 属性，可以用它只改商品页：

```css
body[data-page="product"] {
  --radius: 0;
}

body[data-page="product"] .price {
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.01em;
}
```

## 换字体

自定义字体需要先在“文件”里上传字体文件，然后用 `@font-face` 引用：

```css
@font-face {
  font-family: "Shop Serif";
  src: url("/files/shop-serif.woff2") format("woff2");
  font-display: swap;
}

:root {
  --font-sans: "Shop Serif", "Noto Serif SC", serif;
}
```

注意 `font-display: swap`，没有它的话字体加载完之前文字是看不见的。

## 预览和回滚

保存前可以点“预览”，在新标签页里看效果。每次保存都会留下一个版本，最多保留 20 个，在“历史”里可以一键回到任何一个版本。

如果改坏了，最快的办法是在地址后面加上 `?theme=default`，用默认主题打开店铺，再去后台改回来。

```html
<!-- 自定义 CSS 最终会插在主题样式表之后 -->
<link rel="stylesheet" href="/assets/theme.css">
<style id="custom-css">/* 你写的内容 */</style>
```

有问题可以在后台右下角找到我们。
