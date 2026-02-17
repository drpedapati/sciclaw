package payload

import "errors"

/**
 * @Author linya.jj
 * @Date 2023/3/22 18:22
 */

type SubscriptionModel struct {
	Type  string `json:"type"`
	Topic string `json:"topic"`
}

// 长连接接入点请求
type ConnectionEndpointRequest struct {
	ClientId      string               `json:"clientId"`     //自建应用appKey; 三方应用suiteKey
	ClientSecret  string               `json:"clientSecret"` //自建应用appSecret; 三方应用suiteSecret
	Subscriptions []*SubscriptionModel `json:"subscriptions"`
	UserAgent     string               `json:"ua"`
	LocalIP       string               `json:"localIp"`
	Extras        map[string]string    `json:"extras"`
}

// 长连接接入点参数
type ConnectionEndpointResponse struct {
	Endpoint string `json:"endpoint"`
	Ticket   string `json:"ticket"`
}

func (r *ConnectionEndpointResponse) Valid() error {
	if r == nil {
		return errors.New("ConnectionEndpointResponseNil")
	}

	if r.Endpoint == "" || r.Ticket == "" {
		return errors.New("ConnectionEndpointResponseContentEmpty")
	}

	return nil
}
