package transform

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"
	"strings"
)

const (
	directResponsesUserMaxLen   = 64
	directResponsesUserHashLen  = 32
	responsesMaxOutputTokensMin = 16
	responsesReasoningPartsCap  = 4
	responsesTypeInputText      = "input_text"
	responsesTypeOutputText     = "output_text"
)

type anthropicResponsesRequest struct {
	Model         string                    `json:"model"`
	Messages      []anthropicResponsesInput `json:"messages"`
	System        any                       `json:"system,omitempty"`
	MaxTokens     *int                      `json:"max_tokens,omitempty"`
	Temperature   *float64                  `json:"temperature,omitempty"`
	TopP          *float64                  `json:"top_p,omitempty"`
	StopSequences []string                  `json:"stop_sequences,omitempty"`
	Stream        bool                      `json:"stream,omitempty"`
	Tools         any                       `json:"tools,omitempty"`
	ToolChoice    any                       `json:"tool_choice,omitempty"`
	OutputConfig  map[string]any            `json:"output_config,omitempty"`
	Metadata      map[string]any            `json:"metadata,omitempty"`
}

type anthropicResponsesInput struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func MessagesToResponsesRequest(body []byte) ([]byte, bool) {
	var req anthropicResponsesRequest
	if json.Unmarshal(body, &req) != nil {
		return nil, false
	}

	out := map[string]any{
		"model": req.Model,
		"input": buildResponsesInputFromMessages(req.Messages),
	}
	if req.System != nil {
		if instructions := convertAnthropicSystemForResponses(req.System); instructions != "" {
			out["instructions"] = instructions
		}
	}
	if maxOutputTokens, ok := sanitizeResponsesMaxOutputTokens(req.MaxTokens); ok {
		out["max_output_tokens"] = maxOutputTokens
	}
	if req.Temperature != nil {
		out["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		out["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		out["stop"] = req.StopSequences
	}
	if req.Stream {
		out["stream"] = true
	}
	if req.Metadata != nil {
		if userID, ok := req.Metadata["user_id"].(string); ok && userID != "" {
			out["user"] = normalizeDirectResponsesUser(userID)
		}
	}
	if req.Tools != nil {
		out["tools"] = convertAnthropicToolsToResponses(req.Tools)
	}
	if req.ToolChoice != nil {
		if choice, ok := convertAnthropicToolChoiceToResponses(req.ToolChoice); ok {
			out["tool_choice"] = choice
		}
	}
	if reasoning, ok := buildResponsesReasoningFromOutputConfig(req.OutputConfig); ok {
		out["reasoning"] = reasoning
	}

	updated, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return updated, true
}

func buildResponsesInputFromMessages(messages []anthropicResponsesInput) []any {
	input := make([]any, 0, len(messages))
	for _, msg := range messages {
		appendResponsesInputFromMessage(&input, msg)
	}
	return input
}

func appendResponsesInputFromMessage(input *[]any, msg anthropicResponsesInput) {
	if input == nil {
		return
	}
	role := normalizeResponsesMessageRole(msg.Role)
	if role == "" {
		return
	}

	switch content := msg.Content.(type) {
	case string:
		if content == "" {
			return
		}
		*input = append(*input, map[string]any{
			"role": role,
			"content": []any{
				map[string]any{
					"type": responsesTextBlockTypeForRole(role),
					"text": content,
				},
			},
		})
	case []any:
		appendResponsesInputFromBlocks(input, role, content)
	}
}

func appendResponsesInputFromBlocks(input *[]any, role string, blocks []any) {
	messageContent := make([]any, 0, len(blocks))
	flushMessage := func() {
		if len(messageContent) == 0 {
			return
		}
		*input = append(*input, map[string]any{
			"role":    role,
			"content": messageContent,
		})
		messageContent = make([]any, 0, len(blocks))
	}

	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch blockType {
		case anthropicTypeText:
			if text, ok := block["text"].(string); ok && text != "" {
				messageContent = append(messageContent, map[string]any{
					"type": responsesTextBlockTypeForRole(role),
					"text": text,
				})
			}
		case anthropicTypeThinking:
			thinking, _ := block["thinking"].(string)
			if strings.TrimSpace(thinking) != "" {
				messageContent = append(messageContent, map[string]any{
					"type": responsesTextBlockTypeForRole(role),
					"text": thinking,
				})
			}
		case anthropicTypeImage:
			if image, ok := convertAnthropicImageBlockToResponses(role, block); ok {
				messageContent = append(messageContent, image)
			}
		case anthropicTypeToolUse:
			flushMessage()
			if call, ok := convertAnthropicToolUseToResponses(block); ok {
				*input = append(*input, call)
			}
		case "tool_result":
			flushMessage()
			if output, ok := convertAnthropicToolResultToResponses(block); ok {
				*input = append(*input, output)
			}
		case responsesTypeInputText:
			block["type"] = responsesTextBlockTypeForRole(role)
			messageContent = append(messageContent, block)
		case "input_image":
			if !strings.EqualFold(role, "assistant") {
				messageContent = append(messageContent, block)
			}
		case responsesTypeOutputText:
			if strings.EqualFold(role, "assistant") {
				messageContent = append(messageContent, block)
				continue
			}
			block["type"] = responsesTypeInputText
			messageContent = append(messageContent, block)
		case "refusal":
			if strings.EqualFold(role, "assistant") {
				messageContent = append(messageContent, block)
			}
		}
	}
	flushMessage()
}

func responsesTextBlockTypeForRole(role string) string {
	if strings.EqualFold(role, "assistant") {
		return responsesTypeOutputText
	}
	return responsesTypeInputText
}

func normalizeResponsesMessageRole(role string) string {
	trimmed := strings.TrimSpace(role)
	switch {
	case strings.EqualFold(trimmed, "assistant"):
		return "assistant"
	case strings.EqualFold(trimmed, "user"):
		return "user"
	default:
		return trimmed
	}
}

func convertAnthropicImageBlockToResponses(role string, block map[string]any) (map[string]any, bool) {
	if strings.EqualFold(role, "assistant") {
		return nil, false
	}
	source, ok := block["source"].(map[string]any)
	if !ok {
		return nil, false
	}
	imageURL := ""
	sourceType, _ := source["type"].(string)
	switch sourceType {
	case "base64":
		mediaType, _ := source["media_type"].(string)
		data, _ := source["data"].(string)
		if mediaType != "" && data != "" {
			imageURL = "data:" + mediaType + ";base64," + data
		}
	case "url":
		imageURL, _ = source["url"].(string)
	}
	if imageURL == "" {
		return nil, false
	}
	return map[string]any{
		"type":      "input_image",
		"image_url": map[string]any{"url": imageURL},
	}, true
}

func convertAnthropicToolUseToResponses(block map[string]any) (map[string]any, bool) {
	name, _ := block["name"].(string)
	if name == "" {
		return nil, false
	}
	callID, _ := block["id"].(string)
	arguments := "{}"
	switch input := block["input"].(type) {
	case string:
		if strings.TrimSpace(input) != "" {
			arguments = input
		}
	default:
		if input != nil {
			if b, err := json.Marshal(input); err == nil {
				arguments = string(b)
			}
		}
	}
	return map[string]any{
		"type":      "function_call",
		"call_id":   callID,
		"name":      name,
		"arguments": arguments,
	}, true
}

func convertAnthropicToolResultToResponses(block map[string]any) (map[string]any, bool) {
	callID, _ := block["tool_use_id"].(string)
	if callID == "" {
		return nil, false
	}
	return map[string]any{
		"type":    "function_call_output",
		"call_id": callID,
		"output":  convertAnthropicToolResultContentToResponses(block["content"]),
	}, true
}

func convertAnthropicToolResultContentToResponses(content any) any {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if block["type"] != anthropicTypeText {
				continue
			}
			text, _ := block["text"].(string)
			if text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, "\n\n")
	default:
		return value
	}
}

func convertAnthropicToolsToResponses(tools any) any {
	list, ok := tools.([]any)
	if !ok {
		return tools
	}
	out := make([]any, 0, len(list))
	for _, raw := range list {
		tool, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		name, _ := tool["name"].(string)
		if name == "" {
			out = append(out, raw)
			continue
		}
		converted := map[string]any{
			"type": "function",
			"name": name,
		}
		if desc, ok := tool["description"]; ok {
			converted["description"] = desc
		}
		if params, ok := tool["input_schema"]; ok {
			converted["parameters"] = params
		}
		out = append(out, converted)
	}
	return out
}

func convertAnthropicToolChoiceToResponses(choice any) (any, bool) {
	choiceMap, ok := choice.(map[string]any)
	if !ok {
		return nil, false
	}
	choiceType, _ := choiceMap["type"].(string)
	switch choiceType {
	case "auto":
		return "auto", true
	case "any":
		return "required", true
	case "none":
		return "none", true
	case "tool":
		name, _ := choiceMap["name"].(string)
		if name == "" {
			return nil, false
		}
		return map[string]any{
			"type": "function",
			"name": name,
		}, true
	default:
		return nil, false
	}
}

func convertAnthropicSystemForResponses(system any) string {
	if system == nil {
		return ""
	}
	if s, ok := system.(string); ok {
		return s
	}
	parts, ok := system.([]any)
	if !ok {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		block, ok := part.(map[string]any)
		if !ok {
			continue
		}
		if block["type"] != "text" {
			continue
		}
		text, _ := block["text"].(string)
		if text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n\n")
}

func buildResponsesReasoningFromOutputConfig(outputConfig map[string]any) (map[string]any, bool) {
	if outputConfig == nil {
		return nil, false
	}
	rawEffort, _ := outputConfig["effort"].(string)
	effort, ok := NormalizeEffort(rawEffort)
	if !ok {
		return nil, false
	}
	return map[string]any{
		"summary": "auto",
		"effort":  effort,
	}, true
}

func sanitizeResponsesMaxOutputTokens(maxTokens *int) (int, bool) {
	if maxTokens == nil {
		return 0, false
	}
	if *maxTokens < responsesMaxOutputTokensMin {
		return 0, false
	}
	return *maxTokens, true
}

func normalizeDirectResponsesUser(raw string) string {
	if raw == "" || len(raw) <= directResponsesUserMaxLen {
		return raw
	}
	sum := sha256.Sum256([]byte(raw))
	hashHex := hex.EncodeToString(sum[:])
	if len(hashHex) > directResponsesUserHashLen {
		hashHex = hashHex[:directResponsesUserHashLen]
	}
	prefixLen := directResponsesUserMaxLen - len(hashHex) - 1
	if prefixLen <= 0 {
		return hashHex[:directResponsesUserMaxLen]
	}
	prefix := raw
	if len(prefix) > prefixLen {
		prefix = prefix[:prefixLen]
	}
	return prefix + "_" + hashHex
}

func ResponsesToMessagesResponse(body []byte) ([]byte, bool) {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return nil, false
	}
	output, ok := raw["output"].([]any)
	if !ok {
		return nil, false
	}

	reasoningContent := make([]map[string]any, 0, 1)
	content := make([]map[string]any, 0, len(output))
	stopReason := "end_turn"
	for _, item := range output {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		switch itemType {
		case "reasoning":
			reasoningContent = append(reasoningContent, extractResponsesReasoningBlocks(itemMap)...)
		case "function_call":
			block, ok := convertResponsesFunctionCallToToolUse(itemMap)
			if ok {
				content = append(content, block)
				stopReason = "tool_use"
			}
		case "message":
			reasoningContent = append(reasoningContent, extractResponsesMessageThinkingBlocks(itemMap)...)
			content = append(content, extractResponsesMessageTextBlocks(itemMap)...)
		}
	}
	content = append(reasoningContent, content...)

	out := map[string]any{
		"id":            raw["id"],
		"type":          "message",
		"role":          "assistant",
		"model":         raw["model"],
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
	}
	appendMessagesUsageFromResponses(out, raw)
	updated, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return updated, true
}

func convertResponsesFunctionCallToToolUse(item map[string]any) (map[string]any, bool) {
	callID, _ := item["call_id"].(string)
	name, _ := item["name"].(string)
	if name == "" || callID == "" {
		return nil, false
	}
	input := any(map[string]any{})
	switch args := item["arguments"].(type) {
	case string:
		if strings.TrimSpace(args) != "" {
			var decoded any
			if json.Unmarshal([]byte(args), &decoded) == nil {
				input = decoded
			} else {
				input = args
			}
		}
	case nil:
	default:
		input = args
	}
	return map[string]any{
		"type":  "tool_use",
		"id":    callID,
		"name":  name,
		"input": input,
	}, true
}

func extractResponsesMessageTextBlocks(item map[string]any) []map[string]any {
	content, ok := item["content"].([]any)
	if !ok {
		return nil
	}
	blocks := make([]map[string]any, 0, len(content))
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch blockType {
		case "output_text", "text":
			text, _ := block["text"].(string)
			if text == "" {
				continue
			}
			blocks = append(blocks, map[string]any{
				"type": "text",
				"text": text,
			})
		}
	}
	return blocks
}

func extractResponsesReasoningBlocks(item map[string]any) []map[string]any {
	text := collectResponsesReasoningText(item)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []map[string]any{{
		"type":     "thinking",
		"thinking": text,
	}}
}

func extractResponsesMessageThinkingBlocks(item map[string]any) []map[string]any {
	content, ok := item["content"].([]any)
	if !ok {
		return nil
	}
	parts := make([]string, 0, len(content))
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch blockType {
		case "summary_text", "reasoning_text":
			if text, _ := block["text"].(string); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return []map[string]any{{
		"type":     "thinking",
		"thinking": strings.Join(parts, "\n\n"),
	}}
}

func collectResponsesReasoningText(item map[string]any) string {
	parts := make([]string, 0, responsesReasoningPartsCap)
	appendText := func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		parts = append(parts, text)
	}
	appendTextFromAny := func(raw any) {
		switch value := raw.(type) {
		case string:
			appendText(value)
		case []any:
			for _, entry := range value {
				block, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := block["text"].(string); ok {
					appendText(text)
				}
			}
		}
	}

	appendTextFromAny(item["summary"])
	appendTextFromAny(item["content"])
	if text, ok := item["text"].(string); ok {
		appendText(text)
	}
	if reasoningText, ok := item["reasoning"].(string); ok {
		appendText(reasoningText)
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

func appendMessagesUsageFromResponses(out, raw map[string]any) {
	usageMap, ok := raw["usage"].(map[string]any)
	if !ok {
		return
	}
	usage := map[string]any{}
	if inputTokens, ok := usageMap["input_tokens"]; ok {
		usage["input_tokens"] = inputTokens
	}
	if outputTokens, ok := usageMap["output_tokens"]; ok {
		usage["output_tokens"] = outputTokens
	}
	if details, ok := usageMap["input_tokens_details"].(map[string]any); ok {
		if cached, ok := details["cached_tokens"]; ok {
			usage["cache_read_input_tokens"] = cached
		}
	}
	if cached, ok := usageMap["cache_read_input_tokens"]; ok {
		usage["cache_read_input_tokens"] = cached
	}
	if len(usage) > 0 {
		out["usage"] = usage
	}
}

func TranslateResponsesSSEToMessages(body io.ReadCloser) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		defer func() { _ = body.Close() }()
		defer func() { _ = writer.Close() }()

		state := &responsesToMessagesSSEState{
			id:                   "msg-adapted",
			model:                "",
			toolBlocksByOutput:   map[int]int{},
			contentBlockIndex:    0,
			hasToolUseInStream:   false,
			textBlockOpen:        false,
			thinkingBlockOpen:    false,
			currentToolBlockOpen: map[int]bool{},
			outputTextDeltaSeen:  map[int]bool{},
		}
		ew := &errWriter{w: writer, err: nil}
		buf := bufio.NewReader(body)
		for {
			if ew.failed() {
				return
			}
			line, err := buf.ReadString('\n')
			if err != nil && err != io.EOF {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "data:") {
				dataLine := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if dataLine == "[DONE]" {
					finalizeResponsesSSEIfNeeded(ew, state, nil)
					_, _ = ew.Write([]byte("data: [DONE]\n\n"))
					return
				}
				handleResponsesSSEDataLine(ew, dataLine, state)
			}
			if err == io.EOF {
				finalizeResponsesSSEIfNeeded(ew, state, nil)
				_, _ = ew.Write([]byte("data: [DONE]\n\n"))
				return
			}
		}
	}()
	return reader
}

type responsesToMessagesSSEState struct {
	id                   string
	model                string
	contentBlockIndex    int
	textBlockOpen        bool
	thinkingBlockOpen    bool
	toolBlocksByOutput   map[int]int
	currentToolBlockOpen map[int]bool
	outputTextDeltaSeen  map[int]bool
	hasToolUseInStream   bool
	messageStartSent     bool
	messageStopSent      bool
}

func handleResponsesSSEDataLine(writer io.Writer, dataLine string, state *responsesToMessagesSSEState) {
	event, eventType, ok := parseResponsesSSEDataLine(dataLine)
	if !ok {
		return
	}

	switch eventType {
	case "response.created":
		handleResponsesCreatedEvent(writer, event, state)
	case "response.output_item.added":
		handleResponsesOutputItemAddedEvent(writer, event, state)
	case "response.function_call_arguments.delta":
		handleResponsesFunctionCallArgumentsDeltaEvent(writer, event, state)
	case "response.output_item.done":
		handleResponsesOutputItemDoneEvent(writer, event, state)
	case "response.output_text.delta":
		handleResponsesOutputTextDeltaEvent(writer, event, state)
	case "response.output_text.done":
		handleResponsesOutputTextDoneEvent(writer, event, state)
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		handleResponsesReasoningDeltaEvent(writer, event, state)
	case "response.reasoning_text.done", "response.reasoning_summary_text.done":
		handleResponsesReasoningDoneEvent(writer, event, state)
	case "response.completed":
		handleResponsesCompletedEvent(writer, event, state)
	}
}

func parseResponsesSSEDataLine(dataLine string) (event map[string]any, eventType string, ok bool) {
	if json.Unmarshal([]byte(dataLine), &event) != nil {
		return nil, "", false
	}
	eventType, _ = event["type"].(string)
	return event, eventType, true
}

func handleResponsesCreatedEvent(writer io.Writer, event map[string]any, state *responsesToMessagesSSEState) {
	if response, ok := event["response"].(map[string]any); ok {
		if id, ok := response["id"].(string); ok && id != "" {
			state.id = id
		}
		if model, ok := response["model"].(string); ok {
			state.model = model
		}
	}
	ensureResponsesMessageStart(writer, state)
}

func ensureResponsesMessageStart(writer io.Writer, state *responsesToMessagesSSEState) {
	if state == nil || state.messageStartSent {
		return
	}
	writeResponsesMessagesSSEEvent(writer, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            state.id,
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         state.model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":            0,
				"output_tokens":           0,
				"cache_read_input_tokens": 0,
			},
		},
	})
	state.messageStartSent = true
}

func handleResponsesOutputItemAddedEvent(writer io.Writer, event map[string]any, state *responsesToMessagesSSEState) {
	ensureResponsesMessageStart(writer, state)
	closeResponsesThinkingBlockIfOpen(writer, state)
	item, ok := event["item"].(map[string]any)
	if !ok {
		return
	}
	itemType, _ := item["type"].(string)
	if itemType != "function_call" {
		return
	}

	closeResponsesTextBlockIfOpen(writer, state)
	outputIndex := responsesOutputIndex(event["output_index"])
	blockIndex := state.contentBlockIndex
	state.toolBlocksByOutput[outputIndex] = blockIndex
	state.currentToolBlockOpen[blockIndex] = true
	state.hasToolUseInStream = true
	state.contentBlockIndex++

	callID, _ := item["call_id"].(string)
	name, _ := item["name"].(string)
	writeResponsesMessagesSSEEvent(writer, "content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": blockIndex,
		"content_block": map[string]any{
			"type":  anthropicTypeToolUse,
			"id":    callID,
			"name":  name,
			"input": map[string]any{},
		},
	})
}

func handleResponsesFunctionCallArgumentsDeltaEvent(writer io.Writer, event map[string]any, state *responsesToMessagesSSEState) {
	ensureResponsesMessageStart(writer, state)
	outputIndex := responsesOutputIndex(event["output_index"])
	blockIndex, ok := state.toolBlocksByOutput[outputIndex]
	if !ok {
		return
	}
	delta, _ := event["delta"].(string)
	if delta == "" {
		return
	}
	writeResponsesMessagesSSEEvent(writer, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": blockIndex,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": delta,
		},
	})
}

func handleResponsesOutputItemDoneEvent(writer io.Writer, event map[string]any, state *responsesToMessagesSSEState) {
	ensureResponsesMessageStart(writer, state)
	item, _ := event["item"].(map[string]any)
	itemType, _ := item["type"].(string)
	switch itemType {
	case "message":
		handleResponsesMessageItemDoneEvent(writer, event, state, item)
		return
	case "function_call":
		handleResponsesFunctionCallDoneEvent(writer, event, state)
		return
	}
	handleResponsesFunctionCallDoneEvent(writer, event, state)
}

func handleResponsesFunctionCallDoneEvent(writer io.Writer, event map[string]any, state *responsesToMessagesSSEState) {
	outputIndex := responsesOutputIndex(event["output_index"])
	blockIndex, ok := state.toolBlocksByOutput[outputIndex]
	if !ok || !state.currentToolBlockOpen[blockIndex] {
		return
	}
	writeResponsesMessagesSSEEvent(writer, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": blockIndex,
	})
	state.currentToolBlockOpen[blockIndex] = false
	if blockIndex+1 > state.contentBlockIndex {
		state.contentBlockIndex = blockIndex + 1
	}
}

func handleResponsesMessageItemDoneEvent(
	writer io.Writer,
	event map[string]any,
	state *responsesToMessagesSSEState,
	item map[string]any,
) {
	if state == nil {
		return
	}
	emitResponsesMessageThinkingFromDone(writer, state, item)
	outputIndex := responsesOutputIndex(event["output_index"])
	if state.outputTextDeltaSeen[outputIndex] {
		closeResponsesTextBlockIfOpen(writer, state)
		return
	}

	blocks := extractResponsesMessageTextBlocks(item)
	if len(blocks) == 0 {
		return
	}

	closeResponsesThinkingBlockIfOpen(writer, state)
	closeResponsesAnyOpenToolBlocks(writer, state)
	if !state.textBlockOpen {
		writeResponsesMessagesSSEEvent(writer, "content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": state.contentBlockIndex,
			"content_block": map[string]any{
				"type": anthropicTypeText,
				"text": "",
			},
		})
		state.textBlockOpen = true
	}
	for _, block := range blocks {
		text, _ := block["text"].(string)
		if text == "" {
			continue
		}
		writeResponsesMessagesSSEEvent(writer, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": state.contentBlockIndex,
			"delta": map[string]any{
				"type": "text_delta",
				"text": text,
			},
		})
	}
	closeResponsesTextBlockIfOpen(writer, state)
}

func emitResponsesMessageThinkingFromDone(writer io.Writer, state *responsesToMessagesSSEState, item map[string]any) {
	thinkingBlocks := extractResponsesMessageThinkingBlocks(item)
	if len(thinkingBlocks) == 0 {
		return
	}
	closeResponsesTextBlockIfOpen(writer, state)
	closeResponsesAnyOpenToolBlocks(writer, state)
	for _, block := range thinkingBlocks {
		thinking, _ := block["thinking"].(string)
		if strings.TrimSpace(thinking) == "" {
			continue
		}
		openResponsesThinkingBlockIfNeeded(writer, state)
		writeResponsesMessagesSSEEvent(writer, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": state.contentBlockIndex,
			"delta": map[string]any{
				"type":     "thinking_delta",
				"thinking": thinking,
			},
		})
		closeResponsesThinkingBlockIfOpen(writer, state)
	}
}

func handleResponsesOutputTextDeltaEvent(writer io.Writer, event map[string]any, state *responsesToMessagesSSEState) {
	ensureResponsesMessageStart(writer, state)
	outputIndex := responsesOutputIndex(event["output_index"])
	state.outputTextDeltaSeen[outputIndex] = true
	closeResponsesThinkingBlockIfOpen(writer, state)
	closeResponsesAnyOpenToolBlocks(writer, state)
	if !state.textBlockOpen {
		writeResponsesMessagesSSEEvent(writer, "content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": state.contentBlockIndex,
			"content_block": map[string]any{
				"type": anthropicTypeText,
				"text": "",
			},
		})
		state.textBlockOpen = true
	}
	delta, _ := event["delta"].(string)
	if delta == "" {
		return
	}
	writeResponsesMessagesSSEEvent(writer, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": state.contentBlockIndex,
		"delta": map[string]any{
			"type": "text_delta",
			"text": delta,
		},
	})
}

func handleResponsesOutputTextDoneEvent(writer io.Writer, event map[string]any, state *responsesToMessagesSSEState) {
	ensureResponsesMessageStart(writer, state)
	outputIndex := responsesOutputIndex(event["output_index"])
	if state.outputTextDeltaSeen[outputIndex] {
		return
	}
	text, _ := event["text"].(string)
	if text == "" {
		return
	}
	closeResponsesThinkingBlockIfOpen(writer, state)
	closeResponsesAnyOpenToolBlocks(writer, state)
	if !state.textBlockOpen {
		writeResponsesMessagesSSEEvent(writer, "content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": state.contentBlockIndex,
			"content_block": map[string]any{
				"type": anthropicTypeText,
				"text": "",
			},
		})
		state.textBlockOpen = true
	}
	writeResponsesMessagesSSEEvent(writer, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": state.contentBlockIndex,
		"delta": map[string]any{
			"type": "text_delta",
			"text": text,
		},
	})
	state.outputTextDeltaSeen[outputIndex] = true
	closeResponsesTextBlockIfOpen(writer, state)
}

func handleResponsesReasoningDeltaEvent(writer io.Writer, event map[string]any, state *responsesToMessagesSSEState) {
	ensureResponsesMessageStart(writer, state)
	closeResponsesTextBlockIfOpen(writer, state)
	closeResponsesAnyOpenToolBlocks(writer, state)
	openResponsesThinkingBlockIfNeeded(writer, state)
	delta, _ := event["delta"].(string)
	if delta == "" {
		return
	}
	writeResponsesMessagesSSEEvent(writer, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": state.contentBlockIndex,
		"delta": map[string]any{
			"type":     "thinking_delta",
			"thinking": delta,
		},
	})
}

func handleResponsesReasoningDoneEvent(writer io.Writer, event map[string]any, state *responsesToMessagesSSEState) {
	ensureResponsesMessageStart(writer, state)
	text, _ := event["text"].(string)
	if !state.thinkingBlockOpen && text != "" {
		closeResponsesTextBlockIfOpen(writer, state)
		closeResponsesAnyOpenToolBlocks(writer, state)
		openResponsesThinkingBlockIfNeeded(writer, state)
		writeResponsesMessagesSSEEvent(writer, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": state.contentBlockIndex,
			"delta": map[string]any{
				"type":     "thinking_delta",
				"thinking": text,
			},
		})
	}
	closeResponsesThinkingBlockIfOpen(writer, state)
}

func handleResponsesCompletedEvent(writer io.Writer, event map[string]any, state *responsesToMessagesSSEState) {
	finalizeResponsesSSEIfNeeded(writer, state, extractResponsesSSEUsage(event))
}

func finalizeResponsesSSEIfNeeded(writer io.Writer, state *responsesToMessagesSSEState, usage map[string]any) {
	if state == nil || state.messageStopSent {
		return
	}
	ensureResponsesMessageStart(writer, state)
	closeResponsesThinkingBlockIfOpen(writer, state)
	closeResponsesTextBlockIfOpen(writer, state)
	closeResponsesAnyOpenToolBlocks(writer, state)

	stopReason := chatFinishReasonEnd
	if state.hasToolUseInStream {
		stopReason = anthropicTypeToolUse
	}

	writeResponsesMessagesSSEEvent(writer, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": defaultResponsesSSEUsage(usage),
	})
	writeResponsesMessagesSSEEvent(writer, "message_stop", map[string]any{
		"type": "message_stop",
	})
	state.messageStopSent = true
}

func defaultResponsesSSEUsage(usage map[string]any) map[string]any {
	if usage != nil {
		return usage
	}
	return map[string]any{
		"input_tokens":            0,
		"output_tokens":           0,
		"cache_read_input_tokens": 0,
	}
}

func openResponsesThinkingBlockIfNeeded(writer io.Writer, state *responsesToMessagesSSEState) {
	if state.thinkingBlockOpen {
		return
	}
	writeResponsesMessagesSSEEvent(writer, "content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": state.contentBlockIndex,
		"content_block": map[string]any{
			"type":     "thinking",
			"thinking": "",
		},
	})
	state.thinkingBlockOpen = true
}

func closeResponsesThinkingBlockIfOpen(writer io.Writer, state *responsesToMessagesSSEState) {
	if !state.thinkingBlockOpen {
		return
	}
	writeResponsesMessagesSSEEvent(writer, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": state.contentBlockIndex,
	})
	state.thinkingBlockOpen = false
	state.contentBlockIndex++
}

func responsesOutputIndex(raw any) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func closeResponsesTextBlockIfOpen(writer io.Writer, state *responsesToMessagesSSEState) {
	if !state.textBlockOpen {
		return
	}
	writeResponsesMessagesSSEEvent(writer, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": state.contentBlockIndex,
	})
	state.textBlockOpen = false
	state.contentBlockIndex++
}

func closeResponsesAnyOpenToolBlocks(writer io.Writer, state *responsesToMessagesSSEState) {
	indices := make([]int, 0, len(state.currentToolBlockOpen))
	for blockIndex, isOpen := range state.currentToolBlockOpen {
		if isOpen {
			indices = append(indices, blockIndex)
		}
	}
	sort.Ints(indices)
	for _, blockIndex := range indices {
		writeResponsesMessagesSSEEvent(writer, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": blockIndex,
		})
		state.currentToolBlockOpen[blockIndex] = false
		if blockIndex >= state.contentBlockIndex {
			state.contentBlockIndex = blockIndex + 1
		}
	}
}

func extractResponsesSSEUsage(event map[string]any) map[string]any {
	usage := map[string]any{
		"input_tokens":            0,
		"output_tokens":           0,
		"cache_read_input_tokens": 0,
	}
	response, ok := event["response"].(map[string]any)
	if !ok {
		return usage
	}
	usageMap, ok := response["usage"].(map[string]any)
	if !ok {
		return usage
	}
	if inputTokens, ok := usageMap["input_tokens"]; ok {
		usage["input_tokens"] = inputTokens
	}
	if outputTokens, ok := usageMap["output_tokens"]; ok {
		usage["output_tokens"] = outputTokens
	}
	if details, ok := usageMap["input_tokens_details"].(map[string]any); ok {
		if cached, ok := details["cached_tokens"]; ok {
			usage["cache_read_input_tokens"] = cached
		}
	}
	if cached, ok := usageMap["cache_read_input_tokens"]; ok {
		usage["cache_read_input_tokens"] = cached
	}
	return usage
}

func writeResponsesMessagesSSEEvent(w io.Writer, event string, payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("event: " + event + "\n"))
	_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
}
