import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import styled, { ServerStyleSheet, StyleSheetManager, ThemeProvider } from 'styled-components'

export const head = () => ({ meta: [{ title: 'styled-components' }] })

const theme = { main: '#1d4ed8' }

const Button = styled.button`
  background: ${(p) => p.theme.main};
  color: white;
  border: 0;
  padding: 6px 12px;
  border-radius: 6px;
`
const Input = styled.input`
  border: 2px solid #888;
  padding: 6px;
  border-radius: 6px;
`
const Select = styled.select`
  border: 2px solid #888;
  padding: 6px;
`
const Tab = styled.button<{ $active: boolean }>`
  border: 0;
  background: none;
  padding: 6px 12px;
  border-bottom: 2px solid ${(p) => (p.$active ? p.theme.main : 'transparent')};
`
const Backdrop = styled.div`
  position: fixed;
  inset: 0;
  background: rgb(0 0 0 / 0.4);
`
const Box = styled.div`
  position: fixed;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  background: white;
  padding: 16px;
  border-radius: 8px;
`

// The usual SSR setup of styled-components wraps the render call in a ServerStyleSheet and writes
// getStyleTags() into the head. Tifa owns both the render call and the head, so this page does it
// inside the tree: a sheet per render, and a last sibling that writes the styles the components
// before it have registered (React renders siblings in order, in one pass). The browser renders an
// empty style element in its place, which hydration accepts because of suppressHydrationWarning.
const onServer = typeof document === 'undefined'

function Collected({ sheet }: { sheet?: ServerStyleSheet }) {
  const css = sheet ? sheet.getStyleTags() : ''
  return <style suppressHydrationWarning dangerouslySetInnerHTML={{ __html: css.replace(/^<style[^>]*>|<\/style>$/g, '') }} />
}

function Content() {
  const [open, setOpen] = useState(false)
  const [tab, setTab] = useState('one')
  useEffect(() => {
    const close = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('keydown', close)
    return () => document.removeEventListener('keydown', close)
  }, [])
  return (
    <main>
      <h1>styled-components</h1>
      <Button onClick={() => setOpen(true)}>Open dialog</Button>
      <Input placeholder="Your name" />
      <Select defaultValue="b" aria-label="Fruit">
        <option value="a">Apple</option>
        <option value="b">Banana</option>
      </Select>
      <div role="tablist">
        <Tab role="tab" $active={tab === 'one'} aria-selected={tab === 'one'} onClick={() => setTab('one')}>
          One
        </Tab>
        <Tab role="tab" $active={tab === 'two'} aria-selected={tab === 'two'} onClick={() => setTab('two')}>
          Two
        </Tab>
      </div>
      <p role="tabpanel">Tab {tab} selected</p>
      {open
        ? createPortal(
            <>
              <Backdrop onClick={() => setOpen(false)} />
              <Box role="dialog" aria-label="Hello">
                <h2>Hello</h2>
                <Button onClick={() => setOpen(false)}>Close</Button>
              </Box>
            </>,
            document.body,
          )
        : null}
    </main>
  )
}

export default function StyledPage() {
  if (!onServer) {
    return (
      <ThemeProvider theme={theme}>
        <Content />
        <Collected />
      </ThemeProvider>
    )
  }
  const sheet = new ServerStyleSheet()
  return (
    <StyleSheetManager sheet={sheet.instance}>
      <ThemeProvider theme={theme}>
        <Content />
        <Collected sheet={sheet} />
      </ThemeProvider>
    </StyleSheetManager>
  )
}
