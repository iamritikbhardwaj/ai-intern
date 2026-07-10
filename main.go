package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"google.golang.org/api/genai"
	"google.golang.org/api/option"
)

// Swap the internal AI client pass inside your Agent constructor
func (ai *AgentIntern) analyzeRequirementsWithGemini(rawEmail string) string {
	ctx := context.Background()

	// Pulls your standard GEMINI_API_KEY environment variable securely
	apiKey := os.Getenv("GEMINI_API_KEY")

	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		log.Fatalf("Failed to initialize Gemini client: %v", err)
	}

	// Read memory logs before prompting
	pastFeedback := ai.readMemoryAndFeedback()

	prompt := fmt.Sprintf(`
		You are an elite software engineering intern. Analyze the following client requirement.
		
		CRITICAL RULES TO REMEMBER (PAST FEEDBACK):
		%s

		CLIENT REQUIREMENT EMAIL:
		%s
	`, pastFeedback, rawEmail)

	// Call Gemini 1.5 Pro or Flash for high-performance reasoning passes
	resp, err := client.Models.GenerateContent(ctx, "gemini-1.5-pro", genai.Text(prompt), nil)
	if err != nil {
		log.Printf("Gemini generation failure: %v", err)
		return ""
	}

	return resp.Candidates[0].Content.Parts[0].(genai.Text).String()
}
