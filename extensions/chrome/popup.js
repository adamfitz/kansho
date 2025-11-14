/**
 * Kansho cf Helper - Browser Extension
 * 
 * This script runs when the user clicks the extension icon and opens the popup.
 * It captures cf cookies and browser fingerprint data from the current tab,
 * then copies it to the clipboard in JSON format for use by the Kansho application.
 */

// ============================================================================
// MAIN FUNCTIONALITY: Capture Button Click Handler
// ============================================================================

// Get the "Copy cf Data" button from popup.html and add a click listener
// addEventListener is like a callback in Go - it runs the function when the event happens
document.getElementById('captureBtn').addEventListener('click', async () => {
  // Get references to HTML elements we'll update during the process
  const statusDiv = document.getElementById('status');    // The status message box
  const previewDiv = document.getElementById('preview');  // The data preview area
  const button = document.getElementById('captureBtn');   // The button itself
  
  // Wrap everything in try-catch for error handling (like Go's if err != nil)
  try {
    // -------------------------------------------------------------------------
    // Step 1: Show "working" state
    // -------------------------------------------------------------------------
    button.disabled = true;  // Disable button to prevent double-clicks
    statusDiv.className = 'info';  // Apply blue "info" styling from CSS
    statusDiv.textContent = 'Capturing data...';  // Update message
    
    // -------------------------------------------------------------------------
    // Step 2: Get the current browser tab
    // -------------------------------------------------------------------------
    // chrome.tabs.query() returns a Promise (like a Go channel/future)
    // await waits for the Promise to complete (like reading from a channel)
    // We query for the active tab in the current window
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    
    // Validate we got a tab with a URL
    if (!tab || !tab.url) {
      throw new Error('No active tab found');
    }
    
    // Parse the URL to extract the domain
    // URL is a built-in JavaScript class (like url.Parse in Go)
    const url = new URL(tab.url);
    const domain = url.hostname;  // e.g., "www.mgeko.cc"
    
    // -------------------------------------------------------------------------
    // Step 3: Get all cookies for the current domain
    // -------------------------------------------------------------------------
    // chrome.cookies.getAll() queries the browser's cookie store
    // This is like calling the Chrome API from Go using CGO
    const cookies = await chrome.cookies.getAll({ domain: domain });
    
    // Also get cookies from the parent domain (e.g., ".mgeko.cc")
    // Some cookies are set on the parent domain and apply to all subdomains
    // Split by '.' and take last 2 parts: "www.mgeko.cc" -> ["mgeko", "cc"] -> "mgeko.cc"
    const parentDomain = domain.split('.').slice(-2).join('.');
    const parentCookies = await chrome.cookies.getAll({ domain: parentDomain });
    
    // -------------------------------------------------------------------------
    // Step 4: Combine and deduplicate cookies
    // -------------------------------------------------------------------------
    // We might get the same cookie from multiple queries, so deduplicate using a Map
    // Map is like Go's map[string]Cookie - key is "name-domain", value is cookie object
    const dotCookies = document.cookie
      .split('; ')
      .map(c => {
        const [name, value] = c.split('=');
        return { name, value, domain: window.location.hostname };
      });
    const urlCookies = [];

    const allCookies = [...cookies, ...parentCookies, ...dotCookies, ...urlCookies].reduce((acc, cookie) => {
      // Create a unique key for each cookie (name + domain combination)
      const key = `${cookie.name}-${cookie.domain}`;
      
      // Only add if we haven't seen this cookie before
      if (!acc.has(key)) {
        acc.set(key, cookie);  // Map.set() is like Go's map[key] = value
      }
      return acc;  // Return the accumulator for the next iteration
    }, new Map());  // Start with an empty Map
    
    // -------------------------------------------------------------------------
    // Step 5: Filter for cf-specific cookies
    // -------------------------------------------------------------------------
    // Array.from() converts the Map to an array (like converting Go map to slice)
    // .values() gets just the cookie objects (not the keys)
    // .filter() is like Go's for loop with append if condition is true
    const cfCookies = Array.from(allCookies.values()).filter(cookie => 
      // Check if cookie name contains cf-related strings
      cookie.name.toLowerCase().includes('cf_') ||          // cf_clearance, cf_chl_*, etc.
      cookie.name.toLowerCase().includes('clearance') ||    // Alternative naming
      cookie.name === '__cfduid' ||                         // Old cf cookie
      cookie.name === '__cf_bm'                             // cf Bot Management
    );
    
    // -------------------------------------------------------------------------
    // Step 6: Capture browser fingerprint data
    // -------------------------------------------------------------------------
    // chrome.scripting.executeScript() runs JavaScript code IN THE TARGET TAB
    // This is necessary because the extension popup runs in isolation
    // It's like making an RPC call to execute code in another process
    const entropyResult = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => {
        // This code runs in the context of the target webpage
        // It returns an immediately-invoked function expression (IIFE)
        return {
          // User agent string (browser identification)
          userAgent: navigator.userAgent,
          
          // Browser language settings
          language: navigator.language,      // Primary language (e.g., "en-US")
          languages: navigator.languages,    // All preferred languages
          
          // Operating system platform
          platform: navigator.platform,      // e.g., "Linux x86_64"
          
          // Hardware information
          hardwareConcurrency: navigator.hardwareConcurrency,  // CPU cores
          deviceMemory: navigator.deviceMemory,                // RAM in GB (if available)
          
          // Screen properties (important for fingerprinting)
          screenResolution: {
            width: screen.width,           // Screen width in pixels
            height: screen.height,         // Screen height in pixels
            colorDepth: screen.colorDepth, // Bits per pixel (usually 24)
            pixelDepth: screen.pixelDepth  // Bits per pixel (usually same as colorDepth)
          },
          
          // Timezone information
          timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,  // e.g., "America/New_York"
          timezoneOffset: new Date().getTimezoneOffset(),               // Minutes from UTC
          
          // WebGL fingerprint (GPU information)
          // This is another IIFE that tries to get GPU details
          webgl: (() => {
            try {
              // Create an invisible canvas element for WebGL rendering
              const canvas = document.createElement('canvas');
              
              // Get WebGL context (like getting a graphics device in Go)
              const gl = canvas.getContext('webgl') || canvas.getContext('experimental-webgl');
              if (!gl) return null;  // WebGL not supported
              
              // Get debug extension that reveals GPU info
              const debugInfo = gl.getExtension('WEBGL_debug_renderer_info');
              
              return {
                // GPU vendor (e.g., "Intel Inc.")
                vendor: gl.getParameter(debugInfo.UNMASKED_VENDOR_WEBGL),
                // GPU model (e.g., "Intel Iris OpenGL Engine")
                renderer: gl.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL)
              };
            } catch (e) {
              // If anything fails, return null
              return null;
            }
          })()
        };
      }
    });
    
    // Extract the result (executeScript returns array of results)
    const entropy = entropyResult[0].result;
    
    // -------------------------------------------------------------------------
    // Step 7: Capture Turnstile token (if present)
    // -------------------------------------------------------------------------
    // Turnstile tokens are in hidden form fields or embedded in POST bodies
    let turnstileData = null;
    
    try {
      const turnstileResult = await chrome.scripting.executeScript({
        target: { tabId: tab.id },
        func: () => {
          // Look for Turnstile challenge form or iframe
          const turnstileIframe = document.querySelector('iframe[src*="challenges.cf.com"]');
          
          // Look for form with Turnstile data
          const forms = document.querySelectorAll('form');
          let formData = {};
          let foundTurnstile = false;
          
          for (const form of forms) {
            const inputs = form.querySelectorAll('input[type="hidden"]');
            for (const input of inputs) {
              // cf typically uses very long hex names for form fields
              if (input.name.length > 30 && input.value.length > 100) {
                formData[input.name] = input.value;
                foundTurnstile = true;
              }
            }
            
            // Also capture form action
            if (foundTurnstile && form.action) {
              formData['_form_action'] = form.action;
            }
          }
          
          // Extract challenge token from URL if present
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
    // for using the trurnstyle data to capture the cf_clearance cookie to be stored 
    const cfClearanceStored = await chrome.storage.local.get([
      "cfClearanceRaw",
      "cfClearanceCapturedAt",
      "cfClearanceUrl"
    ]);

    
    // Pull Turnstile POST data captured by background.js
    const turnstileStored = await chrome.storage.local.get([
      "turnstilePayload",
      "turnstileCapturedAt",
      "turnstileUrl"
    ]);
    
    // -------------------------------------------------------------------------
    // Step 8: Build the export data structure
    // -------------------------------------------------------------------------
    // This is the final JSON object we'll copy to clipboard
    const exportData = {
      // Timestamp of when data was captured (ISO 8601 format)
      capturedAt: new Date().toISOString(),
      
      // The full URL of the page where data was captured
      url: tab.url,
      
      // The domain name
      domain: domain,
      
      // Protection type (will be determined by Go code)
      type: turnstileData && turnstileData.hasTurnstile ? "turnstile" : "cookie",
      
      // cf-specific cookies (filtered list)
      // .map() transforms each cookie object, keeping only needed fields
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
      
      // ALL cookies from the domain (not just cf ones)
      allCookies: Array.from(allCookies.values()).map(c => ({
        name: c.name,
        value: c.value,
        domain: c.domain,
        path: c.path
      })),
      
      // Turnstile data (if found)
      turnstileToken: turnstileData && turnstileData.hasTurnstile ? "captured" : "",
      turnstileFormData: turnstileData ? turnstileData.formData : {},
      challengeToken: turnstileData ? turnstileData.challengeToken : "",

      // Turnstile POST payload captured by background.js
      turnstileRequestBody: turnstileStored.turnstilePayload || "",
      turnstileRequestUrl: turnstileStored.turnstileUrl || "",
      turnstileCapturedAt: turnstileStored.turnstileCapturedAt || "",

      // cf clearance data, captured by background.js
      cfClearance: cfClearanceStored.cfClearanceRaw || "",
      cfClearanceCapturedAt: cfClearanceStored.cfClearanceCapturedAt || "",
      cfClearanceUrl: cfClearanceStored.cfClearanceUrl || "",


      
      // Browser fingerprint data
      entropy: entropy,
      
      // HTTP headers
      headers: {
        userAgent: navigator.userAgent,
        acceptLanguage: navigator.language
      }
    };
    
    // -------------------------------------------------------------------------
    // Step 8: Convert to JSON and copy to clipboard
    // -------------------------------------------------------------------------
    // JSON.stringify() is like json.Marshal() in Go
    // null, 2 means: no replacer function, indent with 2 spaces (pretty print)
    const jsonData = JSON.stringify(exportData, null, 2);
    
    // Copy to clipboard using the Clipboard API
    // This is async because it might require user permission
    await navigator.clipboard.writeText(jsonData);
    
    // -------------------------------------------------------------------------
    // Step 9: Show success message with protection type
    // -------------------------------------------------------------------------
    statusDiv.className = 'success';  // Apply green "success" styling
    // Show what type of protection was detected
    const protectionType = turnstileData && turnstileData.hasTurnstile ? 
      `Turnstile (${Object.keys(turnstileData.formData).length} tokens)` : 
      `Cookies (${cfCookies.length} CF + ${allCookies.size} total)`;
    
    statusDiv.textContent = `✓ Copied! Protection: ${protectionType}`;
    
    // Show a preview of the captured data (first 300 characters)
    previewDiv.innerHTML = `
      <strong>Preview:</strong>
      <pre>${jsonData.substring(0, 300)}...</pre>
    `;
    
    // Re-enable the button
    button.disabled = false;
    
  } catch (error) {
    // -------------------------------------------------------------------------
    // Error handling
    // -------------------------------------------------------------------------
    // Log to browser console (View with F12 Developer Tools)
    console.error('Error capturing data:', error);
    
    // Show error message to user
    statusDiv.className = 'error';  // Apply red "error" styling
    statusDiv.textContent = `Error: ${error.message}`;
    
    // Re-enable button even on error
    button.disabled = false;
  }
});

// ============================================================================
// AUTO-DETECT: Check if current page is a cf challenge
// ============================================================================
// This runs immediately when the popup opens (not on button click)

// Query for the current active tab
chrome.tabs.query({ active: true, currentWindow: true }, async ([tab]) => {
  // If no tab, just return (might happen on some special pages)
  if (!tab) return;
  
  try {
    // Execute script in the target tab to detect cf challenge
    const result = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => {
        // Check if the page contains cf-related text or elements
        // This returns true/false
        return document.body.textContent.toLowerCase().includes('cf') ||
               document.body.textContent.toLowerCase().includes('checking your browser') ||
               document.querySelector('[class*="cf-"]') !== null;
      }
    });
    
    // If cf challenge was detected, show an info message
    if (result && result[0] && result[0].result) {
      const statusDiv = document.getElementById('status');
      statusDiv.className = 'info';
      statusDiv.textContent = '⚠️ cf challenge detected on this page';
    }
  } catch (e) {
    // Ignore errors - this can happen on special pages like chrome:// URLs
    // where content scripts aren't allowed to run
  }
});