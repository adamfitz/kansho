document.getElementById('captureBtn').addEventListener('click', async () => {
  const statusDiv = document.getElementById('status');
  const previewDiv = document.getElementById('preview');
  const button = document.getElementById('captureBtn');

  try {
    button.disabled = true;
    statusDiv.className = 'info';
    statusDiv.textContent = 'Capturing data...';

    const [tab] = await browser.tabs.query({ active: true, currentWindow: true });

    if (!tab || !tab.url) {
      throw new Error('No active tab found');
    }

    const url = new URL(tab.url);
    const domain = url.hostname;

    const cookies = await browser.cookies.getAll({ domain: domain });

    const parentDomain = domain.split('.').slice(-2).join('.');
    const parentCookies = await browser.cookies.getAll({ domain: parentDomain });

    const dotCookies = document.cookie
      .split('; ')
      .map(c => {
        const [name, value] = c.split('=');
        return { name, value, domain: window.location.hostname };
      });
    const urlCookies = [];

    const allCookies = [...cookies, ...parentCookies, ...dotCookies, ...urlCookies].reduce((acc, cookie) => {
      const key = `${cookie.name}-${cookie.domain}`;

      if (!acc.has(key)) {
        acc.set(key, cookie);
      }
      return acc;
    }, new Map());

    const cfCookies = Array.from(allCookies.values()).filter(cookie =>
      cookie.name.toLowerCase().includes('cf_') ||
      cookie.name.toLowerCase().includes('clearance') ||
      cookie.name === '__cfduid' ||
      cookie.name === '__cf_bm'
    );

    const entropyResult = await browser.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => {
        return {
          userAgent: navigator.userAgent,
          language: navigator.language,
          languages: navigator.languages,
          platform: navigator.platform,
          hardwareConcurrency: navigator.hardwareConcurrency,
          deviceMemory: navigator.deviceMemory,
          screenResolution: {
            width: screen.width,
            height: screen.height,
            colorDepth: screen.colorDepth,
            pixelDepth: screen.pixelDepth
          },
          timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
          timezoneOffset: new Date().getTimezoneOffset(),
          webgl: (() => {
            try {
              const canvas = document.createElement('canvas');
              const gl = canvas.getContext('webgl') || canvas.getContext('experimental-webgl');
              if (!gl) return null;
              const debugInfo = gl.getExtension('WEBGL_debug_renderer_info');
              return {
                vendor: gl.getParameter(debugInfo.UNMASKED_VENDOR_WEBGL),
                renderer: gl.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL)
              };
            } catch (e) {
              return null;
            }
          })()
        };
      }
    });

    const entropy = entropyResult[0].result;

    let turnstileData = null;

    try {
      const turnstileResult = await browser.scripting.executeScript({
        target: { tabId: tab.id },
        func: () => {
          const turnstileIframe = document.querySelector('iframe[src*="challenges.cf.com"]');

          const forms = document.querySelectorAll('form');
          let formData = {};
          let foundTurnstile = false;

          for (const form of forms) {
            const inputs = form.querySelectorAll('input[type="hidden"]');
            for (const input of inputs) {
              if (input.name.length > 30 && input.value.length > 100) {
                formData[input.name] = input.value;
                foundTurnstile = true;
              }
            }

            if (foundTurnstile && form.action) {
              formData['_form_action'] = form.action;
            }
          }

          const urlParams = new URLSearchParams(window.location.search);
          const challengeToken = urlParams.get('__cf_chl_tk');

          return {
            hasTurnstile: foundTurnstile || turnstileIframe !== null,
            formData: formData,
            challengeToken: challengeToken,
            currentUrl: window.location.href
          };
        }
      });

      turnstileData = turnstileResult[0].result;
      console.log('Turnstile detection:', turnstileData);

    } catch (e) {
      console.error('Failed to detect Turnstile:', e);
    }

    const cfClearanceStored = await browser.storage.local.get([
      "cfClearanceRaw",
      "cfClearanceCapturedAt",
      "cfClearanceUrl"
    ]);

    const turnstileStored = await browser.storage.local.get([
      "turnstilePayload",
      "turnstileCapturedAt",
      "turnstileUrl"
    ]);

    const exportData = {
      capturedAt: new Date().toISOString(),
      url: tab.url,
      domain: domain,
      type: turnstileData && turnstileData.hasTurnstile ? "turnstile" : "cookie",
      cookies: cfCookies.map(c => ({
        name: c.name,
        value: c.value,
        domain: c.domain,
        path: c.path,
        secure: c.secure,
        httpOnly: c.httpOnly,
        sameSite: c.sameSite,
        expirationDate: c.expirationDate
      })),
      allCookies: Array.from(allCookies.values()).map(c => ({
        name: c.name,
        value: c.value,
        domain: c.domain,
        path: c.path
      })),
      turnstileToken: turnstileData && turnstileData.hasTurnstile ? "captured" : "",
      turnstileFormData: turnstileData ? turnstileData.formData : {},
      challengeToken: turnstileData ? turnstileData.challengeToken : "",
      turnstileRequestBody: turnstileStored.turnstilePayload || "",
      turnstileRequestUrl: turnstileStored.turnstileUrl || "",
      turnstileCapturedAt: turnstileStored.turnstileCapturedAt || "",
      cfClearance: cfClearanceStored.cfClearanceRaw || "",
      cfClearanceCapturedAt: cfClearanceStored.cfClearanceCapturedAt || "",
      cfClearanceUrl: cfClearanceStored.cfClearanceUrl || "",
      entropy: entropy,
      headers: {
        userAgent: navigator.userAgent,
        acceptLanguage: navigator.language
      }
    };

    const jsonData = JSON.stringify(exportData, null, 2);

    await navigator.clipboard.writeText(jsonData);

    statusDiv.className = 'success';
    const protectionType = turnstileData && turnstileData.hasTurnstile ?
      `Turnstile (${Object.keys(turnstileData.formData).length} tokens)` :
      `Cookies (${cfCookies.length} CF + ${allCookies.size} total)`;

    statusDiv.textContent = `✓ Copied! Protection: ${protectionType}`;

    previewDiv.innerHTML = `
      <strong>Preview:</strong>
      <pre>${jsonData.substring(0, 300)}...</pre>
    `;

    button.disabled = false;

  } catch (error) {
    console.error('Error capturing data:', error);

    statusDiv.className = 'error';
    statusDiv.textContent = `Error: ${error.message}`;

    button.disabled = false;
  }
});

browser.tabs.query({ active: true, currentWindow: true }, async ([tab]) => {
  if (!tab) return;

  try {
    const result = await browser.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => {
        return document.body.textContent.toLowerCase().includes('cf') ||
               document.body.textContent.toLowerCase().includes('checking your browser') ||
               document.querySelector('[class*="cf-"]') !== null;
      }
    });

    if (result && result[0] && result[0].result) {
      const statusDiv = document.getElementById('status');
      statusDiv.className = 'info';
      statusDiv.textContent = 'cf challenge detected on this page';
    }
  } catch (e) {
  }
});
