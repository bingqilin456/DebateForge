package ai

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"
)

// SDK 会在空 choices 等情况下提前失败，必须在解码前保存计费事实。
// 观察状态通过单次调用的 Context 传递，不在共享 Transport 上保存请求数据。
type observationContextKey struct{}

type responseObserverTransport struct{ base http.RoundTripper }

const maxProviderResponseBytes = 1 << 20

var errInvalidProviderResponse = errors.New("模型响应超过大小限制")

func (t responseObserverTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(req)
	if err != nil || response == nil {
		return response, err
	}
	observation, ok := req.Context().Value(observationContextKey{}).(*responseObservation)
	if !ok {
		return response, nil
	}
	observation.httpStatus = response.StatusCode
	if response.Body == nil {
		observation.invalidBody = true
		return response, nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	_ = response.Body.Close()
	if readErr != nil {
		observation.invalidBody = true
		return nil, readErr
	}
	if len(raw) > maxProviderResponseBytes {
		observation.invalidBody = true
		return nil, errInvalidProviderResponse
	}
	*observation = parseResponseObservation(raw)
	observation.httpStatus = response.StatusCode
	observation.invalidUTF8 = !utf8.Valid(raw)
	// 原始字节继续交给官方 Eino/SDK 解析；本包只保留白名单事实，不留正文。
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return response, nil
}
