// Smoke test: every pre-installed browser must launch headless and render a page.
// Run inside the image:
//   docker run --rm --shm-size=1g -v "$PWD/images/github-runner/smoke.cjs:/smoke.cjs:ro" <image> node /smoke.cjs
const { chromium, firefox, webkit } = require('playwright');

const targets = [
  ['chromium', chromium, {}],
  ['firefox', firefox, {}],
  ['webkit', webkit, {}],
];
if (process.arch === 'x64') targets.push(['chrome', chromium, { channel: 'chrome' }]);

(async () => {
  let failed = false;
  for (const [name, type, opts] of targets) {
    try {
      const browser = await type.launch(opts);
      const page = await browser.newPage();
      await page.setContent('<title>smoke-ok</title><h1>hello</h1>');
      const title = await page.title();
      if (title !== 'smoke-ok') throw new Error(`unexpected title: ${title}`);
      console.log(`PASS ${name} ${browser.version()}`);
      await browser.close();
    } catch (err) {
      failed = true;
      console.error(`FAIL ${name}: ${err.message.split('\n')[0]}`);
    }
  }
  process.exit(failed ? 1 : 0);
})();
