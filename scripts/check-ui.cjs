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
const template = fs.readFileSync(path.join(root, 'internal/server/ui.html'), 'utf8').replace('{{.Nonce}}', nonce).replace('{{.Theme}}', 'yozora').replace('{{.Motion}}', 'system');
// Like the server: the saved language, with "system" drawn first in the browser's Accept-Language.
const html = accept => template.replace('{{.Lang}}', language !== 'system' ? language : /^ja(-|$)/i.test(accept || '') ? 'ja' : 'en').replace('{{.Language}}', language).replace('{{.Version}}', version);
// Use the production policy, including its nonce and restriction on image URLs.
const serverSource = fs.readFileSync(path.join(root, 'internal/server/server.go'), 'utf8');
const cspExpression = serverSource.match(/Set\("Content-Security-Policy", (.+)\)/)[1];
const csp = cspExpression.split('+nonce+').map(part => JSON.parse(part)).join(nonce);
const canonical = ['applemusic', 'spotify', 'spotifyapi', 'youtubemusic', 'browser', 'generic'];
let sources = { enabled: Object.fromEntries([...canonical, 'youtube', 'youtube_tabs', 'twitch'].map(key => [key, !['browser', 'generic', 'youtube', 'youtube_tabs', 'twitch'].includes(key)])), order: [...canonical] };
let state = { playing: true, activity_type: 'listening', title: 'Night Walk', artist: 'Mira', album: 'After Hours', service: 'Spotify', source: 'spotify', elapsed_sec: 82, duration_sec: 243, sends: 12, discord_connected: true, art_url: '/test-art.svg', small_image: '/test-badge.svg', published_activity: { buttons: [{ label: 'Listen on Spotify', url: 'https://open.spotify.com/track/synthetic' }, { label: 'Yozora', url: 'https://github.com/mariyaholic/yozora' }] } };
let stateHTTP = 200;
let sourceGetHTTP = 200;
let sourcePostHTTP = 200;
let autostartHTTP = 200;
let autostart = false;
let theme = 'yozora';
let motion = 'system';
let language = 'system';
let version = 'dev';
let themeHTTP = 200;
const themePosts = [];
let saveDelay = 0;
let setup = { onboarded: true, client_id: '1556599139436077056', default_client_id: true };
const setupPosts = [];
let testCardHTTP = 202;
const testCardPosts = [];
let spotifyHTTP = 200;
let pauseHTTP = 200;
const pausePosts = [];
let recentHTTP = 200;
let recent = [];
const recentGets = [];
// A sample of what the daemon sends: no track text, no IDs, no user paths.
const diagnosticsReport = 'Yozora diagnostics\nVersion: 0.1.42\nWindows: 10.0.22631\nSource: spotify\nArtwork route: deezer\nDiscord: connected\nLast error: none\nEnabled sources: applemusic, spotify, spotifyapi\nTheme: yozora\n';
const noUpdate = { version:'0.1.42', latest:'', available:false, url:'', verification:'checksum', check_enabled:true, install:{ state:'idle' } };
let updateState = { ...noUpdate };
let updateInstallHTTP = 202;
const updatePosts = [];
const spotifyPosts = [];
const posts = [];
// Discord card form and hidden rules. The preview mimics the server's template engine for the plain placeholders only.
const cardDefaults = { paused_mode: 'show', type: 'listening', status_display: 'app', buttons_enabled: true, button_label: 'Listen along', spotify_search_fallback: true, details: '{{.Title}}', state: '{{.Artist}}', large_text: '{{.Album}}', small_text: '{{.Player}}', paused_details: '\u23f8 {{.Title}}' };
let card = { ...cardDefaults };
let hiddenRules = [];
let cardGetHTTP = 200;
const cardPosts = [];
const hiddenPosts = [];
const previewPosts = [];
const foldRule = value => (value || '').trim().toLowerCase();
const ruleHides = (rule, track) => (rule.artist || rule.title) && (!rule.artist || foldRule(rule.artist) === foldRule(track.artist)) && (!rule.title || foldRule(rule.title) === foldRule(track.title));
const renderCard = (tpl, track) => tpl.replace(/\{\{\.(\w+)\}\}/g, (_, name) => ({ Title: track.title, Artist: track.artist, Album: track.album, Player: track.service }[name] ?? ''));
const errors = [];
const violations = [];
const checks = [];
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const json = (body, code = 200) => { res.writeHead(code, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(body)); };
  if (url.pathname === '/') {
    res.writeHead(200, { 'Content-Type': 'text/html', 'Content-Security-Policy': csp, 'Referrer-Policy': 'no-referrer' });
    res.end(html(req.headers['accept-language']));
  } else if (/^\/fonts\/[a-z0-9-]+\.woff2$/.test(url.pathname) && fs.existsSync(path.join(root, 'internal/server', url.pathname))) {
    res.writeHead(200, { 'Content-Type': 'font/woff2' });
    res.end(fs.readFileSync(path.join(root, 'internal/server', url.pathname)));
  } else if (url.pathname.startsWith('/test-')) {
    res.writeHead(200, { 'Content-Type': 'image/svg+xml' });
    res.end('<svg xmlns="http://www.w3.org/2000/svg" width="160" height="160" viewBox="0 0 160 160"><rect width="160" height="160" fill="#253348"/><circle cx="111" cy="48" r="26" fill="#e0b758"/><path d="M0 135 54 65 114 160H0M72 160l49-78 39 60v18" fill="#53647d"/></svg>');
  } else if (url.pathname === '/api/state') json({ ...state, hidden: Boolean(state.title) && hiddenRules.some(rule => ruleHides(rule, state)) || undefined }, stateHTTP);
  else if (url.pathname === '/api/card') {
    if (req.method === 'POST') {
      let body = '';
      for await (const chunk of req) body += chunk;
      cardPosts.push(JSON.parse(body));
      if (saveDelay) await new Promise(resolve => setTimeout(resolve, saveDelay));
      if (cardGetHTTP === 200) card = cardPosts.at(-1);
    }
    json({ card, defaults: cardDefaults, hidden: hiddenRules }, cardGetHTTP);
  } else if (url.pathname === '/api/card/preview' && req.method === 'POST') {
    let body = '';
    for await (const chunk of req) body += chunk;
    const posted = JSON.parse(body);
    previewPosts.push(posted);
    const broken = ['details', 'state', 'large_text', 'small_text', 'paused_details'].find(key => (posted[key].match(/\{\{/g) || []).length !== (posted[key].match(/\}\}/g) || []).length);
    if (broken) { res.writeHead(400, { 'Content-Type': 'text/plain' }); res.end(`${broken} is not a valid template: unclosed action`); return; }
    const sample = !state.title;
    const track = sample ? { title: 'Night Walk', artist: 'Mira', album: 'After Hours', service: 'Spotify', playing: true } : state;
    json({ details: renderCard(track.playing ? posted.details : posted.paused_details, track), state: renderCard(posted.state, track), large_text: renderCard(posted.large_text, track), small_text: renderCard(posted.small_text, track), playing: track.playing, sample, hidden: !sample && hiddenRules.some(rule => ruleHides(rule, track)) });
  } else if (url.pathname === '/api/hidden') {
    if (req.method === 'POST') {
      let body = '';
      for await (const chunk of req) body += chunk;
      const posted = JSON.parse(body);
      hiddenPosts.push(posted);
      if (posted.add && !hiddenRules.some(rule => foldRule(rule.artist) === foldRule(posted.add.artist) && foldRule(rule.title) === foldRule(posted.add.title))) hiddenRules = [...hiddenRules, { artist: posted.add.artist || '', title: posted.add.title || '' }];
      if (posted.remove) hiddenRules = hiddenRules.filter(rule => !(foldRule(rule.artist) === foldRule(posted.remove.artist) && foldRule(rule.title) === foldRule(posted.remove.title)));
    }
    json({ hidden: hiddenRules });
  } else if (url.pathname === '/api/sources') {
    if (req.method === 'POST') {
      let body = '';
      for await (const chunk of req) body += chunk;
      const posted = JSON.parse(body);
      posts.push(posted);
      if (saveDelay) await new Promise(resolve => setTimeout(resolve, saveDelay));
      if (sourcePostHTTP === 200) sources = posted;
      json(sources, sourcePostHTTP);
    } else json(sources, sourceGetHTTP);
  } else if (url.pathname === '/api/appearance') {
    if (req.method === 'POST') {
      let body = '';
      for await (const chunk of req) body += chunk;
      const posted = JSON.parse(body);
      themePosts.push(posted);
      if (themeHTTP === 200) { theme = posted.theme ?? theme; motion = posted.motion ?? motion; language = posted.language ?? language; }
    }
    json({ theme, themes: [], motion, motions: [], language, languages: [] }, themeHTTP);
  } else if (url.pathname === '/api/setup') {
    if (req.method === 'POST') {
      let body = '';
      for await (const chunk of req) body += chunk;
      const posted = JSON.parse(body);
      setupPosts.push(posted);
      if ('onboarded' in posted) setup.onboarded = posted.onboarded;
      if ('client_id' in posted) setup = { ...setup, client_id: posted.client_id || '1556599139436077056', default_client_id: !posted.client_id };
    }
    json(setup);
  } else if (url.pathname === '/api/test-card' && req.method === 'POST') {
    testCardPosts.push(Date.now());
    if (testCardHTTP === 202) state = { ...state, test_card: { state: 'sending', confirmed: false } };
    res.writeHead(testCardHTTP, { 'Content-Type': 'text/plain' });
    res.end(testCardHTTP === 202 ? '{"state":"sending"}' : 'a test card is already showing');
  } else if (url.pathname === '/api/pause' && req.method === 'POST') {
    let body = '';
    for await (const chunk of req) body += chunk;
    const posted = JSON.parse(body);
    pausePosts.push(posted);
    if (pauseHTTP !== 200) { res.writeHead(pauseHTTP, { 'Content-Type': 'text/plain' }); res.end('pause failed'); return; }
    const { paused, paused_until, ...rest } = state;
    state = posted.action === 'pause' ? { ...rest, paused: true, ...(posted.minutes ? { paused_until: new Date(Date.now() + posted.minutes * 60000).toISOString() } : {}) } : rest;
    json({ paused: Boolean(state.paused) });
  } else if (url.pathname === '/api/spotify' && req.method === 'POST') {
    let body = '';
    for await (const chunk of req) body += chunk;
    const posted = JSON.parse(body);
    spotifyPosts.push(posted);
    if (spotifyHTTP !== 200) { res.writeHead(spotifyHTTP, { 'Content-Type': 'text/plain' }); res.end('could not open the browser'); return; }
    const sp = { ...state.spotify, error: '' };
    if (posted.action === 'connect') Object.assign(sp, { waiting: true, client_id: sp.client_id || '' });
    if (posted.action === 'cancel') sp.waiting = false;
    if (posted.action === 'disconnect') Object.assign(sp, { waiting: false, connected: false });
    state = { ...state, spotify: sp };
    json(sp);
  } else if (url.pathname === '/api/recent') {
    recentGets.push(Date.now());
    json({ recent }, recentHTTP);
  } else if (url.pathname === '/api/diagnostics') {
    res.writeHead(200, { 'Content-Type': 'text/plain; charset=utf-8' });
    res.end(diagnosticsReport);
  } else if (url.pathname === '/api/update/install' && req.method === 'POST') {
    updatePosts.push(Date.now());
    if (updateInstallHTTP !== 202) { res.writeHead(updateInstallHTTP, { 'Content-Type': 'text/plain' }); res.end('an update is already in progress\n'); return; }
    updateState = { ...updateState, install:{ state:'downloading' } };
    json(updateState, 202);
  } else if (url.pathname === '/api/update') json(updateState);
  else if (url.pathname === '/api/autostart') {
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
    // Settings are ready once the sources fieldset unlocks; a clean form shows no status line at all.
    const ready = p => p.waitForFunction(() => !document.getElementById('source-fields').disabled);
    const openAppearance = async p => {
      if (!await p.evaluate(() => document.getElementById('appearance').open)) await p.locator('#appearance-open').click();
    };
    const closeAppearance = p => p.evaluate(() => document.getElementById('appearance').close());
    // Picks a theme or motion mode through the dialog, waits for the save, and closes it again.
    const choose = async (p, name, value, saved) => {
      await openAppearance(p);
      const tile = name === 'theme' ? p.locator('label.theme-option', { has: p.locator(`input[value="${value}"]`) }) : p.locator('.segmented label', { has: p.locator(`input[name="${name}"][value="${value}"]`) });
      await tile.scrollIntoViewIfNeeded();
      await tile.click();
      await p.waitForFunction(text => document.querySelector('#theme-status').textContent.includes(text), saved);
      await closeAppearance(p);
    };
    await page.goto(`${origin}/?t=synthetic`);
    await ready(page);
    await page.waitForFunction(() => document.getElementById('playback-state').textContent === 'Playing');
    assert.equal(await page.locator('#source-save').isDisabled(), true);
    assert.equal(await page.locator('#source-status').textContent(), '', 'a clean form says nothing');
    assert.equal(await page.locator('#preview-note').isHidden(), true, 'a healthy connection needs no note');
    assert.equal(await page.locator('#connection').count(), 0, 'no connection pill in the masthead');
    await shot('playing-560');
    assert.equal(await page.locator('#source-order li').count(), 6);
    const firstRow = await page.locator('#source-order li').first().boundingBox();
    assert.ok(firstRow.y + firstRow.height <= 760, 'the hierarchy must be visible immediately at native size');
    assert.equal(await page.locator('#source-order .drag-handle').count(), 6);
    check('native-size playing layout and saved state');

    const firstLink = page.locator('#buttons a').first();
    await firstLink.focus();
    await page.evaluate(() => {
      window.statusMutations = 0;
      window.statusObserver = new MutationObserver(records => { window.statusMutations += records.length; });
      for (const id of ['preview-note', 'order-status']) window.statusObserver.observe(document.getElementById(id), { childList: true });
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

    // Pause sharing: a small control opens the two choices; a paused state shows one compact line with Resume.
    assert.equal(await page.locator('#pause-line').isHidden(), true, 'no pause line while sharing');
    assert.equal(await page.locator('#pause-choices').isHidden(), true);
    assert.equal(await page.locator('#pause-open').textContent(), 'Pause sharing');
    await page.locator('#pause-open').click();
    assert.equal(await page.locator('#pause-open').getAttribute('aria-expanded'), 'true');
    assert.deepEqual(await page.locator('#pause-choices button').allTextContents(), ['For 1 hour', 'Until I resume']);
    await page.locator('#pause-hour').focus();
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#pause-choices').isHidden(), true, 'Escape closes the choices');
    assert.equal(await page.evaluate(() => document.activeElement.id), 'pause-open', 'focus returns to the control');
    await page.locator('#pause-open').click();
    await page.locator('#pause-hour').click();
    await page.waitForFunction(() => !document.getElementById('pause-line').hidden);
    assert.deepEqual(pausePosts.at(-1), { action: 'pause', minutes: 60 });
    assert.match(await page.locator('#pause-text').textContent(), /^Sharing paused until \d\d:\d\d$/);
    assert.equal(await page.locator('#pause-open').isHidden(), true, 'the pause control gives way to Resume');
    assert.equal(await page.locator('#pause-choices').isHidden(), true);
    await shot('paused-560');
    await page.setViewportSize({ width: 320, height: 760 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'the pause line fits 320px');
    await shot('paused-320');
    await page.setViewportSize({ width: 560, height: 760 });
    await page.locator('#pause-resume').click();
    await page.waitForFunction(() => document.getElementById('pause-line').hidden);
    assert.deepEqual(pausePosts.at(-1), { action: 'resume' });
    assert.equal(await page.locator('#pause-open').isVisible(), true);
    await page.locator('#pause-open').click();
    await page.locator('#pause-open-ended').click();
    await page.waitForFunction(() => document.getElementById('pause-text').textContent === 'Sharing paused');
    assert.deepEqual(pausePosts.at(-1), { action: 'pause' });
    state = { ...state, paused: true, paused_until: '2026-11-12T15:30:00' };
    await refresh();
    assert.equal(await page.locator('#pause-text').textContent(), 'Sharing paused until 15:30');
    await page.locator('#pause-resume').click();
    await page.waitForFunction(() => document.getElementById('pause-line').hidden);
    await page.setViewportSize({ width: 320, height: 760 });
    await page.locator('#pause-open').click();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'the pause choices fit 320px');
    await page.setViewportSize({ width: 560, height: 760 });
    await page.locator('#pause-open').click();
    pauseHTTP = 500;
    await page.locator('#pause-open').click();
    await page.locator('#pause-hour').click();
    await waitText('#preview-note', 'Could not change the pause');
    assert.equal(await page.locator('#pause-line').isHidden(), true, 'a failed pause shows no pause line');
    pauseHTTP = 200;
    await page.evaluate(() => { pauseFailure = ''; });
    await refresh();
    assert.equal(await page.locator('#preview-note').isHidden(), true);
    check('Pause sharing: choices, 1 hour and until-resumed, the paused line with Resume, a failed save, and 320px');

    await page.locator('label[for=source-youtube]').click({ position: { x: 3, y: 3 } });
    assert.equal(await page.locator('#browser-dependency').isVisible(), true);
    assert.equal(await page.locator('#source-browser').isChecked(), false, 'dependency hints must not silently change saved preferences');
    assert.equal(await page.locator('#source-save').isEnabled(), true);
    await page.locator('#source-reset').click();
    assert.equal(await page.locator('#source-youtube').isChecked(), false);
    assert.equal(await page.locator('#source-save').isDisabled(), true);
    assert.equal(await page.locator('#browser-dependency').isHidden(), true);
    await page.locator('label[for=source-twitch]').click({ position: { x: 3, y: 3 } });
    assert.equal(await page.locator('#browser-dependency').isVisible(), true, 'Twitch also needs Browser media');
    await page.locator('#source-browser').check();
    assert.equal(await page.locator('#browser-dependency').isHidden(), true);
    await page.locator('#source-reset').click();
    assert.equal(await page.locator('#source-twitch').isChecked(), false);
    check('unsaved changes, dependency hint for YouTube and Twitch, and discard');

    await page.getByRole('button', { name: 'Move Spotify (desktop) up', exact: true }).focus();
    await page.keyboard.press('Enter');
    assert.equal(await page.locator('#source-order li').first().getAttribute('data-source'), 'spotify');
    assert.equal(await page.evaluate(() => document.activeElement.dataset.source), 'spotify');
    await page.keyboard.press('Tab');
    assert.equal(await page.evaluate(() => document.activeElement.getAttribute('aria-label')), 'Reorder Spotify (desktop)');
    await page.keyboard.press('ArrowDown');
    assert.equal(await page.evaluate(() => document.activeElement.getAttribute('aria-label')), 'Reorder Spotify (desktop)');
    assert.equal(await page.locator('#source-save').isDisabled(), true, 'returning to the saved order should clear dirty state');
    assert.equal(await page.locator('#source-status').textContent(), '');
    await page.keyboard.press('ArrowDown');
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
    assert.equal(Object.keys(posts.at(-1).enabled).length, 9);
    assert.equal(posts.at(-1).enabled.twitch, false);
    assert.equal(await page.locator('#source-save').isDisabled(), true);
    saveDelay = 0;
    check('failed-save retry, exact payload, and reorder lock during save');

    await page.locator('#source-browser').check();
    const togglesBeforeDrag = await page.evaluate(() => currentSources());
    await page.locator('#source-order').evaluate(list => list.scrollIntoView({block:'center'}));
    let handle = await page.locator('#source-order li').first().locator('.drag-handle').boundingBox();
    let target = await page.locator('#source-order li').last().boundingBox();
    await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
    await page.mouse.down();
    await page.mouse.move(handle.x + handle.width / 2, target.y + target.height - 8, {steps:12});
    assert.equal(await page.locator('.drag-preview').isVisible(), true);
    assert.equal(await page.locator('.drop-after').count(), 1);
    assert.deepEqual(await page.locator('#source-order li').evaluateAll(rows => rows.map(row => row.dataset.source)), beforeSavingOrder, 'previewing a drop must not mutate the order');
    await shot('mouse-drag-560');
    await page.mouse.up();
    assert.equal(await page.locator('.drag-preview').count(), 0);
    assert.deepEqual(await page.evaluate(() => currentSources()), togglesBeforeDrag, 'drag must retain every toggle value');
    assert.equal(await page.locator('#source-order li').last().getAttribute('data-source'), beforeSavingOrder[0]);
    assert.equal(await page.locator('#source-save').isEnabled(), true);
    await shot('priority-560');
    await page.locator('#source-save').click();
    await waitText('#source-status', 'Saved. Discord');
    assert.deepEqual(posts.at(-1).enabled, togglesBeforeDrag);
    assert.equal(posts.at(-1).order.at(-1), beforeSavingOrder[0]);
    check('mouse drag feedback and toggle-then-drag saved payload');

    const orderBeforeCancel = await page.locator('#source-order li').evaluateAll(rows => rows.map(row => row.dataset.source));
    await page.locator('#source-order').evaluate(list => list.scrollIntoView({block:'center'}));
    handle = await page.locator('#source-order li').last().locator('.drag-handle').boundingBox();
    target = await page.locator('#source-order li').first().boundingBox();
    await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
    await page.mouse.down();
    await page.mouse.move(handle.x + handle.width / 2, target.y + 8, {steps:12});
    assert.equal(await page.locator('.drop-before').count(), 1);
    await page.keyboard.press('Escape');
    await page.mouse.up();
    assert.deepEqual(await page.locator('#source-order li').evaluateAll(rows => rows.map(row => row.dataset.source)), orderBeforeCancel);
    assert.equal(await page.locator('.drag-preview').count(), 0);
    assert.equal(await page.locator('#source-save').isDisabled(), true);
    // Releasing outside the list also cancels without changing settings.
    await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
    await page.mouse.down();
    await page.mouse.move(2, target.y + 8, {steps:12});
    await page.mouse.up();
    assert.deepEqual(await page.locator('#source-order li').evaluateAll(rows => rows.map(row => row.dataset.source)), orderBeforeCancel);
    await page.locator('#source-order li').first().locator('.move button').last().click();
    assert.equal(await page.locator('#order-status').textContent(), '', 'a new reorder must clear stale cancellation feedback');
    await page.locator('#source-reset').click();
    assert.deepEqual(await page.locator('#source-order li').evaluateAll(rows => rows.map(row => row.dataset.source)), orderBeforeCancel);
    check('Escape and outside-list mouse cancellation');

    await page.locator('#autostart').check();
    await waitText('#autostart-status', 'will start');
    autostartHTTP = 500;
    await page.locator('#autostart').uncheck();
    await waitText('#autostart-status', 'Could not change');
    assert.equal(await page.locator('#autostart').isChecked(), true);
    autostartHTTP = 200;
    check('startup save and failed-save rollback');

    // Themes: each one applies at once, saves, rolls back on failure and keeps text readable.
    const themeIds = await page.locator('input[name="theme"]').evaluateAll(inputs => inputs.map(input => input.value));
    assert.deepEqual(themeIds, ['yozora', 'uika', 'pine', 'kurisu', 'ava', 'astra', 'mariyaholic-98', 'sumimi', 'ledger', 'confluence', 'astra-light']);
    assert.equal(await page.locator('input[name="theme"][value="yozora"]').isChecked(), true);
    const ratio = (a, b) => {
      const lum = hex => { const v = hex.match(/\d+(\.\d+)?/g).slice(0, 3).map(Number).map(c => { c /= 255; return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4; }); return 0.2126 * v[0] + 0.7152 * v[1] + 0.0722 * v[2]; };
      const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
      return (x + 0.05) / (y + 0.05);
    };
    // Appearance lives in a dialog off the masthead.
    await page.evaluate(() => scrollTo(0, 0));
    await page.locator('#appearance-open').click();
    assert.equal(await page.evaluate(() => document.getElementById('appearance').open), true);
    assert.equal(await page.evaluate(() => document.activeElement.name === 'theme' && document.activeElement.checked), true, 'focus starts on the saved theme');
    await page.waitForFunction(() => document.fonts.status === 'loaded');
    await page.screenshot({ path: path.join(output, 'appearance-dialog-560.png') });
    await page.keyboard.press('Escape');
    assert.equal(await page.evaluate(() => document.getElementById('appearance').open), false);
    assert.equal(await page.evaluate(() => document.activeElement.id), 'appearance-open', 'closing returns focus to the opener');
    await page.locator('#appearance-open').click();
    await page.mouse.click(4, 4);
    assert.equal(await page.evaluate(() => document.getElementById('appearance').open), false, 'a backdrop click closes the dialog');
    await page.locator('#appearance-open').click();
    await page.locator('#appearance-close').click();
    assert.equal(await page.evaluate(() => document.getElementById('appearance').open), false);
    check('appearance dialog opens from the masthead, starts on the saved theme, and closes by Escape, backdrop, or button');

    // Each theme must actually load its own faces from the embedded files, not fall back to Segoe UI.
    const faces = {
      yozora: [], confluence: [],
      pine: ['Inter', 'Outfit', 'JetBrains Mono'], kurisu: ['Inter', 'Outfit', 'JetBrains Mono'], ava: ['Inter', 'Outfit', 'JetBrains Mono'], ledger: ['Inter', 'Outfit', 'JetBrains Mono'],
      astra: ['IBM Plex Sans', 'Source Serif 4', 'IBM Plex Mono'], 'astra-light': ['IBM Plex Sans', 'Source Serif 4', 'IBM Plex Mono'],
      uika: ['Outfit', 'Cinzel', 'JetBrains Mono'], sumimi: ['Outfit', 'Cinzel', 'JetBrains Mono'], 'mariyaholic-98': ['Departure Mono'],
    };
    const applyTheme = async id => {
      await choose(page, 'theme', id, 'theme saved');
      await page.evaluate(() => document.fonts.ready);
      const loaded = await page.evaluate(() => [...document.fonts].filter(face => face.status === 'loaded').map(face => face.family.replace(/"/g, '')));
      for (const family of faces[id]) assert.ok(loaded.includes(family), `${id}: ${family} did not load (loaded: ${loaded})`);
      const used = await page.evaluate(() => [getComputedStyle(document.querySelector('h1')).fontFamily, getComputedStyle(document.body).fontFamily]);
      if (faces[id].length) assert.ok(faces[id].some(family => used.join().includes(family)), `${id}: the page does not ask for its faces`);
      assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), id);
      assert.deepEqual(themePosts.at(-1), { theme: id });
      const roles = await page.evaluate(() => {
        const probe = document.createElement('i');
        document.body.appendChild(probe);
        const read = name => { probe.style.color = `var(${name})`; return getComputedStyle(probe).color; };
        const out = Object.fromEntries(['--bg', '--panel', '--panel-2', '--text', '--muted', '--dim', '--accent', '--on-accent', '--link', '--good', '--bad', '--warn', '--line-strong'].map(name => [name, read(name)]));
        probe.remove();
        return out;
      });
      for (const ground of ['--bg', '--panel']) {
        for (const ink of ['--text', '--muted', '--dim', '--accent', '--link', '--bad', '--warn']) {
          assert.ok(ratio(roles[ink], roles[ground]) >= 4.5, `${id}: ${ink} on ${ground} is ${ratio(roles[ink], roles[ground]).toFixed(2)}:1`);
        }
      }
      assert.ok(ratio(roles['--on-accent'], roles['--accent']) >= 4.5, `${id}: primary button text is ${ratio(roles['--on-accent'], roles['--accent']).toFixed(2)}:1`);
      assert.ok(ratio(roles['--text'], roles['--panel-2']) >= 4.5, `${id}: button text`);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
      await page.evaluate(() => scrollTo(0, 0));
      await shot(`theme-${id}-560`);
    };
    for (const id of themeIds.slice(1)) await applyTheme(id);
    themeHTTP = 500;
    await choose(page, 'theme', 'ava', 'Could not save');
    assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), 'astra-light', 'a failed save must restore the saved theme');
    assert.equal(await page.locator('input[name="theme"][value="astra-light"]').isChecked(), true);
    themeHTTP = 200;
    await applyTheme('yozora');
    check('eleven design-system themes apply, save, roll back, keep 4.5:1 text, and load their own faces');

    state = { ...state, playing: true, activity_type: 'watching', title: 'Ranked grind, then chat', artist: 'streamer', album: '', service: 'Twitch', source: 'browser', duration_sec: 0, art_url: '/test-art.svg', published_activity: { buttons: [{ label: 'Watch on Twitch', url: 'https://www.twitch.tv/streamer' }] } };
    await refresh();
    assert.equal(await page.locator('#head').textContent(), 'Watching Twitch');
    assert.equal(await page.locator('#buttons a').textContent(), 'Watch on Twitch');
    assert.equal(await page.locator('#timeline').isHidden(), true, 'a live stream has no progress bar');
    await shot('twitch-560');
    check('Twitch stream preview with live thumbnail and watch button');

    state = { ...state, playing: true, activity_type: 'listening', service: 'Spotify', source: 'spotify', duration_sec: 243, title: 'A very long synthetic track title — ' + 'unbroken'.repeat(15), artist: 'Synthetic artist name '.repeat(12), published_activity: { buttons: [{ label: 'Unsafe', url: 'javascript:alert(1)' }, { label: 'Credentials', url: 'https://name:password@example.test' }, { label: 'Yozora', url: 'https://github.com/mariyaholic/yozora' }] } };
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
    assert.match(await page.locator('#preview-note').textContent(), /Discord isn’t open/);
    assert.match(await page.locator('#meta').textContent(), /Discord: not running/);
    await shot('discord-offline-560');
    stateHTTP = 503;
    await refresh();
    assert.match(await page.locator('#preview-note').textContent(), /Can’t reach Yozora/);
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
    await waitText('#preview-note', 'Can’t reach Yozora');
    assert.equal(await page.locator('#det').textContent(), 'Waiting for Yozora');
    await shot('initial-offline-560');
    stateHTTP = 200;
    await refresh();
    await waitText('#source-status', 'Could not load');
    assert.equal(await page.locator('#source-applemusic').isDisabled(), true);
    sourceGetHTTP = 200;
    await page.locator('#source-retry').click();
    await ready(page);
    assert.equal(await page.locator('#source-status').textContent(), '');
    assert.equal(await page.locator('#source-applemusic').isEnabled(), true);
    assert.equal(await page.evaluate(() => document.activeElement.id), 'source-applemusic');
    check('broken-art fallback and initial settings-load retry');

    // Motion, with reduced motion off. Every theme must be still at idle, animate only on events,
    // and leave no transform, class or running animation behind once an interaction ends.
    {
      const motion = await browser.newContext({ viewport:{ width:560, height:760 } });
      await motion.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort());
      const m = await motion.newPage();
      m.on('pageerror', error => errors.push(error.message));
      const mText = (selector, text) => m.waitForFunction(([selector, text]) => document.querySelector(selector).textContent.includes(text), [selector, text]);
      const mRefresh = () => m.evaluate(async () => { while (polling) await new Promise(resolve => setTimeout(resolve, 10)); await tick(); });
      const running = () => m.evaluate(() => document.getAnimations().filter(a => a.playState === 'running').map(a => a.animationName || a.transitionProperty));
      const loops = () => m.evaluate(() => document.getAnimations().filter(a => a.playState === 'running' && a.effect.getComputedTiming().iterations === Infinity).map(a => a.animationName));
      const order = () => m.locator('#source-order li').evaluateAll(rows => rows.map(row => row.dataset.source));
      const settled = async (label = '') => {
        // Wait until still (a hover fade may follow a slide), but fail on anything that keeps running.
        // Stillness must hold across two samples 300ms apart (Chromium may start a hover fade late after
        // rows re-render under a still cursor). The live dot is the one sanctioned loop while music plays.
        const moving = () => m.evaluate(() => document.getAnimations().filter(a => a.playState === 'running' && a.animationName !== 'live-pulse').map(a => `${a.animationName || a.transitionProperty}@${a.effect.target.id || a.effect.target.className || a.effect.target.tagName}:${a.effect.target.textContent.slice(0, 12)}`));
        let left = await moving();
        for (let tries = 0; tries < 6; tries++) {
          if (!left.length) { await m.waitForTimeout(300); left = await moving(); if (!left.length) break; }
          else { await m.waitForTimeout(300); left = await moving(); }
        }
        assert.deepEqual(left, [], `${label}: an interaction left an animation running`);
        assert.equal(await m.locator('#source-order li.parted, #source-order li.dragging, .drag-preview, .drop-before, .drop-after').count(), 0);
        assert.equal(await m.locator('#source-order li').evaluateAll(rows => rows.every(row => !row.style.transform && !row.style.transition && getComputedStyle(row).transform === 'none')), true, 'rows must return to their layout position');
      };
      const pick = id => choose(m, 'theme', id, 'theme saved');
      const playing = { ...state, playing:true, title:'Night Walk', artist:'Mira', album:'After Hours', service:'Spotify', source:'spotify', elapsed_sec:82, duration_sec:243, discord_connected:true, art_url:'/test-art.svg' };
      state = { playing:false, title:'', discord_connected:true, sends:12 };
      await m.goto(`${origin}/?t=synthetic`);
      await ready(m);
      for (const id of themeIds) {
        if (id !== 'yozora') await pick(id);
        state = { playing:false, title:'', discord_connected:true, sends:12 };
        await mRefresh();
        await m.waitForTimeout(700);
        assert.deepEqual(await running(), [], `${id}: something animates at idle`);
        state = playing;
        await mRefresh();
        await m.waitForTimeout(700);
        assert.deepEqual(await loops(), ['pine', 'kurisu', 'ava'].includes(id) ? ['live-pulse'] : [], `${id}: only Argonaut Oak's live dot may loop, and only while playing`);
      }
      check('every theme is still at idle; only the live dot loops, only while playing');

      await pick('yozora');
      state = { ...playing, title:'Second Track' };
      await mRefresh();
      assert.equal(await m.evaluate(() => document.body.classList.contains('track-changed')), true);
      assert.ok((await running()).includes('twinkle') && (await running()).includes('art-in'), 'Night sky cues the star and artwork on a new track');
      await m.waitForTimeout(1000);
      assert.equal(await m.evaluate(() => document.body.classList.contains('track-changed')), false);
      stateHTTP = 503; await mRefresh(); stateHTTP = 200; await mRefresh();
      assert.equal(await m.evaluate(() => document.body.classList.contains('track-changed') || document.body.classList.contains('state-changed')), false, 'a reconnect must not replay the cue');
      await pick('ava');
      state = { ...state, playing:false };
      await mRefresh();
      assert.ok((await running()).includes('readout'), 'Ava flashes its readouts on a play-state change');
      await m.waitForTimeout(1000);
      await pick('kurisu');
      state = { ...state, playing:true, title:'Third Track' };
      await mRefresh();
      assert.ok((await running()).includes('phosphor'));
      await m.waitForTimeout(1000);
      check('track and play-state cues fire once per change and never on reconnect');

      const dragTo = async (from, toRow, edge = 'top') => {
        await m.locator('#source-order').evaluate(list => list.scrollIntoView({ block:'center' }));
        const handle = await m.locator('#source-order li').nth(from).locator('.drag-handle').boundingBox();
        const target = await m.locator('#source-order li').nth(toRow).boundingBox();
        await m.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
        await m.mouse.down();
        await m.mouse.move(handle.x + handle.width / 2, edge === 'top' ? target.y + 4 : target.y + target.height - 4, { steps:10 });
      };
      for (const id of ['yozora', 'uika', 'sumimi', 'confluence', 'mariyaholic-98', 'pine', 'astra', 'ledger', 'ava']) {
        await pick(id);
        const start = await order();
        await dragTo(0, 3);
        assert.equal(await m.locator('#source-order li.parted').count(), 3, `${id}: rows from the drop point part`);
        await m.waitForTimeout(400);
        assert.notEqual(await m.locator('#source-order li').nth(3).evaluate(row => getComputedStyle(row).transform), 'none');
        assert.deepEqual(await order(), start, `${id}: parting must not reorder`);
        if (id === 'mariyaholic-98') assert.equal(await m.locator('.drag-preview').evaluate(node => parseFloat(node.style.left) % 4 === 0 && parseFloat(node.style.top) % 4 === 0), true, 'the PC-98 preview moves on a 4px grid');
        if (id === 'ava') assert.deepEqual(await loops(), ['live-pulse', 'sweep'], 'Ava sweeps only while a drop line is shown');
        await m.screenshot({ path:path.join(output, `motion-drag-${id}-viewport.png`) });
        await m.keyboard.press('Escape');
        await m.mouse.up();
        assert.deepEqual(await order(), start);
        await settled(`${id} cancel`);
        await dragTo(0, 3);
        await m.mouse.up();
        assert.deepEqual(await order(), [start[1], start[2], start[0], ...start.slice(3)], `${id}: drop lands between the parted rows`);
        await settled(`${id} drop`);
        await m.locator('#source-order li').nth(0).locator('.move button').last().click();
        const slid = await m.evaluate(() => document.getAnimations().some(a => a.transitionProperty === 'transform'));
        assert.equal(slid, ['pine', 'astra', 'ledger', 'ava'].includes(id) ? false : true, `${id}: rows ${slid ? 'slid' : 'snapped'} on an arrow move`);
        await settled(`${id} arrow`);
        await m.locator('#source-reset').click();
        assert.deepEqual(await order(), start);
        await settled(`${id} discard`);
      }
      check('drag parts rows, drops between them, cancels cleanly, and each theme slides or snaps as designed');

      await pick('mariyaholic-98');
      await m.locator('#source-generic').check();
      saveDelay = 300;
      await m.locator('#source-save').click();
      await m.waitForFunction(() => document.getElementById('source-status').dataset.busy === 'true');
      assert.equal(await m.locator('#source-status').evaluate(node => getComputedStyle(node, '::after').content), '"█"');
      assert.ok((await loops()).includes('caret'));
      await mText('#source-status', 'Saved. Discord');
      saveDelay = 0;
      assert.equal(await m.locator('#source-status[data-busy]').count(), 0);
      assert.equal((await loops()).includes('caret'), false, 'the cursor stops when the save ends');
      sourcePostHTTP = 500;
      await m.locator('#source-generic').uncheck();
      await m.locator('#source-save').click();
      await mText('#source-status', 'Save failed');
      assert.equal(await m.locator('#source-status[data-busy]').count(), 0, 'a failed save also clears the busy cursor');
      sourcePostHTTP = 200;
      await m.locator('#source-save').click();
      await mText('#source-status', 'Saved. Discord');
      check('PC-98 busy cursor exists only while a save is in flight, including failures');

      await pick('sumimi');
      const button = m.locator('#source-order li').nth(1).locator('.move button').first();
      await button.evaluate(node => { const box = node.getBoundingClientRect(); node.dispatchEvent(new PointerEvent('pointerdown', { bubbles:true, clientX:box.left + 4, clientY:box.top + 4 })); });
      assert.equal(await button.evaluate(node => node.classList.contains('pressed') && node.style.getPropertyValue('--px') === '4px'), true);
      assert.ok((await running()).includes('ink-spread'));
      await m.waitForTimeout(600);
      assert.equal(await button.evaluate(node => node.classList.contains('pressed')), false);
      check('Sumimi press wash starts at the pointer and clears itself');

      // Motion mode: Reduced stills everything even though this context allows motion; a failed save keeps the old mode.
      const allAnimations = page => page.evaluate(() => document.getAnimations().map(a => a.animationName || a.transitionProperty));
      const setMotion = (page, value, name) => choose(page, 'motion', value, `${name} motion saved`);
      await pick('yozora');
      await setMotion(m, 'reduced', 'Reduced');
      assert.deepEqual(themePosts.at(-1), { motion:'reduced' }, 'a motion change posts only the motion field');
      assert.deepEqual(await m.evaluate(() => [document.documentElement.dataset.theme, document.documentElement.dataset.motion]), ['yozora', 'reduced']);
      await m.waitForTimeout(400);
      state = { ...playing, title:'Reduced Mode Track' };
      await mRefresh();
      await m.locator('#source-order li').nth(0).locator('.move button').last().click();
      assert.deepEqual(await allAnimations(m), [], 'Reduced mode: no cue, slide or hover transition');
      await m.locator('#source-reset').click();
      await pick('pine');
      await m.waitForTimeout(300);
      assert.deepEqual(await loops(), [], 'Reduced mode stops the live dot');
      themeHTTP = 500;
      await choose(m, 'motion', 'full', 'Could not save the motion');
      assert.equal(await m.evaluate(() => document.documentElement.dataset.motion), 'reduced');
      assert.equal(await m.locator('input[name="motion"][value="reduced"]').isChecked(), true);
      themeHTTP = 200;
      await setMotion(m, 'system', 'Follow Windows');
      await m.waitForTimeout(300);
      assert.deepEqual(await loops(), ['live-pulse'], 'Follow Windows restores motion when Windows allows it');
      await pick('yozora');
      assert.equal(await m.locator('#source-order li').evaluateAll(rows => rows.every(row => !row.style.transform)), true);
      check('Reduced motion mode stills the panel when Windows allows motion; a failed save keeps the previous mode');

      themeHTTP = 500;
      await m.reload();
      await mText('#theme-status', 'Could not confirm');
      assert.equal(await m.locator('#theme-fields').isEnabled(), true, 'a failed theme read must not lock the picker');
      themeHTTP = 200;
      await pick('pine');
      await pick('yozora');
      check('a failed theme read leaves the picker usable with the server-rendered theme');
      violations.push(...await m.evaluate(() => window.cspViolations || []));
      await motion.close();

      const still = await browser.newContext({ viewport:{ width:560, height:760 }, reducedMotion:'reduce' });
      await still.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort());
      const r = await still.newPage();
      r.on('pageerror', error => errors.push(error.message));
      await r.goto(`${origin}/?t=synthetic`);
      await ready(r);
      state = { ...playing, title:'Reduced Motion Track' };
      await r.evaluate(async () => { while (polling) await new Promise(resolve => setTimeout(resolve, 10)); await tick(); });
      await r.locator('#source-order li').nth(0).locator('.move button').last().click();
      assert.deepEqual(await r.evaluate(() => document.getAnimations().map(a => a.animationName || a.transitionProperty)), [], 'reduced motion turns every animation off');
      await r.locator('#source-reset').click();
      check('reduced motion: no animation or transition at all');
      // Full overrides Windows' reduced setting; Follow Windows goes back to still.
      await setMotion(r, 'full', 'Full');
      state = { ...playing, title:'Full Mode Track' };
      await r.evaluate(async () => { while (polling) await new Promise(resolve => setTimeout(resolve, 10)); await tick(); });
      assert.ok((await allAnimations(r)).includes('twinkle'), 'Full mode cues the track change despite Windows reduced motion');
      await r.waitForTimeout(1000);
      await r.locator('#source-order li').nth(0).locator('.move button').last().click();
      assert.ok((await allAnimations(r)).includes('transform'), 'Full mode slides rows');
      await r.waitForTimeout(600);
      await r.locator('#source-reset').click();
      await r.waitForTimeout(600);
      await setMotion(r, 'system', 'Follow Windows');
      await r.locator('#source-order li').nth(0).locator('.move button').last().click();
      assert.deepEqual(await allAnimations(r), [], 'Follow Windows is still again');
      await r.locator('#source-reset').click();
      await still.close();
      check('Full motion mode overrides Windows reduced motion; Follow Windows returns to it');
      state = playing;
    }

    // CDP dispatches actual touchscreen input through Chromium's event pipeline,
    // including pointer capture, pointercancel, and native page scrolling.
    state = { ...state, playing:true, title:'Night Walk', artist:'Mira', album:'After Hours', service:'Spotify', source:'spotify', elapsed_sec:82, duration_sec:243, discord_connected:true, art_url:'/test-art.svg' };
    for (const width of [390, 320]) {
      const touchContext = await browser.newContext({ viewport:{width, height:760}, hasTouch:true, isMobile:true, deviceScaleFactor:1, reducedMotion:'reduce' });
      await touchContext.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort());
      await touchContext.addInitScript(() => {
        window.cspViolations = [];
        window.pointerTypes = [];
        document.addEventListener('securitypolicyviolation', event => window.cspViolations.push(event.violatedDirective));
        document.addEventListener('pointerdown', event => window.pointerTypes.push(event.pointerType));
      });
      const touchPage = await touchContext.newPage();
      touchPage.on('pageerror', error => errors.push(error.message));
      const cdp = await touchContext.newCDPSession(touchPage);
      const touch = (type, x, y) => cdp.send('Input.dispatchTouchEvent', { type, touchPoints: type === 'touchEnd' || type === 'touchCancel' ? [] : [{x, y, id:1}] });
      const slide = async (x, from, to) => {
        for (let step = 1; step <= 12; step++) {
          await touch('touchMove', x, from + (to - from) * step / 12);
          await touchPage.waitForTimeout(16);
        }
      };
      const touchOrder = () => touchPage.locator('#source-order li').evaluateAll(rows => rows.map(row => row.dataset.source));
      const touchShot = async (name, activeDrag = false) => {
        await touchPage.screenshot({path:path.join(output, `${name}.png`), fullPage:!activeDrag});
        await touchPage.screenshot({path:path.join(output, `${name}-viewport.png`)});
      };
      const centerList = () => touchPage.locator('#source-order').evaluate(list => list.scrollIntoView({block:'center'}));
      await touchPage.goto(`${origin}/?t=synthetic`);
      await ready(touchPage);
      assert.equal(await touchPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
      await touchShot(`phone-${width}`);
      const initialOrder = await touchOrder();
      const initialToggles = await touchPage.evaluate(() => currentSources());
      await centerList();
      let grip = await touchPage.locator('#source-order .drag-handle').first().boundingBox();
      let row = await touchPage.locator('#source-order li').last().boundingBox();
      const originalScroll = await touchPage.evaluate(() => scrollY);
      await touch('touchStart', grip.x + 22, grip.y + 22);
      await slide(grip.x + 22, grip.y + 22, row.y + row.height - 8);
      assert.equal(await touchPage.locator('.drag-preview').isVisible(), true);
      assert.equal(await touchPage.locator('.drop-after').count(), 1);
      assert.deepEqual(await touchOrder(), initialOrder);
      assert.equal(await touchPage.evaluate(() => scrollY), originalScroll, 'handle drag should not initiate native scrolling');
      await touchShot(`touch-drag-${width}`, true);
      await touch('touchEnd');
      assert.deepEqual(await touchOrder(), [...initialOrder.slice(1), initialOrder[0]]);
      assert.deepEqual(await touchPage.evaluate(() => currentSources()), initialToggles);
      await touchPage.locator('#source-save').tap();
      await touchPage.waitForFunction(() => document.querySelector('#source-status').textContent.includes('Saved. Discord'));
      assert.deepEqual(posts.at(-1), {enabled:initialToggles, order:[...initialOrder.slice(1), initialOrder[0]]});
      const savedTouchOrder = await touchOrder();

      await centerList();
      grip = await touchPage.locator('#source-order .drag-handle').first().boundingBox();
      row = await touchPage.locator('#source-order li').last().boundingBox();
      await touch('touchStart', grip.x + 22, grip.y + 22);
      await slide(grip.x + 22, grip.y + 22, row.y + row.height - 8);
      await touch('touchCancel');
      assert.deepEqual(await touchOrder(), savedTouchOrder);
      assert.equal(await touchPage.locator('.drag-preview').count(), 0);
      assert.equal(await touchPage.locator('.drop-after').count(), 0);
      assert.equal(await touchPage.locator('#source-save').isDisabled(), true);
      await touchPage.locator('#source-order .drag-handle').first().tap();
      assert.deepEqual(await touchOrder(), savedTouchOrder, 'a grip tap alone must not reorder');
      assert.equal(await touchPage.locator('#source-save').isDisabled(), true);

      // A swipe on a row's label must scroll, never drag or toggle it.
      await centerList();
      const label = await touchPage.locator('#source-order .source-option').nth(3).boundingBox();
      const scrollBeforeSwipe = await touchPage.evaluate(() => scrollY);
      await touch('touchStart', label.x + label.width / 2, label.y + label.height / 2);
      await slide(label.x + label.width / 2, label.y + label.height / 2, label.y - 100);
      await touch('touchEnd');
      await touchPage.waitForFunction(before => scrollY > before + 30, scrollBeforeSwipe);
      assert.deepEqual(await touchOrder(), savedTouchOrder);
      assert.deepEqual(await touchPage.evaluate(() => currentSources()), initialToggles);
      assert.equal(await touchPage.locator('.drag-preview').count(), 0);
      assert.ok((await touchPage.evaluate(() => window.pointerTypes)).every(type => type === 'touch'));
      check(`${width}px real-touch drag, saved payload, cancellation, and native row scrolling`);

      // A short viewport forces both ends of the hierarchy off-screen in turn.
      await touchPage.setViewportSize({width, height:300});
      await touchPage.locator('#source-order').evaluate(list => scrollTo(0, scrollY + list.getBoundingClientRect().top - 110));
      grip = await touchPage.locator('#source-order .drag-handle').first().boundingBox();
      const scrollBeforeDown = await touchPage.evaluate(() => scrollY);
      await touch('touchStart', grip.x + 22, grip.y + 22);
      await slide(grip.x + 22, grip.y + 22, 284);
      await touchPage.waitForFunction(() => document.getElementById('source-order').getBoundingClientRect().bottom <= innerHeight - 10);
      assert.ok(await touchPage.evaluate(before => scrollY > before + 50, scrollBeforeDown));
      assert.deepEqual(await touchOrder(), savedTouchOrder, 'edge scrolling must only preview the order');
      await touch('touchEnd');
      const afterDown = [...savedTouchOrder.slice(1), savedTouchOrder[0]];
      assert.deepEqual(await touchOrder(), afterDown);
      grip = await touchPage.locator('#source-order .drag-handle').last().boundingBox();
      const scrollBeforeUp = await touchPage.evaluate(() => scrollY);
      await touch('touchStart', grip.x + 22, grip.y + 22);
      await slide(grip.x + 22, grip.y + 22, 16);
      await touchPage.waitForFunction(() => document.getElementById('source-order').getBoundingClientRect().top >= 10);
      assert.ok(await touchPage.evaluate(before => scrollY < before - 30, scrollBeforeUp));
      assert.deepEqual(await touchOrder(), afterDown);
      await touch('touchEnd');
      assert.deepEqual(await touchOrder(), savedTouchOrder);
      assert.equal(await touchPage.locator('#source-save').isDisabled(), true);
      assert.equal(await touchPage.locator('.drag-preview').count(), 0);
      check(`${width}px touch edge scrolling reaches both ends without changing order until release`);
      violations.push(...await touchPage.evaluate(() => window.cspViolations));
      await touchContext.close();
    }

    // First-run checklist and Spotify connect, in a dark and a light theme.
    {
      const sc = await browser.newContext({ viewport:{ width:560, height:760 }, reducedMotion:'reduce' });
      await sc.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort());
      await sc.addInitScript(() => {
        window.cspViolations = [];
        document.addEventListener('securitypolicyviolation', event => window.cspViolations.push(event.violatedDirective));
      });
      const o = await sc.newPage();
      o.on('pageerror', error => errors.push(error.message));
      const oRefresh = () => o.evaluate(async () => { while (polling) await new Promise(resolve => setTimeout(resolve, 10)); await tick(); });
      const oText = (selector, text) => o.waitForFunction(([selector, text]) => document.querySelector(selector).textContent.includes(text), [selector, text]);
      const rowState = id => o.locator(`#check-${id}`).getAttribute('data-state');
      const note = id => o.locator(`#check-${id}-note`).textContent();
      const oShot = async name => { await o.evaluate(() => scrollTo(0, 0)); await o.screenshot({ path:path.join(output, `${name}.png`), fullPage:true }); };
      const playingTrack = { playing:true, activity_type:'listening', title:'Night Walk', artist:'Mira', album:'After Hours', service:'Spotify', source:'spotify', elapsed_sec:82, duration_sec:243, art_url:'/test-art.svg', small_image:'/test-badge.svg' };

      setup = { ...setup, onboarded:true };
      autostart = false;
      state = { playing:false, title:'', discord_connected:false, sends:0 };
      await o.goto(`${origin}/?t=synthetic`);
      await ready(o);
      await oRefresh();
      assert.equal(await o.locator('#setup').isHidden(), true, 'an onboarded panel shows no checklist');
      assert.equal(await o.locator('#spotify').isHidden(), true, 'no Spotify section without the daemon offering it');

      setup = { ...setup, onboarded:false };
      await o.reload();
      await ready(o);
      await o.waitForFunction(() => !document.getElementById('setup').hidden);
      await oRefresh();
      assert.deepEqual([await rowState('discord'), await rowState('media'), await rowState('card')], ['todo', 'todo', 'todo']);
      assert.match(await note('discord'), /Open the Discord desktop app/);
      assert.match(await note('media'), /Play something/);
      assert.equal(await o.locator('#app-id-form').isHidden(), true, 'Yozora’s own app ID needs no field');
      const setupBox = await o.locator('#setup').boundingBox();
      assert.ok(setupBox.y + setupBox.height <= 760, 'the checklist fits on the first screen');
      assert.ok(setupBox.height < 300, `the checklist stays compact (${setupBox.height}px)`);
      await oShot('setup-checklist-yozora-560');
      check('first-run checklist shows its three checks when setup is not complete');

      await o.locator('#test-card').click();
      await o.waitForFunction(() => document.getElementById('test-card').textContent === 'Sending…');
      assert.equal(testCardPosts.length, 1);
      assert.equal(await o.locator('#test-card').isDisabled(), true);
      assert.equal(await rowState('card'), 'busy');
      state = { ...state, discord_connected:true, sends:1, test_card:{ state:'shown', until:new Date(Date.now() + 15000).toISOString(), confirmed:false } };
      await oRefresh();
      assert.equal(await rowState('discord'), 'done');
      assert.equal(await note('discord'), '', 'a passing check says nothing');
      assert.equal(await rowState('card'), 'problem');
      assert.match(await note('card'), /Share my activity/, 'an unconfirmed card points at Discord’s own setting');
      assert.match(await o.locator('#test-card').textContent(), /^Test card up \(1[45] s\)$/);
      await oShot('setup-unconfirmed-yozora-560');
      state = { ...state, test_card:{ state:'done', confirmed:false } };
      await oRefresh();
      assert.equal(await o.locator('#test-card').isEnabled(), true, 'the button comes back once the card is withdrawn');
      assert.equal(await o.locator('#test-card').textContent(), 'Send test card');
      testCardHTTP = 500;
      await o.locator('#test-card').click();
      await oText('#setup-status', 'Could not send the test card');
      assert.equal(await o.locator('#test-card').isEnabled(), true);
      testCardHTTP = 202;
      state = { ...state, discord_connected:false, test_card:{ state:'failed', confirmed:false, error:'discordipc: no discord pipe found' } };
      await oRefresh();
      assert.equal(await rowState('discord'), 'problem');
      assert.match(await note('discord'), /didn’t answer/);
      assert.equal(await rowState('card'), 'problem');
      check('test card: sending, shown with countdown, unconfirmed hint, withdrawn, send failure and Discord failure');

      state = { ...state, last_error:'discordipc: handshake read: EOF', test_card:{ state:'failed', confirmed:false, error:'discordipc: handshake read: EOF' } };
      await oRefresh();
      assert.equal(await o.locator('#app-id-form').isVisible(), true, 'a refused handshake offers the application ID');
      await o.locator('#app-id').fill('12345');
      await o.locator('#app-id-save').click();
      await oText('#app-id-status', '17 to 20 digits');
      assert.equal(await o.locator('#app-id').getAttribute('aria-invalid'), 'true');
      assert.equal(setupPosts.length, 0, 'an invalid ID is not sent');
      await o.locator('#app-id').fill('112233445566778899');
      await o.locator('#app-id-save').click();
      await oText('#app-id-status', 'Saved');
      assert.deepEqual(setupPosts.at(-1), { client_id:'112233445566778899' });
      assert.equal(await o.locator('#app-id').getAttribute('aria-invalid'), null);
      await oShot('setup-app-id-yozora-560');
      check('Discord application ID entry appears only on a refused handshake and validates before saving');

      assert.equal(await o.locator('#setup-autostart').isChecked(), false);
      await o.locator('label[for=setup-autostart]').click();
      await o.waitForFunction(() => document.getElementById('autostart').checked && !document.getElementById('setup-autostart').disabled);
      assert.equal(autostart, true);
      autostartHTTP = 500;
      await o.locator('label[for=setup-autostart]').click();
      await oText('#setup-status', 'Could not change startup setting');
      assert.equal(await o.locator('#setup-autostart').isChecked(), true);
      assert.equal(await o.locator('#autostart').isChecked(), true);
      autostartHTTP = 200;
      check('the checklist’s startup switch saves and stays in step with the Startup section');

      state = { ...playingTrack, discord_connected:true, sends:3, last_error:'', test_card:{ state:'done', confirmed:true } };
      await oRefresh();
      assert.deepEqual([await rowState('discord'), await rowState('media'), await rowState('card')], ['done', 'done', 'done']);
      await oText('#setup-title', 'All set');
      assert.deepEqual(setupPosts.at(-1), { onboarded:true });
      assert.equal(setup.onboarded, true);
      await o.waitForFunction(() => document.getElementById('setup').hidden, null, { timeout:4000 });
      await o.reload();
      await ready(o);
      await o.waitForTimeout(300);
      assert.equal(await o.locator('#setup').isHidden(), true, 'a completed checklist stays away');
      check('all checks passing saves onboarded and collapses the checklist for good');

      await o.locator('.diagnostics summary').click();
      await o.locator('#setup-reopen').click();
      assert.equal(await o.locator('#setup').isVisible(), true);
      assert.equal(await o.evaluate(() => document.activeElement.id), 'test-card');
      await oRefresh();
      await o.waitForTimeout(2800);
      assert.equal(await o.locator('#setup').isVisible(), true, 'a reopened checklist waits for the user');
      const postsBefore = setupPosts.length;
      await o.locator('#setup-hide').click();
      await o.waitForFunction(() => document.getElementById('setup').hidden);
      assert.equal(setupPosts.length, postsBefore + 1);
      check('Diagnostics reopens the checklist and its close button hides it again');

      // Spotify connect.
      const spotifyState = () => o.locator('#spotify-state').textContent();
      state = { ...state, spotify:{ available:true, connected:false, waiting:false, client_id:'', redirect_uri:'http://127.0.0.1:50123/spotify/callback' } };
      await oRefresh();
      assert.equal(await o.locator('#spotify').isVisible(), true);
      assert.equal(await spotifyState(), 'Not connected');
      assert.equal(await o.locator('#spotify-redirect').textContent(), 'http://127.0.0.1:50123/spotify/callback');
      await o.locator('#spotify-id').fill('not-a-client-id');
      await o.locator('#spotify-connect').click();
      await oText('#spotify-status', '32 characters');
      assert.equal(await o.locator('#spotify-id').getAttribute('aria-invalid'), 'true');
      assert.equal(spotifyPosts.length, 0, 'an invalid client ID is not sent');
      await o.locator('#spotify-id').fill('');
      assert.equal(await o.locator('#spotify-status').textContent(), '', 'typing clears the validation message');
      const clientId = '0123456789ABCDEF0123456789ABCDEF';
      await o.locator('#spotify-id').fill(clientId);
      spotifyHTTP = 502;
      await o.locator('#spotify-connect').click();
      await oText('#spotify-status', 'Could not start Spotify sign-in');
      assert.equal(await o.locator('#spotify-connect').isEnabled(), true);
      spotifyHTTP = 200;
      await o.locator('#spotify-connect').click();
      await o.waitForFunction(() => !document.getElementById('spotify-waiting').hidden);
      assert.deepEqual(spotifyPosts.at(-1), { action:'connect', client_id:clientId.toLowerCase() });
      assert.equal(await o.locator('#spotify-status').textContent(), '');
      await oRefresh();
      assert.equal(await spotifyState(), 'Waiting for Spotify');
      assert.equal(await o.locator('#spotify-connect').isDisabled(), true);
      assert.equal(await o.evaluate(() => document.activeElement.id), 'spotify-cancel');
      await o.locator('#spotify').scrollIntoViewIfNeeded();
      await o.screenshot({ path:path.join(output, 'spotify-waiting-yozora-560.png') });
      await o.locator('#spotify-cancel').click();
      await o.waitForFunction(() => document.getElementById('spotify-waiting').hidden);
      assert.deepEqual(spotifyPosts.at(-1), { action:'cancel' });
      assert.equal(await o.locator('#spotify-connect').isEnabled(), true);
      state = { ...state, spotify:{ ...state.spotify, waiting:false, error:'spotify: authorization was not granted (access_denied)' } };
      await oRefresh();
      assert.match(await o.locator('#spotify-status').textContent(), /wasn’t connected: .*not granted/);
      state = { ...state, spotify:{ ...state.spotify, connected:true, waiting:false, error:'', client_id:clientId.toLowerCase() } };
      await oRefresh();
      assert.equal(await spotifyState(), 'Connected');
      assert.equal(await o.locator('#spotify-form').isHidden(), true);
      assert.equal(await o.locator('#spotify-client').textContent(), 'Client 012345…');
      assert.equal(await o.locator('#spotify-status').textContent(), '', 'a healthy connection says nothing');
      await o.locator('#spotify').scrollIntoViewIfNeeded();
      await o.screenshot({ path:path.join(output, 'spotify-connected-yozora-560.png') });
      state = { ...state, spotify:{ ...state.spotify, error:'spotify: token refresh: status 400' } };
      await oRefresh();
      assert.match(await o.locator('#spotify-status').textContent(), /isn’t answering/);
      await o.locator('#spotify-disconnect').click();
      await o.waitForFunction(() => !document.getElementById('spotify-form').hidden);
      assert.deepEqual(spotifyPosts.at(-1), { action:'disconnect' });
      assert.equal(await spotifyState(), 'Not connected');
      assert.equal(await o.evaluate(() => document.activeElement.id), 'spotify-id');
      check('Spotify connect: validation, browser failure, waiting, cancel, denial, connected, poller error and disconnect');

      // The same states in a light theme, and at phone width.
      await choose(o, 'theme', 'ledger', 'theme saved');
      setup = { ...setup, onboarded:false };
      state = { playing:false, title:'', discord_connected:true, sends:1, test_card:{ state:'shown', until:new Date(Date.now() + 9000).toISOString(), confirmed:false }, spotify:{ available:true, connected:true, waiting:false, client_id:'0123456789abcdef0123456789abcdef', redirect_uri:'http://127.0.0.1:50123/spotify/callback' } };
      await o.reload();
      await ready(o);
      await o.waitForFunction(() => !document.getElementById('setup').hidden);
      await oRefresh();
      await oShot('setup-and-spotify-ledger-560');
      state = { ...state, spotify:{ ...state.spotify, connected:false } };
      await oRefresh();
      await o.setViewportSize({ width:320, height:760 });
      assert.equal(await o.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, '320px must not overflow with the checklist and Spotify form');
      await oShot('setup-and-spotify-ledger-320');
      await o.setViewportSize({ width:560, height:760 });
      await choose(o, 'theme', 'yozora', 'theme saved');
      check('checklist and Spotify section in a light theme and at 320px');
      violations.push(...await o.evaluate(() => window.cspViolations));
      await sc.close();
      setup = { ...setup, onboarded:true };
      state = { ...playingTrack, discord_connected:true, sends:12 };
    }

    // Language, version, the config-restored notice, and Windows High Contrast.
    {
      const lc = await browser.newContext({ viewport:{ width:560, height:760 }, reducedMotion:'reduce', locale:'en-US' });
      await lc.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort());
      await lc.addInitScript(() => {
        window.cspViolations = [];
        document.addEventListener('securitypolicyviolation', event => window.cspViolations.push(event.violatedDirective));
      });
      const l = await lc.newPage();
      l.on('pageerror', error => errors.push(error.message));
      const lRefresh = () => l.evaluate(async () => { while (polling) await new Promise(resolve => setTimeout(resolve, 10)); await tick(); });
      const lShot = async name => { await l.evaluate(() => scrollTo(0, 0)); await l.screenshot({ path:path.join(output, `${name}.png`), fullPage:true }); };
      const text = selector => l.locator(selector).first().textContent();
      const playingTrack = { playing:true, activity_type:'listening', title:'Night Walk', artist:'Mira', album:'After Hours', service:'Spotify', source:'spotify', elapsed_sec:82, duration_sec:243, art_url:'/test-art.svg', small_image:'/test-badge.svg', discord_connected:true, sends:4 };
      state = { ...playingTrack };
      await l.goto(`${origin}/?t=synthetic`);
      await ready(l);
      await lRefresh();

      assert.equal(await text('#version-footer'), '· dev build');
      assert.equal(await l.locator('#version-meta').evaluate(node => node.textContent), 'Version dev build');
      version = '0.1.42';
      await l.reload();
      await ready(l);
      assert.equal(await text('#version-footer'), '· v0.1.42');
      assert.equal(await l.locator('#version-meta').evaluate(node => node.textContent), 'Version 0.1.42');
      check('the running version shows in the footer and Diagnostics, and a dev build says so');

      const brokenPath = 'C:\\Users\\mira\\AppData\\Roaming\\uika-resonance\\cadence.toml.broken-20261112-101500';
      state = { ...playingTrack, config_restored:{ at:'2026-11-12T10:15:00Z', broken_path:brokenPath, error:'toml: line 3: expected value' } };
      await lRefresh();
      assert.equal(await l.locator('#restored-note').isVisible(), true, 'a restored config earns a line');
      assert.match(await text('#restored-text'), /previous settings were restored\. Your edited file was saved as cadence\.toml\.broken-20261112-101500\./);
      assert.equal(await l.locator('#restored-text').getAttribute('title'), `${brokenPath}\ntoml: line 3: expected value`);
      assert.equal(await l.locator('#preview-note').isHidden(), true, 'the restored line does not borrow the connection note');
      await l.screenshot({ path:path.join(output, 'restored-notice-560.png') });
      await l.locator('#restored-dismiss').click();
      assert.equal(await l.locator('#restored-note').isHidden(), true);
      await lRefresh();
      assert.equal(await l.locator('#restored-note').isHidden(), true, 'a dismissed notice stays away while the panel polls');
      await l.reload();
      await ready(l);
      await lRefresh();
      assert.equal(await l.locator('#restored-note').isHidden(), true, 'a dismissed notice stays away after a reload');
      state = { ...state, config_restored:{ ...state.config_restored, at:'2026-11-13T08:00:00Z' } };
      await lRefresh();
      assert.equal(await l.locator('#restored-note').isVisible(), true, 'a later restore shows again');
      check('the config-restored notice names the file set aside, and dismisses for that restore');

      // Recent tracks and the copyable report, in English.
      const minutesAgo = n => new Date(Date.now() - n * 60000).toISOString();
      recent = [
        { title:'Night Walk', artist:'Mira', service:'Spotify', art_url:'/test-art.svg', at:minutesAgo(0) },
        { title:'Glass Rain', artist:'Ode', service:'Apple Music', art_url:'/missing-art.png', at:minutesAgo(7) },
        { title:'A very long title that has to be cut short before it pushes the clock off the card', artist:'Someone', service:'YouTube Music', at:minutesAgo(190) },
      ];
      await l.evaluate(() => { window.copied = null; Object.defineProperty(navigator, 'clipboard', { configurable:true, value:{ writeText: async text => { window.copied = text; } } }); });
      await l.locator('.diagnostics summary').click();
      assert.equal(await l.locator('#recent-list').isHidden(), true, 'Recent starts collapsed');
      const getsBefore = recentGets.length;
      await l.locator('#recent summary').click();
      await l.waitForFunction(() => document.querySelectorAll('#recent-list li').length === 3);
      assert.equal(recentGets.length, getsBefore + 1, 'opening Recent loads it once');
      assert.deepEqual(await l.locator('#recent-list li b').allTextContents(), recent.map(r => r.title));
      assert.equal(await l.locator('#recent-list li').first().locator('span').textContent(), 'Mira · Spotify');
      assert.deepEqual(await l.locator('#recent-list time').allTextContents(), ['just now', '7 minutes ago', '3 hours ago']);
      assert.equal(await l.locator('#recent-list img').count(), 1, 'a track without working art gets a plain tile, not a broken image');
      assert.equal(await l.locator('#recent-list .no-art').count(), 2);
      assert.equal(await l.evaluate(() => document.getElementById('recent-list').querySelector('img').naturalWidth > 0), true, 'the thumbnail loads');
      await l.setViewportSize({ width:320, height:760 });
      assert.equal(await l.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'Recent fits 320px');
      await l.locator('#recent').scrollIntoViewIfNeeded();
      await l.screenshot({ path:path.join(output, 'recent-320.png'), fullPage:true });
      await l.setViewportSize({ width:560, height:760 });
      const getsOpen = recentGets.length;
      state = { ...state, title:'Another Song' };
      await lRefresh();
      await l.waitForTimeout(150);
      assert.equal(recentGets.length, getsOpen + 1, 'a track change reloads an open Recent list');
      state = { ...state, title:'Night Walk' };
      await lRefresh();
      await l.waitForTimeout(150);
      recent = [];
      await l.locator('#recent summary').click();
      await l.locator('#recent summary').click();
      await l.waitForFunction(() => document.getElementById('recent-status').textContent.includes('Nothing has been shown'));
      assert.equal(await l.locator('#recent-list li').count(), 0);
      recentHTTP = 500;
      await l.locator('#recent summary').click();
      await l.locator('#recent summary').click();
      await l.waitForFunction(() => document.getElementById('recent-status').textContent.includes('Could not load recent tracks: Error: HTTP 500'));
      recentHTTP = 200;
      await l.locator('#diag-copy').click();
      await l.waitForFunction(() => window.copied !== null);
      const copied = await l.evaluate(() => window.copied);
      console.log('copied diagnostics (English page):\n' + copied);
      assert.equal(copied, diagnosticsReport);
      assert.doesNotMatch(copied, /Night Walk|Mira|After Hours|[A-Za-z]:\\|\d{15,}|token/i, 'the report carries no track text, paths, IDs or tokens');
      assert.equal(await l.locator('#diag-status').textContent(), 'Report copied.');
      assert.equal(await l.locator('#diag-text').isHidden(), true);
      await l.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable:true, value:{ writeText: async () => { throw new Error('denied'); } } }));
      await l.locator('#diag-copy').click();
      await l.waitForFunction(() => !document.getElementById('diag-text').hidden);
      assert.equal(await l.locator('#diag-text').inputValue(), diagnosticsReport, 'without clipboard access the text is shown to select');
      assert.equal(await l.locator('#diag-status').textContent(), 'Could not copy. Select the text below.');
      await l.evaluate(() => { Object.defineProperty(navigator, 'clipboard', { configurable:true, value:{ writeText: async text => { window.copied = text; } } }); });
      await l.screenshot({ path:path.join(output, 'diagnostics-560.png'), fullPage:true });
      check('Recent lists the last tracks with a thumbnail, title, artist and relative time, and Copy report puts the plain-text report on the clipboard');

      // The update line: shown for an available release, dismissed per version, and following an install.
      const pollUpdate = p => p.evaluate(async () => { while (updateBusy) await new Promise(resolve => setTimeout(resolve, 10)); await pollUpdate(); });
      const releasePage = 'https://github.com/mariyaholic/yozora/releases/tag/v0.1.43';
      await pollUpdate(l);
      assert.equal(await l.locator('#update-note').isHidden(), true, 'no update, no line');
      updateState = { ...noUpdate, latest:'0.1.43', available:true, url:releasePage };
      await pollUpdate(l);
      assert.equal(await l.locator('#update-note').isVisible(), true, 'an available update earns a line');
      assert.equal(await text('#update-text'), 'Yozora 0.1.43 is available');
      assert.equal(await text('#update-now'), 'Update now');
      assert.equal(await l.locator('#update-notes').getAttribute('href'), releasePage);
      assert.equal(await l.locator('#update-notes').getAttribute('target'), '_blank');
      assert.equal(await l.locator('#update-note').evaluate(node => node.getBoundingClientRect().top < document.querySelector('#preview').getBoundingClientRect().top), true, 'the line sits above the preview');
      assert.equal(updatePosts.length, 0, 'showing the line installs nothing');
      await lShot('update-available-560');
      await l.setViewportSize({ width:320, height:760 });
      assert.equal(await l.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'the update line fits 320px');
      await lShot('update-available-320');
      await l.setViewportSize({ width:560, height:760 });
      await l.locator('#update-dismiss').click();
      assert.equal(await l.locator('#update-note').isHidden(), true);
      await pollUpdate(l);
      assert.equal(await l.locator('#update-note').isHidden(), true, 'a dismissed version stays away while the panel polls');
      await l.reload();
      await ready(l);
      await pollUpdate(l);
      assert.equal(await l.locator('#update-note').isHidden(), true, 'a dismissed version stays away after a reload');
      updateState = { ...updateState, latest:'0.1.44', url:releasePage.replace('0.1.43', '0.1.44') };
      await pollUpdate(l);
      assert.equal(await text('#update-text'), 'Yozora 0.1.44 is available', 'a newer release shows again');
      check('the update line names the version, links the release, and dismisses per version');

      await l.locator('#update-now').click();
      await l.waitForFunction(() => document.getElementById('update-text').textContent.startsWith('Downloading'));
      assert.equal(updatePosts.length, 1, 'Update now posts once');
      assert.equal(await text('#update-text'), 'Downloading Yozora 0.1.44…');
      assert.equal(await l.locator('#update-now').isHidden(), true, 'no second install button while one runs');
      assert.equal(await l.locator('#update-dismiss').isHidden(), true);
      await lShot('update-downloading-560');
      updateState = { ...updateState, install:{ state:'verifying' } };
      await pollUpdate(l);
      assert.equal(await text('#update-text'), 'Checking the download…');
      updateState = { ...updateState, install:{ state:'installing' } };
      await pollUpdate(l);
      assert.equal(await text('#update-text'), 'Installing Yozora 0.1.44. Yozora restarts when it is done.');
      await l.reload();
      await ready(l);
      await pollUpdate(l);
      assert.equal(await l.locator('#update-note').isVisible(), true, 'progress shows even for a dismissed version');
      updateState = { ...updateState, install:{ state:'error', error:'checksum mismatch for Yozora-Setup.exe' } };
      await pollUpdate(l);
      assert.equal(await text('#update-text'), 'Could not update: checksum mismatch for Yozora-Setup.exe');
      assert.equal(await l.locator('#update-note').getAttribute('data-state'), 'error');
      assert.equal(await text('#update-now'), 'Try again');
      await lShot('update-error-560');
      await l.locator('#update-now').click();
      await l.waitForFunction(() => document.getElementById('update-text').textContent.startsWith('Downloading'));
      assert.equal(updatePosts.length, 2, 'Try again posts again');
      updateInstallHTTP = 409;
      updateState = { ...updateState, install:{ state:'idle' } };
      await pollUpdate(l);
      await l.locator('#update-now').click();
      await l.waitForFunction(() => document.getElementById('update-note').dataset.state === 'error');
      assert.equal(await text('#update-text'), 'Could not update: an update is already in progress');
      updateInstallHTTP = 202;
      updateState = { ...updateState, install:{ state:'idle' } };
      await pollUpdate(l);
      check('Update now posts once, shows downloading, checking and installing, and reports errors with Try again');

      // Japanese, chosen in the Appearance dialog.
      state = { ...state, discord_connected:false };
      await choose(l, 'language', 'ja', '言語（日本語）を保存しました');
      assert.deepEqual(themePosts.at(-1), { language:'ja' }, 'a language change posts only the language');
      assert.deepEqual(await l.evaluate(() => [document.documentElement.lang, document.documentElement.dataset.language, document.title]), ['ja', 'ja', 'Yozora コントロールパネル']);
      await lRefresh();
      assert.equal(await text('#playback-state'), '再生中');
      assert.equal(await text('#head'), 'Spotify で再生中');
      assert.equal(await text('#source-save'), '変更を適用');
      assert.equal(await text('#version-footer'), '· v0.1.42');
      assert.equal(await l.locator('#version-meta').evaluate(node => node.textContent), 'バージョン 0.1.42');
      assert.match(await text('#restored-text'), /前回の設定に戻しました。編集したファイルは cadence\.toml\.broken-20261112-101500 として/);
      assert.equal(await text('#preview-note'), 'Discord が起動していないため、まだプロフィールに表示されていません。');
      recent = [{ title:'夜道', artist:'ミラ', service:'Spotify', art_url:'/test-art.svg', at:minutesAgo(7) }, { title:'ガラスの雨', artist:'オード', service:'Apple Music', at:minutesAgo(190) }];
      // The update checks above reload the page: the clipboard stub and the open
      // sections are gone, so set them up again.
      await l.evaluate(() => { window.copied = null; Object.defineProperty(navigator, 'clipboard', { configurable:true, value:{ writeText: async text => { window.copied = text; } } }); });
      if (await l.locator('#recent').evaluate(node => node.open)) await l.locator('#recent summary').click();
      await l.locator('#recent summary').click();
      await l.waitForFunction(() => document.querySelectorAll('#recent-list li').length === 2);
      assert.equal(await text('#recent summary'), '最近の再生');
      assert.deepEqual(await l.locator('#recent-list time').allTextContents(), ['7 分前', '3 時間前']);
      if (!await l.locator('.diagnostics').evaluate(node => node.open)) await l.locator('.diagnostics summary').click();
      assert.equal(await text('#diag-copy'), 'レポートをコピー');
      await l.locator('#diag-copy').click();
      await l.waitForFunction(() => window.copied !== null);
      assert.equal(await text('#diag-status'), 'レポートをコピーしました。');
      assert.equal(await l.evaluate(() => window.copied), diagnosticsReport);
      await l.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable:true, value:{ writeText: async () => { throw new Error('denied'); } } }));
      await l.locator('#diag-copy').click();
      await l.waitForFunction(() => document.getElementById('diag-status').textContent.startsWith('コピーできませんでした'));
      await l.locator('#recent').scrollIntoViewIfNeeded();
      await l.screenshot({ path:path.join(output, 'recent-ja-560.png'), fullPage:true });
      recent = [];
      await l.locator('#recent summary').click();
      await l.locator('#recent summary').click();
      await l.waitForFunction(() => document.getElementById('recent-status').textContent === 'まだ Discord に表示したトラックはありません。');
      check('Recent and Copy report read naturally in Japanese');
      assert.match(await l.locator('#source-order li').first().getAttribute('aria-label'), /^優先順位 1: /);
      assert.equal(await l.locator('#source-order li[data-source="spotify"] .drag-handle').getAttribute('aria-label'), 'Spotify（デスクトップ） を並べ替え');
      assert.equal(await l.locator('#appearance-open').getAttribute('aria-label'), '外観');
      assert.equal(await l.locator('#spotify-id').getAttribute('placeholder'), 'Spotify の開発者アプリから取得');
      // Only names stay in Latin script; anything else left in a key region is untranslated English.
      const names = /[A-Z]:\\\S+|cadence\.toml[\w.-]*|toml: [^\n]*|Yozora|Discord|Spotify Web API|Web API|Spotify|YouTube Music|YouTube|Twitch|Apple Music|Windows|Edge|Chrome|Mariyaholic-98|Uika|Pine|Kurisu|Ava|Astra|Sumimi|Ledger|Confluence|Maria|Aa|ID|URI|UI|PWA|VOD|msedge|Esc|dev|v\d[\d.]*/g;
      const regions = ['.masthead', '.preview-label', '#preview-note', '#restored-note', '#sources-form', '.startup', '.made-by', '#appearance .dialog-head', '#theme-fields > legend', '#appearance .motion-row', '#language-title'];
      for (const selector of regions) {
        const found = await l.locator(selector).first().evaluate((node, source) => {
          const labels = [...node.querySelectorAll('[aria-label],[title],[placeholder]')].flatMap(el => ['aria-label', 'title', 'placeholder'].map(a => el.getAttribute(a) || ''));
          const all = [node.innerText, node.textContent, node.getAttribute('aria-label') || '', ...labels].join(' ');
          return all.replace(new RegExp(source, 'gi'), ' ').match(/[A-Za-z]{2,}/g) || [];
        }, names.source);
        assert.deepEqual(found, [], `${selector} still has English in Japanese`);
      }
      check('Japanese: the picker saves only the language, and key regions, labels and live text are translated');

      assert.equal(await text('#update-text'), 'Yozora 0.1.44 を利用できます', 'a language change redraws the line at once');
      updateState = { ...updateState, install:{ state:'idle' } };
      await pollUpdate(l);
      assert.equal(await text('#update-text'), 'Yozora 0.1.44 を利用できます');
      assert.equal(await text('#update-now'), '今すぐ更新');
      assert.equal(await text('#update-notes'), '更新内容');
      assert.equal(await l.locator('#update-dismiss').getAttribute('aria-label'), '閉じる');
      for (const [phase, want] of [['downloading', 'Yozora 0.1.44 をダウンロードしています…'], ['verifying', 'ダウンロードを確認しています…'], ['installing', 'Yozora 0.1.44 をインストールしています。完了すると Yozora は自動で起動し直します。']]) {
        updateState = { ...updateState, install:{ state:phase } };
        await pollUpdate(l);
        assert.equal(await text('#update-text'), want);
      }
      updateState = { ...updateState, install:{ state:'error', error:'x' } };
      await pollUpdate(l);
      assert.equal(await text('#update-text'), '更新できませんでした: x');
      assert.equal(await text('#update-now'), 'もう一度試す');
      assert.equal(await l.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
      await lShot('update-error-ja-560');
      updateState = { ...noUpdate };
      await pollUpdate(l);
      assert.equal(await l.locator('#update-note').isHidden(), true);
      check('Japanese: the update line, its buttons and every install state are translated');

      // The pause control and line in Japanese.
      state = { ...state, paused:true, paused_until:'2026-11-12T15:30:00' };
      await lRefresh();
      assert.equal(await text('#pause-text'), '15:30 まで共有を一時停止中');
      assert.equal(await text('#pause-resume'), '再開');
      await lShot('ja-paused-560');
      state = { ...state, paused_until:undefined };
      await lRefresh();
      assert.equal(await text('#pause-text'), '共有を一時停止中');
      const { paused, paused_until, ...unpaused } = state;
      state = unpaused;
      await lRefresh();
      assert.equal(await text('#pause-open'), '共有を一時停止');
      await l.locator('#pause-open').click();
      assert.deepEqual(await l.locator('#pause-choices button').allTextContents(), ['1 時間', '再開するまで']);
      assert.equal(await l.locator('#pause-choices').getAttribute('aria-label'), '共有を一時停止');
      await l.locator('#pause-open').click();
      check('Japanese: the pause control, its choices, and the paused line with and without an end time');

      const ja320 = [];
      for (const id of ['yozora', 'ledger', 'mariyaholic-98']) {
        if (id !== 'yozora') await choose(l, 'theme', id, 'を保存しました');
        setup = { ...setup, onboarded:false };
        state = { ...state, discord_connected:true, test_card:{ state:'failed', confirmed:false, error:'discordipc: no discord pipe found' }, spotify:{ available:true, connected:false, waiting:false, client_id:'', redirect_uri:'http://127.0.0.1:50123/spotify/callback' } };
        await l.reload();
        await ready(l);
        await l.waitForFunction(() => !document.getElementById('setup').hidden);
        await lRefresh();
        await l.evaluate(() => document.fonts.ready);
        assert.equal(await text('#setup-title'), 'はじめに');
        assert.equal(await text('#spotify-state'), '未接続');
        await lShot(`ja-${id}-560`);
        await l.setViewportSize({ width:320, height:760 });
        ja320.push([id, await l.evaluate(() => document.documentElement.scrollWidth <= innerWidth)]);
        // Phrase breaking must not push a label under its row's arrow buttons.
        const clashes = await l.locator('#source-order li').evaluateAll(rows => rows.filter(row => row.querySelector('.source-option > span').getBoundingClientRect().right > row.querySelector('.move').getBoundingClientRect().left + 0.5).map(row => row.dataset.source));
        assert.deepEqual(clashes, [], `${id}: source labels overlap their arrows at 320px`);
        await lShot(`ja-${id}-320`);
        await openAppearance(l);
        assert.equal(await l.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `${id}: the Japanese Appearance dialog fits 320px`);
        await l.screenshot({ path:path.join(output, `ja-${id}-appearance-320.png`) });
        await closeAppearance(l);
        await l.setViewportSize({ width:560, height:760 });
      }
      assert.deepEqual(ja320.filter(([, fits]) => !fits), [], 'Japanese must not overflow at 320px');
      setup = { ...setup, onboarded:true };
      state = { ...playingTrack };
      check('Japanese at 320px in a dark, a light and the PC-98 theme has no horizontal overflow');

      themeHTTP = 500;
      await choose(l, 'language', 'en', '言語を保存できませんでした');
      assert.equal(await l.evaluate(() => document.documentElement.lang), 'ja', 'a failed save keeps the saved language');
      assert.equal(await l.locator('input[name="language"][value="ja"]').isChecked(), true);
      themeHTTP = 200;
      await choose(l, 'language', 'en', 'Language (English) saved');
      assert.deepEqual(themePosts.at(-1), { language:'en' });
      assert.equal(await l.evaluate(() => document.documentElement.lang), 'en');
      await lRefresh();
      assert.equal(await text('#source-save'), 'Apply changes');
      assert.match(await l.locator('#source-order li').first().getAttribute('aria-label'), /^Priority 1: /);
      await choose(l, 'language', 'system', 'Language (Follow Windows) saved');
      assert.deepEqual(themePosts.at(-1), { language:'system' });
      assert.equal(await l.evaluate(() => document.documentElement.lang), 'en', 'Follow Windows in an English browser is English');
      await choose(l, 'theme', 'yozora', 'theme saved');
      violations.push(...await l.evaluate(() => window.cspViolations));
      await lc.close();
      check('switching back to English and to Follow Windows posts each choice; a failed save keeps Japanese');

      // Follow Windows in a Japanese browser is Japanese from the first paint.
      const jc = await browser.newContext({ viewport:{ width:560, height:760 }, reducedMotion:'reduce', locale:'ja-JP' });
      await jc.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort());
      const j = await jc.newPage();
      j.on('pageerror', error => errors.push(error.message));
      const served = await (await j.request.get(`${origin}/?t=synthetic`, { headers:{ 'Accept-Language':'ja-JP,ja;q=0.9' } })).text();
      assert.match(served, /<html lang="ja" [^>]*data-language="system"/, 'the server draws Follow Windows in the browser language');
      await j.goto(`${origin}/?t=synthetic`);
      await ready(j);
      assert.deepEqual(await j.evaluate(() => [document.documentElement.lang, document.getElementById('source-save').textContent]), ['ja', '変更を適用']);
      await jc.close();
      check('Follow Windows picks Japanese from a Japanese browser');

      // Windows High Contrast.
      const fc = await browser.newContext({ viewport:{ width:560, height:760 }, reducedMotion:'reduce', forcedColors:'active', locale:'en-US' });
      await fc.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort());
      const f = await fc.newPage();
      f.on('pageerror', error => errors.push(error.message));
      setup = { ...setup, onboarded:false };
      state = { ...playingTrack, discord_connected:true, test_card:{ state:'failed', confirmed:false } };
      await f.goto(`${origin}/?t=synthetic`);
      await ready(f);
      await f.waitForFunction(() => !document.getElementById('setup').hidden);
      await f.evaluate(async () => { while (polling) await new Promise(resolve => setTimeout(resolve, 10)); await tick(); });
      assert.equal(await f.evaluate(() => matchMedia('(forced-colors: active)').matches), true);
      const transparent = color => color === 'transparent' || /rgba\(.*,\s*0\)$/.test(color);
      const ring = node => { const style = getComputedStyle(node); return [style.outlineStyle, style.outlineWidth, style.outlineColor]; };
      await f.keyboard.press('Tab');
      assert.equal(await f.evaluate(() => document.activeElement.id), 'appearance-open');
      let [style, width, color] = await f.locator('#appearance-open').evaluate(ring);
      assert.ok(style === 'solid' && width === '2px' && !transparent(color), `button focus ring: ${style} ${width} ${color}`);
      await f.evaluate(() => document.getElementById('source-applemusic').focus());
      [style, width, color] = await f.locator('label[for=source-applemusic]').evaluate(ring);
      assert.ok(style === 'solid' && !transparent(color), `checkbox row focus ring: ${style} ${color}`);
      const rowEdge = await f.locator('#source-order li:has(input:checked)').first().evaluate(node => [getComputedStyle(node).borderTopWidth, getComputedStyle(node).borderTopColor]);
      assert.ok(rowEdge[0] === '2px' && !transparent(rowEdge[1]), `a checked source row keeps a heavy edge: ${rowEdge}`);
      const marks = await f.evaluate(() => ['discord', 'card'].map(id => { const s = getComputedStyle(document.getElementById(`check-${id}`), '::before'); return [s.content, s.backgroundColor, s.borderTopColor]; }));
      assert.ok(marks[0][0] === '"✓"' && !transparent(marks[0][1]), `a passed check is filled: ${marks[0]}`);
      assert.ok(marks[1][0] === '"!"' && !transparent(marks[1][2]), `a problem check is marked: ${marks[1]}`);
      state = { ...state, paused:true, paused_until:'2026-11-12T15:30:00' };
      await f.evaluate(async () => { while (polling) await new Promise(resolve => setTimeout(resolve, 10)); await tick(); });
      const pauseEdge = await f.locator('#pause-line').evaluate(node => [getComputedStyle(node).borderTopWidth, getComputedStyle(node).borderTopColor]);
      assert.ok(pauseEdge[0] === '2px' && !transparent(pauseEdge[1]), `the paused line keeps a heavy edge: ${pauseEdge}`);
      const { paused: pausedOnce, paused_until: pausedUntilOnce, ...notPaused } = state;
      state = notPaused;
      await f.evaluate(async () => { while (polling) await new Promise(resolve => setTimeout(resolve, 10)); await tick(); });
      await f.locator('#pause-open').click();
      const openEdge = await f.locator('#pause-open').evaluate(node => [getComputedStyle(node).borderTopWidth, getComputedStyle(node).borderTopColor]);
      assert.ok(openEdge[0] === '2px' && !transparent(openEdge[1]), `the open pause control is marked: ${openEdge}`);
      await f.locator('#pause-open').click();
      await f.evaluate(() => scrollTo(0, 0));
      await f.screenshot({ path:path.join(output, 'forced-colors-560.png'), fullPage:true });

      await f.locator('#source-order').evaluate(list => list.scrollIntoView({ block:'center' }));
      const grip = await f.locator('#source-order li').first().locator('.drag-handle').boundingBox();
      const last = await f.locator('#source-order li').last().boundingBox();
      await f.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2);
      await f.mouse.down();
      await f.mouse.move(grip.x + grip.width / 2, last.y + last.height - 8, { steps:10 });
      const line = await f.locator('.drop-after').evaluate(node => getComputedStyle(node, '::after').backgroundColor);
      const preview = await f.locator('.drag-preview').evaluate(node => [getComputedStyle(node).borderTopColor, getComputedStyle(node).borderTopWidth]);
      assert.ok(!transparent(line), `the drop line is drawn: ${line}`);
      assert.ok(!transparent(preview[0]) && preview[1] === '2px', `the drop preview is outlined: ${preview}`);
      await f.screenshot({ path:path.join(output, 'forced-colors-drag-560.png') });
      await f.keyboard.press('Escape');
      await f.mouse.up();

      await f.evaluate(() => scrollTo(0, 0));
      await f.locator('#appearance-open').click();
      const tiles = await f.evaluate(() => {
        const edge = node => [getComputedStyle(node).borderTopWidth, getComputedStyle(node).borderTopColor];
        const on = document.querySelector('.theme-option:has(input:checked)');
        const off = document.querySelector('.theme-option:not(:has(input:checked))');
        const seg = document.querySelector('.segmented label:has(input:checked)');
        const segOff = document.querySelector('.segmented label:not(:has(input:checked))');
        return { on:edge(on), off:edge(off), seg:getComputedStyle(seg).backgroundColor, segOff:getComputedStyle(segOff).backgroundColor };
      });
      assert.ok(tiles.on[0] === '2px' && !transparent(tiles.on[1]) && tiles.on[1] !== tiles.off[1], `the checked theme tile stands out: ${JSON.stringify(tiles)}`);
      assert.ok(!transparent(tiles.seg) && tiles.seg !== tiles.segOff, `the chosen segment is filled: ${JSON.stringify(tiles)}`);
      updateState = { ...noUpdate, latest:'0.1.43', available:true, url:'https://github.com/mariyaholic/yozora/releases/tag/v0.1.43', install:{ state:'error', error:'checksum mismatch' } };
      await f.evaluate(async () => { while (updateBusy) await new Promise(resolve => setTimeout(resolve, 10)); await pollUpdate(); });
      const note = await f.evaluate(() => { const n = document.getElementById('update-note'); const e = getComputedStyle(document.getElementById('update-text')); const b = getComputedStyle(n); return { border:[b.borderTopStyle, b.borderTopWidth], err:[e.borderInlineStartStyle, e.borderInlineStartWidth], shown:!n.hidden }; });
      assert.deepEqual(note, { border:['solid', '1px'], err:['solid', '3px'], shown:true }, 'forced colours keep the update line and its error edge visible');
      await f.screenshot({ path:path.join(output, 'forced-colors-update-560.png') });
      updateState = { ...noUpdate };
      await f.keyboard.press('Tab');
      await f.keyboard.press('Shift+Tab');
      const tileRing = await f.evaluate(() => { const s = getComputedStyle(document.querySelector('.theme-option:has(input:focus-visible)')); return [s.outlineStyle, s.outlineColor]; });
      assert.ok(tileRing[0] === 'solid' && !transparent(tileRing[1]), `theme tile focus ring: ${tileRing}`);
      await f.screenshot({ path:path.join(output, 'forced-colors-appearance-560.png') });
      await f.keyboard.press('Escape');
      await f.setViewportSize({ width:320, height:760 });
      assert.equal(await f.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'forced colours fit 320px');
      await fc.close();
      setup = { ...setup, onboarded:true };
      state = { ...playingTrack };
      version = 'dev';
      check('forced colours keep focus rings, checked rows and tiles, the chosen segment, checks and the drop line visible');
    }

    // Discord card form, live preview, hidden rules and the hide button, in both languages.
    const cardTrack = { playing:true, activity_type:'listening', title:'Night Walk', artist:'Mira', album:'After Hours', service:'Spotify', source:'spotify', elapsed_sec:82, duration_sec:243, discord_connected:true, art_url:'/test-art.svg', small_image:'/test-badge.svg' };
    for (const lang of ['en', 'ja']) {
      const ja = lang === 'ja';
      const words = ja
        ? { saved: '保存しました', unsaved: '未保存', hiddenMark: 'Discord には表示されません', hideSong: 'この曲を隠す', invalid: 'まだ適用できません', reset: '初期設定を入力しました', removeLabel: 'の非表示をやめる', sample: 'サンプルの曲' }
        : { saved: 'Saved', unsaved: 'Unsaved', hiddenMark: 'Hidden from Discord', hideSong: 'Hide this song', invalid: 'Cannot apply yet', reset: 'Defaults filled in', removeLabel: 'Stop hiding', sample: 'sample track' };
      language = lang;
      card = { ...cardDefaults }; hiddenRules = []; state = { ...cardTrack };
      cardPosts.length = 0; hiddenPosts.length = 0; previewPosts.length = 0;
      const cc = await browser.newContext({ viewport: { width: 560, height: 760 }, reducedMotion: 'reduce' });
      await cc.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort());
      await cc.addInitScript(() => { window.cspViolations = []; document.addEventListener('securitypolicyviolation', event => window.cspViolations.push(event.violatedDirective)); });
      const c = await cc.newPage();
      c.on('pageerror', error => errors.push(error.message));
      await c.goto(`${origin}/?t=synthetic`);
      await ready(c);
      await c.waitForFunction(() => !document.getElementById('card-fields').disabled && document.getElementById('cf-r-details').textContent !== '');
      const val = id => c.locator(`#cf-${id}`).inputValue();
      const text = sel => c.locator(sel).textContent();
      assert.equal(await val('details'), '{{.Title}}');
      assert.equal(await text('#cf-r-details'), 'Night Walk');
      assert.equal(await text('#cf-r-state'), 'Mira');
      assert.equal(await c.locator('#card-save').isDisabled(), true);
      assert.equal(await c.locator('#card-discard').isHidden(), true);
      assert.equal(await c.locator('#card-status').textContent(), '', 'a clean card form says nothing');
      assert.equal(await c.locator('#card-placeholders').evaluate(node => node.open), false, 'placeholders are on demand');
      assert.equal(await c.locator('#card-hidden').isHidden(), true, 'no rules, no hidden list');
      // Section order: Sources, then the Discord card, then Spotify / Startup.
      assert.deepEqual(await c.evaluate(() => ['#sources-form', '#card-form', '.startup'].map(sel => document.querySelector(sel).getBoundingClientRect().top + scrollY)).then(tops => tops.every((top, i) => !i || top > tops[i - 1])), true);

      // Editing: the preview follows, Apply and Discard appear, Discard restores.
      await c.locator('#cf-details').fill('{{.Title}} by {{.Artist}}');
      await c.waitForFunction(() => document.getElementById('cf-r-details').textContent === 'Night Walk by Mira');
      assert.equal(await c.locator('#card-save').isEnabled(), true);
      assert.equal(await c.locator('#card-discard').isVisible(), true);
      assert.ok((await text('#card-status')).includes(words.unsaved));
      await c.locator('#card-discard').click();
      assert.equal(await val('details'), '{{.Title}}');
      await c.waitForFunction(() => document.getElementById('cf-r-details').textContent === 'Night Walk');
      assert.equal(await c.locator('#card-save').isDisabled(), true);

      // A template that does not parse blocks Apply and says why; fixing it frees Apply.
      await c.locator('#cf-state').fill('{{.Artist');
      await c.waitForFunction(word => document.getElementById('card-status').textContent.includes(word), words.invalid);
      assert.equal(await c.locator('#card-save').isDisabled(), true);
      await c.locator('#cf-state').fill('{{.Artist}} / {{.Album}}');
      await c.waitForFunction(() => document.getElementById('cf-r-state').textContent === 'Mira / After Hours');
      assert.equal(await c.locator('#card-save').isEnabled(), true);

      // Apply posts the whole form, and the saved state becomes the new baseline.
      await c.locator('#cf-paused_mode').selectOption('hide');
      await c.locator('#cf-type').selectOption('watching');
      await c.locator('#cf-status_display').selectOption('details');
      await c.locator('#cf-buttons_enabled').uncheck();
      await c.locator('#cf-button_label').fill('Hear it');
      await c.locator('#cf-spotify_search_fallback').uncheck();
      await c.locator('#cf-small_text').fill('');
      await c.locator('#card-save').click();
      await c.waitForFunction(word => document.getElementById('card-status').textContent.includes(word), words.saved);
      assert.deepEqual(cardPosts.at(-1), { paused_mode: 'hide', type: 'watching', status_display: 'details', buttons_enabled: false, button_label: 'Hear it', spotify_search_fallback: false, details: '{{.Title}}', state: '{{.Artist}} / {{.Album}}', large_text: '{{.Album}}', small_text: '', paused_details: '⏸ {{.Title}}' });
      assert.equal(await c.locator('#card-save').isDisabled(), true);
      assert.equal(await c.locator('#card-discard').isHidden(), true);
      await shot(`card-${lang}-560`);

      // Reset fills the defaults but only Apply saves them.
      await c.locator('#card-reset').click();
      assert.equal(await val('state'), '{{.Artist}}');
      assert.equal(await c.locator('#cf-paused_mode').inputValue(), 'show');
      assert.equal(await c.locator('#cf-buttons_enabled').isChecked(), true);
      assert.ok((await text('#card-status')).includes(words.reset));
      assert.equal(cardPosts.length, 1, 'reset alone saves nothing');
      await c.locator('#card-save').click();
      await c.waitForFunction(word => document.getElementById('card-status').textContent.includes(word), words.saved);
      assert.deepEqual(cardPosts.at(-1), cardDefaults);

      // A paused track and a hidden track are said in the card section.
      await c.locator('#cf-paused_mode').selectOption('hide');
      state = { ...state, playing: false };
      await c.evaluate(() => refreshSoon());
      await c.waitForFunction(() => document.getElementById('card-note').textContent !== '');
      await c.locator('#cf-paused_mode').selectOption('show');
      await c.waitForFunction(() => document.getElementById('card-note').textContent === '');
      state = { ...state, playing: true };

      // Hiding from the preview: song, then artist, then removal from the list.
      assert.equal(await c.locator('#hide-menu').isHidden(), true);
      await c.locator('#hide-open').click();
      assert.equal(await c.locator('#hide-open').getAttribute('aria-expanded'), 'true');
      assert.equal(await c.locator('#hide-song').textContent(), words.hideSong);
      await c.keyboard.press('Escape');
      assert.equal(await c.locator('#hide-menu').isHidden(), true, 'Escape closes the hide menu');
      await c.locator('#hide-open').click();
      await c.locator('#hide-song').click();
      await c.waitForFunction(() => !document.getElementById('hidden-mark').hidden);
      assert.deepEqual(hiddenPosts.at(-1), { add: { artist: 'Mira', title: 'Night Walk' } });
      assert.equal(await text('#hidden-mark'), words.hiddenMark);
      assert.equal(await c.locator('#hide-open').isHidden(), true);
      assert.equal(await c.locator('#card-hidden').isVisible(), true);
      assert.equal(await c.locator('#hidden-list li').count(), 1);
      assert.ok((await c.locator('#hidden-list button').getAttribute('aria-label')).includes(words.removeLabel));
      await c.waitForFunction(() => document.getElementById('card-note').textContent !== '');
      await shot(`hidden-${lang}-560`);
      await c.locator('#hidden-list button').click();
      await c.waitForFunction(() => document.getElementById('hidden-mark').hidden && !document.getElementById('hide-open').hidden);
      assert.deepEqual(hiddenPosts.at(-1), { remove: { artist: 'Mira', title: 'Night Walk' } });
      assert.equal(await c.locator('#card-hidden').isHidden(), true);
      await c.locator('#hide-open').click();
      await c.locator('#hide-artist').click();
      await c.waitForFunction(() => !document.getElementById('hidden-mark').hidden);
      assert.deepEqual(hiddenPosts.at(-1), { add: { artist: 'Mira', title: '' } });
      await c.locator('#hidden-list button').click();
      await c.waitForFunction(() => document.getElementById('hidden-mark').hidden);
      // Nothing playing: no hide button, and the preview says it is a sample.
      state = { ...state, title: '', artist: '', album: '' };
      await c.evaluate(() => refreshSoon());
      await c.waitForFunction(() => document.getElementById('hide-row').hidden);
      await c.locator('#cf-details').fill('{{.Title}}!');
      await c.waitForFunction(() => document.getElementById('cf-r-details').textContent === 'Night Walk!');
      assert.ok((await text('#card-note')).includes(words.sample) || ja, 'the sample note shows');
      assert.equal(await c.locator('#card-note').textContent() !== '', true);
      // 320px: no horizontal scroll with the card section.
      await c.setViewportSize({ width: 320, height: 760 });
      assert.equal(await c.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'card section fits 320px');
      violations.push(...await c.evaluate(() => window.cspViolations));
      await cc.close();
      state = { ...cardTrack };
      check(`${lang === 'ja' ? 'Japanese: ' : ''}Discord card form edits, previews, applies, discards and resets; tracks can be hidden and unhidden`);
    }
    language = 'system';

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
