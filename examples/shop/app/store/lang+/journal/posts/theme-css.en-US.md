Owners often ask whether they can give their shop their own brand colours. They can, without touching a template. Every colour, corner and typeface of a shop theme comes from a dozen CSS variables, and you override them under "Settings → Theme → Custom CSS".

## The variables a theme reads

| Variable | Default | Used for |
| --- | --- | --- |
| `--background` | `#f2f4f5` | The page |
| `--foreground` | `#1c2433` | Body text |
| `--primary` | `#1f3a5f` | Buttons, links and prices |
| `--muted-foreground` | `#5b6876` | Secondary text |
| `--border` | `#d3dae0` | Borders of cards and inputs |
| `--radius` | `0.125rem` | The base of every rounded corner |
| `--font-sans` | System fonts | Body type |
| `--font-heading` | `Newsreader Variable` | Headings |

## A new set of colours

Here is a florist's shop with a deep rose for the main colour and a warm white page:

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

Colours can be hex or `oklch()`. We suggest the latter: the hue stays put when you change the lightness.

```css
:root {
  --primary: oklch(0.45 0.12 10);
  --ring: oklch(0.7 0.08 10);
}
```

## Only some pages

The `<body>` of every page carries a `data-page` attribute, so you can change product pages alone:

```css
body[data-page="product"] {
  --radius: 0;
}

body[data-page="product"] .price {
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.01em;
}
```

## Another typeface

Upload the font files under "Files" first, then refer to them with `@font-face`:

```css
@font-face {
  font-family: "Shop Serif";
  src: url("/files/shop-serif.woff2") format("woff2");
  font-display: swap;
}

:root {
  --font-sans: "Shop Serif", "Noto Serif", serif;
}
```

Mind `font-display: swap`: without it, text stays invisible until the font has loaded.

## Preview and roll back

Before saving, press "Preview" to see it in a new tab. Every save keeps a version, up to 20, and "History" takes you back to any of them in one click.

If something breaks, the quickest fix is to add `?theme=default` to the address, open the shop with the default theme, and change it back in the admin.

```html
<!-- Custom CSS goes in after the theme's stylesheet -->
<link rel="stylesheet" href="/assets/theme.css">
<style id="custom-css">/* what you wrote */</style>
```

If you get stuck, find us at the bottom right of the admin.
