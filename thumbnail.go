package particle

import _ "embed"

// thumbnailPNG is every item's thumbnail: the game's 10.0 icon artwork
// without the logo (Gdp10.0icon_nologo.webp, 1024² scaled to 256² PNG).
//
//go:embed assets/thumbnail.png
var thumbnailPNG []byte
