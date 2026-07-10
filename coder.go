package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/genai"
)

// GenerateFeatureCode dispatches target code modification instructions to disk storage
func (ai *AgentIntern) GenerateFeatureCode(ctx context.Context, requirementText string, base64VisualPrompt string, targetFileRelativePath string) (string, error) {
	// 1. Resolve target workspace paths defensively
	absoluteFilePath := filepath.Join(ai.TargetDir, targetFileRelativePath)

	var existingCodeBaseline string
	if fileBytes, err := os.ReadFile(absoluteFilePath); err == nil {
		existingCodeBaseline = fmt.Sprintf("\n### Existing File Baseline:\n```typescript\n%s\n```", string(fileBytes))
	} else {
		existingCodeBaseline = "\n(Target file is a new asset block vector - build fresh)"
	}

	log.Printf("[Code Synthesis] 🏗️ Engineering modification context payload for target: %s\n", targetFileRelativePath)

	systemDirectives := fmt.Sprintf(`
		You are an elite software engineering intern. Write optimized, type-safe clean code matching standard TypeScript architectures.
		
		Target Output Target File Path: %s
		%s

		Strict Coding Directives:
		1. Return ONLY the raw file execution content. No conversational introductions, no markdown backticks block structures.
		2. Follow standard structural patterns. Write matching unit tests if relevant.
	`, targetFileRelativePath, existingCodeBaseline)

	// Combine components into a pure high-efficiency text layout matrix
	promptParts := []*genai.Part{
		{Text: systemDirectives},
		{Text: fmt.Sprintf("Client Request Tasks Matrix:\n%s", requirementText)},
	}

	log.Println("[Code Synthesis] 🧠 Dispatching file writer context to Gemini 2.5 Flash...")

	// Request code production pass securely via your rate-limiting wrapper
	resp, err := ai.GenerateContentSafe(ctx, "gemini-2.5-flash", &genai.Content{Parts: promptParts})
	if err != nil {
		return "", fmt.Errorf("gemini code synthesis request failed: %w", err)
	}

	cleanCodeContent := resp.Text()
	cleanCodeContent = strings.TrimPrefix(cleanCodeContent, "```typescript")
	cleanCodeContent = strings.TrimPrefix(cleanCodeContent, "```ts")
	cleanCodeContent = strings.TrimPrefix(cleanCodeContent, "```")
	cleanCodeContent = strings.TrimSuffix(cleanCodeContent, "```")
	cleanCodeContent = strings.TrimSpace(cleanCodeContent)

	// 2. Persist the code directly into your flights-scanner workspace
	if err := os.MkdirAll(filepath.Dir(absoluteFilePath), 0755); err != nil {
		return "", fmt.Errorf("failed to generate system target directories: %w", err)
	}
	if err := os.WriteFile(absoluteFilePath, []byte(cleanCodeContent), 0644); err != nil {
		return "", fmt.Errorf("failed to save generated code payload: %w", err)
	}

	log.Printf("[Code Synthesis] ✅ Successfully wrote source changes down to file asset point.")
	return absoluteFilePath, nil
}
