// ============================================================================
// cf Turnstile POST Payload Capture
// ============================================================================

chrome.webRequest.onBeforeRequest.addListener(
  (details) => {
    try {
      // cf Managed Challenge POST request
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
    urls: [
      "*://challenges.cf.com/*",
      "*://*.mgeko.cc/*"
    ]
  },
  ["requestBody"]
);

// ============================================================================
// cf cf_clearance Cookie Capture
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

          // OLD WORKING METHOD: store raw cookie only
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
    urls: [
      "*://*.mgeko.cc/*",
      "*://challenges.cf.com/*"
    ]
  },
  ["responseHeaders", "extraHeaders"]
);


// ============================================================================
// END OF FILE
// ============================================================================
