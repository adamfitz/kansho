/**
 * Kansho Cloudflare Helper - Browser Extension
 * 
 * This script runs when the user clicks the extension icon and opens the popup.
 * It captures Cloudflare cookies and browser fingerprint data from the current tab,
 * then copies it to the clipboard in JSON format for use by the Kansho application.
 */

// ============================================================================
// MAIN FUNCTIONALITY: Capture Button Click Handler
// ============================================================================

// Get the "Copy Cloudflare Data" button from popup.html and add a click listener
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
    // We might get the same cookie from both queries, so deduplicate using a Map
    // Map is like Go's map[string]Cookie - key is "name-domain", value is cookie object
    const allCookies = [...cookies, ...parentCookies].reduce((acc, cookie) => {
      // Create a unique key for each cookie (name + domain combination)
      const key = `${cookie.name}-${cookie.domain}`;
      
      // Only add if we haven't seen this cookie before
      if (!acc.has(key)) {
        acc.set(key, cookie);  // Map.set() is like Go's map[key] = value
      }
      return acc;  // Return the accumulator for the next iteration
    }, new Map());  // Start with an empty Map
    
    // -------------------------------------------------------------------------
    // Step 5: Filter for Cloudflare-specific cookies
    // -------------------------------------------------------------------------
    // Array.from() converts the Map to an array (like converting Go map to slice)
    // .values() gets just the cookie objects (not the keys)
    // .filter() is like Go's for loop with append if condition is true
    const cfCookies = Array.from(allCookies.values()).filter(cookie => 
      // Check if cookie name contains Cloudflare-related strings
      cookie.name.toLowerCase().includes('cf_') ||          // cf_clearance, cf_chl_*, etc.
      cookie.name.toLowerCase().includes('clearance') ||    // Alternative naming
      cookie.name === '__cfduid' ||                         // Old Cloudflare cookie
      cookie.name === '__cf_bm'                             // Cloudflare Bot Management
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
    // Step 7: Build the export data structure
    // -------------------------------------------------------------------------
    // This is the final JSON object we'll copy to clipboard
    const exportData = {
      // Timestamp of when data was captured (ISO 8601 format)
      capturedAt: new Date().toISOString(),
      
      // The full URL of the page where data was captured
      url: tab.url,
      
      // The domain name
      domain: domain,
      
      // Cloudflare-specific cookies (filtered list)
      // .map() transforms each cookie object, keeping only needed fields
      // It's like a for loop that creates a new slice in Go
      cookies: cfCookies.map(c => ({
        name: c.name,                      // Cookie name
        value: c.value,                    // Cookie value (the important part!)
        domain: c.domain,                  // Which domain it belongs to
        path: c.path,                      // URL path where cookie is valid
        secure: c.secure,                  // HTTPS only?
        httpOnly: c.httpOnly,              // JavaScript can't access it?
        sameSite: c.sameSite,              // CSRF protection setting
        expirationDate: c.expirationDate   // When it expires (Unix timestamp)
      })),
      
      // ALL cookies from the domain (not just Cloudflare ones)
      // Some sites use additional cookies for authentication
      allCookies: Array.from(allCookies.values()).map(c => ({
        name: c.name,
        value: c.value,
        domain: c.domain,
        path: c.path
      })),
      
      // Browser fingerprint data captured from the tab
      // entropy already contains the result object
      entropy: entropy,
      
      // HTTP headers that we can construct from JavaScript
      headers: {
        userAgent: navigator.userAgent,      // User-Agent header
        acceptLanguage: navigator.language   // Accept-Language header
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
    // Step 9: Show success message
    // -------------------------------------------------------------------------
    statusDiv.className = 'success';  // Apply green "success" styling
    // Template string (like fmt.Sprintf in Go) - note the backticks
    statusDiv.textContent = `✓ Copied! Found ${cfCookies.length} Cloudflare cookies and ${allCookies.size} total cookies`;
    
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
// AUTO-DETECT: Check if current page is a Cloudflare challenge
// ============================================================================
// This runs immediately when the popup opens (not on button click)

// Query for the current active tab
chrome.tabs.query({ active: true, currentWindow: true }, async ([tab]) => {
  // If no tab, just return (might happen on some special pages)
  if (!tab) return;
  
  try {
    // Execute script in the target tab to detect Cloudflare challenge
    const result = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => {
        // Check if the page contains Cloudflare-related text or elements
        // This returns true/false
        return document.body.textContent.toLowerCase().includes('cloudflare') ||
               document.body.textContent.toLowerCase().includes('checking your browser') ||
               document.querySelector('[class*="cf-"]') !== null;
      }
    });
    
    // If Cloudflare challenge was detected, show an info message
    if (result && result[0] && result[0].result) {
      const statusDiv = document.getElementById('status');
      statusDiv.className = 'info';
      statusDiv.textContent = '⚠️ Cloudflare challenge detected on this page';
    }
  } catch (e) {
    // Ignore errors - this can happen on special pages like chrome:// URLs
    // where content scripts aren't allowed to run
  }
});