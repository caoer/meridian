package cli

import (
	"runtime"

	"github.com/caoer/meridian/internal/version"
)

func NewVersionHandler() Handler {
	return func(req *Request) *Response {
		return &Response{
			Version: ResponseVersion,
			Data: map[string]string{
				"meridian": version.Info(),
				"go":       runtime.Version(),
				"platform": runtime.GOOS + "/" + runtime.GOARCH,
			},
		}
	}
}
