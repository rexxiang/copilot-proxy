package transform

import (
	"io"

	protocolmessages "copilot-proxy/internal/runtime/protocol/messages"
)

const (
	responsesTypeInputText  = "input_text"
	responsesTypeOutputText = "output_text"
)

func MessagesToResponsesRequest(body []byte) ([]byte, bool) {
	return protocolmessages.MessagesToResponsesRequest(body)
}

func MessagesToResponsesRequestWithOptions(body []byte, options MessagesReasoningOptions) ([]byte, bool) {
	return protocolmessages.MessagesToResponsesRequestWithOptions(body, options)
}

func ResponsesToMessagesResponse(body []byte) ([]byte, bool) {
	return protocolmessages.ResponsesToMessagesResponse(body)
}

func TranslateResponsesSSEToMessages(body io.ReadCloser) io.ReadCloser {
	return protocolmessages.TranslateResponsesSSEToMessages(body)
}
