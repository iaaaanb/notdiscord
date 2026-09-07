// Package web embebe el cliente (HTML/CSS/JS) dentro del binario,
// así el servidor final es un solo ejecutable sin archivos sueltos.
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.js style.css
var files embed.FS

// FS expone los archivos estáticos listos para http.FileServerFS.
var FS fs.FS = files
