package utils

import (
	"errors"
	"io"
	"net/http"
)

/**
 * @Author linya.jj
 * @Date 2023/3/31 09:51
 */

// 把http response的内容转换成error对象
func ErrorFromHttpResponseBody(resp *http.Response) error {
	if resp == nil {
		return errors.New("HttpResponseNil")
	}

	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return errors.New(string(responseBody))
}
