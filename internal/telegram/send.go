package telegram

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// chunkMessage splits a message into chunks that fit within Telegram's message size limit.
func chunkMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		// Try to split at a newline
		cutAt := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/2 {
			cutAt = idx + 1
		}

		chunks = append(chunks, text[:cutAt])
		text = text[cutAt:]
	}

	return chunks
}

// MarkdownV2 special characters that must be escaped outside formatting entities.
const mdV2Special = `_*[]()~` + "`" + `>#+-=|{}.!`

// stripMarkdownEscapes removes existing Markdown backslash escapes so that
// agent output like `photo\_2024` doesn't double-escape to `\\_2024`.
func stripMarkdownEscapes(text string) string {
	var sb strings.Builder
	sb.Grow(len(text))
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) && strings.ContainsRune(mdV2Special, runes[i+1]) {
			// Skip the backslash, keep the character (it will be re-escaped properly)
			continue
		}
		sb.WriteRune(runes[i])
	}
	return sb.String()
}

// escapeMarkdownV2 escapes all MarkdownV2 special characters in plain text.
// It first strips existing Markdown escapes to prevent double-escaping.
func escapeMarkdownV2(text string) string {
	text = stripMarkdownEscapes(text)
	var sb strings.Builder
	sb.Grow(len(text) + len(text)/4)
	for _, r := range text {
		if strings.ContainsRune(mdV2Special, r) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

var (
	reBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldSingle = regexp.MustCompile(`(?:^|[^*\\])\*([^*\n]+?)\*(?:[^*]|$)`)
	reHeader   = regexp.MustCompile(`^#{1,6}\s+(.+)$`)
	reHR       = regexp.MustCompile(`^-{3,}$`)
	reBullet   = regexp.MustCompile(`^(\s*)[-*]\s+`)
	reItalic   = regexp.MustCompile(`(?:^|[^\\])_(.+?)_`)
	reImageEmb = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	reLink     = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reInline   = regexp.MustCompile("`[^`]+`")
)

// toTelegramMarkdown converts standard Markdown to Telegram MarkdownV2.
// It converts bold, headers, horizontal rules, bullet lists, links, and image embeds,
// while preserving content inside code blocks (``` ... ```).
func toTelegramMarkdown(text string) string {
	text = convertMarkdownTables(text)

	// Split text into code blocks and non-code segments to protect code blocks.
	parts := strings.Split(text, "```")
	for i := 0; i < len(parts); i++ {
		if i%2 == 1 {
			// Inside a code block — escape only ` and \ for MarkdownV2
			parts[i] = escapeCodeBlock(parts[i])
			continue
		}
		parts[i] = convertMarkdownSegment(parts[i])
	}
	return strings.Join(parts, "```")
}

// escapeCodeBlock escapes characters inside a code block for MarkdownV2.
// Only backtick and backslash need escaping inside code blocks.
func escapeCodeBlock(text string) string {
	// Backticks inside ``` blocks are rare but must be escaped.
	// Backslashes must also be escaped.
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, "`", "\\`")
	return text
}

// convertMarkdownSegment applies Markdown-to-MarkdownV2 conversions on a
// segment that is known to be outside code blocks.
func convertMarkdownSegment(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Headers: ## Title → *escaped(Title)*
		if m := reHeader.FindStringSubmatch(trimmed); m != nil {
			lines[i] = "*" + escapeInlineMarkdown(m[1]) + "*"
			continue
		}

		// Horizontal rules: --- → empty line
		if reHR.MatchString(trimmed) {
			lines[i] = ""
			continue
		}

		// Bullet lists: - item / * item → • escaped(rest) (preserve indentation)
		if loc := reBullet.FindStringSubmatchIndex(line); loc != nil {
			indent := line[loc[2]:loc[3]]
			rest := line[loc[1]:]
			lines[i] = indent + "• " + escapeInlineMarkdown(rest)
			continue
		}

		// Regular line — escape with inline formatting preserved
		lines[i] = escapeInlineMarkdown(line)
	}
	return strings.Join(lines, "\n")
}

// escapeInlineMarkdown escapes a line for MarkdownV2 while preserving
// inline formatting: **bold**, [text](url), ![alt](url), `inline code`.
func escapeInlineMarkdown(line string) string {
	// Find all protected spans (bold, links, images, inline code) and their positions.
	var spans []span

	// Image embeds: ![alt](url) → [escaped(alt)](url)
	for _, m := range reImageEmb.FindAllStringSubmatchIndex(line, -1) {
		alt := line[m[2]:m[3]]
		url := line[m[4]:m[5]]
		spans = append(spans, span{m[0], m[1], "[" + escapeMarkdownV2(alt) + "](" + escapeURL(url) + ")"})
	}

	// Regular links: [text](url) — only if not already captured as image
	for _, m := range reLink.FindAllStringSubmatchIndex(line, -1) {
		// Skip if this is part of an image embed (preceded by !)
		if m[0] > 0 && line[m[0]-1] == '!' {
			continue
		}
		// Skip if overlaps with existing span
		if overlaps(spans, m[0], m[1]) {
			continue
		}
		text := line[m[2]:m[3]]
		url := line[m[4]:m[5]]
		spans = append(spans, span{m[0], m[1], "[" + escapeMarkdownV2(text) + "](" + escapeURL(url) + ")"})
	}

	// Bold: **text** → *escaped(text)*
	for _, m := range reBold.FindAllStringSubmatchIndex(line, -1) {
		if overlaps(spans, m[0], m[1]) {
			continue
		}
		inner := line[m[2]:m[3]]
		spans = append(spans, span{m[0], m[1], "*" + escapeMarkdownV2(inner) + "*"})
	}

	// Single-star bold: *text* → *escaped(text)* (already valid MarkdownV2 bold)
	for _, m := range reBoldSingle.FindAllStringSubmatchIndex(line, -1) {
		// m[0]:m[1] is full match (may include surrounding char), m[2]:m[3] is inner text
		start := strings.Index(line[m[0]:m[1]], "*") + m[0]
		end := strings.LastIndex(line[m[0]:m[1]], "*") + m[0] + 1
		if overlaps(spans, start, end) {
			continue
		}
		inner := line[m[2]:m[3]]
		spans = append(spans, span{start, end, "*" + escapeMarkdownV2(inner) + "*"})
	}

	// Italic: _text_ → _escaped(text)_
	for _, m := range reItalic.FindAllStringSubmatchIndex(line, -1) {
		// m[0]:m[1] is full match (may include preceding char), m[2]:m[3] is inner text
		// Find the actual underscore positions
		start := strings.Index(line[m[0]:m[1]], "_") + m[0]
		end := m[1]
		if overlaps(spans, start, end) {
			continue
		}
		inner := line[m[2]:m[3]]
		spans = append(spans, span{start, end, "_" + escapeMarkdownV2(inner) + "_"})
	}

	// Inline code: `code` — preserve as-is, escape inside
	for _, m := range reInline.FindAllStringIndex(line, -1) {
		if overlaps(spans, m[0], m[1]) {
			continue
		}
		code := line[m[0]+1 : m[1]-1]
		code = strings.ReplaceAll(code, `\`, `\\`)
		code = strings.ReplaceAll(code, "`", "\\`")
		spans = append(spans, span{m[0], m[1], "`" + code + "`"})
	}

	// Build result: escape gaps between spans, insert span replacements
	// Sort spans by start position (they should already be in order from regex)
	sortSpans(spans)

	var sb strings.Builder
	pos := 0
	for _, s := range spans {
		if s.start > pos {
			sb.WriteString(escapeMarkdownV2(line[pos:s.start]))
		}
		sb.WriteString(s.replacement)
		pos = s.end
	}
	if pos < len(line) {
		sb.WriteString(escapeMarkdownV2(line[pos:]))
	}
	return sb.String()
}

// escapeURL escapes only `)` and `\` inside a MarkdownV2 link URL.
func escapeURL(url string) string {
	url = strings.ReplaceAll(url, `\`, `\\`)
	url = strings.ReplaceAll(url, ")", "\\)")
	return url
}

type span struct {
	start, end  int
	replacement string
}

func overlaps(spans []span, start, end int) bool {
	for _, s := range spans {
		if start < s.end && end > s.start {
			return true
		}
	}
	return false
}

func sortSpans(spans []span) {
	// Simple insertion sort — few spans per line
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j].start < spans[j-1].start; j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}
}

// convertMarkdownTables finds Markdown tables and converts them to
// pre-formatted monospace blocks for Telegram.
func convertMarkdownTables(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	i := 0
	for i < len(lines) {
		// Detect start of a table: line with pipes
		if isTableRow(lines[i]) && i+1 < len(lines) && isSeparatorRow(lines[i+1]) {
			// Collect all table lines
			var rows [][]string
			header := parseTableRow(lines[i])
			rows = append(rows, header)
			i++ // skip header
			i++ // skip separator
			for i < len(lines) && isTableRow(lines[i]) {
				rows = append(rows, parseTableRow(lines[i]))
				i++
			}
			result = append(result, formatTable(rows))
			continue
		}
		result = append(result, lines[i])
		i++
	}
	return strings.Join(result, "\n")
}

func isTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|")
}

func isSeparatorRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	for _, ch := range trimmed {
		if ch != '|' && ch != '-' && ch != ':' && ch != ' ' {
			return false
		}
	}
	return true
}

func parseTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	// Remove leading/trailing pipes
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	for i, p := range parts {
		cell := strings.TrimSpace(p)
		// Strip bold markers (**text**) since pre blocks render them literally
		cell = reBold.ReplaceAllString(cell, "$1")
		parts[i] = cell
	}
	return parts
}

func formatTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	// Calculate column widths
	cols := len(rows[0])
	widths := make([]int, cols)
	for _, row := range rows {
		for j := 0; j < cols && j < len(row); j++ {
			w := utf8.RuneCountInString(row[j])
			if w > widths[j] {
				widths[j] = w
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("```\n")
	for ri, row := range rows {
		for j := 0; j < cols; j++ {
			cell := ""
			if j < len(row) {
				cell = row[j]
			}
			if j > 0 {
				sb.WriteString(" │ ")
			}
			sb.WriteString(cell)
			// Pad with spaces (using rune count for correct alignment)
			for k := utf8.RuneCountInString(cell); k < widths[j]; k++ {
				sb.WriteByte(' ')
			}
		}
		sb.WriteByte('\n')
		// Draw separator after header
		if ri == 0 {
			for j := 0; j < cols; j++ {
				if j > 0 {
					sb.WriteString("─┼─")
				}
				for k := 0; k < widths[j]; k++ {
					sb.WriteString("─")
				}
			}
			sb.WriteByte('\n')
		}
	}
	sb.WriteString("```")
	return sb.String()
}
