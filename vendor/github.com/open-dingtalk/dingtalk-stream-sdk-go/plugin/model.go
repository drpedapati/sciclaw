package plugin

import (
	"encoding/json"
)

type GraphRequestLine struct {
	Method string `json:"method"`
	Uri    string `json:"uri"`
	Path   string `json:"-"`
}
type GraphRequest struct {
	RequestLine GraphRequestLine  `json:"requestLine"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
}
type GraphStatusLine struct {
	Code   int    `json:"code"`
	Reason string `json:"reasonPhrase"`
}
type GraphResponse struct {
	StatusLine GraphStatusLine   `json:"statusLine"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

// 用于将数据转换成插件的请求参数
func (req *GraphRequest) ParseRequest(pluginRequest interface{}) error {
	data, err := json.Marshal(req.Body)
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, pluginRequest)
	if err != nil {
		return err
	}
	return nil
}

type PluginResponse struct {
	Result    interface{} `json:"result"`
	RequestId string      `json:"requestId"`
}
