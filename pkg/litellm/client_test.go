package litellm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	proxytpes "github.com/BerriAI/litellm/go-proxy/pkg/types"
)

func TestCompletionUsesOpenAIProxyContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer client-key" {
			t.Fatalf("request path=%s authorization=%s", request.URL.Path, request.Header.Get("Authorization"))
		}
		var payload proxytpes.ChatCompletionRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.Model != "gateway-model" {
			t.Fatalf("payload=%#v err=%v", payload, err)
		}
		_, _ = writer.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	defer server.Close()
	response, err := (Client{BaseURL: server.URL + "/v1", APIKey: "client-key", HTTPClient: server.Client()}).Completion(context.Background(), proxytpes.ChatCompletionRequest{Model: "gateway-model"})
	if err != nil || response.ID != "chatcmpl-1" || response.Usage.TotalTokens != 3 {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

func TestCompletionReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer server.Close()
	_, err := (Client{BaseURL: server.URL, HTTPClient: server.Client()}).Completion(context.Background(), proxytpes.ChatCompletionRequest{Model: "gateway-model"})
	apiError, ok := err.(*APIError)
	if !ok || apiError.StatusCode != http.StatusUnauthorized || apiError.Message != "bad key" {
		t.Fatalf("error=%#v", err)
	}
}

func TestTextCompletionUsesOpenAIProxyContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/completions" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		var payload proxytpes.TextCompletionRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.Model != "gateway-model" || payload.Prompt != "hello" {
			t.Fatalf("payload=%#v err=%v", payload, err)
		}
		_, _ = writer.Write([]byte(`{"id":"cmpl-test","choices":[{"text":"world","index":0,"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	response, err := (Client{BaseURL: server.URL + "/v1", HTTPClient: server.Client()}).TextCompletion(context.Background(), proxytpes.TextCompletionRequest{Model: "gateway-model", Prompt: "hello"})
	if err != nil || response.ID != "cmpl-test" || len(response.Choices) != 1 || response.Choices[0].Text != "world" {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

func TestClientModerationAndRerank(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/moderations":
			_, _ = writer.Write([]byte(`{"id":"modr-1","results":[{"flagged":false,"categories":{},"category_scores":{}}]}`))
		case "/v1/rerank":
			_, _ = writer.Write([]byte(`{"results":[{"index":0,"relevance_score":0.9}]}`))
		default:
			t.Fatalf("path=%q", request.URL.Path)
		}
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL + "/v1", HTTPClient: server.Client()}
	moderation, err := client.Moderation(context.Background(), proxytpes.ModerationRequest{Model: "moderation-model", Input: "hello"})
	if err != nil || moderation.ID != "modr-1" || len(moderation.Results) != 1 || moderation.Results[0].Flagged {
		t.Fatalf("moderation=%#v err=%v", moderation, err)
	}
	rerank, err := client.Rerank(context.Background(), proxytpes.RerankRequest{Model: "rerank-model", Query: "hello", Documents: []string{"hello"}})
	if err != nil || len(rerank.Results) != 1 || rerank.Results[0].RelevanceScore != 0.9 {
		t.Fatalf("rerank=%#v err=%v", rerank, err)
	}
}

func TestClientImageGeneration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/images/generations" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"created":1,"data":[{"url":"https://image.example/test"}]}`))
	}))
	defer server.Close()
	response, err := (Client{BaseURL: server.URL + "/v1", HTTPClient: server.Client()}).ImageGeneration(context.Background(), proxytpes.ImageGenerationRequest{Model: "image-model", Prompt: "a lighthouse"})
	if err != nil || len(response.Data) != 1 || response.Data[0].URL != "https://image.example/test" {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

func TestResponseUsesOpenAIResponsesContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"id":"resp_1","object":"response","model":"gateway-model","output":[]}`))
	}))
	defer server.Close()
	response, err := (Client{BaseURL: server.URL + "/v1", HTTPClient: server.Client()}).Response(context.Background(), proxytpes.ResponsesRequest{Model: "gateway-model", Input: json.RawMessage(`"hello"`)})
	if err != nil || response.ID != "resp_1" {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

func TestCompletionStreamReadsOpenAISSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("accept = %q", request.Header.Get("Accept"))
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	stream, err := (Client{BaseURL: server.URL, HTTPClient: server.Client()}).CompletionStream(context.Background(), proxytpes.ChatCompletionRequest{Model: "gateway-model"})
	if err != nil {
		t.Fatal(err)
	}
	chunk := <-stream.Chunks
	if chunk.ID != "chunk-1" || chunk.Choices[0].Delta.Content != "hello" {
		t.Fatalf("chunk = %#v", chunk)
	}
	if _, open := <-stream.Chunks; open {
		t.Fatal("stream must close after [DONE]")
	}
	if err, open := <-stream.Errors; open || err != nil {
		t.Fatalf("stream error = %v open=%t", err, open)
	}
}

func TestTextCompletionStreamReadsOpenAISSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/completions" || request.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("path=%q accept=%q", request.URL.Path, request.Header.Get("Accept"))
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"id\":\"cmpl-chunk\",\"object\":\"text_completion\",\"choices\":[{\"text\":\"hello\",\"index\":0}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	stream, err := (Client{BaseURL: server.URL, HTTPClient: server.Client()}).TextCompletionStream(context.Background(), proxytpes.TextCompletionRequest{Model: "gateway-model", Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	chunk := <-stream.Chunks
	if chunk.ID != "cmpl-chunk" || chunk.Choices[0].Text != "hello" {
		t.Fatalf("chunk=%#v", chunk)
	}
	if _, open := <-stream.Chunks; open {
		t.Fatal("stream must close after [DONE]")
	}
}
