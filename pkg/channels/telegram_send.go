package channels

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const (
	telegramMaxMessageRunes = 4096
	telegramChunkRunes      = 3900
)

func sendOrEditTelegramMessage(
	ctx context.Context,
	bot *telego.Bot,
	chatID int64,
	placeholderID *int,
	content string,
	parseMode string,
) error {
	if placeholderID != nil {
		editMsg := tu.EditMessageText(tu.ID(chatID), *placeholderID, content)
		editMsg.ParseMode = parseMode
		if _, err := bot.EditMessageText(ctx, editMsg); err == nil {
			return nil
		}
	}

	tgMsg := tu.Message(tu.ID(chatID), content)
	tgMsg.ParseMode = parseMode
	_, err := bot.SendMessage(ctx, tgMsg)
	return err
}

func sendTelegramPlainChunks(
	ctx context.Context,
	bot *telego.Bot,
	chatID int64,
	placeholderID *int,
	content string,
) error {
	chunks := splitTelegramMessage(content, telegramChunkRunes)
	if len(chunks) == 0 {
		chunks = []string{" "}
	}

	for i, chunk := range chunks {
		var err error
		if i == 0 {
			err = sendOrEditTelegramMessage(ctx, bot, chatID, placeholderID, chunk, "")
		} else {
			err = sendOrEditTelegramMessage(ctx, bot, chatID, nil, chunk, "")
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func splitTelegramMessage(text string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = telegramChunkRunes
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return []string{}
	}

	runes := []rune(text)
	chunks := make([]string, 0, len(runes)/maxRunes+1)

	for len(runes) > 0 {
		if len(runes) <= maxRunes {
			chunk := strings.TrimSpace(string(runes))
			if chunk != "" {
				chunks = append(chunks, chunk)
			}
			break
		}

		split := bestSplitIndex(runes, maxRunes)
		chunk := strings.TrimSpace(string(runes[:split]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		runes = runes[split:]
	}

	return chunks
}

func bestSplitIndex(runes []rune, maxRunes int) int {
	if len(runes) <= maxRunes {
		return len(runes)
	}

	minSearch := maxRunes / 2

	for i := maxRunes; i >= minSearch; i-- {
		if runes[i-1] == '\n' {
			return i
		}
	}
	for i := maxRunes; i >= minSearch; i-- {
		if runes[i-1] == ' ' || runes[i-1] == '\t' {
			return i
		}
	}

	return maxRunes
}

func runeCount(s string) int {
	return utf8.RuneCountInString(s)
}
