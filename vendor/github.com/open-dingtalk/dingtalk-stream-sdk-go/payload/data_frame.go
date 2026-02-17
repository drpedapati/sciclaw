package payload

import (
	"encoding/json"
	"strconv"
)

/**
 * @Author linya.jj
 * @Date 2023/3/31 09:57
 */

type DataFrameHeader map[string]string

func (h DataFrameHeader) Get(key string) string {
	return h[key]
}

func (h DataFrameHeader) Set(key, value string) {
	h[key] = value
}

type DataFrame struct {
	SpecVersion string          `json:"specVersion"`
	Type        string          `json:"type"`
	Time        int64           `json:"time"`
	Headers     DataFrameHeader `json:"headers"`
	Data        string          `json:"data"`
}

func (df *DataFrame) Encode() []byte {
	if df == nil {
		return nil
	}

	data, _ := json.Marshal(df)
	return data
}

func (df *DataFrame) GetTopic() string {
	if df == nil {
		return ""
	}

	return df.Headers.Get(DataFrameHeaderKTopic)
}

func (df *DataFrame) GetMessageId() string {
	if df == nil {
		return ""
	}

	return df.Headers.Get(DataFrameHeaderKMessageId)
}

func (df *DataFrame) GetTimestamp() int64 {
	if df == nil {
		return 0
	}

	strTs := df.Headers.Get(DataFrameHeaderKTime)
	ts, err := strconv.ParseInt(strTs, 10, 64)
	if err != nil {
		return 0
	}
	return ts
}

func (df *DataFrame) GetHeader(header string) string {
	if df == nil {
		return ""
	}

	return df.Headers.Get(header)
}

func DecodeDataFrame(rawData []byte) (*DataFrame, error) {
	df := &DataFrame{}

	err := json.Unmarshal(rawData, df)
	if err != nil {
		return nil, err
	}

	return df, nil
}

type DataFrameResponse struct {
	Code    int             `json:"code"`
	Headers DataFrameHeader `json:"headers"`
	Message string          `json:"message"`
	Data    string          `json:"data"`
}

func NewDataFrameResponse(code int) *DataFrameResponse {
	return &DataFrameResponse{
		Code:    code,
		Headers: DataFrameHeader{},
		Message: "",
		Data:    "",
	}
}

func NewSuccessDataFrameResponse() *DataFrameResponse {
	return NewDataFrameResponse(DataFrameResponseStatusCodeKOK)
}

func (r *DataFrameResponse) SetHeader(key, value string) {
	if r == nil {
		return
	}

	r.Headers.Set(key, value)
}

func (r *DataFrameResponse) GetHeader(key string) string {
	if r == nil {
		return ""
	}

	return r.Headers.Get(key)
}

func (r *DataFrameResponse) SetData(data string) {
	if r == nil {
		return
	}

	r.Data = data
}

func (r *DataFrameResponse) SetJson(dataModel interface{}) error {
	if r == nil {
		return nil
	}

	data, err := json.Marshal(dataModel)
	if err != nil {
		return err
	}

	r.Data = string(data)
	return nil
}

func (df *DataFrameResponse) Encode() []byte {
	if df == nil {
		return nil
	}

	data, _ := json.Marshal(df)
	return data
}

func DecodeDataFrameResponse(rawData []byte) (*DataFrameResponse, error) {
	resp := &DataFrameResponse{}

	err := json.Unmarshal(rawData, resp)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func NewDataFrameAckPong(messageId string) *DataFrameResponse {
	return &DataFrameResponse{
		Code: DataFrameResponseStatusCodeKOK,
		Headers: DataFrameHeader{
			DataFrameHeaderKContentType: DataFrameContentTypeKJson,
			DataFrameHeaderKMessageId:   messageId,
		},
		Message: "ok",
		Data:    "",
	}
}

func NewErrorDataFrameResponse(err error) *DataFrameResponse {
	if err == nil {
		return nil
	}

	return &DataFrameResponse{
		Code:    DataFrameResponseStatusCodeKInternalError,
		Headers: DataFrameHeader{},
		Message: err.Error(),
		Data:    "",
	}
}
