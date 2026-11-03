// Browser regression checks for the embedded panel. Uses synthetic data only.
// Requires Playwright; see README.md. No daemon, Discord, or Windows APIs are used.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { chromium } = require('playwright');

const root = path.resolve(__dirname, '..');
const output = path.resolve(process.env.UI_EVIDENCE_DIR || path.join(os.tmpdir(), 'yozora-ui-check'));
fs.mkdirSync(output, { recursive: true });
const nonce = 'synthetic-ui-test';
const html = fs.readFileSync(path.join(root, 'internal/server/ui.html'), 'utf8').replace('{{.Nonce}}', nonce);
// Use the production policy, including its nonce and restriction on image URLs.
const serverSource = fs.readFileSync(path.join(root, 'internal/server/server.go'), 'utf8');
const cspExpression = serverSource.match(/Set\("Content-Security-Policy", (.+)\)/)[1];
const csp = cspExpression.split('+nonce+').map(part => JSON.parse(part)).join(nonce);
const canonical = ['applemusic', 'spotify', 'spotifyapi', 'youtubemusic', 'browser', 'generic'];
let sources = { enabled: Object.fromEntries([...canonical, 'youtube', 'youtube_tabs'].map(key => [key, !['browser', 'generic', 'youtube', 'youtube_tabs'].includes(key)])), order: [...canonical] };
let state = { playing: true, activity_type: 'listening', title: 'Night Walk', artist: 'Mira', album: 'After Hours', service: 'Spotify', source: 'spotify', elapsed_sec: 82, duration_sec: 243, sends: 12, discord_connected: true, art_url: '/test-art.svg', small_image: '/test-badge.svg', published_activity: { buttons: [{ label: 'Listen on Spotify', url: 'https://open.spotify.com/track/synthetic' }, { label: 'Yozora', url: 'https://github.com/mariyaholic/yozora' }] } };
let stateHTTP = 200;
let sourceGetHTTP = 200;
let sourcePostHTTP = 200;
let autostartHTTP = 200;
let autostart = false;
let saveDelay = 0;
const posts = [];
const errors = [];
const violations = [];
const checks = [];
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const json = (body, code = 200) => { res.writeHead(code, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(body)); };
  if (url.pathname === '/') {
    res.writeHead(200, { 'Content-Type': 'text/html', 'Content-Security-Policy': csp, 'Referrer-Policy': 'no-referrer' });
    res.end(html);
  } else if (url.pathname.startsWith('/test-')) {
    res.writeHead(200, { 'Content-Type': 'image/svg+xml' });
    res.end('<svg xmlns="http://www.w3.org/2000/svg" width="160" height="160" viewBox="0 0 160 160"><rect width="160" height="160" fill="#253348"/><circle cx="111" cy="48" r="26" fill="#e0b758"/><path d="M0 135 54 65 114 160H0M72 160l49-78 39 60v18" fill="#53647d"/></svg>');
  } else if (url.pathname === '/api/state') json(state, stateHTTP);
  else if (url.pathname === '/api/sources') {
    if (req.method === 'POST') {
      let body = '';
      for await (const chunk of req) body += chunk;
      const posted = JSON.parse(body);
      posts.push(posted);
      if (saveDelay) await new Promise(resolve => setTimeout(resolve, saveDelay));
      if (sourcePostHTTP === 200) sources = posted;
      json(sources, sourcePostHTTP);
    } else json(sources, sourceGetHTTP);
  } else if (url.pathname === '/api/autostart') {
    if (req.method === 'POST') {
      let body = '';
      for await (const chunk of req) body += chunk;
      if (autostartHTTP === 200) autostart = JSON.parse(body).enabled;
    }
    json({ enabled: autostart }, autostartHTTP);
  } else { res.writeHead(404); res.end(); }
});

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const origin = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined, headless: true, args: ['--no-sandbox'] });
  try {
    const context = await browser.newContext({ viewport: { width: 560, height: 760 }, reducedMotion: 'reduce' });
    // Never allow a synthetic fixture to contact a real service.
    await context.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort());
    await context.addInitScript(() => {
      window.cspViolations = [];
      document.addEventListener('securitypolicyviolation', event => window.cspViolations.push(event.violatedDirective));
    });
    const page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    const waitText = (selector, text) => page.waitForFunction(([selector, text]) => document.querySelector(selector).textContent.includes(text), [selector, text]);
    const refresh = () => page.evaluate(async () => {
      while (polling) await new Promise(resolve => setTimeout(resolve, 10));
      await tick();
    });
    const shot = async name => {
      await page.screenshot({ path: path.join(output, `${name}.png`), fullPage: true });
      await page.screenshot({ path: path.join(output, `${name}-viewport.png`) });
    };
    const check = name => { checks.push(name); console.log(`PASS ${name}`); };
    await page.goto(`${origin}/?t=synthetic`);
    await waitText('#source-status', 'All changes saved');
    await waitText('#connection', 'Discord connected');
    assert.equal(await page.locator('#source-save').isDisabled(), true);
    await shot('playing-560');
    const saveBounds = await page.locator('#source-save').boundingBox();
    assert.ok(saveBounds.y + saveBounds.height <= 760, 'Apply must fit in the native viewport with a typical playing track');
    check('native-size playing layout and saved state');

    const firstLink = page.locator('#buttons a').first();
    await firstLink.focus();
    await page.evaluate(() => {
      window.statusMutations = 0;
      window.statusObserver = new MutationObserver(records => { window.statusMutations += records.length; });
      for (const id of ['connection', 'preview-note']) window.statusObserver.observe(document.getElementById(id), { childList: true });
    });
    await refresh();
    assert.equal(await page.evaluate(() => window.statusMutations), 0, 'unchanged live regions must stay quiet during polling');
    assert.equal(await firstLink.evaluate(node => node === document.activeElement), true);
    state = { ...state, playing: false };
    await refresh();
    assert.equal(await page.locator('#progress').getAttribute('aria-valuenow'), '34');
    assert.equal(await page.locator('#playback-state').textContent(), 'Paused');
    await shot('paused-560');
    check('polling preserves link focus and paused progress');

    await page.locator('label[for=source-youtube]').click({ position: { x: 3, y: 3 } });
    assert.equal(await page.locator('#browser-dependency').isVisible(), true);
    assert.equal(await page.locator('#source-browser').isChecked(), false, 'dependency hints must not silently change saved preferences');
    assert.equal(await page.locator('#source-save').isEnabled(), true);
    await page.locator('#source-reset').click();
    assert.equal(await page.locator('#source-youtube').isChecked(), false);
    assert.equal(await page.locator('#source-save').isDisabled(), true);
    check('unsaved changes, dependency hint, and discard');

    await page.locator('#priority-settings summary').click();
    await page.getByRole('button', { name: 'Move Spotify (desktop) up', exact: true }).focus();
    await page.keyboard.press('Enter');
    assert.equal(await page.locator('#source-order li').first().getAttribute('data-source'), 'spotify');
    assert.equal(await page.evaluate(() => document.activeElement.dataset.source), 'spotify');
    await page.keyboard.press('Tab');
    assert.equal(await page.evaluate(() => document.activeElement.getAttribute('aria-label')), 'Move Spotify (desktop) down');
    await page.keyboard.press('Enter');
    assert.equal(await page.evaluate(() => document.activeElement.getAttribute('aria-label')), 'Move Spotify (desktop) down');
    assert.equal(await page.locator('#source-save').isDisabled(), true, 'returning to the saved order should clear dirty state');
    assert.equal(await page.locator('#source-status').textContent(), 'All changes saved.');
    await page.keyboard.press('Enter');
    assert.equal(await page.locator('#source-order li').nth(2).getAttribute('data-source'), 'spotify');
    check('keyboard reorder, focus retention, and restoring saved order');

    sourcePostHTTP = 500;
    await page.locator('#source-save').click();
    await waitText('#source-status', 'Save failed');
    assert.equal(await page.locator('#source-save').isEnabled(), true);
    assert.equal(await page.locator('#source-order li').nth(2).getAttribute('data-source'), 'spotify');
    await shot('save-error-560');
    sourcePostHTTP = 200;
    saveDelay = 200;
    await page.locator('#source-save').click();
    await waitText('#source-status', 'Saving changes');
    const beforeSavingOrder = await page.locator('#source-order li').evaluateAll(rows => rows.map(row => row.dataset.source));
    await page.evaluate(() => moveSource(0, 1));
    assert.deepEqual(await page.locator('#source-order li').evaluateAll(rows => rows.map(row => row.dataset.source)), beforeSavingOrder);
    await waitText('#source-status', 'Saved. Discord');
    assert.deepEqual(posts.at(-1).order, beforeSavingOrder);
    assert.equal(Object.keys(posts.at(-1).enabled).length, 8);
    assert.equal(await page.locator('#source-save').isDisabled(), true);
    saveDelay = 0;
    check('failed-save retry, exact payload, and reorder lock during save');

    await page.locator('#source-order li').first().dragTo(page.locator('#source-order li').last());
    assert.equal(await page.locator('#source-order li').last().getAttribute('data-source'), beforeSavingOrder[0]);
    assert.equal(await page.locator('#source-save').isEnabled(), true);
    await shot('priority-560');
    await page.locator('#source-reset').click();
    await page.locator('#priority-settings summary').click();
    check('drag ordering and discard after save');

    await page.locator('#autostart').check();
    await waitText('#autostart-status', 'will start');
    autostartHTTP = 500;
    await page.locator('#autostart').uncheck();
    await waitText('#autostart-status', 'Could not change');
    assert.equal(await page.locator('#autostart').isChecked(), true);
    autostartHTTP = 200;
    check('startup save and failed-save rollback');

    state = { ...state, playing: true, title: 'A very long synthetic track title — ' + 'unbroken'.repeat(15), artist: 'Synthetic artist name '.repeat(12), published_activity: { buttons: [{ label: 'Unsafe', url: 'javascript:alert(1)' }, { label: 'Credentials', url: 'https://name:password@example.test' }, { label: 'Yozora', url: 'https://github.com/mariyaholic/yozora' }] } };
    await refresh();
    assert.equal(await page.locator('#buttons a').count(), 1);
    for (const width of [320, 390, 560, 960]) {
      await page.setViewportSize({ width, height: 760 });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `${width}px must not overflow horizontally`);
      assert.equal(await page.locator('#det').getAttribute('title'), state.title);
      await page.evaluate(() => scrollTo(0, 0));
      await shot(`long-title-${width}`);
    }
    check('320/390/560/960px layouts, full title access, and unsafe-link rejection');

    await page.setViewportSize({ width: 560, height: 760 });
    state = { playing: false, title: '', discord_connected: true, sends: 12 };
    await refresh();
    assert.equal(await page.locator('#det').textContent(), 'Ready when you are');
    assert.equal(await page.locator('#art').isHidden(), true);
    assert.equal(await page.locator('#timeline').isHidden(), true);
    assert.equal(await page.locator('#buttons a').count(), 0);
    await shot('idle-560');
    state.discord_connected = false;
    await refresh();
    assert.equal(await page.locator('#connection').textContent(), 'Discord offline');
    await shot('discord-offline-560');
    stateHTTP = 503;
    await refresh();
    assert.equal(await page.locator('#connection').textContent(), 'Panel offline');
    assert.equal(await page.locator('#preview').getAttribute('data-stale'), 'true');
    await shot('panel-offline-560');
    stateHTTP = 200;
    await refresh();
    assert.equal(await page.locator('#preview').getAttribute('data-stale'), 'false');
    check('empty, Discord-offline, panel-offline, and recovered states');

    state = { ...state, title: 'Artwork unavailable', art_url: '/missing-art.png' };
    await refresh();
    await page.waitForFunction(() => document.getElementById('art').hidden);
    assert.equal(await page.locator('.art-placeholder').isVisible(), true);
    violations.push(...await page.evaluate(() => window.cspViolations));
    sourceGetHTTP = 500;
    stateHTTP = 503;
    await page.reload();
    await waitText('#connection', 'Panel offline');
    assert.equal(await page.locator('#det').textContent(), 'Waiting for Yozora');
    await shot('initial-offline-560');
    stateHTTP = 200;
    await refresh();
    await waitText('#source-status', 'Could not load');
    assert.equal(await page.locator('#source-applemusic').isDisabled(), true);
    sourceGetHTTP = 200;
    await page.locator('#source-retry').click();
    await waitText('#source-status', 'All changes saved');
    assert.equal(await page.locator('#source-applemusic').isEnabled(), true);
    assert.equal(await page.evaluate(() => document.activeElement.id), 'source-applemusic');
    check('broken-art fallback and initial settings-load retry');

    violations.push(...await page.evaluate(() => window.cspViolations));
    assert.deepEqual(errors, [], 'no browser runtime exceptions');
    assert.deepEqual(violations, [], 'no production CSP violations');
    check('production CSP and browser console');
    fs.writeFileSync(path.join(output, 'results.json'), JSON.stringify({ checks, errors, violations, screenshots: output }, null, 2));
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => { console.error(error); process.exitCode = 1; server.close(); });
