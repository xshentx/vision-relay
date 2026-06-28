package frontend

import "embed"

//go:embed index.html style.css app.js favicon.ico
var FS embed.FS
