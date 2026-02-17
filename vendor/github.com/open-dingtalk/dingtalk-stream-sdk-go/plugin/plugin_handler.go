package plugin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/payload"
)

type CallbackResponse struct {
	Response interface{} `json:"response"`
}

type IPluginMessageHandler func(c context.Context, data *GraphRequest) (*GraphResponse, error)

type DefaultPluginFrameHandler struct {
	defaultHandler IPluginMessageHandler
}

func NewDefaultPluginFrameHandler(defaultHandler IPluginMessageHandler) *DefaultPluginFrameHandler {
	return &DefaultPluginFrameHandler{
		defaultHandler: defaultHandler,
	}
}

func (h *DefaultPluginFrameHandler) OnEventReceived(ctx context.Context, df *payload.DataFrame) (*payload.DataFrameResponse, error) {
	msgData := &GraphRequest{}
	err := json.Unmarshal([]byte(df.Data), msgData)
	if err != nil {
		return nil, err
	}
	pos := strings.Index(msgData.RequestLine.Uri, "?")
	if pos >= 0 {
		msgData.RequestLine.Path = msgData.RequestLine.Uri[:pos]
	} else {
		msgData.RequestLine.Path = msgData.RequestLine.Uri
	}

	if h.defaultHandler == nil {
		return payload.NewDataFrameResponse(payload.DataFrameResponseStatusCodeKHandlerNotFound), nil
	}

	result, err := h.defaultHandler(ctx, msgData)
	if err != nil {
		return nil, err
	}
	result.StatusLine.Code = 200
	result.StatusLine.Reason = "OK"
	callbackResponse := &CallbackResponse{Response: result}
	frameResp := payload.NewSuccessDataFrameResponse()
	if err = frameResp.SetJson(callbackResponse); err != nil {
		return nil, err
	}
	return frameResp, nil
}
