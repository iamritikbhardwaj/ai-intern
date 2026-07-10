package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/genai"
)

// GenerateFeatureCode dispatches the visual context maps alongside target code modification text payloads
func (ai *AgentIntern) GenerateFeatureCode(ctx context.Context, requirementText string, base64VisualPrompt string, targetFileRelativePath string) (string, error) {
	// 1. Defensively resolve the target output file workspace path context
	absoluteFilePath := filepath.Join(ai.TargetDir, targetFileRelativePath)

	// Read current existing code baseline if file is an update patch operation instead of a new file vector
	var existingCodeBaseline string
	if fileBytes, err := os.ReadFile(absoluteFilePath); err == nil {
		existingCodeBaseline = fmt.Sprintf("\n### Existing File Baseline Code Data:\n```typescript\n%s\n```", string(fileBytes))
	} else {
		existingCodeBaseline = "\n(Target file is a new asset block insertion vector - create from scratch)"
	}

	log.Printf("[Code Synthesis] 🏗️ Engineering modification context payload for target: %s\n", targetFileRelativePath)

	systemDirectives := fmt.Sprintf(`
		You are an elite software engineering intern. Write highly optimized, type-safe clean code matching standard TypeScript architectures.
		
		Target Output Target File Path: %s
		%s

		Strict Coding Directives:
		1. Return ONLY the raw file execution content. No conversational introduction, no markdown backticks block syntax.
		2. Follow standard structural patterns. Write matching unit tests if relevant.
	`, targetFileRelativePath, existingCodeBaseline)

	// Decode the base64 string back into raw image bytes for the Blob structure
	imageBytes, err := base64.StdEncoding.DecodeString(base64VisualPrompt)
	if err != nil {
		return "", fmt.Errorf("failed to decode visual memory base64 payload: %w", err)
	}

	// Combine components into the multi-part request matrix
	promptParts := []*genai.Part{
		{InlineData: &genai.Blob{Data: imageBytes, MIMEType: "image/png"}}, // ✨ FIX: Passed decoded []byte instead of string
		{Text: systemDirectives},
		{Text: fmt.Sprintf("Client Request Tasks Matrix:\n%s", requirementText)},
	}

	// ✨ Fixed: Pass the content object address natively as a direct parameter block to satisfy the variadic method signature
	resp, err := ai.GenerateContentSafe(ctx, "gemini-2.5-pro", &genai.Content{Parts: promptParts})
	if err != nil {
		return "", fmt.Errorf("gemini code synthesis request failed: %w", err)
	}

	// ✨ FIX: Called resp.Text() as a method to properly retrieve the text string
	cleanCodeContent := resp.Text()
	cleanCodeContent = strings.TrimPrefix(cleanCodeContent, "```typescript")
	cleanCodeContent = strings.TrimPrefix(cleanCodeContent, "```ts")
	cleanCodeContent = strings.TrimPrefix(cleanCodeContent, "```")
	cleanCodeContent = strings.TrimSuffix(cleanCodeContent, "```")
	cleanCodeContent = strings.TrimSpace(cleanCodeContent)

	// 3. Persist the generated feature code directly down to disk storage inside your TS project space
	if err := os.MkdirAll(filepath.Dir(absoluteFilePath), 0755); err != nil {
		return "", fmt.Errorf("failed to generate system target directories: %w", err)
	}
	if err := os.WriteFile(absoluteFilePath, []byte(cleanCodeContent), 0644); err != nil {
		return "", fmt.Errorf("failed to save generated code payload: %w", err)
	}

	log.Printf("[Code Synthesis] ✅ Successfully wrote source changes to file asset point.")
	return absoluteFilePath, nil
}
