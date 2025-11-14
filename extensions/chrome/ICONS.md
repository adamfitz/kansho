# Extension Icons

The extension requires three icon sizes:
- `icon16.png` (16x16 pixels)
- `icon48.png` (48x48 pixels)
- `icon128.png` (128x128 pixels)

## Quick Solution: Use Placeholder Icons

For testing, you can create simple colored squares:

### Using ImageMagick (Linux/Mac):
```bash
cd extensions/chrome/
convert -size 16x16 xc:#5e35b1 icon16.png
convert -size 48x48 xc:#5e35b1 icon48.png
convert -size 128x128 xc:#5e35b1 icon128.png
```

### Using GIMP or any image editor:
1. Create a new image with the specified dimensions
2. Fill it with the Kansho purple color (#5e35b1)
3. Optionally add text like "K" or "鑑賞"
4. Save as PNG

### Online Icon Generator:
- Use https://www.favicon-generator.org/
- Upload any image
- Download the PNG files and rename them

## For Production:

Design a proper icon that represents Kansho:
- Use the Kansho logo or kanji (鑑賞)
- Keep it simple and recognizable at small sizes
- Use the purple theme color (#5e35b1)
- Make sure it's readable at 16x16 pixels

The Firefox version uses the same icons, so just copy them to `extensions/firefox/` when ready.