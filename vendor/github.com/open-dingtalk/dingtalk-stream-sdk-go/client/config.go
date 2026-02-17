package client

import (
	"errors"
)

/**
 * @Author linya.jj
 * @Date 2023/3/22 14:50
 */

// 应用秘钥信息
type AppCredentialConfig struct {
	ClientId     string `json:"clientKey" yaml:"clientKey"`       //自建应用appKey; 三方应用suiteKey
	ClientSecret string `json:"clientSecret" yaml:"clientSecret"` //自建应用appSecret; 三方应用suiteSecret
}

func NewAppCredentialConfig(clientId, clientSecret string) *AppCredentialConfig {
	return &AppCredentialConfig{
		ClientId:     clientId,
		ClientSecret: clientSecret,
	}
}

func (c *AppCredentialConfig) Valid() error {
	if c == nil {
		return errors.New("AppCredentialConfigNil")
	}

	if c.ClientId == "" || c.ClientSecret == "" {
		return errors.New("AppCredentialConfigEmpty")
	}

	return nil
}

// UA信息
type UserAgentConfig struct {
	UserAgent string `json:"user_agent"`
}

func NewDingtalkGoSDKUserAgent() *UserAgentConfig {
	return &UserAgentConfig{
		UserAgent: "dingtalk-sdk-go/v0.9.1",
	}
}

func (c *UserAgentConfig) Valid() error {
	if c == nil {
		return errors.New("UserAgentConfigNil")
	}

	if c.UserAgent == "" {
		return errors.New("UserAgentConfigEmpty")
	}

	return nil
}
