// 本文件负责把前端静态页面挂到本地 HTTP 服务上。
package main

import (
	"embed"
	"net/http"
	"os"
	"path/filepath"
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
	if path == "/debug" {
		s.serveFrontendHTML(w, r, "index.html")
		return
	}
	if path == "/meeting" || strings.HasPrefix(path, "/meeting/") {
		s.serveMeetingWeb(w, r)
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

func (s *Server) serveMeetingWeb(w http.ResponseWriter, r *http.Request) {
	distRoot := filepath.Join("meeting-web", "dist")
	indexPath := filepath.Join(distRoot, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		http.Error(w, "meeting-web dist not found, run npm run build in meeting-web first", http.StatusNotFound)
		return
	}

	requestPath := strings.TrimPrefix(r.URL.Path, "/meeting")
	if requestPath == "" || requestPath == "/" {
		http.ServeFile(w, r, indexPath)
		return
	}

	cleanPath := filepath.Clean(strings.TrimPrefix(requestPath, "/"))
	candidate := filepath.Join(distRoot, cleanPath)
	rootAbs, rootErr := filepath.Abs(distRoot)
	candidateAbs, candidateErr := filepath.Abs(candidate)
	if rootErr != nil || candidateErr != nil || !isPathInside(candidateAbs, rootAbs) {
		http.NotFound(w, r)
		return
	}

	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		http.ServeFile(w, r, candidate)
		return
	}
	http.ServeFile(w, r, indexPath)
}

func isPathInside(candidate, root string) bool {
	if candidate == root {
		return true
	}
	rootWithSeparator := strings.TrimRight(root, string(os.PathSeparator)) + string(os.PathSeparator)
	return strings.HasPrefix(candidate, rootWithSeparator)
}
