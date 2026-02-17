package card

import (
	"context"
	"encoding/json"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/payload"
)

type ICardCallbackHandler func(c context.Context, request *CardRequest) (*CardResponse, error)

type DefaultCardCallbackFrameHandler struct {
	defaultHandler ICardCallbackHandler
}

func NewDefaultPluginFrameHandler(defaultHandler ICardCallbackHandler) *DefaultCardCallbackFrameHandler {
	return &DefaultCardCallbackFrameHandler{
		defaultHandler: defaultHandler,
	}
}

func (h *DefaultCardCallbackFrameHandler) OnEventReceived(ctx context.Context, df *payload.DataFrame) (*payload.DataFrameResponse, error) {
	msgData := &CardRequest{}
	err := json.Unmarshal([]byte(df.Data), msgData)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(msgData.Content), &msgData.CardActionData)

	if h.defaultHandler == nil {
		return payload.NewDataFrameResponse(payload.DataFrameResponseStatusCodeKHandlerNotFound), nil
	}

	result, err := h.defaultHandler(ctx, msgData)
	if err != nil {
		return nil, err
	}
	frameResp := payload.NewSuccessDataFrameResponse()
	callbackData := make(map[string]any)
	callbackData["response"] = result
	if err = frameResp.SetJson(callbackData); err != nil {
		return nil, err
	}
	return frameResp, nil
}
