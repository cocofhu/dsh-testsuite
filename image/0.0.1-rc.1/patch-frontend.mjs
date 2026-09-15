#!/usr/bin/env node
// Adapt a baked dsh install for HTTP + non-loopback access (LAN/public IP).
//
// Old-structure family (0.0.1-rc.1/rc.2, 0.0.1-rc.5, 0.1.0-rc.2/rc.3).
// The host request fence pins isTrustedApiRequest(request, []) instead of
// honoring the configured trustedHosts, and the browser client gates the
// settings/credentials pages on connection.isLoopback. Both are rewritten so a
// LAN/HTTP deployment keeps Models and credentials working.
//
// The frontend bundle package was renamed across this family:
//   - 0.0.1-rc.1 / rc.2 -> @deepseek-ai/dsh-frontend
//   - 0.0.1-rc.5 and up -> @deepseek-ai/dsh-web-frontend
// Resolve whichever is installed; skip the polyfill gracefully if the install
// has no resolvable frontend bundle.
//
import { createRequire } from "node:module";
import { execSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";

const POLYFILL_MARKER = "dsh-testsuite-crypto-polyfill";
const SNIPPET = `    <!-- ${POLYFILL_MARKER} -->
    <script>
      (function () {
        var c = globalThis.crypto;
        if (!c || typeof c.randomUUID === "function") return;
        function randomUUID() {
          var b = new Uint8Array(16);
          if (c.getRandomValues) c.getRandomValues(b);
          else for (var i = 0; i < 16; i++) b[i] = (Math.random() * 256) | 0;
          b[6] = (b[6] & 15) | 64;
          b[8] = (b[8] & 63) | 128;
          var h = "";
          for (var i = 0; i < 16; i++) h += (b[i] + 256).toString(16).slice(1);
          return h.slice(0, 8) + "-" + h.slice(8, 12) + "-" + h.slice(12, 16) + "-" + h.slice(16, 20) + "-" + h.slice(20);
        }
        try {
          Object.defineProperty(c, "randomUUID", { value: randomUUID, configurable: true, writable: true });
        } catch (e) {
          try { c.randomUUID = randomUUID; } catch (e2) {}
        }
      })();
    </script>
`;

function patchOnce(path, find, replace, label) {
  const src = readFileSync(path, "utf8");
  if (src.includes(replace.trim()) && !src.includes(find)) {
    process.stdout.write(`already patched ${label}\n`);
    return;
  }
  if (!src.includes(find)) {
    throw new Error(`patch-frontend: ${label}: pattern not found in ${path}`);
  }
  writeFileSync(path, src.replaceAll(find, replace));
  process.stdout.write(`patched ${label}\n`);
}

const root = execSync("npm root -g", { encoding: "utf8" }).trim();
const require = createRequire(`${root}/@deepseek-ai/dsh/package.json`);

function resolveFrontendIndex() {
  for (const name of [
    "@deepseek-ai/dsh-web-frontend",
    "@deepseek-ai/dsh-frontend",
  ]) {
    try {
      return require.resolve(`${name}/dist/index.html`);
    } catch {}
  }
  return undefined;
}

const index = resolveFrontendIndex();
if (index === undefined) {
  process.stdout.write(
    "patch-frontend: no resolvable frontend bundle, skipping crypto polyfill\n",
  );
} else {
  const html = readFileSync(index, "utf8");
  if (html.includes(POLYFILL_MARKER)) {
    process.stdout.write(`already patched ${index}\n`);
  } else {
    const patched = html.replace(/<head>/i, `<head>\n${SNIPPET}`);
    if (patched === html) throw new Error(`patch-frontend: no <head> in ${index}`);
    writeFileSync(index, patched);
    process.stdout.write(`patched ${index}\n`);
  }
}

const hostConn = require.resolve("@deepseek-ai/dsh-client-connection");
patchOnce(
  hostConn,
  `interceptor.options.authority === "loopback" && !isTrustedApiRequest(request, [])`,
  `interceptor.options.authority === "loopback" && !isTrustedApiRequest(request, this.trustedHosts)`,
  "host interceptor loopback pin",
);
patchOnce(
  hostConn,
  `PRIVILEGED_METHODS.has(method) && !isTrustedApiRequest(request, [])`,
  `PRIVILEGED_METHODS.has(method) && !isTrustedApiRequest(request, trustedHosts)`,
  "host privileged methods pin",
);

const clientConn = require.resolve("@deepseek-ai/dsh-client-connection/client");
patchOnce(
  clientConn,
  `isLoopback: pageLocation === void 0 || isLoopbackHostname(pageLocation.hostname)`,
  `isLoopback: true`,
  "client connection.isLoopback",
);
