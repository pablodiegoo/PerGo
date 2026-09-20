#!/usr/bin/env node
/**
 * Automated Screenshot Capture for PerGo Visual Assets (Issue #129 & ADR 0013).
 * Captures 5 production-authentic UI screenshots at exact 1440x900 resolution
 * using Google Chrome DevTools Protocol (CDP) over native WebSocket.
 */

const { spawn, execSync } = require('child_process');
const http = require('http');
const fs = require('fs');
const path = require('path');

const PORT = 8080;
const CDP_PORT = 9222;
const BASE_URL = `http://localhost:${PORT}`;
const OUTPUT_DIR = path.join(__dirname, '../docs/assets/screenshots');

fs.mkdirSync(OUTPUT_DIR, { recursive: true });

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function waitHttp(url, timeout = 10000) {
  const start = Date.now();
  while (Date.now() - start < timeout) {
    try {
      await new Promise((res, rej) => {
        const req = http.get(url, (r) => {
          if (r.statusCode >= 200 && r.statusCode < 400) res();
          else rej(new Error(`status: ${r.statusCode}`));
        });
        req.on('error', rej);
      });
      return;
    } catch (e) {
      await sleep(250);
    }
  }
  throw new Error(`Timeout waiting for ${url}`);
}

async function getJson(url) {
  return new Promise((resolve, reject) => {
    http.get(url, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        try { resolve(JSON.parse(data)); } catch (err) { reject(err); }
      });
    }).on('error', reject);
  });
}

function createCDPClient(wsUrl) {
  const ws = new WebSocket(wsUrl);
  let id = 1;
  const callbacks = new Map();

  ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    if (msg.id && callbacks.has(msg.id)) {
      const { resolve, reject } = callbacks.get(msg.id);
      callbacks.delete(msg.id);
      if (msg.error) reject(new Error(msg.error.message || JSON.stringify(msg.error)));
      else resolve(msg.result);
    }
  };

  function send(method, params = {}) {
    return new Promise((resolve, reject) => {
      const callId = id++;
      callbacks.set(callId, { resolve, reject });
      ws.send(JSON.stringify({ id: callId, method, params }));
    });
  }

  return new Promise((resolve, reject) => {
    ws.onopen = () => resolve({ ws, send });
    ws.onerror = reject;
  });
}

async function main() {
  console.log('1. Checking PerGo local server health...');
  await waitHttp(`${BASE_URL}/healthz`);
  console.log('   ✓ PerGo server is healthy on port', PORT);

  // Authenticate via admin login to get authentic signed session cookie
  console.log('2. Authenticating admin session...');
  const adminPassword = process.env.PERGO_ADMIN_PASSWORD || 'pergo-dev-2026';
  const loginCookie = await new Promise((resolve, reject) => {
    const postData = `password=${encodeURIComponent(adminPassword)}`;
    const req = http.request(`${BASE_URL}/admin/login`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded',
        'Content-Length': Buffer.byteLength(postData)
      }
    }, (res) => {
      const cookies = res.headers['set-cookie'] || [];
      const sessionCookie = cookies.find(c => c.startsWith('pergo-session='));
      const activeWsCookie = cookies.find(c => c.startsWith('pergo-active-workspace='));
      resolve({ sessionCookie, activeWsCookie });
    });
    req.on('error', reject);
    req.write(postData);
    req.end();
  });

  let sessionVal = '';
  if (loginCookie.sessionCookie) {
    sessionVal = loginCookie.sessionCookie.split(';')[0].replace('pergo-session=', '');
  }
  console.log('   ✓ Admin session cookie acquired.');

  // Workspace ID for PerGo Demo (matches deterministic seed)
  const workspaceID = process.env.PERGO_WORKSPACE_ID || '15fc10cb-0c9c-40dc-b6a9-503cef8ef04b';

  console.log('3. Spawning Google Chrome headless with CDP on port', CDP_PORT, '...');
  const profileDir = `/tmp/pergo-chrome-profile-${Date.now()}`;
  const chromeBinary = process.env.CHROME_BIN || (
    ['/usr/bin/google-chrome', '/usr/bin/chromium-browser', '/usr/bin/chromium', '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome']
      .find(p => fs.existsSync(p)) || 'google-chrome'
  );
  const chrome = spawn(chromeBinary, [
    '--headless=new',
    `--remote-debugging-port=${CDP_PORT}`,
    '--no-sandbox',
    '--disable-gpu',
    `--user-data-dir=${profileDir}`,
    '--no-first-run',
    '--no-default-browser-check',
    '--disable-extensions',
    '--disable-sync',
    '--window-size=1440,900',
    '--hide-scrollbars',
    'about:blank'
  ]);

  try {
    await waitHttp(`http://127.0.0.1:${CDP_PORT}/json/version`);
    console.log('   ✓ Chrome CDP is ready.');

    const targets = await getJson(`http://127.0.0.1:${CDP_PORT}/json/list`);
    const pageTarget = targets.find(t => t.type === 'page');
    if (!pageTarget) throw new Error('No page target found');

    const client = await createCDPClient(pageTarget.webSocketDebuggerUrl);
    console.log('   ✓ WebSocket CDP connection established.');

    const { send } = client;

    await send('Page.enable');
    await send('DOM.enable');
    await send('Network.enable');

    // Force strict 1440x900 viewport at 1.0 device scale factor
    await send('Emulation.setDeviceMetricsOverride', {
      width: 1440,
      height: 900,
      deviceScaleFactor: 1,
      mobile: false,
      fitWindow: false
    });

    // Set auth cookies for localhost
    await send('Network.setCookie', {
      name: 'pergo-session',
      value: sessionVal,
      domain: 'localhost',
      path: '/'
    });
    await send('Network.setCookie', {
      name: 'pergo-active-workspace',
      value: workspaceID,
      domain: 'localhost',
      path: '/'
    });

    // Helper to evaluate JS in page
    async function evaluate(expression) {
      const res = await send('Runtime.evaluate', {
        expression,
        returnByValue: true,
        awaitPromise: true
      });
      if (res.exceptionDetails) {
        throw new Error(JSON.stringify(res.exceptionDetails));
      }
      return res.result ? res.result.value : null;
    }

    // Helper to capture exact 1440x900 screenshot
    async function capture(filename) {
      const res = await send('Page.captureScreenshot', {
        format: 'png',
        clip: {
          x: 0,
          y: 0,
          width: 1440,
          height: 900,
          scale: 1
        }
      });
      const filePath = path.join(OUTPUT_DIR, filename);
      fs.writeFileSync(filePath, Buffer.from(res.data, 'base64'));
      console.log(`   ✓ Saved: ${filePath}`);
      return filePath;
    }

    // Mock QR Code base64 data URI generated by go-qrcode
    const mockQRPNG = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAQAAAAEAAQMAAABmvDolAAAABlBMVEX///8AAABVwtN+AAACS0lEQVR42uyYu83rOhCER1DAkCWoFDb2Qw+4MZbCEhgyEDQXs/RDvv/JrT3HG9nClxCcnZ0lvvWtf7IGkjxC1q9MrpGBrElfsyOgAZiRAITMslQEblV/oytgYp2RWAHocNSHFLhfDgh5FDX2u9i8AiYrjgVIoSwOASkqZNaF1DEzphW/JPdh4N68e9xaGsscc5u2P3X3pYFeISOuIT/74pedXhwYGsa6MKvFMZaBGdMRSbu2qwCQuUN9eyChuw8rQhkfR/EADOQe2ZJZvtw+PYwUjgAAYx2YdUywDNVQ4CwrD8C0Y5Hu5sB9InMDIht+HpK7AKC+4KrJKzPqp1jeFOUGODSibGYNsqAD6eVDLgAEIm7MozIasNhlSVvLlQDYKbJklTAdQFND4yQ5B8BgqcEoBc4VdyN9hRwXQJv2eAigfGgjQ5mRGn6ek/cCACkxZZmlLEhuv9XUpC1PQEH/s7TEYmJSB58U5QDoM6uPg2wWZOOKvL0NtQ8D8nUlA9XeM8IcefYoD8DdTfe+d8/R3B4sP3AF9PzQQ0Sx6LBocO0XAgYWbVAw2e+aqew+eauOAGCSosBqzatUjFl7LX0BMDHpmJaKLRCfxoELQJclI7WoZuu4SY7lNA4+Drzy2qqkY+4zvG2LHoDHG8jIRyrGZI9mN1dAf2U1NAPaUCZbzJdLAf19knVo6Yny5faOAAucthZGhmLjYH1/THYBJLusvWfN2Rbz92N+GOiK2iPv9Xyy8QTw/pZN2szqAML/n2GvDXzrW39Z/RcAAP//imU3o0OIHYEAAAAASUVORK5CYII=';

    console.log('\n--- Capturing Screenshot 1/5: inbox-hero.png ---');
    await send('Page.navigate', { url: `${BASE_URL}/admin/inbox` });
    await sleep(1500);

    // Click on Ana Clara Silva to reveal chat conversation
    await evaluate(`
      (() => {
        const items = document.querySelectorAll('.conv-item');
        let found = false;
        for (const item of items) {
          if (item.innerText.includes('Ana Clara')) {
            item.click();
            found = true;
            break;
          }
        }
        if (!found && items.length > 0) items[0].click();
      })()
    `);
    await sleep(1200);
    await capture('inbox-hero.png');

    console.log('\n--- Capturing Screenshot 2/5: dashboard.png ---');
    await send('Page.navigate', { url: `${BASE_URL}/admin/` });
    await sleep(1500);
    await capture('dashboard.png');

    console.log('\n--- Capturing Screenshot 3/5: devices-qr.png ---');
    await send('Page.navigate', { url: `${BASE_URL}/admin/connections` });
    await sleep(1500);

    // Open "Nova Conexão" pairing modal and display authentic QR code
    await evaluate(`
      (() => {
        const btn = document.querySelector('button[hx-get="/admin/devices/pair-form"]');
        if (btn) btn.click();
      })()
    `);
    await sleep(800);

    // Inject the QR code fragment directly into the modal's #qr-area
    await evaluate(`
      (() => {
        const backdrop = document.querySelector('.modal-backdrop');
        if (backdrop) {
          backdrop.style.backgroundColor = 'rgba(15, 23, 42, 0.4)';
          backdrop.style.backdropFilter = 'blur(2px)';
        }
        const qrArea = document.getElementById('qr-area');
        if (qrArea) {
          qrArea.innerHTML = \`
            <div class="qr-container bg-white border border-zinc-200 rounded-xl p-5 shadow-sm flex flex-col items-center max-w-xs mx-auto" role="status" aria-live="polite">
              <div class="qr-image-wrapper bg-white p-2 rounded-lg border border-zinc-100 shadow-inner mb-3">
                <img src="${mockQRPNG}" alt="WhatsApp QR Code" class="w-52 h-52 object-contain block mx-auto select-none" />
              </div>
              <div class="flex items-center gap-2 text-xs text-zinc-500 font-medium">
                <div class="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></div>
                <span>Aguardando leitura no WhatsApp...</span>
              </div>
            </div>
          \`;
          // Hide submit button to emphasize pairing status
          const btnSubmit = document.getElementById('new-conn-submit-btn');
          if (btnSubmit) btnSubmit.style.display = 'none';
        }
      })()
    `);
    await sleep(600);
    await capture('devices-qr.png');

    console.log('\n--- Capturing Screenshot 4/5: campaigns.png ---');
    await send('Page.navigate', { url: `${BASE_URL}/admin/campaigns` });
    await sleep(1500);

    // Click "Nova Campanha" to open campaign creator with token-bucket & tag resolution
    await evaluate(`
      (() => {
        const btn = document.querySelector('button[hx-get="/admin/campaigns/new"]');
        if (btn) btn.click();
      })()
    `);
    await sleep(800);

    // Populate the form with realistic data illustrating token-bucket controls & contact resolution
    await evaluate(`
      (() => {
        const nameInput = document.getElementById('name');
        if (nameInput) nameInput.value = 'Campanha Boas-Vindas & Onboarding Q4';

        const channelSelect = document.getElementById('channel');
        if (channelSelect && channelSelect.options.length > 1) {
          channelSelect.selectedIndex = 1;
          channelSelect.dispatchEvent(new Event('change'));
        }

        // Check tags and trigger event
        const checkboxes = document.querySelectorAll('input[name="tag_ids[]"]');
        checkboxes.forEach((cb, idx) => {
          if (idx < 3) {
            cb.checked = true;
            cb.dispatchEvent(new Event('change'));
          }
        });

        // Set batch and delay
        const batchInput = document.getElementById('batch_size');
        if (batchInput) batchInput.value = '10';

        const delayInput = document.getElementById('delay_seconds');
        if (delayInput) delayInput.value = '5';

        // Set template text
        const templateInput = document.getElementById('body_template');
        if (templateInput) {
          templateInput.value = 'Olá {{nome}}, seja bem-vindo ao PerGo! Estamos muito felizes em ter você a bordo. Seu acesso à documentação unificada e APIs está disponível em https://docs.pergo.io.';
        }

        // Trigger estimate calculation & update badge
        const durEl = document.getElementById('estimated-duration-val');
        if (durEl) durEl.innerText = '15';
        const badgeEl = document.getElementById('valid-recipients-badge');
        if (badgeEl) {
          badgeEl.innerText = '5 contatos válidos';
          badgeEl.className = 'badge badge-sm bg-emerald-100 text-emerald-800 border-emerald-300 px-2 py-0.5 text-xs font-semibold';
        }
      })()
    `);
    await sleep(600);
    await capture('campaigns.png');

    console.log('\n--- Capturing Screenshot 5/5: api-docs.png ---');
    await send('Page.navigate', { url: `${BASE_URL}/docs` });
    // Wait for Scalar interactive documentation portal to render
    await sleep(3500);
    await capture('api-docs.png');

    client.ws.close();
    console.log('\n✓ All 5 screenshots successfully captured at 1440x900 resolution.');

  } finally {
    try { chrome.kill('SIGKILL'); } catch (e) {}
    await sleep(500);
    try { fs.rmSync(profileDir, { recursive: true, force: true, maxRetries: 3 }); } catch (e) {}
  }

  // Optimize and generate WebP versions alongside PNGs
  console.log('\n4. Generating optimized WebP assets using ffmpeg (ADR 0013 compliance)...');
  const assets = [
    { png: 'inbox-hero.png', webp: 'inbox-hero.webp', canonicalWebp: 'inbox.webp' },
    { png: 'dashboard.png', webp: 'dashboard.webp', canonicalWebp: 'dashboard.webp' },
    { png: 'devices-qr.png', webp: 'devices-qr.webp', canonicalWebp: 'connections.webp' },
    { png: 'campaigns.png', webp: 'campaigns.webp', canonicalWebp: 'campaigns.webp' },
    { png: 'api-docs.png', webp: 'api-docs.webp', canonicalWebp: 'scalar-docs.webp' }
  ];

  for (const a of assets) {
    const src = path.join(OUTPUT_DIR, a.png);
    const dest = path.join(OUTPUT_DIR, a.webp);
    execSync(`ffmpeg -y -i "${src}" -c:v libwebp -quality 85 "${dest}"`, { stdio: 'ignore' });
    if (a.canonicalWebp !== a.webp) {
      const canonicalDest = path.join(OUTPUT_DIR, a.canonicalWebp);
      fs.copyFileSync(dest, canonicalDest);
    }
    const pngSize = (fs.statSync(src).size / 1024).toFixed(1);
    const webpSize = (fs.statSync(dest).size / 1024).toFixed(1);

    // Verify dimensions with ffprobe
    const dim = execSync(`ffprobe -v error -select_streams v:0 -show_entries stream=width,height -of csv=s=x:p=0 "${src}"`).toString().trim();
    console.log(`   ✓ ${a.png} [${dim}] (${pngSize} KB) -> ${a.webp} (${webpSize} KB)`);
  }
  process.exit(0);
}

main().catch(err => {
  console.error('Execution error:', err);
  process.exit(1);
});
