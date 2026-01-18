// ============================================================================
// SUPPORTED DOMAINS CONFIGURATION
// ============================================================================
// To add new domains, simply add them to this array
const SUPPORTED_DOMAINS = [
  "*://*.mgeko.cc/*",
  "*://challenges.cloudflare.com/*",
  // "*://*.xbato.com/*", // removed xbato from supported domains, due to site uptime issues
  // "*://xbato.com/*", // removed xbato from supported domains, due to site uptime issues
  "*://rizzfables.com/*",
  "*://*.rizzfables.com/*",
  "*://manhuaus.com/*",
  "*://*.manhuaus.com/*",
  "*://manhuaus.org/*",
  "*://*.manhuaus.org/*",
  "*://kunmanga.com/*",
  "*://*.kunmanga.com/*",
  "*://asuracomic.net/*",
  "*://*.asuracomic.net/*",
  "*://mangakatana.com/*",
  "*://*.mangakatana.com/*",
  "*://mangadex.org/*",
  "*://*.mangadex.org/*"
];

// URL_PATTERNS is just the SUPPORTED_DOMAINS since they're already properly formatted
const URL_PATTERNS = SUPPORTED_DOMAINS;

console.log("[CF Monitor] Monitoring domains:", SUPPORTED_DOMAINS);
console.log("[CF Monitor] URL patterns:", URL_PATTERNS);

// ============================================================================
// CF Turnstile POST Payload Capture
// ============================================================================

chrome.webRequest.onBeforeRequest.addListener(
  (details) => {
    try {
      // CF Managed Challenge POST request
      if (
        details.method === "POST" &&
        details.url.includes("challenge-platform")
      ) {
        if (details.requestBody && details.requestBody.raw?.length) {
          const raw = details.requestBody.raw[0].bytes;
          const body = new TextDecoder().decode(raw);

          console.log("🔥 [CF TURNSTILE PAYLOAD CAPTURED]");
          console.log(body);

          // Store it for popup.js
          chrome.storage.local.set({
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

// ============================================================================
// CF cf_clearance Cookie Capture
// ============================================================================

chrome.webRequest.onHeadersReceived.addListener(
  (details) => {
    try {
      const headers = details.responseHeaders;
      if (!headers) return;

      for (const header of headers) {
        if (header.name.toLowerCase() === "set-cookie" &&
            header.value.includes("cf_clearance=")) {

          console.log("🔥 [CF CLEARANCE COOKIE CAPTURED]");
          console.log(header.value);

          // Store raw cookie only
          chrome.storage.local.set({
            cfClearanceRaw: header.value,          // original raw string
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


// ============================================================================
// END OF FILE
// ============================================================================