#!/usr/bin/env node
// Adapt a baked dsh install for HTTP + non-loopback access (LAN/public IP).
//
// New-structure family (0.1.2-alpha.* and up). Upstream now honors the
// configured trustedHosts in isTrustedApiRequest(request, this.trustedHosts)
// and has dropped PRIVILEGED_METHODS, so no host-side rewrite is needed: the
// entrypoint's --trusted-host values are already respected. The browser client
// still gates settings/credentials on connection.isLoopback, now written as
// transport?.ownsHost === true || pageLocation === void 0 || <hostname check>,
// which is false through the LAN/TCP proxy; pin it to true. The
// crypto.randomUUID polyfill is still required for non-secure HTTP origins.
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

// 0.1.2-rc.1+ host fence already calls isTrustedApiRequest(request, this.trustedHosts).
const clientConn = require.resolve("@deepseek-ai/dsh-client-connection/client");
patchOnce(
  clientConn,
  `isLoopback: transport?.ownsHost === true || pageLocation === void 0 || isLoopbackHostname(pageLocation.hostname)`,
  `isLoopback: true`,
  "client connection.isLoopback",
);
