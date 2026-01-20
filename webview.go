// webview.go - отдельный файл
package main

import (
    _ "embed"
    
    "github.com/webview/webview_go"
)

//go:embed web/index.html
var indexHTML string

// Если вам нужна файловая система для статических файлов, создайте ее отдельно
// var staticFiles embed.FS  // если будете использовать в будущем

func startEmbeddedGUI(port string) {
    w := webview.New(true)
    defer w.Destroy()
    
    // Убираем обработку статических файлов, так как все в indexHTML
    // Если в будущем понадобятся отдельные файлы, можно будет добавить обратно
    
    w.SetTitle("AI Cogitor")
    w.SetSize(1200, 800, webview.HintNone)
    
    // Загружаем HTML напрямую
    w.SetHtml(indexHTML)
    
    // Настраиваем WebSocket на правильный URL
    w.Bind("connectWebSocket", func() string {
        return "ws://127.0.0.1:" + port + "/api/ws"
    })
    
    w.Run()
}