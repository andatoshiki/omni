package telegramhtml

import (
	"bytes"
	stdhtml "html"
	"net/url"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	xhtml "golang.org/x/net/html"
)

var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()),
)

// RenderMarkdown converts CommonMark/GFM plus Telegram's ||spoiler||
// syntax into the restricted HTML dialect accepted by the Telegram Bot API.
func RenderMarkdown(input string) string {
	var rendered bytes.Buffer
	if err := markdownRenderer.Convert([]byte(input), &rendered); err != nil {
		return stdhtml.EscapeString(input)
	}
	return filterTelegramHTML(rendered.String())
}

type telegramHTMLRenderer struct {
	output         strings.Builder
	listDepth      int
	inListItem     bool
	literalDepth   int
	spoilerEnabled bool
	spoilerOpen    bool
}

func filterTelegramHTML(input string) string {
	doc, err := xhtml.Parse(strings.NewReader(input))
	if err != nil {
		return stdhtml.EscapeString(input)
	}

	renderer := &telegramHTMLRenderer{}
	markers := countSpoilerMarkers(doc, false)
	renderer.spoilerEnabled = markers > 0 && markers%2 == 0
	renderer.renderChildren(doc)
	if renderer.spoilerOpen {
		renderer.output.WriteString("</tg-spoiler>")
	}
	return strings.Trim(renderer.output.String(), "\n")
}

func countSpoilerMarkers(node *xhtml.Node, literal bool) int {
	if node.Type == xhtml.ElementNode {
		switch node.Data {
		case "code", "pre", "tg-spoiler":
			literal = true
		case "script", "style", "template", "iframe", "object":
			return 0
		}
	}
	if node.Type == xhtml.TextNode && !literal {
		return strings.Count(node.Data, "||")
	}
	count := 0
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		count += countSpoilerMarkers(child, literal)
	}
	return count
}

func (r *telegramHTMLRenderer) renderChildren(node *xhtml.Node) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		r.render(child)
	}
}

func (r *telegramHTMLRenderer) render(node *xhtml.Node) {
	switch node.Type {
	case xhtml.TextNode:
		r.renderText(node.Data)
	case xhtml.ElementNode:
		r.renderElement(node)
	}
}

func (r *telegramHTMLRenderer) renderText(text string) {
	if r.literalDepth == 0 && strings.Contains(text, "\n") && strings.TrimSpace(text) == "" {
		return
	}
	if r.literalDepth > 0 || !r.spoilerEnabled || !strings.Contains(text, "||") {
		r.output.WriteString(stdhtml.EscapeString(text))
		return
	}

	parts := strings.Split(text, "||")
	for i, part := range parts {
		if i > 0 {
			if r.spoilerOpen {
				r.output.WriteString("</tg-spoiler>")
			} else {
				r.output.WriteString("<tg-spoiler>")
			}
			r.spoilerOpen = !r.spoilerOpen
		}
		r.output.WriteString(stdhtml.EscapeString(part))
	}
}

func (r *telegramHTMLRenderer) renderElement(node *xhtml.Node) {
	switch node.Data {
	case "script", "style", "template", "iframe", "object", "svg", "math":
		return
	case "p":
		r.renderChildren(node)
		if !r.inListItem {
			r.output.WriteString("\n\n")
		}
	case "h1", "h2", "h3", "h4", "h5", "h6":
		r.wrap("b", node)
		r.output.WriteString("\n\n")
	case "strong", "b":
		r.wrap("b", node)
	case "em", "i":
		r.wrap("i", node)
	case "del", "s", "strike":
		r.wrap("s", node)
	case "u", "ins":
		r.wrap("u", node)
	case "tg-spoiler":
		r.output.WriteString("<tg-spoiler>")
		r.literalDepth++
		r.renderChildren(node)
		r.literalDepth--
		r.output.WriteString("</tg-spoiler>")
	case "span":
		if hasClass(node, "tg-spoiler") {
			r.output.WriteString("<tg-spoiler>")
			r.renderChildren(node)
			r.output.WriteString("</tg-spoiler>")
		} else {
			r.renderChildren(node)
		}
	case "a":
		r.renderLink(node)
	case "code":
		r.renderCode(node)
	case "pre":
		r.output.WriteString("<pre>")
		r.literalDepth++
		r.renderChildren(node)
		r.literalDepth--
		r.output.WriteString("</pre>\n\n")
	case "blockquote":
		tag := "blockquote"
		if hasAttribute(node, "expandable") {
			tag += " expandable"
		}
		r.output.WriteString("<" + tag + ">")
		r.renderChildren(node)
		r.output.WriteString("</blockquote>\n\n")
	case "br":
		r.output.WriteByte('\n')
	case "hr":
		r.output.WriteString("────────\n\n")
	case "ul":
		r.renderList(node, false)
	case "ol":
		r.renderList(node, true)
	case "li":
		r.renderChildren(node)
	case "table":
		r.renderTable(node)
	case "input":
		if attribute(node, "type") == "checkbox" {
			if hasAttribute(node, "checked") {
				r.output.WriteString("☑ ")
			} else {
				r.output.WriteString("☐ ")
			}
		}
	case "img":
		r.renderImage(node)
	case "tg-emoji":
		r.renderCustomEmoji(node)
	default:
		r.renderChildren(node)
	}
}

func (r *telegramHTMLRenderer) wrap(tag string, node *xhtml.Node) {
	r.output.WriteString("<" + tag + ">")
	r.renderChildren(node)
	r.output.WriteString("</" + tag + ">")
}

func (r *telegramHTMLRenderer) renderLink(node *xhtml.Node) {
	href := strings.TrimSpace(attribute(node, "href"))
	if !safeTelegramURL(href) {
		r.renderChildren(node)
		return
	}
	r.output.WriteString(`<a href="` + stdhtml.EscapeString(href) + `">`)
	r.renderChildren(node)
	r.output.WriteString("</a>")
}

func safeTelegramURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "tg", "mailto", "tel":
		return true
	default:
		return false
	}
}

func (r *telegramHTMLRenderer) renderCode(node *xhtml.Node) {
	r.output.WriteString("<code")
	if node.Parent != nil && node.Parent.Data == "pre" {
		class := attribute(node, "class")
		if strings.HasPrefix(class, "language-") && safeLanguageName(strings.TrimPrefix(class, "language-")) {
			r.output.WriteString(` class="` + stdhtml.EscapeString(class) + `"`)
		}
	}
	r.output.WriteString(">")
	r.literalDepth++
	r.renderChildren(node)
	r.literalDepth--
	r.output.WriteString("</code>")
}

func safeLanguageName(language string) bool {
	if language == "" {
		return false
	}
	for _, char := range language {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("_+-.", char) {
			continue
		}
		return false
	}
	return true
}

func (r *telegramHTMLRenderer) renderList(node *xhtml.Node, ordered bool) {
	start := 1
	if ordered {
		if parsed, err := strconv.Atoi(attribute(node, "start")); err == nil {
			start = parsed
		}
	}

	r.listDepth++
	index := start
	for item := node.FirstChild; item != nil; item = item.NextSibling {
		if item.Type != xhtml.ElementNode || item.Data != "li" {
			continue
		}
		r.output.WriteString(strings.Repeat("  ", r.listDepth-1))
		if ordered {
			r.output.WriteString(strconv.Itoa(index) + ". ")
			index++
		} else {
			r.output.WriteString("• ")
		}
		wasInListItem := r.inListItem
		r.inListItem = true
		r.renderChildren(item)
		r.inListItem = wasInListItem
		r.output.WriteByte('\n')
	}
	r.listDepth--
	if r.listDepth == 0 {
		r.output.WriteByte('\n')
	}
}

func (r *telegramHTMLRenderer) renderTable(node *xhtml.Node) {
	rows := make([]string, 0)
	collectTableRows(node, &rows)
	if len(rows) == 0 {
		return
	}
	r.output.WriteString("<pre>")
	r.output.WriteString(stdhtml.EscapeString(strings.Join(rows, "\n")))
	r.output.WriteString("</pre>\n\n")
}

func collectTableRows(node *xhtml.Node, rows *[]string) {
	if node.Type == xhtml.ElementNode && node.Data == "tr" {
		cells := make([]string, 0)
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == xhtml.ElementNode && (child.Data == "th" || child.Data == "td") {
				cells = append(cells, strings.TrimSpace(nodeText(child)))
			}
		}
		*rows = append(*rows, strings.Join(cells, " | "))
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectTableRows(child, rows)
	}
}

func nodeText(node *xhtml.Node) string {
	var output strings.Builder
	var walk func(*xhtml.Node)
	walk = func(current *xhtml.Node) {
		if current.Type == xhtml.TextNode {
			output.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return output.String()
}

func (r *telegramHTMLRenderer) renderCustomEmoji(node *xhtml.Node) {
	id := attribute(node, "emoji-id")
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		r.renderChildren(node)
		return
	}
	r.output.WriteString(`<tg-emoji emoji-id="` + id + `">`)
	r.renderChildren(node)
	r.output.WriteString("</tg-emoji>")
}

func (r *telegramHTMLRenderer) renderImage(node *xhtml.Node) {
	alt := attribute(node, "alt")
	parsed, err := url.Parse(attribute(node, "src"))
	if err != nil || parsed.Scheme != "tg" || parsed.Host != "emoji" {
		r.output.WriteString(stdhtml.EscapeString(alt))
		return
	}
	id := parsed.Query().Get("id")
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		r.output.WriteString(stdhtml.EscapeString(alt))
		return
	}
	r.output.WriteString(`<tg-emoji emoji-id="` + id + `">`)
	r.output.WriteString(stdhtml.EscapeString(alt))
	r.output.WriteString("</tg-emoji>")
}

func attribute(node *xhtml.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func hasAttribute(node *xhtml.Node, key string) bool {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

func hasClass(node *xhtml.Node, class string) bool {
	return strings.Contains(" "+attribute(node, "class")+" ", " "+class+" ")
}
