// Integration check against a local staging agent. Creates and deletes only its
// own isolated test catalogue. Requires Playwright and access to the agent key.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright-core');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { execFileSync } = require('node:child_process');
const base = process.env.FASTREMOTE_TEST_URL || 'http://127.0.0.1:9091';
const owner = `terminal-check-${Date.now()}`;
const secret = fs.readFileSync(path.resolve(__dirname, '../../agent/.jwt_secret'), 'utf8').trim();
const enc = v => Buffer.from(JSON.stringify(v)).toString('base64url');
const payload = enc({alg:'HS256',typ:'JWT'})+'.'+enc({username:owner,role:'user',iss:'fastremote-agent',exp:Math.floor(Date.now()/1000)+1800});
const token = payload+'.'+crypto.createHmac('sha256',secret).update(payload).digest('base64url');
const api = async (method='GET',body) => {
 const res=await fetch(base+'/api/terminals',{method,headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},...(body?{body:JSON.stringify(body)}:{})});
 const data=await res.json(); assert.ok(res.ok,JSON.stringify(data)); return data;
};
const errors=[];
(async()=>{
 const browser=await chromium.launch({executablePath:process.env.CHROME_PATH||'/opt/google/chrome/chrome',headless:true,args:['--no-sandbox']});
 const context=await browser.newContext({viewport:{width:1440,height:900}});
 const authenticate=async ctx=>ctx.addInitScript(({token,owner})=>{localStorage.setItem('fastremote_token',token);localStorage.setItem('fastremote_username',owner)},{token,owner});
 await authenticate(context);const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
 const content=p=>p.locator('.xterm-rows').innerText();
 const waitText=async(p,needle)=>{await p.waitForFunction(n=>document.querySelector('.xterm-rows')?.textContent.includes(n),needle,{timeout:15000})};
 const command=async(p,text)=>{await p.locator('.xterm-helper-textarea').focus();await p.keyboard.insertText(`eval "$(printf %s ${Buffer.from(text).toString('base64')} | base64 -d)"`);await p.keyboard.press('Enter')};
 try{
  await page.goto(base+'/terminal');await page.getByRole('heading',{name:'Ishingiz shu yerda davom etadi'}).waitFor();
  await page.getByRole('button',{name:'Yangi terminal'}).click();await page.getByRole('button',{name:'Ochish',exact:true}).click();await page.getByText('Ulangan',{exact:true}).waitFor();await waitText(page,'$');
  await command(page,"export FR_CHECK=retained_state; cd /tmp; printf '\\nSESSION_ONE_世界\\n'");await waitText(page,'SESSION_ONE_世界');
  const first=(await api()).sessions[0];
  await page.locator('.terminal-menu summary').click();await page.getByRole('button',{name:'Terminal nomini o‘zgartirish'}).click();await page.getByRole('textbox',{name:'Terminal nomi'}).fill('Build • 世界');await page.getByRole('button',{name:'Saqlash',exact:true}).click();
  await page.getByRole('tab',{name:'Build • 世界'}).waitFor();
  await page.getByRole('button',{name:'Yangi terminal'}).click();await page.getByRole('button',{name:'Ochish',exact:true}).click();await page.getByRole('tab',{selected:true}).filter({hasText:'Terminal 2'}).waitFor();await page.getByText('Ulangan',{exact:true}).waitFor();
  await command(page,"printf '\\nSESSION_TWO_ONLY\\n'");await waitText(page,'SESSION_TWO_ONLY');assert.ok(!(await content(page)).includes('SESSION_ONE_世界'));
  await page.getByRole('tab',{name:'Build • 世界'}).click();await waitText(page,'SESSION_ONE_世界');assert.ok(!(await content(page)).includes('SESSION_TWO_ONLY'));
  // A background job and shell environment survive completely closing a browser.
  await command(page,"sleep 1; printf '\\nOFFLINE_JOB_COMPLETED\\n'");await page.close();
  const mobile=await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true,deviceScaleFactor:2});await authenticate(mobile);
  const phone=await mobile.newPage();phone.on('pageerror',e=>errors.push(e.message));await phone.goto(base+'/terminal');await waitText(phone,'OFFLINE_JOB_COMPLETED');
  await command(phone,'echo "$FR_CHECK"; pwd');await waitText(phone,'retained_state');await waitText(phone,'/tmp');
  await phone.reload();await waitText(phone,'retained_state');
  // Keep the same shell PID through an actual HTTP gateway service restart.
  const tmuxSocket='/run/user/1000/fastremote/tmux.sock';
  const panePID=()=>execFileSync('tmux',['-S',tmuxSocket,'display-message','-p','-t','fr-'+first.id,'#{pane_pid}'],{encoding:'utf8'}).trim();
  if(process.env.TEST_SERVICE_RESTART==='1'){
   const before=panePID();await command(phone,"sleep 2; printf '\\nGATEWAY_RESTART_SURVIVED\\n'");
   execFileSync('systemctl',['--user','restart','fastremote-web.service']);await waitText(phone,'GATEWAY_RESTART_SURVIVED');assert.equal(panePID(),before);
   console.log('PASS actual gateway restart, same shell PID and offline command completion');
  }
  // Second independent browser discovers identical terminals from the server.
  const other=await browser.newContext({viewport:{width:1280,height:800}});await authenticate(other);const second=await other.newPage();await second.goto(base+'/terminal');await waitText(second,'retained_state');
  assert.equal(await second.getByRole('tab').count(),2);
  await phone.screenshot({path:'/tmp/fastremote-check/terminal-mobile.png'});await second.screenshot({path:'/tmp/fastremote-check/terminal-desktop.png'});
  // Restore deep scrollback, not just the visible screen, and a full-screen TUI.
  await command(second, "for i in $(seq 1 250); do printf 'HISTORY_%04d\\n' \"$i\"; done");
  await waitText(second,'HISTORY_0250');await second.reload();await waitText(second,'HISTORY_0250');
  await second.locator('.terminal-menu summary').click();const downloadPromise=second.waitForEvent('download');await second.getByRole('button',{name:'↓ Tarixni yuklash'}).click();const download=await downloadPromise;
  const text=fs.readFileSync(await download.path(),'utf8');assert.ok(text.includes('HISTORY_0001'),'deep scrollback survived refresh');assert.ok(text.includes('HISTORY_0250'));
  await command(second, "printf '\\033[?1049h\\033[2J\\033[HAPP_ACTIVE'; sleep 4; printf '\\033[?1049l'");
  await waitText(second,'APP_ACTIVE');await second.reload();await waitText(second,'APP_ACTIVE');await waitText(second,'HISTORY_0250');
  console.log('PASS 250-line scrollback recovery and running alternate-screen application');
  await second.close();
  const overflow=[];
  for(const width of [320,360,390,768,1024]){
   await phone.setViewportSize({width,height:844});
   for(const route of ['/','/terminal','/files','/settings','/login']){
    await phone.goto(base+route);await phone.waitForTimeout(450);
    const bad=await phone.evaluate(()=>Array.from(document.querySelectorAll('body *')).filter(el=>{const s=getComputedStyle(el),r=el.getBoundingClientRect();return s.position!=='absolute'&&s.position!=='fixed'&&s.display!=='none'&&r.width>0&&r.left>=0&&r.right>innerWidth+2&&!el.closest('.xterm,.rd-terminal-tabs,.terminal-keyboard,.terminal-tools')}).map(el=>({tag:el.tagName,cls:el.className,right:el.getBoundingClientRect().right})).slice(0,8));
    if(bad.length)overflow.push({width,route,bad});
   }
  }
  assert.deepEqual(overflow,[],JSON.stringify(overflow));
  await phone.setViewportSize({width:390,height:400});await phone.goto(base+'/terminal');await phone.getByText('Ulangan',{exact:true}).waitFor();
  assert.ok(await phone.locator('.xterm').evaluate(el=>el.getBoundingClientRect().height)>90,'keyboard viewport leaves usable terminal');
  await phone.getByRole('button',{name:'Menyuni ochish'}).click();await phone.locator('#nav-settings').click();assert.equal(await phone.locator('.rd-sidebar').count(),0);
  assert.deepEqual(errors,[]);
  console.log('PASS session isolation, Unicode, rename, cross-browser restore, offline work, refresh, 25 responsive page/viewport checks, mobile menu, keyboard height; no JS errors');
 }finally{
  try { const list=await api();for(const s of list.sessions)await api('DELETE',{id:s.id}); } finally { await browser.close(); }
 }
})().catch(e=>{console.error(e);process.exitCode=1});
