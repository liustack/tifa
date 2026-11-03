// The checks of the links inside <HTML> on /article/links of test/apps/pages, run by
// TestHTMLLinks with playwright-cli run-code. __BASE__ is the address of the app. It returns one
// line per check, starting with PASS or FAIL.
async (page) => {
  const base = '__BASE__'
  const out = []
  const log = (name, ok, extra = '') => out.push((ok ? 'PASS ' : 'FAIL ') + name + (extra ? ' :: ' + extra : ''))
  const consoleMessages = []
  page.on('console', (m) => {
    if (m.type() === 'error' || m.type() === 'warning') consoleMessages.push(m.type() + ': ' + m.text())
  })
  const dataRequests = []
  page.on('request', (r) => {
    if (r.url().includes('_tifa=')) dataRequests.push(r.url())
  })

  // fresh opens the links page and marks the document, which only a whole page load loses.
  const fresh = async () => {
    await page.goto(base + '/article/links')
    await page.waitForFunction(() => document.getElementById('tifa-root') !== null && Object.keys(document.getElementById('body')).some((k) => k.startsWith('__reactFiber$')))
    await page.waitForTimeout(200)
    await page.evaluate(() => {
      window.__mark = 'same-document'
    })
    dataRequests.length = 0
  }
  const same = () => page.evaluate(() => window.__mark === 'same-document').catch(() => false)
  const link = (text) => page.locator('#body a', { hasText: text })
  const bodyHas = (text) => page.evaluate((text) => (document.getElementById('body')?.innerHTML ?? '').includes(text), text)
  // The code runs without URL.
  const preloads = (path) => dataRequests.filter((u) => u.startsWith(base + path + '?') && u.endsWith('.p')).length

  // decided clicks a link without leaving the page: a listener of the window runs after the one
  // React has on its root, records whether the router took the click, and keeps the browser from
  // following the link.
  const decided = (text, init) =>
    page.evaluate(
      ({ text, init }) => {
        const a = [...document.querySelectorAll('#body a')].find((x) => x.textContent.includes(text))
        let prevented
        window.addEventListener('click', (e) => {
          prevented = e.defaultPrevented
          e.preventDefault()
        }, { once: true })
        a.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0, ...init }))
        return prevented
      },
      { text, init },
    )

  await fresh()
  log('the hydrated article holds the markup', await bodyHas('LINKS_END'))

  // A link to a page: the router shows it in the same document with one data request.
  await link('next article').click()
  await page.waitForURL('**/article/read')
  await page.waitForFunction(() => (document.getElementById('body')?.innerHTML ?? '').includes('BODY_END'))
  log('a link to a page navigates in the same document', await same(), 'data requests: ' + dataRequests.length)
  log('the page of the link shows', (await bodyHas('BODY_START')) && (await page.locator('h1').textContent()) === 'Article')

  // Back on the page whose data came with the page, with a marker in place of the article.
  await page.goBack()
  await page.waitForURL('**/article/links')
  await page.waitForTimeout(300)
  log('back on the first page shows its article again', (await bodyHas('LINKS_END')) && (await same()))

  // A hash of this page: the browser jumps, no data request.
  dataRequests.length = 0
  await link('to the end').click()
  await page.waitForTimeout(300)
  log('a hash of the page is left to the browser', page.url().endsWith('/article/links#end') && (await same()) && dataRequests.length === 0 && (await page.evaluate(() => window.scrollY > 0)), page.url())

  // A page with a hash, and the home page.
  await fresh()
  await link('cached intro').click()
  await page.waitForURL('**/article/cached#intro')
  await page.waitForFunction(() => (document.getElementById('body')?.innerHTML ?? '').includes('BODY_END'))
  log('a link to a page with a hash navigates in the same document', await same())
  await fresh()
  await link('home page').click()
  await page.waitForURL(base + '/')
  await page.waitForTimeout(200)
  log('a link to the home page navigates in the same document', (await same()) && (await page.getByText('North shop').count()) === 1)

  // A hover preloads the page once, after a moment.
  await fresh()
  await link('next article').hover()
  await page.waitForTimeout(400)
  const once = preloads('/article/read')
  await page.mouse.move(0, 0)
  await link('next article').locator('em').hover()
  await page.waitForTimeout(400)
  log('a hover preloads the page once', once === 1 && preloads('/article/read') === 1, dataRequests.join(' '))
  // A hover that leaves at once preloads nothing.
  await fresh()
  await link('cached intro').hover()
  await page.mouse.move(0, 0)
  await page.waitForTimeout(300)
  log('a hover that leaves at once preloads nothing', dataRequests.length === 0, dataRequests.join(' '))
  // A touch preloads at once: the leave right after it does not stop it.
  await fresh()
  await link('cached intro').evaluate((a) => {
    a.dispatchEvent(new Event('touchstart', { bubbles: true }))
    a.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }))
  })
  await page.waitForTimeout(300)
  log('a touch preloads at once', preloads('/article/cached') === 1, dataRequests.join(' '))
  // A focus preloads too.
  await fresh()
  await link('cached intro').focus()
  await page.waitForTimeout(300)
  log('a focus preloads the page', preloads('/article/cached') === 1, dataRequests.join(' '))

  // The decisions that stay with the browser, checked without leaving.
  await fresh()
  for (const [text, init, router] of [
    ['TanStack', {}, false],
    ['write', {}, false],
    ['example', {}, false],
    ['tifa path', {}, false],
    ['next article', { ctrlKey: true }, false],
    ['next article', { metaKey: true }, false],
    ['next article', { shiftKey: true }, false],
    ['next article', { altKey: true }, false],
    ['next article', { button: 1 }, false],
    ['next article', {}, true],
  ]) {
    const took = await decided(text, init)
    log('the router takes ' + text + ' ' + JSON.stringify(init) + ': ' + took, took === router)
  }

  // The page's own handler calls preventDefault: the router stays out. The hover before the
  // click may preload the page.
  await fresh()
  await link('own handler').click()
  await page.waitForTimeout(300)
  log(
    "the page's own click handler takes over",
    page.url().endsWith('/article/links') && (await same()) && dataRequests.every((u) => u.endsWith('.p')) && (await page.evaluate(() => window.ownClicks === 1)),
    page.url() + ' ' + dataRequests.join(' '),
  )

  // target=_blank: the browser opens a tab and this page stays.
  await fresh()
  const [tab] = await Promise.all([page.context().waitForEvent('page'), link('new tab').click()])
  await tab.waitForLoadState()
  log('target=_blank opens a new tab', tab.url().endsWith('/article/plain') && page.url().endsWith('/article/links') && (await same()))
  await tab.close()
  // Ctrl or Cmd: the browser opens a tab.
  const [tab2] = await Promise.all([page.context().waitForEvent('page'), link('next article').click({ modifiers: ['ControlOrMeta'] })])
  await tab2.waitForLoadState()
  log('a ctrl or cmd click opens a new tab', tab2.url().endsWith('/article/read') && page.url().endsWith('/article/links') && (await same()))
  await tab2.close()
  // download: the browser downloads the file.
  const [download] = await Promise.all([page.waitForEvent('download'), link('download notes').click()])
  log('a download link downloads', download.suggestedFilename() === 'notes.txt' && (await same()))

  // What Go answers without a page, and rel="external": whole page loads, no data request.
  for (const [text, path, shows] of [
    ['notes file', '/guide/notes.txt', 'NOTES_FILE'],
    ['missing asset', '/assets/missing.js', ''],
    ['ping', '/api/ping', 'pong'],
    ['nowhere', '/nowhere/at/all', ''],
    ['reload read', '/article/read', 'BODY_END'],
  ]) {
    await fresh()
    const navigated = page.waitForEvent('framenavigated')
    await link(text).click()
    await navigated
    await page.waitForLoadState()
    const text2 = await page.evaluate(() => document.body.innerText)
    log(path + ' (' + text + ') loads as a whole page', page.url().endsWith(path) && !(await same()) && dataRequests.length === 0 && text2.includes(shows), page.url() + ' ' + dataRequests.join(' '))
  }

  // The 404 pages the whole page loads land on say so, and the development build of TanStack Router
  // warns about their route errors.
  const unexpected = consoleMessages.filter(
    (m) => !m.includes('Failed to load resource') && !['/assets/missing.js', '/nowhere/at/all'].some((path) => m === 'warning: Warning: Error in route match: /_app/$' + path),
  )
  log('the console has no errors or warnings', unexpected.length === 0, JSON.stringify(unexpected))
  return out.join('\n')
}
