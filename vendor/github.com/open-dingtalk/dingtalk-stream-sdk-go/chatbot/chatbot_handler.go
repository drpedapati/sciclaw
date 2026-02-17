package chatbot

import (
	"context"
	"encoding/json"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/payload"
)

/**
 * @Author linya.jj
 * @Date 2023/3/22 18:30
 */

type IChatBotMessageHandler func(c context.Context, data *BotCallbackDataModel) ([]byte, error)

type DefaultChatBotFrameHandler struct {
	defaultHandler IChatBotMessageHandler
}

func NewDefaultChatBotFrameHandler(defaultHandler IChatBotMessageHandler) *DefaultChatBotFrameHandler {
	return &DefaultChatBotFrameHandler{
		defaultHandler: defaultHandler,
	}
}

func (h *DefaultChatBotFrameHandler) OnEventReceived(ctx context.Context, df *payload.DataFrame) (*payload.DataFrameResponse, error) {
	msgData := &BotCallbackDataModel{}
	err := json.Unmarshal([]byte(df.Data), msgData)
	if err != nil {
		return nil, err
	}

	if h.defaultHandler == nil {
		return payload.NewDataFrameResponse(payload.DataFrameResponseStatusCodeKHandlerNotFound), nil
	}

	data, err := h.defaultHandler(ctx, msgData)
	if err != nil {
		return nil, err
	}

	frameResp := payload.NewSuccessDataFrameResponse()
	frameResp.SetData(string(data))
	return frameResp, nil
}
