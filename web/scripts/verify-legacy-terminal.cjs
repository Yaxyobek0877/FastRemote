// A legacy PTY redraws only when its size changes. Reconnecting at the same
// size used to show "connected" with an empty terminal.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright-core');
const assert = require('node:assert/strict');
const base = process.env.FASTREMOTE_TEST_URL || 'http://127.0.0.1:9091';
(async () => {
  const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || '/opt/google/chrome/chrome', args: ['--no-sandbox'] });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    await context.addInitScript(() => localStorage.setItem('fastremote_token', 'isolated-browser-test'));
    const page = await context.newPage();
    await page.route('**/api/terminals', route => route.fulfill({ json: { sessions: [{ id: 'legacy', title: 'Avvalgi terminal', status: 'legacy' }], activeId: 'legacy' } }));
    await page.routeWebSocket('**/ws?*', () => {});
    let cols, rows, connections = 0, inputs = 0, permitOutput = false;
    await page.routeWebSocket('**/ws/legacy-terminal?*', ws => {
      connections++;
      ws.onMessage(raw => {
        const { type, payload } = JSON.parse(raw);
        if (type === 'shell_input') inputs++;
        if (type !== 'shell_resize') return;
        const changed = cols !== payload.cols || rows !== payload.rows;
        cols = payload.cols; rows = payload.rows;
        if (changed && permitOutput) ws.send(JSON.stringify({ type: 'shell_output', payload: { data: '\x1b[2J\x1b[HRESTORED_LEGACY_SESSION\r\n$ ' } }));
      });
    });
    const visible = () => page.waitForFunction(() => document.querySelector('.xterm-rows')?.textContent.includes('RESTORED_LEGACY_SESSION'));
    await page.goto(base + '/terminal');
    await page.waitForTimeout(500);
    assert.equal(await page.locator('.terminal-connection').innerText(), 'Ekran tiklanmoqda…', 'socket open alone must not report a restored screen');
    // A silent running program must remain interruptible even without output.
    await page.getByRole('button', { name: 'Terminal tugmalarini ko‘rsatish' }).click();
    await page.getByRole('button', { name: 'Ctrl C', exact: true }).click();
    await page.waitForTimeout(100);
    assert.equal(inputs, 1, 'keyboard remains available for a quiet legacy job');
    inputs = 0;
    permitOutput = true;
    await page.getByRole('button', { name: 'Qayta ulanish', exact: true }).click();
    await visible();
    await page.getByText('Ulangan', { exact: true }).waitFor();
    await page.waitForTimeout(300);
    for (let i = 0; i < 3; i++) {
      await page.reload();
      await visible();
      await page.getByText('Ulangan', { exact: true }).waitFor();
      await page.waitForTimeout(300);
    }
    assert.equal(inputs, 0, 'restoring the screen must not inject program input');
    assert.equal(connections, 5);
    console.log('PASS legacy readiness, manual reconnect, 3 same-size reloads, no injected keystrokes');
  } finally { await browser.close(); }
})().catch(err => { console.error(err); process.exitCode = 1; });
