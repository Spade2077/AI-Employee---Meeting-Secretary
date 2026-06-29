// 本文件负责会议纪要分享页、Markdown 导出和二维码生成。
package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	meetingID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/share/"), "/")
	if meetingID == "" {
		writeError(w, http.StatusNotFound, "missing meeting id")
		return
	}
	if _, err := s.store.GetMeeting(r.Context(), meetingID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.serveFrontendHTML(w, r, "share.html")
}

func (s *Server) handleMinutesMarkdown(w http.ResponseWriter, r *http.Request, meetingID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	markdown, _, err := s.buildMeetingMinutes(r.Context(), meetingID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	filename := "meeting-minutes-" + safeFilename(meetingID) + ".md"
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_, _ = w.Write([]byte(markdown))
}

func (s *Server) handleShareQRCode(w http.ResponseWriter, r *http.Request, meetingID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, err := s.store.GetMeeting(r.Context(), meetingID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	png, err := qrcode.Encode(s.meetingShareURL(r, meetingID), qrcode.Medium, 320)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

type meetingMinutesPage struct {
	Title    string
	Meta     string
	Markdown string
}

func (s *Server) buildMeetingMinutes(ctx context.Context, meetingID string) (string, meetingMinutesPage, error) {
	meeting, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return "", meetingMinutesPage{}, err
	}
	snapshot, err := s.store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		return "", meetingMinutesPage{}, err
	}

	var b bytes.Buffer
	title := strings.TrimSpace(meeting.Title)
	if title == "" {
		title = "会议纪要"
	}
	b.WriteString("# ")
	b.WriteString(markdownLine(title))
	b.WriteString("\n\n")

	b.WriteString("## 基本信息\n\n")
	writeMarkdownBullet(&b, "会议 ID", meeting.ID)
	writeMarkdownBullet(&b, "会议类型", meeting.MeetingType)
	writeMarkdownBullet(&b, "会议状态", meeting.Status)
	writeMarkdownBullet(&b, "开始时间", formatMinutesTime(meeting.StartedAt))
	writeMarkdownBullet(&b, "结束时间", formatMinutesTime(meeting.EndedAt))
	writeMarkdownBullet(&b, "会议时长", formatMinutesDuration(meeting.DurationSeconds))
	b.WriteString("\n")

	b.WriteString("## 会议摘要\n\n")
	if snapshot.Summary != nil && strings.TrimSpace(snapshot.Summary.Content) != "" {
		b.WriteString(strings.TrimSpace(snapshot.Summary.Content))
		b.WriteString("\n\n")
	} else {
		b.WriteString("暂无摘要。\n\n")
	}

	b.WriteString("## 会议决策\n\n")
	if len(snapshot.Decisions) == 0 {
		b.WriteString("暂无决策。\n\n")
	} else {
		for i, decision := range snapshot.Decisions {
			fmt.Fprintf(&b, "%d. %s\n", i+1, markdownLine(decision.Content))
		}
		b.WriteString("\n")
	}

	b.WriteString("## 待办事项\n\n")
	if len(snapshot.ActionItems) == 0 {
		b.WriteString("暂无待办。\n\n")
	} else {
		b.WriteString("| 事项 | 负责人 | 截止时间 | 状态 | 缺失项 |\n")
		b.WriteString("|---|---|---|---|---|\n")
		for _, item := range snapshot.ActionItems {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				markdownTableCell(item.Task),
				markdownTableCell(defaultText(item.Owner, "待定")),
				markdownTableCell(defaultText(item.DueDate, "待定")),
				markdownTableCell(defaultText(item.Status, "open")),
				markdownTableCell(missingFieldLabelsText(item.MissingFields)))
		}
		b.WriteString("\n")
	}

	b.WriteString("## 风险与阻塞\n\n")
	if len(snapshot.Risks) == 0 {
		b.WriteString("暂无风险。\n\n")
	} else {
		b.WriteString("| 风险 | 影响 | 等级 | 负责人 | 处理方案 | 截止时间 | 状态 | 缺失项 |\n")
		b.WriteString("|---|---|---|---|---|---|---|---|\n")
		for _, risk := range snapshot.Risks {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
				markdownTableCell(risk.Content),
				markdownTableCell(defaultText(risk.Impact, "无")),
				markdownTableCell(defaultText(risk.Level, "medium")),
				markdownTableCell(defaultText(risk.Owner, "待定")),
				markdownTableCell(defaultText(risk.Mitigation, "待定")),
				markdownTableCell(defaultText(risk.DueDate, "待定")),
				markdownTableCell(defaultText(risk.Status, "open")),
				markdownTableCell(missingFieldLabelsText(risk.MissingFields)))
		}
		b.WriteString("\n")
	}

	b.WriteString("## 待确认问题\n\n")
	if len(snapshot.OpenQuestions) == 0 {
		b.WriteString("暂无待确认问题。\n\n")
	} else {
		b.WriteString("| 问题 | 跟进人 | 确认时间 | 状态 | 缺失项 |\n")
		b.WriteString("|---|---|---|---|---|\n")
		for _, question := range snapshot.OpenQuestions {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				markdownTableCell(question.Question),
				markdownTableCell(defaultText(question.Owner, "待定")),
				markdownTableCell(defaultText(question.DueDate, "待定")),
				markdownTableCell(defaultText(question.Status, "open")),
				markdownTableCell(missingFieldLabelsText(question.MissingFields)))
		}
		b.WriteString("\n")
	}

	b.WriteString("## 缺失项提醒\n\n")
	if len(snapshot.MissingFieldAlerts) == 0 {
		b.WriteString("暂无缺失项。\n\n")
	} else {
		for _, alert := range snapshot.MissingFieldAlerts {
			fmt.Fprintf(&b, "- %s\n", markdownLine(alert.Message))
		}
		b.WriteString("\n")
	}

	markdown := b.String()
	meta := "会议 ID：" + meeting.ID
	if meeting.EndedAt != "" {
		meta += " · 结束时间：" + formatMinutesTime(meeting.EndedAt)
	}
	return markdown, meetingMinutesPage{Title: title, Meta: meta, Markdown: markdown}, nil
}

func (s *Server) meetingShareURL(r *http.Request, meetingID string) string {
	base := strings.TrimRight(strings.TrimSpace(s.cfg.PublicBaseURL), "/")
	if base == "" {
		base = inferPublicBaseURL(r, s.cfg.Addr)
	}
	return base + "/share/" + url.PathEscape(meetingID)
}

func inferPublicBaseURL(r *http.Request, listenAddr string) string {
	host := r.Host
	if host == "" {
		host = "localhost" + listenPort(listenAddr)
	}
	hostname, port, err := net.SplitHostPort(host)
	if err != nil {
		hostname = host
		port = strings.TrimPrefix(listenPort(listenAddr), ":")
	}
	if isLocalHost(hostname) {
		if ip := firstLANIPv4(); ip != "" {
			hostname = ip
		}
	}
	if port == "" {
		port = strings.TrimPrefix(listenPort(listenAddr), ":")
	}
	return "http://" + net.JoinHostPort(hostname, port)
}

func firstLANIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

func isLocalHost(host string) bool {
	host = strings.Trim(host, "[]")
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func listenPort(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ":8090"
	}
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	_, port, err := net.SplitHostPort(addr)
	if err == nil && port != "" {
		return ":" + port
	}
	if index := strings.LastIndex(addr, ":"); index >= 0 && index < len(addr)-1 {
		return ":" + addr[index+1:]
	}
	return ":8090"
}

func writeMarkdownBullet(b *bytes.Buffer, label, value string) {
	fmt.Fprintf(b, "- %s：%s\n", label, defaultText(value, "无"))
}

func formatMinutesTime(value string) string {
	if value == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func formatMinutesDuration(seconds int64) string {
	if seconds <= 0 {
		return "0 秒"
	}
	minutes := seconds / 60
	remain := seconds % 60
	if minutes == 0 {
		return strconv.FormatInt(remain, 10) + " 秒"
	}
	return fmt.Sprintf("%d 分 %d 秒", minutes, remain)
}

func markdownLine(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
}

func markdownTableCell(value string) string {
	value = markdownLine(value)
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}

func defaultText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func missingFieldLabelsText(fields []string) string {
	if len(fields) == 0 {
		return "无"
	}
	labels := make([]string, 0, len(fields))
	for _, field := range fields {
		labels = append(labels, missingFieldLabel(field))
	}
	return strings.Join(labels, "、")
}

func safeFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "meeting"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", `"`, "-", "<", "-", ">", "-", "|", "-")
	return replacer.Replace(value)
}
