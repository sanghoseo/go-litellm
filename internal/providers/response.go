package providers

import (
	"context"
	"io"
	"net/http"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

type Response struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

type ChatCompleter interface {
	ChatCompletion(context.Context, config.Model, []byte) (Response, error)
}
