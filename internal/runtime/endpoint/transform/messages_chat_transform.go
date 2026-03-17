package transform

import (
	"io"

	protocolmessages "copilot-proxy/internal/runtime/protocol/messages"
)

const (
	anthropicTypeText       = "text"
	anthropicTypeToolUse    = "tool_use"
	anthropicTypeToolResult = "tool_result"
	anthropicRoleUser       = "user"
	anthropicToolChoice     = "tool"
	anthropicTypeThinking   = "thinking"
	anthropicTypeImage      = "image"
	chatFinishReasonEnd     = "end_turn"
	toolChoiceAuto          = "auto"
	toolChoiceNone          = "none"
)

func MessagesToChatRequest(body []byte) ([]byte, bool) {
	return protocolmessages.MessagesToChatRequest(body)
}

func MessagesToChatRequestWithOptions(body []byte, options MessagesReasoningOptions) ([]byte, bool) {
	return protocolmessages.MessagesToChatRequestWithOptions(body, options)
}

func ChatToMessagesResponse(bodyBytes []byte) ([]byte, bool) {
	return protocolmessages.ChatToMessagesResponse(bodyBytes)
}

func TranslateChatSSEToMessages(body io.ReadCloser) io.ReadCloser {
	return protocolmessages.TranslateChatSSEToMessages(body)
}
