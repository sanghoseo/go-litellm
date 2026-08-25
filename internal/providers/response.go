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

type ResponseCreator interface {
	CreateResponse(context.Context, config.Model, []byte) (Response, error)
}

type Embedder interface {
	CreateEmbedding(context.Context, config.Model, []byte) (Response, error)
}

type ImageGenerator interface {
	GenerateImage(context.Context, config.Model, []byte) (Response, error)
}

type SpeechCreator interface {
	CreateSpeech(context.Context, config.Model, []byte) (Response, error)
}
