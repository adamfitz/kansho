# Kansho Browser Extensions

Browser extensions to capture cf cookies and entropy data for use with the Kansho manga downloader.

Supports Chromium-based browsers (Chrome, Brave, Edge) and Firefox.

## Why is this needed?

Some manga sites use cf protection. When Kansho detects this, it will:
1. Open the challenge page in your browser
2. You solve the challenge manually
3. Use this extension to capture the cf data
4. Import it into Kansho to continue downloading

## Installation Instructions

### Chrome / Brave / Edge (Chromium-based browsers)

1. Open your browser's extension page:
   - **Chrome**: Navigate to `chrome://extensions/`
   - **Brave**: Navigate to `brave://extensions/`
   - **Edge**: Navigate to `edge://extensions/`

2. Enable **Developer mode**:
   - Look for a toggle switch in the top-right corner
   - Turn it ON

3. Click **"Load unpacked"** button

4. Navigate to the Kansho project folder and select:
   ```
   kansho/extensions/chrome/
   ```

5. The extension should now appear in your extensions list

6. **Pin the extension** (recommended):
   - Click the puzzle piece icon in your browser toolbar
   - Find "Kansho cf Helper"
   - Click the pin icon to keep it visible

### Firefox

The extension can be installed permanently (survives restarts) using **Firefox Developer Edition**, which allows installing unsigned extensions.

1. Install **Firefox Developer Edition** from [mozilla.org](https://www.mozilla.org/en-US/firefox/developer/)

2. Open Firefox Developer Edition and go to:
   ```
   about:config
   ```

3. Search for `xpinstall.signatures.required` and set it to **false**

4. Package the extension:
   ```bash
   cd extensions/firefox
   zip -r ../kansho-cf-helper.xpi * -x "*.gitkeep"
   ```

5. Open `about:addons`, click the gear icon ⚙️ → **Install Add-on From File**, and select the `.xpi` file

6. The extension is now installed permanently — it will survive browser restarts

**Note**: Temporary loading (for quick testing) also works:
- Navigate to `about:debugging#/runtime/this-firefox`
- Click **"Load Temporary Add-on..."**
- Select `kansho/extensions/firefox/manifest.json`
- ⚠️ Temporary add-ons are removed when Firefox restarts

## How to Use

1. When Kansho detects a cf challenge, it will automatically open the page in your browser

2. Complete the cf challenge (checkbox, CAPTCHA, etc.)

3. Once you're on the actual manga page (behind cf), click the **Kansho extension icon** in your browser toolbar

4. Click **"Copy cf Data"** button

5. You should see a success message: "✓ Copied! Found X cf cookies..."

6. Return to Kansho and click **"Import cf Data"** (we'll add this button in the next step)

7. The data will be imported and Kansho can continue downloading

## What Data is Captured?

The extension captures:
- **cf cookies** (cf_clearance, __cf_bm, etc.)
- **All cookies** from the domain (some sites use additional cookies)
- **Browser fingerprint data**:
  - User agent
  - Screen resolution
  - WebGL renderer
  - Timezone
  - Language settings
  - Hardware info

This data is used to make Kansho's requests appear identical to your browser, bypassing cf's bot detection.

## Privacy & Security

- ✅ All data stays **local** - nothing is sent to external servers
- ✅ Data is only captured when **you click the button**
- ✅ The extension **only runs when you activate it**
- ✅ You can review the source code in this folder
- ✅ Data is copied to your clipboard and immediately used by Kansho

## Troubleshooting

**Extension doesn't appear after installation:**
- Make sure Developer Mode is enabled
- Try restarting your browser
- Check the browser console for errors

**"No cf cookies found":**
- Make sure you've completed the cf challenge
- Verify you're on the actual site (not still on the challenge page)
- Try refreshing the page and clicking the extension again

**Firefox: Extension disappears after restart:**
- This is normal for temporary add-ons
- Reload it using the same steps above
- Consider packaging it as a permanent extension (advanced)

## Development

To modify the extension:

1. Edit the files in `chrome/` or `firefox/`
2. For Chrome/Brave: Click the refresh icon on the extension card
3. For Firefox: Click "Reload" in about:debugging

The extension is intentionally simple and has no external dependencies.