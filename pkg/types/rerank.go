package types

type RerankRequest struct {
	Model     string `json:"model"`
	Query     string `json:"query"`
	Documents any    `json:"documents"`
	TopN      int    `json:"top_n,omitempty"`
}

type RerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
	Document       any     `json:"document,omitempty"`
}

type RerankResponse struct {
	Results []RerankResult `json:"results"`
}
