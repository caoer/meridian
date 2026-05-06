package cli

import "runtime"

func newVersionHandler() Handler {
	return func(req *Request) *Response {
		return &Response{
			Version: ResponseVersion,
			Data: map[string]string{
				"meridian": "0.1.0",
				"go":       runtime.Version(),
				"platform": runtime.GOOS + "/" + runtime.GOARCH,
			},
		}
	}
}
