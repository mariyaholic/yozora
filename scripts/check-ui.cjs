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
const html = fs.readFileSync(path.join(root, 'internal/server/ui.html'), 'utf8').replace('{{.Nonce}}', nonce).replace('{{.Theme}}', 'yozora').replace('{{.Motion}}', 'system');
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
let themeHTTP = 200;
const themePosts = [];
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
  } else if (/^\/fonts\/[a-z0-9-]+\.woff2$/.test(url.pathname) && fs.existsSync(path.join(root, 'internal/server', url.pathname))) {
    res.writeHead(200, { 'Content-Type': 'font/woff2' });
    res.end(fs.readFileSync(path.join(root, 'internal/server', url.pathname)));
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
  } else if (url.pathname === '/api/appearance') {
    if (req.method === 'POST') {
      let body = '';
      for await (const chunk of req) body += chunk;
      const posted = JSON.parse(body);
      themePosts.push(posted);
      if (themeHTTP === 200) { theme = posted.theme ?? theme; motion = posted.motion ?? motion; }
    }
    json({ theme, themes: [], motion, motions: [] }, themeHTTP);
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
    // Settings are ready once the sources fieldset unlocks; a clean form shows no status line at all.
    const ready = p => p.waitForFunction(() => !document.getElementById('source-fields').disabled);
    const openAppearance = async p => {
      if (!await p.evaluate(() => document.getElementById('appearance').open)) await p.locator('#appearance-open').click();
    };
    const closeAppearance = p => p.evaluate(() => document.getElementById('appearance').close());
    // Picks a theme or motion mode through the dialog, waits for the save, and closes it again.
    const choose = async (p, name, value, saved) => {
      await openAppearance(p);
      const tile = name === 'theme' ? p.locator('label.theme-option', { has: p.locator(`input[value="${value}"]`) }) : p.locator('.segmented label', { has: p.locator(`input[value="${value}"]`) });
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
    const generatedConfig = fs.readFileSync(path.join(root, 'internal/config/theme_catalog.go'), 'utf8');
    const generatedIds = [...generatedConfig.match(/var Themes = \[\]string\{([\s\S]*?)\}/)[1].matchAll(/"([a-z0-9-]+)"/g)].map(match => match[1]);
    assert.deepEqual(themeIds, generatedIds, 'picker and compiled configuration share the same catalog');
    assert.equal(themeIds.length, 37);
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
      for (const family of (faces[id] || [])) assert.ok(loaded.includes(family), `${id}: ${family} did not load (loaded: ${loaded})`);
      const used = await page.evaluate(() => [getComputedStyle(document.querySelector('h1')).fontFamily, getComputedStyle(document.body).fontFamily]);
      if ((faces[id] || []).length) assert.ok(faces[id].some(family => used.join().includes(family)), `${id}: the page does not ask for its faces`);
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
    assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), themeIds.at(-1), 'a failed save must restore the saved theme');
    assert.equal(await page.locator(`input[name="theme"][value="${themeIds.at(-1)}"]`).isChecked(), true);
    themeHTTP = 200;
    await applyTheme('yozora');
    check('all shared design-system themes apply, save, roll back, keep 4.5:1 text, and load their own faces');

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
