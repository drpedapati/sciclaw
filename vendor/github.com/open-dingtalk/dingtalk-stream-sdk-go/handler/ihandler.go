package handler

import (
	"context"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/payload"
)

/**
 * @Author linya.jj
 * @Date 2023/3/22 14:27
 */

type IFrameHandler func(c context.Context, df *payload.DataFrame) (*payload.DataFrameResponse, error)
