#!/usr/bin/env node
// Adapt a baked dsh install for HTTP + non-loopback access (LAN/public IP).
//
// Community (dsh-web-lan-access) injects a crypto.randomUUID polyfill via
// webServer.tapIndex. We bake the same polyfill into dist/index.html because
// the control plane already publishes via a TCP proxy, not dsh --host 0.0.0.0.
//
// Settings/credentials stay loopback-only in unmodified dsh. The same plugin
// README documents the one-line host change (PRIVILEGED_METHODS must honor
// trustedHosts). The browser client also skips settings.describe unless
// connection.isLoopback, which is why Models shows
// "settings are unavailable in this browser".
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

const index = require.resolve("@deepseek-ai/dsh-web-frontend/dist/index.html");
const html = readFileSync(index, "utf8");
if (html.includes(POLYFILL_MARKER)) {
  process.stdout.write(`already patched ${index}\n`);
} else {
  const patched = html.replace(/<head>/i, `<head>\n${SNIPPET}`);
  if (patched === html) throw new Error(`patch-frontend: no <head> in ${index}`);
  writeFileSync(index, patched);
  process.stdout.write(`patched ${index}\n`);
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
