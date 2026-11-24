// 用本机 Edge 无头模式把演示页逐屏截图（1x，控制体积），输出到 docs/product/ui/mockup/shots
const { chromium } = require('playwright');
const path = require('path');
const fs = require('fs');

const root = process.argv[2];
if (!root) { console.error('usage: node shoot.js <repoRoot>'); process.exit(1); }
const outDir = path.join(root, 'docs', 'product', 'ui', 'mockup', 'shots');

(async () => {
  fs.mkdirSync(outDir, { recursive: true });
  const browser = await chromium.launch({
    channel: 'msedge',
    executablePath: 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
    args: ['--force-color-profile=srgb', '--font-render-hinting=none'],
  });
  const page = await browser.newPage({ viewport: { width: 1360, height: 1200 }, deviceScaleFactor: 1 });
  const file = 'file:///' + path.join(root, 'docs', 'product', 'ui', 'mockup', 'index.html').replace(/\\/g, '/');
  await page.goto(file, { waitUntil: 'load' });
  const shots = [
    ['s1', '01-entry.png'],
    ['s2', '02-workspace.png'],
    ['s3', '03-review.png'],
    ['s4', '04-history.png'],
    ['s5', '05-settings.png'],
    ['s6', '06-mobile.png'],
  ];
  for (const [id, name] of shots) {
    await page.setViewportSize(id === 's6' ? { width: 430, height: 1000 } : { width: 1360, height: 1200 });
    await page.waitForTimeout(120);
    const el = await page.$('#' + id);
    await el.scrollIntoViewIfNeeded();
    await el.screenshot({ path: path.join(outDir, name) });
    console.log('saved', name);
  }
  await browser.close();
})().catch(err => { console.error(err); process.exit(1); });
