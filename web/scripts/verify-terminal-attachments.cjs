// Isolated browser checks: no live sessions or server uploads are changed.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright-core');
const assert = require('node:assert/strict');
const base = process.env.FASTREMOTE_TEST_URL || 'http://127.0.0.1:9091';

(async () => {
  const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || '/opt/google/chrome/chrome', args: ['--no-sandbox'] });
  try {
    const context = await browser.newContext({ viewport: { width: 320, height: 740 }, isMobile: true, hasTouch: true });
    await context.addInitScript(() => {
      localStorage.setItem('fastremote_token', 'isolated-attachment-test');
      window.clipboardMode = 'text';
      window.copiedText = '';
      Object.defineProperty(navigator, 'clipboard', { value: {
        read: async () => window.clipboardMode === 'image' ? [{ types: ['image/png'], getType: async () => new Blob(['image'], { type: 'image/png' }) }] : [{ types: ['text/plain'], getType: async () => new Blob(['clipboard text']) }],
        readText: async () => 'clipboard text',
        writeText: async text => { window.copiedText = text; },
      } });
    });
    const page = await context.newPage(), errors = [], input = [];
    page.on('pageerror', e => errors.push(e.message));
    const session = { id: 'isolated-session', title: 'Loyiha', cwd: '/home/test/project', status: 'running' };
    let uploads = 0, failUpload = false;
    const uploadedPath = "/private/uploads/photo '$(touch never-run).png";
    await page.route('**/api/terminals', route => route.fulfill({ json: { sessions: [session], activeId: session.id, projects: [{ name: 'Loyiha', path: session.cwd }] } }));
    await page.route('**/api/terminals/files?*', route => {
      uploads++;
      assert.equal(route.request().method(), 'POST');
      assert.equal(new URL(route.request().url()).searchParams.get('id'), session.id);
      assert.match(route.request().headers()['content-type'], /^multipart\/form-data; boundary=/);
      return failUpload ? route.fulfill({ status: 503, json: { error: 'Sinov: fayl yuklanmadi' } }) : route.fulfill({ json: { files: [{ name: 'photo.png', path: uploadedPath, size: 5, mime: 'image/png' }] } });
    });
    await page.routeWebSocket('**/ws?*', () => {});
    await page.routeWebSocket('**/ws/terminal?*', ws => {
      ws.send(Buffer.from('$ '));
      ws.send(JSON.stringify({ type: 'ready' }));
      ws.onMessage(raw => { const msg = JSON.parse(raw); if (msg.type === 'input') input.push(msg.data); });
    });
    await page.goto(base + '/terminal');
    await page.getByText('Ulangan', { exact: true }).waitFor();
    const attach = page.getByRole('button', { name: 'Rasm yoki fayl biriktirish' });
    for (const width of [320, 390, 768, 1440]) {
      await page.setViewportSize({ width, height: 740 });
      const layout = await page.evaluate(() => {
        const bars = ['.terminal-session-bar', '.terminal-toolbar'].map(s => document.querySelector(s).getBoundingClientRect());
        return { height: bars.reduce((n, r) => n + r.height, 0), overflow: document.documentElement.scrollWidth > innerWidth, toolsFit: [...document.querySelectorAll('.terminal-tools > button, .terminal-menu > summary')].every(el => el.getBoundingClientRect().right <= innerWidth) };
      });
      assert.deepEqual(layout, { height: 92, overflow: false, toolsFit: true }, `toolbar at ${width}px`);
    }
    await page.setViewportSize({ width: 320, height: 740 });
    await page.getByRole('button', { name: 'Yangi terminal' }).click();
    const colors = await page.getByRole('combobox').evaluate(el => ({ scheme: getComputedStyle(el).colorScheme, background: getComputedStyle(el.options[1]).backgroundColor }));
    assert.equal(colors.scheme, 'dark');
    assert.equal(colors.background, 'rgb(17, 27, 44)');
    await page.getByRole('button', { name: 'Bekor qilish', exact: true }).click();
    const pasteFile = async size => page.locator('.xterm-helper-textarea').evaluate((el, size) => {
      const clipboard = new DataTransfer();
      clipboard.items.add(new File([new Uint8Array(size)], 'pasted.png', { type: 'image/png' }));
      el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
    }, size);
    await pasteFile(5);
    await page.getByText(/1 ta fayl yuklandi, yo‘li terminalga joylandi/).waitFor();
    assert.equal(uploads, 1);
    assert.equal(input.join(''), "'/private/uploads/photo '\\''$(touch never-run).png' ");
    assert.ok(!input.join('').includes('\r') && !input.join('').includes('\n'), 'upload never submits shell input');
    await page.getByRole('button', { name: 'Xabarni yopish' }).click();
    const beforeText = input.length;
    await page.getByRole('button', { name: 'Joylash', exact: true }).click();
    await page.waitForTimeout(100);
    assert.equal(input.slice(beforeText).join(''), 'clipboard text');
    const beforeNativeText = input.length;
    await page.locator('.xterm-helper-textarea').evaluate(el => {
      const clipboard = new DataTransfer(); clipboard.setData('text/plain', 'native text\nsecond line');
      el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
    });
    await page.waitForTimeout(100);
    // xterm's normal paste handler preserves lines using the terminal CR convention.
    assert.equal(input.slice(beforeNativeText).join(''), 'native text\rsecond line');
    await page.evaluate(() => { window.clipboardMode = 'image'; });
    await page.getByRole('button', { name: 'Joylash', exact: true }).click();
    await page.getByText(/1 ta fayl yuklandi, yo‘li terminalga joylandi/).waitFor();
    assert.equal(uploads, 2);
    failUpload = true;
    const beforeFailure = input.length;
    await page.locator('input[type=file]').setInputFiles({ name: 'retry.png', mimeType: 'image/png', buffer: Buffer.from('image') });
    await page.getByText('Sinov: fayl yuklanmadi', { exact: true }).waitFor();
    assert.equal(input.length, beforeFailure);
    assert.equal(await attach.isEnabled(), true);
    assert.equal(await page.locator('input[type=file]').inputValue(), '');
    failUpload = false;
    await page.locator('input[type=file]').setInputFiles({ name: 'retry.png', mimeType: 'image/png', buffer: Buffer.from('image') });
    await page.getByText(/1 ta fayl yuklandi, yo‘li terminalga joylandi/).waitFor();
    assert.equal(uploads, 4, 'same file can be retried');
    await pasteFile(26 * 1024 * 1024);
    await page.getByText('Ko‘pi bilan 10 ta fayl: har biri 25 MB, jami 50 MB gacha.', { exact: true }).waitFor();
    assert.equal(uploads, 4, 'oversized files rejected before upload');
    await page.getByRole('button', { name: 'Xabarni yopish' }).click();
    await page.setViewportSize({ width: 320, height: 400 });
    await page.locator('.terminal-menu summary').click();
    assert.ok(await page.locator('.terminal-menu-content').evaluate(el => el.getBoundingClientRect().bottom <= innerHeight), 'menu fits a short mobile viewport');
    assert.deepEqual(errors, []);
    console.log('PASS image/file paste, clipboard button, native text paste, safe path quoting without Enter, upload failure and retry, size cap, dark project picker, 320–1440px two-row controls; no JS errors');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
