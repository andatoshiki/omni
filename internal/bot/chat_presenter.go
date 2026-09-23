package bot

import (
	"context"
	stdhtml "html"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
)

const (
	// Provider output is HTML-escaped before it is sent. These conservative raw
	// limits keep the shared callout under Telegram's message limit even when
	// every character expands during HTML escaping.
	reasoningPreviewLimit = 600
	previewFlushInterval  = 250 * time.Millisecond
)

type previewPhase int

const (
	previewReasoning previewPhase = iota
	previewAnswering
)

// chatStreamPresenter owns the temporary Telegram UI for one generation. It
// deliberately never exposes reasoning text to persistence or finalization.
type chatStreamPresenter struct {
	handler *CommandHandler
	ctx     context.Context
	source  *models.Message

	phase           previewPhase
	reply           *models.Message
	reasoning       string
	answer          string
	reasoningCapped bool
	lastHTML        string
	lastUpdate      time.Time
	updateInterval  time.Duration
	updatesDisabled bool
	previewCapped   bool
}

func newChatStreamPresenter(handler *CommandHandler, ctx context.Context, source *models.Message) *chatStreamPresenter {
	interval := minReplyIntervalPrivateChat
	if source != nil && source.Chat.ID < 0 {
		interval = minReplyIntervalGroupChat
	}
	return &chatStreamPresenter{
		handler:        handler,
		ctx:            ctx,
		source:         source,
		phase:          previewReasoning,
		updateInterval: interval,
	}
}

func (p *chatStreamPresenter) start() {
	if p == nil || p.source == nil {
		return
	}
	p.sendInitialEditable(p.thinkingHTML())
}

func (p *chatStreamPresenter) sendInitialEditable(html string) {
	var (
		reply *models.Message
		err   error
	)
	if p.source.Chat.ID < 0 {
		reply, err = p.handler.app.sendHTMLReplyToMessage(p.ctx, p.source, html)
	} else {
		reply, err = p.handler.app.sendHTMLInThread(p.ctx, p.source.Chat.ID, p.source.MessageThreadID, html)
	}
	if err == nil {
		p.reply = reply
		p.lastHTML = html
	}
	p.lastUpdate = time.Now()
}

func (p *chatStreamPresenter) appendReasoning(delta string) {
	if p == nil || p.phase == previewAnswering || delta == "" {
		return
	}
	p.reasoning += delta
	if len(p.reasoning) > reasoningPreviewLimit {
		p.reasoning = tailUTF8(p.reasoning, reasoningPreviewLimit)
		p.reasoningCapped = true
	}
	p.flushThinking(false)
}

func (p *chatStreamPresenter) showAnswer(answer string) {
	if p == nil || answer == "" {
		return
	}
	force := p.phase != previewAnswering
	p.phase = previewAnswering
	p.answer = answer
	p.flushAnswer(answer, force)
}

func (p *chatStreamPresenter) flushLatest(force bool) {
	if p == nil {
		return
	}
	if p.phase == previewAnswering {
		p.flushAnswer(p.answer, force)
		return
	}
	p.flushThinking(force)
}

func (p *chatStreamPresenter) flushThinking(force bool) {
	if p.updatesDisabled || p.phase == previewAnswering {
		return
	}
	if !force && time.Since(p.lastUpdate) < p.updateInterval {
		return
	}
	p.sendPreviewHTML(p.thinkingHTML())
}

func (p *chatStreamPresenter) flushAnswer(answer string, force bool) {
	if p.updatesDisabled {
		return
	}
	if !force && time.Since(p.lastUpdate) < p.updateInterval {
		return
	}
	p.sendPreviewHTML(p.answerHTML(answer))
}

func (p *chatStreamPresenter) answerHTML(answer string) string {
	preview, capped := escapedPrefix(answer, streamPreviewLimit-100)
	if capped {
		preview += "\n\n⏳ Response is long; sending it in full when complete…"
		p.previewCapped = true
	}
	return preview
}

func (p *chatStreamPresenter) sendPreviewHTML(html string) {
	if html == p.lastHTML {
		return
	}
	if p.reply == nil {
		p.sendInitialEditable(html)
		return
	}
	reply, err := p.handler.app.editMessageHTML(p.ctx, p.reply, html)
	if err == nil {
		p.reply = reply
		p.lastHTML = html
		p.lastUpdate = time.Now()
		return
	}

	// Once answer text exists, a stale thinking callout is worse than losing the
	// live preview. Remove it and try one fresh message before waiting for final.
	if p.phase == previewAnswering {
		_, _ = p.handler.app.deleteMessage(p.ctx, p.reply)
		p.reply = nil
		p.sendInitialEditable(html)
		return
	}
	p.updatesDisabled = true
}

func (p *chatStreamPresenter) thinkingHTML() string {
	body := "Waiting for the model…"
	if strings.TrimSpace(p.reasoning) != "" {
		body = p.reasoning
		if p.reasoningCapped {
			body = "… Earlier thinking omitted …\n" + body
		}
	}
	body = stdhtml.EscapeString(body)
	return "💭 <b>Thinking…</b>\n\n<blockquote expandable>" + body + "</blockquote>"
}

func (p *chatStreamPresenter) fail(text string) {
	if p == nil || p.source == nil {
		return
	}
	if p.reply != nil {
		if reply, err := p.handler.app.editReplyToMessage(p.ctx, p.reply, text); err == nil {
			p.reply = reply
			return
		}
		_, _ = p.handler.app.deleteMessage(p.ctx, p.reply)
	}
	_, _ = p.handler.reply(p.ctx, p.source, text)
}

func (p *chatStreamPresenter) finish(text string) {
	if p == nil || p.source == nil || text == "" {
		return
	}
	chunks := splitText(text, streamPreviewLimit)
	start := 0
	if p.reply != nil {
		first, err := p.handler.app.editReplyToMessage(p.ctx, p.reply, chunks[0])
		if err == nil {
			p.handler.saveAssistantTranscriptMessage(p.source, first, chunks[0])
			start = 1
		} else {
			_, _ = p.handler.app.deleteMessage(p.ctx, p.reply)
		}
	}
	for _, chunk := range chunks[start:] {
		var (
			sent *models.Message
			err  error
		)
		if p.source.Chat.ID >= 0 {
			sent, err = p.handler.app.sendMessageInThread(p.ctx, p.source.Chat.ID, p.source.MessageThreadID, chunk)
		} else {
			sent, err = p.handler.app.sendReplyToMessage(p.ctx, p.source, chunk)
		}
		if err == nil {
			p.handler.saveAssistantTranscriptMessage(p.source, sent, chunk)
		}
	}
}

func tailUTF8(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	start := len(text) - maxBytes
	for start < len(text) && !isUTF8Boundary(text[start]) {
		start++
	}
	return text[start:]
}

func escapedPrefix(text string, maxBytes int) (string, bool) {
	if maxBytes <= 0 {
		return "", text != ""
	}
	var output strings.Builder
	for _, character := range text {
		escaped := stdhtml.EscapeString(string(character))
		if output.Len()+len(escaped) > maxBytes {
			return output.String(), true
		}
		output.WriteString(escaped)
	}
	return output.String(), false
}
