// 本文件负责把前端静态页面挂到本地 HTTP 服务上。
package main

import (
	"embed"
	"net/http"
	"strings"
)

// frontendFiles 保存从 frontend 目录编译进二进制的前端页面。
//
//go:embed frontend/*.html
var frontendFiles embed.FS

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimRight(r.URL.Path, "/")
	if path == "" {
		http.Redirect(w, r, "/debug", http.StatusFound)
		return
	}
	if path == "/debug" || path == "/admin" || path == "/meeting" || strings.HasPrefix(path, "/meeting/") {
		s.serveFrontendHTML(w, r, "index.html")
		return
	}
	http.NotFound(w, r)
}

func (s *Server) serveFrontendHTML(w http.ResponseWriter, r *http.Request, filename string) {
	content, err := frontendFiles.ReadFile("frontend/" + filename)
	if err != nil {
		http.Error(w, "frontend page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(content)
}
