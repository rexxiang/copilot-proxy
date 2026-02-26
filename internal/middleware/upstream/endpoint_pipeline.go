package upstream

import (
	"fmt"
	"net/http"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/middleware/upstream/transform"
	"copilot-proxy/internal/models"
)

// MessagesTranslateMiddleware atomically handles endpoint-related request processing.
type MessagesTranslateMiddleware struct {
	catalog     models.Catalog
	selector    *models.Selector
	pathMapping map[string]string
	codec       transform.EndpointCodec
}

// NewMessagesTranslate builds a single middleware for:
// model rewrite -> endpoint select -> endpoint transform -> path rewrite.
func NewMessagesTranslate(catalog models.Catalog, selector *models.Selector, mapping map[string]string) *MessagesTranslateMiddleware {
	if selector == nil {
		selector = models.NewSelector()
	}
	return &MessagesTranslateMiddleware{
		catalog:     catalog,
		selector:    selector,
		pathMapping: mapping,
		codec: transform.EndpointCodec{
			MessagesToChatRequest:       transform.MessagesToChatRequest,
			ChatToMessagesResponse:      transform.ChatToMessagesResponse,
			ChatSSEToMessages:           transform.TranslateChatSSEToMessages,
			MessagesToResponsesRequest:  transform.MessagesToResponsesRequest,
			ResponsesToMessagesResponse: transform.ResponsesToMessagesResponse,
			ResponsesSSEToMessages:      transform.TranslateResponsesSSEToMessages,
		},
	}
}

func (m *MessagesTranslateMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}

	req := ctx.Request
	rc := ensureRequestContext(req)
	transform.RewriteModel(req, rc, m.catalog, m.selector)
	transform.SelectTargetEndpoint(req, rc)
	ctx.Request = withRequestContext(req, rc)

	resp, err := transform.ApplyEndpointTransform(ctx.Request, rc, m.codec, func(req *http.Request) (*http.Response, error) {
		ctx.Request = req
		rc, ok := middleware.RequestContextFrom(req.Context())
		if !ok || rc == nil {
			rc = ensureRequestContext(req)
			ctx.Request = withRequestContext(req, rc)
		}
		transform.ApplyUpstreamPath(req, rc, m.pathMapping)
		return next()
	})
	if err != nil {
		return nil, fmt.Errorf("apply endpoint transform: %w", err)
	}
	return resp, nil
}
