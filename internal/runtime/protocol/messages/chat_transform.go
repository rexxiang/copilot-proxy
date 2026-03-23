package messages

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type anthropicRequest struct {
	Model         string         `json:"model"`
	Messages      []aMessage     `json:"messages"`
	System        any            `json:"system,omitempty"`
	MaxTokens     *int           `json:"max_tokens,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	TopP          *float64       `json:"top_p,omitempty"`
	TopK          *int           `json:"top_k,omitempty"`
	Stop          any            `json:"stop,omitempty"`
	StopSequences []string       `json:"stop_sequences,omitempty"`
	Stream        *bool          `json:"stream,omitempty"`
	Tools         any            `json:"tools,omitempty"`
	ToolChoice    any            `json:"tool_choice,omitempty"`
	Thinking      map[string]any `json:"thinking,omitempty"`
	OutputConfig  map[string]any `json:"output_config,omitempty"`
	ServiceTier   string         `json:"service_tier,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type aMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

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
	return MessagesToChatRequestWithOptions(body, MessagesReasoningOptions{
		SupportedReasoningEffort: []string{"low", "medium", "high"},
	})
}

func MessagesToChatRequestWithOptions(body []byte, options MessagesReasoningOptions) ([]byte, bool) {
	var req anthropicRequest
	if json.Unmarshal(body, &req) != nil {
		return nil, false
	}
	chatMessages := make([]map[string]any, 0, len(req.Messages)+1)

	if req.System != nil {
		if sysContent, ok := mapSystemContentToChat(req.System); ok {
			chatMessages = append(chatMessages, map[string]any{
				"role":    "system",
				"content": sysContent,
			})
		}
	}

	for _, msg := range req.Messages {
		chatMessages = append(chatMessages, convertAnthropicMessageToChat(msg)...)
	}

	out := map[string]any{
		"model":    req.Model,
		"messages": chatMessages,
	}
	appendChatRequestParams(out, &req, options)
	updated, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return updated, true
}

func appendChatRequestParams(out map[string]any, req *anthropicRequest, options MessagesReasoningOptions) {
	if req == nil {
		return
	}
	if req.MaxTokens != nil {
		out["max_tokens"] = *req.MaxTokens
	}
	if req.Temperature != nil {
		out["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		out["top_p"] = *req.TopP
	}
	if req.StopSequences != nil {
		out["stop"] = req.StopSequences
	}
	if req.Stream != nil {
		out["stream"] = *req.Stream
	}
	if req.Metadata != nil {
		if userID, ok := req.Metadata["user_id"].(string); ok && userID != "" {
			out["user"] = userID
		}
	}
	if req.Tools != nil {
		out["tools"] = convertAnthropicTools(req.Tools)
	}
	if req.ToolChoice != nil {
		if translated := translateAnthropicToolChoice(req.ToolChoice); translated != nil {
			out["tool_choice"] = translated
		}
	}
	if effort, ok := resolveMessagesReasoningEffort(req.OutputConfig, options); ok {
		out["reasoning_effort"] = effort
	}
}

func convertAnthropicTools(tools any) any {
	list, ok := tools.([]any)
	if !ok {
		return tools
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		tool, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		name, _ := tool["name"].(string)
		if name == "" {
			out = append(out, item)
			continue
		}
		fn := map[string]any{
			"name": name,
		}
		if desc, ok := tool["description"].(string); ok && desc != "" {
			fn["description"] = desc
		}
		if inputSchema, ok := tool["input_schema"]; ok {
			fn["parameters"] = inputSchema
		}
		out = append(out, map[string]any{
			"type":     "function",
			"function": fn,
		})
	}
	return out
}

func convertAnthropicMessageToChat(msg aMessage) []map[string]any {
	if msg.Role == anthropicRoleUser {
		return translateUserMessageInOrder(msg.Content)
	}

	assistantMessage := mapAssistantBlocksToChatMessage(msg.Content)
	if assistantMessage == nil {
		return nil
	}
	return []map[string]any{assistantMessage}
}

func mapSystemContentToChat(system any) (any, bool) {
	if system == nil {
		return nil, false
	}

	if systemText, ok := system.(string); ok {
		if systemText == "" {
			return nil, false
		}
		return systemText, true
	}

	blocks, ok := system.([]any)
	if !ok {
		return nil, false
	}

	contentParts := make([]any, 0, len(blocks))
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		if blockType != anthropicTypeText {
			continue
		}
		text, ok := block["text"].(string)
		if !ok {
			continue
		}
		contentParts = append(contentParts, toOpenAITextPart(text))
	}

	if len(contentParts) == 0 {
		return nil, false
	}
	return contentParts, true
}

func translateUserMessageInOrder(content any) []map[string]any {
	parts, isArray := content.([]any)
	if !isArray {
		return mapNonArrayUserContent(content)
	}

	translated := make([]map[string]any, 0, len(parts))
	pendingUserParts := make([]any, 0, len(parts))
	flushUserParts := func() {
		if len(pendingUserParts) == 0 {
			return
		}
		userContent := make([]any, len(pendingUserParts))
		copy(userContent, pendingUserParts)
		translated = append(translated, map[string]any{
			"role":    anthropicRoleUser,
			"content": userContent,
		})
		pendingUserParts = pendingUserParts[:0]
	}

	for _, raw := range parts {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch blockType {
		case anthropicTypeToolResult:
			flushUserParts()
			translated = appendToolResultMessage(translated, block)
		case anthropicTypeThinking:
			continue
		default:
			if part, ok := mapUserBlockToChatPart(block); ok {
				pendingUserParts = append(pendingUserParts, part)
			}
		}
	}
	flushUserParts()
	return translated
}

func mapNonArrayUserContent(content any) []map[string]any {
	switch v := content.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []map[string]any{{
			"role":    anthropicRoleUser,
			"content": v,
		}}
	case map[string]any:
		if part, ok := mapUserBlockToChatPart(v); ok {
			return []map[string]any{{
				"role":    anthropicRoleUser,
				"content": []any{part},
			}}
		}
		return nil
	default:
		return nil
	}
}

func appendToolResultMessage(messages []map[string]any, block map[string]any) []map[string]any {
	toolCallID, _ := block["tool_use_id"].(string)
	content := mapToolResultContentToToolMessageContent(block["content"])
	return append(messages, map[string]any{
		"role":         anthropicToolChoice,
		"tool_call_id": toolCallID,
		"content":      content,
	})
}

func mapToolResultContentToToolMessageContent(content any) any {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]any, 0, len(v))
		for _, raw := range v {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			blockType, _ := block["type"].(string)
			if blockType != anthropicTypeText {
				continue
			}
			text, ok := block["text"].(string)
			if !ok {
				continue
			}
			parts = append(parts, toOpenAITextPart(text))
		}
		if len(parts) == 0 {
			return ""
		}
		return parts
	default:
		return ""
	}
}

func mapAssistantBlocksToChatMessage(content any) map[string]any {
	if textContent, ok := content.(string); ok {
		if textContent == "" {
			return nil
		}
		return map[string]any{
			"role":    "assistant",
			"content": textContent,
		}
	}

	parts, ok := content.([]any)
	if !ok {
		return nil
	}

	textParts := make([]any, 0, len(parts))
	toolCalls := make([]any, 0, len(parts))

	for _, raw := range parts {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch blockType {
		case anthropicTypeText:
			text, ok := block["text"].(string)
			if !ok {
				continue
			}
			textParts = append(textParts, toOpenAITextPart(text))
		case anthropicTypeToolUse:
			toolCall := mapToolUseBlockToChatToolCall(block)
			if toolCall != nil {
				toolCalls = append(toolCalls, toolCall)
			}
		case anthropicTypeThinking:
			continue
		}
	}

	if len(toolCalls) > 0 {
		message := map[string]any{
			"role":       "assistant",
			"tool_calls": toolCalls,
		}
		if len(textParts) > 0 {
			message["content"] = textParts
		}
		return message
	}

	if len(textParts) == 0 {
		return nil
	}

	return map[string]any{
		"role":    "assistant",
		"content": textParts,
	}
}

func mapToolUseBlockToChatToolCall(block map[string]any) map[string]any {
	id, _ := block["id"].(string)
	name, _ := block["name"].(string)
	input := block["input"]
	args := "{}"
	if input != nil {
		if b, err := json.Marshal(input); err == nil {
			args = string(b)
		}
	}

	return map[string]any{
		"id":   id,
		"type": "function",
		"function": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
}

func mapUserBlockToChatPart(block map[string]any) (map[string]any, bool) {
	blockType, _ := block["type"].(string)
	switch blockType {
	case anthropicTypeText:
		text, ok := block["text"].(string)
		if !ok {
			return nil, false
		}
		return toOpenAITextPart(text), true
	case anthropicTypeImage:
		source, _ := block["source"].(map[string]any)
		dataURL := buildDataURL(source)
		if dataURL == "" {
			return nil, false
		}
		return toOpenAIImagePart(dataURL), true
	default:
		return nil, false
	}
}

func toOpenAITextPart(text string) map[string]any {
	return map[string]any{
		"type": anthropicTypeText,
		"text": text,
	}
}

func toOpenAIImagePart(url string) map[string]any {
	return map[string]any{
		"type":      "image_url",
		"image_url": map[string]any{"url": url},
	}
}

func buildDataURL(source map[string]any) string {
	if source == nil {
		return ""
	}
	sourceType, _ := source["type"].(string)
	if sourceType == "base64" {
		mediaType, _ := source["media_type"].(string)
		data, _ := source["data"].(string)
		if mediaType != "" && data != "" {
			return "data:" + mediaType + ";base64," + data
		}
	}
	if sourceType == "url" {
		if url, ok := source["url"].(string); ok {
			return url
		}
	}
	return ""
}

type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func ChatToMessagesResponse(bodyBytes []byte) ([]byte, bool) {
	if len(bodyBytes) == 0 {
		return nil, false
	}
	return fallbackChatToMessages(bodyBytes)
}

func fallbackChatToMessages(bodyBytes []byte) ([]byte, bool) {
	var raw map[string]any
	if json.Unmarshal(bodyBytes, &raw) != nil {
		return nil, false
	}
	id, model, choices := decodeChatResponse(raw)
	blocks, stopReason, ok := buildAnthropicBlocks(choices)
	if !ok {
		return nil, false
	}
	out := buildAnthropicMessagePayload(id, model, blocks, stopReason)
	appendAnthropicUsage(out, raw)
	updated, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return updated, true
}

func decodeChatResponse(raw map[string]any) (id, model string, choices []any) {
	id, _ = raw["id"].(string)
	model, _ = raw["model"].(string)
	choices, _ = raw["choices"].([]any)
	if choices == nil {
		choices = make([]any, 0)
	}
	return id, model, choices
}

func buildAnthropicBlocks(choices []any) (blocks []map[string]any, stopReason any, ok bool) {
	allTextBlocks := make([]map[string]any, 0, len(choices))
	allToolBlocks := make([]map[string]any, 0, len(choices))
	rawStopReason := initialChatFinishReason(choices)
	for i, choice := range choices {
		choiceMap, _ := choice.(map[string]any)
		textBlocks, toolBlocks, choiceOK := blocksForChoice(choiceMap)
		if !choiceOK {
			return nil, nil, false
		}
		allTextBlocks = append(allTextBlocks, textBlocks...)
		allToolBlocks = append(allToolBlocks, toolBlocks...)
		if i > 0 {
			rawStopReason = maybeUpdateStopReason(rawStopReason, choiceMap)
		}
	}
	return append(allTextBlocks, allToolBlocks...), mapFinishReason(rawStopReason), true
}

func initialChatFinishReason(choices []any) any {
	if len(choices) == 0 {
		return nil
	}
	firstChoice, _ := choices[0].(map[string]any)
	return normalizeChatFinishReason(firstChoice["finish_reason"])
}

func blocksForChoice(choiceMap map[string]any) (textBlocks, toolBlocks []map[string]any, ok bool) {
	messageMap, _ := choiceMap["message"].(map[string]any)
	contentBlocks := make([]map[string]any, 0, 4)
	if messageMap != nil {
		contentBlocks = append(contentBlocks, getAnthropicThinkingBlocks(messageMap["reasoning_text"])...)
		contentBlocks = append(contentBlocks, getAnthropicTextBlocks(messageMap["content"])...)
	}
	textBlocks = contentBlocks
	toolCalls := parseRawToolCalls(nil)
	if messageMap != nil {
		toolCalls = parseRawToolCalls(messageMap["tool_calls"])
	}
	toolBlocks, ok = getAnthropicToolUseBlocks(toolCalls)
	return textBlocks, toolBlocks, ok
}

func maybeUpdateStopReason(stopReason any, choiceMap map[string]any) any {
	finishReason := normalizeChatFinishReason(choiceMap["finish_reason"])
	finishReasonText, _ := finishReason.(string)
	stopReasonText, _ := stopReason.(string)
	if finishReasonText == "tool_calls" || stopReasonText == "stop" {
		return finishReason
	}
	return stopReason
}

func normalizeChatFinishReason(raw any) any {
	text, ok := raw.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return nil
	}
	return text
}

func buildAnthropicMessagePayload(id, model string, contentBlocks []map[string]any, stopReason any) map[string]any {
	content := make([]any, 0, len(contentBlocks))
	for _, block := range contentBlocks {
		content = append(content, block)
	}
	return map[string]any{
		"id":            normalizeMessageID(id),
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
	}
}

func appendAnthropicUsage(out, raw map[string]any) {
	usage := map[string]any{
		"input_tokens":  float64(0),
		"output_tokens": float64(0),
	}

	usageMap, ok := raw["usage"].(map[string]any)
	if !ok {
		out["usage"] = usage
		return
	}

	inputTokens := numberFromAny(usageMap["prompt_tokens"])
	outputTokens := numberFromAny(usageMap["completion_tokens"])
	cacheTokens, cacheTokensPresent := cachedTokensFromUsage(usageMap)
	adjustedInput := inputTokens - cacheTokens
	if adjustedInput < 0 {
		adjustedInput = 0
	}
	usage["input_tokens"] = adjustedInput
	usage["output_tokens"] = outputTokens
	if cacheTokensPresent {
		usage["cache_read_input_tokens"] = cacheTokens
	}
	out["usage"] = usage
}

func cachedTokensFromUsage(usageMap map[string]any) (float64, bool) {
	details, ok := usageMap["prompt_tokens_details"].(map[string]any)
	if !ok {
		return 0, false
	}
	cachedRaw, ok := details["cached_tokens"]
	if !ok {
		return 0, false
	}
	return numberFromAny(cachedRaw), true
}

func numberFromAny(raw any) float64 {
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	case uint:
		return float64(v)
	case uint64:
		return float64(v)
	case uint32:
		return float64(v)
	default:
		return 0
	}
}

func parseRawToolCalls(raw any) []chatToolCall {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]chatToolCall, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var call chatToolCall
		if id, ok := m["id"].(string); ok {
			call.ID = id
		}
		if t, ok := m["type"].(string); ok {
			call.Type = t
		}
		if fn, ok := m["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok {
				call.Function.Name = name
			}
			switch v := fn["arguments"].(type) {
			case string:
				call.Function.Arguments = v
			default:
				if v != nil {
					if b, err := json.Marshal(v); err == nil {
						call.Function.Arguments = string(b)
					}
				}
			}
		}
		out = append(out, call)
	}
	return out
}

func normalizeMessageID(id string) string {
	return id
}

func mapFinishReason(reason any) any {
	reasonText, _ := reason.(string)
	switch reasonText {
	case "stop":
		return chatFinishReasonEnd
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return anthropicTypeToolUse
	case "content_filter":
		return chatFinishReasonEnd
	default:
		return nil
	}
}

type chatToMessagesPromptTokensDetails struct {
	CachedTokens *int `json:"cached_tokens,omitempty"`
}

type chatToMessagesUsage struct {
	PromptTokens        *int                               `json:"prompt_tokens,omitempty"`
	CompletionTokens    *int                               `json:"completion_tokens,omitempty"`
	PromptTokensDetails *chatToMessagesPromptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type chatToMessagesToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatToMessagesDeltaToolCall struct {
	Index    int                        `json:"index"`
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Function chatToMessagesToolFunction `json:"function"`
}

type chatToMessagesDelta struct {
	Role          string                        `json:"role"`
	Content       string                        `json:"content"`
	ReasoningText string                        `json:"reasoning_text"`
	Refusal       string                        `json:"refusal"`
	ToolCalls     []chatToMessagesDeltaToolCall `json:"tool_calls"`
}

type chatToMessagesChoice struct {
	Index        int                 `json:"index"`
	Delta        chatToMessagesDelta `json:"delta"`
	FinishReason string              `json:"finish_reason"`
}

type chatToMessagesStreamChunk struct {
	ID      string                 `json:"id"`
	Model   string                 `json:"model"`
	Usage   *chatToMessagesUsage   `json:"usage,omitempty"`
	Choices []chatToMessagesChoice `json:"choices"`
}

func TranslateChatSSEToMessages(body io.ReadCloser) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		defer func() {
			_ = body.Close()
		}()
		defer func() {
			_ = writer.Close()
		}()
		state := newAnthropicStreamState()
		buf := bufio.NewReader(body)
		ew := &errWriter{w: writer, err: nil}
		for {
			if ew.failed() {
				return
			}
			line, err := readSSELine(buf)
			if errors.Is(err, errStreamRead) {
				writeSSEError(ew)
				return
			}
			if handleSSELine(ew, line, err, state) {
				return
			}
		}
	}()
	return reader
}

var errStreamRead = errors.New("stream read error")

func newAnthropicStreamState() *anthropicStreamState {
	return &anthropicStreamState{
		messageStartSent: false,
		messageStopSent:  false,
		nextBlockIndex:   0,
		openBlockIndex:   -1,
		toolCalls:        map[string]toolCallState{},
		lastUsage: map[string]any{
			"input_tokens":  0,
			"output_tokens": 0,
		},
	}
}

func readSSELine(buf *bufio.Reader) (string, error) {
	line, err := buf.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("%w: %w", errStreamRead, err)
	}
	line = strings.TrimRight(line, "\r\n")
	if err != nil {
		return line, fmt.Errorf("read SSE line: %w", err)
	}
	return line, nil
}

func handleSSELine(
	writer io.Writer,
	line string,
	err error,
	state *anthropicStreamState,
) bool {
	if line == "" {
		if errors.Is(err, io.EOF) {
			writeAnthropicEvents(writer, finalizeAnthropicStreamEvents(state))
			return true
		}
		return false
	}
	if strings.HasPrefix(line, "data:") {
		dataLine := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if dataLine == "[DONE]" {
			writeAnthropicEvents(writer, finalizeAnthropicStreamEvents(state))
			return true
		}
		handleSSEDataLine(writer, dataLine, state)
	}
	return false
}

func handleSSEDataLine(writer io.Writer, dataLine string, state *anthropicStreamState) {
	var chunk chatToMessagesStreamChunk
	if json.Unmarshal([]byte(dataLine), &chunk) != nil {
		return
	}
	writeAnthropicEvents(writer, translateChunkToAnthropicEvents(&chunk, state))
}

func writeSSEError(writer io.Writer) {
	writeSSEEvent(writer, "error", map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "api_error",
			"message": "An unexpected error occurred during streaming.",
		},
	})
}

func translateChunkToAnthropicEvents(chunk *chatToMessagesStreamChunk, state *anthropicStreamState) []map[string]any {
	events := make([]map[string]any, 0)
	if chunk == nil || state == nil {
		return events
	}

	rememberChunkMetadataAndUsage(state, chunk)
	ensureMessageStartEvent(&events, state, chunk)

	if len(chunk.Choices) == 0 {
		return events
	}

	shouldFinalize := false
	for _, choice := range chunk.Choices {
		processChoiceDelta(&events, state, choice)
		if choice.FinishReason != "" {
			shouldFinalize = true
			state.stopReason = mergeStreamStopReason(state.stopReason, mapFinishReason(choice.FinishReason))
		}
	}
	if shouldFinalize {
		events = append(events, finalizeAnthropicStreamEvents(state)...)
	}
	return events
}

func rememberChunkMetadataAndUsage(state *anthropicStreamState, chunk *chatToMessagesStreamChunk) {
	if state == nil || chunk == nil {
		return
	}
	if chunk.ID != "" {
		state.streamID = chunk.ID
	}
	if chunk.Model != "" {
		state.streamModel = chunk.Model
	}
	state.lastUsage = cloneUsageMap(buildStreamUsage(chunk, false))
}

func ensureMessageStartEvent(events *[]map[string]any, state *anthropicStreamState, chunk *chatToMessagesStreamChunk) {
	if events == nil || state == nil || chunk == nil || state.messageStartSent {
		return
	}
	*events = append(*events, map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            state.streamID,
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         state.streamModel,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         buildStreamUsage(chunk, true),
		},
	})
	state.messageStartSent = true
}

func processChoiceDelta(events *[]map[string]any, state *anthropicStreamState, choice chatToMessagesChoice) {
	if events == nil || state == nil || state.messageStopSent {
		return
	}
	delta := choice.Delta
	for _, toolCall := range delta.ToolCalls {
		processToolCallDelta(events, state, choice.Index, toolCall)
	}
	appendThinkingDelta(events, state, delta.ReasoningText)
	appendTextDelta(events, state, delta.Refusal)
	appendTextDelta(events, state, delta.Content)
}

func processToolCallDelta(
	events *[]map[string]any,
	state *anthropicStreamState,
	choiceIndex int,
	toolCall chatToMessagesDeltaToolCall,
) {
	if events == nil || state == nil {
		return
	}
	toolKey := streamToolKey(choiceIndex, toolCall.Index)
	if toolCall.ID != "" && toolCall.Function.Name != "" {
		closeOpenContentBlock(events, state)
		blockIndex := state.nextBlockIndex
		state.nextBlockIndex++
		state.toolCalls[toolKey] = toolCallState{
			id:                  toolCall.ID,
			name:                toolCall.Function.Name,
			anthropicBlockIndex: blockIndex,
			closed:              false,
		}
		*events = append(*events, map[string]any{
			"type":  "content_block_start",
			"index": blockIndex,
			"content_block": map[string]any{
				"type":  anthropicTypeToolUse,
				"id":    toolCall.ID,
				"name":  toolCall.Function.Name,
				"input": map[string]any{},
			},
		})
		state.openBlockIndex = blockIndex
		state.openBlockType = anthropicTypeToolUse
		state.openToolKey = toolKey
	}
	if toolCall.Function.Arguments == "" {
		return
	}
	info, ok := state.toolCalls[toolKey]
	if !ok || info.closed {
		return
	}
	if state.openBlockIndex != info.anthropicBlockIndex {
		closeOpenContentBlock(events, state)
		state.openBlockIndex = info.anthropicBlockIndex
		state.openBlockType = anthropicTypeToolUse
		state.openToolKey = toolKey
	}
	*events = append(*events, map[string]any{
		"type":  "content_block_delta",
		"index": info.anthropicBlockIndex,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": toolCall.Function.Arguments,
		},
	})
}

func appendTextDelta(events *[]map[string]any, state *anthropicStreamState, text string) {
	if strings.TrimSpace(text) == "" || events == nil || state == nil {
		return
	}
	ensureTextBlockOpen(events, state)
	*events = append(*events, map[string]any{
		"type":  "content_block_delta",
		"index": state.openBlockIndex,
		"delta": map[string]any{
			"type": "text_delta",
			"text": text,
		},
	})
}

func appendThinkingDelta(events *[]map[string]any, state *anthropicStreamState, thinking string) {
	if strings.TrimSpace(thinking) == "" || events == nil || state == nil {
		return
	}
	ensureThinkingBlockOpen(events, state)
	*events = append(*events, map[string]any{
		"type":  "content_block_delta",
		"index": state.openBlockIndex,
		"delta": map[string]any{
			"type":     "thinking_delta",
			"thinking": thinking,
		},
	})
}

func ensureTextBlockOpen(events *[]map[string]any, state *anthropicStreamState) {
	if state.openBlockType == anthropicTypeText && state.openBlockIndex >= 0 {
		return
	}
	closeOpenContentBlock(events, state)
	index := state.nextBlockIndex
	state.nextBlockIndex++
	*events = append(*events, map[string]any{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]any{
			"type": anthropicTypeText,
			"text": "",
		},
	})
	state.openBlockIndex = index
	state.openBlockType = anthropicTypeText
	state.openToolKey = ""
}

func ensureThinkingBlockOpen(events *[]map[string]any, state *anthropicStreamState) {
	if state.openBlockType == anthropicTypeThinking && state.openBlockIndex >= 0 {
		return
	}
	closeOpenContentBlock(events, state)
	index := state.nextBlockIndex
	state.nextBlockIndex++
	*events = append(*events, map[string]any{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]any{
			"type":     anthropicTypeThinking,
			"thinking": "",
		},
	})
	state.openBlockIndex = index
	state.openBlockType = anthropicTypeThinking
	state.openToolKey = ""
}

func closeOpenContentBlock(events *[]map[string]any, state *anthropicStreamState) {
	if events == nil || state == nil || state.openBlockIndex < 0 {
		return
	}
	*events = append(*events, map[string]any{
		"type":  "content_block_stop",
		"index": state.openBlockIndex,
	})
	if state.openBlockType == anthropicTypeToolUse && state.openToolKey != "" {
		info, ok := state.toolCalls[state.openToolKey]
		if ok {
			info.closed = true
			state.toolCalls[state.openToolKey] = info
		}
	}
	state.openBlockIndex = -1
	state.openBlockType = ""
	state.openToolKey = ""
}

func finalizeAnthropicStreamEvents(state *anthropicStreamState) []map[string]any {
	if state == nil || state.messageStopSent || !state.messageStartSent {
		return nil
	}
	events := make([]map[string]any, 0, 2)
	closeOpenContentBlock(&events, state)
	stopReason := state.stopReason
	if stopReason == nil {
		stopReason = chatFinishReasonEnd
	}
	usage := cloneUsageMap(state.lastUsage)
	if usage == nil {
		usage = map[string]any{
			"input_tokens":  0,
			"output_tokens": 0,
		}
	}
	events = append(events, map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": usage,
	}, map[string]any{
		"type": "message_stop",
	})
	state.messageStopSent = true
	return events
}

func mergeStreamStopReason(current, candidate any) any {
	currentText, _ := current.(string)
	candidateText, _ := candidate.(string)
	if candidateText == "" {
		return current
	}
	if currentText == anthropicTypeToolUse {
		return current
	}
	if candidateText == anthropicTypeToolUse {
		return candidate
	}
	if currentText == "max_tokens" {
		return current
	}
	if candidateText == "max_tokens" {
		return candidate
	}
	if currentText == "" {
		return candidate
	}
	return current
}

func streamToolKey(choiceIndex, toolIndex int) string {
	return fmt.Sprintf("%d:%d", choiceIndex, toolIndex)
}

func cloneUsageMap(usage map[string]any) map[string]any {
	if usage == nil {
		return nil
	}
	copied := make(map[string]any, len(usage))
	for k, v := range usage {
		copied[k] = v
	}
	return copied
}

func writeAnthropicEvents(writer io.Writer, events []map[string]any) {
	for _, event := range events {
		eventType, ok := event["type"].(string)
		if !ok {
			continue
		}
		writeSSEEvent(writer, eventType, event)
	}
}

func buildStreamUsage(chunk *chatToMessagesStreamChunk, forceZeroOutput bool) map[string]any {
	inputTokens, outputTokens, cacheTokens, hasCache := streamUsageValues(chunk)
	if forceZeroOutput {
		outputTokens = 0
	}
	usage := map[string]any{
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
	}
	if hasCache {
		usage["cache_read_input_tokens"] = cacheTokens
	}
	return usage
}

func streamUsageValues(chunk *chatToMessagesStreamChunk) (inputTokens, outputTokens, cacheTokens int, hasCache bool) {
	if chunk == nil || chunk.Usage == nil {
		return 0, 0, 0, false
	}
	prompt := 0
	if chunk.Usage.PromptTokens != nil {
		prompt = *chunk.Usage.PromptTokens
	}
	output := 0
	if chunk.Usage.CompletionTokens != nil {
		output = *chunk.Usage.CompletionTokens
	}
	cached := 0
	cachePresent := false
	if chunk.Usage.PromptTokensDetails != nil && chunk.Usage.PromptTokensDetails.CachedTokens != nil {
		cached = *chunk.Usage.PromptTokensDetails.CachedTokens
		cachePresent = true
	}
	input := prompt - cached
	if input < 0 {
		input = 0
	}
	return input, output, cached, cachePresent
}

type toolCallState struct {
	id                  string
	name                string
	anthropicBlockIndex int
	closed              bool
}

type anthropicStreamState struct {
	messageStartSent bool
	messageStopSent  bool
	nextBlockIndex   int
	openBlockIndex   int
	openBlockType    string
	openToolKey      string
	toolCalls        map[string]toolCallState
	stopReason       any
	streamID         string
	streamModel      string
	lastUsage        map[string]any
}

func writeSSEEvent(w io.Writer, event string, payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("event: " + event + "\n"))
	_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
}

// Intentionally no exported helpers for translation.

func translateAnthropicToolChoice(choice any) any {
	choiceMap, ok := choice.(map[string]any)
	if !ok {
		return nil
	}
	choiceType, _ := choiceMap["type"].(string)
	switch choiceType {
	case toolChoiceAuto:
		return toolChoiceAuto
	case "any":
		return "required"
	case anthropicToolChoice:
		if name, ok := choiceMap["name"].(string); ok && name != "" {
			return map[string]any{
				"type":     "function",
				"function": map[string]any{"name": name},
			}
		}
		return nil
	case toolChoiceNone:
		return toolChoiceNone
	default:
		return nil
	}
}

func getAnthropicTextBlocks(messageContent any) []map[string]any {
	if messageContent == nil {
		return nil
	}
	switch v := messageContent.(type) {
	case string:
		return []map[string]any{{"type": anthropicTypeText, "text": v}}
	case []any:
		blocks := make([]map[string]any, 0)
		for _, part := range v {
			if m, ok := part.(map[string]any); ok {
				partType, _ := m["type"].(string)
				switch partType {
				case anthropicTypeText:
					if text, ok := m["text"].(string); ok {
						blocks = append(blocks, map[string]any{"type": anthropicTypeText, "text": text})
					}
				case "refusal":
					if text, ok := m["text"].(string); ok && text != "" {
						blocks = append(blocks, map[string]any{"type": anthropicTypeText, "text": text})
					}
				case "reasoning_text":
					if text, ok := m["text"].(string); ok && text != "" {
						blocks = append(blocks, map[string]any{"type": anthropicTypeThinking, "thinking": text})
					}
				case anthropicTypeThinking:
					if thinking, ok := m["thinking"].(string); ok && thinking != "" {
						blocks = append(blocks, map[string]any{"type": anthropicTypeThinking, "thinking": thinking})
					}
				}
			}
		}
		return blocks
	}
	return nil
}

func getAnthropicThinkingBlocks(raw any) []map[string]any {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []map[string]any{{"type": anthropicTypeThinking, "thinking": v}}
	case []any:
		blocks := make([]map[string]any, 0, len(v))
		for _, item := range v {
			switch thinking := item.(type) {
			case string:
				if strings.TrimSpace(thinking) != "" {
					blocks = append(blocks, map[string]any{"type": anthropicTypeThinking, "thinking": thinking})
				}
			case map[string]any:
				if text, ok := thinking["text"].(string); ok && strings.TrimSpace(text) != "" {
					blocks = append(blocks, map[string]any{"type": anthropicTypeThinking, "thinking": text})
				}
			}
		}
		return blocks
	default:
		return nil
	}
}

func getAnthropicToolUseBlocks(toolCalls []chatToolCall) ([]map[string]any, bool) {
	if len(toolCalls) == 0 {
		return []map[string]any{}, true
	}
	blocks := make([]map[string]any, 0, len(toolCalls))
	for _, call := range toolCalls {
		input := any(map[string]any{})
		if call.Function.Arguments != "" {
			var decoded any
			if err := json.Unmarshal([]byte(call.Function.Arguments), &decoded); err != nil {
				return nil, false
			}
			input = decoded
		}
		blocks = append(blocks, map[string]any{
			"type":  anthropicTypeToolUse,
			"id":    call.ID,
			"name":  call.Function.Name,
			"input": input,
		})
	}
	return blocks, true
}
