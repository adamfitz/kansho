const SUPPORTED_DOMAINS = [
  "*://*.mgeko.cc/*",
  "*://challenges.cloudflare.com/*",
  "*://manhuaus.com/*",
  "*://*.manhuaus.com/*",
  "*://manhuaus.org/*",
  "*://*.manhuaus.org/*",
  "*://kunmanga.com/*",
  "*://*.kunmanga.com/*",
  "*://kunmanga.online/*",
  "*://*.kunmanga.online/*",
  "*://kunmanga.co.uk/*",
  "*://*.kunmanga.co.uk/*",
  "*://asuracomic.net/*",
  "*://*.asuracomic.net/*",
  "*://mangakatana.com/*",
  "*://*.mangakatana.com/*",
  "*://mangadex.org/*",
  "*://*.mangadex.org/*",
  "*://flamecomics.xyz/*",
  "*://*.flamecomics.xyz/*",
  "*://weebcentral.com/*",
  "*://*.weebcentral.com/*"
];

const URL_PATTERNS = SUPPORTED_DOMAINS;

console.log("[CF Monitor] Monitoring domains:", SUPPORTED_DOMAINS);
console.log("[CF Monitor] URL patterns:", URL_PATTERNS);

browser.webRequest.onBeforeRequest.addListener(
  (details) => {
    try {
      if (
        details.method === "POST" &&
        details.url.includes("challenge-platform")
      ) {
        if (details.requestBody && details.requestBody.raw?.length) {
          const raw = details.requestBody.raw[0];
          const body = new TextDecoder().decode(raw.bytes || raw);

          console.log("🔥 [CF TURNSTILE PAYLOAD CAPTURED]");
          console.log(body);

          browser.storage.local.set({
            turnstilePayload: body,
            turnstileCapturedAt: new Date().toISOString(),
            turnstileUrl: details.url
          });
        }
      }
    } catch (e) {
      console.error("Error capturing CF Turnstile POST:", e);
    }
  },
  {
    urls: URL_PATTERNS
  },
  ["requestBody"]
);

browser.webRequest.onHeadersReceived.addListener(
  (details) => {
    try {
      const headers = details.responseHeaders;
      if (!headers) return;

      for (const header of headers) {
        if (header.name.toLowerCase() === "set-cookie" &&
            header.value.includes("cf_clearance=")) {

          console.log("🔥 [CF CLEARANCE COOKIE CAPTURED]");
          console.log(header.value);

          browser.storage.local.set({
            cfClearanceRaw: header.value,
            cfClearanceCapturedAt: new Date().toISOString(),
            cfClearanceUrl: details.url
          });
        }
      }
    } catch (e) {
      console.error("Error capturing cf_clearance cookie:", e);
    }
  },
  {
    urls: URL_PATTERNS
  },
  ["responseHeaders", "extraHeaders"]
);
