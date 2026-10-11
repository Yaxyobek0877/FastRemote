// Isolated browser recovery checks; does not restart the server or live sessions.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright-core');
const assert = require('node:assert/strict');
const base = process.env.FASTREMOTE_TEST_URL || 'http://127.0.0.1:9091';
(async () => {
  const browser = await chromium.launch({executablePath:process.env.CHROME_PATH || '/opt/google/chrome/chrome', args:['--no-sandbox']});
  try {
    const context = await browser.newContext({viewport:{width:320,height:740}});
    await context.addInitScript(() => {
      localStorage.setItem('fastremote_token','isolated-recovery-test');
      window.copiedText = '';
      Object.defineProperty(navigator, 'clipboard', {value:{writeText:async text => {window.copiedText = text;}}});
    });
    const page = await context.newPage(), errors = [];
    page.on('pageerror', e => errors.push(e.message));
    let session = {id:'saved-session', title:'Saqlangan loyiha', status:'stopped', cwd:'/home/test/project'};
    let restartCalls = 0, sockets = 0, inputs = 0;
    await page.route('**/api/terminals', route => route.fulfill({json:{sessions:[session],activeId:session.id,projects:[]}}));
    await page.route('**/api/terminals/restart', async route => {
      restartCalls++;
      assert.deepEqual(route.request().postDataJSON(), {id:session.id});
      session = {...session,status:'running',recoveredAt:'2026-10-11T20:01:02Z',recoveryNote:'Terminal qayta ochildi. Avvalgi dastur ishlamayapti; saqlangan tarix ko‘rsatilgan.',resumeCommand:'codex resume 11111111-1111-4111-8111-111111111111'};
      await route.fulfill({json:session});
    });
    await page.routeWebSocket('**/ws?*', () => {});
    await page.routeWebSocket('**/ws/terminal?*', ws => {
      sockets++;
      ws.send(Buffer.from('SAVED_HISTORY_MARKER\r\n$ '));
      const stopped = ['stopped', 'exited'].includes(session.status);
      ws.send(JSON.stringify({type:stopped ? 'stopped' : 'ready'}));
      ws.onMessage(raw => { if (JSON.parse(raw).type === 'input') inputs++; });
      if (stopped) ws.close({code:4004, reason:'Session stopped'});
    });
    await page.goto(base + '/terminal');
    await page.getByText('Saqlangan tarix',{exact:true}).waitFor();
    const restart = page.getByRole('button',{name:'↻ Qayta ochish',exact:true});
    await restart.waitFor();
    assert.equal(await page.getByRole('button',{name:'Qayta ulanish',exact:true}).count(),0);
    assert.ok((await page.locator('.xterm-rows').innerText()).includes('SAVED_HISTORY_MARKER'));
    await restart.click();
    await page.getByText('Ulangan',{exact:true}).waitFor();
    await page.locator('.terminal-recovery-notice').waitFor();
    assert.equal(restartCalls,1);
    assert.equal(sockets,2);
    const layout = await page.evaluate(() => {
      const tabs=document.querySelector('.terminal-session-bar').getBoundingClientRect();
      const tools=document.querySelector('.terminal-toolbar').getBoundingClientRect();
      const notice=document.querySelector('.terminal-recovery-notice');
      return {height:tabs.height+tools.height, absolute:getComputedStyle(notice).position,overflow:document.documentElement.scrollWidth>innerWidth, buttonsFit:[...document.querySelectorAll('.terminal-tools > button, .terminal-menu > summary')].every(el=>el.getBoundingClientRect().right<=innerWidth)};
    });
    assert.deepEqual(layout,{height:92,absolute:'absolute',overflow:false,buttonsFit:true});
    await page.getByRole('button',{name:'Xabarni yopish'}).click();
    await page.reload();
    await page.getByText('Ulangan',{exact:true}).waitFor();
    assert.equal(await page.locator('.terminal-recovery-notice').count(),0);
    await page.locator('.terminal-menu summary').click();
    await page.getByRole('button',{name:'Tiklanish haqida',exact:true}).click();
    await page.locator('.terminal-recovery-notice').waitFor();
    await page.getByRole('button',{name:'Xabarni yopish'}).click();
    await page.locator('.terminal-menu summary').click();
    await page.getByRole('button',{name:'Codex davom ettirish buyrug‘i',exact:true}).click();
    assert.equal(await page.evaluate(() => window.copiedText),session.resumeCommand);
    assert.equal(inputs,0,'restoration and resume-copy must not inject terminal input');
    // A tmux dead pane can keep the existing socket open after `exit`.
    // The next catalogue poll must replace the stale "connected" view.
    await page.evaluate(() => { window.sameDocument = 'no reload'; });
    const socketsBeforeExit = sockets;
    session = {...session, status:'exited'};
    await page.getByText('Saqlangan tarix',{exact:true}).waitFor({timeout:8000});
    await restart.waitFor();
    assert.equal(await page.getByText('Ulangan',{exact:true}).count(),0);
    assert.equal(await page.evaluate(() => window.sameDocument),'no reload');
    assert.equal(sockets,socketsBeforeExit + 1,'exit discovered by polling remounts the terminal once');
    assert.ok((await page.locator('.xterm-rows').innerText()).includes('SAVED_HISTORY_MARKER'));
    assert.deepEqual(errors,[]);
    console.log('PASS stopped history, manual reopen, recovery reconnect, dismiss persistence and rediscovery, copy-only resume, live-to-exited polling without reload, 320px two-row toolbar, no JS errors');
  } finally { await browser.close(); }
})().catch(e=>{console.error(e);process.exitCode=1});
