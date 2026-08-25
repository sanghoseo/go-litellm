package types

type ModerationRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type ModerationCategories map[string]bool
type ModerationCategoryScores map[string]float64

type ModerationResult struct {
	Flagged        bool                     `json:"flagged"`
	Categories     ModerationCategories     `json:"categories"`
	CategoryScores ModerationCategoryScores `json:"category_scores"`
}

type ModerationResponse struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Results []ModerationResult `json:"results"`
}
