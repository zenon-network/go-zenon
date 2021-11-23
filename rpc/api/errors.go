package api

import (
	"github.com/zenon-network/go-zenon/common"
)

var (
	ErrPageSizeParamTooBig  = common.NewErrorWCode(-32000, "page-size parameter is too big")
	ErrPageIndexParamTooBig = common.NewErrorWCode(-32000, "page-index parameter is too big")
	ErrCountParamTooBig     = common.NewErrorWCode(-32000, "count parameter is too big")
	ErrHeightParamIsZero    = common.NewErrorWCode(-32000, "height parameter must be strictly greater than zero")
	ErrParamIsNull          = common.NewErrorWCode(-32000, "parameter must not be null")
)
