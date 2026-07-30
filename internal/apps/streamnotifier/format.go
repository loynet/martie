package streamnotifier

import (
	"martie/internal/localization"
	"martie/internal/telegram"
)

type Formatter struct {
	text localization.Localizer
}

type LiveNotice struct {
	PageURL string
}

func NewFormatter(text localization.Localizer) Formatter {
	return Formatter{text: text}
}

func (f Formatter) LiveNotification(stream LiveNotice) telegram.OutgoingMessage {
	title := f.text.Text(localization.TelegramStreamLive, "🔴 Miau stream live")
	return telegram.MarkdownMessage("*" + title + "*\n" + stream.PageURL)
}
