// ============================================================================
// SUPPORTED DOMAINS CONFIGURATION
// ============================================================================
// To add new domains, simply add them to this array
const SUPPORTED_DOMAINS = [
  "*://*.mgeko.cc/*",
  "*://challenges.cloudflare.com/*",
  // "*://*.xbato.com/*", // removed xbato from supported domains, due to site uptime issues
  // "*://xbato.com/*", // removed xbato from supported domains, due to site uptime issues
  //"*://rizzfables.com/*",
  //"*://*.rizzfables.com/*", removed rizzfables from supported domains, sie is basically dead
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
  "*://*.mangadex.org/*",
  "*://flamecomics.xyz/*",
  "*://*.flamecomics.xyz/*",
  "*://weebcentral.com/*",
  "*://*.weebcentral.com/*",
  "*://comix.to/*",
  "*://*.comix.to/*",
  "*://arenascan.com/*",
  "*://*.arenascan.com/*",
  "*://en-thunderscans.com/*",
  "*://*.en-thunderscans.com/*"
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

// Fallback capture path: cf_clearance can be set without a webRequest event we
// observe (for example when it already existed before the extension loaded).
// The cookies API still fires onChanged when the browser writes it.
chrome.cookies.onChanged.addListener(({ removed, cookie }) => {
  try {
    if (removed || !cookie || cookie.name !== "cf_clearance" || !cookie.value) {
      return;
    }

    let raw = `cf_clearance=${cookie.value}`;
    if (cookie.domain) raw += `; Domain=${cookie.domain}`;
    if (cookie.path) raw += `; Path=${cookie.path}`;
    if (cookie.sameSite) raw += `; SameSite=${cookie.sameSite}`;
    if (cookie.secure) raw += `; Secure`;
    if (cookie.httpOnly) raw += `; HttpOnly`;
    if (cookie.partitionKey && cookie.partitionKey.topLevelSite) {
      raw += `; Partitioned`;
    }
    if (cookie.expirationDate) {
      raw += `; Expires=${new Date(cookie.expirationDate * 1000).toUTCString()}`;
    }

    const domain = (cookie.domain || "").replace(/^\./, "");
    chrome.storage.local.set({
      cfClearanceRaw: raw,
      cfClearanceCapturedAt: new Date().toISOString(),
      cfClearanceUrl: `https://${domain}${cookie.path || "/"}`
    });
  } catch (e) {
    console.error("Error storing cf_clearance from cookies.onChanged:", e);
  }
});


// ============================================================================
// END OF FILE
// ============================================================================